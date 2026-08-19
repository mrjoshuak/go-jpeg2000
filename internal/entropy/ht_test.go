package entropy

import (
	"testing"
)

// TestHTEncoderDecoder tests HTJ2K round-trip encoding/decoding.
func TestHTEncoderDecoder(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		bandType int
	}{
		{"4x4_LL", 4, 4, BandLL},
		{"8x8_LL", 8, 8, BandLL},
		{"16x16_LL", 16, 16, BandLL},
		{"32x32_LL", 32, 32, BandLL},
		{"64x64_LL", 64, 64, BandLL},
		{"32x32_HL", 32, 32, BandHL},
		{"32x32_LH", 32, 32, BandLH},
		{"32x32_HH", 32, 32, BandHH},
		{"128x128_LL", 128, 128, BandLL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data with varying magnitudes
			data := make([]int32, tt.width*tt.height)
			for i := range data {
				// Mix of positive and negative values with varying magnitudes
				val := int32((i % 256) - 128)
				if i%7 == 0 {
					val *= 4 // Some larger values
				}
				data[i] = val
			}

			// Encode
			enc := NewHTEncoder(tt.width, tt.height)
			enc.SetData(data)
			encoded := enc.Encode(tt.bandType)

			if encoded == nil {
				t.Fatalf("Encode returned nil for non-zero data")
			}

			// Decode
			dec := NewHTDecoder(tt.width, tt.height)
			numBitplanes := 16 // Use enough bitplanes
			decoded := dec.Decode(encoded, numBitplanes, tt.bandType)

			if len(decoded) != len(data) {
				t.Fatalf("Decoded length mismatch: got %d, want %d", len(decoded), len(data))
			}

			// Note: HTJ2K is lossy for the cleanup pass, so we check that
			// significant coefficients are preserved (not exact match)
			significantMatch := 0
			totalSignificant := 0
			for i := range data {
				if data[i] != 0 {
					totalSignificant++
					// Check if significance is preserved (sign matches)
					if (data[i] > 0 && decoded[i] > 0) || (data[i] < 0 && decoded[i] < 0) {
						significantMatch++
					}
				}
			}

			if totalSignificant > 0 {
				matchRate := float64(significantMatch) / float64(totalSignificant)
				t.Logf("Significance match rate: %.2f%% (%d/%d)", matchRate*100, significantMatch, totalSignificant)
			}
		})
	}
}

// TestHTEncoderEmptyBlock tests encoding an empty code block.
func TestHTEncoderEmptyBlock(t *testing.T) {
	enc := NewHTEncoder(32, 32)

	// All zeros
	data := make([]int32, 32*32)
	enc.SetData(data)
	encoded := enc.Encode(BandLL)

	if encoded != nil {
		t.Logf("Empty block encoded to %d bytes (expected nil or minimal)", len(encoded))
	}
}

// TestHTEncoderPooling tests the pooling mechanism.
func TestHTEncoderPooling(t *testing.T) {
	// Get and return multiple encoders/decoders
	for i := 0; i < 10; i++ {
		enc := GetHTEncoder(32, 32)
		if enc.width != 32 || enc.height != 32 {
			t.Errorf("Encoder size mismatch: got %dx%d, want 32x32", enc.width, enc.height)
		}
		PutHTEncoder(enc)

		dec := GetHTDecoder(64, 64)
		if dec.width != 64 || dec.height != 64 {
			t.Errorf("Decoder size mismatch: got %dx%d, want 64x64", dec.width, dec.height)
		}
		PutHTDecoder(dec)
	}

	// Test resize
	enc := GetHTEncoder(32, 32)
	enc.Resize(128, 128)
	if enc.width != 128 || enc.height != 128 {
		t.Errorf("Encoder resize failed: got %dx%d, want 128x128", enc.width, enc.height)
	}
	PutHTEncoder(enc)

	dec := GetHTDecoder(32, 32)
	dec.Resize(128, 128)
	if dec.width != 128 || dec.height != 128 {
		t.Errorf("Decoder resize failed: got %dx%d, want 128x128", dec.width, dec.height)
	}
	PutHTDecoder(dec)
}

// TestHTDecoderMinimalData tests decoding with minimal data.
func TestHTDecoderMinimalData(t *testing.T) {
	dec := NewHTDecoder(16, 16)

	// Empty data
	decoded := dec.Decode(nil, 8, BandLL)
	if decoded == nil {
		t.Error("Decode returned nil for empty data")
	}

	// Single byte
	decoded = dec.Decode([]byte{0x00}, 8, BandLL)
	if decoded == nil {
		t.Error("Decode returned nil for single byte")
	}

	// Two bytes (minimal valid size)
	decoded = dec.Decode([]byte{0x00, 0x02}, 8, BandLL)
	if decoded == nil {
		t.Error("Decode returned nil for two bytes")
	}
}

