//go:build amd64

package entropy

// clearFlags_avx fast zeroes a T1Flags slice using AVX.
//
//go:noescape
func clearFlags_avx(flags []T1Flags)

// clearFlagsFast uses AVX to zero flags efficiently.
func clearFlagsFast(flags []T1Flags) {
	if len(flags) == 0 {
		return
	}
	clearFlags_avx(flags)
}

// useSIMD indicates SIMD is available for entropy coding
const useSIMD = true
