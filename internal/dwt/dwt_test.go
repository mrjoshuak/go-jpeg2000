package dwt

import (
	"math"
	"testing"
)

func TestForward53_Inverse53_Roundtrip(t *testing.T) {
	tests := []struct {
		name string
		data []int32
	}{
		{"single", []int32{42}},
		{"two", []int32{10, 20}},
		{"four", []int32{1, 2, 3, 4}},
		{"eight", []int32{1, 2, 3, 4, 5, 6, 7, 8}},
		{"odd", []int32{1, 2, 3, 4, 5, 6, 7}},
		{"ramp", []int32{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}},
		{"constant", []int32{50, 50, 50, 50, 50, 50, 50, 50}},
		{"alternating", []int32{-10, 10, -10, 10, -10, 10, -10, 10}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy
			original := make([]int32, len(tt.data))
			copy(original, tt.data)

			data := make([]int32, len(tt.data))
			copy(data, tt.data)

			// Forward transform
			Forward53(data, len(data))

			// Inverse transform
			Inverse53(data, len(data))

			// Check roundtrip
			for i := range original {
				if data[i] != original[i] {
					t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
				}
			}
		})
	}
}

func TestForward97_Inverse97_Roundtrip(t *testing.T) {
	tests := []struct {
		name string
		data []float64
	}{
		{"single", []float64{42.0}},
		{"two", []float64{10.0, 20.0}},
		{"four", []float64{1.0, 2.0, 3.0, 4.0}},
		{"eight", []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0}},
		{"ramp", []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := make([]float64, len(tt.data))
			copy(original, tt.data)

			data := make([]float64, len(tt.data))
			copy(data, tt.data)

			Forward97(data, len(data))
			Inverse97(data, len(data))

			// Check roundtrip with tolerance
			for i := range original {
				if math.Abs(data[i]-original[i]) > 1e-10 {
					t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
				}
			}
		})
	}
}

func TestForward2D53_Inverse2D53_Roundtrip(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"2x2", 2, 2},
		{"4x4", 4, 4},
		{"8x8", 8, 8},
		{"16x16", 16, 16},
		{"8x4", 8, 4},
		{"4x8", 4, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.width * tt.height
			original := make([]int32, size)
			for i := range original {
				original[i] = int32(i * 10)
			}

			data := make([]int32, size)
			copy(data, original)

			Forward2D53(data, tt.width, tt.height)
			Inverse2D53(data, tt.width, tt.height)

			for i := range original {
				if data[i] != original[i] {
					t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
				}
			}
		})
	}
}

func TestForward2D97_Inverse2D97_Roundtrip(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"4x4", 4, 4},
		{"8x8", 8, 8},
		{"16x16", 16, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.width * tt.height
			original := make([]float64, size)
			for i := range original {
				original[i] = float64(i * 10)
			}

			data := make([]float64, size)
			copy(data, original)

			Forward2D97(data, tt.width, tt.height)
			Inverse2D97(data, tt.width, tt.height)

			for i := range original {
				if math.Abs(data[i]-original[i]) > 1e-9 {
					t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
				}
			}
		})
	}
}

func TestMultiLevel53_Roundtrip(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		levels int
	}{
		{"8x8_1level", 8, 8, 1},
		{"8x8_2levels", 8, 8, 2},
		{"16x16_3levels", 16, 16, 3},
		{"32x32_4levels", 32, 32, 4},
		{"64x64_5levels", 64, 64, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.width * tt.height
			original := make([]int32, size)
			for i := range original {
				original[i] = int32(i % 256)
			}

			data := make([]int32, size)
			copy(data, original)

			DecomposeMultiLevel53(data, tt.width, tt.height, tt.levels)
			ReconstructMultiLevel53(data, tt.width, tt.height, tt.levels)

			for i := range original {
				if data[i] != original[i] {
					t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
				}
			}
		})
	}
}

func TestDeinterleave_Interleave_Roundtrip(t *testing.T) {
	data := []int32{0, 1, 2, 3, 4, 5, 6, 7}
	original := make([]int32, len(data))
	copy(original, data)

	deinterleave(data, len(data))
	interleave(data, len(data))

	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
		}
	}
}

func TestQuantize_Dequantize(t *testing.T) {
	data := []float64{0.0, 1.5, -2.3, 100.7, -50.2}
	stepSize := 0.5

	quantized := Quantize(data, stepSize)
	dequantized := Dequantize(quantized, stepSize)

	for i := range data {
		// Check quantized values are integers of original/stepSize
		expected := int32(math.Round(data[i] / stepSize))
		if quantized[i] != expected {
			t.Errorf("quantize position %d: got %d, want %d", i, quantized[i], expected)
		}

		// Dequantized should be quantized * stepSize
		if dequantized[i] != float64(quantized[i])*stepSize {
			t.Errorf("dequantize position %d: got %v, want %v", i, dequantized[i], float64(quantized[i])*stepSize)
		}
	}
}

func TestCalculateSubbands(t *testing.T) {
	ll, hl, lh, hh := CalculateSubbands(16, 16, 0)

	if ll.X1-ll.X0 != 8 || ll.Y1-ll.Y0 != 8 {
		t.Errorf("LL band size wrong: got %dx%d, want 8x8", ll.X1-ll.X0, ll.Y1-ll.Y0)
	}
	if hl.X1-hl.X0 != 8 || hl.Y1-hl.Y0 != 8 {
		t.Errorf("HL band size wrong: got %dx%d, want 8x8", hl.X1-hl.X0, hl.Y1-hl.Y0)
	}
	if lh.X1-lh.X0 != 8 || lh.Y1-lh.Y0 != 8 {
		t.Errorf("LH band size wrong: got %dx%d, want 8x8", lh.X1-lh.X0, lh.Y1-lh.Y0)
	}
	if hh.X1-hh.X0 != 8 || hh.Y1-hh.Y0 != 8 {
		t.Errorf("HH band size wrong: got %dx%d, want 8x8", hh.X1-hh.X0, hh.Y1-hh.Y0)
	}
}

