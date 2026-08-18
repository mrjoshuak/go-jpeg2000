package jpeg2000

import (
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
)

// Subband geometry.
//
// A 5/3 or 9/7 split of a signal of length W produces ceil(W/2) lowpass samples
// and floor(W/2) highpass samples. Those differ whenever W is odd, and which of
// the two applies to a subband depends on its orientation: HL is highpass
// horizontally and lowpass vertically, LH is the reverse, and HH is highpass in
// both directions.
//
// Every site in this package previously used ceil for all three detail bands.
// That is correct only when the resolution's dimensions are even, which is why
// power-of-two images interoperated and a 17-pixel image did not: the encoder
// claimed a 9-wide HL band where the standard defines an 8-wide one, so the
// code-block partition, the packet headers and the coefficient placement were
// all one column too wide.
//
// The arithmetic itself lives in internal/codestream, so that the tile decoder,
// which cannot import this package, dequantizes exactly the rectangles this one
// quantized.

// Band orientation indices, matching the order subbands appear in a packet at
// resolution levels above zero.
const (
	bandHL = 0
	bandLH = 1
	bandHH = 2
)

// bandDims returns the dimensions of the subband identified by (r, band): the
// LL band at resolution 0, or one of the three detail bands above it.
//
// This is the origin-zero case, and nothing on the live path uses it any more:
// the codec derives every subband from absolute coordinates through
// tileBandGeom, because a tile away from the image origin splits differently.
// It is kept as the independent statement of the origin-zero rule that
// TestTileGeometryMatchesOriginZero pins the general form to — that agreement
// is what shows the tiling work left untiled output alone.
func bandDims(tcW, tcH, numRes, r, band int) (int, int) {
	return codestream.BandDims(tcW, tcH, numRes, r, band)
}

// computeSubbandOffset computes the (x, y) offset of a subband within the
// DWT-decomposed data array. Used by both encoder and decoder.
func computeSubbandOffset(width, height, numRes, res, bandType int) (int, int) {
	return codestream.SubbandOffset(width, height, numRes, res, bandType)
}

// Tile geometry.
//
// ISO/IEC 15444-1 B.5 derives every subband coordinate from the tile
// component's absolute coordinates, not from its size:
//
//	tbx0 = ceil((tcx0 - 2^(nb-1) * xob) / 2^nb)
//
// so two tiles of identical size split differently when their origins have
// different parity at some decomposition level. A 32x32 image cut into 20x20
// tiles puts the second column of tiles at x = 20, which is odd once halved
// twice, and its detail bands are a column narrower than the size-only rule
// predicts. Encoding it with that rule mis-sizes the code-block partition,
// the packet headers and the coefficient placement all at once.

// ceilShift returns ceil(a / 2^n) for a >= 0.
func ceilShift(a, n int) int {
	if n <= 0 {
		return a
	}
	return (a + (1 << uint(n)) - 1) >> uint(n)
}

// tileResCoords returns the absolute coordinates of resolution level r of a
// tile component spanning [x0, x1) x [y0, y1), per Equation B-14.
func tileResCoords(x0, y0, x1, y1, numRes, r int) (rx0, ry0, rx1, ry1 int) {
	n := numRes - 1 - r
	return ceilShift(x0, n), ceilShift(y0, n), ceilShift(x1, n), ceilShift(y1, n)
}

// tileResDims returns the dimensions of resolution level r of a tile
// component spanning [x0, x1) x [y0, y1).
func tileResDims(x0, y0, x1, y1, numRes, r int) (int, int) {
	rx0, ry0, rx1, ry1 := tileResCoords(x0, y0, x1, y1, numRes, r)
	return rx1 - rx0, ry1 - ry0
}

// subbandGeom locates one subband of one tile component: its absolute
// coordinates, which is what the code-block partition is measured in, and its
// offset within the tile component's coefficient array, which holds the
// subbands in the Mallat layout the wavelet transform leaves them in.
type subbandGeom struct {
	x0, y0, x1, y1 int // absolute band coordinates (Equation B-15)
	ox, oy         int // offset within the tile-component array
}

func (s subbandGeom) width() int  { return s.x1 - s.x0 }
func (s subbandGeom) height() int { return s.y1 - s.y0 }

