package entropy

import (
	"testing"
)

func TestT1_Encode_Decode_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		bandType int
		data     []int32
	}{
		{"4x4_LL_simple", 4, 4, BandLL, []int32{
			1, 2, 3, 4,
			5, 6, 7, 8,
			9, 10, 11, 12,
			13, 14, 15, 16,
		}},
		{"4x4_LL_zeros", 4, 4, BandLL, make([]int32, 16)},
		{"4x4_HL", 4, 4, BandHL, []int32{
			-1, 2, -3, 4,
			5, -6, 7, -8,
			-9, 10, -11, 12,
			13, -14, 15, -16,
		}},
		{"4x4_HH", 4, 4, BandHH, []int32{
			1, -1, 1, -1,
			-1, 1, -1, 1,
			1, -1, 1, -1,
			-1, 1, -1, 1,
		}},
		{"8x8_LL", 8, 8, BandLL, func() []int32 {
			data := make([]int32, 64)
			for i := range data {
				data[i] = int32(i * 2)
			}
			return data
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create T1 encoder
			t1Enc := NewT1(tt.width, tt.height)
			t1Enc.SetData(tt.data)

			// Encode
			encoded := t1Enc.Encode(tt.bandType)

			// Skip if all zeros (no encoding needed)
			if len(encoded) == 0 {
				allZero := true
				for _, v := range tt.data {
					if v != 0 {
						allZero = false
						break
					}
				}
				if !allZero {
					t.Error("expected encoded data for non-zero input")
				}
				return
			}

			// Find number of bit-planes
			maxVal := int32(0)
			for _, v := range tt.data {
				if v < 0 {
					v = -v
				}
				if v > maxVal {
					maxVal = v
				}
			}
			numBPS := 1
			for (1 << numBPS) <= maxVal {
				numBPS++
			}

			// Create T1 decoder
			t1Dec := NewT1(tt.width, tt.height)
			decoded := t1Dec.Decode(encoded, numBPS, tt.bandType)

			// Compare
			for i := range tt.data {
				if decoded[i] != tt.data[i] {
					t.Errorf("position %d: got %d, want %d", i, decoded[i], tt.data[i])
				}
			}
		})
	}
}

func TestT1_FlagsIndex(t *testing.T) {
	t1 := NewT1(4, 4)

	// Test that flagIndex returns valid indices
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			idx := t1.flagIndex(x, y)
			if idx < 0 || idx >= len(t1.flags) {
				t.Errorf("flagIndex(%d, %d) = %d, out of range", x, y, idx)
			}
		}
	}
}

func TestT1_SetFlag_HasFlag(t *testing.T) {
	t1 := NewT1(4, 4)

	// Set a flag
	t1.setFlag(1, 1, T1Sig)

	// Check it's set
	if !t1.hasFlag(1, 1, T1Sig) {
		t.Error("expected T1Sig to be set")
	}

	// Check other flags are not set
	if t1.hasFlag(1, 1, T1Visit) {
		t.Error("expected T1Visit to not be set")
	}

	// Check other positions are not set
	if t1.hasFlag(0, 0, T1Sig) {
		t.Error("expected (0,0) T1Sig to not be set")
	}
}

func TestT1_ClearFlag(t *testing.T) {
	t1 := NewT1(4, 4)

	t1.setFlag(1, 1, T1Sig)
	t1.setFlag(1, 1, T1Visit)

	t1.clearFlag(1, 1, T1Sig)

	if t1.hasFlag(1, 1, T1Sig) {
		t.Error("expected T1Sig to be cleared")
	}
	if !t1.hasFlag(1, 1, T1Visit) {
		t.Error("expected T1Visit to still be set")
	}
}

func TestT1_GetZCContext(t *testing.T) {
	t1 := NewT1(4, 4)

	// With no significant neighbors, should return base context
	ctx := t1.getZCContext(1, 1, BandLL)
	if ctx != CtxZC0 {
		t.Errorf("expected CtxZC0 with no neighbors, got %d", ctx)
	}

	// Set a horizontal neighbor as significant
	t1.setFlag(0, 1, T1Sig)
	t1.updateNeighborFlags(0, 1)

	ctx = t1.getZCContext(1, 1, BandLL)
	// With one horizontal neighbor, should return a higher context
	if ctx == CtxZC0 {
		t.Error("expected non-zero context with significant neighbor")
	}
}

func TestT1_GetSCContext(t *testing.T) {
	t1 := NewT1(4, 4)

	// With no significant neighbors
	ctx, pred := t1.getSCContext(1, 1)
	if ctx < CtxSC0 || ctx > CtxSC4 {
		t.Errorf("context out of range: %d", ctx)
	}
	if pred != 0 && pred != 1 {
		t.Errorf("prediction out of range: %d", pred)
	}
}

func TestT1_GetMRContext(t *testing.T) {
	t1 := NewT1(4, 4)

	// First refinement, no neighbors
	ctx := t1.getMRContext(1, 1)
	if ctx != CtxMag0 {
		t.Errorf("expected CtxMag0, got %d", ctx)
	}

	// After refinement
	t1.setFlag(1, 1, T1Refine)
	ctx = t1.getMRContext(1, 1)
	if ctx != CtxMag2 {
		t.Errorf("expected CtxMag2, got %d", ctx)
	}
}

func TestT1_Reset(t *testing.T) {
	t1 := NewT1(4, 4)

	// Set some data
	for i := range t1.data {
		t1.data[i] = int32(i)
	}
	t1.setFlag(1, 1, T1Sig)

	// Reset
	t1.Reset()

	// Check data is cleared
	for i, v := range t1.data {
		if v != 0 {
			t.Errorf("data[%d] not cleared: %d", i, v)
		}
	}

	// Check flags are cleared
	if t1.hasFlag(1, 1, T1Sig) {
		t.Error("expected flags to be cleared")
	}
}

func BenchmarkT1_Encode(b *testing.B) {
	data := make([]int32, 64)
	for i := range data {
		data[i] = int32(i * 4)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t1 := NewT1(8, 8)
		t1.SetData(data)
		t1.Encode(BandLL)
	}
}

func BenchmarkT1_Decode(b *testing.B) {
	// First encode some data
	data := make([]int32, 64)
	for i := range data {
		data[i] = int32(i * 4)
	}
	t1 := NewT1(8, 8)
	t1.SetData(data)
	encoded := t1.Encode(BandLL)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t1 := NewT1(8, 8)
		t1.Decode(encoded, 10, BandLL)
	}
}
