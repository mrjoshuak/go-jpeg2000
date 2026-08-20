// Command halfpgm moves half-float samples between this library and the 16-bit
// PGM raster opj_decompress writes, so a reduced-resolution half decode can be
// compared against the reference's own.
//
// It exists because the half entry point is the one an EXR HTJ2K chunk of half
// channels reaches, and it is a different function from the float one:
// DecodeHalfConfig refused a reduced decode outright until it was measured, so
// it had never been compared with anything. scripts/floatpfm covers binary32
// through PFM; nothing covered binary16.
//
//	halfpgm enc <out.j2c> <out.pgm> [numres]   encode a fixture, and write the
//	                                           samples it was built from
//	halfpgm dec <in.j2c> <out.pgm> [reduce]    decode, optionally reduced
//
// The PGM is 16-bit big-endian, and the samples are written in the convention
// opj_decompress uses for a signed component: the value plus 2^15, so the
// raster is unsigned. Without that shift every sample differs by exactly 32768
// and the comparison says nothing about the decode.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

const size = 64

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// halfBits converts a float32 to binary16 for the smooth positive values this
// fixture uses. It is not a general converter and does not need to be.
func halfBits(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int32((b>>23)&0xFF) - 127 + 15
	mant := uint16((b >> 13) & 0x3FF)
	if exp <= 0 || exp >= 31 {
		return sign
	}
	return sign | uint16(exp)<<10 | mant
}

// writePGM writes the samples as a 16-bit PGM, shifting into the unsigned
// domain the way opj_decompress does for a signed component.
func writePGM(path string, w, h int, samples []uint16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	defer bw.Flush()
	fmt.Fprintf(bw, "P5\n%d %d\n65535\n", w, h)
	for _, s := range samples {
		v := uint16(int32(int16(s)) + 32768)
		bw.WriteByte(byte(v >> 8))
		bw.WriteByte(byte(v))
	}
	return nil
}

func main() {
	if len(os.Args) < 4 {
		fail("usage: halfpgm enc <out.j2c> <out.pgm> [numres] | halfpgm dec <in.j2c> <out.pgm> [reduce]")
	}
	mode, csPath, pgmPath := os.Args[1], os.Args[2], os.Args[3]
	n := 0
	if len(os.Args) > 4 {
		v, err := strconv.Atoi(os.Args[4])
		if err != nil {
			fail("numeric argument: %v", err)
		}
		n = v
	}

	switch mode {
	case "enc":
		numres := 4
		if n > 0 {
			numres = n
		}
		samples := make([]uint16, size*size)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				// A smooth positive ramp with gentle structure. Half samples
				// are 16 bits wide, so nothing here approaches the magnitude
				// the wide path exists for; what is being measured is the
				// resolution, not the coefficient budget.
				v := 0.25 + 1.5*float32(x)/(size-1) + 0.75*float32(y)/(size-1) +
					0.1*float32(math.Sin(float64(x)*0.4))
				samples[y*size+x] = halfBits(v)
			}
		}
		f, err := os.Create(csPath)
		if err != nil {
			fail("%v", err)
		}
		defer f.Close()
		if err := jp2.EncodeHalf(f, &jp2.HalfImage{
			Width: size, Height: size, Components: [][]uint16{samples},
		}, &jp2.Options{Lossless: true, Format: jp2.FormatJ2K, NumResolutions: numres}); err != nil {
			fail("EncodeHalf: %v", err)
		}
		if err := writePGM(pgmPath, size, size, samples); err != nil {
			fail("%v", err)
		}
		fmt.Println("ok")

	case "dec":
		f, err := os.Open(csPath)
		if err != nil {
			fail("%v", err)
		}
		defer f.Close()
		var img *jp2.HalfImage
		if n > 0 {
			img, err = jp2.DecodeHalfConfig(f, &jp2.Config{ReduceResolution: n})
		} else {
			img, err = jp2.DecodeHalf(f)
		}
		if err != nil {
			fail("decode: %v", err)
		}
		if err := writePGM(pgmPath, img.Width, img.Height, img.Components[0]); err != nil {
			fail("%v", err)
		}
		fmt.Println("ok")

	default:
		fail("unknown mode %q", mode)
	}
}
