// Package entropy - t1.go implements Tier-1 (EBCOT) coding.
//
// EBCOT (Embedded Block Coding with Optimized Truncation) is the
// entropy coding algorithm used in JPEG 2000. It operates on
// code-blocks (typically 64x64 or 32x32) and produces embedded
// bit-streams that can be truncated at various points.
package entropy

import (
	"math"
	"sync"
)

// t1Pool provides pooled T1 encoders to reduce allocations.
var t1Pool = sync.Pool{
	New: func() interface{} {
		// Create a T1 with max typical code-block size (64x64)
		t := &T1{
			width:  64,
			height: 64,
			data:   make([]int32, 64*64),
			flags:  make([]T1Flags, (64+2)*(64+2)),
			mqEnc:  NewMQEncoder(),
			mqBuf:  make([]byte, 1, 8192),
		}
		t.mqBuf[0] = 0
		return t
	},
}

// GetT1 returns a pooled T1 encoder, resizing if necessary.
func GetT1(width, height int) *T1 {
	t := t1Pool.Get().(*T1)
	t.resize(width, height)
	return t
}

// PutT1 returns a T1 encoder to the pool.
func PutT1(t *T1) {
	t1Pool.Put(t)
}

// Resize adjusts the T1 to the given dimensions and clears state.
// This is the public version for reusing T1 across multiple jobs.
func (t *T1) Resize(width, height int) {
	t.resize(width, height)
}

// resize adjusts the T1 to the given dimensions and clears state.
func (t *T1) resize(width, height int) {
	t.width = width
	t.height = height

	dataSize := width * height
	if cap(t.data) < dataSize {
		t.data = make([]int32, dataSize)
	} else {
		t.data = t.data[:dataSize]
	}

	flagsSize := (width + 2) * (height + 2)
	if cap(t.flags) < flagsSize {
		t.flags = make([]T1Flags, flagsSize)
	} else {
		t.flags = t.flags[:flagsSize]
		// Clear flags from previous use using SIMD when available
		clearFlagsFast(t.flags)
	}
}

// T1Flags contains the significance and refinement state for a coefficient.
type T1Flags uint8

const (
	// T1Sig indicates the coefficient is significant.
	T1Sig T1Flags = 1 << iota
	// T1Visit indicates the coefficient has been visited in this pass.
	T1Visit
	// T1Refine indicates the coefficient needs refinement.
	T1Refine
	// T1SignNeg indicates the coefficient is negative.
	T1SignNeg
	// T1SigN indicates north neighbor is significant.
	T1SigN
	// T1SigS indicates south neighbor is significant.
	T1SigS
	// T1SigE indicates east neighbor is significant.
	T1SigE
	// T1SigW indicates west neighbor is significant.
	T1SigW
)

// T1 implements Tier-1 EBCOT coding.
type T1 struct {
	// Code-block dimensions
	width  int
	height int

	// Coefficient data (absolute values)
	data []int32

	// Flags for each coefficient
	flags []T1Flags

	// MQ coder
	mqEnc *MQEncoder
	mqDec *MQDecoder

	// Band type (LL, HL, LH, HH)
	bandType int

	// Number of bit-planes
	numBPS int

	// Inlined MQ encoder state for hot path
	mqA        uint32
	mqC        uint32
	mqCT       uint32
	mqBuf      []byte
	mqBp       int
	mqContexts [NumContexts]uint8
}

// Band type constants.
const (
	BandLL = iota
	BandHL
	BandLH
	BandHH
)

// NewT1 creates a new T1 encoder/decoder.
func NewT1(width, height int) *T1 {
	t := &T1{
		width:  width,
		height: height,
		data:   make([]int32, width*height),
		flags:  make([]T1Flags, (width+2)*(height+2)), // Include border
		mqEnc:  NewMQEncoder(),
		mqBuf:  make([]byte, 1, 8192),
	}
	t.mqBuf[0] = 0
	return t
}

// Reset resets the T1 state for a new code-block.
func (t *T1) Reset() {
	for i := range t.data {
		t.data[i] = 0
	}
	for i := range t.flags {
		t.flags[i] = 0
	}
	t.mqEnc.Reset()
}

// resetMQInlined resets the inlined MQ encoder state.
func (t *T1) resetMQInlined() {
	t.mqA = 0x8000
	t.mqC = 0
	t.mqCT = 12
	if cap(t.mqBuf) > 0 {
		t.mqBuf = t.mqBuf[:1]
	} else {
		t.mqBuf = make([]byte, 1, 8192)
	}
	t.mqBuf[0] = 0
	t.mqBp = 0
	for i := range t.mqContexts {
		t.mqContexts[i] = 0
	}
	t.mqContexts[CtxUni] = 92
}

