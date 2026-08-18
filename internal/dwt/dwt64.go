package dwt

// Wide reversible 5/3 transform.
//
// The transforms in dwt.go and tile.go compute their lifting steps in int64 and
// then store the result back into int32. That is enough for every integer
// component the format carries, whose samples leave at least two bits of head
// room, and it is not enough for a binary32 one: after the NLT Type 3 point
// transform a sample occupies the whole int32 range, so the first
// decomposition level already needs 33 magnitude bits and the RCT pushes the
// chrominance differences to 35.
//
// The overflow is invisible to a round trip. The 5/3 lifting steps are exactly
// invertible modulo 2^32, so this library's own decoder reproduces the input
// bit for bit from wrapped coefficients; every other implementation reads the
// magnitude budget the codestream signals, decodes into a word that holds it,
// and reconstructs different samples. OpenJPH does exactly that.
//
// These routines are the same filter at 64 bits. TestWide53MatchesNarrow checks
// that they agree with the int32 forms sample for sample on data the narrow
// form can represent.

import "sync"

var int64BufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]int64, 4096)
		return &buf
	},
}

// getInt64Buf returns a buffer of at least size n from the pool.
func getInt64Buf(n int) []int64 {
	bp := int64BufPool.Get().(*[]int64)
	buf := *bp
	if cap(buf) < n {
		buf = make([]int64, n)
		*bp = buf
	}
	return buf[:n]
}

// putInt64Buf returns a buffer to the pool.
func putInt64Buf(buf []int64) {
	bp := &buf
	int64BufPool.Put(bp)
}

// Forward53Line64 is Forward53Line on 64-bit samples.
func Forward53Line64(data []int64, n, i0 int) {
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

	x := func(i int) int64 { return data[reflectIndex(i, i0, i1)-i0] }

	buf := getInt64Buf(n + 2)
	defer putInt64Buf(buf)
	base := i0 - 1

	// Y(2n+1) = X(2n+1) - floor((X(2n) + X(2n+2)) / 2)
	for i := base; i <= i1; i++ {
		if i&1 == 0 {
			continue
		}
		buf[i-base] = x(i) - ((x(i-1) + x(i+1)) >> 1)
	}
	// Y(2n) = X(2n) + floor((Y(2n-1) + Y(2n+1) + 2) / 4)
	for i := i0; i < i1; i++ {
		if i&1 != 0 {
			continue
		}
		buf[i-base] = x(i) + ((buf[i-1-base] + buf[i+1-base] + 2) >> 2)
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

// Inverse53Line64 is the exact inverse of Forward53Line64.
func Inverse53Line64(data []int64, n, i0 int) {
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

	y := getInt64Buf(n)
	defer putInt64Buf(y)
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
	yv := func(i int) int64 { return y[reflectIndex(i, i0, i1)-i0] }

	out := getInt64Buf(n + 2)
	defer putInt64Buf(out)
	base := i0 - 1

	// X(2n) = Y(2n) - floor((Y(2n-1) + Y(2n+1) + 2) / 4)
	for i := base; i <= i1; i++ {
		if i&1 != 0 {
			continue
		}
		out[i-base] = yv(i) - ((yv(i-1) + yv(i+1) + 2) >> 2)
	}
	// X(2n+1) = Y(2n+1) + floor((X(2n) + X(2n+2)) / 2)
	for i := i0; i < i1; i++ {
		if i&1 == 0 {
			continue
		}
		out[i-base] = yv(i) + ((out[i-1-base] + out[i+1-base]) >> 1)
	}

	copy(data[:n], out[1:n+1])
}

// forward2D53Tile64 transforms the w x h sub-rectangle whose top-left sample
// has absolute coordinates (x0, y0). Columns first, then rows, matching
// forward2D53Tile and ISO/IEC 15444-1 F.4.8.
func forward2D53Tile64(data []int64, stride, w, h, x0, y0 int) {
	col := getInt64Buf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Forward53Line64(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putInt64Buf(col)

	for y := 0; y < h; y++ {
		Forward53Line64(data[y*stride:y*stride+w], w, x0)
	}
}

// inverse2D53Tile64 undoes forward2D53Tile64: rows first, then columns.
func inverse2D53Tile64(data []int64, stride, w, h, x0, y0 int) {
	for y := 0; y < h; y++ {
		Inverse53Line64(data[y*stride:y*stride+w], w, x0)
	}

	col := getInt64Buf(h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = data[y*stride+x]
		}
		Inverse53Line64(col[:h], h, y0)
		for y := 0; y < h; y++ {
			data[y*stride+x] = col[y]
		}
	}
	putInt64Buf(col)
}

// DecomposeMultiLevel53Tile64 applies the reversible 5/3 decomposition to a
// tile-component of w x h 64-bit samples whose top-left sample has absolute
// coordinates (x0, y0).
func DecomposeMultiLevel53Tile64(data []int64, w, h, x0, y0, levels int) {
	for l := 0; l < levels; l++ {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			return
		}
		forward2D53Tile64(data, w, cw, ch, cx0, cy0)
	}
}

// ReconstructMultiLevel53Tile64 inverts DecomposeMultiLevel53Tile64.
func ReconstructMultiLevel53Tile64(data []int64, w, h, x0, y0, levels int) {
	for l := levels - 1; l >= 0; l-- {
		cx0, cy0, cw, ch := levelRect(w, h, x0, y0, l)
		if cw <= 0 || ch <= 0 {
			continue
		}
		inverse2D53Tile64(data, w, cw, ch, cx0, cy0)
	}
}
