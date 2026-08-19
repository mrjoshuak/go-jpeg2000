// Command subsampgen writes a codestream whose components are subsampled, so
// an external decoder can be asked whether the components land where SIZ says
// they do.
//
//	subsampgen <out.j2k> <size> <dx> <dy> [tile] [lossy]
//
// The image is generated rather than read so the expected sample at every
// reference-grid position is a closed form the checker recomputes, which keeps
// the comparison independent of any file this library also wrote.
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// Sample is the value of component c at reference-grid position (x, y), before
// subsampling. scripts/validate.sh recomputes the same expression.
func Sample(c, x, y int) uint8 {
	switch c {
	case 0:
		return uint8((x*3 + y) % 256)
	case 1:
		return uint8((x + y*5) % 256)
	case 2:
		return uint8((x*x + y*y) / 32 % 256)
	}
	return 255
}

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: subsampgen <out.j2k> <size> <dx> <dy> [tile] [lossy]")
		os.Exit(2)
	}
	out := os.Args[1]
	size, _ := strconv.Atoi(os.Args[2])
	dx, _ := strconv.Atoi(os.Args[3])
	dy, _ := strconv.Atoi(os.Args[4])
	tile := 0
	if len(os.Args) > 5 {
		tile, _ = strconv.Atoi(os.Args[5])
	}
	lossy := len(os.Args) > 6 && os.Args[6] != "0"

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{
				R: Sample(0, x, y), G: Sample(1, x, y), B: Sample(2, x, y), A: 255,
			})
		}
	}

	opts := &jp2.Options{
		Lossless:       !lossy,
		Format:         jp2.FormatJ2K,
		NumResolutions: 3,
		ComponentSubsampling: []image.Point{
			{X: 1, Y: 1}, {X: dx, Y: dy}, {X: dx, Y: dy}, {X: 1, Y: 1},
		},
	}
	if tile > 0 {
		opts.TileSize = image.Point{X: tile, Y: tile}
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := jp2.Encode(f, img, opts); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
