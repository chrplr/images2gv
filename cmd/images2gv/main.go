/*
 * images2gv - Convert image sequences to .gv format
 * Copyright (C) 2026 Christophe Pallier
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/pierrec/lz4/v4"
)

// GVHeader matches the binary format expected by gvvideo players
type GVHeader struct {
	Width      uint32
	Height     uint32
	FrameCount uint32
	FPS        float32
	Format     uint32 // 0 = Raw RGBA, 1 = DXT1, 3 = DXT3, 5 = DXT5, 7 = BC7
	FrameBytes uint32 // Total uncompressed bytes per frame
}

type FrameIndex struct {
	Address uint64
	Size    uint64
}

type result struct {
	index int
	data  []byte
	err   error
}

func main() {
	fpsFlag := flag.Float64("fps", 60.0, "Frames per second")
	forceSize := flag.Bool("force-size", false,
		"Crop/pad frames that do not match the first frame instead of failing")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <input_dir> <output.gv>\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	inputDir := flag.Arg(0)
	outputFile := flag.Arg(1)

	// 1. Get and sort all image files
	// Supporting PNG, JPG, GIF
	var files []string
	patterns := []string{"*.png", "*.jpg", "*.jpeg", "*.gif"}
	for _, p := range patterns {
		matches, _ := filepath.Glob(filepath.Join(inputDir, p))
		files = append(files, matches...)
	}

	if len(files) == 0 {
		log.Fatalf("No images found in %s", inputDir)
	}

	// Order numerically, not lexicographically: with unpadded names a plain
	// string sort puts frame_10 before frame_2 and the clip plays out of order.
	lexical := slices.Clone(files)
	slices.Sort(lexical)
	slices.SortFunc(files, naturalCompare)

	if !slices.Equal(files, lexical) {
		fmt.Fprintf(os.Stderr,
			"Note: using natural numeric order; a plain string sort would order these differently.\n"+
				"      First frame: %s\n", filepath.Base(files[0]))
	}

	// 2. Get dimensions from the first frame
	firstFile, err := os.Open(files[0])
	if err != nil {
		log.Fatal(err)
	}
	img, _, err := image.Decode(firstFile)
	firstFile.Close()
	if err != nil {
		log.Fatalf("Could not decode first image %s: %v", files[0], err)
	}
	bounds := img.Bounds()
	width, height := uint32(bounds.Dx()), uint32(bounds.Dy())

	// Check every frame's size before creating the output file. A frame that
	// silently gets cropped or letterboxed is the kind of fault that only
	// surfaces once the data are analysed, so refuse by default. Doing it here
	// rather than mid-encode means a rejected sequence leaves no partial .gv
	// behind, and reports every offending file at once.
	if !*forceSize {
		if err := checkFrameSizes(files, width, height); err != nil {
			log.Fatal(err)
		}
	}

	// 3. Create Output File
	out, err := os.Create(outputFile)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	// 4. Write Header
	header := GVHeader{
		Width:      width,
		Height:     height,
		FrameCount: uint32(len(files)),
		FPS:        float32(*fpsFlag),
		Format:     0, // 0 = Raw RGBA
		FrameBytes: width * height * 4,
	}
	if err := binary.Write(out, binary.LittleEndian, header); err != nil {
		log.Fatal(err)
	}

	// 5. Process Frames in Parallel
	numCPU := runtime.NumCPU()
	jobs := make(chan int, len(files))
	results := make(chan result, len(files))
	var wg sync.WaitGroup

	// Worker Pool
	for w := 0; w < numCPU; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				data, err := processFrame(files[i], width, height)
				results <- result{index: i, data: data, err: err}
			}
		}()
	}

	// Feed jobs
	for i := 0; i < len(files); i++ {
		jobs <- i
	}
	close(jobs)

	// Collect results in order and write to file
	offsets := make([]FrameIndex, len(files))
	receivedCount := 0
	orderedResults := make(map[int][]byte)

	for receivedCount < len(files) {
		res := <-results
		if res.err != nil {
			log.Fatalf("\nError processing frame %d (%s): %v", res.index, files[res.index], res.err)
		}
		orderedResults[res.index] = res.data

		// Write any consecutive ready results
		for {
			data, ok := orderedResults[receivedCount]
			if !ok {
				break
			}

			currentPos, _ := out.Seek(0, io.SeekCurrent)
			offsets[receivedCount] = FrameIndex{
				Address: uint64(currentPos),
				Size:    uint64(len(data)),
			}

			if _, err := out.Write(data); err != nil {
				log.Fatal(err)
			}

			delete(orderedResults, receivedCount)
			receivedCount++
			fmt.Printf("\rProcessed %d/%d frames...", receivedCount, len(files))
		}
	}
	wg.Wait()

	// 6. Write Address Table at the end of the file
	for _, idx := range offsets {
		if err := binary.Write(out, binary.LittleEndian, idx.Address); err != nil {
			log.Fatal(err)
		}
		if err := binary.Write(out, binary.LittleEndian, idx.Size); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf("\nDone! Saved %d frames to %s (%dx%d, %.2f fps)\n", len(files), outputFile, width, height, *fpsFlag)
}

// checkFrameSizes verifies that every image matches the given dimensions.
// It reads only image headers (DecodeConfig), so it is cheap even for long
// sequences. All mismatches are reported together rather than one per run.
func checkFrameSizes(files []string, w, h uint32) error {
	type mismatch struct {
		path string
		w, h int
	}
	var bad []mismatch

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("could not read %s: %w", path, err)
		}
		if uint32(cfg.Width) != w || uint32(cfg.Height) != h {
			bad = append(bad, mismatch{path, cfg.Width, cfg.Height})
		}
	}
	if len(bad) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d frames do not match the first frame (%dx%d):\n",
		len(bad), len(files), w, h)
	const maxShown = 10
	for i, m := range bad {
		if i == maxShown {
			fmt.Fprintf(&b, "  ... and %d more\n", len(bad)-maxShown)
			break
		}
		fmt.Fprintf(&b, "  %s is %dx%d\n", filepath.Base(m.path), m.w, m.h)
	}
	b.WriteString("All frames must be the same size. " +
		"Pass --force-size to crop/pad them against the first frame instead.")
	return errors.New(b.String())
}

func processFrame(path string, w, h uint32) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	// Ensure we have RGBA and correct size
	var rgba *image.RGBA
	if r, ok := img.(*image.RGBA); ok && r.Rect.Dx() == int(w) && r.Rect.Dy() == int(h) {
		rgba = r
	} else {
		rgba = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	}

	// Raw pixel data is in rgba.Pix
	// Compress with LZ4
	compressed := make([]byte, lz4.CompressBlockBound(len(rgba.Pix)))
	n, err := lz4.CompressBlock(rgba.Pix, compressed, nil)
	if err != nil {
		return nil, err
	}

	return compressed[:n], nil
}