// mqEncodeInlined is an inlined MQ encode for maximum performance.
// This avoids method call overhead on the hot path.
func (t *T1) mqEncodeInlined(ctx int, decision int) {
	stateIdx := t.mqContexts[ctx]
	qe := mqQe[stateIdx]
	mps := stateIdx & 1

	t.mqA -= qe

	if uint8(decision) == mps {
		// MPS path
		if (t.mqA & 0x8000) == 0 {
			if t.mqA < qe {
				t.mqA = qe
			} else {
				t.mqC += qe
			}
			t.mqContexts[ctx] = mqNMPS[stateIdx]
			t.mqRenormInlined()
		} else {
			t.mqC += qe
		}
	} else {
		// LPS path
		if t.mqA < qe {
			t.mqC += qe
		} else {
			t.mqA = qe
		}
		t.mqContexts[ctx] = mqNLPS[stateIdx]
		t.mqRenormInlined()
	}
}

// mqRenormInlined performs inlined encoder renormalization.
func (t *T1) mqRenormInlined() {
	for (t.mqA & 0x8000) == 0 {
		t.mqA <<= 1
		t.mqC <<= 1
		t.mqCT--
		if t.mqCT == 0 {
			t.mqByteOutInlined()
		}
	}
}

// mqByteOutInlined outputs a byte with bit stuffing (inlined version).
func (t *T1) mqByteOutInlined() {
	if t.mqBuf[t.mqBp] == 0xFF {
		t.mqBp++
		if t.mqBp >= len(t.mqBuf) {
			t.mqBuf = append(t.mqBuf, 0)
		}
		t.mqBuf[t.mqBp] = byte(t.mqC >> 20)
		t.mqC &= 0xFFFFF
		t.mqCT = 7
	} else {
		if (t.mqC & 0x8000000) == 0 {
			t.mqBp++
			if t.mqBp >= len(t.mqBuf) {
				t.mqBuf = append(t.mqBuf, 0)
			}
			t.mqBuf[t.mqBp] = byte(t.mqC >> 19)
			t.mqC &= 0x7FFFF
			t.mqCT = 8
		} else {
			t.mqBuf[t.mqBp]++
			if t.mqBuf[t.mqBp] == 0xFF {
				t.mqC &= 0x7FFFFFF
				t.mqBp++
				if t.mqBp >= len(t.mqBuf) {
					t.mqBuf = append(t.mqBuf, 0)
				}
				t.mqBuf[t.mqBp] = byte(t.mqC >> 20)
				t.mqC &= 0xFFFFF
				t.mqCT = 7
			} else {
				t.mqBp++
				if t.mqBp >= len(t.mqBuf) {
					t.mqBuf = append(t.mqBuf, 0)
				}
				t.mqBuf[t.mqBp] = byte(t.mqC >> 19)
				t.mqC &= 0x7FFFF
				t.mqCT = 8
			}
		}
	}
}

// mqFlushInlined flushes the inlined MQ encoder and returns the data.
func (t *T1) mqFlushInlined() []byte {
	// setbits
	tempC := t.mqC + t.mqA
	t.mqC |= 0xFFFF
	if t.mqC >= tempC {
		t.mqC -= 0x8000
	}

	t.mqC <<= t.mqCT
	t.mqByteOutInlined()
	t.mqC <<= t.mqCT
	t.mqByteOutInlined()

	endPos := t.mqBp + 1
	if endPos > 0 && t.mqBuf[endPos-1] == 0xFF {
		endPos--
	}

	if endPos > 1 {
		return t.mqBuf[1:endPos]
	}
	return nil
}

// SetData sets the coefficient data for encoding.
// Signs are stored separately in flags.
// Note: Flags must be pre-cleared by resize() before calling this.
func (t *T1) SetData(data []int32) {
	width := t.width
	flags := t.flags
	copy(t.data, data)
	for i, v := range t.data {
		if v < 0 {
			t.data[i] = -v
			// Inline setFlag for performance
			idx := (i/width+1)*(width+2) + (i%width + 1)
			flags[idx] |= T1SignNeg
		}
	}
}

// flagIndex returns the index into the flags array.
// Flags array has a 1-pixel border around the code-block.
func (t *T1) flagIndex(x, y int) int {
	return (y+1)*(t.width+2) + (x + 1)
}

// setFlag sets a flag for the coefficient at (x, y).
func (t *T1) setFlag(x, y int, flag T1Flags) {
	t.flags[t.flagIndex(x, y)] |= flag
}

// hasFlag checks if a flag is set.
func (t *T1) hasFlag(x, y int, flag T1Flags) bool {
	return t.flags[t.flagIndex(x, y)]&flag != 0
}

// clearFlag clears a flag.
func (t *T1) clearFlag(x, y int, flag T1Flags) {
	t.flags[t.flagIndex(x, y)] &^= flag
}

// updateNeighborFlags updates neighbor significance flags.
func (t *T1) updateNeighborFlags(x, y int) {
	idx := t.flagIndex(x, y)
	stride := t.width + 2

	// Set significance flags in neighbors
	if y > 0 {
		t.flags[idx-stride] |= T1SigS
	}
	if y < t.height-1 {
		t.flags[idx+stride] |= T1SigN
	}
	if x > 0 {
		t.flags[idx-1] |= T1SigE
	}
	if x < t.width-1 {
		t.flags[idx+1] |= T1SigW
	}
}

