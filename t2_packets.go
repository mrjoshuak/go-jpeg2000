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
	blocks         []*cbState
	incl           *tagTree
	imsb           *tagTree
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
func bandGridFor(x0, y0, x1, y1, numRes, res, cbWidth, cbHeight int) []*bandGeometry {
	rw, rh := tileResDims(x0, y0, x1, y1, numRes, res)
	if rw <= 0 || rh <= 0 {
		return nil
	}
	numBands := 1
	if res > 0 {
		numBands = 3
	}
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
		firstX, nx := codeBlockRange(sb.x0, sb.x1, cbWidth)
		firstY, ny := codeBlockRange(sb.y0, sb.y1, cbHeight)
		bg := &bandGeometry{
			bandType: bt, sb: sb,
			firstX: firstX, firstY: firstY,
			cbX: nx, cbY: ny,
		}
		bg.blocks = make([]*cbState, nx*ny)
		for i := range bg.blocks {
			bg.blocks[i] = &cbState{lblock: 3}
		}
		bg.incl = newTagTree(nx, ny)
		bg.imsb = newTagTree(nx, ny)
		bands = append(bands, bg)
	}
	return bands
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
func forEachPacket(order codestream.ProgressionOrder, numLayers, numRes, numComp int, visit func(layer, res, comp int) bool) {
	switch order {
	case codestream.RLCP:
		for r := 0; r < numRes; r++ {
			for l := 0; l < numLayers; l++ {
				for c := 0; c < numComp; c++ {
					if !visit(l, r, c) {
						return
					}
				}
			}
		}
	case codestream.RPCL:
		for r := 0; r < numRes; r++ {
			for c := 0; c < numComp; c++ {
				for l := 0; l < numLayers; l++ {
					if !visit(l, r, c) {
						return
					}
				}
			}
		}
	case codestream.PCRL, codestream.CPRL:
		for c := 0; c < numComp; c++ {
			for r := 0; r < numRes; r++ {
				for l := 0; l < numLayers; l++ {
					if !visit(l, r, c) {
						return
					}
				}
			}
		}
	default: // LRCP
		for l := 0; l < numLayers; l++ {
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					if !visit(l, r, c) {
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
	grid := map[resKey][]*bandGeometry{}
	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		for r := 0; r < numRes; r++ {
			bands := bandGridFor(tc.FullX0, tc.FullY0, tc.FullX1, tc.FullY1,
				numRes, r, cbWidth, cbHeight)
			if bands == nil {
				continue
			}
			grid[resKey{c, r}] = bands
		}
	}

	useSOP, useEPH := packetMarkers(h.CodingStyle.CodingStyle)
	r := newPktReader(tileData, useSOP, useEPH)

	// One precinct per resolution: Scod without a precinct partition uses the
	// maximal precinct, so every code-block of a band falls in one packet.
	// Progression order only permutes these packets; with one precinct and the
	// orders in use the resolution-major walk below matches all of them.
	order := codestream.ProgressionOrder(h.CodingStyle.ProgressionOrder)
	var perr error
	forEachPacket(order, numLayers, numRes, len(tile.Components), func(layer, res, c int) bool {
		bands := grid[resKey{c, res}]
		if bands == nil {
			return true
		}
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
			bands := grid[resKey{c, res}]
			for b, bg := range bands {
				sb := bg.sb
				for cby := 0; cby < bg.cbY; cby++ {
					by0 := max(sb.y0, (bg.firstY+cby)*cbHeight)
					by1 := min(sb.y1, (bg.firstY+cby+1)*cbHeight)
					for cbx := 0; cbx < bg.cbX; cbx++ {
						cb := bg.blocks[cby*bg.cbX+cbx]
						if !cb.included || len(cb.data) == 0 {
							continue
						}
						bx0 := max(sb.x0, (bg.firstX+cbx)*cbWidth)
						bx1 := min(sb.x1, (bg.firstX+cbx+1)*cbWidth)
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
