package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

// rampFloatImage is a smooth ramp: a reduced-resolution decode of noise has no
// relationship to the full one, so a comparison against it would say nothing.
func rampFloatImage(w, h int) *FloatImage {
	img := &FloatImage{
		Width: w, Height: h, BitDepth: 32,
		Components: [][]float32{make([]float32, w*h)},
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Components[0][y*w+x] = float32(x)/float32(w) + float32(y)/float32(h)
		}
	}
	return img
}

func encodeRamp(t *testing.T, w, h, nres int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, rampFloatImage(w, h), &Options{
		HighThroughput: true, Lossless: true, Format: FormatJ2K, NumResolutions: nres,
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	return buf.Bytes()
}

// TestReduceResolutionRefusedForFloat pins the refusal rather than the wrong
// answer.
//
// ReduceResolution stops the inverse wavelet at an LL subband, leaving values
// in the sign-magnitude domain the NLT point transform maps back from. Those
// are wavelet coefficients, not samples, and reinterpreting them as floats gave
// values off by 175 on a ramp spanning 0 to 2 — with the dimensions correct,
// which is what made it look like it worked.
//
// The half path has refused this since it was measured there. The float path
// returned the garbage silently.
func TestReduceResolutionRefusedForFloat(t *testing.T) {
	const w, h, nres = 64, 32, 4
	cs := encodeRamp(t, w, h, nres)

	if _, err := DecodeFloatConfig(bytes.NewReader(cs), nil); err != nil {
		t.Fatalf("full float decode must still work: %v", err)
	}
	got, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{ReduceResolution: 1})
	if err == nil {
		t.Fatalf("ReduceResolution on a float codestream returned a %dx%d image instead of an error",
			got.Width, got.Height)
	}
	if !strings.Contains(err.Error(), "ReduceResolution") {
		t.Errorf("error does not name the option that was refused: %v", err)
	}
}

// TestDecodeAreaMatchesTheFullDecode is the correctness half of region decode:
// the samples must be the same ones a full decode produces for that rectangle,
// exactly, and the returned image must cover the region rather than the image.
//
// Exactness is the right bar here and not a lucky one. A region decode stops
// short of entropy-decoding code-blocks that cannot reach the region, but every
// coefficient that can reach it is decoded and synthesised as usual, so the
// samples are not an approximation of the full decode — they are the same
// arithmetic.
func TestDecodeAreaMatchesTheFullDecode(t *testing.T) {
	const w, h, nres = 64, 48, 4

	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 251)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres,
		PrecinctSizes: []PrecinctSize{{4, 4}, {4, 4}, {4, 4}, {4, 4}},
	}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cs := buf.Bytes()

	full, err := DecodeConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full decode: %v", err)
	}
	at := func(im image.Image, x, y int) uint32 {
		r, _, _, _ := im.At(im.Bounds().Min.X+x, im.Bounds().Min.Y+y).RGBA()
		return r >> 8
	}

	for _, region := range []image.Rectangle{
		image.Rect(0, 0, 16, 16),
		image.Rect(16, 8, 48, 40),
		image.Rect(48, 32, 64, 48),
		image.Rect(0, 0, w, h),
		image.Rect(31, 23, 33, 25), // straddles every precinct boundary nearby
	} {
		got, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
		if err != nil {
			t.Errorf("region %v: %v", region, err)
			continue
		}
		b := got.Bounds()
		if b.Dx() != region.Dx() || b.Dy() != region.Dy() {
			t.Errorf("region %v produced %dx%d, want %dx%d",
				region, b.Dx(), b.Dy(), region.Dx(), region.Dy())
			continue
		}
		bad := 0
		for y := 0; y < region.Dy(); y++ {
			for x := 0; x < region.Dx(); x++ {
				if at(got, x, y) != at(full, region.Min.X+x, region.Min.Y+y) {
					bad++
				}
			}
		}
		if bad != 0 {
			t.Errorf("region %v: %d of %d samples differ from the full decode",
				region, bad, region.Dx()*region.Dy())
		}
	}
}

// TestDecodeAreaIsHonouredNotIgnored replaces a test that asserted the option
// was refused, which was true while it was unimplemented.
//
// A dropped request is worse than a missing feature: a caller sizing a buffer
// for a 32x16 region and receiving the whole 64x32 image has no indication, and
// reads the wrong pixels out of it. That is what this pinned before and what it
// pins now, from the other side.
func TestDecodeAreaIsHonouredNotIgnored(t *testing.T) {
	const w, h, nres = 64, 32, 4
	cs := encodeRamp(t, w, h, nres)
	region := image.Rect(16, 8, 48, 24)

	got, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("float path: %v", err)
	}
	if got.Width != region.Dx() || got.Height != region.Dy() {
		t.Errorf("float path produced %dx%d for region %v", got.Width, got.Height, region)
	}

	gi, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("integer path: %v", err)
	}
	if b := gi.Bounds(); b.Dx() != region.Dx() || b.Dy() != region.Dy() {
		t.Errorf("integer path produced %dx%d for region %v", b.Dx(), b.Dy(), region)
	}

	// A region outside the image is clipped to nothing, which decodes the
	// whole image rather than failing: an empty request is not an error.
	outside := image.Rect(1000, 1000, 1100, 1100)
	if _, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &outside}); err != nil {
		t.Errorf("a region outside the image was refused: %v", err)
	}
}