// getZCContext returns the zero coding context based on neighbor significance.
// Uses lookup table for O(1) context calculation.
func (t *T1) getZCContext(x, y int, bandType int) int {
	idx := t.flagIndex(x, y)
	stride := t.width + 2
	f := t.flags

	// Pack neighbor significance into 8-bit index:
	// bit 0: W, bit 1: E, bit 2: N, bit 3: S
	// bit 4: NW, bit 5: NE, bit 6: SW, bit 7: SE
	var packed uint8
	if f[idx-1]&T1Sig != 0 {
		packed |= 0x01 // W
	}
	if f[idx+1]&T1Sig != 0 {
		packed |= 0x02 // E
	}
	if f[idx-stride]&T1Sig != 0 {
		packed |= 0x04 // N
	}
	if f[idx+stride]&T1Sig != 0 {
		packed |= 0x08 // S
	}
	if f[idx-stride-1]&T1Sig != 0 {
		packed |= 0x10 // NW
	}
	if f[idx-stride+1]&T1Sig != 0 {
		packed |= 0x20 // NE
	}
	if f[idx+stride-1]&T1Sig != 0 {
		packed |= 0x40 // SW
	}
	if f[idx+stride+1]&T1Sig != 0 {
		packed |= 0x80 // SE
	}

	return int(lutZCCtx[bandType*256+int(packed)])
}

// getSCContext returns the sign coding context and prediction.
func (t *T1) getSCContext(x, y int) (ctx int, pred int) {
	idx := t.flagIndex(x, y)
	stride := t.width + 2
	f := t.flags

	// Compute sign contribution from horizontal neighbors
	hc := 0
	if f[idx-1]&T1Sig != 0 {
		if f[idx-1]&T1SignNeg != 0 {
			hc--
		} else {
			hc++
		}
	}
	if f[idx+1]&T1Sig != 0 {
		if f[idx+1]&T1SignNeg != 0 {
			hc--
		} else {
			hc++
		}
	}

	// Compute sign contribution from vertical neighbors
	vc := 0
	if f[idx-stride]&T1Sig != 0 {
		if f[idx-stride]&T1SignNeg != 0 {
			vc--
		} else {
			vc++
		}
	}
	if f[idx+stride]&T1Sig != 0 {
		if f[idx+stride]&T1SignNeg != 0 {
			vc--
		} else {
			vc++
		}
	}

	// Determine context and prediction from contributions
	pred = 0
	if hc < 0 {
		pred = 1
		hc = -hc
	}
	if hc == 0 {
		if vc < 0 {
			pred = 1
			vc = -vc
		}
	}

	// Map to context
	ctx = CtxSC0
	if hc == 1 {
		if vc == 1 {
			ctx = CtxSC4
		} else if vc == 0 {
			ctx = CtxSC2
		} else {
			ctx = CtxSC1
		}
	} else if hc == 0 {
		if vc == 1 {
			ctx = CtxSC1
		} else if vc == 0 {
			ctx = CtxSC0
		}
	} else if hc == 2 {
		ctx = CtxSC3
	}

	return
}

// getMRContext returns the magnitude refinement context.
func (t *T1) getMRContext(x, y int) int {
	idx := t.flagIndex(x, y)
	stride := t.width + 2
	f := t.flags

	// Check if this is the first refinement
	if f[idx]&T1Refine == 0 {
		// Check if any neighbor is significant
		hasNeighbor := (f[idx-1]|f[idx+1]|f[idx-stride]|f[idx+stride]|
			f[idx-stride-1]|f[idx-stride+1]|f[idx+stride-1]|f[idx+stride+1])&T1Sig != 0
		if hasNeighbor {
			return CtxMag1
		}
		return CtxMag0
	}
	return CtxMag2
}

// encodeSignInlined encodes sign using inlined MQ encoder.
func (t *T1) encodeSignInlined(x, y int) {
	idx := t.flagIndex(x, y)
	stride := t.width + 2
	f := t.flags

	// Compute sign contribution from horizontal neighbors
	hc := 0
	if f[idx-1]&T1Sig != 0 {
		if f[idx-1]&T1SignNeg != 0 {
			hc--
		} else {
			hc++
		}
	}
	if f[idx+1]&T1Sig != 0 {
		if f[idx+1]&T1SignNeg != 0 {
			hc--
		} else {
			hc++
		}
	}

	// Compute sign contribution from vertical neighbors
	vc := 0
	if f[idx-stride]&T1Sig != 0 {
		if f[idx-stride]&T1SignNeg != 0 {
			vc--
		} else {
			vc++
		}
	}
	if f[idx+stride]&T1Sig != 0 {
		if f[idx+stride]&T1SignNeg != 0 {
			vc--
		} else {
			vc++
		}
	}

	// Determine prediction
	pred := 0
	if hc < 0 {
		pred = 1
		hc = -hc
	}
	if hc == 0 && vc < 0 {
		pred = 1
		vc = -vc
	}

	// Map to context
	ctx := CtxSC0
	if hc == 1 {
		if vc == 1 {
			ctx = CtxSC4
		} else if vc == 0 {
			ctx = CtxSC2
		} else {
			ctx = CtxSC1
		}
	} else if hc == 0 {
		if vc == 1 {
			ctx = CtxSC1
		}
	} else if hc == 2 {
		ctx = CtxSC3
	}

	sign := 0
	if f[idx]&T1SignNeg != 0 {
		sign = 1
	}
	t.mqEncodeInlined(ctx, sign^pred)
}

