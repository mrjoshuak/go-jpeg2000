package entropy

// HTJ2K cleanup-pass block encoder.
//
// Ported from OpenJPH's ojph_block_encoder.cpp (2-Clause BSD, Copyright (c)
// 2019 Aous Naman, Kakadu Software Pty Ltd, The University of New South
// Wales), which is the reference implementation of ISO/IEC 15444-15.
//
// This replaces an earlier encoder that shared none of the standard's
// conventions. Because the matching decoder deviated in the same way, every
// round-trip test passed while no conforming decoder could read the output.

import "math/bits"

// uvlcEntry holds the prefix, suffix and extension codewords for one u value.
type uvlcEntry struct {
	pre, preLen byte
	suf, sufLen byte
	ext, extLen byte
}

const numUVLCEntries = 75

var uvlcEncTbl [numUVLCEntries]uvlcEntry

// uvlcEntryFor returns the codeword triple for one u value. The table covers
// every u the block coder can produce — the largest magnitude exponent a
// 64-bit coefficient word admits is well inside it — and the bound is checked
// rather than assumed so that a coefficient outside the range the caller
// promised cannot index past the table.
func uvlcEntryFor(u int) uvlcEntry {
	if u < 0 || u >= numUVLCEntries {
		return uvlcEncTbl[numUVLCEntries-1]
	}
	return uvlcEncTbl[u]
}

func init() {
	// Codes run from 0 to 31; the extension covers larger values.
	uvlcEncTbl[0] = uvlcEntry{}
	uvlcEncTbl[1] = uvlcEntry{pre: 1, preLen: 1}
	uvlcEncTbl[2] = uvlcEntry{pre: 2, preLen: 2}
	uvlcEncTbl[3] = uvlcEntry{pre: 4, preLen: 3, suf: 0, sufLen: 1}
	uvlcEncTbl[4] = uvlcEntry{pre: 4, preLen: 3, suf: 1, sufLen: 1}
	for i := 5; i < 33; i++ {
		uvlcEncTbl[i] = uvlcEntry{pre: 0, preLen: 3, suf: byte(i - 5), sufLen: 5}
	}
	for i := 33; i < numUVLCEntries; i++ {
		uvlcEncTbl[i] = uvlcEntry{
			pre: 0, preLen: 3,
			suf: byte(28 + (i-33)%4), sufLen: 5,
			ext: byte((i - 33) / 4), extLen: 4,
		}
	}
}

// melEncoder emits the MEL bitstream, which grows forward.
type melEncoder struct {
	buf       []byte
	remaining int
	tmp       int
	run       int
	k         int
	threshold int
}

func newMELEncoder() *melEncoder {
	return &melEncoder{remaining: 8, threshold: 1 << melExp[0]}
}

func (m *melEncoder) emitBit(v int) {
	m.tmp = (m.tmp << 1) + v
	m.remaining--
	if m.remaining == 0 {
		m.buf = append(m.buf, byte(m.tmp))
		if m.tmp == 0xFF {
			m.remaining = 7
		} else {
			m.remaining = 8
		}
		m.tmp = 0
	}
}

func (m *melEncoder) encode(bit bool) {
	if !bit {
		m.run++
		if m.run >= m.threshold {
			m.emitBit(1)
			m.run = 0
			if m.k+1 < 12 {
				m.k++
			} else {
				m.k = 12
			}
			m.threshold = 1 << melExp[m.k]
		}
		return
	}
	m.emitBit(0)
	t := melExp[m.k]
	for t > 0 {
		t--
		m.emitBit((m.run >> t) & 1)
	}
	m.run = 0
	if m.k > 0 {
		m.k--
	}
	m.threshold = 1 << melExp[m.k]
}

// vlcEncoder emits the VLC bitstream, which grows backward from the end of the
// cleanup segment. Bytes are accumulated in order and reversed on assembly.
type vlcEncoder struct {
	buf               []byte
	usedBits          int
	tmp               int
	lastGreaterThan8F bool
}

func newVLCEncoder() *vlcEncoder {
	// The segment's final byte is 0xFF with its low nibble reserved, which is
	// what lets the decoder locate Scup.
	return &vlcEncoder{buf: []byte{0xFF}, usedBits: 4, tmp: 0xF, lastGreaterThan8F: true}
}

