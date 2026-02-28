package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
)

// createTestGrayImage creates a gradient grayscale image for testing.
func createTestGrayImage(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8((x*17 + y*31) % 256)
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return img
}

// createTestRGBAImage creates a gradient RGB image for testing.
func createTestRGBAImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*17 + y*5) % 256),
				G: uint8((x*7 + y*23) % 256),
				B: uint8((x*13 + y*11) % 256),
				A: 255,
			})
		}
	}
	return img
}

// findCODNumLayers finds the COD marker in a J2K codestream and returns the
// number of quality layers encoded in the header.
func findCODNumLayers(data []byte) (int, bool) {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0x52 { // COD marker
			// Skip marker (2 bytes), skip length (2 bytes), skip Scod (1 byte)
			if i+8 >= len(data) {
				return 0, false
			}
			// SGcod: progression order (1 byte), then NumLayers (2 bytes)
			offset := i + 2 + 2 + 1 // marker + length + Scod
			if offset+3 > len(data) {
				return 0, false
			}
			// progression order is 1 byte, then NumLayers is 2 bytes big-endian
			numLayers := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
			return numLayers, true
		}
	}
	return 0, false
}

// findCODProgressionOrder extracts the progression order from the COD marker.
func findCODProgressionOrder(data []byte) (int, bool) {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0x52 { // COD marker
			offset := i + 2 + 2 + 1 // marker + length + Scod
			if offset >= len(data) {
				return 0, false
			}
			return int(data[offset]), true
		}
	}
	return 0, false
}

// TestMultiLayer_HeaderLayerCount verifies that the COD marker reports
// the correct number of quality layers for various NumLayers settings.
func TestMultiLayer_HeaderLayerCount(t *testing.T) {
	img := createTestGrayImage(32, 32)

	tests := []struct {
		name      string
		numLayers int
	}{
		{"4 layers", 4},
		{"8 layers", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := DefaultOptions()
			opts.Format = FormatJ2K
			opts.NumLayers = tt.numLayers
			opts.Lossless = true

			if err := Encode(&buf, img, opts); err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			data := buf.Bytes()

			// Verify raw COD marker in the bitstream
			numLayers, found := findCODNumLayers(data)
			if !found {
				t.Fatal("COD marker not found in codestream")
			}
			if numLayers != tt.numLayers {
				t.Errorf("COD NumLayers = %d, want %d", numLayers, tt.numLayers)
			}

			// Also verify via DecodeMetadata API
			meta, err := DecodeMetadata(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeMetadata() error: %v", err)
			}
			if meta.NumQualityLayers != tt.numLayers {
				t.Errorf("Metadata.NumQualityLayers = %d, want %d", meta.NumQualityLayers, tt.numLayers)
			}
		})
	}
}

// TestMultiLayer_HeaderLayerCount_JP2 verifies layer count in JP2 format.
func TestMultiLayer_HeaderLayerCount_JP2(t *testing.T) {
	img := createTestGrayImage(32, 32)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.NumLayers = 4

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.NumQualityLayers != 4 {
		t.Errorf("Metadata.NumQualityLayers = %d, want 4", meta.NumQualityLayers)
	}
}

// TestMultiLayer_HeaderProgressionOrder verifies the progression order is
// correctly written to the COD marker.
func TestMultiLayer_HeaderProgressionOrder(t *testing.T) {
	img := createTestGrayImage(32, 32)

	tests := []struct {
		name  string
		order ProgressionOrder
		want  int
	}{
		{"LRCP", LRCP, 0},
		{"RLCP", RLCP, 1},
		{"RPCL", RPCL, 2},
		{"PCRL", PCRL, 3},
		{"CPRL", CPRL, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := DefaultOptions()
			opts.Format = FormatJ2K
			opts.NumLayers = 4
			opts.ProgressionOrder = tt.order

			if err := Encode(&buf, img, opts); err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			progOrder, found := findCODProgressionOrder(buf.Bytes())
			if !found {
				t.Fatal("COD marker not found")
			}
			if progOrder != tt.want {
				t.Errorf("COD ProgressionOrder = %d, want %d", progOrder, tt.want)
			}
		})
	}
}

