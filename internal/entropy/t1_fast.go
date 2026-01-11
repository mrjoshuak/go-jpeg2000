//go:build !purego

package entropy

// mqByteOutLocal is a local version of byteOut that returns updated values.
// IMPORTANT: buf must be a slice with len == cap, and bp < len-1.
// The caller is responsible for growing the buffer before calling if needed.
//
//go:nosplit
func mqByteOutLocal(buf []byte, bp int, c uint32) (newBp int, newC uint32, newCT uint32) {
	if buf[bp] == 0xFF {
		bp++
		buf[bp] = byte(c >> 20)
		return bp, c & 0xFFFFF, 7
	}

	if (c & 0x8000000) == 0 {
		bp++
		buf[bp] = byte(c >> 19)
		return bp, c & 0x7FFFF, 8
	}

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
