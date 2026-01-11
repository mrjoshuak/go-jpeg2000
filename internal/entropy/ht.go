// Package entropy provides HTJ2K (High-Throughput JPEG 2000) block coder.
//
// This implementation is based on the OpenJPEG reference (ht_dec.c),
// which is licensed under the BSD-2-Clause license.
//
// HTJ2K replaces the MQ arithmetic coder with a fast block coding stream (FBCS)
// that uses VLC (Variable Length Codes) and MEL (Magnitude Exponent Length)
// coding for improved throughput.
package entropy

import "sync"

// HTDecoder is the High-Throughput JPEG 2000 block decoder.
type HTDecoder struct {
	// Output coefficient data
	data   []int32
	width  int
	height int

	// MEL decoder state
	mel melState

	// VLC bitstream reader (backward)
	vlc revBitstream

	// MagSgn bitstream reader (forward)
	magSgn frwdBitstream

	// MRP bitstream reader (backward) for magnitude refinement
	mrp revBitstream

	// SPP bitstream reader (forward) for significance propagation
	spp frwdBitstream

	// Significance buffers (4 bits per quad column)
	sigma1 []uint8
	sigma2 []uint8

	// Line state buffer (8 bits per quad: MSB=significance, lower 7=max exponent)
	lineState []uint8
}

// melState holds the MEL decoder state.
type melState struct {
	data    []byte  // MEL bitstream data
	pos     int     // Current position in data
	tmp     uint64  // Temporary buffer for bits
	bits    int     // Number of bits in tmp
	size    int     // Remaining bytes in MEL segment
	unstuff bool    // True if next bit needs unstuffing
	k       int     // MEL state (0-12)
	numRuns int     // Number of decoded runs in queue
	runs    uint64  // Queue of decoded runs (7 bits each)
}

// revBitstream reads a backward-growing bitstream (VLC, MRP).
type revBitstream struct {
	data    []byte  // Bitstream data
	pos     int     // Current position (reading backward)
	tmp     uint64  // Temporary buffer
	bits    uint32  // Number of bits in tmp
	size    int     // Remaining bytes
	unstuff bool    // True if last byte was > 0x8F
}

// frwdBitstream reads a forward-growing bitstream (MagSgn, SPP).
type frwdBitstream struct {
	data    []byte  // Bitstream data
	pos     int     // Current position
	tmp     uint64  // Temporary buffer
	bits    uint32  // Number of bits in tmp
	unstuff bool    // True if next bit needs unstuffing
	size    int     // Remaining bytes
	x       uint32  // Value to feed when exhausted (0 or 0xFF)
}

// NewHTDecoder creates a new HTJ2K block decoder.
func NewHTDecoder(width, height int) *HTDecoder {
	quadCols := (width + 3) / 4
	return &HTDecoder{
		data:      make([]int32, width*height),
		width:     width,
		height:    height,
		sigma1:    make([]uint8, quadCols+1),
		sigma2:    make([]uint8, quadCols+1),
		lineState: make([]uint8, quadCols+1),
	}
}

// Decode decodes an HTJ2K code block.
// data contains the code block data, numBitplanes is the number of bitplanes,
// and bandType specifies the subband (LL, HL, LH, or HH).
func (d *HTDecoder) Decode(data []byte, numBitplanes, bandType int) []int32 {
	if len(data) < 2 {
		// Empty or minimal code block
		for i := range d.data {
			d.data[i] = 0
		}
		return d.data
	}

	// Parse code block header to get lengths
	// LCUP is stored in the last 2 bytes as a 12-bit value
	scup := int(data[len(data)-1]) + (int(data[len(data)-2]&0x0F) << 8)
	if scup < 2 || scup > len(data) {
		// Invalid scup, return zeros
		for i := range d.data {
			d.data[i] = 0
		}
		return d.data
	}

	lcup := len(data)
	len2 := 0 // Length of SPP+MRP segments (0 for cleanup-only pass)

	// Initialize bitstream readers
	if !d.initMEL(data, lcup, scup) {
		for i := range d.data {
			d.data[i] = 0
		}
		return d.data
	}
	d.initVLC(data, lcup, scup)
	d.initMagSgn(data, lcup-scup)

	// Initialize MRP if present
	if len2 > 0 {
		d.initMRP(data, lcup, len2)
		d.initSPP(data, lcup, len2)
	}

	// Clear significance buffers
	for i := range d.sigma1 {
		d.sigma1[i] = 0
		d.sigma2[i] = 0
	}
	for i := range d.lineState {
		d.lineState[i] = 0
	}

	// Decode the cleanup pass
	d.decodeCleanup(numBitplanes)

	// Decode SPP and MRP if present
	if len2 > 0 {
		d.decodeSPPMRP()
	}

	return d.data
}

// initMEL initializes the MEL decoder.
func (d *HTDecoder) initMEL(data []byte, lcup, scup int) bool {
	m := &d.mel
	m.data = data
	m.pos = lcup - scup
	m.bits = 0
	m.tmp = 0
	m.unstuff = false
	m.size = scup - 1 // Size is MEL+VLC-1
	m.k = 0
	m.numRuns = 0
	m.runs = 0

	// Read initial bytes to align
	num := 4 - (m.pos & 0x3)
	if num > 4 {
		num = 4
	}
	for i := 0; i < num && m.size > 0; i++ {
		if m.unstuff && m.pos < len(m.data) && m.data[m.pos] > 0x8F {
			return false
		}
		var b byte
		if m.size > 0 && m.pos < len(m.data) {
			b = m.data[m.pos]
			m.pos++
			m.size--
		} else {
			b = 0xFF
		}
		if m.size == 1 {
			b |= 0x0F
		}
		dBits := 8
		if m.unstuff {
			dBits = 7
		}
		m.tmp = (m.tmp << dBits) | uint64(b)
		m.bits += dBits
		m.unstuff = (b == 0xFF)
	}
	m.tmp <<= (64 - m.bits)
	return true
}

