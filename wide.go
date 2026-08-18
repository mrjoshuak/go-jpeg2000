package jpeg2000

// Wide samples: the binary32 path.
//
// EncodeFloat carries IEEE 754 binary32 samples through the integer pipeline as
// reinterpreted bit patterns, with an NLT Type 3 point transform to restore the
// numeric ordering. After that transform a sample occupies the whole int32
// range, and that is the fact the rest of this file exists for: one level of
// the reversible 5/3 transform then needs 33 magnitude bits, and 35 once the
// RCT has widened the chrominance differences, which is more than a 32-bit
// sign-magnitude word can hold.
//
// The overflow was invisible for as long as this library was its own only
// witness. Both the 5/3 lifting steps and the RCT are exactly invertible
// modulo 2^32, so a wrapped coefficient decodes back to the sample it came
// from — every round-trip test passed. A conforming decoder reads the magnitude
// budget the QCD marker signals, holds the coefficients in a word wide enough
// for it, and reconstructs different samples: measured against OpenJPH, 877 of
// 1024 samples of a 32x32 float image differed.
//
// What this file adds:
//
//   - int64 coefficient planes for the float path, transformed by the 64-bit
//     5/3 and RCT in internal/dwt and internal/mct;
//   - a per-subband magnitude budget, signalled in QCD as guard bits and
//     exponents the way OpenJPH signals it, and raised above the nominal value
//     whenever the coefficients actually measured need more;
//   - the Ccap^15 B_p field, which declares that budget in the CAP marker.

import (
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/mct"
)

// maxSignalledMb is the largest Mb the QCD marker can express: the exponent
// field is five bits and the guard-bit field three, so Mb = G + eps - 1 tops
// out at 7 + 31 - 1.
const maxSignalledMb = 37

// preprocessWide runs the reversible transform chain for a component whose
// samples do not fit a 32-bit coefficient word, and records the magnitude
// budget the codestream has to declare for the result.
//
// It runs after the NLT point transform and instead of the int32 MCT and DWT,
// not alongside them.
func (e *encoder) preprocessWide() error {
	e.wideData = make([][]int64, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		plane := make([]int64, len(e.componentData[c]))
		for i, v := range e.componentData[c] {
			plane[i] = int64(v)
		}
		e.wideData[c] = plane
	}

	if e.mctApplies() {
		mct.ForwardRCT64(e.wideData[0], e.wideData[1], e.wideData[2])
	}

	numLevels := e.numResolutions() - 1
	if e.numTiles() > 1 {
		// Each tile is transformed at its own absolute origin, and the result
		// is kept rather than recomputed: the magnitude budget below has to be
		// measured over every tile before the first marker is written, and the
		// packets are then built from the same coefficients that measurement
		// saw.
		e.wideTiles = make([][][]int64, e.numTiles())
		for idx := range e.wideTiles {
			x0, y0, x1, y1 := e.tileBounds(idx)
			w, h := x1-x0, y1-y0
			if w <= 0 || h <= 0 {
				return fmt.Errorf("tile %d has empty bounds [%d,%d)x[%d,%d)", idx, x0, x1, y0, y1)
			}
			comps := make([][]int64, e.numComponents)
			for c := 0; c < e.numComponents; c++ {
				plane := e.wideData[c]
				tile := make([]int64, w*h)
				for row := 0; row < h; row++ {
					src := (y0+row)*e.width + x0
					if src+w > len(plane) {
						return fmt.Errorf("component %d is short of samples", c)
					}
					copy(tile[row*w:(row+1)*w], plane[src:src+w])
				}
				dwt.DecomposeMultiLevel53Tile64(tile, w, h, x0, y0, numLevels)
				comps[c] = tile
			}
			e.wideTiles[idx] = comps
		}
		e.wideData = nil
	} else {
		for c := 0; c < e.numComponents; c++ {
			dwt.DecomposeMultiLevel53Tile64(e.wideData[c], e.width, e.height, 0, 0, numLevels)
		}
	}

	return e.setWideMagnitudeBudget()
}

