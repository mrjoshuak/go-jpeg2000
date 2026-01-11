package jpeg2000

import (
	"bytes"
	"testing"
)

// FuzzDecode tests the decoder with arbitrary input data.
// Run with: go test -fuzz=FuzzDecode -fuzztime=60s
func FuzzDecode(f *testing.F) {
	// Add seed corpus with minimal valid JP2 and J2K headers
	// Minimal JP2 signature
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, // JP2 signature box
		0x0D, 0x0A, 0x87, 0x0A,
	})

	// Minimal J2K SOC marker
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51}) // SOC + SIZ start

	// Empty input
	f.Add([]byte{})

	// Single byte inputs
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		// The decoder should never panic, regardless of input
		r := bytes.NewReader(data)
		_, _ = Decode(r)
	})
}

// FuzzDecodeConfig tests configuration parsing with arbitrary input.
func FuzzDecodeConfig(f *testing.F) {
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20,
		0x0D, 0x0A, 0x87, 0x0A,
	})
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = DecodeConfig(r, nil)
	})
}

// FuzzDecodeMetadata tests metadata extraction with arbitrary input.
func FuzzDecodeMetadata(f *testing.F) {
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20,
		0x0D, 0x0A, 0x87, 0x0A,
	})
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = DecodeMetadata(r)
	})
}
