package mct

import (
	"math"
	"testing"
)

func TestForwardRCT_InverseRCT_Roundtrip(t *testing.T) {
	r := []int32{100, 150, 200, 50}
	g := []int32{110, 140, 190, 60}
	b := []int32{120, 130, 180, 70}

	// Make copies
	origR := make([]int32, len(r))
	origG := make([]int32, len(g))
	origB := make([]int32, len(b))
	copy(origR, r)
	copy(origG, g)
	copy(origB, b)

	// Forward transform
	ForwardRCT(r, g, b)

	// Inverse transform
	InverseRCT(r, g, b)

	// Check roundtrip
	for i := range origR {
		if r[i] != origR[i] {
			t.Errorf("R[%d]: got %d, want %d", i, r[i], origR[i])
		}
		if g[i] != origG[i] {
			t.Errorf("G[%d]: got %d, want %d", i, g[i], origG[i])
		}
		if b[i] != origB[i] {
			t.Errorf("B[%d]: got %d, want %d", i, b[i], origB[i])
		}
	}
}

func TestForwardICT_InverseICT_Roundtrip(t *testing.T) {
	r := []float64{100.0, 150.0, 200.0, 50.0}
	g := []float64{110.0, 140.0, 190.0, 60.0}
	b := []float64{120.0, 130.0, 180.0, 70.0}

	origR := make([]float64, len(r))
	origG := make([]float64, len(g))
	origB := make([]float64, len(b))
	copy(origR, r)
	copy(origG, g)
	copy(origB, b)

	ForwardICT(r, g, b)
	InverseICT(r, g, b)

	// ICT uses floating-point coefficients, so allow for some numerical error
	const tolerance = 1e-2
	for i := range origR {
		if math.Abs(r[i]-origR[i]) > tolerance {
			t.Errorf("R[%d]: got %v, want %v", i, r[i], origR[i])
		}
		if math.Abs(g[i]-origG[i]) > tolerance {
			t.Errorf("G[%d]: got %v, want %v", i, g[i], origG[i])
		}
		if math.Abs(b[i]-origB[i]) > tolerance {
			t.Errorf("B[%d]: got %v, want %v", i, b[i], origB[i])
		}
	}
}

func TestDCLevelShiftForward_Inverse_Roundtrip(t *testing.T) {
	data := []int32{0, 64, 128, 192, 255}
	original := make([]int32, len(data))
	copy(original, data)

	DCLevelShiftForward(data, 8)
	DCLevelShiftInverse(data, 8)

	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], original[i])
		}
	}
}

func TestDCLevelShiftForwardFloat_InverseFloat_Roundtrip(t *testing.T) {
	data := []float64{0.0, 64.0, 128.0, 192.0, 255.0}
	original := make([]float64, len(data))
	copy(original, data)

	DCLevelShiftForwardFloat(data, 8)
	DCLevelShiftInverseFloat(data, 8)

	for i := range original {
		if data[i] != original[i] {
			t.Errorf("position %d: got %v, want %v", i, data[i], original[i])
		}
	}
}

