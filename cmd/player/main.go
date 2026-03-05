package main

import (
	"log"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/funatsufumiya/ebiten_gvvideo"
)

type Game struct {
	player *gvvideo.Player
}

func (g *Game) Update() error {
	// Advance the video frame based on internal clock
	g.player.Update()
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// Draw the current frame directly to the screen
	if img := g.player.CurrentFrame(); img != nil {
		screen.DrawImage(img, nil)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 1920, 1080 // Set to your video resolution
}

func main() {
	// Load the GV file
	p, err := gvvideo.NewPlayerFromFile("video.gv")
	if err != nil {
		log.Fatal(err)
	}

	game := &Game{player: p}
	p.Play() // Start playback

	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
