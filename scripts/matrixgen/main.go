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
//	name  components  depth  codestream  reference-raster
//
// The reference raster is a binary PGM (one component) or PPM (three), so
// ojph_expand and opj_decompress can be pointed straight at it for comparison.
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

// ramp is a per-component gradient with no flat regions, so a decoder that
// silently produces zeros or a constant is immediately distinguishable from one
// that works. A uniform fixture would compare equal against a broken decoder
// that happened to emit the same constant.
func ramp(x, y, c int) int { return 20 + ((x*13 + y*3 + c*29) % 200) }

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

type result struct {
	name   string
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

	var results []result
	run := func(name string, comps, depth int, fn func(f *os.File) error) {
		p := filepath.Join(dir, name+".j2c")
		f, err := os.Create(p)
		if err != nil {
			results = append(results, result{name: name, err: err})
			return
		}
		err = fn(f)
		f.Close()
		ref := filepath.Join(dir, name+".ref")
		if err == nil {
			err = writeRef(ref, comps, depth)
		}
		results = append(results, result{name, comps, depth, p, ref, err})
	}

	// Integer greyscale across resolution counts: the wavelet is only exercised
	// above one resolution, and each further level compounds any error.
	for _, n := range []int{1, 3, 6} {
		run(fmt.Sprintf("gray8_res%d", n), 1, 8, func(f *os.File) error {
			return jp2.Encode(f, gray(), options(n, true, 0))
		})
	}

	run("gray16", 1, 16, func(f *os.File) error {
		img := image.NewGray16(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Set(x, y, color.Gray16{Y: uint16(ramp(x, y, 0))})
			}
		}
		return jp2.Encode(f, img, options(3, true, 0))
	})

	// Irreversible 9/7.
	run("gray8_lossy", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, false, 0))
	})

	// Tiled, both a size that divides the image and one that does not.
	run("gray8_tile16", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, true, 16))
	})
	run("gray8_tile12", 1, 8, func(f *os.File) error {
		return jp2.Encode(f, gray(), options(3, true, 12))
	})

	// Three components, exercising the multiple component transform.
	run("rgb8", 3, 8, func(f *os.File) error {
		img := &jp2.FloatImage{Width: size, Height: size, BitDepth: 8,
			Components: [][]float32{
				make([]float32, size*size),
				make([]float32, size*size),
				make([]float32, size*size),
			}}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				for c := 0; c < 3; c++ {
					img.Components[c][y*size+x] = float32(ramp(x, y, c))
				}
			}
		}
		return jp2.EncodeFloat(f, img, options(3, true, 0))
	})

	// The half and float entry points. go-openexr's HTJ2K compressor calls
	// both, so a defect here reaches every EXR written with HTJ2K.
	run("half1", 1, 16, func(f *os.File) error {
		img := &jp2.HalfImage{Width: size, Height: size,
			Components: [][]uint16{make([]uint16, size*size)}}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Components[0][y*size+x] = uint16(ramp(x, y, 0))
			}
		}
		return jp2.EncodeHalf(f, img, options(3, true, 0))
	})
	run("float1", 1, 16, func(f *os.File) error {
		img := &jp2.FloatImage{Width: size, Height: size, BitDepth: 16,
			Components: [][]float32{make([]float32, size*size)}}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Components[0][y*size+x] = float32(ramp(x, y, 0))
			}
		}
		return jp2.EncodeFloat(f, img, options(3, true, 0))
	})

	for _, r := range results {
		if r.err != nil {
			fmt.Printf("%s\tENCODE_FAIL\t0\t\t%v\n", r.name, r.err)
			continue
		}
		fmt.Printf("%s\t%d\t%d\t%s\t%s\n", r.name, r.comps, r.depth, r.stream, r.ref)
	}
}
