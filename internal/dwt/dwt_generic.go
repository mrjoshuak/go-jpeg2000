//go:build !amd64 && !arm64

package dwt

// Forward53Fast falls back to the standard implementation on non-SIMD platforms.
func Forward53Fast(data []int32, length int) {
	Forward53(data, length)
}

// clearInt32SliceFast uses a simple loop on non-SIMD platforms.
func clearInt32SliceFast(data []int32) {
	for i := range data {
		data[i] = 0
	}
}
