package entropy

// Wide-coefficient entry points to the HT block coder.
//
// A binary32 component is carried through the integer pipeline as a
// reinterpreted IEEE 754 bit pattern, so after the NLT Type 3 point transform a
// sample occupies the whole int32 range. One level of the reversible 5/3
// transform then needs 33 magnitude bits, and 35 once the RCT has widened the
// chrominance differences, which is more than the 31 a sign-magnitude 32-bit
// word can hold. OpenJPH signals exactly that budget in QCD and moves such a
// component to 64-bit lines; these functions are this library's equivalent.
//
// Nothing here is a different algorithm. encodeCleanupHT is instantiated at
// int64 rather than int32 and the decoder writes a 64-bit output buffer;
// TestWideAgreesWithNarrow checks that the two agree wherever both are valid.

// EncodeCleanup64 encodes the cleanup pass of one code-block whose
// coefficients need more than 32 bits, returning the complete segment.
//
// It returns nil for an all-zero code-block, which is signalled by omitting the
// block from the packet rather than by an empty segment — the same convention
// HTEncoder.Encode uses.
func EncodeCleanup64(data []int64, width, height int) []byte {
	for _, v := range data {
		if v != 0 {
			// p = 0: encode every magnitude bit. See encodeCleanupHT.
			return encodeCleanupHT(data, width, height, 0)
		}
	}
	return nil
}

// NumBitPlanes64 returns the number of magnitude bit-planes the largest
// coefficient in data occupies. It is the wide counterpart of the bit-plane
// count the encoder records for each code-block.
func NumBitPlanes64(data []int64) int {
	var maxVal uint64
	for _, v := range data {
		m := uint64(v)
		if v < 0 {
			m = uint64(-v)
		}
		if m > maxVal {
			maxVal = m
		}
	}
	n := 0
	for maxVal > 0 {
		n++
		maxVal >>= 1
	}
	return n
}
