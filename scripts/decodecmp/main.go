// Command decodecmp decodes a JPEG 2000 codestream with this library and
// reports how many samples differ from a reference PGM, printing just the
// count so a shell gate can test it.
//
// This exists so validation can be driven by a codestream another
// implementation wrote. A round trip through this library cannot detect a
// convention our encoder and decoder both get wrong; a foreign codestream can.
package main

import (
	"fmt"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// readPNM returns the raster of a binary PGM or PPM, how many samples each
// pixel carries, and whether each sample is two bytes. A PPM holds three
// interleaved samples per pixel, so comparing only the first channel of one
// would pass a decoder that got the other two entirely wrong. A maxval above
// 255 means 16-bit big-endian samples, and those are compared at full width:
// comparing their top byte only would pass a decoder whose 16-bit samples are
// each off by one.
func readPNM(path string) ([]byte, int, bool, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	n := 1
	if len(d) >= 2 && d[1] == '6' {
		n = 3
	}
	raster, maxval, err := readPGM(path)
	return raster, n, maxval > 255, err
}

// readPGM returns the raster of a binary PGM and its maxval, skipping the four
// header tokens.
func readPGM(path string) ([]byte, int, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	i, tok := 0, 0
	var maxval int
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
		start := i
		for i < len(d) && d[i] != ' ' && d[i] != '\n' && d[i] != '\t' && d[i] != '\r' {
			i++
		}
		if tok == 3 {
			maxval, _ = strconv.Atoi(string(d[start:i]))
		}
		tok++
	}
	if i >= len(d) {
		return nil, 0, fmt.Errorf("bad PGM header in %s", path)
	}
	return d[i+1:], maxval, nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: decodecmp <codestream> <reference.pgm>")
		os.Exit(2)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()

	img, err := jp2.Decode(f)
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}

	want, chans, wide, err := readPNM(os.Args[2])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bytesPer := 1
	if wide {
		bytesPer = 2
	}

	b := img.Bounds()
	if b.Dx()*b.Dy()*chans*bytesPer != len(want) {
		fmt.Printf("size mismatch: decoded %dx%d, reference %d samples over %d channels\n",
			b.Dx(), b.Dy(), len(want), chans)
		os.Exit(1)
	}

	diff := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*b.Dx() + x) * chans
			if wide {
				// RGBA returns the full 16 bits of a Gray16 or RGBA64 sample.
				px := [3]uint32{r, g, bl}
				for k, v := range px[:chans] {
					j := 2 * (i + k)
					if uint16(v) != uint16(want[j])<<8|uint16(want[j+1]) {
						diff++
					}
				}
				continue
			}
			if chans == 1 {
				if byte(r>>8) != want[i] {
					diff++
				}
				continue
			}
			for k, v := range [3]uint32{r, g, bl} {
				if byte(v>>8) != want[i+k] {
					diff++
				}
			}
		}
	}
	fmt.Println(diff)
}
