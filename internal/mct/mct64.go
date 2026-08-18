package mct

// Wide reversible colour transform.
//
// ForwardRCT32 widens only the luma sum: the chrominance differences B-G and
// R-G are computed in int32 and stored back into int32. That is correct for
// every integer component the format carries, and wrong for a binary32 one,
// whose NLT Type 3 samples occupy the whole int32 range — a difference of two
// of them needs 33 bits, and the difference of two 33-bit coefficients after
// one decomposition level needs 34.
//
// The wrap is invisible to a round trip, because InverseRCT32 wraps by exactly
// the same amount: the transform is a bijection modulo 2^32. It is visible to
// any decoder that reads the magnitude budget the codestream signals and holds
// the coefficients in a word wide enough for it, which is what OpenJPH does.

// ForwardRCT64 applies the reversible colour transform to 64-bit samples,
// leaving Y in r, U = B-G in g and V = R-G in b.
func ForwardRCT64(r, g, b []int64) {
	for i := range r {
		y := (r[i] + 2*g[i] + b[i]) >> 2
		u := b[i] - g[i]
		v := r[i] - g[i]

		r[i] = y
		g[i] = u
		b[i] = v
	}
}

// InverseRCT64 is the exact inverse of ForwardRCT64.
func InverseRCT64(y, u, v []int64) {
	for i := range y {
		g := y[i] - ((u[i] + v[i]) >> 2)
		r := v[i] + g
		b := u[i] + g

		y[i] = r
		u[i] = g
		v[i] = b
	}
}
