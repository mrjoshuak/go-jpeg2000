// Command sopgen writes codestreams with each combination of the SOP and EPH
// error resilience markers, so a reference decoder can be asked whether the
// markers the coding style declares are actually there.
//
//	sopgen <outdir>
//
// It prints one tab-separated row per fixture: name, SOP marker count, EPH
// marker count, path.
//
// The marker counts are in the output because the failure this guards against
// is a codestream that declares markers and omits them. Until v1.5.9 that is
// exactly what this library wrote: Options.EnableSOP and EnableEPH set their
// bits in the COD's Scod field and no marker was ever emitted, so OpenJPEG
// refused an EnableEPH file outright while this library round-tripped it
// perfectly — our decoder skips the marker only when present and never minded
// that it never was.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

const size = 32

// ramp is the same gradient the rest of the gate's fixtures use, so the
// comparison script can recompute the expected samples.
func ramp(x, y, c int) int { return 20 + ((x*13 + y*3 + c*29) % 200) }

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sopgen <outdir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.Gray{Y: uint8(ramp(x, y, 0))})
		}
	}

	for _, c := range []struct {
		name     string
		sop, eph bool
	}{
		{"sop_neither", false, false},
		{"sop_only", true, false},
		{"eph_only", false, true},
		{"sop_and_eph", true, true},
	} {
		var buf bytes.Buffer
		err := jp2.Encode(&buf, img, &jp2.Options{
			HighThroughput: true,
			Lossless:       true,
			Format:         jp2.FormatJ2K,
			NumResolutions: 3,
			EnableSOP:      c.sop,
			EnableEPH:      c.eph,
			// Small precincts so the tile holds several packets; with one
			// packet the markers appear once and nothing about their placement
			// is exercised.
			PrecinctSizes: []jp2.PrecinctSize{
				{WidthExp: 6, HeightExp: 6},
				{WidthExp: 6, HeightExp: 6},
				{WidthExp: 6, HeightExp: 6},
			},
		})
		if err != nil {
			fmt.Printf("%s\tENCODE_FAIL\t0\t%v\n", c.name, err)
			continue
		}
		b := buf.Bytes()
		p := filepath.Join(dir, c.name+".j2k")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s\t%d\t%d\t%s\n",
			c.name,
			bytes.Count(b, []byte{0xFF, 0x91}),
			bytes.Count(b, []byte{0xFF, 0x92}),
			p)
	}
}