// tileBandGeom returns the geometry of the subband identified by (r, band) for
// a tile component spanning [x0, x1) x [y0, y1).
//
// band is bandHL, bandLH or bandHH for r > 0 and is ignored at r = 0, where
// the only subband is LL.
func tileBandGeom(x0, y0, x1, y1, numRes, r, band int) subbandGeom {
	rx0, ry0, rx1, ry1 := tileResCoords(x0, y0, x1, y1, numRes, r)
	if r == 0 {
		return subbandGeom{x0: rx0, y0: ry0, x1: rx1, y1: ry1}
	}
	// Halving the resolution's coordinates splits them into the even ones,
	// which are the lowpass band, and the odd ones, which are the highpass
	// band. Rounding up gives the first, rounding down the second, and the
	// two rules differ exactly when a coordinate is odd — which is the case a
	// tile away from the origin runs into and a tile at the origin never does.
	lx0, lx1 := ceilShift(rx0, 1), ceilShift(rx1, 1)
	ly0, ly1 := ceilShift(ry0, 1), ceilShift(ry1, 1)
	hx0, hx1 := rx0>>1, rx1>>1
	hy0, hy1 := ry0>>1, ry1>>1
	// The array offsets are sizes, not coordinates: the lowpass quadrant
	// occupies the top-left corner of the region this level was taken from.
	lw, lh := lx1-lx0, ly1-ly0
	switch band {
	case bandHL:
		return subbandGeom{x0: hx0, y0: ly0, x1: hx1, y1: ly1, ox: lw}
	case bandLH:
		return subbandGeom{x0: lx0, y0: hy0, x1: lx1, y1: hy1, oy: lh}
	default: // bandHH
		return subbandGeom{x0: hx0, y0: hy0, x1: hx1, y1: hy1, ox: lw, oy: lh}
	}
}

// tileBand is one subband of one tile-component together with its code-block
// partition.
type tileBand struct {
	res      int         // resolution level it belongs to
	band     int         // bandHL, bandLH or bandHH; 0 (LL) at res 0
	bandType int         // the entropy package's band constant
	sb       subbandGeom // absolute coordinates and array offset
	// firstX and firstY are the indices, in the zero-anchored code-block
	// partition, of the first code-block this band overlaps.
	firstX, firstY int
	cbX, cbY       int // code-block grid size
}

// blockRect returns the offset within the tile-component array and the size of
// one code-block of this band, indexed from the band's own first block.
func (b tileBand) blockRect(cbx, cby, cbWidth, cbHeight int) (ox, oy, w, h int) {
	bx0 := max(b.sb.x0, (b.firstX+cbx)*cbWidth)
	bx1 := min(b.sb.x1, (b.firstX+cbx+1)*cbWidth)
	by0 := max(b.sb.y0, (b.firstY+cby)*cbHeight)
	by1 := min(b.sb.y1, (b.firstY+cby+1)*cbHeight)
	return b.sb.ox + bx0 - b.sb.x0, b.sb.oy + by0 - b.sb.y0, bx1 - bx0, by1 - by0
}

// tileBands returns the subbands of one tile-component in the order the
// codestream describes them: resolution, then band within the resolution.
//
// A resolution with no samples is skipped entirely. It has no precinct, so it
// carries no packet (ISO/IEC 15444-1 B.6) — which is not the same as carrying
// an empty one, and a decoder that reads a packet for it desynchronises. This
// is reachable: a one-pixel-wide column of tiles has empty resolutions at
// every level above the finest.
//
// Encoder and decoder both walk this list, which is what keeps the code-block
// order they agree on in one place.
func tileBands(x0, y0, x1, y1, numRes, cbWidth, cbHeight int) []tileBand {
	var out []tileBand
	for r := 0; r < numRes; r++ {
		rw, rh := tileResDims(x0, y0, x1, y1, numRes, r)
		if rw <= 0 || rh <= 0 {
			continue
		}
		numBands := 1 // LL only
		if r > 0 {
			numBands = 3 // HL, LH, HH
		}
		for b := 0; b < numBands; b++ {
			bt := entropy.BandLL
			if r > 0 {
				switch b {
				case bandHL:
					bt = entropy.BandHL
				case bandLH:
					bt = entropy.BandLH
				default:
					bt = entropy.BandHH
				}
			}
			sb := tileBandGeom(x0, y0, x1, y1, numRes, r, b)
			firstX, nx := codeBlockRange(sb.x0, sb.x1, cbWidth)
			firstY, ny := codeBlockRange(sb.y0, sb.y1, cbHeight)
			out = append(out, tileBand{
				res: r, band: b, bandType: bt, sb: sb,
				firstX: firstX, firstY: firstY, cbX: nx, cbY: ny,
			})
		}
	}
	return out
}

// codeBlockRange returns the index of the first code-block covering [b0, b1)
// and how many there are, for a partition of size cb.
//
// ISO/IEC 15444-1 B.7 anchors that partition at coordinate zero, not at the
// band's own origin, so a band that starts inside a code-block gets a short
// first block. Only a band starting at zero — that is, a single tile at the
// image origin — can ignore the distinction, which is why partitioning from
// the band origin read correctly to this library and split a 65-pixel image
// into the wrong code-blocks for everyone else.
func codeBlockRange(b0, b1, cb int) (first, count int) {
	if b1 <= b0 || cb <= 0 {
		return 0, 0
	}
	first = b0 / cb
	return first, (b1+cb-1)/cb - first
}
