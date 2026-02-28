package jpeg2000

import (
	"bytes"
	"math"
	"testing"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// TestEncodeFloatGrayscaleRoundtrip tests encoding and decoding a single-channel
// float32 image through the NLT pipeline, verifying bitwise equality.
func TestEncodeFloatGrayscaleRoundtrip(t *testing.T) {
	width, height := 8, 8
	values := []float32{
		0, math.Float32frombits(0x80000000), // +0, -0
		1.0, -1.0,
		math.SmallestNonzeroFloat32,
		math.MaxFloat32,
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
	}

	// Fill an 8x8 image with test values, cycling through them
	comp := make([]float32, width*height)
	for i := range comp {
		comp[i] = values[i%len(values)]
	}

	img := &FloatImage{
		Width:      width,
		Height:     height,
		Components: [][]float32{comp},
		BitDepth:   32,
		Signed:     true,
	}

	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumResolutions = 2

	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, opts); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}

	decoded, err := DecodeFloatConfig(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DecodeFloat: %v", err)
	}

	if decoded.Width != width || decoded.Height != height {
		t.Fatalf("dimensions mismatch: got %dx%d, want %dx%d",
			decoded.Width, decoded.Height, width, height)
	}
	if len(decoded.Components) != 1 {
		t.Fatalf("component count: got %d, want 1", len(decoded.Components))
	}

	for i := 0; i < width*height; i++ {
		got := decoded.Components[0][i]
		want := comp[i]

		// NaN needs special comparison
		if math.IsNaN(float64(want)) {
			if !math.IsNaN(float64(got)) {
				t.Errorf("pixel %d: got %v, want NaN", i, got)
			}
			continue
		}

		gotBits := math.Float32bits(got)
		wantBits := math.Float32bits(want)
		if gotBits != wantBits {
			t.Errorf("pixel %d: got bits 0x%08X (%v), want bits 0x%08X (%v)",
				i, gotBits, got, wantBits, want)
		}
	}
}

// TestEncodeFloatRGBRoundtrip tests encoding and decoding a 3-component
// float32 image, verifying the RCT + NLT interaction.
func TestEncodeFloatRGBRoundtrip(t *testing.T) {
	width, height := 8, 8
	numPixels := width * height

	img := &FloatImage{
		Width:      width,
		Height:     height,
		Components: make([][]float32, 3),
		BitDepth:   32,
		Signed:     true,
	}

	for c := 0; c < 3; c++ {
		img.Components[c] = make([]float32, numPixels)
		for i := 0; i < numPixels; i++ {
			// Each component gets different values to stress the RCT
			img.Components[c][i] = float32(c+1) * float32(i) * 0.01
		}
	}

	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumResolutions = 2

	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, opts); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}

	decoded, err := DecodeFloatConfig(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DecodeFloat: %v", err)
	}

	if len(decoded.Components) != 3 {
		t.Fatalf("component count: got %d, want 3", len(decoded.Components))
	}

	for c := 0; c < 3; c++ {
		for i := 0; i < numPixels; i++ {
			gotBits := math.Float32bits(decoded.Components[c][i])
			wantBits := math.Float32bits(img.Components[c][i])
			if gotBits != wantBits {
				t.Errorf("comp %d pixel %d: got bits 0x%08X (%v), want 0x%08X (%v)",
					c, i, gotBits, decoded.Components[c][i],
					wantBits, img.Components[c][i])
			}
		}
	}
}

// TestEncodeFloatNLTMarkerPresent verifies that the encoded codestream
// contains NLT markers.
func TestEncodeFloatNLTMarkerPresent(t *testing.T) {
	width, height := 4, 4
	img := &FloatImage{
		Width:      width,
		Height:     height,
		Components: [][]float32{make([]float32, width*height)},
		BitDepth:   32,
		Signed:     true,
	}
	for i := range img.Components[0] {
		img.Components[0][i] = float32(i) * 0.5
	}

	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumResolutions = 2

	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, opts); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}

	// Parse the codestream header and check for NLT markers
	parser := codestream.NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}

	if len(header.NLTMarkers) == 0 {
		t.Fatal("expected NLT markers in codestream, found none")
	}

	nlt := header.NLTMarkers[0]
	if nlt.ComponentIndex != 0 {
		t.Errorf("NLT component index: got %d, want 0", nlt.ComponentIndex)
	}
	if nlt.BitDepth != 0x9F {
		t.Errorf("NLT bit depth: got 0x%02X, want 0x9F", nlt.BitDepth)
	}
	if nlt.TransformType != 3 {
		t.Errorf("NLT transform type: got %d, want 3", nlt.TransformType)
	}
}

// TestNLTType3SelfInverse verifies the NLT transform is its own inverse.
func TestNLTType3SelfInverse(t *testing.T) {
	testValues := []float32{
		0, 1.0, -1.0,
		math.SmallestNonzeroFloat32,
		-math.SmallestNonzeroFloat32,
		math.MaxFloat32,
		-math.MaxFloat32,
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		3.14159,
		-2.71828,
	}

	for _, v := range testValues {
		original := int32(math.Float32bits(v))
		data := []int32{original}

		// Apply twice (self-inverse)
		nltType3(data)
		nltType3(data)

		if data[0] != original {
			t.Errorf("NLT not self-inverse for %v (0x%08X): got 0x%08X",
				v, uint32(original), uint32(data[0]))
		}
	}
}

// TestEncodeFloatNaN tests that NaN values survive the roundtrip.
// NaN has many bit patterns; we test a canonical one.
func TestEncodeFloatNaN(t *testing.T) {
	width, height := 4, 4
	img := &FloatImage{
		Width:      width,
		Height:     height,
		Components: [][]float32{make([]float32, width*height)},
		BitDepth:   32,
		Signed:     true,
	}
	// Fill with NaN
	nan := float32(math.NaN())
	for i := range img.Components[0] {
		img.Components[0][i] = nan
	}

	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumResolutions = 2

	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, opts); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}

	decoded, err := DecodeFloatConfig(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DecodeFloat: %v", err)
	}

	for i := 0; i < width*height; i++ {
		if !math.IsNaN(float64(decoded.Components[0][i])) {
			t.Errorf("pixel %d: expected NaN, got %v", i, decoded.Components[0][i])
		}
	}
}

// TestEncodeIntegerRegression ensures existing integer encode paths still work
// after the float encoder changes.
func TestEncodeIntegerRegression(t *testing.T) {
	// Create a simple 8-bit grayscale image
	width, height := 16, 16
	img := makeGrayImage(width, height)

	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.NumResolutions = 3

	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode (integer): %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode (integer): %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		t.Fatalf("dimensions: got %dx%d, want %dx%d",
			bounds.Dx(), bounds.Dy(), width, height)
	}
}
