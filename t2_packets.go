package jpeg2000

// Standard T2 packet decoding.
//
// Tile data in a conforming JPEG 2000 codestream is a sequence of packets, each
// with a tag-tree coded header naming which code-blocks contribute, how many
// coding passes each contributes and how many bytes those passes occupy.
//
// This library previously wrote and read a private container instead — a
// two-byte code-block count followed by a fixed-width table — and silently
// returned a blank image for anything else. That is why no external decoder
// could read our output and why we recovered nothing from theirs. Both
// directions are packets now, and the private container is gone.

import (
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/tcd"
)

// pktReader reads packet-header bits, applying the bit-unstuffing rule: a byte
// following one equal to 0xFF carries only seven bits.
// useSOP and useEPH come from the coding style (Table A.13) and say whether the
// tile data carries the two optional packet-delimiting markers. They are not
// decorative: a decoder that reads through an SOP segment takes six bytes of
// marker as packet header and recovers nothing from that point on.
type pktReader struct {
	data    []byte
	pos     int
	bitBuf  uint32
	bitCnt  uint
	lastFF  bool
	overrun bool
	useSOP  bool
	useEPH  bool
}

func newPktReader(data []byte, useSOP, useEPH bool) *pktReader {
	return &pktReader{data: data, useSOP: useSOP, useEPH: useEPH}
}

// packetMarkers reports whether an Scod byte declares SOP marker segments
// before packets and EPH markers after packet headers.
func packetMarkers(style uint8) (useSOP, useEPH bool) {
	return style&codestream.CodingStyleSOP != 0, style&codestream.CodingStyleEPH != 0
}

// skipSOP steps over a start-of-packet marker segment (A.8.1): the 0xFF91
// marker, a two-byte length of 4 and a two-byte packet counter, six bytes in
// all. SOP is permitted rather than required before any given packet even when
// the coding style allows it, so its absence is not an error and only an actual
// marker is consumed.
func (r *pktReader) skipSOP() {
	if !r.useSOP || r.bitCnt != 0 || r.pos+6 > len(r.data) {
		return
	}
	if r.data[r.pos] != 0xFF || r.data[r.pos+1] != 0x91 {
		return
	}
	r.pos += 6
	r.lastFF = false
}

// skipEPH steps over the end-of-packet-header marker (A.8.2), which follows
// every packet header when the coding style declares it, including the single
// zero bit that is an empty packet's whole header.
func (r *pktReader) skipEPH() {
	if !r.useEPH || r.pos+2 > len(r.data) {
		return
	}
	if r.data[r.pos] != 0xFF || r.data[r.pos+1] != 0x92 {
		return
	}
	r.pos += 2
}

func (r *pktReader) readBit() uint32 {
	if r.bitCnt == 0 {
		if r.pos >= len(r.data) {
			r.overrun = true
			return 0
		}
		b := r.data[r.pos]
		r.pos++
		if r.lastFF {
			// Only seven bits after 0xFF; the high bit is the stuffed zero.
			r.bitBuf = uint32(b & 0x7F)
			r.bitCnt = 7
			r.lastFF = false
		} else {
			r.bitBuf = uint32(b)
			r.bitCnt = 8
			r.lastFF = b == 0xFF
		}
	}
	r.bitCnt--
	return (r.bitBuf >> r.bitCnt) & 1
}

func (r *pktReader) readBits(n uint) uint32 {
	v := uint32(0)
	for i := uint(0); i < n; i++ {
		v = (v << 1) | r.readBit()
	}
	return v
}

// align completes the current byte. A header ending on a 0xFF consumes the
// stuffed byte that follows it.
func (r *pktReader) align() {
	r.bitCnt = 0
	if r.lastFF {
		if r.pos < len(r.data) {
			r.pos++
		}
		r.lastFF = false
	}
}

// tagTree is the tag tree of ISO/IEC 15444-1 Annex B.10. Values are coded
// incrementally against a rising threshold, and partial knowledge persists
// across queries, so a fresh unary decode per leaf is not equivalent except in
// the degenerate 1x1 case.
type tagTree struct {
	w, h   int
	levels int
	value  [][]int
	known  [][]bool
	dimW   []int
	dimH   []int
}