// melRead reads more data into the MEL buffer.
func (d *HTDecoder) melRead() {
	m := &d.mel
	if m.bits > 32 {
		return
	}

	// Read up to 4 bytes
	for i := 0; i < 4 && m.size > 0; i++ {
		if m.unstuff && m.pos < len(m.data) && m.data[m.pos] > 0x8F {
			break
		}
		var b byte
		if m.size > 0 && m.pos < len(m.data) {
			b = m.data[m.pos]
			m.pos++
			m.size--
		} else {
			b = 0xFF
		}
		dBits := 8
		if m.unstuff {
			dBits = 7
		}
		m.tmp |= uint64(b) << (56 - m.bits)
		m.bits += dBits
		m.unstuff = (b == 0xFF)
	}
}

// melDecode decodes MEL codewords into runs.
func (d *HTDecoder) melDecode() {
	m := &d.mel
	if m.bits < 6 {
		d.melRead()
	}

	for m.bits >= 6 && m.numRuns < 8 {
		eval := melExp[m.k]
		var run int
		if m.tmp&(1<<63) != 0 {
			// Next bit is 1
			run = (1 << eval) - 1
			if m.k < 12 {
				m.k++
			}
			m.tmp <<= 1
			m.bits--
			run = run << 1 // Stretch of zeros not terminating in one
		} else {
			// Next bit is 0
			run = int((m.tmp >> (63 - eval)) & uint64((1<<eval)-1))
			if m.k > 0 {
				m.k--
			}
			m.tmp <<= eval + 1
			m.bits -= eval + 1
			run = (run << 1) + 1 // Stretch of zeros terminating with one
		}
		evalPos := m.numRuns * 7
		m.runs &= ^(uint64(0x3F) << evalPos)
		m.runs |= uint64(run) << evalPos
		m.numRuns++
	}
}

// melGetRun retrieves one run from the MEL decoder.
func (d *HTDecoder) melGetRun() int {
	m := &d.mel
	if m.numRuns == 0 {
		d.melDecode()
	}
	t := int(m.runs & 0x7F)
	m.runs >>= 7
	m.numRuns--
	return t
}

// initVLC initializes the VLC bitstream reader.
func (d *HTDecoder) initVLC(data []byte, lcup, scup int) {
	v := &d.vlc
	v.data = data
	v.pos = lcup - 2 // Start at end of data
	v.size = scup - 2
	v.tmp = 0
	v.bits = 0

	// Read first half-byte
	if v.pos >= 0 && v.pos < len(v.data) {
		b := v.data[v.pos]
		v.pos--
		v.tmp = uint64(b >> 4)
		v.bits = 4 - uint32((v.tmp&7)>>2) // Check standard
		v.unstuff = (b | 0x0F) > 0x8F
	}

	// Read to align
	num := 1 + (v.pos & 0x3)
	if num > v.size {
		num = v.size
	}
	for i := 0; i < num; i++ {
		var b byte
		if v.pos >= 0 && v.pos < len(v.data) {
			b = v.data[v.pos]
			v.pos--
		}
		dBits := uint32(8)
		if v.unstuff && (b&0x7F) == 0x7F {
			dBits = 7
		}
		v.tmp |= uint64(b) << v.bits
		v.bits += dBits
		v.unstuff = b > 0x8F
	}
	v.size -= num
	d.revRead(&d.vlc)
}

// revRead reads 32 bits from a backward-growing bitstream.
func (d *HTDecoder) revRead(v *revBitstream) {
	if v.bits > 32 {
		return
	}

	var val uint32
	if v.size > 3 {
		// Read 4 bytes in little-endian order
		p := v.pos - 3
		if p >= 0 && p+3 < len(v.data) {
			val = uint32(v.data[p]) | uint32(v.data[p+1])<<8 |
				uint32(v.data[p+2])<<16 | uint32(v.data[p+3])<<24
		}
		v.pos -= 4
		v.size -= 4
	} else if v.size > 0 {
		i := 24
		for v.size > 0 {
			if v.pos >= 0 && v.pos < len(v.data) {
				val |= uint32(v.data[v.pos]) << i
				v.pos--
			}
			v.size--
			i -= 8
		}
	}

	// Accumulate with unstuffing
	tmp := val >> 24
	bits := uint32(8)
	if v.unstuff && ((val>>24)&0x7F) == 0x7F {
		bits = 7
	}
	unstuff := (val >> 24) > 0x8F

	tmp |= ((val >> 16) & 0xFF) << bits
	if unstuff && ((val>>16)&0x7F) == 0x7F {
		bits += 7
	} else {
		bits += 8
	}
	unstuff = ((val >> 16) & 0xFF) > 0x8F

	tmp |= ((val >> 8) & 0xFF) << bits
	if unstuff && ((val>>8)&0x7F) == 0x7F {
		bits += 7
	} else {
		bits += 8
	}
	unstuff = ((val >> 8) & 0xFF) > 0x8F

	tmp |= (val & 0xFF) << bits
	if unstuff && (val&0x7F) == 0x7F {
		bits += 7
	} else {
		bits += 8
	}
	v.unstuff = (val & 0xFF) > 0x8F

	v.tmp |= uint64(tmp) << v.bits
	v.bits += bits
}

