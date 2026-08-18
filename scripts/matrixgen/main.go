// Command matrixgen writes one codestream per capability combination, so that
// an external decoder can be asked about each one.
//
// It exists because this library's validation used to exercise a single corner
// of the format — 8-bit greyscale, one tile, lossless 5/3 — and every defect
// outside that corner survived undetected. Widening the matrix immediately
// found non-conformant output for the lossy path, for tiled images and for
// float components, none of which any round-trip test could see.
//
// For each case it prints one tab-separated line:
//
//	name  kind  components  depth  codestream  reference-raster
//
// kind is the raster format the reference is written in, and is what the gate
// dispatches on: pgm for one integer component, ppm for three, and pfm for a
// binary32 component. That last distinction is the point of the column.
// ojph_expand cannot write a 32-bit component to PGM or PPM — it emits an
// all-zero raster with maxval 0, and does so for its own codestreams as much as
// for this library's — so a float case compared through PGM measures nothing at
// all. PFM is the only raster either oracle carries binary32 on.
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

const size = 32

// ramp is a per-component gradient with no flat regions, so a decoder that
// silently produces zeros or a constant is immediately distinguishable from one
// that works. A uniform fixture would compare equal against a broken decoder
// that happened to emit the same constant.
func ramp(x, y, c int) int { return 20 + ((x*13 + y*3 + c*29) % 200) }

// floatFixture returns the binary32 sample at index i of component c.
//
// Small positive integers are what hid the 32-bit overflow on the float path:
// they occupy a handful of magnitude bits, so no coefficient ever needed the
// 33rd and encoder and decoder agreed on a wrapped value no one else computes.
// The patterns here span the whole binary32 encoding instead — both zeros, both
// infinities, quiet NaNs, the smallest and largest denormals, the largest
// finite value, and a spread of exponents and signs — because those are the
// samples that drive the coefficient word to its extremes after the NLT Type 3
// point transform.
//
// The sequence is a fixed table followed by a deterministic LCG, so the fixture
// is reproducible without a testdata file.
func floatFixture(i, c int) float32 {
	special := []uint32{
		0x00000000, // +0
		0x80000000, // -0
		0x7f800000, // +Inf
		0xff800000, // -Inf
		0x7fc00000, // quiet NaN
		0xffc00000, // negative quiet NaN
		0x7fffffff, // NaN, all payload bits set
		0xffffffff, // NaN, every bit set: the extreme of the transformed word
		0x00000001, // smallest positive denormal
		0x80000001, // smallest negative denormal
		0x007fffff, // largest denormal
		0x807fffff,
		0x7f7fffff, // FLT_MAX
		0xff7fffff, // -FLT_MAX
		0x3f800000, // 1.0
		0xbf800000, // -1.0
	}
	if i < len(special) {
		return math.Float32frombits(special[i])
	}
	// A 32-bit LCG, seeded per component. The low bits of the state are the
	// mantissa and the high bits the exponent and sign, so the values cover
	// the encoding rather than a narrow numeric range.
	state := uint32(i)*1664525 + uint32(c)*1013904223 + 22695477
	state = state*1664525 + 1013904223
	state ^= state >> 15
	return math.Float32frombits(state)
}

func options(nres int, lossless bool, tile int) *jp2.Options {
	o := &jp2.Options{
		HighThroughput: true,
		Lossless:       lossless,
		Format:         jp2.FormatJ2K,
		NumResolutions: nres,
	}
	if tile > 0 {
		o.TileSize = image.Point{X: tile, Y: tile}
	}
	return o
}

func writeRef(path string, comps, depth int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	magic, maxv := "P5", 255
	if comps == 3 {
		magic = "P6"
	}
	if depth >= 16 {
		maxv = 65535
	}
	fmt.Fprintf(f, "%s\n%d %d\n%d\n", magic, size, size, maxv)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			for c := 0; c < comps; c++ {
				v := ramp(x, y, c)
				if depth >= 16 {
					f.Write([]byte{byte(v >> 8), byte(v)})
				} else {
					f.Write([]byte{byte(v)})
				}
			}
		}
	}
	return nil
}

// writeFloatRef writes the binary32 fixture as a little-endian PFM, whose rows
// run bottom to top.
func writeFloatRef(path string, comps int) error {
	magic := "Pf"
	if comps == 3 {
		magic = "PF"
	}
	buf := []byte(fmt.Sprintf("%s\n%d %d\n-1.0\n", magic, size, size))
	var word [4]byte
	for row := 0; row < size; row++ {
		y := size - 1 - row
		for x := 0; x < size; x++ {
			for c := 0; c < comps; c++ {
				binary.LittleEndian.PutUint32(word[:], math.Float32bits(floatFixture(y*size+x, c)))
				buf = append(buf, word[:]...)
			}
		}
	}
	return os.WriteFile(path, buf, 0o644)
}

// rgbColor is an alpha-free 8-bit colour. The encoder picks its component count
// from whether the image's colour model can represent transparency, so this is
// what makes a three-component integer image rather than a four-component one.
type rgbColor struct{ R, G, B uint8 }

func (c rgbColor) RGBA() (r, g, b, a uint32) {
	return uint32(c.R) * 0x101, uint32(c.G) * 0x101, uint32(c.B) * 0x101, 0xFFFF
}

