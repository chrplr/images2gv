#! /bin/sh
mkdir -p frames
# Export frames at original quality
ffmpeg -i $1 -q:v 2 frames/frame_%04d.png