// encodeSignificancePassInlined uses inlined MQ encoding.
func (t *T1) encodeSignificancePassInlined(bp int) {
	bit := int32(1) << bp
	stride := t.width + 2
	flags := t.flags
	data := t.data
	width := t.width
	height := t.height
	bandOffset := t.bandType * 256

	for y := 0; y < height; y++ {
		rowIdx := (y + 1) * stride
		dataRowIdx := y * width
		isFirstRow := y == 0
		isLastRow := y == height-1

		for x := 0; x < width; x++ {
			i := rowIdx + x + 1
			f := flags[i]

			if f&T1Sig != 0 {
				continue
			}

			neighbors := flags[i-1] | flags[i+1] | flags[i-stride] | flags[i+stride] |
				flags[i-stride-1] | flags[i-stride+1] | flags[i+stride-1] | flags[i+stride+1]
			if neighbors&T1Sig == 0 {
				continue
			}

			sig := 0
			if data[dataRowIdx+x]&bit != 0 {
				sig = 1
			}

			var packed uint8
			if flags[i-1]&T1Sig != 0 {
				packed |= 0x01
			}
			if flags[i+1]&T1Sig != 0 {
				packed |= 0x02
			}
			if flags[i-stride]&T1Sig != 0 {
				packed |= 0x04
			}
			if flags[i+stride]&T1Sig != 0 {
				packed |= 0x08
			}
			if flags[i-stride-1]&T1Sig != 0 {
				packed |= 0x10
			}
			if flags[i-stride+1]&T1Sig != 0 {
				packed |= 0x20
			}
			if flags[i+stride-1]&T1Sig != 0 {
				packed |= 0x40
			}
			if flags[i+stride+1]&T1Sig != 0 {
				packed |= 0x80
			}
			ctx := int(lutZCCtx[bandOffset+int(packed)])
			t.mqEncodeInlined(ctx, sig)

			if sig != 0 {
				t.encodeSignInlined(x, y)
				flags[i] |= T1Sig
				if !isFirstRow {
					flags[i-stride] |= T1SigS
				}
				if !isLastRow {
					flags[i+stride] |= T1SigN
				}
				if x > 0 {
					flags[i-1] |= T1SigE
				}
				if x < width-1 {
					flags[i+1] |= T1SigW
				}
			}
			flags[i] |= T1Visit
		}
	}
}

// encodeMagnitudeRefinementPassInlined uses inlined MQ encoding.
func (t *T1) encodeMagnitudeRefinementPassInlined(bp int) {
	bit := int32(1) << bp
	stride := t.width + 2
	flags := t.flags
	data := t.data
	width := t.width
	height := t.height

	for y := 0; y < height; y++ {
		rowIdx := (y + 1) * stride
		dataRowIdx := y * width
		for x := 0; x < width; x++ {
			idx := rowIdx + x + 1
			f := flags[idx]

			if f&T1Sig == 0 || f&T1Visit != 0 {
				continue
			}

			refBit := 0
			if data[dataRowIdx+x]&bit != 0 {
				refBit = 1
			}

			var ctx int
			if f&T1Refine == 0 {
				neighbors := flags[idx-1] | flags[idx+1] | flags[idx-stride] | flags[idx+stride] |
					flags[idx-stride-1] | flags[idx-stride+1] | flags[idx+stride-1] | flags[idx+stride+1]
				if neighbors&T1Sig != 0 {
					ctx = CtxMag1
				} else {
					ctx = CtxMag0
				}
			} else {
				ctx = CtxMag2
			}

			t.mqEncodeInlined(ctx, refBit)
			flags[idx] |= T1Refine
		}
	}
}