func newTagTree(w, h int) *tagTree {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	t := &tagTree{w: w, h: h}
	lw, lh := w, h
	for {
		t.dimW = append(t.dimW, lw)
		t.dimH = append(t.dimH, lh)
		t.value = append(t.value, make([]int, lw*lh))
		t.known = append(t.known, make([]bool, lw*lh))
		if lw == 1 && lh == 1 {
			break
		}
		lw = (lw + 1) / 2
		lh = (lh + 1) / 2
	}
	t.levels = len(t.dimW)
	return t
}

// decode returns whether the leaf's value is below threshold, reading only as
// many bits as the standard requires.
func (t *tagTree) decode(r *pktReader, x, y, threshold int) (int, bool) {
	// Walk root to leaf, carrying each node's lower bound downward.
	low := 0
	for lvl := t.levels - 1; lvl >= 0; lvl-- {
		xx := x >> uint(lvl)
		yy := y >> uint(lvl)
		idx := yy*t.dimW[lvl] + xx
		if idx < 0 || idx >= len(t.value[lvl]) {
			return 0, false
		}
		if t.value[lvl][idx] < low {
			t.value[lvl][idx] = low
		}
		for !t.known[lvl][idx] && t.value[lvl][idx] < threshold {
			if r.readBit() == 1 {
				t.known[lvl][idx] = true
			} else {
				t.value[lvl][idx]++
			}
			if r.overrun {
				return t.value[lvl][idx], false
			}
		}
		low = t.value[lvl][idx]
		if !t.known[lvl][idx] {
			// Value is at least threshold; nothing below is determined yet.
			return low, false
		}
	}
	return low, true
}

// cbState is the decoder's per-code-block bookkeeping across layers.
type cbState struct {
	included   bool
	zeroPlanes int
	lblock     int
	data       []byte
	numPasses  int
}

// bandGeometry describes one subband's code-block grid.
type bandGeometry struct {
	bandType int
	sb       subbandGeom
	// firstX and firstY are the indices of the first code-block of the
	// zero-anchored partition that this band overlaps; the grid below is
	// indexed from zero relative to them.
	firstX, firstY int
	cbX, cbY       int
	// cbW and cbH are the code-block dimensions actually in force here: B.7
	// clips the partition to the precinct, so a precinct smaller than the
	// declared code-block silently makes the blocks smaller.
	cbW, cbH int
	blocks   []*cbState
	incl     *tagTree
	imsb     *tagTree
}

// bandGridFor builds the code-block grid of one resolution of one
// tile-component, which is what a packet's header describes.
//
// Every coordinate is absolute: ISO/IEC 15444-1 B.5 derives the subband
// coordinates from the tile-component's position in the image, and B.7 anchors
// the code-block partition at zero rather than at the band. A tile away from
// the image origin differs on both counts, so deriving them from the tile's
// size alone reads the packet headers of every tile but the first against the
// wrong grid.
//
// A resolution with no samples has no precinct, so the codestream holds no
// packet for it at all (B.6); that case returns nil.
// precinctExps returns the precinct exponents (PPx, PPy) in force at one
// resolution. Scod bit 0 signals an explicit partition; without it the maximal
// precinct applies (B.6), which is the single-precinct geometry this library
// used to assume everywhere.
func precinctExps(cs codestream.CodingStyleDefault, res int) (int, int) {
	if cs.CodingStyle&codestream.CodingStylePrecincts == 0 || res >= len(cs.PrecinctSizes) {
		return 15, 15
	}
	p := cs.PrecinctSizes[res]
	return int(p.WidthExp), int(p.HeightExp)
}

