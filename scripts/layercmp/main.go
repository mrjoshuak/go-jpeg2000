// Command layercmp decodes a codestream at a given number of quality layers
// and reports the mean squared error against a reference raster.
//
// It exists for the read direction of quality layers. Decoding every layer and
// getting the exact image says the split loses nothing; it does not say the
// split means anything, because a codestream with one layer holding everything
// would pass that too. What has to hold is that each further layer of someone
// else's codestream improves what this library reconstructs, and that a partial
// decode yields a complete image at lower quality rather than a partial one.
//
//	layercmp <codestream> <reference.pgm> <layers>
//
// Prints the MSE and the decoded dimensions. A decode that returns fewer
// samples than the reference is an error, not a low-quality result.
package main

import (
	"fmt"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func readPGM(path string) ([]byte, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	i, tok := 0, 0
	for tok < 4 && i < len(d) {
		for i < len(d) && (d[i] == ' ' || d[i] == '\n' || d[i] == '\t' || d[i] == '\r') {
			i++
		}
		if i < len(d) && d[i] == '#' {
			for i < len(d) && d[i] != '\n' {
				i++
			}
			continue
		}
		for i < len(d) && d[i] != ' ' && d[i] != '\n' && d[i] != '\t' && d[i] != '\r' {
			i++
		}
		tok++
	}
	if i >= len(d) {
		return nil, fmt.Errorf("bad PGM header in %s", path)
	}
	return d[i+1:], nil
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: layercmp <codestream> <reference.pgm> <layers>")
		os.Exit(2)
	}
	layers, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Println("bad layer count:", os.Args[3])
		os.Exit(2)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()

	img, err := jp2.DecodeConfig(f, &jp2.Config{QualityLayers: layers})
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}

	want, err := readPGM(os.Args[2])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	b := img.Bounds()
	if b.Dx()*b.Dy() != len(want) {
		// A truncated or partly-decoded image is a failure here: the whole
		// point of a quality layer is that fewer layers cost quality, not area.
		fmt.Printf("decoded %dx%d (%d samples), reference has %d\n",
			b.Dx(), b.Dy(), b.Dx()*b.Dy(), len(want))
		os.Exit(1)
	}

	var sum float64
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			d := float64(int(byte(r>>8)) - int(want[y*b.Dx()+x]))
			sum += d * d
		}
	}
	fmt.Printf("%.3f %dx%d\n", sum/float64(len(want)), b.Dx(), b.Dy())
}
