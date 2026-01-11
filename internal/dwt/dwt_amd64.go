//go:build amd64

package dwt

// liftStep1_53_avx performs the first lifting step using AVX.
// data[i] -= (data[i-1] + data[i+1]) >> 1 for odd indices
//
//go:noescape
func liftStep1_53_avx(data []int32, length int)

// liftStep2_53_avx performs the second lifting step using AVX.
// data[i] += (data[i-1] + data[i+1] + 2) >> 2 for even indices
//
//go:noescape
func liftStep2_53_avx(data []int32, length int)

// clearInt32Slice_avx fast zeroes a slice using AVX.
//
//go:noescape
func clearInt32Slice_avx(data []int32)

// useSIMD indicates SIMD is available on this platform
const useSIMD = true

// Forward53Fast performs optimized forward 5-3 transform using AVX.
func Forward53Fast(data []int32, length int) {
	if length < 2 {
		return
	}

	// Use AVX-accelerated lifting steps
	liftStep1_53_avx(data, length)
	liftStep2_53_avx(data, length)

	// Rearrange coefficients
	deinterleave(data, length)
}

// clearInt32SliceFast uses AVX to zero a slice efficiently.
func clearInt32SliceFast(data []int32) {
	if len(data) == 0 {
		return
	}
	clearInt32Slice_avx(data)
}
