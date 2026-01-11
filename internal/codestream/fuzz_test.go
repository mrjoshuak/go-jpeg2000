package codestream

import (
	"bytes"
	"testing"
)

// FuzzParseCodestream tests the codestream parser with arbitrary input.
// Run with: go test -fuzz=FuzzParseCodestream -fuzztime=60s
func FuzzParseCodestream(f *testing.F) {
	// Minimal SOC + SIZ header start
	f.Add([]byte{
		0xFF, 0x4F, // SOC
		0xFF, 0x51, // SIZ marker
		0x00, 0x2F, // Length = 47
	})

	// Just SOC
	f.Add([]byte{0xFF, 0x4F})

	// Invalid marker after SOC
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x00})

	// Empty
	f.Add([]byte{})

	// Random markers
	f.Add([]byte{0xFF, 0x90, 0xFF, 0x93, 0xFF, 0xD9})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		p := NewParser(r)
		_, _ = p.ReadHeader()
	})
}

// FuzzReadMarkerSegments tests marker segment parsing with the full parser.
func FuzzReadMarkerSegments(f *testing.F) {
	// Minimal SOC + SIZ
	f.Add([]byte{
		0xFF, 0x4F, // SOC
		0xFF, 0x51, // SIZ marker
		0x00, 0x2F, // Length = 47
		0x00, 0x00, // Rsiz
		0x00, 0x00, 0x00, 0x40, // Width
		0x00, 0x00, 0x00, 0x40, // Height
		0x00, 0x00, 0x00, 0x00, // X offset
		0x00, 0x00, 0x00, 0x00, // Y offset
		0x00, 0x00, 0x00, 0x40, // Tile width
		0x00, 0x00, 0x00, 0x40, // Tile height
		0x00, 0x00, 0x00, 0x00, // Tile X offset
		0x00, 0x00, 0x00, 0x00, // Tile Y offset
		0x00, 0x01, // 1 component
		0x07, 0x01, 0x01, // Component info
	})

	f.Add([]byte{})
	f.Add([]byte{0xFF, 0x4F})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		p := NewParser(r)
		// The parser should not panic on any input
		_, _ = p.ReadHeader()
	})
}
