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

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// readPGM returns the raster of a binary PGM, skipping the four header tokens.
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

	want, err := readPGM(os.Args[2])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	b := img.Bounds()
	if b.Dx()*b.Dy() != len(want) {
		fmt.Printf("size mismatch: decoded %dx%d, reference %d samples\n",
			b.Dx(), b.Dy(), len(want))
		os.Exit(1)
	}

	diff := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if byte(r>>8) != want[y*b.Dx()+x] {
				diff++
			}
		}
	}
	fmt.Println(diff)
}