// revFetch ensures at least 32 bits are available and returns them.
func (d *HTDecoder) revFetch(v *revBitstream) uint32 {
	if v.bits < 32 {
		d.revRead(v)
		if v.bits < 32 {
			d.revRead(v)
		}
	}
	return uint32(v.tmp)
}

// revAdvance consumes bits from the VLC stream.
func (d *HTDecoder) revAdvance(v *revBitstream, numBits uint32) uint32 {
	v.tmp >>= numBits
	v.bits -= numBits
	return uint32(v.tmp)
}

// initMagSgn initializes the MagSgn bitstream reader.
func (d *HTDecoder) initMagSgn(data []byte, size int) {
	f := &d.magSgn
	f.data = data
	f.pos = 0
	f.size = size
	f.tmp = 0
	f.bits = 0
	f.unstuff = false
	f.x = 0xFF // MagSgn feeds 0xFF when exhausted

	// Read to align
	num := 4 - (f.pos & 0x3)
	for i := 0; i < num; i++ {
		var b byte
		if f.size > 0 && f.pos < len(f.data) {
			b = f.data[f.pos]
			f.pos++
			f.size--
		} else {
			b = byte(f.x)
		}
		dBits := uint32(8)
		if f.unstuff {
			dBits = 7
		}
		f.tmp |= uint64(b) << f.bits
		f.bits += dBits
		f.unstuff = (b == 0xFF)
	}
	d.frwdRead(&d.magSgn)
}

// frwdRead reads 32 bits from a forward-growing bitstream.
func (d *HTDecoder) frwdRead(f *frwdBitstream) {
	if f.bits > 32 {
		return
	}

	var val uint32
	if f.size > 3 {
		// Read 4 bytes in little-endian order
		if f.pos+3 < len(f.data) {
			val = uint32(f.data[f.pos]) | uint32(f.data[f.pos+1])<<8 |
				uint32(f.data[f.pos+2])<<16 | uint32(f.data[f.pos+3])<<24
		}
		f.pos += 4
		f.size -= 4
	} else if f.size > 0 {
		if f.x != 0 {
			val = 0xFFFFFFFF
		}
		i := 0
		for f.size > 0 {
			if f.pos < len(f.data) {
				v := uint32(f.data[f.pos])
				m := ^(uint32(0xFF) << i)
				val = (val & m) | (v << i)
				f.pos++
			}
			f.size--
			i += 8
		}
	} else {
		if f.x != 0 {
			val = 0xFFFFFFFF
		}
	}

	// Accumulate with unstuffing
	bits := uint32(8)
	if f.unstuff {
		bits = 7
	}
	t := val & 0xFF
	unstuff := (val & 0xFF) == 0xFF

	t |= ((val >> 8) & 0xFF) << bits
	if unstuff {
		bits += 7
	} else {
		bits += 8
	}
	unstuff = ((val >> 8) & 0xFF) == 0xFF

	t |= ((val >> 16) & 0xFF) << bits
	if unstuff {
		bits += 7
	} else {
		bits += 8
	}
	unstuff = ((val >> 16) & 0xFF) == 0xFF

	t |= ((val >> 24) & 0xFF) << bits
	if unstuff {
		bits += 7
	} else {
		bits += 8
	}
	f.unstuff = ((val >> 24) & 0xFF) == 0xFF

	f.tmp |= uint64(t) << f.bits
	f.bits += bits
}

// frwdFetch ensures at least 32 bits are available and returns them.
func (d *HTDecoder) frwdFetch(f *frwdBitstream) uint32 {
	if f.bits < 32 {
		d.frwdRead(f)
		if f.bits < 32 {
			d.frwdRead(f)
		}
	}
	return uint32(f.tmp)
}

// frwdAdvance consumes bits from a forward bitstream.
func (d *HTDecoder) frwdAdvance(f *frwdBitstream, numBits uint32) uint32 {
	f.tmp >>= numBits
	f.bits -= numBits
	return uint32(f.tmp)
}

// initMRP initializes the MRP bitstream reader.
func (d *HTDecoder) initMRP(data []byte, lcup, len2 int) {
	m := &d.mrp
	m.data = data
	m.pos = lcup + len2 - 1
	m.size = len2
	m.unstuff = true
	m.bits = 0
	m.tmp = 0

	// Read to align
	num := 1 + (m.pos & 0x3)
	for i := 0; i < num; i++ {
		var b byte
		if m.size > 0 && m.pos >= 0 && m.pos < len(m.data) {
			b = m.data[m.pos]
			m.pos--
			m.size--
		}
		dBits := uint32(8)
		if m.unstuff && (b&0x7F) == 0x7F {
			dBits = 7
		}
		m.tmp |= uint64(b) << m.bits
		m.bits += dBits
		m.unstuff = b > 0x8F
	}
	d.revRead(&d.mrp)
}

// initSPP initializes the SPP bitstream reader.
func (d *HTDecoder) initSPP(data []byte, lcup, len2 int) {
	f := &d.spp
	f.data = data
	f.pos = lcup
	f.size = len2
	f.tmp = 0
	f.bits = 0
	f.unstuff = false
	f.x = 0 // SPP feeds 0 when exhausted

	// Read to align
	num := 4 - (f.pos & 0x3)
	for i := 0; i < num; i++ {
		var b byte
		if f.size > 0 && f.pos < len(f.data) {
			b = f.data[f.pos]
			f.pos++
			f.size--
		}
		dBits := uint32(8)
		if f.unstuff {
			dBits = 7
		}
		f.tmp |= uint64(b) << f.bits
		f.bits += dBits
		f.unstuff = (b == 0xFF)
	}
	d.frwdRead(&d.spp)
}

