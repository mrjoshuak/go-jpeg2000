package entropy

import (
	"testing"
)

func TestMQEncoder_Decoder_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		bits     []int
		contexts []int
	}{
		{"single_zero", []int{0}, []int{0}},
		{"single_one", []int{1}, []int{0}},
		{"alternating", []int{0, 1, 0, 1, 0, 1, 0, 1}, []int{0, 0, 0, 0, 0, 0, 0, 0}},
		{"all_zeros", []int{0, 0, 0, 0, 0, 0, 0, 0}, []int{0, 0, 0, 0, 0, 0, 0, 0}},
		{"all_ones", []int{1, 1, 1, 1, 1, 1, 1, 1}, []int{0, 0, 0, 0, 0, 0, 0, 0}},
		{"mixed_contexts", []int{0, 1, 0, 1}, []int{0, 1, 2, 3}},
		{"uniform_context", []int{0, 1, 0, 1}, []int{CtxUni, CtxUni, CtxUni, CtxUni}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			enc := NewMQEncoder()
			for i, bit := range tt.bits {
				enc.Encode(tt.contexts[i], bit)
			}
			encoded := enc.Flush()

			// Decode
			dec := NewMQDecoder(encoded)
			for i, expected := range tt.bits {
				got := dec.Decode(tt.contexts[i])
				if got != expected {
					t.Errorf("bit %d: got %d, want %d", i, got, expected)
				}
			}
		})
	}
}

func TestMQEncoder_LongSequence(t *testing.T) {
	// Test with a longer sequence
	bits := make([]int, 1000)
	contexts := make([]int, 1000)
	for i := range bits {
		bits[i] = i % 2
		contexts[i] = i % 10
	}

	enc := NewMQEncoder()
	for i, bit := range bits {
		enc.Encode(contexts[i], bit)
	}
	encoded := enc.Flush()

	dec := NewMQDecoder(encoded)
	for i, expected := range bits {
		got := dec.Decode(contexts[i])
		if got != expected {
			t.Errorf("bit %d: got %d, want %d", i, got, expected)
		}
	}
}

func TestMQEncoder_Reset(t *testing.T) {
	enc := NewMQEncoder()
	enc.Encode(0, 1)
	enc.Encode(0, 0)

	enc.Reset()

	// After reset, should be able to encode again
	enc.Encode(0, 0)
	enc.Encode(0, 1)
	data := enc.Flush()

	if len(data) == 0 {
		t.Error("expected encoded data after reset")
	}
}

func TestMQDecoder_ResetContext(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03}
	dec := NewMQDecoder(data)

	dec.ResetContext(0)
	dec.ResetContext(CtxUni)

	// Should not panic
	dec.Decode(0)
}

func TestMQDecoder_ResetAllContexts(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03}
	dec := NewMQDecoder(data)

	dec.ResetAllContexts()

	// Should not panic
	dec.Decode(0)
}

func TestRawEncoder_Decoder_Roundtrip(t *testing.T) {
	bits := []int{0, 1, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 0}

	// Encode
	enc := NewRawEncoder()
	for _, bit := range bits {
		enc.EncodeBit(bit)
	}
	encoded := enc.Flush()

	// Decode
	dec := NewRawDecoder(encoded)
	for i, expected := range bits {
		got := dec.DecodeBit()
		if got != expected {
			t.Errorf("bit %d: got %d, want %d", i, got, expected)
		}
	}
}

func TestRawEncoder_ByteStuffing(t *testing.T) {
	// Encode 8 ones which gives 0xFF
	enc := NewRawEncoder()
	for i := 0; i < 8; i++ {
		enc.EncodeBit(1)
	}
	// Then encode more bits
	for i := 0; i < 8; i++ {
		enc.EncodeBit(0)
	}
	encoded := enc.Flush()

	// Check that byte stuffing is handled
	if len(encoded) == 0 {
		t.Error("expected encoded data")
	}
}

func BenchmarkMQEncoder(b *testing.B) {
	bits := make([]int, 1000)
	for i := range bits {
		bits[i] = i % 2
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc := NewMQEncoder()
		for _, bit := range bits {
			enc.Encode(0, bit)
		}
		enc.Flush()
	}
}

func BenchmarkMQDecoder(b *testing.B) {
	// First encode some data
	bits := make([]int, 1000)
	for i := range bits {
		bits[i] = i % 2
	}

	enc := NewMQEncoder()
	for _, bit := range bits {
		enc.Encode(0, bit)
	}
	encoded := enc.Flush()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewMQDecoder(encoded)
		for range bits {
			dec.Decode(0)
		}
	}
}
