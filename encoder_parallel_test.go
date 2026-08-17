package jpeg2000

import (
	"bytes"
	"image"
	"runtime"
	"sync"
	"testing"
)

// parallelTestImage builds a deterministic image large enough to force
// encodeTile down its parallel worker path (more than four code-blocks).
func parallelTestImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	s := uint32(2463534242)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8((x*3 + y*5) & 0xff)
			img.Pix[i+1] = uint8((x ^ y) & 0xff)
			img.Pix[i+2] = uint8(s & 0x3f)
			img.Pix[i+3] = 0xff
		}
	}
	return img
}

func parallelTestCases() []struct {
	name string
	opts *Options
} {
	return []struct {
		name string
		opts *Options
	}{
		{"lossless_1layer", func() *Options {
			o := DefaultOptions()
			o.Lossless = true
			return o
		}()},
		{"lossless_3layer", func() *Options {
			o := DefaultOptions()
			o.Lossless = true
			o.NumLayers = 3
			return o
		}()},
		{"lossy_5layer", func() *Options {
			o := DefaultOptions()
			o.NumLayers = 5
			return o
		}()},
		{"lossless_4layer_cb32", func() *Options {
			o := DefaultOptions()
			o.Lossless = true
			o.NumLayers = 4
			o.CodeBlockSize = image.Point{5, 5}
			return o
		}()},
	}
}

// TestEncodeParallelMatchesSequential pins the parallel code-block encoder to
// the sequential one. Both paths must produce byte-identical codestreams; if a
// worker reads state out of a *T1 that has already been handed back to the
// pool (and therefore may belong to another worker), the multi-layer
// truncation points go wrong and the bytes diverge. Under -race the same test
// also flags the use-after-Put directly.
func TestEncodeParallelMatchesSequential(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("needs more than one CPU to exercise the parallel path")
	}
	img := parallelTestImage(320, 256)

	for _, tc := range parallelTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Sequential reference: GOMAXPROCS(1) selects the
			// single-threaded branch of encodeTile.
			prev := runtime.GOMAXPROCS(1)
			var seq bytes.Buffer
			err := Encode(&seq, img, tc.opts)
			runtime.GOMAXPROCS(prev)
			if err != nil {
				t.Fatalf("sequential encode: %v", err)
			}

			for i := 0; i < 8; i++ {
				var par bytes.Buffer
				if err := Encode(&par, img, tc.opts); err != nil {
					t.Fatalf("parallel encode %d: %v", i, err)
				}
				if !bytes.Equal(seq.Bytes(), par.Bytes()) {
					t.Fatalf("parallel encode %d differs from sequential encode: %d vs %d bytes",
						i, par.Len(), seq.Len())
				}
			}
		})
	}
}

// TestEncodeConcurrentStable runs many encodes at once so that the pooled T1
// encoders are shared across goroutines under contention. Every result must
// still equal the single-goroutine reference.
func TestEncodeConcurrentStable(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("needs more than one CPU to exercise the parallel path")
	}
	img := parallelTestImage(192, 160)
	opts := DefaultOptions()
	opts.Lossless = true
	opts.NumLayers = 3

	var want bytes.Buffer
	if err := Encode(&want, img, opts); err != nil {
		t.Fatalf("reference encode: %v", err)
	}

	const goroutines = 8
	const iterations = 4
	var wg sync.WaitGroup
	errs := make(chan string, goroutines*iterations)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				var got bytes.Buffer
				if err := Encode(&got, img, opts); err != nil {
					errs <- err.Error()
					return
				}
				if !bytes.Equal(want.Bytes(), got.Bytes()) {
					errs <- "concurrent encode produced a different codestream"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}
