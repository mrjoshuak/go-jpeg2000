package jpeg2000

// nltType3 applies the NLT Type 3 (sign-magnitude ↔ two's complement)
// transform. This is a self-inverse XOR that converts IEEE 754 float bit
// patterns (reinterpreted as int32) into a form suitable for the integer
// DWT, and back again after reconstruction.
//
// The transform ensures that the numeric ordering of the transformed
// int32 values matches the ordering of the original float values,
// which is required for the wavelet lifting steps to produce
// meaningful results.
func nltType3(data []int32) {
	for i, v := range data {
		if v < 0 {
			data[i] = v ^ 0x7FFFFFFF
		}
	}
}