func (v *vlcEncoder) encode(cwd, cwdLen int) {
	for cwdLen > 0 {
		avail := 8 - v.usedBits
		if v.lastGreaterThan8F {
			avail--
		}
		t := avail
		if cwdLen < t {
			t = cwdLen
		}
		v.tmp |= (cwd & ((1 << uint(t)) - 1)) << uint(v.usedBits)
		v.usedBits += t
		avail -= t
		cwdLen -= t
		cwd >>= uint(t)
		if avail == 0 {
			if v.lastGreaterThan8F && v.tmp != 0x7F {
				v.lastGreaterThan8F = false
				continue // one empty bit remains
			}
			v.buf = append(v.buf, byte(v.tmp))
			v.lastGreaterThan8F = v.tmp > 0x8F
			v.tmp = 0
			v.usedBits = 0
		}
	}
}

// msEncoder emits the MagSgn bitstream, which grows forward.
type msEncoder struct {
	buf      []byte
	usedBits int
	tmp      int
	maxBits  int
}

func newMSEncoder() *msEncoder { return &msEncoder{maxBits: 8} }

// encode appends cwdLen bits of cwd. cwd is 64 bits wide because a binary32
// component carried through the NLT Type 3 point transform needs 33 to 35
// magnitude bit-planes after the reversible 5/3 transform, so a single
// MagSgn codeword can be wider than 32 bits.
func (m *msEncoder) encode(cwd uint64, cwdLen int) {
	for cwdLen > 0 {
		t := m.maxBits - m.usedBits
		if cwdLen < t {
			t = cwdLen
		}
		m.tmp |= int(cwd&(1<<uint(t)-1)) << uint(m.usedBits)
		m.usedBits += t
		cwd >>= uint(t)
		cwdLen -= t
		if m.usedBits >= m.maxBits {
			m.buf = append(m.buf, byte(m.tmp))
			if m.tmp == 0xFF {
				m.maxBits = 7
			} else {
				m.maxBits = 8
			}
			m.tmp = 0
			m.usedBits = 0
		}
	}
}

// terminateMELVLC flushes both streams, fusing their final partial bytes when
// the bits do not collide.
func terminateMELVLC(m *melEncoder, v *vlcEncoder) {
	if m.run > 0 {
		m.emitBit(1)
	}
	m.tmp <<= uint(m.remaining)
	melMask := (0xFF << uint(m.remaining)) & 0xFF
	vlcMask := 0xFF >> uint(8-v.usedBits)
	if (melMask | vlcMask) == 0 {
		return
	}
	fuse := m.tmp | v.tmp
	if ((fuse^m.tmp)&melMask|(fuse^v.tmp)&vlcMask) == 0 && fuse != 0xFF && len(v.buf) > 1 {
		m.buf = append(m.buf, byte(fuse))
		return
	}
	m.buf = append(m.buf, byte(m.tmp))
	v.buf = append(v.buf, byte(v.tmp))
}