// precinctsFor builds the precinct partition of one resolution of one
// tile-component, and within each precinct the code-block grid of every band.
// One packet describes one precinct, so this is the unit the packet headers
// are read against.
//
// Every coordinate is absolute: ISO/IEC 15444-1 B.5 derives the subband
// coordinates from the tile-component's position in the image, B.6 anchors the
// precinct partition at zero in the resolution's own coordinates, and B.7
// anchors the code-block partition at zero as well. A tile away from the image
// origin differs on all three.
//
// Two things about a precinct are easy to get wrong and are both load-bearing
// here. Its band-space origin comes from the resolution origin halved, not from
// the band's own origin; and the code-block partition inside it is clipped to
// the precinct, so a precinct smaller than the declared code-block makes the
// blocks smaller rather than overflowing the precinct.
//
// A resolution with no samples has no precinct, so the codestream holds no
// packet for it at all (B.6); that case returns nil.
func precinctsFor(x0, y0, x1, y1, numRes, res, cbWidth, cbHeight, ppx, ppy int) [][]*bandGeometry {
	rx0, ry0, rx1, ry1 := tileResCoords(x0, y0, x1, y1, numRes, res)
	if rx1 <= rx0 || ry1 <= ry0 {
		return nil
	}

	// The precinct grid of the resolution, anchored at zero (B-16).
	prcX0, prcY0 := rx0>>uint(ppx), ry0>>uint(ppy)
	npx := ceilShift(rx1, ppx) - prcX0
	npy := ceilShift(ry1, ppy) - prcY0
	if npx <= 0 || npy <= 0 {
		return nil
	}

	// In a band of resolution 0 the precinct keeps its size; above it the band
	// is half the resolution in each direction, so the precinct is too.
	bppx, bppy := ppx, ppy
	if res > 0 {
		bppx, bppy = ppx-1, ppy-1
	}

	// B.7 clips the code-block partition to the precinct.
	ecbW := min(cbWidth, 1<<uint(bppx))
	ecbH := min(cbHeight, 1<<uint(bppy))

	numBands := 1
	if res > 0 {
		numBands = 3
	}

	precincts := make([][]*bandGeometry, 0, npx*npy)
	for py := 0; py < npy; py++ {
		for px := 0; px < npx; px++ {
			bands := make([]*bandGeometry, 0, numBands)
			for b := 0; b < numBands; b++ {
				bt := entropy.BandLL
				if res > 0 {
					switch b {
					case bandHL:
						bt = entropy.BandHL
					case bandLH:
						bt = entropy.BandLH
					default:
						bt = entropy.BandHH
					}
				}
				sb := tileBandGeom(x0, y0, x1, y1, numRes, res, b)

				// The precinct's rectangle in this band's coordinates, clipped
				// to the band.
				bx0 := (prcX0 + px) << uint(bppx)
				by0 := (prcY0 + py) << uint(bppy)
				px0, px1 := max(sb.x0, bx0), min(sb.x1, bx0+(1<<uint(bppx)))
				py0, py1 := max(sb.y0, by0), min(sb.y1, by0+(1<<uint(bppy)))

				firstX, nx := codeBlockRange(px0, px1, ecbW)
				firstY, ny := codeBlockRange(py0, py1, ecbH)

				bg := &bandGeometry{
					bandType: bt, sb: sb,
					firstX: firstX, firstY: firstY,
					cbX: nx, cbY: ny,
					cbW: ecbW, cbH: ecbH,
				}
				bg.blocks = make([]*cbState, nx*ny)
				for i := range bg.blocks {
					bg.blocks[i] = &cbState{lblock: 3}
				}
				// An empty precinct in this band still takes part in the
				// packet: it contributes no code-blocks, and its tag trees are
				// never read, but the band must be present so the bands stay
				// in order.
				bg.incl = newTagTree(nx, ny)
				bg.imsb = newTagTree(nx, ny)
				bands = append(bands, bg)
			}
			precincts = append(precincts, bands)
		}
	}
	return precincts
}

// forEachPacket visits the (layer, resolution, component) triple of every
// packet of one tile, in the order the codestream's progression order places
// them, for the one-precinct-per-resolution geometry this library writes and
// the maximal precinct a Scod without a precinct partition declares. Visiting
// stops when visit returns false.
//
// The five orders of Table A.16 differ only in which of layer, resolution and
// component is outermost once the position index has a single value. Reading or
// writing a resolution-major sequence for a component-major order recovers the
// right packets in the wrong places, which a single layer and a single
// component hide completely.
//
// The triples are visited rather than returned as a slice because the layer
// count comes from the file: a corrupt COD declaring 65535 layers would
// otherwise buy tens of megabytes from a few hundred bytes of input.
// posInfo carries what the positional progression orders need and the others
// ignore: the tile's own coordinates, each component's subsampling, and the
// precinct exponents of each resolution.
//
// PCRL and CPRL place position outside resolution, and precinct index p means a
// different region of the image at each resolution, so those two cannot be
// walked by raster index — B.12.1.4 walks image coordinates and asks, at each
// one, which resolutions have a precinct starting there. Walking them by index
// decodes every packet against the wrong precinct from the second resolution on.
type posInfo struct {
	tx0, ty0, tx1, ty1 int
	dx, dy             []int // per component subsampling
	ppx, ppy           []int // per resolution precinct exponents
	pw                 func(res, comp int) int
}