// mctApplies reports whether the multiple component transform is used, which
// generateCOD signals and the decoder inverts.
func (e *encoder) mctApplies() bool { return e.numComponents >= 3 }

// nominalWideBits returns the magnitude bit-planes each subband is expected to
// occupy, in the QCD marker's subband order.
//
// These are the values OpenJPH derives for a reversible 5/3 codestream, read
// back out of the QCD markers it writes: with B = precision - 1 magnitude bits
// in a sample, one more when the RCT is in use, the LL band and the finest
// level's detail bands take B + 2 and every coarser detail band takes B + 3.
// Verified against ojph_compress at 0 to 5 decomposition levels, at 8, 16 and
// 32 bits, with and without the colour transform: guard bits and exponents come
// out identical.
//
// The values are nominal, not bounds — the extreme sample of a full-range
// component can exceed them — so setWideMagnitudeBudget raises any band whose
// coefficients measure larger.
func (e *encoder) nominalWideBits() []int {
	numRes := e.numResolutions()
	base := e.maxPrecision() - 1
	if e.mctApplies() {
		base++
	}
	out := make([]int, 3*(numRes-1)+1)
	if numRes == 1 {
		out[0] = base
		return out
	}
	out[0] = base + 2
	for res := 1; res < numRes; res++ {
		v := base + 3
		if res == numRes-1 {
			v = base + 2
		}
		for band := 0; band < 3; band++ {
			out[1+(res-1)*3+band] = v
		}
	}
	return out
}

// setWideMagnitudeBudget fixes Mb for every subband and the guard-bit count
// that lets QCD express it.
//
// Mb has to cover what the block coder will actually emit. A decoder derives
// the position it places magnitudes at from Mb and the code-block's zero
// bit-plane count, so an Mb that is short by one bit does not lose the bottom
// bit — it shifts every magnitude in the band.
func (e *encoder) setWideMagnitudeBudget() error {
	mb := e.nominalWideBits()
	measured := e.measureWideBands(len(mb))
	for i := range mb {
		if measured[i] > mb[i] {
			mb[i] = measured[i]
		}
		if mb[i] < 1 {
			mb[i] = 1
		}
	}

	maxMb := 0
	for _, m := range mb {
		if m > maxMb {
			maxMb = m
		}
	}
	if maxMb > maxSignalledMb {
		// Refuse rather than write a file whose declared budget is smaller
		// than the coefficients it carries. That is the failure this whole
		// path exists to remove, and silently clamping would reintroduce it.
		return fmt.Errorf("jpeg2000: subband needs %d magnitude bit-planes, which QCD cannot signal (max %d)",
			maxMb, maxSignalledMb)
	}
	// The exponent field is five bits, so the guard bits absorb whatever Mb
	// does not fit; maxSignalledMb above is exactly the point at which seven
	// guard bits stop being enough. OpenJPH picks the same minimum: guard 1 for
	// an 8-bit image, 4 for binary32, 5 for binary32 under the RCT.
	guard := 1
	for guard < 7 && maxMb-guard+1 > 31 {
		guard++
	}
	e.wideGuard = guard
	e.wideMb = mb
	return nil
}

