package dwt

// Origin-aware wavelet transforms.
//
// ISO/IEC 15444-1 Annex F defines the wavelet split by absolute coordinate:
// the sample at coordinate i is a lowpass sample when i is even and a highpass
// sample when i is odd, and the boundary extension is symmetric about the two
// ends of the interval the signal actually occupies. The transforms in dwt.go
// assume that interval starts at zero, which holds for a single tile at the
// origin and for nothing else. A tile whose origin is odd at some
// decomposition level splits the other way round and produces subbands of
// different sizes, so encoding it with the origin-zero transform yields
// coefficients no conforming decoder can place.
//
// The routines here take the absolute coordinate of the first sample and
// implement the transform as the standard states it, on a signal extended by
// period-symmetric reflection. At an even origin they agree with the
// origin-zero transforms sample for sample; TestTileMatchesOriginZero checks
// that for every length up to 64.

// ceilShift returns ceil(a / 2^n) for a >= 0.
func ceilShift(a, n int) int {
	if n <= 0 {
		return a
	}
	return (a + (1 << uint(n)) - 1) >> uint(n)
}

// reflectIndex maps an arbitrary absolute coordinate onto [i0, i1) by the
// period-symmetric extension of ISO/IEC 15444-1 F.3.7: the signal is mirrored
// about i0 and about i1-1, with both boundary samples appearing once.
func reflectIndex(i, i0, i1 int) int {
	n := i1 - i0
	if n <= 1 {
		return i0
	}
	p := 2 * (n - 1)
	k := (i - i0) % p
	if k < 0 {
		k += p
	}
	if k >= n {
		k = p - k
	}
	return i0 + k
}

// LowpassCount returns the number of lowpass samples a signal spanning
// [i0, i1) splits into: the number of even coordinates it contains. The
// highpass count is the remainder. These are the subband sizes ISO/IEC
// 15444-1 Equation B-15 defines, expressed for one dimension.
func LowpassCount(i0, i1 int) int {
	return ceilShift(i1, 1) - ceilShift(i0, 1)
}

// Forward53Line applies the reversible 5/3 analysis filter to n samples whose
// first sample sits at absolute coordinate i0. On return the slice holds the
// lowpass coefficients followed by the highpass coefficients.
//
// Intermediates are 64-bit, so a full-range int32 signal (the float pipeline
// carries one) cannot overflow them.
func Forward53Line(data []int32, n, i0 int) {
	if n <= 0 {
		return
	}
	i1 := i0 + n
	if n == 1 {
		// A single sample is a lowpass sample when its coordinate is even and
		// a highpass sample, doubled, when it is odd.
		if i0&1 != 0 {
			data[0] *= 2
		}
		return
	}

	x := func(i int) int64 { return int64(data[reflectIndex(i, i0, i1)-i0]) }

	// buf covers the absolute range [i0-1, i1], one sample of slack at each
	// end so the lowpass step can read the highpass samples astride it.
	buf := getIntBuf(n + 2)
	defer putIntBuf(buf)
	base := i0 - 1

	// Y(2n+1) = X(2n+1) - floor((X(2n) + X(2n+2)) / 2)
	for i := base; i <= i1; i++ {
		if i&1 == 0 {
			continue
		}
		buf[i-base] = int32(x(i) - ((x(i-1) + x(i+1)) >> 1))
	}
	// Y(2n) = X(2n) + floor((Y(2n-1) + Y(2n+1) + 2) / 4)
	for i := i0; i < i1; i++ {
		if i&1 != 0 {
			continue
		}
		d := int64(buf[i-1-base]) + int64(buf[i+1-base]) + 2
		buf[i-base] = int32(x(i) + (d >> 2))
	}

	lo, hi := 0, LowpassCount(i0, i1)
	for i := i0; i < i1; i++ {
		v := buf[i-base]
		if i&1 == 0 {
			data[lo] = v
			lo++
		} else {
			data[hi] = v
			hi++
		}
	}
}