// precAt returns the precinct index that covers image coordinate (x, y) at one
// resolution of one component, and whether a precinct actually starts there.
func (pi *posInfo) precAt(x, y, res, comp int) (int, bool) {
	if pi == nil || comp >= len(pi.dx) || res >= len(pi.ppx) {
		return 0, false
	}
	n := len(pi.ppx) - 1 - res
	dx, dy := pi.dx[comp]<<uint(n), pi.dy[comp]<<uint(n)
	if dx <= 0 || dy <= 0 {
		return 0, false
	}
	trx0 := ceilDiv(pi.tx0, dx)
	try0 := ceilDiv(pi.ty0, dy)
	ppx, ppy := pi.ppx[res], pi.ppy[res]

	// A precinct starts here when the coordinate is on the precinct grid, or
	// when it is the tile's own first coordinate and the grid does not start
	// on it.
	okX := x%(dx<<uint(ppx)) == 0 || (x == pi.tx0 && (trx0<<uint(n))%(dx<<uint(ppx)) != 0)
	okY := y%(dy<<uint(ppy)) == 0 || (y == pi.ty0 && (try0<<uint(n))%(dy<<uint(ppy)) != 0)
	if !okX || !okY {
		return 0, false
	}
	w := pi.pw(res, comp)
	if w <= 0 {
		return 0, false
	}
	i := ceilDiv(x, dx)>>uint(ppx) - trx0>>uint(ppx)
	j := ceilDiv(y, dy)>>uint(ppy) - try0>>uint(ppy)
	if i < 0 || j < 0 {
		return 0, false
	}
	return j*w + i, true
}

// step returns the coordinate increment for the positional walk: the largest
// stride that cannot skip over any precinct boundary.
func (pi *posInfo) step(numRes, numComp int) (int, int) {
	sx, sy := 0, 0
	for c := 0; c < numComp && c < len(pi.dx); c++ {
		for r := 0; r < numRes && r < len(pi.ppx); r++ {
			n := len(pi.ppx) - 1 - r
			sx = gcdInt(sx, pi.dx[c]<<uint(n+pi.ppx[r]))
			sy = gcdInt(sy, pi.dy[c]<<uint(n+pi.ppy[r]))
		}
	}
	if sx <= 0 {
		sx = 1
	}
	if sy <= 0 {
		sy = 1
	}
	return sx, sy
}

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	return (a + b - 1) / b
}