// measureWideBands returns, per subband, the number of magnitude bits the
// largest transformed coefficient in it actually occupies, taken over every
// tile.
func (e *encoder) measureWideBands(numBands int) []int {
	out := make([]int, numBands)
	cbWidth, cbHeight := e.codeBlockExponents()
	numRes := e.numResolutions()

	visit := func(comps [][]int64, x0, y0, x1, y1 int) {
		stride := x1 - x0
		for _, bd := range tileBands(x0, y0, x1, y1, numRes, 1<<cbWidth, 1<<cbHeight) {
			idx := 0
			if bd.res > 0 {
				idx = 1 + (bd.res-1)*3 + bd.band
			}
			if idx >= numBands {
				continue
			}
			for _, plane := range comps {
				for y := 0; y < bd.sb.height(); y++ {
					row := (bd.sb.oy + y) * stride
					for x := 0; x < bd.sb.width(); x++ {
						i := row + bd.sb.ox + x
						if i < 0 || i >= len(plane) {
							continue
						}
						if n := coefficientBits(plane[i]); n > out[idx] {
							out[idx] = n
						}
					}
				}
			}
		}
	}

	if e.wideTiles != nil {
		for idx, comps := range e.wideTiles {
			x0, y0, x1, y1 := e.tileBounds(idx)
			visit(comps, x0, y0, x1, y1)
		}
		return out
	}
	visit(e.wideData, 0, 0, e.width, e.height)
	return out
}

// coefficientBits returns the number of bits |v| occupies.
func coefficientBits(v int64) int {
	m := uint64(v)
	if v < 0 {
		m = uint64(-v)
	}
	n := 0
	for m > 0 {
		n++
		m >>= 1
	}
	return n
}

// jobNumBPS returns the magnitude bit-plane count of one code-block, from
// whichever coefficient word it carries.
func jobNumBPS(job codeBlockJob) int {
	if job.data64 != nil {
		return entropy.NumBitPlanes64(job.data64)
	}
	return computeNumBPS(job.data)
}

// ccapMagB returns the Ccap^15 B_p field for a codestream whose largest
// subband magnitude budget is magb bit-planes.
//
// ISO/IEC 15444-15 Table A.3 encodes the budget in five bits: exactly for small
// values, and in steps of four above 27. The mapping is OpenJPH's, checked
// against the CAP markers ojph_compress writes: 0 at 8 bits, 2 at 10, 7 at 15,
// 10 at 18 and 21 at 34 and 35.
//
// The field used to be a constant, so every codestream this library wrote
// declared the budget of a 10-bit one whatever it actually carried.
func ccapMagB(magb int) uint16 {
	switch {
	case magb <= 8:
		return 0
	case magb < 28:
		return uint16(magb - 8)
	case magb < 48:
		return uint16(13 + magb>>2)
	default:
		return 31
	}
}

// maxBandMbSignalled returns the largest Mb this codestream declares, which is
// what the CAP marker's B_p field has to cover.
func (e *encoder) maxBandMbSignalled() int {
	numRes := e.numResolutions()
	m := e.bandMb(0, 0)
	for res := 1; res < numRes; res++ {
		for band := 0; band < 3; band++ {
			if v := e.bandMb(res, band); v > m {
				m = v
			}
		}
	}
	return m
}

// wideDecode holds the state a decoder needs to reconstruct a codestream whose
// coefficients do not fit int32.
//
// The planes are int64 only as far as the inverse colour transform: after it
// the samples are the NLT Type 3 words again, which are int32 by construction,
// so the rest of the decode path is unchanged.
type wideDecode struct {
	planes [][]int64
}

func newWideDecode(numComp, n int) *wideDecode {
	w := &wideDecode{planes: make([][]int64, numComp)}
	for c := range w.planes {
		w.planes[c] = make([]int64, n)
	}
	return w
}

// finish applies the inverse colour transform and narrows the planes back to
// the int32 sample words the rest of the decoder works in.
func (w *wideDecode) finish(h *codestream.Header, dst [][]int32) {
	if h.CodingStyle.MultipleComponentXf != 0 && len(w.planes) >= 3 {
		mct.InverseRCT64(w.planes[0], w.planes[1], w.planes[2])
	}
	for c := range dst {
		if c >= len(w.planes) {
			break
		}
		src := w.planes[c]
		for i := range dst[c] {
			if i < len(src) {
				dst[c][i] = int32(src[i])
			}
		}
	}
}
