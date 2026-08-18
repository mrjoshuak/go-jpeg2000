package jpeg2000

// Standard T2 packet encoding.
//
// The inverse of t2_packets.go: emit conforming packets so that other
// implementations can read what this library writes. Previously the encoder
// wrote a private container (a two-byte code-block count and a fixed-width
// table), which no conforming decoder can interpret.

import "math/bits"

// pktWriter emits packet-header bits with JPEG 2000 bit stuffing: after a byte
// of 0xFF the next byte carries only seven bits, its high bit forced to zero.
type pktWriter struct {
	buf    []byte
	acc    uint32
	nbits  uint
	lastFF bool
}

func (w *pktWriter) writeBit(b uint32) {
	limit := uint(8)
	if w.lastFF {
		limit = 7
	}
	w.acc = (w.acc << 1) | (b & 1)
	w.nbits++
	if w.nbits == limit {
		v := byte(w.acc)
		w.buf = append(w.buf, v)
		w.lastFF = v == 0xFF
		w.acc = 0
		w.nbits = 0
	}
}

func (w *pktWriter) writeBits(v uint32, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		w.writeBit((v >> uint(i)) & 1)
	}
}

// flush pads the current byte with zeros to a byte boundary.
func (w *pktWriter) flush() {
	if w.nbits > 0 {
		limit := uint(8)
		if w.lastFF {
			limit = 7
		}
		w.acc <<= limit - w.nbits
		v := byte(w.acc)
		w.buf = append(w.buf, v)
		w.lastFF = v == 0xFF
		w.acc = 0
		w.nbits = 0
	}
	// A header that ends on 0xFF must be followed by a stuffing byte, or the
	// decoder will read the next byte's high bit as data.
	if w.lastFF {
		w.buf = append(w.buf, 0x00)
		w.lastFF = false
	}
}

// tagTreeEnc encodes values into a tag tree, mirroring tagTree.decode.
type tagTreeEnc struct {
	levels int
	dimW   []int
	dimH   []int
	value  [][]int
	state  [][]int  // per-node lower bound already emitted
	known  [][]bool // per-node: the value has been established in the stream
}