// Inverse53Line is the exact inverse of Forward53Line.
func Inverse53Line(data []int32, n, i0 int) {
	if n <= 0 {
		return
	}
	i1 := i0 + n
	if n == 1 {
		if i0&1 != 0 {
			data[0] /= 2
		}
		return
	}

	// Interleave back into absolute order.
	y := getIntBuf(n)
	defer putIntBuf(y)
	lo, hi := 0, LowpassCount(i0, i1)
	for i := i0; i < i1; i++ {
		if i&1 == 0 {
			y[i-i0] = data[lo]
			lo++
		} else {
			y[i-i0] = data[hi]
			hi++
		}
	}
	yv := func(i int) int64 { return int64(y[reflectIndex(i, i0, i1)-i0]) }

	out := getIntBuf(n + 2)
	defer putIntBuf(out)
	base := i0 - 1

	// X(2n) = Y(2n) - floor((Y(2n-1) + Y(2n+1) + 2) / 4)
	for i := base; i <= i1; i++ {
		if i&1 != 0 {
			continue
		}
		out[i-base] = int32(yv(i) - ((yv(i-1) + yv(i+1) + 2) >> 2))
	}
	// X(2n+1) = Y(2n+1) + floor((X(2n) + X(2n+2)) / 2)
	for i := i0; i < i1; i++ {
		if i&1 == 0 {
			continue
		}
		s := int64(out[i-1-base]) + int64(out[i+1-base])
		out[i-base] = int32(yv(i) + (s >> 1))
	}

	copy(data[:n], out[1:n+1])
}

// Forward97Line applies the irreversible 9/7 analysis filter to n samples
// whose first sample sits at absolute coordinate i0, leaving the lowpass
// coefficients first.
func Forward97Line(data []float64, n, i0 int) {
	if n <= 0 {
		return
	}
	i1 := i0 + n
	if n == 1 {
		if i0&1 != 0 {
			data[0] *= 2
		}
		return
	}

	// Work on the signal extended by four samples at each end, which is the
	// reach of the four lifting steps.
	const pad = 4
	base := i0 - pad
	buf := getFloatBuf(n + 2*pad)
	defer putFloatBuf(buf)
	for i := base; i < i1+pad; i++ {
		buf[i-base] = data[reflectIndex(i, i0, i1)-i0]
	}

	lift := func(lo, hi int, odd bool, c float64) {
		for i := lo; i < hi; i++ {
			if (i&1 != 0) != odd {
				continue
			}
			buf[i-base] += c * (buf[i-1-base] + buf[i+1-base])
		}
	}
	lift(i0-3, i1+3, true, alpha97)
	lift(i0-2, i1+2, false, beta97)
	lift(i0-1, i1+1, true, gamma97)
	lift(i0, i1, false, delta97)

	lo, hi := 0, LowpassCount(i0, i1)
	for i := i0; i < i1; i++ {
		v := buf[i-base]
		if i&1 == 0 {
			data[lo] = v * k97Inv
			lo++
		} else {
			data[hi] = v * k97
			hi++
		}
	}
}

// Inverse97Line is the inverse of Forward97Line.
func Inverse97Line(data []float64, n, i0 int) {
	if n <= 0 {
		return
	}
	i1 := i0 + n
	if n == 1 {
		if i0&1 != 0 {
			data[0] /= 2
		}
		return
	}

	y := getFloatBuf(n)
	defer putFloatBuf(y)
	lo, hi := 0, LowpassCount(i0, i1)
	for i := i0; i < i1; i++ {
		if i&1 == 0 {
			y[i-i0] = data[lo] * k97
			lo++
		} else {
			y[i-i0] = data[hi] * k97Inv
			hi++
		}
	}

	const pad = 4
	base := i0 - pad
	buf := getFloatBuf(n + 2*pad)
	defer putFloatBuf(buf)
	for i := base; i < i1+pad; i++ {
		buf[i-base] = y[reflectIndex(i, i0, i1)-i0]
	}

	lift := func(lo, hi int, odd bool, c float64) {
		for i := lo; i < hi; i++ {
			if (i&1 != 0) != odd {
				continue
			}
			buf[i-base] -= c * (buf[i-1-base] + buf[i+1-base])
		}
	}
	lift(i0-3, i1+3, false, delta97)
	lift(i0-2, i1+2, true, gamma97)
	lift(i0-1, i1+1, false, beta97)
	lift(i0, i1, true, alpha97)

	copy(data[:n], buf[pad:pad+n])
}