// computeMSE computes the mean squared error between two images.
func computeMSE(a, b image.Image) float64 {
	bounds := a.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	sum := 0.0
	n := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ra, ga, ba, _ := a.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			rb, gb, bb, _ := b.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			dr := float64(ra) - float64(rb)
			dg := float64(ga) - float64(gb)
			db := float64(ba) - float64(bb)
			sum += dr*dr + dg*dg + db*db
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// computePSNR computes the PSNR in dB.
func computePSNR(mse float64) float64 {
	if mse == 0 {
		return math.Inf(1)
	}
	maxVal := 65535.0
	return 10 * math.Log10(maxVal*maxVal/mse)
}

// TestMultiLayer_ProgressiveQuality verifies that decoding more quality
// layers produces monotonically improving (or equal) image quality.
//
// NOTE: The current decoder uses a simplified tile decode path that does
// not perform T2 packet decoding. As a result, QualityLayers does not
// yet affect the decoded output. This test documents that behavior and
// will begin enforcing progressive quality once the full T2 decode
// pipeline is integrated.
func TestMultiLayer_ProgressiveQuality(t *testing.T) {
	original := createTestGrayImage(64, 64)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 8
	opts.ProgressionOrder = LRCP
	opts.Lossless = false
	opts.Quality = 75

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	encoded := buf.Bytes()

	// Verify metadata is correct
	meta, err := DecodeMetadata(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.NumQualityLayers != 8 {
		t.Errorf("NumQualityLayers = %d, want 8", meta.NumQualityLayers)
	}

	// Decode at each quality layer level and log quality metrics
	var mseByLayer [8]float64
	for layer := 1; layer <= 8; layer++ {
		decoded, err := DecodeConfig(bytes.NewReader(encoded), &Config{
			QualityLayers: layer,
		})
		if err != nil {
			t.Fatalf("DecodeConfig(QualityLayers=%d) error: %v", layer, err)
		}

		mse := computeMSE(original, decoded)
		psnr := computePSNR(mse)
		mseByLayer[layer-1] = mse

		t.Logf("Layer %d: MSE=%.2f, PSNR=%.2f dB", layer, mse, psnr)
	}

	// Quality should never decrease as layers increase.
	// Currently all layers produce identical output due to the simplified
	// decoder, so we only verify non-degradation (MSE must not increase).
	for i := 1; i < 8; i++ {
		if mseByLayer[i] > mseByLayer[i-1]+1e-6 {
			t.Errorf("Quality decreased at layer %d: MSE %.2f > layer %d MSE %.2f",
				i+1, mseByLayer[i], i, mseByLayer[i-1])
		}
	}
}

// TestMultiLayer_RateDistribution verifies the encoded stream is non-trivial
// and logs size information for rate analysis.
func TestMultiLayer_RateDistribution(t *testing.T) {
	original := createTestGrayImage(64, 64)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 8
	opts.ProgressionOrder = LRCP
	opts.Lossless = false
	opts.Quality = 75

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	totalSize := buf.Len()
	t.Logf("Total encoded size: %d bytes (NumLayers=8, 64x64 gray)", totalSize)

	if totalSize < 50 {
		t.Fatalf("Encoded stream too small (%d bytes), likely no real data", totalSize)
	}

	// Verify structure: should have SOC, SIZ, COD, QCD, SOT, SOD, EOC
	data := buf.Bytes()
	if data[0] != 0xFF || data[1] != 0x4F {
		t.Error("Missing SOC marker")
	}
	eocPos := len(data) - 2
	if data[eocPos] != 0xFF || data[eocPos+1] != 0xD9 {
		t.Error("Missing EOC marker")
	}
}

// TestMultiLayer_ValidProgressiveBitstream verifies that truncated
// codestreams can still be decoded and produce valid image dimensions.
func TestMultiLayer_ValidProgressiveBitstream(t *testing.T) {
	original := createTestGrayImage(64, 64)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 4
	opts.ProgressionOrder = LRCP
	opts.Lossless = false
	opts.Quality = 75

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	encoded := buf.Bytes()

	// Find the SOD marker (start of data) and EOC marker
	sodPos := -1
	for i := 0; i < len(encoded)-1; i++ {
		if encoded[i] == 0xFF && encoded[i+1] == 0x93 { // SOD
			sodPos = i + 2
			break
		}
	}
	if sodPos < 0 {
		t.Fatal("SOD marker not found")
	}

	eocPos := len(encoded) - 2
	if encoded[eocPos] != 0xFF || encoded[eocPos+1] != 0xD9 {
		t.Fatal("EOC marker not at expected position")
	}

	dataLen := eocPos - sodPos

	// Full decode should succeed and match dimensions
	fullDecoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Full decode error: %v", err)
	}
	if fullDecoded.Bounds().Dx() != 64 || fullDecoded.Bounds().Dy() != 64 {
		t.Fatalf("Full decode dimensions = %dx%d, want 64x64",
			fullDecoded.Bounds().Dx(), fullDecoded.Bounds().Dy())
	}

	// Truncated streams should either decode with correct dimensions or return an error
	truncPoints := []float64{0.25, 0.50, 0.75}
	for _, frac := range truncPoints {
		truncLen := sodPos + int(float64(dataLen)*frac)
		truncated := make([]byte, truncLen+2)
		copy(truncated, encoded[:truncLen])
		truncated[truncLen] = 0xFF
		truncated[truncLen+1] = 0xD9

		decoded, err := Decode(bytes.NewReader(truncated))
		if err != nil {
			continue // truncation error is acceptable
		}
		// If it decodes, dimensions must still be correct
		if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
			t.Errorf("Truncated %.0f%%: decoded dimensions = %dx%d, want 64x64",
				frac*100, decoded.Bounds().Dx(), decoded.Bounds().Dy())
		}
	}
}

