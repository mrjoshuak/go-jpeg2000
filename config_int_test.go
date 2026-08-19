package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
)

// TestReduceResolutionIntegerPath checks Config.ReduceResolution on ordinary
// integer samples, where a reduced resolution has an obvious meaning: the
// low-pass band of a ramp is a smaller ramp.
//
// The float path is separated out because it encodes through the NLT point
// transform, so its wavelet coefficients are of reinterpreted bit patterns
// rather than of sample values. Whether a partial synthesis of those means
// anything is a different question from whether the machinery works.
func TestReduceResolutionIntegerPath(t *testing.T) {
	const w, h, nres = 64, 32, 4

	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*2 + y)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres,
	}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cs := buf.Bytes()

	full, err := DecodeConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full decode: %v", err)
	}
	at := func(im image.Image, x, y int) float64 {
		r, _, _, _ := im.At(x, y).RGBA()
		return float64(byte(r >> 8))
	}

	for _, reduce := range []int{1, 2} {
		got, err := DecodeConfig(bytes.NewReader(cs), &Config{ReduceResolution: reduce})
		if err != nil {
			t.Errorf("ReduceResolution=%d: %v", reduce, err)
			continue
		}
		b := got.Bounds()
		wantW, wantH := (w+(1<<reduce)-1)>>reduce, (h+(1<<reduce)-1)>>reduce
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("ReduceResolution=%d produced %dx%d, want %dx%d",
				reduce, b.Dx(), b.Dy(), wantW, wantH)
			continue
		}

		step := 1 << reduce
		worst := 0.0
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				fy, fx := min(y*step, h-1), min(x*step, w-1)
				d := math.Abs(at(got, b.Min.X+x, b.Min.Y+y) - at(full, fx, fy))
				if d > worst {
					worst = d
				}
			}
		}
		// Samples span 0..190. The low-pass band of a linear ramp is the same
		// ramp, so a correct partial synthesis lands within a few counts; a
		// wrong level or an unscaled band is off by tens.
		const tol = 8.0
		if worst > tol {
			t.Errorf("ReduceResolution=%d: worst sample differs from the full decode by %.1f counts, tolerance %.0f",
				reduce, worst, tol)
		} else {
			t.Logf("ReduceResolution=%d: %dx%d, worst deviation %.1f counts", reduce, b.Dx(), b.Dy(), worst)
		}
	}
}
