package entropy

import (
	"testing"
)

func TestT1_Encode(t *testing.T) {
	// Test that Encode produces valid output
	data := make([]int32, 16*16)
	for i := range data {
		data[i] = int32(i % 128)
		if i%5 == 0 {
			data[i] = -data[i]
		}
	}

	t1 := NewT1(16, 16)
	t1.SetData(data)
	encoded := t1.Encode(BandLL)

	if len(encoded) == 0 {
		t.Error("Encode returned empty result")
	}
}

func TestT1_Encode_LargerBlock(t *testing.T) {
	// Test with 64x64 block
	data := make([]int32, 64*64)
	for i := range data {
		data[i] = int32(i % 256)
		if i%3 == 0 {
			data[i] = -data[i]
		}
	}

	t1 := NewT1(64, 64)
	t1.SetData(data)
	encoded := t1.Encode(BandLL)

	if len(encoded) == 0 {
		t.Error("Encode returned empty result for 64x64 block")
	}
}

func TestT1_Encode_AllBandTypes(t *testing.T) {
	data := make([]int32, 32*32)
	for i := range data {
		data[i] = int32(i % 256)
	}

	bandTypes := []int{BandLL, BandHL, BandLH, BandHH}
	for _, bt := range bandTypes {
		t1 := NewT1(32, 32)
		t1.SetData(data)
		encoded := t1.Encode(bt)
		if len(encoded) == 0 {
			t.Errorf("Encode returned empty result for band type %d", bt)
		}
	}
}

func BenchmarkT1_Encode64(b *testing.B) {
	data := make([]int32, 64*64)
	for i := range data {
		data[i] = int32(i % 256)
		if i%3 == 0 {
			data[i] = -data[i]
		}
	}

	t1 := NewT1(64, 64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t1.SetData(data)
		t1.Encode(BandLL)
	}
}