// encodeCleanupPassInlined uses inlined MQ encoding.
func (t *T1) encodeCleanupPassInlined(bp int) {
	bit := int32(1) << bp
	stride := t.width + 2
	flags := t.flags
	data := t.data
	width := t.width
	height := t.height
	bandOffset := t.bandType * 256

	for y := 0; y < height; y += 4 {
		for x := 0; x < width; x++ {
			// Check for run-length coding opportunity
			if t.canUseRunLengthInlined(x, y, bp, stride, flags) {
				t.encodeRunLengthInlined(x, y, bp, bit, stride, flags, data, bandOffset)
				continue
			}

			// Regular cleanup coding
			for yy := y; yy < y+4 && yy < height; yy++ {
				idx := (yy+1)*stride + x + 1
				f := flags[idx]

				if f&T1Visit != 0 {
					flags[idx] &^= T1Visit
					continue
				}
				if f&T1Sig != 0 {
					continue
				}

				sig := 0
				if data[yy*width+x]&bit != 0 {
					sig = 1
				}

				// Inline ZC context
				var packed uint8
				if flags[idx-1]&T1Sig != 0 {
					packed |= 0x01
				}
				if flags[idx+1]&T1Sig != 0 {
					packed |= 0x02
				}
				if flags[idx-stride]&T1Sig != 0 {
					packed |= 0x04
				}
				if flags[idx+stride]&T1Sig != 0 {
					packed |= 0x08
				}
				if flags[idx-stride-1]&T1Sig != 0 {
					packed |= 0x10
				}
				if flags[idx-stride+1]&T1Sig != 0 {
					packed |= 0x20
				}
				if flags[idx+stride-1]&T1Sig != 0 {
					packed |= 0x40
				}
				if flags[idx+stride+1]&T1Sig != 0 {
					packed |= 0x80
				}
				ctx := int(lutZCCtx[bandOffset+int(packed)])
				t.mqEncodeInlined(ctx, sig)

				if sig != 0 {
					t.encodeSignInlined(x, yy)
					flags[idx] |= T1Sig
					// Update neighbor flags
					if yy > 0 {
						flags[idx-stride] |= T1SigS
					}
					if yy < height-1 {
						flags[idx+stride] |= T1SigN
					}
					if x > 0 {
						flags[idx-1] |= T1SigE
					}
					if x < width-1 {
						flags[idx+1] |= T1SigW
					}
				}
			}
		}
	}
}

// canUseRunLengthInlined checks if run-length coding can be used.
// Optimized to reduce memory accesses by checking all 4 positions at once.
func (t *T1) canUseRunLengthInlined(x, y, bp, stride int, flags []T1Flags) bool {
	if y+4 > t.height {
		return false
	}

	// Calculate base index for first position
	idx0 := (y+1)*stride + x + 1
	idx1 := idx0 + stride
	idx2 := idx1 + stride
	idx3 := idx2 + stride

	// Check all 4 positions for T1Sig|T1Visit first (fast check)
	f0, f1, f2, f3 := flags[idx0], flags[idx1], flags[idx2], flags[idx3]
	combined := f0 | f1 | f2 | f3
	if combined&(T1Sig|T1Visit) != 0 {
		return false
	}

	// Check neighbors - combine checks to reduce branches
	// Left and right neighbors (shared across rows)
	left := flags[idx0-1] | flags[idx1-1] | flags[idx2-1] | flags[idx3-1]
	right := flags[idx0+1] | flags[idx1+1] | flags[idx2+1] | flags[idx3+1]
	if (left|right)&T1Sig != 0 {
		return false
	}

	// North neighbors (above row y)
	n := flags[idx0-stride] | flags[idx0-stride-1] | flags[idx0-stride+1]
	if n&T1Sig != 0 {
		return false
	}

	// South neighbors (below row y+3)
	s := flags[idx3+stride] | flags[idx3+stride-1] | flags[idx3+stride+1]
	if s&T1Sig != 0 {
		return false
	}

	return true
}

// encodeRunLengthInlined encodes run-length with inlined MQ.
func (t *T1) encodeRunLengthInlined(x, y, bp int, bit int32, stride int, flags []T1Flags, data []int32, bandOffset int) {
	width := t.width
	height := t.height

	// Find first significant
	firstSig := -1
	for i := 0; i < 4; i++ {
		if y+i >= height {
			break
		}
		if data[(y+i)*width+x]&bit != 0 {
			firstSig = i
			break
		}
	}

	if firstSig == -1 {
		t.mqEncodeInlined(CtxRL, 0)
		return
	}

	t.mqEncodeInlined(CtxRL, 1)
	t.mqEncodeInlined(CtxUni, (firstSig>>1)&1)
	t.mqEncodeInlined(CtxUni, firstSig&1)

	// Encode sign and set flags
	yy := y + firstSig
	idx := (yy+1)*stride + x + 1
	t.encodeSignInlined(x, yy)
	flags[idx] |= T1Sig
	if yy > 0 {
		flags[idx-stride] |= T1SigS
	}
	if yy < height-1 {
		flags[idx+stride] |= T1SigN
	}
	if x > 0 {
		flags[idx-1] |= T1SigE
	}
	if x < width-1 {
		flags[idx+1] |= T1SigW
	}

	// Continue with remaining positions
	for i := firstSig + 1; i < 4 && y+i < height; i++ {
		yy := y + i
		idx := (yy+1)*stride + x + 1

		sig := 0
		if data[yy*width+x]&bit != 0 {
			sig = 1
		}

		var packed uint8
		if flags[idx-1]&T1Sig != 0 {
			packed |= 0x01
		}
		if flags[idx+1]&T1Sig != 0 {
			packed |= 0x02
		}
		if flags[idx-stride]&T1Sig != 0 {
			packed |= 0x04
		}
		if flags[idx+stride]&T1Sig != 0 {
			packed |= 0x08
		}
		if flags[idx-stride-1]&T1Sig != 0 {
			packed |= 0x10
		}
		if flags[idx-stride+1]&T1Sig != 0 {
			packed |= 0x20
		}
		if flags[idx+stride-1]&T1Sig != 0 {
			packed |= 0x40
		}
		if flags[idx+stride+1]&T1Sig != 0 {
			packed |= 0x80
		}
		ctx := int(lutZCCtx[bandOffset+int(packed)])
		t.mqEncodeInlined(ctx, sig)

		if sig != 0 {
			t.encodeSignInlined(x, yy)
			flags[idx] |= T1Sig
			if yy > 0 {
				flags[idx-stride] |= T1SigS
			}
			if yy < height-1 {
				flags[idx+stride] |= T1SigN
			}
			if x > 0 {
				flags[idx-1] |= T1SigE
			}
			if x < width-1 {
				flags[idx+1] |= T1SigW
			}
		}
	}
}

