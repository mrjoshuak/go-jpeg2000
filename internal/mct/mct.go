// Package mct implements multi-component transforms for JPEG 2000.
//
// JPEG 2000 supports two types of component transforms:
// - ICT (Irreversible Color Transform): RGB to YCbCr for lossy compression
// - RCT (Reversible Color Transform): RGB to YCrCb for lossless compression
package mct

import "math"

// Forward transforms

// ForwardICT applies the irreversible color transform (RGB to YCbCr).
// This is used for lossy compression.
func ForwardICT(r, g, b []float64) {
	for i := range r {
		y := 0.299*r[i] + 0.587*g[i] + 0.114*b[i]
		cb := -0.16875*r[i] - 0.33126*g[i] + 0.5*b[i]
		cr := 0.5*r[i] - 0.41869*g[i] - 0.08131*b[i]

		r[i] = y
		g[i] = cb
		b[i] = cr
	}
}

// ForwardRCT applies the reversible color transform.
// This is used for lossless compression.
func ForwardRCT(r, g, b []int32) {
	for i := range r {
		y := (r[i] + 2*g[i] + b[i]) >> 2
		u := b[i] - g[i]
		v := r[i] - g[i]

		r[i] = y
		g[i] = u
		b[i] = v
	}
}

// Inverse transforms

// InverseICT applies the inverse irreversible color transform (YCbCr to RGB).
func InverseICT(y, cb, cr []float64) {
	for i := range y {
		r := y[i] + 1.402*cr[i]
		g := y[i] - 0.34413*cb[i] - 0.71414*cr[i]
		b := y[i] + 1.772*cb[i]

		y[i] = r
		cb[i] = g
		cr[i] = b
	}
}

// InverseRCT applies the inverse reversible color transform.
func InverseRCT(y, u, v []int32) {
	for i := range y {
		g := y[i] - ((u[i] + v[i]) >> 2)
		r := v[i] + g
		b := u[i] + g

		y[i] = r
		u[i] = g
		v[i] = b
	}
}

// Clamp functions

