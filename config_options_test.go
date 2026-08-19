package jpeg2000

import (
	"bytes"
	"image"
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

// TestDecodeAreaRefused pins the same for DecodeArea, which was declared in
// Config, documented as "specifies a region to decode", and read by nothing.
//
// A dropped request is worse than a missing feature: a caller sizing a buffer
// for a 32x16 region received a 64x32 image and no indication.
func TestDecodeAreaRefused(t *testing.T) {
	const w, h, nres = 64, 32, 4
	cs := encodeRamp(t, w, h, nres)

	region := image.Rect(16, 8, 48, 24)
	if _, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{DecodeArea: &region}); err == nil {
		t.Error("DecodeArea on the float path was accepted and ignored")
	}
	if _, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &region}); err == nil {
		t.Error("DecodeArea on the integer path was accepted and ignored")
	}
	if _, err := DecodeHalfConfig(bytes.NewReader(cs), &Config{DecodeArea: &region}); err == nil {
		t.Error("DecodeArea on the half path was accepted and ignored")
	}

	// The control: without the option the same codestream decodes.
	if _, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{}); err != nil {
		t.Errorf("an empty Config must not be refused: %v", err)
	}
}