// Encode encodes a code-block and returns the bit-stream.
// Uses fully inlined MQ encoding for maximum performance.
func (t *T1) Encode(bandType int) []byte {
	return t.EncodeFast5(bandType)
}

// EncodeSafe encodes a code-block without unsafe optimizations.
func (t *T1) EncodeSafe(bandType int) []byte {
	t.bandType = bandType
	t.resetMQInlined() // Use inlined MQ encoder for better performance

	// Find number of bit-planes
	maxVal := int32(0)
	for _, v := range t.data {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		return nil
	}
	t.numBPS = int(math.Ceil(math.Log2(float64(maxVal + 1))))

	// Encode each bit-plane
	for bp := t.numBPS - 1; bp >= 0; bp-- {
		t.encodeSignificancePassInlined(bp)
		t.encodeMagnitudeRefinementPassInlined(bp)
		t.encodeCleanupPassInlined(bp)
	}

	return t.mqFlushInlined()
}

// encodeSignificancePass encodes the significance propagation pass.
func (t *T1) encodeSignificancePass(bp int) {
	bit := int32(1) << bp
	stride := t.width + 2
	flags := t.flags
	data := t.data
	width := t.width
	height := t.height
	bandType := t.bandType
	bandOffset := bandType * 256 // Pre-calculate for LUT

	for y := 0; y < height; y++ {
		rowIdx := (y + 1) * stride
		dataRowIdx := y * width
		isFirstRow := y == 0
		isLastRow := y == height-1

		// Process 4 coefficients at a time (unrolled)
		x := 0
		for ; x+4 <= width; x += 4 {
			idx := rowIdx + x + 1

			// Process 4 coefficients
			for dx := 0; dx < 4; dx++ {
				i := idx + dx
				f := flags[i]

				if f&T1Sig != 0 {
					continue
				}

				// Check significant neighbors
				neighbors := flags[i-1] | flags[i+1] | flags[i-stride] | flags[i+stride] |
					flags[i-stride-1] | flags[i-stride+1] | flags[i+stride-1] | flags[i+stride+1]
				if neighbors&T1Sig == 0 {
					continue
				}

				// Encode significance bit
				sig := 0
				if data[dataRowIdx+x+dx]&bit != 0 {
					sig = 1
				}

				// Inline LUT context lookup
				var packed uint8
				if flags[i-1]&T1Sig != 0 {
					packed |= 0x01
				}
				if flags[i+1]&T1Sig != 0 {
					packed |= 0x02
				}
				if flags[i-stride]&T1Sig != 0 {
					packed |= 0x04
				}
				if flags[i+stride]&T1Sig != 0 {
					packed |= 0x08
				}
				if flags[i-stride-1]&T1Sig != 0 {
					packed |= 0x10
				}
				if flags[i-stride+1]&T1Sig != 0 {
					packed |= 0x20
				}
				if flags[i+stride-1]&T1Sig != 0 {
					packed |= 0x40
				}
				if flags[i+stride+1]&T1Sig != 0 {
					packed |= 0x80
				}
				ctx := int(lutZCCtx[bandOffset+int(packed)])
				t.mqEnc.Encode(ctx, sig)

				if sig != 0 {
					t.encodeSign(x+dx, y)
					flags[i] |= T1Sig
					if !isFirstRow {
						flags[i-stride] |= T1SigS
					}
					if !isLastRow {
						flags[i+stride] |= T1SigN
					}
					if x+dx > 0 {
						flags[i-1] |= T1SigE
					}
					if x+dx < width-1 {
						flags[i+1] |= T1SigW
					}
				}
				flags[i] |= T1Visit
			}
		}

		// Handle remaining coefficients
		for ; x < width; x++ {
			idx := rowIdx + x + 1
			f := flags[idx]

			if f&T1Sig != 0 {
				continue
			}

			neighbors := flags[idx-1] | flags[idx+1] | flags[idx-stride] | flags[idx+stride] |
				flags[idx-stride-1] | flags[idx-stride+1] | flags[idx+stride-1] | flags[idx+stride+1]
			if neighbors&T1Sig == 0 {
				continue
			}

			sig := 0
			if data[dataRowIdx+x]&bit != 0 {
				sig = 1
			}

			ctx := t.getZCContext(x, y, bandType)
			t.mqEnc.Encode(ctx, sig)

			if sig != 0 {
				t.encodeSign(x, y)
				flags[idx] |= T1Sig
				if !isFirstRow {
					flags[idx-stride] |= T1SigS
				}
				if !isLastRow {
					flags[idx+stride] |= T1SigN
				}
				if x > 0 {
					flags[idx-1] |= T1SigE
				}
				if x < width-1 {
					flags[idx+1] |= T1SigW
				}
			}
			flags[idx] |= T1Visit
		}
	}
}

