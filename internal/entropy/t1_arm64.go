//go:build arm64

package entropy

// clearFlags_neon fast zeroes a T1Flags slice using NEON.
//
//go:noescape
func clearFlags_neon(flags []T1Flags)

// clearFlagsFast uses NEON to zero flags efficiently.
func clearFlagsFast(flags []T1Flags) {
	if len(flags) == 0 {
		return
	}
	clearFlags_neon(flags)
}

// useSIMD indicates SIMD is available for entropy coding
const useSIMD = true