// forward2D53Tile transforms the w x h sub-rectangle whose top-left sample has
// absolute coordinates (x0, y0), in a row-major array of the given stride.
// Columns first, then rows, matching forward2D53Stride and ISO/IEC 15444-1
// F.4.8 (VER_SD before HOR_SD).
func forward2D53Tile(data []int32, stride, w, h, x0, y0 int) {
	col := getIntBuf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Forward53Line(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putIntBuf(col)

	for y := 0; y < h; y++ {
		Forward53Line(data[y*stride:y*stride+w], w, x0)
	}
}

// inverse2D53Tile undoes forward2D53Tile: rows first, then columns, matching
// inverse2D53Stride and F.3.8.1 (HOR_SR before VER_SR).
func inverse2D53Tile(data []int32, stride, w, h, x0, y0 int) {
	for y := 0; y < h; y++ {
		Inverse53Line(data[y*stride:y*stride+w], w, x0)
	}

	col := getIntBuf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Inverse53Line(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putIntBuf(col)
}

func forward2D97Tile(data []float64, stride, w, h, x0, y0 int) {
	col := getFloatBuf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Forward97Line(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putFloatBuf(col)

	for y := 0; y < h; y++ {
		Forward97Line(data[y*stride:y*stride+w], w, x0)
	}
}

func inverse2D97Tile(data []float64, stride, w, h, x0, y0 int) {
	for y := 0; y < h; y++ {
		Inverse97Line(data[y*stride:y*stride+w], w, x0)
	}

	col := getFloatBuf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Inverse97Line(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putFloatBuf(col)
}

// levelRect returns the coordinates of the region decomposition level l
// operates on, for a tile-component spanning [x0, x0+w) x [y0, y0+h).
// Level 0 is the tile-component itself; each further level is the lowpass
// quadrant of the one before, whose coordinates are the halved ones.
func levelRect(w, h, x0, y0, l int) (cx0, cy0, cw, ch int) {
	x1, y1 := x0+w, y0+h
	cx0, cy0 = ceilShift(x0, l), ceilShift(y0, l)
	return cx0, cy0, ceilShift(x1, l) - cx0, ceilShift(y1, l) - cy0
}

// DecomposeMultiLevel53Tile applies the reversible 5/3 decomposition to a
// tile-component of w x h samples whose top-left sample has absolute
// coordinates (x0, y0).
func DecomposeMultiLevel53Tile(data []int32, w, h, x0, y0, levels int) {
	for l := 0; l < levels; l++ {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			return
		}
		forward2D53Tile(data, w, cw, ch, cx0, cy0)
	}
}

// ReconstructMultiLevel53Tile inverts DecomposeMultiLevel53Tile.
func ReconstructMultiLevel53Tile(data []int32, w, h, x0, y0, levels int) {
	for l := levels - 1; l >= 0; l-- {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			continue
		}
		inverse2D53Tile(data, w, cw, ch, cx0, cy0)
	}
}

// DecomposeMultiLevel97Tile applies the irreversible 9/7 decomposition to a
// tile-component with the given absolute origin.
func DecomposeMultiLevel97Tile(data []float64, w, h, x0, y0, levels int) {
	for l := 0; l < levels; l++ {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			return
		}
		forward2D97Tile(data, w, cw, ch, cx0, cy0)
	}
}

// ReconstructMultiLevel97Tile inverts DecomposeMultiLevel97Tile.
func ReconstructMultiLevel97Tile(data []float64, w, h, x0, y0, levels int) {
	for l := levels - 1; l >= 0; l-- {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			continue
		}
		inverse2D97Tile(data, w, cw, ch, cx0, cy0)
	}
}