// hasSignificantNeighbor checks if any neighbor is significant.
func (t *T1) hasSignificantNeighbor(x, y int) bool {
	idx := t.flagIndex(x, y)
	stride := t.width + 2
	return (t.flags[idx-1]|t.flags[idx+1]|t.flags[idx-stride]|t.flags[idx+stride]|
		t.flags[idx-stride-1]|t.flags[idx-stride+1]|t.flags[idx+stride-1]|t.flags[idx+stride+1])&T1Sig != 0
}

// encodeSign encodes the sign of a newly significant coefficient.
func (t *T1) encodeSign(x, y int) {
	ctx, pred := t.getSCContext(x, y)
	sign := 0
	if t.hasFlag(x, y, T1SignNeg) {
		sign = 1
	}
	t.mqEnc.Encode(ctx, sign^pred)
}

// encodeMagnitudeRefinementPass encodes magnitude refinement.
func (t *T1) encodeMagnitudeRefinementPass(bp int) {
	bit := int32(1) << bp
	stride := t.width + 2
	flags := t.flags
	data := t.data
	width := t.width
	height := t.height

	for y := 0; y < height; y++ {
		rowIdx := (y + 1) * stride
		dataRowIdx := y * width
		for x := 0; x < width; x++ {
			idx := rowIdx + x + 1
			f := flags[idx]

			// Only process coefficients that are significant and not visited
			if f&T1Sig == 0 || f&T1Visit != 0 {
				continue
			}

			// Encode refinement bit
			refBit := 0
			if data[dataRowIdx+x]&bit != 0 {
				refBit = 1
			}

			// Get MR context (inlined)
			var ctx int
			if f&T1Refine == 0 {
				// Check if any neighbor is significant
				neighbors := flags[idx-1] | flags[idx+1] | flags[idx-stride] | flags[idx+stride] |
					flags[idx-stride-1] | flags[idx-stride+1] | flags[idx+stride-1] | flags[idx+stride+1]
				if neighbors&T1Sig != 0 {
					ctx = CtxMag1
				} else {
					ctx = CtxMag0
				}
			} else {
				ctx = CtxMag2
			}

			t.mqEnc.Encode(ctx, refBit)
			flags[idx] |= T1Refine
		}
	}
}

// encodeCleanupPass encodes the cleanup pass.
func (t *T1) encodeCleanupPass(bp int) {
	bit := int32(1) << bp

	for y := 0; y < t.height; y += 4 {
		for x := 0; x < t.width; x++ {
			// Check for run-length coding opportunity
			if t.canUseRunLength(x, y, bp) {
				// Encode run-length for the 4-row stripe at column x
				t.encodeRunLength(x, y, bp, bit)
				continue
			}

			// Regular cleanup coding
			for yy := y; yy < y+4 && yy < t.height; yy++ {
				if t.hasFlag(x, yy, T1Visit) {
					t.clearFlag(x, yy, T1Visit)
					continue
				}
				if t.hasFlag(x, yy, T1Sig) {
					continue
				}

				// Encode significance
				sig := 0
				if t.data[yy*t.width+x]&bit != 0 {
					sig = 1
				}

				ctx := t.getZCContext(x, yy, t.bandType)
				t.mqEnc.Encode(ctx, sig)

				if sig != 0 {
					t.encodeSign(x, yy)
					t.setFlag(x, yy, T1Sig)
					t.updateNeighborFlags(x, yy)
				}
			}
		}
	}
}

// canUseRunLength checks if run-length coding can be used.
func (t *T1) canUseRunLength(x, y, bp int) bool {
	if y+4 > t.height {
		return false
	}
	for yy := y; yy < y+4; yy++ {
		if t.hasFlag(x, yy, T1Sig|T1Visit) {
			return false
		}
		if t.hasSignificantNeighbor(x, yy) {
			return false
		}
	}
	return true
}

// encodeRunLength encodes a run of insignificant coefficients.
func (t *T1) encodeRunLength(x, y, bp int, bit int32) int {
	// Find first significant in the run
	firstSig := -1
	for i := 0; i < 4; i++ {
		if y+i >= t.height {
			break
		}
		if t.data[(y+i)*t.width+x]&bit != 0 {
			firstSig = i
			break
		}
	}

	if firstSig == -1 {
		// All zero - encode single bit
		t.mqEnc.Encode(CtxRL, 0)
		return 4
	}

	// Encode run symbol
	t.mqEnc.Encode(CtxRL, 1)

	// Encode position with uniform context
	t.mqEnc.Encode(CtxUni, (firstSig>>1)&1)
	t.mqEnc.Encode(CtxUni, firstSig&1)

	// Encode sign
	t.encodeSign(x, y+firstSig)
	t.setFlag(x, y+firstSig, T1Sig)
	t.updateNeighborFlags(x, y+firstSig)

	// Continue with remaining positions
	for i := firstSig + 1; i < 4 && y+i < t.height; i++ {
		sig := 0
		if t.data[(y+i)*t.width+x]&bit != 0 {
			sig = 1
		}
		ctx := t.getZCContext(x, y+i, t.bandType)
		t.mqEnc.Encode(ctx, sig)
		if sig != 0 {
			t.encodeSign(x, y+i)
			t.setFlag(x, y+i, T1Sig)
			t.updateNeighborFlags(x, y+i)
		}
	}

	return 4
}

