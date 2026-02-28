//go:build purego

package entropy

// mqByteOutLocal falls back to a safe implementation when built with purego tag.
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