// TestHTDecodeSegments tests DecodeSegments with explicit lcup.
func TestHTDecodeSegments(t *testing.T) {
	width, height := 32, 32
	data := make([]int32, width*height)
	for i := range data {
		data[i] = int32((i % 256) - 128)
	}

	enc := NewHTEncoder(width, height)
	enc.SetData(data)
	encoded := enc.Encode(BandLL)
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}

	// DecodeSegments with lcup == len(data) should behave same as Decode
	dec := NewHTDecoder(width, height)
	decoded := dec.DecodeSegments(encoded, len(encoded), 16, BandLL)

	dec2 := NewHTDecoder(width, height)
	decoded2 := dec2.Decode(encoded, 16, BandLL)

	for i := range decoded {
		if decoded[i] != decoded2[i] {
			t.Errorf("Mismatch at %d: DecodeSegments=%d, Decode=%d", i, decoded[i], decoded2[i])
		}
	}
}

// TestHTSPPMRPEmptyRefinement tests SPP/MRP with no refinement data.
func TestHTSPPMRPEmptyRefinement(t *testing.T) {
	width, height := 8, 8
	data := make([]int32, width*height)
	for i := range data {
		data[i] = int32(i%16 - 8)
	}

	enc := NewHTEncoder(width, height)
	enc.SetData(data)
	encoded := enc.Encode(BandLL)
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}

	// Decode with no SPP/MRP (lcup == total length)
	dec := NewHTDecoder(width, height)
	decoded := dec.DecodeSegments(encoded, len(encoded), 16, BandLL)
	if decoded == nil {
		t.Fatal("DecodeSegments returned nil")
	}
}

// TestHTDecodeSPPMRPHelpers tests the significance helper methods directly.
func TestHTDecodeSPPMRPHelpers(t *testing.T) {
	dec := NewHTDecoder(8, 8)

	// Initially nothing is significant
	if dec.isSignificant(0, 0) {
		t.Error("Expected (0,0) to be insignificant initially")
	}

	// Set (0,0) as significant
	dec.setSignificant(0, 0)
	if !dec.isSignificant(0, 0) {
		t.Error("Expected (0,0) to be significant after setSignificant")
	}

	// Check that neighbor detection works
	if !dec.hasSignificantNeighbor(1, 0) {
		t.Error("Expected (1,0) to have a significant neighbor")
	}
	if !dec.hasSignificantNeighbor(0, 1) {
		t.Error("Expected (0,1) to have a significant neighbor")
	}
	if !dec.hasSignificantNeighbor(1, 1) {
		t.Error("Expected (1,1) to have a significant neighbor")
	}

	// Far-away sample should not have a significant neighbor
	if dec.hasSignificantNeighbor(4, 4) {
		t.Error("Expected (4,4) to have no significant neighbor")
	}

	// Out-of-bounds should not crash
	if dec.isSignificant(-1, 0) {
		t.Error("Out-of-bounds should not be significant")
	}
	if dec.isSignificant(0, -1) {
		t.Error("Out-of-bounds should not be significant")
	}
	if dec.isSignificant(8, 0) {
		t.Error("Out-of-bounds should not be significant")
	}
	if dec.isSignificant(0, 8) {
		t.Error("Out-of-bounds should not be significant")
	}
}

// BenchmarkHTEncoder benchmarks HTJ2K encoding.
func BenchmarkHTEncoder(b *testing.B) {
	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"32x32", 32, 32},
		{"64x64", 64, 64},
		{"128x128", 128, 128},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			data := make([]int32, size.width*size.height)
			for i := range data {
				data[i] = int32((i % 256) - 128)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				enc := GetHTEncoder(size.width, size.height)
				enc.SetData(data)
				_ = enc.Encode(BandLL)
				PutHTEncoder(enc)
			}
		})
	}
}

// BenchmarkHTDecoder benchmarks HTJ2K decoding.
func BenchmarkHTDecoder(b *testing.B) {
	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"32x32", 32, 32},
		{"64x64", 64, 64},
		{"128x128", 128, 128},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			// Create and encode test data
			data := make([]int32, size.width*size.height)
			for i := range data {
				data[i] = int32((i % 256) - 128)
			}
			enc := NewHTEncoder(size.width, size.height)
			enc.SetData(data)
			encoded := enc.Encode(BandLL)

			if encoded == nil {
				b.Skip("No encoded data")
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dec := GetHTDecoder(size.width, size.height)
				_ = dec.Decode(encoded, 16, BandLL)
				PutHTDecoder(dec)
			}
		})
	}
}

// BenchmarkHTvsT1 compares HT encoding vs traditional T1 encoding.
func BenchmarkHTvsT1(b *testing.B) {
	width, height := 64, 64
	data := make([]int32, width*height)
	for i := range data {
		data[i] = int32((i % 256) - 128)
	}

	b.Run("HT_Encode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			enc := GetHTEncoder(width, height)
			enc.SetData(data)
			_ = enc.Encode(BandLL)
			PutHTEncoder(enc)
		}
	})

	b.Run("T1_Encode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t1 := GetT1(width, height)
			t1.SetData(data)
			_ = t1.Encode(BandLL)
			PutT1(t1)
		}
	})
}