var rgbModel = color.ModelFunc(func(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	return rgbColor{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
})

type rgbImage struct {
	pix  []rgbColor
	rect image.Rectangle
}

func (m *rgbImage) ColorModel() color.Model { return rgbModel }
func (m *rgbImage) Bounds() image.Rectangle { return m.rect }
func (m *rgbImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(m.rect) {
		return rgbColor{}
	}
	return m.pix[(y-m.rect.Min.Y)*m.rect.Dx()+(x-m.rect.Min.X)]
}

type result struct {
	name   string
	kind   string
	comps  int
	depth  int
	stream string
	ref    string
	err    error
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: matrixgen <outdir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	gray := func() *image.Gray {
		img := image.NewGray(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Set(x, y, color.Gray{Y: uint8(ramp(x, y, 0))})
			}
		}
		return img
	}

	floatImage := func(comps int) *jp2.FloatImage {
		img := &jp2.FloatImage{Width: size, Height: size, BitDepth: 32, Signed: true,
			Components: make([][]float32, comps)}
		for c := 0; c < comps; c++ {
			img.Components[c] = make([]float32, size*size)
			for i := range img.Components[c] {
				img.Components[c][i] = floatFixture(i, c)
			}
		}
		return img
	}

	var results []result
	run := func(name, kind string, comps, depth int, fn func(f *os.File) error) {
		p := filepath.Join(dir, name+".j2c")
		f, err := os.Create(p)
		if err != nil {
			results = append(results, result{name: name, kind: kind, err: err})
			return
		}
		err = fn(f)
		f.Close()
		ref := filepath.Join(dir, name+".ref."+kind)
		if err == nil {
			if kind == "pfm" {
				err = writeFloatRef(ref, comps)
			} else {
				err = writeRef(ref, comps, depth)
			}
		}
		results = append(results, result{name, kind, comps, depth, p, ref, err})
	}

	// Integer greyscale across resolution counts: the wavelet is only exercised
	// above one resolution, and each further level compounds any error.
	for _, n := range []int{1, 3, 6} {
		run(fmt.Sprintf("gray8_res%d", n), "pgm", 1, 8, func(f *os.File) error {
			return jp2.Encode(f, gray(), options(n, true, 0))
		})
	}

	run("gray16", "pgm", 1, 16, func(f *os.File) error {
		img := image.NewGray16(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Set(x, y, color.Gray16{Y: uint16(ramp(x, y, 0))})
			}
		}
		return jp2.Encode(f, img, options(3, true, 0))
	})

	// Irreversible 9/7.
	run("gray8_lossy", "pgm", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, false, 0))
	})

	// Tiled, both a size that divides the image and one that does not.
	run("gray8_tile16", "pgm", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, true, 16))
	})
	run("gray8_tile12", "pgm", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, true, 12))
	})

	// Three integer components, exercising the multiple component transform.
	// This used to be written through EncodeFloat, which signals binary32
	// components whatever the samples hold, so it tested the float path and
	// called it RGB while nothing tested the integer RGB path at all.
	run("rgb8", "ppm", 3, 8, func(f *os.File) error {
		img := &rgbImage{rect: image.Rect(0, 0, size, size), pix: make([]rgbColor, size*size)}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.pix[y*size+x] = rgbColor{
					uint8(ramp(x, y, 0)), uint8(ramp(x, y, 1)), uint8(ramp(x, y, 2)),
				}
			}
		}
		return jp2.Encode(f, img, options(3, true, 0))
	})

	// The half entry point. go-openexr's HTJ2K compressor calls it, so a defect
	// here reaches every EXR written with HTJ2K. Its samples are 16 bits wide,
	// which a PGM does carry.
	run("half1", "pgm", 1, 16, func(f *os.File) error {
		img := &jp2.HalfImage{Width: size, Height: size,
			Components: [][]uint16{make([]uint16, size*size)}}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Components[0][y*size+x] = uint16(ramp(x, y, 0))
			}
		}
		return jp2.EncodeHalf(f, img, options(3, true, 0))
	})

	// The float entry point, over genuine binary32 content. Every resolution
	// count matters here for a reason the integer path does not have: the
	// magnitude budget a subband needs grows with the decomposition level, and
	// it is that budget the codestream has to signal correctly.
	for _, n := range []int{1, 3, 6} {
		run(fmt.Sprintf("float1_res%d", n), "pfm", 1, 32, func(f *os.File) error {
			return jp2.EncodeFloat(f, floatImage(1), options(n, true, 0))
		})
	}
	run("float1_tile12", "pfm", 1, 32, func(f *os.File) error {
		return jp2.EncodeFloat(f, floatImage(1), options(3, true, 12))
	})
	// Three float components: the RCT widens the chrominance differences by a
	// further bit, which is the case that needs 35 magnitude bit-planes.
	run("float3", "pfm", 3, 32, func(f *os.File) error {
		return jp2.EncodeFloat(f, floatImage(3), options(3, true, 0))
	})

	for _, r := range results {
		if r.err != nil {
			fmt.Printf("%s\t%s\tENCODE_FAIL\t0\t\t%v\n", r.name, r.kind, r.err)
			continue
		}
		fmt.Printf("%s\t%s\t%d\t%d\t%s\t%s\n", r.name, r.kind, r.comps, r.depth, r.stream, r.ref)
	}
}