func BenchmarkForward53(b *testing.B) {
	data := make([]int32, 1024)
	for i := range data {
		data[i] = int32(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Forward53(data, len(data))
	}
}

func BenchmarkForward2D53(b *testing.B) {
	data := make([]int32, 64*64)
	for i := range data {
		data[i] = int32(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Forward2D53(data, 64, 64)
	}
}

func BenchmarkForward97(b *testing.B) {
	data := make([]float64, 1024)
	for i := range data {
		data[i] = float64(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Forward97(data, len(data))
	}
}

func TestMultiLevel97_Roundtrip(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		levels int
	}{
		{"8x8_1level", 8, 8, 1},
		{"8x8_2levels", 8, 8, 2},
		{"16x16_3levels", 16, 16, 3},
		{"32x32_4levels", 32, 32, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.width * tt.height
			original := make([]float64, size)
			for i := range original {
				original[i] = float64(i % 256)
			}

			data := make([]float64, size)
			copy(data, original)

			DecomposeMultiLevel97(data, tt.width, tt.height, tt.levels)
			ReconstructMultiLevel97(data, tt.width, tt.height, tt.levels)

			for i := range original {
				if math.Abs(data[i]-original[i]) > 1e-9 {
					t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
				}
			}
		})
	}
}

func TestDeinterleave_SmallLength(t *testing.T) {
	// Test edge case where length < 2
	data := []int32{42}
	original := make([]int32, len(data))
	copy(original, data)

	deinterleave(data, len(data))

	// Data should remain unchanged
	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
		}
	}

	// Test with length 0
	emptyData := []int32{}
	deinterleave(emptyData, 0)
}

func TestInterleave_SmallLength(t *testing.T) {
	// Test edge case where length < 2
	data := []int32{42}
	original := make([]int32, len(data))
	copy(original, data)

	interleave(data, len(data))

	// Data should remain unchanged
	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
		}
	}

	// Test with length 0
	emptyData := []int32{}
	interleave(emptyData, 0)
}

func TestDeinterleaveFloat_SmallLength(t *testing.T) {
	// Test edge case where length < 2
	data := []float64{42.0}
	original := make([]float64, len(data))
	copy(original, data)

	deinterleaveFloat(data, len(data))

	// Data should remain unchanged
	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
		}
	}

	// Test with length 0
	emptyData := []float64{}
	deinterleaveFloat(emptyData, 0)
}

func TestInterleaveFloat_SmallLength(t *testing.T) {
	// Test edge case where length < 2
	data := []float64{42.0}
	original := make([]float64, len(data))
	copy(original, data)

	interleaveFloat(data, len(data))

	// Data should remain unchanged
	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
		}
	}

	// Test with length 0
	emptyData := []float64{}
	interleaveFloat(emptyData, 0)
}

func TestForward53Fast(t *testing.T) {
	tests := []struct {
		name string
		data []int32
	}{
		{"single", []int32{42}},
		{"two", []int32{10, 20}},
		{"four", []int32{1, 2, 3, 4}},
		{"eight", []int32{1, 2, 3, 4, 5, 6, 7, 8}},
		{"sixteen", []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that Forward53Fast produces consistent results
			data := make([]int32, len(tt.data))
			copy(data, tt.data)

			Forward53Fast(data, len(data))

			// Verify it matches standard Forward53
			standard := make([]int32, len(tt.data))
			copy(standard, tt.data)
			Forward53(standard, len(standard))

			for i := range data {
				if data[i] != standard[i] {
					t.Errorf("position %d: Forward53Fast got %d, Forward53 got %d", i, data[i], standard[i])
				}
			}
		})
	}
}

func TestClearInt32SliceFast(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"empty", 0},
		{"single", 1},
		{"small", 8},
		{"medium", 64},
		{"large", 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]int32, tt.size)
			for i := range data {
				data[i] = int32(i + 1)
			}

			clearInt32SliceFast(data)

			for i := range data {
				if data[i] != 0 {
					t.Errorf("position %d: got %d, want 0", i, data[i])
				}
			}
		})
	}
}

func TestLargeBufferPool(t *testing.T) {
	// Test buffer pool with size larger than initial 4096
	// This exercises the buffer reallocation path
	size := 8192
	original := make([]int32, size)
	for i := range original {
		original[i] = int32(i)
	}

	data := make([]int32, size)
	copy(data, original)

	// This will trigger getIntBuf with n > 4096
	Forward53(data, size)
	Inverse53(data, size)

	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
		}
	}

	// Test float buffer pool similarly
	floatOriginal := make([]float64, size)
	for i := range floatOriginal {
		floatOriginal[i] = float64(i)
	}

	floatData := make([]float64, size)
	copy(floatData, floatOriginal)

	// This will trigger getFloatBuf with n > 4096
	Forward97(floatData, size)
	Inverse97(floatData, size)

	for i := range floatOriginal {
		if math.Abs(floatData[i]-floatOriginal[i]) > 1e-9 {
			t.Errorf("position %d: got %v, want %v", i, floatData[i], floatOriginal[i])
		}
	}
}