// decodeCleanup decodes the cleanup pass.
func (d *HTDecoder) decodeCleanup(numBitplanes int) {
	width := d.width
	height := d.height
	quadCols := (width + 3) / 4

	// Process in 4-row stripes
	for y := 0; y < height; y += 4 {
		isInitial := (y == 0)

		// Process each quad pair (two quads of 4 samples each)
		for qx := 0; qx < quadCols; qx += 2 {
			// Get VLC codeword
			vlcVal := d.revFetch(&d.vlc)

			// Determine context for first quad
			var context uint8
			if isInitial {
				// Initial line: context from previous quads only
				if qx > 0 {
					context = d.sigma1[qx-1] >> 4
				}
			} else {
				// Non-initial: context from vertical neighbors too
				context = (d.sigma1[qx] >> 4) | (d.lineState[qx] >> 4)
			}

			// Decode first quad using VLC table
			var tbl *[1024]uint16
			if isInitial {
				tbl = &vlcTbl0
			} else {
				tbl = &vlcTbl1
			}

			// VLC lookup
			idx := (uint32(context) << 7) | (vlcVal & 0x7F)
			qinf := tbl[idx]
			vlcLen := qinf & 0x0F
			rho := (qinf >> 4) & 0x0F
			uOff1 := (qinf >> 3) & 0x01

			d.revAdvance(&d.vlc, uint32(vlcLen))
			vlcVal = d.revFetch(&d.vlc)

			// Determine context for second quad
			context2 := uint8(rho>>2) | (d.sigma1[qx+1] >> 4)

			// Decode second quad
			idx2 := (uint32(context2) << 7) | (vlcVal & 0x7F)
			qinf2 := tbl[idx2]
			vlcLen2 := qinf2 & 0x0F
			rho2 := (qinf2 >> 4) & 0x0F
			uOff2 := (qinf2 >> 3) & 0x01

			d.revAdvance(&d.vlc, uint32(vlcLen2))

			// Update significance
			d.sigma1[qx] = uint8(rho)
			d.sigma1[qx+1] = uint8(rho2)

			// Decode u values if context was 0 and MEL needed
			var u [2]uint32
			mode := (uOff1 << 1) | uOff2
			if mode > 0 {
				vlcVal = d.revFetch(&d.vlc)
				var consumed uint32
				if isInitial {
					consumed = d.decodeInitUVLC(vlcVal, uint32(mode), &u)
				} else {
					consumed = d.decodeNonInitUVLC(vlcVal, uint32(mode), &u)
				}
				d.revAdvance(&d.vlc, consumed)
			} else {
				u[0] = 1
				u[1] = 1
			}

			// Decode magnitudes and signs from MagSgn stream
			for i := 0; i < 4 && qx*4+i < width; i++ {
				if rho&(1<<i) != 0 {
					// This sample is significant
					magVal := d.frwdFetch(&d.magSgn)
					emb := u[0] // Exponent magnitude bits

					// Extract magnitude
					mag := int32((magVal & ((1 << emb) - 1)) + (1 << (emb - 1)))
					d.frwdAdvance(&d.magSgn, emb)

					// Extract sign (LSB of next fetch)
					signVal := d.frwdFetch(&d.magSgn)
					sign := signVal & 1
					d.frwdAdvance(&d.magSgn, 1)

					// Store coefficient
					idx := y*width + qx*4 + i
					if idx < len(d.data) {
						if sign != 0 {
							d.data[idx] = -mag
						} else {
							d.data[idx] = mag
						}
					}
				}
			}

			// Process second quad similarly
			for i := 0; i < 4 && (qx+1)*4+i < width; i++ {
				if rho2&(1<<i) != 0 {
					magVal := d.frwdFetch(&d.magSgn)
					emb := u[1]

					mag := int32((magVal & ((1 << emb) - 1)) + (1 << (emb - 1)))
					d.frwdAdvance(&d.magSgn, emb)

					signVal := d.frwdFetch(&d.magSgn)
					sign := signVal & 1
					d.frwdAdvance(&d.magSgn, 1)

					idx := y*width + (qx+1)*4 + i
					if idx < len(d.data) {
						if sign != 0 {
							d.data[idx] = -mag
						} else {
							d.data[idx] = mag
						}
					}
				}
			}
		}
	}
}