// ClampFloat64 clamps a float64 value to the given range.
func ClampFloat64(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ClampInt32 clamps an int32 value to the given range.
func ClampInt32(v, min, max int32) int32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// DC level shift functions

// DCLevelShiftForward applies DC level shift before encoding.
// For unsigned data: subtract 2^(precision-1)
func DCLevelShiftForward(data []int32, precision int) {
	shift := int32(1) << (precision - 1)
	for i := range data {
		data[i] -= shift
	}
}

// DCLevelShiftForwardFloat applies DC level shift for float data.
func DCLevelShiftForwardFloat(data []float64, precision int) {
	shift := float64(int32(1) << (precision - 1))
	for i := range data {
		data[i] -= shift
	}
}

// DCLevelShiftInverse applies inverse DC level shift after decoding.
// For unsigned data: add 2^(precision-1)
func DCLevelShiftInverse(data []int32, precision int) {
	shift := int32(1) << (precision - 1)
	for i := range data {
		data[i] += shift
	}
}

// DCLevelShiftInverseFloat applies inverse DC level shift for float data.
func DCLevelShiftInverseFloat(data []float64, precision int) {
	shift := float64(int32(1) << (precision - 1))
	for i := range data {
		data[i] += shift
	}
}

// Utility functions for component transforms

// ShouldApplyMCT determines if MCT should be applied based on
// the number of components and coding parameters.
func ShouldApplyMCT(numComponents int, mctEnabled bool) bool {
	return numComponents >= 3 && mctEnabled
}

// ConvertFloat64ToInt32 converts float data to int32 with rounding.
func ConvertFloat64ToInt32(src []float64, dst []int32) {
	for i, v := range src {
		if v >= 0 {
			dst[i] = int32(v + 0.5)
		} else {
			dst[i] = int32(v - 0.5)
		}
	}
}

// ConvertInt32ToFloat64 converts int32 data to float64.
func ConvertInt32ToFloat64(src []int32, dst []float64) {
	for i, v := range src {
		dst[i] = float64(v)
	}
}

// ApplyPrecisionClamp clamps values to valid range for the given precision.
func ApplyPrecisionClamp(data []int32, precision int, signed bool) {
	var minVal, maxVal int32
	if signed {
		minVal = -(1 << (precision - 1))
		maxVal = (1 << (precision - 1)) - 1
	} else {
		minVal = 0
		maxVal = (1 << precision) - 1
	}

	for i := range data {
		data[i] = ClampInt32(data[i], minVal, maxVal)
	}
}

// ApplyPrecisionClampFloat clamps float values for the given precision.
func ApplyPrecisionClampFloat(data []float64, precision int, signed bool) {
	var minVal, maxVal float64
	if signed {
		minVal = float64(-(int64(1) << (precision - 1)))
		maxVal = float64((int64(1) << (precision - 1)) - 1)
	} else {
		minVal = 0
		maxVal = float64((int64(1) << precision) - 1)
	}

	for i := range data {
		data[i] = ClampFloat64(data[i], minVal, maxVal)
	}
}

// Custom MCT matrix transforms

// CustomMCT represents a custom multi-component transform matrix.
type CustomMCT struct {
	// Forward transform matrix (row-major)
	Forward []float64
	// Inverse transform matrix (row-major)
	Inverse []float64
	// Number of components
	NumComponents int
}

// NewCustomMCT creates a custom MCT with the given forward matrix.
// The inverse is computed automatically.
func NewCustomMCT(forward []float64, numComponents int) *CustomMCT {
	mct := &CustomMCT{
		Forward:       forward,
		NumComponents: numComponents,
	}
	mct.Inverse = mct.computeInverse()
	return mct
}

// computeInverse computes the inverse matrix.
func (m *CustomMCT) computeInverse() []float64 {
	n := m.NumComponents
	inv := make([]float64, n*n)

	// For 3x3, use explicit formula
	if n == 3 {
		a := m.Forward
		det := a[0]*(a[4]*a[8]-a[5]*a[7]) -
			a[1]*(a[3]*a[8]-a[5]*a[6]) +
			a[2]*(a[3]*a[7]-a[4]*a[6])

		if math.Abs(det) < 1e-10 {
			// Singular matrix, return identity
			for i := 0; i < n; i++ {
				inv[i*n+i] = 1
			}
			return inv
		}

		invDet := 1.0 / det
		inv[0] = (a[4]*a[8] - a[5]*a[7]) * invDet
		inv[1] = (a[2]*a[7] - a[1]*a[8]) * invDet
		inv[2] = (a[1]*a[5] - a[2]*a[4]) * invDet
		inv[3] = (a[5]*a[6] - a[3]*a[8]) * invDet
		inv[4] = (a[0]*a[8] - a[2]*a[6]) * invDet
		inv[5] = (a[2]*a[3] - a[0]*a[5]) * invDet
		inv[6] = (a[3]*a[7] - a[4]*a[6]) * invDet
		inv[7] = (a[1]*a[6] - a[0]*a[7]) * invDet
		inv[8] = (a[0]*a[4] - a[1]*a[3]) * invDet
	} else {
		// For larger matrices, use Gauss-Jordan elimination
		// (simplified implementation)
		aug := make([]float64, n*2*n)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				aug[i*2*n+j] = m.Forward[i*n+j]
				if i == j {
					aug[i*2*n+n+j] = 1
				}
			}
		}

		// Forward elimination
		for i := 0; i < n; i++ {
			// Find pivot
			maxRow := i
			for k := i + 1; k < n; k++ {
				if math.Abs(aug[k*2*n+i]) > math.Abs(aug[maxRow*2*n+i]) {
					maxRow = k
				}
			}
			// Swap rows
			for k := 0; k < 2*n; k++ {
				aug[i*2*n+k], aug[maxRow*2*n+k] = aug[maxRow*2*n+k], aug[i*2*n+k]
			}

			// Scale pivot row
			pivot := aug[i*2*n+i]
			if math.Abs(pivot) < 1e-10 {
				continue
			}
			for k := 0; k < 2*n; k++ {
				aug[i*2*n+k] /= pivot
			}

			// Eliminate column
			for k := 0; k < n; k++ {
				if k != i {
					factor := aug[k*2*n+i]
					for j := 0; j < 2*n; j++ {
						aug[k*2*n+j] -= factor * aug[i*2*n+j]
					}
				}
			}
		}

		// Extract inverse
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				inv[i*n+j] = aug[i*2*n+n+j]
			}
		}
	}

	return inv
}

// Apply applies the forward transform to the given component data.
func (m *CustomMCT) Apply(components [][]float64) {
	if len(components) != m.NumComponents {
		return
	}

	n := m.NumComponents
	numSamples := len(components[0])
	temp := make([]float64, n)

	for s := 0; s < numSamples; s++ {
		// Read input samples
		for i := 0; i < n; i++ {
			temp[i] = components[i][s]
		}
		// Apply matrix
		for i := 0; i < n; i++ {
			sum := 0.0
			for j := 0; j < n; j++ {
				sum += m.Forward[i*n+j] * temp[j]
			}
			components[i][s] = sum
		}
	}
}

// ApplyInverse applies the inverse transform.
func (m *CustomMCT) ApplyInverse(components [][]float64) {
	if len(components) != m.NumComponents {
		return
	}

	n := m.NumComponents
	numSamples := len(components[0])
	temp := make([]float64, n)

	for s := 0; s < numSamples; s++ {
		for i := 0; i < n; i++ {
			temp[i] = components[i][s]
		}
		for i := 0; i < n; i++ {
			sum := 0.0
			for j := 0; j < n; j++ {
				sum += m.Inverse[i*n+j] * temp[j]
			}
			components[i][s] = sum
		}
	}
}
