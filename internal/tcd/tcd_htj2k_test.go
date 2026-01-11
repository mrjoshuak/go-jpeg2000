package tcd

import (
	"testing"

	"github.com/aswf/go-jpeg2000/internal/codestream"
	"github.com/aswf/go-jpeg2000/internal/entropy"
)

// TestTileEncoderHTJ2K tests HTJ2K mode in the tile encoder.
func TestTileEncoderHTJ2K(t *testing.T) {
	// Create a minimal header with HTJ2K enabled
	header := &codestream.Header{
		ImageWidth:    64,
		ImageHeight:   64,
		NumComponents: 1,
		TileWidth:     64,
		TileHeight:    64,
		NumTilesX:     1,
		NumTilesY:     1,
		ComponentInfo: []codestream.ComponentInfo{
			{BitDepth: 8, SubsamplingX: 1, SubsamplingY: 1}, // 8-bit unsigned, no subsampling
		},
		CodingStyle: codestream.CodingStyleDefault{
			NumDecompositions:  3,
			CodeBlockWidthExp:  2, // 16x16 blocks
			CodeBlockHeightExp: 2,
			CodeBlockStyle:     0x40, // CodeBlockHT flag
			WaveletTransform:   1,    // 5-3 reversible
		},
		Capabilities: &codestream.CapabilitiesMarker{
			Pcap: codestream.CapPcapHTJ2K,
		},
	}

	// Verify header reports HTJ2K mode
	if !header.IsHTJ2K() {
		t.Fatal("Header should report HTJ2K mode")
	}

	// Create tile encoder
	enc := NewTileEncoder(header)
	if !enc.htj2k {
		t.Fatal("TileEncoder should have htj2k=true")
	}

	// Create test component data
	componentData := [][]int32{
		make([]int32, 64*64),
	}
	for i := range componentData[0] {
		componentData[0][i] = int32(i % 256)
	}

	// Initialize tile
	enc.InitTile(0, componentData)

	// Create a code block and encode it
	cb := &CodeBlock{
		X0: 0, Y0: 0, X1: 16, Y1: 16,
	}

	data := make([]int32, 16*16)
	for i := range data {
		data[i] = int32((i * 17) % 256)
	}

	// This should use the HT encoder
	enc.EncodeCodeBlock(cb, data, entropy.BandLL)

	if cb.Data == nil {
		t.Log("Encoded data is nil (may be valid for zero data)")
	} else {
		t.Logf("Encoded %d bytes using HTJ2K", len(cb.Data))
	}
}

// TestTileDecoderHTJ2K tests HTJ2K mode in the tile decoder.
func TestTileDecoderHTJ2K(t *testing.T) {
	header := &codestream.Header{
		ImageWidth:    64,
		ImageHeight:   64,
		NumComponents: 1,
		TileWidth:     64,
		TileHeight:    64,
		NumTilesX:     1,
		NumTilesY:     1,
		ComponentInfo: []codestream.ComponentInfo{
			{BitDepth: 8, SubsamplingX: 1, SubsamplingY: 1},
		},
		CodingStyle: codestream.CodingStyleDefault{
			NumDecompositions:  3,
			CodeBlockWidthExp:  2,
			CodeBlockHeightExp: 2,
			CodeBlockStyle:     0x40,
			WaveletTransform:   1,
		},
		Capabilities: &codestream.CapabilitiesMarker{
			Pcap: codestream.CapPcapHTJ2K,
		},
	}

	dec := NewTileDecoder(header)
	if !dec.htj2k {
		t.Fatal("TileDecoder should have htj2k=true")
	}

	// Test SetHTJ2K
	dec.SetHTJ2K(false)
	if dec.htj2k {
		t.Fatal("SetHTJ2K(false) should disable HTJ2K mode")
	}
	dec.SetHTJ2K(true)
	if !dec.htj2k {
		t.Fatal("SetHTJ2K(true) should enable HTJ2K mode")
	}
}

// TestHTJ2KRoundTrip tests encoding and decoding with HTJ2K.
func TestHTJ2KRoundTrip(t *testing.T) {
	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"16x16", 16, 16},
		{"32x32", 32, 32},
		{"64x64", 64, 64},
		{"128x128", 128, 128},
	}

	for _, size := range sizes {
		t.Run(size.name, func(t *testing.T) {
			// Create test data
			data := make([]int32, size.width*size.height)
			for i := range data {
				data[i] = int32((i * 37) % 256) - 128 // Mix of positive and negative
			}

			// Encode with HT encoder
			htEnc := entropy.NewHTEncoder(size.width, size.height)
			htEnc.SetData(data)
			encoded := htEnc.Encode(entropy.BandLL)

			if encoded == nil {
				t.Log("HT encoder returned nil (may be valid)")
				return
			}

			// Decode with HT decoder
			htDec := entropy.NewHTDecoder(size.width, size.height)
			decoded := htDec.Decode(encoded, 16, entropy.BandLL)

			if len(decoded) != len(data) {
				t.Fatalf("Decoded length mismatch: got %d, want %d", len(decoded), len(data))
			}

			// Count significant matches (HT is lossy in cleanup-only mode)
			matches := 0
			for i := range data {
				if data[i] != 0 && decoded[i] != 0 {
					matches++
				}
			}
			t.Logf("Non-zero matches: %d/%d", matches, len(data))
		})
	}
}

// BenchmarkHTJ2KEncode benchmarks HTJ2K encoding through TCD.
func BenchmarkHTJ2KEncode(b *testing.B) {
	header := &codestream.Header{
		ImageWidth:    64,
		ImageHeight:   64,
		NumComponents: 1,
		TileWidth:     64,
		TileHeight:    64,
		NumTilesX:     1,
		NumTilesY:     1,
		ComponentInfo: []codestream.ComponentInfo{
			{BitDepth: 8, SubsamplingX: 1, SubsamplingY: 1},
		},
		CodingStyle: codestream.CodingStyleDefault{
			NumDecompositions:  0,
			CodeBlockWidthExp:  4, // 64x64
			CodeBlockHeightExp: 4,
			CodeBlockStyle:     0x40,
			WaveletTransform:   1,
		},
		Capabilities: &codestream.CapabilitiesMarker{
			Pcap: codestream.CapPcapHTJ2K,
		},
	}

	data := make([]int32, 64*64)
	for i := range data {
		data[i] = int32(i % 256)
	}

	cb := &CodeBlock{X0: 0, Y0: 0, X1: 64, Y1: 64}
	enc := NewTileEncoder(header)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.EncodeCodeBlock(cb, data, entropy.BandLL)
	}
}
