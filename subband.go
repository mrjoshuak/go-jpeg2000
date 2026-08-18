package jpeg2000

// Subband geometry.
//
// A 5/3 or 9/7 split of a signal of length W produces ceil(W/2) lowpass samples
// and floor(W/2) highpass samples. Those differ whenever W is odd, and which of
// the two applies to a subband depends on its orientation: HL is highpass
// horizontally and lowpass vertically, LH is the reverse, and HH is highpass in
// both directions.
//
// Every site in this package previously used ceil for all three detail bands.
// That is correct only when the resolution's dimensions are even, which is why
// power-of-two images interoperated and a 17-pixel image did not: the encoder
// claimed a 9-wide HL band where the standard defines an 8-wide one, so the
// code-block partition, the packet headers and the coefficient placement were
// all one column too wide.

// Band orientation indices, matching the order subbands appear in a packet at
// resolution levels above zero.
const (
	bandHL = 0
	bandLH = 1
	bandHH = 2
)

// subbandDims returns the dimensions of one detail subband, given the
// dimensions of the resolution level it is split from.
func subbandDims(resW, resH, band int) (int, int) {
	lowW, highW := (resW+1)/2, resW/2
	lowH, highH := (resH+1)/2, resH/2
	switch band {
	case bandHL:
		return highW, lowH
	case bandLH:
		return lowW, highH
	default: // bandHH
		return highW, highH
	}
}

// resolutionDims returns the dimensions of resolution level r for a tile
// component of the given size, i.e. ceil(size / 2^(numRes-1-r)).
func resolutionDims(tcW, tcH, numRes, r int) (int, int) {
	scale := 1 << uint(numRes-1-r)
	return (tcW + scale - 1) / scale, (tcH + scale - 1) / scale
}

// bandDims returns the dimensions of the subband identified by (r, band): the
// LL band at resolution 0, or one of the three detail bands above it.
func bandDims(tcW, tcH, numRes, r, band int) (int, int) {
	w, h := resolutionDims(tcW, tcH, numRes, r)
	if r == 0 {
		return w, h
	}
	return subbandDims(w, h, band)
}
