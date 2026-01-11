//go:build !amd64 && !arm64

package entropy

// clearFlagsFast uses a simple loop on non-SIMD platforms.
func clearFlagsFast(flags []T1Flags) {
	for i := range flags {
		flags[i] = 0
	}
}

// useSIMD indicates SIMD is not available for entropy coding
const useSIMD = false
