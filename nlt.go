package jpeg2000

// nltType3 applies the NLT Type 3 (sign-magnitude ↔ two's complement)
// transform at the given sample precision. This is a self-inverse XOR that
// converts IEEE 754 float bit patterns (reinterpreted as a signed integer of
// that precision) into a form suitable for the integer DWT, and back again
// after reconstruction.
//
// The transform ensures that the numeric ordering of the transformed
// values matches the ordering of the original float values,
// which is required for the wavelet lifting steps to produce
// meaningful results.
//
// precision is the component bit depth signalled by the NLT marker: 32 for
// binary32 samples, 16 for binary16 (half) samples. Samples are held
// sign-extended in int32, so the mask covers only the magnitude bits of the
// declared precision.
func nltType3(data []int32, precision int) {
	if precision < 2 || precision > 32 {
		// Defensive: an out-of-range precision would produce a nonsense mask.
		// Callers validate precision, so this cannot happen for our own
		// codestreams; leaving the data untouched is the only safe action.
		return
	}
	mask := int32(uint32(1)<<(precision-1) - 1)
	for i, v := range data {
		if v < 0 {
			data[i] = v ^ mask
		}
	}
}
