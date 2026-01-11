package entropy

import (
	"testing"
)

// FuzzT1Decode tests the T1 decoder with arbitrary input.
// Run with: go test -fuzz=FuzzT1Decode -fuzztime=60s
func FuzzT1Decode(f *testing.F) {
	// Minimal MQ-encoded data
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x80, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test with various block sizes
		for _, size := range []int{4, 8, 16, 32, 64} {
			t1 := NewT1(size, size)
			// The decoder should never panic
			_ = t1.Decode(data, 8, BandLL)
		}
	})
}

// FuzzHTDecode tests the HTJ2K decoder with arbitrary input.
func FuzzHTDecode(f *testing.F) {
	// Minimal HT data with SCUP marker
	f.Add([]byte{0x00, 0x02}) // Minimal SCUP = 2
	f.Add([]byte{0x00, 0x00, 0x00, 0x04}) // SCUP = 4
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		for _, size := range []int{4, 8, 16, 32, 64} {
			dec := NewHTDecoder(size, size)
			// The decoder should never panic
			_ = dec.Decode(data, 8, BandLL)
		}
	})
}

// FuzzMQDecode tests the MQ decoder directly.
func FuzzMQDecode(f *testing.F) {
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0xFF, 0xFF})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		dec := NewMQDecoder(data)
		// Decode some symbols - should never panic
		for i := 0; i < 100 && i < len(data)*8; i++ {
			_ = dec.Decode(i % NumContexts)
		}
	})
}

// FuzzHTEncoder tests the HT encoder with arbitrary coefficient data.
func FuzzHTEncoder(f *testing.F) {
	// Add some seed data patterns
	f.Add([]byte{0, 0, 0, 0, 1, 2, 3, 4})
	f.Add([]byte{255, 128, 64, 32, 16, 8, 4, 2})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Convert bytes to int32 coefficients
		coeffs := make([]int32, len(data))
		for i, b := range data {
			coeffs[i] = int32(int8(b)) // Signed conversion
		}

		// Find a suitable block size
		size := 4
		for size*size < len(coeffs) && size < 128 {
			size *= 2
		}
		if size*size > len(coeffs) {
			// Pad coefficients
			padded := make([]int32, size*size)
			copy(padded, coeffs)
			coeffs = padded
		}

		enc := NewHTEncoder(size, size)
		enc.SetData(coeffs)
		// Should never panic
		_ = enc.Encode(BandLL)
	})
}
