//go:build !purego

package entropy

// This file contains simpler versions of MQ functions designed to be inlined.

// Sign context lookup table based on neighbor contributions.
// Index: (h_contrib + 2) * 5 + (v_contrib + 2) where contrib is -2 to +2
// Value: (context, xorbit)
var lutSC [25]struct {
	ctx    uint8
	xorbit uint8
}

func init() {
	// Build sign context LUT matching JPEG 2000 spec
	for h := -2; h <= 2; h++ {
		for v := -2; v <= 2; v++ {
			idx := (h+2)*5 + (v + 2)
			ctx, xorbit := getSignContextParams(h, v)
			lutSC[idx] = struct{ ctx, xorbit uint8 }{ctx, xorbit}
		}
	}
}

func getSignContextParams(hc, vc int) (ctx uint8, xorbit uint8) {
	// Normalize contributions
	xorbit = 0
	if hc < 0 {
		xorbit = 1
		hc = -hc
	}
	if hc == 0 && vc < 0 {
		xorbit = 1
		vc = -vc
	}

	// Clamp to 0-1
	if hc > 1 {
		hc = 1
	}
	if vc < 0 {
		vc = -vc
	}
	if vc > 1 {
		vc = 1
	}

	// Context from table (CtxSC0 + offset)
	switch {
	case hc == 1:
		if vc == 1 {
			ctx = 14 // CtxSC4
		} else {
			ctx = 12 // CtxSC2
		}
	case hc == 0:
		if vc == 0 {
			ctx = 10 // CtxSC0
		} else {
			ctx = 11 // CtxSC1
		}
	default:
		ctx = 10 // CtxSC0
	}
	return
}

// getSignContrib returns the sign contribution (-1, 0, +1) from a neighbor flag.
// This is a simpler function that should inline.
//
//go:nosplit
func getSignContrib(f T1Flags) int {
	if f&T1Sig == 0 {
		return 0
	}
	if f&T1SignNeg != 0 {
		return -1
	}
	return 1
}

// clampContrib clamps contribution to [-2, 2] range for LUT lookup.
//
//go:nosplit
func clampContrib(c int) int {
	if c < -2 {
		return -2
	}
	if c > 2 {
		return 2
	}
	return c
}

// mqNeedsSlowPath checks if we need the slow byte output path.
//
//go:nosplit
func mqNeedsSlowPath(buf []byte, bp int, c uint32) bool {
	return buf[bp] == 0xFF || (c&0x8000000) != 0
}

// mqByteOutCommon is the common fast path byte output.
// Only call when mqNeedsSlowPath returns false!
//
//go:nosplit
func mqByteOutCommon(buf []byte, bp int, c uint32) (int, uint32, uint32) {
	bp++
	buf[bp] = byte(c >> 19)
	return bp, c & 0x7FFFF, 8
}

// mqByteOutRare handles the rare byte output cases (0xFF, carry).
//
//go:noinline
func mqByteOutRare(buf []byte, bp int, c uint32) (int, uint32, uint32) {
	if buf[bp] == 0xFF {
		bp++
		buf[bp] = byte(c >> 20)
		return bp, c & 0xFFFFF, 7
	}
	// Carry case
	buf[bp]++
	if buf[bp] == 0xFF {
		c &= 0x7FFFFFF
		bp++
		buf[bp] = byte(c >> 20)
		return bp, c & 0xFFFFF, 7
	}
	bp++
	buf[bp] = byte(c >> 19)
	return bp, c & 0x7FFFF, 8
}