func forEachPacket(order codestream.ProgressionOrder, numLayers, numRes, numComp int,
	numPrec func(res, comp int) int, pos *posInfo, visit func(layer, res, comp, prec int) bool) {

	// visitAll runs the precinct dimension for one (layer, res, comp) triple.
	// A packet exists per precinct, so this is where a multi-precinct
	// codestream stops being one packet per resolution.
	visitAll := func(l, r, c int) bool {
		for p := 0; p < numPrec(r, c); p++ {
			if !visit(l, r, c, p) {
				return false
			}
		}
		return true
	}

	switch order {
	case codestream.RLCP:
		for r := 0; r < numRes; r++ {
			for l := 0; l < numLayers; l++ {
				for c := 0; c < numComp; c++ {
					if !visitAll(l, r, c) {
						return
					}
				}
			}
		}
	case codestream.RPCL:
		// Position is outside component here, so the precinct index is walked
		// by this level rather than by visitAll.
		for r := 0; r < numRes; r++ {
			maxP := 0
			for c := 0; c < numComp; c++ {
				maxP = max(maxP, numPrec(r, c))
			}
			for p := 0; p < maxP; p++ {
				for c := 0; c < numComp; c++ {
					if p >= numPrec(r, c) {
						continue
					}
					for l := 0; l < numLayers; l++ {
						if !visit(l, r, c, p) {
							return
						}
					}
				}
			}
		}
	case codestream.CPRL:
		if pos == nil {
			// No positional information: the caller has one precinct per
			// resolution, where every order collapses to the same sequence.
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					for l := 0; l < numLayers; l++ {
						for p := 0; p < numPrec(r, c); p++ {
							if !visit(l, r, c, p) {
								return
							}
						}
					}
				}
			}
			return
		}
		sx, sy := pos.step(numRes, numComp)
		for c := 0; c < numComp; c++ {
			for y := pos.ty0; y < pos.ty1; y += sy - y%sy {
				for x := pos.tx0; x < pos.tx1; x += sx - x%sx {
					for r := 0; r < numRes; r++ {
						pn, ok := pos.precAt(x, y, r, c)
						if !ok || pn >= numPrec(r, c) {
							continue
						}
						for l := 0; l < numLayers; l++ {
							if !visit(l, r, c, pn) {
								return
							}
						}
					}
				}
			}
		}
	case codestream.PCRL:
		if pos == nil {
			// No positional information: the caller has one precinct per
			// resolution, where every order collapses to the same sequence.
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					for l := 0; l < numLayers; l++ {
						for p := 0; p < numPrec(r, c); p++ {
							if !visit(l, r, c, p) {
								return
							}
						}
					}
				}
			}
			return
		}
		sx, sy := pos.step(numRes, numComp)
		for y := pos.ty0; y < pos.ty1; y += sy - y%sy {
			for x := pos.tx0; x < pos.tx1; x += sx - x%sx {
				for c := 0; c < numComp; c++ {
					for r := 0; r < numRes; r++ {
						pn, ok := pos.precAt(x, y, r, c)
						if !ok || pn >= numPrec(r, c) {
							continue
						}
						for l := 0; l < numLayers; l++ {
							if !visit(l, r, c, pn) {
								return
							}
						}
					}
				}
			}
		}
	default: // LRCP
		for l := 0; l < numLayers; l++ {
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					if !visitAll(l, r, c) {
						return
					}
				}
			}
		}
	}
}