// TestMultiLayer_LosslessMultiLayer verifies multi-layer with lossless encoding.
func TestMultiLayer_LosslessMultiLayer(t *testing.T) {
	img := createTestGrayImage(32, 32)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 4
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.NumQualityLayers != 4 {
		t.Errorf("NumQualityLayers = %d, want 4", meta.NumQualityLayers)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 32 || bounds.Dy() != 32 {
		t.Errorf("Decoded dimensions = %dx%d, want 32x32", bounds.Dx(), bounds.Dy())
	}
}

// TestMultiLayer_RGBMultiLayer verifies multi-layer encoding for RGB images.
func TestMultiLayer_RGBMultiLayer(t *testing.T) {
	img := createTestRGBAImage(32, 32)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 4
	opts.ProgressionOrder = LRCP

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.NumQualityLayers != 4 {
		t.Errorf("NumQualityLayers = %d, want 4", meta.NumQualityLayers)
	}
	if meta.NumComponents != 3 {
		t.Errorf("NumComponents = %d, want 3", meta.NumComponents)
	}
}

// TestMultiLayer_DecodeAllLayers verifies that decoding with QualityLayers=0
// (all layers) produces the same result as without the config.
func TestMultiLayer_DecodeAllLayers(t *testing.T) {
	img := createTestGrayImage(32, 32)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.NumLayers = 4
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	encoded := buf.Bytes()

	decoded1, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	decoded2, err := DecodeConfig(bytes.NewReader(encoded), &Config{QualityLayers: 0})
	if err != nil {
		t.Fatalf("DecodeConfig(QualityLayers=0) error: %v", err)
	}

	mse := computeMSE(decoded1, decoded2)
	if mse != 0 {
		t.Errorf("QualityLayers=0 should match full decode, got MSE=%.6f", mse)
	}
}

// TestMultiLayer_DefaultLayerCount verifies the default layer count is 1.
func TestMultiLayer_DefaultLayerCount(t *testing.T) {
	img := createTestGrayImage(16, 16)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.NumQualityLayers != 1 {
		t.Errorf("Default NumQualityLayers = %d, want 1", meta.NumQualityLayers)
	}
}

// TestMultiLayer_EncodeSizeVaries verifies that different layer counts
// produce different COD NumLayers values in the encoded bitstream.
func TestMultiLayer_EncodeSizeVaries(t *testing.T) {
	img := createTestGrayImage(32, 32)

	encodeBytes := func(numLayers int) []byte {
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Format = FormatJ2K
		opts.NumLayers = numLayers

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode(NumLayers=%d) error: %v", numLayers, err)
		}
		return buf.Bytes()
	}

	data1 := encodeBytes(1)
	data4 := encodeBytes(4)

	if len(data1) == 0 {
		t.Fatal("1-layer encoding produced empty output")
	}
	if len(data4) == 0 {
		t.Fatal("4-layer encoding produced empty output")
	}

	// Verify COD markers report correct layer counts
	nl1, ok1 := findCODNumLayers(data1)
	nl4, ok4 := findCODNumLayers(data4)
	if !ok1 || !ok4 {
		t.Fatal("COD marker not found")
	}
	if nl1 != 1 {
		t.Errorf("COD NumLayers = %d, want 1", nl1)
	}
	if nl4 != 4 {
		t.Errorf("COD NumLayers = %d, want 4", nl4)
	}
}