// decodeInitUVLC decodes initial UVLC to get u values.
func (d *HTDecoder) decodeInitUVLC(vlc, mode uint32, u *[2]uint32) uint32 {
	// UVLC prefix decoder table
	dec := [8]uint8{
		3 | (5 << 2) | (5 << 5), // 000
		1 | (0 << 2) | (1 << 5), // 001 = xx1
		2 | (0 << 2) | (2 << 5), // 010 = x10
		1 | (0 << 2) | (1 << 5), // 011 = xx1
		3 | (1 << 2) | (3 << 5), // 100
		1 | (0 << 2) | (1 << 5), // 101 = xx1
		2 | (0 << 2) | (2 << 5), // 110 = x10
		1 | (0 << 2) | (1 << 5), // 111 = xx1
	}

	consumed := uint32(0)
	if mode == 0 {
		u[0] = 1
		u[1] = 1
	} else if mode <= 2 {
		t := dec[vlc&0x7]
		prefixLen := uint32(t & 0x3)
		vlc >>= prefixLen
		consumed += prefixLen

		suffixLen := uint32((t >> 2) & 0x7)
		consumed += suffixLen

		val := uint32(t>>5) + (vlc & ((1 << suffixLen) - 1))
		if mode == 1 {
			u[0] = val + 1
			u[1] = 1
		} else {
			u[0] = 1
			u[1] = val + 1
		}
	} else if mode == 3 {
		t1 := dec[vlc&0x7]
		prefixLen1 := uint32(t1 & 0x3)
		vlc >>= prefixLen1
		consumed += prefixLen1

		if prefixLen1 > 2 {
			u[1] = (vlc & 1) + 2
			consumed++
			vlc >>= 1

			suffixLen := uint32((t1 >> 2) & 0x7)
			consumed += suffixLen
			val := uint32(t1>>5) + (vlc & ((1 << suffixLen) - 1))
			u[0] = val + 1
		} else {
			t2 := dec[vlc&0x7]
			prefixLen2 := uint32(t2 & 0x3)
			vlc >>= prefixLen2
			consumed += prefixLen2

			suffixLen1 := uint32((t1 >> 2) & 0x7)
			consumed += suffixLen1
			val1 := uint32(t1>>5) + (vlc & ((1 << suffixLen1) - 1))
			u[0] = val1 + 1
			vlc >>= suffixLen1

			suffixLen2 := uint32((t2 >> 2) & 0x7)
			consumed += suffixLen2
			val2 := uint32(t2>>5) + (vlc & ((1 << suffixLen2) - 1))
			u[1] = val2 + 1
		}
	} else if mode == 4 {
		t1 := dec[vlc&0x7]
		prefixLen1 := uint32(t1 & 0x3)
		vlc >>= prefixLen1
		consumed += prefixLen1

		t2 := dec[vlc&0x7]
		prefixLen2 := uint32(t2 & 0x3)
		vlc >>= prefixLen2
		consumed += prefixLen2

		suffixLen1 := uint32((t1 >> 2) & 0x7)
		consumed += suffixLen1
		val1 := uint32(t1>>5) + (vlc & ((1 << suffixLen1) - 1))
		u[0] = val1 + 3
		vlc >>= suffixLen1

		suffixLen2 := uint32((t2 >> 2) & 0x7)
		consumed += suffixLen2
		val2 := uint32(t2>>5) + (vlc & ((1 << suffixLen2) - 1))
		u[1] = val2 + 3
	}
	return consumed
}

// decodeNonInitUVLC decodes non-initial UVLC to get u values.
func (d *HTDecoder) decodeNonInitUVLC(vlc, mode uint32, u *[2]uint32) uint32 {
	dec := [8]uint8{
		3 | (5 << 2) | (5 << 5),
		1 | (0 << 2) | (1 << 5),
		2 | (0 << 2) | (2 << 5),
		1 | (0 << 2) | (1 << 5),
		3 | (1 << 2) | (3 << 5),
		1 | (0 << 2) | (1 << 5),
		2 | (0 << 2) | (2 << 5),
		1 | (0 << 2) | (1 << 5),
	}

	consumed := uint32(0)
	if mode == 0 {
		u[0] = 1
		u[1] = 1
	} else if mode <= 2 {
		t := dec[vlc&0x7]
		prefixLen := uint32(t & 0x3)
		vlc >>= prefixLen
		consumed += prefixLen

		suffixLen := uint32((t >> 2) & 0x7)
		consumed += suffixLen

		val := uint32(t>>5) + (vlc & ((1 << suffixLen) - 1))
		if mode == 1 {
			u[0] = val + 1
			u[1] = 1
		} else {
			u[0] = 1
			u[1] = val + 1
		}
	} else if mode == 3 {
		t1 := dec[vlc&0x7]
		prefixLen1 := uint32(t1 & 0x3)
		vlc >>= prefixLen1
		consumed += prefixLen1

		t2 := dec[vlc&0x7]
		prefixLen2 := uint32(t2 & 0x3)
		vlc >>= prefixLen2
		consumed += prefixLen2

		suffixLen1 := uint32((t1 >> 2) & 0x7)
		consumed += suffixLen1
		val1 := uint32(t1>>5) + (vlc & ((1 << suffixLen1) - 1))
		u[0] = val1 + 1
		vlc >>= suffixLen1

		suffixLen2 := uint32((t2 >> 2) & 0x7)
		consumed += suffixLen2
		val2 := uint32(t2>>5) + (vlc & ((1 << suffixLen2) - 1))
		u[1] = val2 + 1
	}
	return consumed
}

// decodeSPPMRP decodes the SPP and MRP passes.
func (d *HTDecoder) decodeSPPMRP() {
	// TODO: Implement SPP and MRP decoding for refinement passes
}

// HTEncoder is the High-Throughput JPEG 2000 block encoder.
type HTEncoder struct {
	// Input coefficient data
	data   []int32
	width  int
	height int

	// MEL encoder state
	mel melEncState

	// VLC bitstream writer (backward-growing)
	vlc revBitWriter

	// MagSgn bitstream writer (forward-growing)
	magSgn frwdBitWriter

	// Output buffer
	output []byte

	// Significance buffers
	sigma1 []uint8
	sigma2 []uint8
}

// melEncState holds the MEL encoder state.
type melEncState struct {
	data []byte  // Output buffer
	tmp  uint64  // Temporary buffer
	bits int     // Number of bits in tmp
	k    int     // MEL state (0-12)
	run  int     // Current run length
}

// revBitWriter writes bits to a backward-growing stream.
type revBitWriter struct {
	data    []byte  // Output buffer
	pos     int     // Current write position (backward)
	tmp     uint64  // Temporary buffer
	bits    int     // Number of bits in tmp
	lastByte byte   // Last byte written (for unstuffing check)
}

