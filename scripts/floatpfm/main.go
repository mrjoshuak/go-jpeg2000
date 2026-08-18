// Command floatpfm moves binary32 samples between this library and the PFM
// raster format, which is the only raster either external oracle carries
// floating point in.
//
// It exists because ojph_expand cannot write a 32-bit component to PGM or PPM:
// it emits an all-zero raster with maxval 0, and it does that for its own
// output as much as for ours. A gate that compares float cases through PGM
// therefore measures nothing at all. PFM is the format ojph_compress reads and
// ojph_expand writes for such components, so it is the only channel on which a
// float comparison means anything.
//
// Modes:
//
//	floatpfm enc <src.pfm> <out.j2c> [numres [tile]]  encode with EncodeFloat
//	floatpfm dec <in.j2c> <out.pfm>             decode with DecodeFloat
//	floatpfm cmp <a.pfm> <b.pfm>                compare bit patterns
//
// cmp compares the raw sample bits, not the numeric values: -0.0 and +0.0 are
// different samples here, and two NaNs with different payloads are different
// samples, because a lossless codec has to return exactly what it was given.
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// pfm holds a decoded PFM raster in top-to-bottom, planar order.
type pfm struct {
	width, height int
	comps         [][]float32
}

// readPFM parses a binary PFM file. "Pf" is one component, "PF" is three
// interleaved. A negative scale means little-endian samples. Rows are stored
// bottom-to-top on disk and are flipped here.
func readPFM(path string) (*pfm, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tok := make([]string, 0, 4)
	i := 0
	for len(tok) < 4 {
		for i < len(d) && (d[i] == ' ' || d[i] == '\n' || d[i] == '\t' || d[i] == '\r') {
			i++
		}
		if i >= len(d) {
			return nil, fmt.Errorf("%s: truncated PFM header", path)
		}
		s := i
		for i < len(d) && d[i] != ' ' && d[i] != '\n' && d[i] != '\t' && d[i] != '\r' {
			i++
		}
		tok = append(tok, string(d[s:i]))
	}
	i++ // single whitespace byte after the scale

	nc := 1
	switch tok[0] {
	case "Pf":
	case "PF":
		nc = 3
	default:
		return nil, fmt.Errorf("%s: not a PFM file (%q)", path, tok[0])
	}
	w, err := strconv.Atoi(tok[1])
	if err != nil {
		return nil, fmt.Errorf("%s: bad width %q", path, tok[1])
	}
	h, err := strconv.Atoi(tok[2])
	if err != nil {
		return nil, fmt.Errorf("%s: bad height %q", path, tok[2])
	}
	scale, err := strconv.ParseFloat(tok[3], 64)
	if err != nil {
		return nil, fmt.Errorf("%s: bad scale %q", path, tok[3])
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%s: bad size %dx%d", path, w, h)
	}
	need := w * h * nc * 4
	if len(d)-i < need {
		return nil, fmt.Errorf("%s: raster is %d bytes, want %d", path, len(d)-i, need)
	}
	raster := d[i : i+need]

	get := func(off int) uint32 {
		if scale < 0 {
			return binary.LittleEndian.Uint32(raster[off:])
		}
		return binary.BigEndian.Uint32(raster[off:])
	}

	p := &pfm{width: w, height: h, comps: make([][]float32, nc)}
	for c := range p.comps {
		p.comps[c] = make([]float32, w*h)
	}
	for row := 0; row < h; row++ {
		y := h - 1 - row // PFM rows run bottom-to-top
		for x := 0; x < w; x++ {
			for c := 0; c < nc; c++ {
				off := ((row*w+x)*nc + c) * 4
				p.comps[c][y*w+x] = math.Float32frombits(get(off))
			}
		}
	}
	return p, nil
}

// writePFM writes a little-endian binary PFM, bottom-to-top as the format
// requires.
func writePFM(path string, p *pfm) error {
	nc := len(p.comps)
	magic := "Pf"
	if nc == 3 {
		magic = "PF"
	} else if nc != 1 {
		return fmt.Errorf("PFM carries one or three components, not %d", nc)
	}
	buf := make([]byte, 0, 32+p.width*p.height*nc*4)
	buf = append(buf, fmt.Sprintf("%s\n%d %d\n-1.0\n", magic, p.width, p.height)...)
	var word [4]byte
	for row := 0; row < p.height; row++ {
		y := p.height - 1 - row
		for x := 0; x < p.width; x++ {
			for c := 0; c < nc; c++ {
				binary.LittleEndian.PutUint32(word[:], math.Float32bits(p.comps[c][y*p.width+x]))
				buf = append(buf, word[:]...)
			}
		}
	}
	return os.WriteFile(path, buf, 0o644)
}

func fail(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: floatpfm enc|dec|cmp <in> <out> [numres [tile]]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enc":
		src, err := readPFM(os.Args[2])
		if err != nil {
			fail("%v", err)
		}
		nres := 3
		if len(os.Args) > 4 {
			nres, _ = strconv.Atoi(os.Args[4])
		}
		tile := 0
		if len(os.Args) > 5 {
			tile, _ = strconv.Atoi(os.Args[5])
		}
		f, err := os.Create(os.Args[3])
		if err != nil {
			fail("%v", err)
		}
		img := &jp2.FloatImage{
			Width: src.width, Height: src.height,
			Components: src.comps, BitDepth: 32, Signed: true,
		}
		opts := &jp2.Options{
			HighThroughput: true, Lossless: true,
			Format: jp2.FormatJ2K, NumResolutions: nres,
		}
		if tile > 0 {
			opts.TileSize = image.Point{X: tile, Y: tile}
		}
		err = jp2.EncodeFloat(f, img, opts)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			fail("encode: %v", err)
		}
		fmt.Println("ok")

	case "dec":
		f, err := os.Open(os.Args[2])
		if err != nil {
			fail("%v", err)
		}
		defer f.Close()
		img, err := jp2.DecodeFloat(f)
		if err != nil {
			fail("decode: %v", err)
		}
		if err := writePFM(os.Args[3], &pfm{
			width: img.Width, height: img.Height, comps: img.Components,
		}); err != nil {
			fail("%v", err)
		}
		fmt.Println("ok")

	case "cmp":
		a, err := readPFM(os.Args[2])
		if err != nil {
			fail("%v", err)
		}
		b, err := readPFM(os.Args[3])
		if err != nil {
			fail("%v", err)
		}
		if a.width != b.width || a.height != b.height || len(a.comps) != len(b.comps) {
			fail("geometry %dx%dx%d vs %dx%dx%d",
				a.width, a.height, len(a.comps), b.width, b.height, len(b.comps))
		}
		diff, first := 0, ""
		for c := range a.comps {
			for i := range a.comps[c] {
				x, y := math.Float32bits(a.comps[c][i]), math.Float32bits(b.comps[c][i])
				if x == y {
					continue
				}
				diff++
				if first == "" {
					first = fmt.Sprintf(" first at comp %d sample %d: %08x vs %08x", c, i, x, y)
				}
			}
		}
		n := a.width * a.height * len(a.comps)
		if diff != 0 {
			fail("%d/%d samples differ;%s", diff, n, first)
		}
		fmt.Println("0")

	default:
		fmt.Fprintln(os.Stderr, "usage: floatpfm enc|dec|cmp <in> <out> [numres [tile]]")
		os.Exit(2)
	}
}
