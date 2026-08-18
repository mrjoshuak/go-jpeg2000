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
// could read our output and why we recovered nothing from theirs.

import (
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/tcd"
)

// pktReader reads packet-header bits, applying the bit-unstuffing rule: a byte
// following one equal to 0xFF carries only seven bits.
type pktReader struct {
	data    []byte
	pos     int
	bitBuf  uint32
	bitCnt  uint
	lastFF  bool
	overrun bool
}

func newPktReader(data []byte) *pktReader { return &pktReader{data: data} }

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
	w, h     int
	cbX, cbY int
	blocks   []*cbState
	incl     *tagTree
	imsb     *tagTree
}

// decodeStandardTileData decodes conforming T2 packets for one tile and writes
// the recovered coefficients into the tile components.
func (d *decoder) decodeStandardTileData(tile *tcd.Tile, tileData []byte) error {
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

	// Build the code-block grid for every (component, resolution, band).
	type resKey struct{ c, r int }
	grid := map[resKey][]*bandGeometry{}
	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		tcW := tc.X1 - tc.X0
		tcH := tc.Y1 - tc.Y0
		for r := 0; r < numRes; r++ {
			numBands := 1
			if r > 0 {
				numBands = 3
			}
			var bands []*bandGeometry
			for b := 0; b < numBands; b++ {
				scale := 1 << uint(numRes-1-r)
				bw := (tcW + scale - 1) / scale
				bh := (tcH + scale - 1) / scale
				if r > 0 {
					bw = (bw + 1) / 2
					bh = (bh + 1) / 2
				}
				bt := entropy.BandLL
				if r > 0 {
					switch b {
					case 0:
						bt = entropy.BandHL
					case 1:
						bt = entropy.BandLH
					default:
						bt = entropy.BandHH
					}
				}
				nx, ny := 0, 0
				for cbx := 0; cbx*cbWidth < bw; cbx++ {
					nx++
				}
				for cby := 0; cby*cbHeight < bh; cby++ {
					ny++
				}
				bg := &bandGeometry{bandType: bt, w: bw, h: bh, cbX: nx, cbY: ny}
				bg.blocks = make([]*cbState, nx*ny)
				for i := range bg.blocks {
					bg.blocks[i] = &cbState{lblock: 3}
				}
				bg.incl = newTagTree(nx, ny)
				bg.imsb = newTagTree(nx, ny)
				bands = append(bands, bg)
			}
			grid[resKey{c, r}] = bands
		}
	}

	r := newPktReader(tileData)

	// One precinct per resolution: Scod without a precinct partition uses the
	// maximal precinct, so every code-block of a band falls in one packet.
	// Progression order only permutes these packets; with one precinct and the
	// orders in use the resolution-major walk below matches all of them.
	for layer := 0; layer < numLayers; layer++ {
		for res := 0; res < numRes; res++ {
			for c := 0; c < len(tile.Components); c++ {
				bands := grid[resKey{c, res}]
				if bands == nil {
					continue
				}
				if err := d.readPacket(r, bands, layer); err != nil {
					return err
				}
				if r.overrun {
					break
				}
			}
		}
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
				xOff, yOff := computeSubbandOffset(tcW, tcH, numRes, res, bg.bandType)
				for cby := 0; cby < bg.cbY; cby++ {
					for cbx := 0; cbx < bg.cbX; cbx++ {
						cb := bg.blocks[cby*bg.cbX+cbx]
						if !cb.included || len(cb.data) == 0 {
							continue
						}
						w := cbWidth
						if cbx*cbWidth+w > bg.w {
							w = bg.w - cbx*cbWidth
						}
						hh := cbHeight
						if cby*cbHeight+hh > bg.h {
							hh = bg.h - cby*cbHeight
						}
						if w <= 0 || hh <= 0 {
							continue
						}
						// Mb is the band's total magnitude bit-planes; the
						// tag-tree above gives how many leading ones are all
						// zero in this block, so the coded planes are the
						// difference. cb.zeroPlanes already holds the decoded
						// tag-tree value, not the threshold that resolved it,
						// so there is no extra plane to add back.
						mb := h.BandMb(res, b)
						numbps := mb - cb.zeroPlanes
						if numbps < 1 {
							continue
						}
						var coeffs []int32
						if h.IsHTJ2K() {
							dec := entropy.GetHTDecoder(w, hh)
							coeffs = dec.Decode(cb.data, numbps, bg.bandType)
							entropy.PutHTDecoder(dec)
						} else {
							t1 := entropy.NewT1(w, hh)
							coeffs = t1.Decode(cb.data, numbps, bg.bandType)
						}
						for yy := 0; yy < hh; yy++ {
							for xx := 0; xx < w; xx++ {
								dx := xOff + cbx*cbWidth + xx
								dy := yOff + cby*cbHeight + yy
								if dx < 0 || dy < 0 || dx >= tcW || dy >= tcH {
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

// readPacket decodes one packet header and body for the given bands.
func (d *decoder) readPacket(r *pktReader, bands []*bandGeometry, layer int) error {
	if r.pos >= len(r.data) {
		return nil
	}
	if r.readBit() == 0 {
		r.align()
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

	for _, ct := range order {
		if ct.length < 0 || r.pos+ct.length > len(r.data) {
			return nil
		}
		ct.cb.data = append(ct.cb.data, r.data[r.pos:r.pos+ct.length]...)
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