// frwdBitWriter writes bits to a forward-growing stream.
type frwdBitWriter struct {
	data     []byte  // Output buffer
	pos      int     // Current write position
	tmp      uint64  // Temporary buffer
	bits     int     // Number of bits in tmp
	lastByte byte    // Last byte written (for unstuffing)
}

// NewHTEncoder creates a new HTJ2K block encoder.
func NewHTEncoder(width, height int) *HTEncoder {
	quadCols := (width + 3) / 4
	return &HTEncoder{
		data:   make([]int32, width*height),
		width:  width,
		height: height,
		sigma1: make([]uint8, quadCols+1),
		sigma2: make([]uint8, quadCols+1),
	}
}

// SetData sets the coefficient data to encode.
func (e *HTEncoder) SetData(data []int32) {
	copy(e.data, data)
}

// Encode encodes the code block using HTJ2K.
// bandType specifies the subband (BandLL, BandHL, BandLH, or BandHH).
// Returns the encoded data.
func (e *HTEncoder) Encode(bandType int) []byte {
	width := e.width
	height := e.height

	// Find the maximum magnitude to determine the number of bitplanes
	maxMag := int32(0)
	for _, v := range e.data {
		if v < 0 {
			v = -v
		}
		if v > maxMag {
			maxMag = v
		}
	}

	if maxMag == 0 {
		// Empty code block
		return nil
	}

	// Count the number of magnitude bits needed
	numBits := 0
	for m := maxMag; m > 0; m >>= 1 {
		numBits++
	}

	// Estimate output buffer size
	maxSize := width * height * 2 // Conservative estimate
	if maxSize < 64 {
		maxSize = 64
	}

	// Initialize output buffers
	e.output = make([]byte, maxSize)

	// Initialize MEL encoder
	e.mel.data = make([]byte, maxSize/4)
	e.mel.tmp = 0
	e.mel.bits = 0
	e.mel.k = 0
	e.mel.run = 0

	// Initialize VLC writer (writes backward from end)
	e.vlc.data = make([]byte, maxSize/2)
	e.vlc.pos = len(e.vlc.data) - 1
	e.vlc.tmp = 0
	e.vlc.bits = 0
	e.vlc.lastByte = 0

	// Initialize MagSgn writer (writes forward from start)
	e.magSgn.data = make([]byte, maxSize/2)
	e.magSgn.pos = 0
	e.magSgn.tmp = 0
	e.magSgn.bits = 0
	e.magSgn.lastByte = 0

	// Clear significance buffers
	for i := range e.sigma1 {
		e.sigma1[i] = 0
		e.sigma2[i] = 0
	}

	// Encode the cleanup pass
	e.encodeCleanup(numBits)

	// Flush all streams
	e.melFlush()
	e.vlcFlush()
	e.magSgnFlush()

	// Combine streams into output:
	// Output format: MagSgn | MEL+VLC | SCUP (2 bytes)
	//
	// MagSgn grows forward, MEL and VLC are interleaved growing toward each other

	// Get the actual lengths
	magSgnLen := e.magSgn.pos
	melLen := len(e.mel.data)
	vlcLen := len(e.vlc.data) - e.vlc.pos - 1

	// Calculate SCUP (length of MEL+VLC segment)
	scup := melLen + vlcLen + 2 // +2 for the SCUP marker itself

	// Build output
	totalLen := magSgnLen + scup
	output := make([]byte, totalLen)

	// Copy MagSgn data
	copy(output[0:magSgnLen], e.magSgn.data[0:magSgnLen])

	// Copy MEL data
	copy(output[magSgnLen:magSgnLen+melLen], e.mel.data[0:melLen])

	// Copy VLC data (reversed)
	for i := 0; i < vlcLen; i++ {
		output[magSgnLen+melLen+i] = e.vlc.data[len(e.vlc.data)-1-i]
	}

	// Write SCUP at the end (12 bits in last 2 bytes)
	output[totalLen-2] = byte(scup >> 8)
	output[totalLen-1] = byte(scup & 0xFF)

	return output
}

