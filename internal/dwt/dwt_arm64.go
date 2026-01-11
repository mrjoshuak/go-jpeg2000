//go:build arm64

package dwt

// liftStep1_53_neon performs the first lifting step using NEON.
// data[i] -= (data[i-1] + data[i+1]) >> 1 for odd indices
//
//go:noescape
func liftStep1_53_neon(data []int32, length int)

// liftStep2_53_neon performs the second lifting step using NEON.
// data[i] += (data[i-1] + data[i+1] + 2) >> 2 for even indices
//
//go:noescape
func liftStep2_53_neon(data []int32, length int)

// clearInt32Slice_neon fast zeroes a slice using NEON.
//
//go:noescape
func clearInt32Slice_neon(data []int32)

// useSIMD indicates SIMD is available on this platform
const useSIMD = true

// Forward53Fast performs optimized forward 5-3 transform using NEON.
func Forward53Fast(data []int32, length int) {
	if length < 2 {
		return
	}

	// Use NEON-accelerated lifting steps
	liftStep1_53_neon(data, length)
	liftStep2_53_neon(data, length)

	// Rearrange coefficients
	deinterleave(data, length)
}

// clearInt32SliceFast uses NEON to zero a slice efficiently.
func clearInt32SliceFast(data []int32) {
	if len(data) == 0 {
		return
	}
	clearInt32Slice_neon(data)
}