func TestClampInt32(t *testing.T) {
	tests := []struct {
		v, min, max, want int32
	}{
		{50, 0, 100, 50},
		{-10, 0, 100, 0},
		{200, 0, 100, 100},
		{0, 0, 100, 0},
		{100, 0, 100, 100},
	}

	for _, tt := range tests {
		got := ClampInt32(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("ClampInt32(%d, %d, %d) = %d, want %d", tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestClampFloat64(t *testing.T) {
	tests := []struct {
		v, min, max, want float64
	}{
		{50.0, 0.0, 100.0, 50.0},
		{-10.0, 0.0, 100.0, 0.0},
		{200.0, 0.0, 100.0, 100.0},
	}

	for _, tt := range tests {
		got := ClampFloat64(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("ClampFloat64(%v, %v, %v) = %v, want %v", tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestShouldApplyMCT(t *testing.T) {
	tests := []struct {
		numComponents int
		mctEnabled    bool
		want          bool
	}{
		{3, true, true},
		{3, false, false},
		{1, true, false},
		{4, true, true},
	}

	for _, tt := range tests {
		got := ShouldApplyMCT(tt.numComponents, tt.mctEnabled)
		if got != tt.want {
			t.Errorf("ShouldApplyMCT(%d, %v) = %v, want %v", tt.numComponents, tt.mctEnabled, got, tt.want)
		}
	}
}

func TestConvertFloat64ToInt32(t *testing.T) {
	src := []float64{0.4, 0.5, 0.6, -0.4, -0.5, -0.6}
	dst := make([]int32, len(src))

	ConvertFloat64ToInt32(src, dst)

	expected := []int32{0, 1, 1, 0, -1, -1}
	for i := range expected {
		if dst[i] != expected[i] {
			t.Errorf("position %d: got %d, want %d", i, dst[i], expected[i])
		}
	}
}

func TestConvertInt32ToFloat64(t *testing.T) {
	src := []int32{0, 1, -1, 100, -100}
	dst := make([]float64, len(src))

	ConvertInt32ToFloat64(src, dst)

	for i := range src {
		if dst[i] != float64(src[i]) {
			t.Errorf("position %d: got %v, want %v", i, dst[i], float64(src[i]))
		}
	}
}

func TestApplyPrecisionClamp(t *testing.T) {
	// Unsigned 8-bit
	data := []int32{-10, 0, 128, 255, 300}
	ApplyPrecisionClamp(data, 8, false)
	expected := []int32{0, 0, 128, 255, 255}
	for i := range expected {
		if data[i] != expected[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], expected[i])
		}
	}

	// Signed 8-bit
	data = []int32{-200, -128, 0, 127, 200}
	ApplyPrecisionClamp(data, 8, true)
	expected = []int32{-128, -128, 0, 127, 127}
	for i := range expected {
		if data[i] != expected[i] {
			t.Errorf("position %d: got %d, want %d", i, data[i], expected[i])
		}
	}
}

func TestCustomMCT_Apply_ApplyInverse_Roundtrip(t *testing.T) {
	// Identity matrix
	forward := []float64{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}

	mct := NewCustomMCT(forward, 3)

	components := [][]float64{
		{100, 200, 150},
		{110, 190, 140},
		{120, 180, 130},
	}

	original := make([][]float64, 3)
	for i := range original {
		original[i] = make([]float64, len(components[i]))
		copy(original[i], components[i])
	}

	mct.Apply(components)
	mct.ApplyInverse(components)

	for c := range original {
		for i := range original[c] {
			if math.Abs(components[c][i]-original[c][i]) > 1e-9 {
				t.Errorf("component %d, position %d: got %v, want %v", c, i, components[c][i], original[c][i])
			}
		}
	}
}

func BenchmarkForwardRCT(b *testing.B) {
	size := 1024
	r := make([]int32, size)
	g := make([]int32, size)
	bl := make([]int32, size)
	for i := 0; i < size; i++ {
		r[i] = int32(i % 256)
		g[i] = int32((i + 85) % 256)
		bl[i] = int32((i + 170) % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForwardRCT(r, g, bl)
	}
}

func BenchmarkForwardICT(b *testing.B) {
	size := 1024
	r := make([]float64, size)
	g := make([]float64, size)
	bl := make([]float64, size)
	for i := 0; i < size; i++ {
		r[i] = float64(i % 256)
		g[i] = float64((i + 85) % 256)
		bl[i] = float64((i + 170) % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForwardICT(r, g, bl)
	}
}

// Additional tests for improved coverage

func TestApplyPrecisionClampFloat_Unsigned(t *testing.T) {
	// Unsigned 8-bit
	data := []float64{-10.0, 0.0, 128.0, 255.0, 300.0}
	ApplyPrecisionClampFloat(data, 8, false)
	expected := []float64{0.0, 0.0, 128.0, 255.0, 255.0}
	for i := range expected {
		if data[i] != expected[i] {
			t.Errorf("unsigned position %d: got %v, want %v", i, data[i], expected[i])
		}
	}
}

func TestApplyPrecisionClampFloat_Signed(t *testing.T) {
	// Signed 8-bit
	data := []float64{-200.0, -128.0, 0.0, 127.0, 200.0}
	ApplyPrecisionClampFloat(data, 8, true)
	expected := []float64{-128.0, -128.0, 0.0, 127.0, 127.0}
	for i := range expected {
		if data[i] != expected[i] {
			t.Errorf("signed position %d: got %v, want %v", i, data[i], expected[i])
		}
	}
}

func TestApplyPrecisionClampFloat_HighPrecision(t *testing.T) {
	// Test with 16-bit precision
	data := []float64{-100.0, 0.0, 32767.0, 65535.0, 70000.0}
	ApplyPrecisionClampFloat(data, 16, false)
	expected := []float64{0.0, 0.0, 32767.0, 65535.0, 65535.0}
	for i := range expected {
		if data[i] != expected[i] {
			t.Errorf("16-bit position %d: got %v, want %v", i, data[i], expected[i])
		}
	}
}

func TestCustomMCT_SingularMatrix(t *testing.T) {
	// Singular matrix (determinant = 0)
	// All rows are the same, so it's singular
	forward := []float64{
		1, 2, 3,
		1, 2, 3,
		1, 2, 3,
	}

	mct := NewCustomMCT(forward, 3)

	// With a singular matrix, computeInverse returns identity
	// Check that the inverse is identity
	expectedInverse := []float64{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}

	for i := range expectedInverse {
		if mct.Inverse[i] != expectedInverse[i] {
			t.Errorf("inverse[%d]: got %v, want %v", i, mct.Inverse[i], expectedInverse[i])
		}
	}
}

func TestCustomMCT_LargerMatrix_4x4(t *testing.T) {
	// 4x4 identity matrix to test Gauss-Jordan path
	forward := []float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}

	mct := NewCustomMCT(forward, 4)

	// Identity's inverse is identity
	for i := 0; i < 16; i++ {
		if math.Abs(mct.Inverse[i]-forward[i]) > 1e-9 {
			t.Errorf("4x4 identity inverse[%d]: got %v, want %v", i, mct.Inverse[i], forward[i])
		}
	}
}

func TestCustomMCT_LargerMatrix_4x4_NonIdentity(t *testing.T) {
	// Non-identity 4x4 matrix
	forward := []float64{
		2, 0, 0, 0,
		0, 3, 0, 0,
		0, 0, 4, 0,
		0, 0, 0, 5,
	}

	mct := NewCustomMCT(forward, 4)

	// Diagonal inverse
	expectedInverse := []float64{
		0.5, 0, 0, 0,
		0, 1.0 / 3.0, 0, 0,
		0, 0, 0.25, 0,
		0, 0, 0, 0.2,
	}

	for i := 0; i < 16; i++ {
		if math.Abs(mct.Inverse[i]-expectedInverse[i]) > 1e-9 {
			t.Errorf("4x4 diagonal inverse[%d]: got %v, want %v", i, mct.Inverse[i], expectedInverse[i])
		}
	}
}

func TestCustomMCT_LargerMatrix_Roundtrip(t *testing.T) {
	// 4x4 matrix with full values
	forward := []float64{
		1, 2, 0, 0,
		3, 4, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}

	mct := NewCustomMCT(forward, 4)

	components := [][]float64{
		{10, 20},
		{30, 40},
		{50, 60},
		{70, 80},
	}

	original := make([][]float64, 4)
	for i := range original {
		original[i] = make([]float64, 2)
		copy(original[i], components[i])
	}

	mct.Apply(components)
	mct.ApplyInverse(components)

	for c := range original {
		for i := range original[c] {
			if math.Abs(components[c][i]-original[c][i]) > 1e-6 {
				t.Errorf("4x4 roundtrip component %d, pos %d: got %v, want %v",
					c, i, components[c][i], original[c][i])
			}
		}
	}
}

func TestCustomMCT_Apply_WrongComponentCount(t *testing.T) {
	forward := []float64{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
	mct := NewCustomMCT(forward, 3)

	// Pass only 2 components when 3 are expected
	components := [][]float64{
		{100, 200},
		{110, 190},
	}

	original := make([][]float64, 2)
	for i := range original {
		original[i] = make([]float64, 2)
		copy(original[i], components[i])
	}

	// Should return early without modifying
	mct.Apply(components)

	for c := range original {
		for i := range original[c] {
			if components[c][i] != original[c][i] {
				t.Errorf("wrong component count: component %d, pos %d was modified", c, i)
			}
		}
	}
}

func TestCustomMCT_ApplyInverse_WrongComponentCount(t *testing.T) {
	forward := []float64{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
	mct := NewCustomMCT(forward, 3)

	// Pass only 2 components when 3 are expected
	components := [][]float64{
		{100, 200},
		{110, 190},
	}

	original := make([][]float64, 2)
	for i := range original {
		original[i] = make([]float64, 2)
		copy(original[i], components[i])
	}

	// Should return early without modifying
	mct.ApplyInverse(components)

	for c := range original {
		for i := range original[c] {
			if components[c][i] != original[c][i] {
				t.Errorf("wrong component count inverse: component %d, pos %d was modified", c, i)
			}
		}
	}
}

func TestCustomMCT_3x3_NonIdentity_Roundtrip(t *testing.T) {
	// Non-identity 3x3 matrix (simple scaling)
	forward := []float64{
		2, 0, 0,
		0, 3, 0,
		0, 0, 4,
	}

	mct := NewCustomMCT(forward, 3)

	components := [][]float64{
		{10, 20, 30},
		{40, 50, 60},
		{70, 80, 90},
	}

	original := make([][]float64, 3)
	for i := range original {
		original[i] = make([]float64, 3)
		copy(original[i], components[i])
	}

	mct.Apply(components)

	// Check forward transform result
	for i := range components[0] {
		if math.Abs(components[0][i]-original[0][i]*2) > 1e-9 {
			t.Errorf("forward transform R[%d]: got %v, want %v", i, components[0][i], original[0][i]*2)
		}
		if math.Abs(components[1][i]-original[1][i]*3) > 1e-9 {
			t.Errorf("forward transform G[%d]: got %v, want %v", i, components[1][i], original[1][i]*3)
		}
		if math.Abs(components[2][i]-original[2][i]*4) > 1e-9 {
			t.Errorf("forward transform B[%d]: got %v, want %v", i, components[2][i], original[2][i]*4)
		}
	}

	mct.ApplyInverse(components)

	for c := range original {
		for i := range original[c] {
			if math.Abs(components[c][i]-original[c][i]) > 1e-9 {
				t.Errorf("roundtrip component %d, pos %d: got %v, want %v",
					c, i, components[c][i], original[c][i])
			}
		}
	}
}

func TestForwardRCT_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		r    []int32
		g    []int32
		b    []int32
	}{
		{
			name: "zero values",
			r:    []int32{0, 0, 0},
			g:    []int32{0, 0, 0},
			b:    []int32{0, 0, 0},
		},
		{
			name: "max 8-bit values",
			r:    []int32{255, 255, 255},
			g:    []int32{255, 255, 255},
			b:    []int32{255, 255, 255},
		},
		{
			name: "negative values",
			r:    []int32{-128, -64, 0},
			g:    []int32{-128, -64, 0},
			b:    []int32{-128, -64, 0},
		},
		{
			name: "mixed positive negative",
			r:    []int32{-100, 0, 100},
			g:    []int32{50, -50, 150},
			b:    []int32{-50, 100, -100},
		},
		{
			name: "single element",
			r:    []int32{128},
			g:    []int32{128},
			b:    []int32{128},
		},
		{
			name: "empty slices",
			r:    []int32{},
			g:    []int32{},
			b:    []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origR := make([]int32, len(tt.r))
			origG := make([]int32, len(tt.g))
			origB := make([]int32, len(tt.b))
			copy(origR, tt.r)
			copy(origG, tt.g)
			copy(origB, tt.b)

			ForwardRCT(tt.r, tt.g, tt.b)
			InverseRCT(tt.r, tt.g, tt.b)

			for i := range origR {
				if tt.r[i] != origR[i] || tt.g[i] != origG[i] || tt.b[i] != origB[i] {
					t.Errorf("roundtrip failed at %d: got (%d,%d,%d), want (%d,%d,%d)",
						i, tt.r[i], tt.g[i], tt.b[i], origR[i], origG[i], origB[i])
				}
			}
		})
	}
}

func TestForwardICT_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		r    []float64
		g    []float64
		b    []float64
	}{
		{
			name: "zero values",
			r:    []float64{0, 0, 0},
			g:    []float64{0, 0, 0},
			b:    []float64{0, 0, 0},
		},
		{
			name: "max 8-bit values",
			r:    []float64{255, 255, 255},
			g:    []float64{255, 255, 255},
			b:    []float64{255, 255, 255},
		},
		{
			name: "negative values",
			r:    []float64{-128, -64, 0},
			g:    []float64{-128, -64, 0},
			b:    []float64{-128, -64, 0},
		},
		{
			name: "single element",
			r:    []float64{128.5},
			g:    []float64{128.5},
			b:    []float64{128.5},
		},
		{
			name: "empty slices",
			r:    []float64{},
			g:    []float64{},
			b:    []float64{},
		},
		{
			name: "very small values",
			r:    []float64{0.001, 0.002, 0.003},
			g:    []float64{0.001, 0.002, 0.003},
			b:    []float64{0.001, 0.002, 0.003},
		},
		{
			name: "very large values",
			r:    []float64{1e6, 1e7, 1e8},
			g:    []float64{1e6, 1e7, 1e8},
			b:    []float64{1e6, 1e7, 1e8},
		},
	}

	const tolerance = 1e-2
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origR := make([]float64, len(tt.r))
			origG := make([]float64, len(tt.g))
			origB := make([]float64, len(tt.b))
			copy(origR, tt.r)
			copy(origG, tt.g)
			copy(origB, tt.b)

			ForwardICT(tt.r, tt.g, tt.b)
			InverseICT(tt.r, tt.g, tt.b)

			for i := range origR {
				// Use relative tolerance for large values
				relTol := tolerance
				if math.Abs(origR[i]) > 1000 {
					relTol = tolerance * math.Abs(origR[i]) / 100
				}
				if math.Abs(tt.r[i]-origR[i]) > relTol ||
					math.Abs(tt.g[i]-origG[i]) > relTol ||
					math.Abs(tt.b[i]-origB[i]) > relTol {
					t.Errorf("roundtrip failed at %d: got (%v,%v,%v), want (%v,%v,%v)",
						i, tt.r[i], tt.g[i], tt.b[i], origR[i], origG[i], origB[i])
				}
			}
		})
	}
}

func TestDCLevelShift_DifferentPrecisions(t *testing.T) {
	precisions := []int{1, 4, 8, 10, 12, 16}

	for _, prec := range precisions {
		t.Run("int32_precision_"+string(rune('0'+prec%10)), func(t *testing.T) {
			maxVal := int32((1 << prec) - 1)
			data := []int32{0, maxVal / 2, maxVal}
			original := make([]int32, len(data))
			copy(original, data)

			DCLevelShiftForward(data, prec)
			DCLevelShiftInverse(data, prec)

			for i := range original {
				if data[i] != original[i] {
					t.Errorf("precision %d, pos %d: got %d, want %d", prec, i, data[i], original[i])
				}
			}
		})

		t.Run("float64_precision_"+string(rune('0'+prec%10)), func(t *testing.T) {
			maxVal := float64(int(1<<prec) - 1)
			data := []float64{0, maxVal / 2, maxVal}
			original := make([]float64, len(data))
			copy(original, data)

			DCLevelShiftForwardFloat(data, prec)
			DCLevelShiftInverseFloat(data, prec)

			for i := range original {
				if data[i] != original[i] {
					t.Errorf("precision %d, pos %d: got %v, want %v", prec, i, data[i], original[i])
				}
			}
		})
	}
}

func TestShouldApplyMCT_EdgeCases(t *testing.T) {
	tests := []struct {
		numComponents int
		mctEnabled    bool
		want          bool
	}{
		{0, true, false},
		{0, false, false},
		{2, true, false},
		{2, false, false},
		{3, true, true},
		{3, false, false},
		{10, true, true},
		{10, false, false},
		{100, true, true},
	}

	for _, tt := range tests {
		got := ShouldApplyMCT(tt.numComponents, tt.mctEnabled)
		if got != tt.want {
			t.Errorf("ShouldApplyMCT(%d, %v) = %v, want %v",
				tt.numComponents, tt.mctEnabled, got, tt.want)
		}
	}
}

func TestClampFloat64_EdgeCases(t *testing.T) {
	tests := []struct {
		v, min, max, want float64
	}{
		{0.0, 0.0, 100.0, 0.0},
		{100.0, 0.0, 100.0, 100.0},
		{math.Inf(1), 0.0, 100.0, 100.0},
		{math.Inf(-1), 0.0, 100.0, 0.0},
		{-0.0, 0.0, 100.0, 0.0}, // negative zero
		{50.5, 50.5, 50.5, 50.5}, // min == max == v
	}

	for _, tt := range tests {
		got := ClampFloat64(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("ClampFloat64(%v, %v, %v) = %v, want %v",
				tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestClampInt32_EdgeCases(t *testing.T) {
	tests := []struct {
		v, min, max, want int32
	}{
		{0, 0, 0, 0},
		{math.MaxInt32, 0, math.MaxInt32, math.MaxInt32},
		{math.MinInt32, math.MinInt32, 0, math.MinInt32},
		{50, 50, 50, 50}, // min == max == v
	}

	for _, tt := range tests {
		got := ClampInt32(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("ClampInt32(%d, %d, %d) = %d, want %d",
				tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestConvertFloat64ToInt32_EdgeCases(t *testing.T) {
	tests := []struct {
		src      []float64
		expected []int32
	}{
		{[]float64{}, []int32{}},
		{[]float64{0.0}, []int32{0}},
		{[]float64{0.49999}, []int32{0}},
		{[]float64{0.50001}, []int32{1}},
		{[]float64{-0.49999}, []int32{0}},
		{[]float64{-0.50001}, []int32{-1}},
		{[]float64{100.5, -100.5}, []int32{101, -101}},
	}

	for _, tt := range tests {
		dst := make([]int32, len(tt.src))
		ConvertFloat64ToInt32(tt.src, dst)
		for i := range tt.expected {
			if dst[i] != tt.expected[i] {
				t.Errorf("ConvertFloat64ToInt32 pos %d: got %d, want %d", i, dst[i], tt.expected[i])
			}
		}
	}
}

func TestConvertInt32ToFloat64_EdgeCases(t *testing.T) {
	tests := []struct {
		src      []int32
		expected []float64
	}{
		{[]int32{}, []float64{}},
		{[]int32{0}, []float64{0.0}},
		{[]int32{math.MaxInt32}, []float64{float64(math.MaxInt32)}},
		{[]int32{math.MinInt32}, []float64{float64(math.MinInt32)}},
	}

	for _, tt := range tests {
		dst := make([]float64, len(tt.src))
		ConvertInt32ToFloat64(tt.src, dst)
		for i := range tt.expected {
			if dst[i] != tt.expected[i] {
				t.Errorf("ConvertInt32ToFloat64 pos %d: got %v, want %v", i, dst[i], tt.expected[i])
			}
		}
	}
}

func TestCustomMCT_5x5_Matrix(t *testing.T) {
	// 5x5 diagonal matrix to further test Gauss-Jordan
	forward := make([]float64, 25)
	for i := 0; i < 5; i++ {
		forward[i*5+i] = float64(i + 1) // 1, 2, 3, 4, 5 on diagonal
	}

	mct := NewCustomMCT(forward, 5)

	// Check inverse is correct (1/1, 1/2, 1/3, 1/4, 1/5 on diagonal)
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			expected := 0.0
			if i == j {
				expected = 1.0 / float64(i+1)
			}
			if math.Abs(mct.Inverse[i*5+j]-expected) > 1e-9 {
				t.Errorf("5x5 inverse[%d][%d]: got %v, want %v", i, j, mct.Inverse[i*5+j], expected)
			}
		}
	}
}

func TestCustomMCT_4x4_WithPivoting(t *testing.T) {
	// Matrix that requires row swapping during Gauss-Jordan
	forward := []float64{
		0, 1, 0, 0, // First pivot is 0, needs swap
		1, 0, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}

	mct := NewCustomMCT(forward, 4)

	// This is a permutation matrix; its inverse is its transpose
	expectedInverse := []float64{
		0, 1, 0, 0,
		1, 0, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}

	for i := 0; i < 16; i++ {
		if math.Abs(mct.Inverse[i]-expectedInverse[i]) > 1e-9 {
			t.Errorf("4x4 pivot inverse[%d]: got %v, want %v", i, mct.Inverse[i], expectedInverse[i])
		}
	}
}

func TestCustomMCT_4x4_NearSingular(t *testing.T) {
	// Near-singular matrix with very small pivot
	forward := []float64{
		1e-15, 1, 0, 0,
		1, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}

	mct := NewCustomMCT(forward, 4)

	// Just verify it doesn't panic and produces some result
	// The exact values may vary due to numerical precision
	if len(mct.Inverse) != 16 {
		t.Errorf("expected inverse length 16, got %d", len(mct.Inverse))
	}
}

func TestCustomMCT_4x4_SingularGaussJordan(t *testing.T) {
	// 4x4 singular matrix to trigger the continue path in Gauss-Jordan
	// This matrix has a zero row after pivoting, making it singular
	forward := []float64{
		1, 2, 3, 4,
		2, 4, 6, 8, // Second row is 2x first row
		0, 0, 0, 0, // Zero row
		1, 1, 1, 1,
	}

	mct := NewCustomMCT(forward, 4)

	// Just verify it doesn't panic and produces some result
	if len(mct.Inverse) != 16 {
		t.Errorf("expected inverse length 16, got %d", len(mct.Inverse))
	}
}

func TestApplyPrecisionClamp_DifferentPrecisions(t *testing.T) {
	tests := []struct {
		precision int
		signed    bool
		input     []int32
		expected  []int32
	}{
		{1, false, []int32{-1, 0, 1, 2}, []int32{0, 0, 1, 1}},
		{1, true, []int32{-2, -1, 0, 1}, []int32{-1, -1, 0, 0}},
		{12, false, []int32{-1, 0, 2048, 4095, 5000}, []int32{0, 0, 2048, 4095, 4095}},
		{12, true, []int32{-3000, -2048, 0, 2047, 3000}, []int32{-2048, -2048, 0, 2047, 2047}},
	}

	for _, tt := range tests {
		data := make([]int32, len(tt.input))
		copy(data, tt.input)
		ApplyPrecisionClamp(data, tt.precision, tt.signed)
		for i := range tt.expected {
			if data[i] != tt.expected[i] {
				t.Errorf("precision %d, signed %v, pos %d: got %d, want %d",
					tt.precision, tt.signed, i, data[i], tt.expected[i])
			}
		}
	}
}