// encodeCleanup encodes the cleanup pass.
func (e *HTEncoder) encodeCleanup(numBits int) {
	width := e.width
	height := e.height
	quadCols := (width + 3) / 4

	// Process in 4-row stripes
	for y := 0; y < height; y += 4 {
		isInitial := (y == 0)

		// Process each quad pair
		for qx := 0; qx < quadCols; qx += 2 {
			// Compute significance pattern (rho) for first quad
			var rho uint8
			for i := 0; i < 4 && qx*4+i < width; i++ {
				idx := y*width + qx*4 + i
				if idx < len(e.data) && e.data[idx] != 0 {
					rho |= 1 << i
				}
			}

			// Compute significance pattern for second quad
			var rho2 uint8
			for i := 0; i < 4 && (qx+1)*4+i < width; i++ {
				idx := y*width + (qx+1)*4 + i
				if idx < len(e.data) && e.data[idx] != 0 {
					rho2 |= 1 << i
				}
			}

			// Determine context
			var context uint8
			if isInitial {
				if qx > 0 {
					context = e.sigma1[qx-1] >> 4
				}
			} else {
				context = (e.sigma1[qx] >> 4)
			}

			// Encode VLC for first quad
			e.encodeVLCQuad(context, rho, isInitial)

			// Update sigma
			e.sigma1[qx] = rho

			// Determine context for second quad
			context2 := (rho >> 2) | (e.sigma1[qx+1] >> 4)

			// Encode VLC for second quad
			e.encodeVLCQuad(context2, rho2, isInitial)

			// Update sigma
			e.sigma1[qx+1] = rho2

			// Encode u-values if needed
			uOff1 := (rho != 0)
			uOff2 := (rho2 != 0)
			if uOff1 || uOff2 {
				// Find maximum magnitude in each quad
				var u1, u2 uint32 = 1, 1
				for i := 0; i < 4 && qx*4+i < width; i++ {
					idx := y*width + qx*4 + i
					if idx < len(e.data) {
						v := e.data[idx]
						if v < 0 {
							v = -v
						}
						if uint32(v) >= (1 << u1) {
							u1++
						}
					}
				}
				for i := 0; i < 4 && (qx+1)*4+i < width; i++ {
					idx := y*width + (qx+1)*4 + i
					if idx < len(e.data) {
						v := e.data[idx]
						if v < 0 {
							v = -v
						}
						if uint32(v) >= (1 << u2) {
							u2++
						}
					}
				}

				// Encode u-values
				mode := uint32(0)
				if uOff1 {
					mode |= 1
				}
				if uOff2 {
					mode |= 2
				}
				e.encodeUVLC(mode, u1, u2, isInitial)
			}

			// Encode magnitudes and signs to MagSgn stream
			for i := 0; i < 4 && qx*4+i < width; i++ {
				if rho&(1<<i) != 0 {
					idx := y*width + qx*4 + i
					if idx < len(e.data) {
						v := e.data[idx]
						sign := uint32(0)
						if v < 0 {
							sign = 1
							v = -v
						}
						mag := uint32(v)

						// Find number of magnitude bits
						emb := uint32(1)
						for mag >= (1 << emb) {
							emb++
						}

						// Write magnitude (without leading 1)
						e.magSgnWrite(mag&((1<<(emb-1))-1), emb-1)
						// Write sign
						e.magSgnWrite(sign, 1)
					}
				}
			}

			// Encode second quad magnitudes and signs
			for i := 0; i < 4 && (qx+1)*4+i < width; i++ {
				if rho2&(1<<i) != 0 {
					idx := y*width + (qx+1)*4 + i
					if idx < len(e.data) {
						v := e.data[idx]
						sign := uint32(0)
						if v < 0 {
							sign = 1
							v = -v
						}
						mag := uint32(v)

						emb := uint32(1)
						for mag >= (1 << emb) {
							emb++
						}

						e.magSgnWrite(mag&((1<<(emb-1))-1), emb-1)
						e.magSgnWrite(sign, 1)
					}
				}
			}
		}
	}
}

// encodeVLCQuad encodes a quad's VLC codeword.
func (e *HTEncoder) encodeVLCQuad(context, rho uint8, isInitial bool) {
	// Find VLC codeword for this (context, rho) pair
	// This is a simplified encoding - in practice we'd use a reverse lookup

	var tbl *[1024]uint16
	if isInitial {
		tbl = &vlcTbl0
	} else {
		tbl = &vlcTbl1
	}

	// Search for matching entry (simplified - should use encoder table)
	for cwd := uint32(0); cwd < 128; cwd++ {
		idx := (uint32(context) << 7) | cwd
		entry := tbl[idx]
		entryLen := entry & 0x0F
		entryRho := (entry >> 4) & 0x0F

		if uint8(entryRho) == rho && entryLen > 0 {
			// Found matching entry
			e.vlcWrite(cwd, uint32(entryLen))
			return
		}
	}

	// Default: write 0 with length 1
	e.vlcWrite(0, 1)
}

// encodeUVLC encodes u-values.
func (e *HTEncoder) encodeUVLC(mode, u1, u2 uint32, isInitial bool) {
	// Simplified UVLC encoding
	if mode == 0 {
		return
	}

	// Encode prefix and suffix for u values
	// This is a simplified version - full implementation would use proper UVLC tables
	if mode == 1 || mode == 2 {
		u := u1
		if mode == 2 {
			u = u2
		}
		if u <= 1 {
			e.vlcWrite(1, 1) // Prefix "1"
		} else if u <= 2 {
			e.vlcWrite(2, 2) // Prefix "01"
		} else {
			e.vlcWrite(0, 3) // Prefix "000" + suffix
			e.vlcWrite(u-3, 5)
		}
	} else if mode == 3 {
		// Both u_off are 1
		for _, u := range []uint32{u1, u2} {
			if u <= 1 {
				e.vlcWrite(1, 1)
			} else if u <= 2 {
				e.vlcWrite(2, 2)
			} else {
				e.vlcWrite(0, 3)
				e.vlcWrite(u-3, 5)
			}
		}
	}
}

// vlcWrite writes bits to the VLC stream (backward).
func (e *HTEncoder) vlcWrite(val uint32, numBits uint32) {
	e.vlc.tmp |= uint64(val) << e.vlc.bits
	e.vlc.bits += int(numBits)

	// Flush complete bytes
	for e.vlc.bits >= 8 {
		b := byte(e.vlc.tmp & 0xFF)

		// Handle bit-stuffing
		if e.vlc.lastByte > 0x8F && (b&0x7F) == 0x7F {
			// Skip MSB (set to 0)
			b &= 0x7F
		}

		e.vlc.data[e.vlc.pos] = b
		e.vlc.pos--
		e.vlc.lastByte = b
		e.vlc.tmp >>= 8
		e.vlc.bits -= 8
	}
}

// vlcFlush flushes remaining bits in VLC stream.
func (e *HTEncoder) vlcFlush() {
	for e.vlc.bits > 0 {
		b := byte(e.vlc.tmp & 0xFF)
		e.vlc.data[e.vlc.pos] = b
		e.vlc.pos--
		e.vlc.tmp >>= 8
		e.vlc.bits -= 8
		if e.vlc.bits < 0 {
			e.vlc.bits = 0
		}
	}
}

