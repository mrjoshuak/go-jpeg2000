// Command jp2gen writes JP2 container files so another implementation can be
// asked whether the boxes are right.
//
//	jp2gen <outdir>
//
// It prints one tab-separated row per fixture: name, components, bit depth,
// path. The gate hands each to a reference decoder and compares the samples
// against the same fixture the codestream matrix uses.
//
// This exists because the JP2 boxes this library writes had never been read by
// anything else. The raw codestream had — thoroughly, across the whole
// capability matrix — but a codestream is the payload of a JP2, not the
// container, and every box around it was checked only by the parser in this
// same repository. A wrapper both halves of one library agree on is exactly the
// shape of defect an external oracle exists to catch.
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

const size = 32

// ramp is the same per-component gradient the codestream matrix uses: no flat
// regions, so a decoder that emits zeros or a constant is distinguishable from
// one that works.
func ramp(x, y, c int) int { return 20 + ((x*13 + y*3 + c*29) % 200) }

type rgbColor struct{ R, G, B uint8 }

func (c rgbColor) RGBA() (r, g, b, a uint32) {
	return uint32(c.R) * 0x101, uint32(c.G) * 0x101, uint32(c.B) * 0x101, 0xFFFF
}

var rgbModel = color.ModelFunc(func(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	return rgbColor{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
})

// rgbImage is a three-component image with no alpha, so the encoder writes a
// three-component JP2 rather than a four-component one.
type rgbImage struct {
	w, h int
	pix  []rgbColor
}

func (im *rgbImage) ColorModel() color.Model { return rgbModel }
func (im *rgbImage) Bounds() image.Rectangle { return image.Rect(0, 0, im.w, im.h) }
func (im *rgbImage) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= im.w || y >= im.h {
		return rgbColor{}
	}
	return im.pix[y*im.w+x]
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: jp2gen <outdir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	gray := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			gray.Set(x, y, color.Gray{Y: uint8(ramp(x, y, 0))})
		}
	}
	gray16 := image.NewGray16(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			gray16.Set(x, y, color.Gray16{Y: uint16(ramp(x, y, 0)) * 257})
		}
	}
	rgb := &rgbImage{w: size, h: size, pix: make([]rgbColor, size*size)}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			rgb.pix[y*size+x] = rgbColor{
				uint8(ramp(x, y, 0)), uint8(ramp(x, y, 1)), uint8(ramp(x, y, 2)),
			}
		}
	}

	opts := func(nres int) *jp2.Options {
		return &jp2.Options{
			HighThroughput: true,
			Lossless:       true,
			Format:         jp2.FormatJP2,
			NumResolutions: nres,
		}
	}

	type fixture struct {
		name  string
		comps int
		depth int
		img   image.Image
		nres  int
	}
	for _, f := range []fixture{
		// One and three components exercise the colour-specification box's two
		// enumerated colourspaces, greyscale and sRGB, which is where a
		// wrapper that writes the wrong enumeration shows up.
		{"jp2_gray8", 1, 8, gray, 3},
		{"jp2_gray16", 1, 16, gray16, 3},
		{"jp2_rgb8", 3, 8, rgb, 3},
		// One resolution level: the codestream is trivial, so anything the
		// reference objects to here is the container rather than the payload.
		{"jp2_rgb8_res1", 3, 8, rgb, 1},
	} {
		p := filepath.Join(dir, f.name+".jp2")
		fh, err := os.Create(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		err = jp2.Encode(fh, f.img, opts(f.nres))
		fh.Close()
		if err != nil {
			fmt.Printf("%s\tENCODE_FAIL\t%d\t%v\n", f.name, f.depth, err)
			continue
		}
		fmt.Printf("%s\t%d\t%d\t%s\n", f.name, f.comps, f.depth, p)
	}
}