// decodeStandardTileData decodes conforming T2 packets for one tile and writes
// the recovered coefficients into the tile components.
func (d *decoder) decodeStandardTileData(tile *tcd.Tile, tileData []byte, qualityLimit int) error {
	h := d.header
	numRes := int(h.CodingStyle.NumDecompositions) + 1
	if numRes < 1 || numRes > codestream.MaxDecompositionLevels+1 {
		return fmt.Errorf("jpeg2000: COD declares %d resolution levels", numRes)
	}
	cbWidth := h.CodingStyle.CodeBlockWidth()
	cbHeight := h.CodingStyle.CodeBlockHeight()
	if cbWidth <= 0 || cbHeight <= 0 {
		return fmt.Errorf("jpeg2000: COD declares a %dx%d code-block", cbWidth, cbHeight)
	}
	numLayers := h.CodingStyle.Layers()
	if numLayers < 1 {
		numLayers = 1
	}
	wide := h.WideSamples()

	// Build the code-block grid for every (component, resolution, band).
	//
	// Every coordinate here is absolute: ISO/IEC 15444-1 B.5 derives the
	// subband coordinates from the tile-component's position in the image, and
	// B.7 anchors the code-block partition at zero rather than at the band.
	// A tile away from the image origin differs on both counts, so deriving
	// them from the tile's size alone reads the packet headers of every tile
	// but the first against the wrong grid.
	type resKey struct{ c, r int }
	grid := map[resKey][][]*bandGeometry{}
	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		for r := 0; r < numRes; r++ {
			ppx, ppy := precinctExps(h.CodingStyle, r)
			precincts := precinctsFor(tc.FullX0, tc.FullY0, tc.FullX1, tc.FullY1,
				numRes, r, cbWidth, cbHeight, ppx, ppy)
			if precincts == nil {
				continue
			}
			grid[resKey{c, r}] = precincts
		}
	}

	useSOP, useEPH := packetMarkers(h.CodingStyle.CodingStyle)
	r := newPktReader(tileData, useSOP, useEPH)

	// One packet per precinct. Without an explicit partition there is exactly
	// one precinct per resolution, which is the geometry this walk used to
	// assume unconditionally — and why a codestream declaring a real partition
	// was read against the wrong grid from its second packet onward.
	order := codestream.ProgressionOrder(h.CodingStyle.ProgressionOrder)
	numPrec := func(res, c int) int { return len(grid[resKey{c, res}]) }

	pos := &posInfo{tx0: tile.X0, ty0: tile.Y0, tx1: tile.X1, ty1: tile.Y1}
	for c := 0; c < len(tile.Components); c++ {
		sx, sy := 1, 1
		if c < len(h.ComponentInfo) {
			sx, sy = int(h.ComponentInfo[c].SubsamplingX), int(h.ComponentInfo[c].SubsamplingY)
		}
		pos.dx, pos.dy = append(pos.dx, max(sx, 1)), append(pos.dy, max(sy, 1))
	}
	for r := 0; r < numRes; r++ {
		px, py := precinctExps(h.CodingStyle, r)
		pos.ppx, pos.ppy = append(pos.ppx, px), append(pos.ppy, py)
	}
	pos.pw = func(res, c int) int {
		tc := tile.Components[c]
		if tc == nil {
			return 0
		}
		w, _ := precinctGridDims(tc.FullX0, tc.FullY0, tc.FullX1, tc.FullY1,
			numRes, res, pos.ppx[res], pos.ppy[res])
		return w
	}

	var perr error
	forEachPacket(order, numLayers, numRes, len(tile.Components), numPrec, pos, func(layer, res, c, prec int) bool {
		precincts := grid[resKey{c, res}]
		if prec >= len(precincts) {
			return true
		}
		bands := precincts[prec]
		// A quality limit drops the contributions of the later layers, but
		// their packets still have to be parsed: they carry the inclusion and
		// length state the packets between them are read against, and their
		// bodies are what separates one packet from the next.
		keep := qualityLimit <= 0 || layer < qualityLimit
		if err := readPacket(r, bands, layer, keep); err != nil {
			perr = err
			return false
		}
		return !r.overrun
	})
	if perr != nil {
		return perr
	}

	// Decode every contributing code-block and scatter its coefficients.
	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		tcW := tc.X1 - tc.X0
		tcH := tc.Y1 - tc.Y0
		for res := 0; res < numRes; res++ {
			for _, bands := range grid[resKey{c, res}] {
				for b, bg := range bands {
					sb := bg.sb
					for cby := 0; cby < bg.cbY; cby++ {
						by0 := max(sb.y0, (bg.firstY+cby)*bg.cbH)
						by1 := min(sb.y1, (bg.firstY+cby+1)*bg.cbH)
						for cbx := 0; cbx < bg.cbX; cbx++ {
							cb := bg.blocks[cby*bg.cbX+cbx]
							if !cb.included || len(cb.data) == 0 {
								continue
							}
							// A reduced decode reconstructs only the lower
							// resolutions, so the blocks above them contribute
							// nothing to any output sample. Skipping them is
							// where a reduced decode's saving lives, and it is
							// the largest saving available anywhere here: the
							// finest resolution alone carries about 72% of a
							// codestream's code-block bytes.
							//
							// The packets are still parsed. Their headers
							// carry the inclusion and length state the packets
							// after them are read against, and without PLT
							// their bodies are what separates one packet from
							// the next — so what is skipped is the block
							// coder, which is where the time goes.
							if d.skipForResolution(res, numRes) {
								d.skippedBytes += len(cb.data)
								continue
							}
							// A region decode entropy-decodes only the blocks
							// that can reach it. The saving is the point: a
							// viewport should cost a viewport, and the block
							// coder is where a decode spends its time.
							if d.skipForRegion(res, numRes, bg, cbx, cby, tc) {
								d.skippedBytes += len(cb.data)
								continue
							}
							d.regionBytes += len(cb.data)
							bx0 := max(sb.x0, (bg.firstX+cbx)*bg.cbW)
							bx1 := min(sb.x1, (bg.firstX+cbx+1)*bg.cbW)
							w, hh := bx1-bx0, by1-by0
							if w <= 0 || hh <= 0 {
								continue
							}
							// Mb is the band's total magnitude bit-planes; the
							// tag tree above gives how many leading ones are all
							// zero in this block, so the coded planes are the
							// difference. The clamp bounds a file-supplied count
							// against the coefficient word being decoded into.
							mb := h.BandMb(res, b)
							numbps := clampBitPlanes(mb-cb.zeroPlanes, wide)
							if numbps < 1 {
								continue
							}
							var coeffs []int32
							var coeffs64 []int64
							switch {
							case wide && h.IsHTJ2K():
								// The signalled magnitude budget does not fit a
								// 32-bit coefficient word; see wide.go.
								dec := entropy.GetHTDecoder(w, hh)
								coeffs64 = append([]int64(nil),
									dec.Decode64(cb.data, numbps, bg.bandType)...)
								entropy.PutHTDecoder(dec)
							case h.IsHTJ2K():
								dec := entropy.GetHTDecoder(w, hh)
								coeffs = dec.Decode(cb.data, numbps, bg.bandType)
								entropy.PutHTDecoder(dec)
							default:
								// The Part 1 MQ coder carries int32 coefficients
								// and nothing wider. A codestream that declares a
								// budget above 32 bits and codes it with MQ is out
								// of this decoder's reach; widening what MQ
								// produces at least keeps the data flow consistent
								// with the wide tile-component, rather than
								// running the HT decoder over MQ bytes.
								t1 := entropy.NewT1(w, hh)
								coeffs = t1.Decode(cb.data, numbps, bg.bandType)
								if wide {
									coeffs64 = make([]int64, len(coeffs))
									for i, v := range coeffs {
										coeffs64[i] = int64(v)
									}
								}
							}
							// The offsets are relative to the tile-component
							// array, which holds the subbands in the Mallat
							// layout: band origin plus the position of this
							// code-block within the band.
							for yy := 0; yy < hh; yy++ {
								for xx := 0; xx < w; xx++ {
									dx := sb.ox + bx0 - sb.x0 + xx
									dy := sb.oy + by0 - sb.y0 + yy
									if dx < 0 || dy < 0 || dx >= tcW || dy >= tcH {
										continue
									}
									if wide {
										tc.Data64[dy*tcW+dx] = coeffs64[yy*w+xx]
										continue
									}
									tc.Data[dy*tcW+dx] = coeffs[yy*w+xx]
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

// readPacket decodes one packet header and body for the given bands, leaving
// the reader positioned on the packet that follows.
func readPacket(r *pktReader, bands []*bandGeometry, layer int, keep bool) error {
	if r.pos >= len(r.data) {
		return nil
	}
	r.skipSOP()
	if r.readBit() == 0 {
		r.align()
		r.skipEPH()
		return nil // empty packet
	}

	type contrib struct {
		cb     *cbState
		length int
	}
	var order []contrib

	for _, bg := range bands {
		for cby := 0; cby < bg.cbY; cby++ {
			for cbx := 0; cbx < bg.cbX; cbx++ {
				cb := bg.blocks[cby*bg.cbX+cbx]

				var included bool
				if !cb.included {
					// First inclusion is coded in the inclusion tag tree: the
					// value is the layer in which the block first appears.
					_, below := bg.incl.decode(r, cbx, cby, layer+1)
					included = below
				} else {
					included = r.readBit() == 1
				}
				if !included {
					continue
				}

				if !cb.included {
					// Zero bit-planes, coded in its own tag tree with a rising
					// threshold until the value is fully determined.
					v := 0
					for th := 1; ; th++ {
						var ok bool
						v, ok = bg.imsb.decode(r, cbx, cby, th)
						if ok || r.overrun {
							break
						}
						if th > 74 {
							break
						}
					}
					cb.zeroPlanes = v
					cb.included = true
				}

				// The pass count sizes the length field that follows it and
				// is recorded for that reason. The block decoder derives its
				// own pass structure from the bit-plane count instead, so a
				// segment holding fewer passes than its bit-planes imply --
				// which a rate-controlled encoder produces -- is decoded to the
				// end of the data rather than to the last signalled pass.
				passes := readNumPasses(r)
				cb.numPasses += passes

				// Lblock grows by the number of leading one bits.
				for r.readBit() == 1 {
					cb.lblock++
					if r.overrun || cb.lblock > 32 {
						break
					}
				}
				nbits := uint(cb.lblock) + uint(log2Floor(passes))
				length := int(r.readBits(nbits))
				order = append(order, contrib{cb: cb, length: length})
				if r.overrun {
					return nil
				}
			}
		}
	}

	r.align()
	r.skipEPH()

	for _, ct := range order {
		if ct.length < 0 || r.pos+ct.length > len(r.data) {
			return nil
		}
		if keep {
			ct.cb.data = append(ct.cb.data, r.data[r.pos:r.pos+ct.length]...)
		}
		r.pos += ct.length
	}
	return nil
}

// readNumPasses decodes the coding-pass count, per Table B.4.
func readNumPasses(r *pktReader) int {
	if r.readBit() == 0 {
		return 1
	}
	if r.readBit() == 0 {
		return 2
	}
	if v := r.readBits(2); v < 3 {
		return int(v) + 3
	}
	if v := r.readBits(5); v < 31 {
		return int(v) + 6
	}
	return int(r.readBits(7)) + 37
}

func log2Floor(n int) int {
	l := 0
	for n > 1 {
		n >>= 1
		l++
	}
	return l
}

// skipForResolution reports whether a code-block belongs to a resolution the
// decode is not reconstructing.
//
// A decode reduced by k produces resolutions 0 through numRes-1-k; the bands of
// every resolution above that are discarded by the inverse wavelet, so
// entropy-decoding them is pure waste. Before this they were decoded in full
// and thrown away — a reduced decode returned the right samples at every level
// and cost exactly what a full one did, which is the shape of saving that
// isn't one.
func (d *decoder) skipForResolution(res, numRes int) bool {
	if d.reduceRes <= 0 {
		return false
	}
	return res > numRes-1-d.reduceRes
}

// skipForRegion reports whether a code-block cannot contribute to the region
// being decoded, and so need not be entropy-decoded at all.
//
// A block in a band of resolution r covers band coordinates that scale up by
// 2^(numRes-1-r) in the image. The synthesis filter spreads each coefficient
// over a few neighbouring samples at every level it passes through, so the
// block's influence is that rectangle grown by a margin — generously, since
// decoding a block that was not needed costs time and skipping one that was
// costs correctness.
//
// It answers false whenever anything is uncertain: no region, a reduced decode
// (where the resolution count the region was expressed against is not this
// one), or a band whose geometry is degenerate.
func (d *decoder) skipForRegion(res, numRes int, bg *bandGeometry, cbx, cby int, tc *tcd.TileComponent) bool {
	if d.region == nil || bg == nil || tc == nil {
		return false
	}
	// Band coordinates scale to output coordinates by 2^(numRes-1-res) for the
	// LL band at resolution 0, and by one factor more for the detail bands of
	// every resolution above it: those sit at half the resolution's own grid,
	// so a coefficient at band coordinate b reaches resolution samples around
	// 2b before the remaining levels double it again.
	//
	// Ignoring that treats a detail band as if it were an LL band and skips
	// blocks that do reach the region — measured, when this first went in, as
	// 254 of 256 samples wrong in a corner region while the middle of the
	// image was fine.
	shift := uint(numRes - 1 - res)
	if res > 0 {
		shift++
	}

	bx0 := max(bg.sb.x0, (bg.firstX+cbx)*bg.cbW)
	bx1 := min(bg.sb.x1, (bg.firstX+cbx+1)*bg.cbW)
	by0 := max(bg.sb.y0, (bg.firstY+cby)*bg.cbH)
	by1 := min(bg.sb.y1, (bg.firstY+cby+1)*bg.cbH)
	if bx1 <= bx0 || by1 <= by0 {
		return false
	}

	// The margin covers the 5/3 and 9/7 synthesis support at every level this
	// band still passes through: four samples a level is more than either
	// filter reaches.
	margin := 4 << shift
	x0 := (bx0 << shift) - margin
	y0 := (by0 << shift) - margin
	x1 := (bx1 << shift) + margin
	y1 := (by1 << shift) + margin

	r := *d.region
	return x1 <= r.Min.X || x0 >= r.Max.X || y1 <= r.Min.Y || y0 >= r.Max.Y
}