// magSgnWrite writes bits to the MagSgn stream (forward).
func (e *HTEncoder) magSgnWrite(val uint32, numBits uint32) {
	e.magSgn.tmp |= uint64(val) << e.magSgn.bits
	e.magSgn.bits += int(numBits)

	// Flush complete bytes
	for e.magSgn.bits >= 8 {
		b := byte(e.magSgn.tmp & 0xFF)

		// Handle bit-stuffing: after 0xFF, skip MSB of next byte
		if e.magSgn.lastByte == 0xFF {
			// Write 7 bits only, MSB is 0
			b &= 0x7F
			e.magSgn.data[e.magSgn.pos] = b
			e.magSgn.pos++
			e.magSgn.tmp >>= 7
			e.magSgn.bits -= 7
		} else {
			e.magSgn.data[e.magSgn.pos] = b
			e.magSgn.pos++
			e.magSgn.tmp >>= 8
			e.magSgn.bits -= 8
		}
		e.magSgn.lastByte = b
	}
}

// magSgnFlush flushes remaining bits in MagSgn stream.
func (e *HTEncoder) magSgnFlush() {
	for e.magSgn.bits > 0 {
		b := byte(e.magSgn.tmp & 0xFF)
		e.magSgn.data[e.magSgn.pos] = b
		e.magSgn.pos++
		e.magSgn.tmp >>= 8
		e.magSgn.bits -= 8
		if e.magSgn.bits < 0 {
			e.magSgn.bits = 0
		}
	}
}

// melFlush flushes the MEL encoder.
func (e *HTEncoder) melFlush() {
	// Write any remaining run
	if e.mel.run > 0 {
		e.melEncodeRun()
	}

	// Flush remaining bits
	for e.mel.bits > 0 {
		b := byte(e.mel.tmp >> (e.mel.bits - 8))
		e.mel.data = append(e.mel.data, b)
		e.mel.bits -= 8
		if e.mel.bits < 0 {
			e.mel.bits = 0
		}
	}
}

// melEncodeRun encodes a run in the MEL stream.
func (e *HTEncoder) melEncodeRun() {
	// MEL encoding using state machine
	eval := melExp[e.mel.k]
	maxRun := (1 << eval) - 1

	if e.mel.run >= maxRun {
		// Run is at max for this state, output 1 bit
		e.mel.tmp = (e.mel.tmp << 1) | 1
		e.mel.bits++
		if e.mel.k < 12 {
			e.mel.k++
		}
		e.mel.run -= maxRun
	} else {
		// Output 0 followed by eval bits for run
		e.mel.tmp = (e.mel.tmp << (eval + 1)) | uint64(e.mel.run)
		e.mel.bits += eval + 1
		if e.mel.k > 0 {
			e.mel.k--
		}
		e.mel.run = 0
	}

	// Flush complete bytes
	for e.mel.bits >= 8 {
		b := byte(e.mel.tmp >> (e.mel.bits - 8))
		e.mel.data = append(e.mel.data, b)
		e.mel.bits -= 8
	}
}

// htDecoderPool provides pooled HT decoders to reduce allocations.
var htDecoderPool = sync.Pool{
	New: func() interface{} {
		return NewHTDecoder(64, 64)
	},
}

// htEncoderPool provides pooled HT encoders to reduce allocations.
var htEncoderPool = sync.Pool{
	New: func() interface{} {
		return NewHTEncoder(64, 64)
	},
}

// GetHTDecoder returns a pooled HT decoder, resizing if necessary.
func GetHTDecoder(width, height int) *HTDecoder {
	d := htDecoderPool.Get().(*HTDecoder)
	d.Resize(width, height)
	return d
}

// PutHTDecoder returns an HT decoder to the pool.
func PutHTDecoder(d *HTDecoder) {
	htDecoderPool.Put(d)
}

// GetHTEncoder returns a pooled HT encoder, resizing if necessary.
func GetHTEncoder(width, height int) *HTEncoder {
	e := htEncoderPool.Get().(*HTEncoder)
	e.Resize(width, height)
	return e
}

// PutHTEncoder returns an HT encoder to the pool.
func PutHTEncoder(e *HTEncoder) {
	htEncoderPool.Put(e)
}

// Resize resizes the HT decoder for a new code block size.
func (d *HTDecoder) Resize(width, height int) {
	d.width = width
	d.height = height

	dataSize := width * height
	if cap(d.data) < dataSize {
		d.data = make([]int32, dataSize)
	} else {
		d.data = d.data[:dataSize]
	}

	quadCols := (width + 3) / 4
	if cap(d.sigma1) < quadCols+1 {
		d.sigma1 = make([]uint8, quadCols+1)
		d.sigma2 = make([]uint8, quadCols+1)
		d.lineState = make([]uint8, quadCols+1)
	} else {
		d.sigma1 = d.sigma1[:quadCols+1]
		d.sigma2 = d.sigma2[:quadCols+1]
		d.lineState = d.lineState[:quadCols+1]
	}
}

// Resize resizes the HT encoder for a new code block size.
func (e *HTEncoder) Resize(width, height int) {
	e.width = width
	e.height = height

	dataSize := width * height
	if cap(e.data) < dataSize {
		e.data = make([]int32, dataSize)
	} else {
		e.data = e.data[:dataSize]
	}

	quadCols := (width + 3) / 4
	if cap(e.sigma1) < quadCols+1 {
		e.sigma1 = make([]uint8, quadCols+1)
		e.sigma2 = make([]uint8, quadCols+1)
	} else {
		e.sigma1 = e.sigma1[:quadCols+1]
		e.sigma2 = e.sigma2[:quadCols+1]
	}
}