// Decode decodes a code-block from the given bit-stream.
func (t *T1) Decode(data []byte, numBPS int, bandType int) []int32 {
	t.bandType = bandType
	t.numBPS = numBPS
	t.mqDec = NewMQDecoder(data)

	// Clear data and flags
	for i := range t.data {
		t.data[i] = 0
	}
	for i := range t.flags {
		t.flags[i] = 0
	}

	// Decode each bit-plane
	for bp := numBPS - 1; bp >= 0; bp-- {
		t.decodeSignificancePass(bp)
		t.decodeMagnitudeRefinementPass(bp)
		t.decodeCleanupPass(bp)
	}

	// Apply signs
	result := make([]int32, len(t.data))
	for i, v := range t.data {
		if t.flags[t.flagIndex(i%t.width, i/t.width)]&T1SignNeg != 0 {
			result[i] = -v
		} else {
			result[i] = v
		}
	}

	return result
}

// decodeSignificancePass decodes the significance propagation pass.
func (t *T1) decodeSignificancePass(bp int) {
	bit := int32(1) << bp

	for y := 0; y < t.height; y++ {
		for x := 0; x < t.width; x++ {
			if t.hasFlag(x, y, T1Sig) {
				continue
			}
			if !t.hasSignificantNeighbor(x, y) {
				continue
			}

			ctx := t.getZCContext(x, y, t.bandType)
			sig := t.mqDec.Decode(ctx)

			if sig != 0 {
				t.data[y*t.width+x] = bit
				t.decodeSign(x, y)
				t.setFlag(x, y, T1Sig)
				t.updateNeighborFlags(x, y)
			}
			t.setFlag(x, y, T1Visit)
		}
	}
}

// decodeSign decodes the sign of a coefficient.
func (t *T1) decodeSign(x, y int) {
	ctx, pred := t.getSCContext(x, y)
	sign := t.mqDec.Decode(ctx) ^ pred
	if sign != 0 {
		t.setFlag(x, y, T1SignNeg)
	}
}

// decodeMagnitudeRefinementPass decodes magnitude refinement.
func (t *T1) decodeMagnitudeRefinementPass(bp int) {
	bit := int32(1) << bp

	for y := 0; y < t.height; y++ {
		for x := 0; x < t.width; x++ {
			if !t.hasFlag(x, y, T1Sig) || t.hasFlag(x, y, T1Visit) {
				continue
			}

			ctx := t.getMRContext(x, y)
			if t.mqDec.Decode(ctx) != 0 {
				t.data[y*t.width+x] |= bit
			}
			t.setFlag(x, y, T1Refine)
		}
	}
}

// decodeCleanupPass decodes the cleanup pass.
func (t *T1) decodeCleanupPass(bp int) {
	bit := int32(1) << bp

	for y := 0; y < t.height; y += 4 {
		for x := 0; x < t.width; x++ {
			if t.canUseRunLength(x, y, bp) {
				t.decodeRunLength(x, y, bit)
				continue
			}

			for yy := y; yy < y+4 && yy < t.height; yy++ {
				if t.hasFlag(x, yy, T1Visit) {
					t.clearFlag(x, yy, T1Visit)
					continue
				}
				if t.hasFlag(x, yy, T1Sig) {
					continue
				}

				ctx := t.getZCContext(x, yy, t.bandType)
				sig := t.mqDec.Decode(ctx)

				if sig != 0 {
					t.data[yy*t.width+x] = bit
					t.decodeSign(x, yy)
					t.setFlag(x, yy, T1Sig)
					t.updateNeighborFlags(x, yy)
				}
			}
		}
	}
}

// decodeRunLength decodes a run-length coded segment.
func (t *T1) decodeRunLength(x, y int, bit int32) {
	if t.mqDec.Decode(CtxRL) == 0 {
		// All zeros
		return
	}

	// Decode position
	pos := t.mqDec.Decode(CtxUni) << 1
	pos |= t.mqDec.Decode(CtxUni)

	// Set significant
	t.data[(y+pos)*t.width+x] = bit
	t.decodeSign(x, y+pos)
	t.setFlag(x, y+pos, T1Sig)
	t.updateNeighborFlags(x, y+pos)

	// Decode remaining
	for i := pos + 1; i < 4 && y+i < t.height; i++ {
		ctx := t.getZCContext(x, y+i, t.bandType)
		if t.mqDec.Decode(ctx) != 0 {
			t.data[(y+i)*t.width+x] = bit
			t.decodeSign(x, y+i)
			t.setFlag(x, y+i, T1Sig)
			t.updateNeighborFlags(x, y+i)
		}
	}
}