// encodeCleanupHT encodes the cleanup pass of one code-block and returns the
// complete segment: MagSgn forward from the start, then MEL and VLC, with Scup
// in the final two bytes.
//
// data holds plain coefficients. p is the number of least significant bits to
// drop: the reference keeps samples pre-shifted into the top of a 32-bit word
// and uses p = 30 - missing_msbs, but with plain coefficients p = 0 encodes
// losslessly, which is what the matching decoder's (v_n + 2) >> 1 inverts.
// The type parameter is the width of the coefficient word. int32 covers every
// integer component the format carries; int64 is needed for a binary32
// component, whose NLT Type 3 samples fill the whole int32 range and grow to 33
// to 35 magnitude bits under the reversible 5/3 transform. The arithmetic below
// is 64-bit in both instantiations, so the two agree sample for sample wherever
// the narrow one is valid — TestWideAgreesWithNarrow asserts that.
func encodeCleanupHT[T int32 | int64](data []T, width, height, p int) []byte {
	if width <= 0 || height <= 0 || p < 0 {
		return nil
	}

	mel := newMELEncoder()
	vlc := newVLCEncoder()
	ms := newMSEncoder()

	// lep carries the exponent of the bottom row of each quad to the stripe
	// below; lcxp carries its significance.
	quadCols := (width+1)/2 + 4
	lep := make([]uint8, quadCols)
	lcxp := make([]uint8, quadCols)

	// sampleAt returns the coefficient as OpenJPH's sign-magnitude word, with
	// the sign in the top bit of a 64-bit word.
	sampleAt := func(x, y int) uint64 {
		if x >= width || y >= height {
			return 0
		}
		v := int64(data[y*width+x])
		if v < 0 {
			return uint64(-v) | 1<<63
		}
		return uint64(v)
	}

	for y := 0; y < height; y += 2 {
		initial := y == 0
		tbl := &vlcEncTbl0
		if !initial {
			tbl = &vlcEncTbl1
		}

		// Line state carried down from the stripe above. lep holds the
		// exponent of each quad's bottom row, lcxp its significance.
		lepIdx, lcxIdx := 0, 0
		maxE, cq0 := 0, 0
		if !initial {
			maxE = maxInt(int(lep[0]), int(lep[1])) - 1
			lep[0] = 0
			cq0 = int(lcxp[0]) + (int(lcxp[1]) << 2)
			lcxp[0] = 0
		}

		for x := 0; x < width; x += 4 {
			var rho [2]int
			var eq [8]int
			var eqmax [2]int
			var sv [8]uint64

			pairs := 1
			if x+2 < width {
				pairs = 2
			}

			for q := 0; q < pairs; q++ {
				for n := 0; n < 4; n++ {
					t := sampleAt(x+2*q+(n>>1), y+(n&1))
					val := t + t // multiply by two, discarding the sign
					val >>= uint(p)
					val &^= 1 // 2*mu_p
					if val == 0 {
						continue
					}
					rho[q] |= 1 << uint(n)
					val--
					eq[q*4+n] = 64 - bits.LeadingZeros64(val)
					if eq[q*4+n] > eqmax[q] {
						eqmax[q] = eq[q*4+n]
					}
					val--
					sv[q*4+n] = val + (t >> 63) // v_n = 2(mu_p - 1) + s_n
				}
			}

			var uqv [2]int
			for q := 0; q < pairs; q++ {
				// kappa is 1 on the initial stripe; below it rises with the
				// exponent of the quads above when this quad has more than one
				// significant sample.
				kappa := 1
				if !initial && rho[q]&(rho[q]-1) != 0 {
					kappa = maxInt(1, maxE)
				}
				uq := maxInt(eqmax[q], kappa)
				uqv[q] = uq - kappa

				eps := 0
				if uqv[q] > 0 {
					for n := 0; n < 4; n++ {
						if eq[q*4+n] == eqmax[q] {
							eps |= 1 << uint(n)
						}
					}
				}

				cq := cq0
				if q == 1 {
					if initial {
						cq = (rho[0] >> 1) | (rho[0] & 1)
					} else {
						cq = cq0
					}
				}

				tuple := tbl[(cq<<8)+(rho[q]<<4)+eps]
				vlc.encode(int(tuple>>8), int((tuple>>4)&7))
				if cq == 0 {
					mel.encode(rho[q] != 0)
				}

				for n := 0; n < 4; n++ {
					if rho[q]&(1<<uint(n)) == 0 {
						continue
					}
					m := uq - int((tuple>>uint(n))&1)
					if m > 0 {
						ms.encode(sv[q*4+n]&(1<<uint(m)-1), m)
					}
				}

				// Advance the line state, exactly as the reference does.
				if e := uint8(eq[q*4+1]); e > lep[lepIdx] {
					lep[lepIdx] = e
				}
				lepIdx++
				if !initial {
					maxE = maxInt(int(lep[lepIdx]), int(lep[lepIdx+1])) - 1
				}
				lep[lepIdx] = uint8(eq[q*4+3])

				lcxp[lcxIdx] |= uint8((rho[q] & 2) >> 1)
				lcxIdx++
				if initial {
					cq0 = (rho[q] >> 1) | (rho[q] & 1)
				} else {
					cq0 = int(lcxp[lcxIdx]) + (int(lcxp[lcxIdx+1]) << 2)
					cq0 |= ((rho[q] & 4) >> 1) | ((rho[q] & 8) >> 2)
				}
				lcxp[lcxIdx] = uint8((rho[q] & 8) >> 3)
			}

			// u values for the pair. The extra MEL event and the two
			// "u > 2" prefix modes exist only on the initial stripe: below
			// it, kappa comes from the context, so a conforming decoder
			// reads mode 3 as two plain prefixes plus two suffixes
			// (decode_noninit_uvlc) and never consults MEL for u.
			u0, u1 := uqv[0], uqv[1]
			if initial && u0 > 0 && u1 > 0 {
				mel.encode(min2(u0, u1) > 2)
			}
			//
			// The four-bit extension after the suffixes is what carries u
			// above 32. Without it the codeword for such a u is the codeword
			// for u mod 4 + 33, so every quad whose magnitude exponent passes
			// 32 decodes with the wrong number of magnitude bits and the
			// MagSgn stream desynchronises from there on. Only a component
			// wide enough to reach that exponent — a binary32 one — can
			// produce it, which is why the omission survived.
			switch {
			case initial && u0 > 2 && u1 > 2:
				e0, e1 := uvlcEntryFor(u0-2), uvlcEntryFor(u1-2)
				vlc.encode(int(e0.pre), int(e0.preLen))
				vlc.encode(int(e1.pre), int(e1.preLen))
				vlc.encode(int(e0.suf), int(e0.sufLen))
				vlc.encode(int(e1.suf), int(e1.sufLen))
				vlc.encode(int(e0.ext), int(e0.extLen))
				vlc.encode(int(e1.ext), int(e1.extLen))
			case initial && u0 > 2 && u1 > 0:
				e0 := uvlcEntryFor(u0)
				vlc.encode(int(e0.pre), int(e0.preLen))
				vlc.encode(u1-1, 1)
				vlc.encode(int(e0.suf), int(e0.sufLen))
				vlc.encode(int(e0.ext), int(e0.extLen))
			default:
				e0, e1 := uvlcEntryFor(u0), uvlcEntryFor(u1)
				vlc.encode(int(e0.pre), int(e0.preLen))
				vlc.encode(int(e1.pre), int(e1.preLen))
				vlc.encode(int(e0.suf), int(e0.sufLen))
				vlc.encode(int(e1.suf), int(e1.sufLen))
				vlc.encode(int(e0.ext), int(e0.extLen))
				vlc.encode(int(e1.ext), int(e1.extLen))
			}
		}
	}

	terminateMELVLC(mel, vlc)
	ms.terminate()

	// Assemble: MagSgn forward, then MEL, then VLC reversed, with Scup last.
	melvlc := len(mel.buf) + len(vlc.buf)
	out := make([]byte, 0, len(ms.buf)+melvlc)
	out = append(out, ms.buf...)
	out = append(out, mel.buf...)
	for i := len(vlc.buf) - 1; i >= 0; i-- {
		out = append(out, vlc.buf[i])
	}

	// Scup is the size of the MEL+VLC region and is written into bits the VLC
	// encoder reserved for it, not appended: the final byte holds its high
	// bits and the low nibble of the one before holds the rest. This is the
	// exact inverse of the decoder's
	// scup = (data[Lcup-1] << 4) | (data[Lcup-2] & 0x0F).
	n := len(out)
	if n < 2 {
		return nil
	}
	out[n-1] = byte(melvlc >> 4)
	out[n-2] = (out[n-2] & 0xF0) | byte(melvlc&0x0F)
	return out
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// terminate flushes the MagSgn stream's final partial byte, padding the unused
// bits with ones. A byte that would come out 0xFF is dropped instead, since
// 0xFF is reserved by the bit-stuffing rule; and a stream that ended exactly on
// a stuffed byte gives that byte back.
//
// Omitting this left the MagSgn segment one byte short, which moved the
// MEL+VLC boundary and made Scup disagree with the reference by one.
func (m *msEncoder) terminate() {
	if m.usedBits != 0 {
		t := m.maxBits - m.usedBits
		m.tmp |= (0xFF & ((1 << uint(t)) - 1)) << uint(m.usedBits)
		m.usedBits += t
		if m.tmp != 0xFF {
			m.buf = append(m.buf, byte(m.tmp))
		}
		return
	}
	if m.maxBits == 7 && len(m.buf) > 0 {
		m.buf = m.buf[:len(m.buf)-1]
	}
}