func newTagTreeEnc(w, h int) *tagTreeEnc {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	t := &tagTreeEnc{}
	lw, lh := w, h
	for {
		t.dimW = append(t.dimW, lw)
		t.dimH = append(t.dimH, lh)
		t.value = append(t.value, make([]int, lw*lh))
		t.state = append(t.state, make([]int, lw*lh))
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

// setLeaf records a leaf value and propagates the minimum up the tree, which is
// what makes the coded representation compact.
func (t *tagTreeEnc) setLeaf(x, y, v int) {
	t.value[0][y*t.dimW[0]+x] = v
	for lvl := 1; lvl < t.levels; lvl++ {
		xx, yy := x>>uint(lvl), y>>uint(lvl)
		idx := yy*t.dimW[lvl] + xx
		// Recompute as the minimum of the children.
		min := -1
		pw, ph := t.dimW[lvl-1], t.dimH[lvl-1]
		for dy := 0; dy < 2; dy++ {
			for dx := 0; dx < 2; dx++ {
				cx, cy := xx*2+dx, yy*2+dy
				if cx >= pw || cy >= ph {
					continue
				}
				cv := t.value[lvl-1][cy*pw+cx]
				if min < 0 || cv < min {
					min = cv
				}
			}
		}
		if min < 0 {
			min = 0
		}
		t.value[lvl][idx] = min
	}
}

// encode emits the bits establishing whether the leaf value is below threshold.
//
// This mirrors tagTree.decode bit for bit: walk root to leaf, and at each node
// emit a zero for every step the lower bound has to climb and a one when it
// reaches the node's value. The `known` flag is what makes the two agree — a
// node whose value is already established emits nothing on a later visit, and
// without it the encoder re-emitted bits the decoder never reads.
func (t *tagTreeEnc) encode(w *pktWriter, x, y, threshold int) {
	low := 0
	for lvl := t.levels - 1; lvl >= 0; lvl-- {
		xx, yy := x>>uint(lvl), y>>uint(lvl)
		idx := yy*t.dimW[lvl] + xx
		if t.state[lvl][idx] < low {
			t.state[lvl][idx] = low
		}
		v := t.value[lvl][idx]
		for !t.known[lvl][idx] && t.state[lvl][idx] < threshold {
			if t.state[lvl][idx] == v {
				w.writeBit(1)
				t.known[lvl][idx] = true
			} else {
				w.writeBit(0)
				t.state[lvl][idx]++
			}
		}
		low = t.state[lvl][idx]
		if !t.known[lvl][idx] {
			// Value is at or above the threshold; nothing below is coded.
			return
		}
	}
}

// t2Block is one code-block's contribution to a packet.
type t2Block struct {
	data       []byte
	zeroPlanes int
	numPasses  int
}

// t2Band groups the code-blocks of one subband.
type t2Band struct {
	cbX, cbY int
	blocks   []*t2Block
	incl     *tagTreeEnc
	imsb     *tagTreeEnc
}

// encodePacket writes one packet: header then bodies, for the given bands.
func encodePacket(bands []*t2Band, layer int) []byte {
	w := &pktWriter{}

	anyIncluded := false
	for _, bg := range bands {
		for _, cb := range bg.blocks {
			if cb != nil && len(cb.data) > 0 {
				anyIncluded = true
			}
		}
	}
	if !anyIncluded {
		w.writeBit(0) // empty packet
		w.flush()
		return w.buf
	}
	w.writeBit(1)

	// Every leaf value must be in place before any of them is coded: setLeaf
	// recomputes each parent as the minimum of its children, so setting leaves
	// as we go would change a parent after earlier blocks had already been
	// written against the old minimum. That is invisible with one code-block
	// per band and corrupts every packet with more than one.
	for _, bg := range bands {
		for cby := 0; cby < bg.cbY; cby++ {
			for cbx := 0; cbx < bg.cbX; cbx++ {
				cb := bg.blocks[cby*bg.cbX+cbx]
				if cb != nil && len(cb.data) > 0 {
					bg.incl.setLeaf(cbx, cby, layer)
					bg.imsb.setLeaf(cbx, cby, cb.zeroPlanes)
				} else {
					// Not included in this layer: a value above it.
					bg.incl.setLeaf(cbx, cby, layer+1)
				}
			}
		}
	}

	var bodies [][]byte
	for _, bg := range bands {
		for cby := 0; cby < bg.cbY; cby++ {
			for cbx := 0; cbx < bg.cbX; cbx++ {
				cb := bg.blocks[cby*bg.cbX+cbx]
				included := cb != nil && len(cb.data) > 0

				bg.incl.encode(w, cbx, cby, layer+1)
				if !included {
					continue
				}

				// Zero bit-planes, coded until fully determined.
				for th := 1; th <= cb.zeroPlanes+1; th++ {
					bg.imsb.encode(w, cbx, cby, th)
				}

				writeNumPasses(w, cb.numPasses)

				// Lblock: emit no increments, then the length in
				// 3 + floor(log2(passes)) bits, widening if it does not fit.
				lblock := 3
				nbits := uint(lblock) + uint(bits.Len(uint(cb.numPasses))-1)
				for len(cb.data) >= 1<<nbits {
					lblock++
					nbits++
					w.writeBit(1)
				}
				w.writeBit(0)
				w.writeBits(uint32(len(cb.data)), nbits)
				bodies = append(bodies, cb.data)
			}
		}
	}

	w.flush()
	out := w.buf
	for _, b := range bodies {
		out = append(out, b...)
	}
	return out
}

// writeNumPasses encodes the coding-pass count, per Table B.4.
func writeNumPasses(w *pktWriter, n int) {
	switch {
	case n <= 1:
		w.writeBit(0)
	case n == 2:
		w.writeBit(1)
		w.writeBit(0)
	case n <= 5:
		w.writeBit(1)
		w.writeBit(1)
		w.writeBits(uint32(n-3), 2)
	case n <= 36:
		w.writeBit(1)
		w.writeBit(1)
		w.writeBits(3, 2)
		w.writeBits(uint32(n-6), 5)
	default:
		w.writeBit(1)
		w.writeBit(1)
		w.writeBits(3, 2)
		w.writeBits(31, 5)
		w.writeBits(uint32(n-37), 7)
	}
}

// bandMb mirrors the exponent the encoder writes into QCD, so that the
// zero-bit-plane counts in the packet headers agree with what a decoder derives
// from that marker.
func (e *encoder) bandMb(res, bandIdx int) int {
	idx := 0
	if res > 0 {
		idx = 1 + (res-1)*3 + bandIdx
	}
	maxPrec := e.maxPrecision()
	// Must match generateQCD.
	guardBits := 2
	if maxPrec > 16 {
		guardBits = 2
	}
	exp := maxPrec + idx/3
	if exp > 31 {
		exp = 31
	}
	mb := guardBits + exp - 1
	if mb < 1 {
		mb = 1
	}
	return mb
}

// buildStandardTileData assembles conforming T2 packets for one tile from the
// per-code-block encoded data.
//
// Packets are emitted resolution-major, one per (resolution, component), which
// is the order every progression produces when there is a single precinct and
// a single layer. A resolution that has no samples carries no precinct, so it
// carries no packet either: the layout says which resolutions are present.
func (e *encoder) buildStandardTileData(layout *tileLayout, jobs []codeBlockJob, encoded [][]byte, numBPS []int, passes []int) []byte {
	numRes := layout.numRes
	numComp := 0
	if numRes > 0 {
		numComp = len(layout.res) / numRes
	}

	// The code-block grid of every band comes from the tile geometry, not from
	// the jobs: a band can legitimately hold no code-blocks, and a packet must
	// then say nothing about it rather than describe a phantom block.
	type key struct{ c, r int }
	bandsFor := map[key][]*t2Band{}
	for c := 0; c < numComp; c++ {
		for r := 0; r < numRes; r++ {
			rl := layout.at(c, r)
			if !rl.present {
				continue
			}
			bands := make([]*t2Band, len(rl.bands))
			for b, bl := range rl.bands {
				bands[b] = &t2Band{
					cbX: bl.cbX, cbY: bl.cbY,
					blocks: make([]*t2Block, bl.cbX*bl.cbY),
					incl:   newTagTreeEnc(bl.cbX, bl.cbY),
					imsb:   newTagTreeEnc(bl.cbX, bl.cbY),
				}
			}
			bandsFor[key{c, r}] = bands
		}
	}

	// Place each code-block's data.
	for i, j := range jobs {
		if i >= len(encoded) {
			break
		}
		bands := bandsFor[key{j.comp, j.res}]
		if bands == nil || j.bandIdx >= len(bands) {
			continue
		}
		bg := bands[j.bandIdx]
		if j.cbx >= bg.cbX || j.cby >= bg.cbY {
			continue
		}
		if len(encoded[i]) == 0 {
			continue
		}
		// The signalled bit-plane count positions the magnitudes a conforming
		// decoder reconstructs: it computes (v_n + 2) << (numbps - 1).
		//
		// encodeCleanupHT emits v_n = 2*mu - 2 + s with no bit-plane shift of
		// its own, so (v_n + 2) is 2*mu and numbps = 2 is what places the
		// result in the domain the reference expects. Verified end to end:
		// OpenJPH decodes such a file to the exact source samples, and gets
		// half of them at numbps = 1. OpenJPH signals the same 2 for 8-bit
		// input, with 6 zero bit-planes against Mb = 7.
		//
		// numBPS is the magnitude bit count, which HT carries per quad in U_q
		// rather than here, so it does not enter this calculation.
		const htNumBps = 2
		zp := e.bandMb(j.res, j.bandIdx) + 1 - htNumBps
		_ = numBPS[i]
		if zp < 0 {
			zp = 0
		}
		// The HT cleanup pass is a single coding pass; the MQ coder emits one
		// per bit-plane pass and the packet header must say how many, or a
		// decoder mis-sizes the length field that follows.
		np := 1
		if i < len(passes) && passes[i] > 0 {
			np = passes[i]
		}
		bg.blocks[j.cby*bg.cbX+j.cbx] = &t2Block{
			data:       encoded[i],
			zeroPlanes: zp,
			numPasses:  np,
		}
	}

	var out []byte
	for res := 0; res < numRes; res++ {
		for c := 0; c < numComp; c++ {
			bands := bandsFor[key{c, res}]
			if bands == nil {
				continue
			}
			out = append(out, encodePacket(bands, 0)...)
		}
	}
	return out
}
