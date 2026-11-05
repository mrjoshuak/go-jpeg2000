// Command int16gen writes signed 16-bit codestreams with EncodeInt16, each
// beside the source samples as little-endian int16, so an external decoder can
// be asked whether it reads back exactly what was coded.
//
// The samples are signed and stay signed: SIZ declares Ssiz 0x8F and there is
// no DC level shift. A round trip through this library cannot tell a correct
// signed codestream from one that agrees with this decoder about some other
// convention, so the gate decodes these with OpenJPH and OpenJPEG.
//
// For each case it prints one tab-separated line:
//
//	name  coder  width  height  codestream  reference-raw
//
// coder is ht or part1. OpenJPH decodes only the first; OpenJPEG both.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// ctSlice is a synthetic CT slice in Hounsfield units: air at -1000, a body
// ellipse of soft tissue, a cortical bone ring up to +2000 and scattered dense
// spots, with noise throughout. Smooth regions with structure are what real CT
// is made of; uniform noise alone would leave most of the coder idle.
func ctSlice(w, h int, seed uint32) []int16 {
	s := seed*2654435761 | 1
	rnd := func() uint32 { s ^= s << 13; s ^= s >> 17; s ^= s << 5; return s }
	noise := func(a int) int { return int(rnd()%uint32(2*a+1)) - a }
	out := make([]int16, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := (float64(x) - float64(w)/2) / (0.42 * float64(w))
			dy := (float64(y) - float64(h)/2) / (0.36 * float64(h))
			r := math.Hypot(dx, dy)
			v := -100 + int(180*(0.5+0.5*math.Sin(float64(x)/23)*math.Cos(float64(y)/31))) + noise(15)
			switch {
			case r > 1:
				v = -1000 + noise(20)
			case r > 0.93:
				v = 1200 + int(800*(1-math.Abs(r-0.965)/0.035)) + noise(40)
			}
			out[y*w+x] = int16(min(v, 2000))
		}
	}
	for i := 0; i < w*h/2000; i++ {
		out[int(rnd()%uint32(h))*w+int(rnd()%uint32(w))] = int16(1500 + rnd()%1500)
	}
	return out
}

// extremes alternates the two ends of the int16 range with values around zero,
// which no CT slice reaches and every signed decoder has to.
func extremes(w, h int) []int16 {
	probes := []int16{math.MinInt16, math.MaxInt16, -1, 0, 1, math.MinInt16 + 1, math.MaxInt16 - 1}
	out := make([]int16, w*h)
	for i := range out {
		out[i] = probes[(i+i/w)%len(probes)]
	}
	return out
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: int16gen <outdir>")
		os.Exit(2)
	}
	dir := os.Args[1]

	type tcase struct {
		name    string
		w, h    int
		samples []int16
		opts    func(*jp2.Options)
	}
	cases := []tcase{
		// The DICOM HTJ2K Lossless RPCL parameters (PS3.5 A.4.4, 10.18.1),
		// at the size of a real CT frame: RPCL, a base resolution at most 64
		// samples across, 64x64 code-blocks, one tile, and TLM.
		{"ct_dicom_rpcl", 736, 736, ctSlice(736, 736, 1), func(o *jp2.Options) {
			o.HighThroughput = true
			o.ProgressionOrder = jp2.RPCL
			o.NumResolutions = 5
			o.HTBlockWidth, o.HTBlockHeight = 64, 64
			o.WritePacketLengths = true
		}},
		{"ct_ht_tiled", 96, 80, ctSlice(96, 80, 2), func(o *jp2.Options) {
			o.HighThroughput = true
			o.NumResolutions = 3
			o.TileSize = image.Point{40, 40}
		}},
		{"ct_part1", 96, 80, ctSlice(96, 80, 3), func(o *jp2.Options) {
			o.NumResolutions = 4
		}},
		{"extremes_ht", 64, 48, extremes(64, 48), func(o *jp2.Options) {
			o.HighThroughput = true
			o.NumResolutions = 4
		}},
		{"extremes_part1", 64, 48, extremes(64, 48), func(o *jp2.Options) {
			o.NumResolutions = 4
		}},
	}

	for _, c := range cases {
		opts := jp2.DefaultOptions()
		opts.Format = jp2.FormatJ2K
		c.opts(opts)
		var cs bytes.Buffer
		img := &jp2.Int16Image{Width: c.w, Height: c.h, Components: [][]int16{c.samples}}
		if err := jp2.EncodeInt16(&cs, img, opts); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", c.name, err)
			os.Exit(1)
		}
		raw := make([]byte, 2*len(c.samples))
		for i, v := range c.samples {
			binary.LittleEndian.PutUint16(raw[2*i:], uint16(v))
		}
		stream := filepath.Join(dir, c.name+".j2c")
		ref := filepath.Join(dir, c.name+".raw")
		if err := os.WriteFile(stream, cs.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(ref, raw, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		coder := "part1"
		if opts.HighThroughput {
			coder = "ht"
		}
		fmt.Printf("%s\t%s\t%d\t%d\t%s\t%s\n", c.name, coder, c.w, c.h, stream, ref)
	}
}
