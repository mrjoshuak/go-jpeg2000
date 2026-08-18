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
	state  [][]int // per-node lower bound already emitted
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
func (t *tagTreeEnc) encode(w *pktWriter, x, y, threshold int) {
	low := 0
	for lvl := t.levels - 1; lvl >= 0; lvl-- {
		xx, yy := x>>uint(lvl), y>>uint(lvl)
		idx := yy*t.dimW[lvl] + xx
		if t.state[lvl][idx] < low {
			t.state[lvl][idx] = low
		}
		v := t.value[lvl][idx]
		for t.state[lvl][idx] < threshold {
			if t.state[lvl][idx] < v {
				w.writeBit(0)
				t.state[lvl][idx]++
				continue
			}
			w.writeBit(1)
			break
		}
		if t.state[lvl][idx] < v || t.state[lvl][idx] >= threshold && t.state[lvl][idx] != v {
			// Value not yet established at this node; nothing below is coded.
			return
		}
		low = v
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

	var bodies [][]byte
	for _, bg := range bands {
		for cby := 0; cby < bg.cbY; cby++ {
			for cbx := 0; cbx < bg.cbX; cbx++ {
				cb := bg.blocks[cby*bg.cbX+cbx]
				included := cb != nil && len(cb.data) > 0

				if included {
					bg.incl.setLeaf(cbx, cby, layer)
				} else {
					// Not included in this layer: code a value above it.
					bg.incl.setLeaf(cbx, cby, layer+1)
				}
				bg.incl.encode(w, cbx, cby, layer+1)
				if !included {
					continue
				}

				// Zero bit-planes, coded until fully determined.
				bg.imsb.setLeaf(cbx, cby, cb.zeroPlanes)
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
	guardBits := 0
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
// a single layer.
func (e *encoder) buildStandardTileData(jobs []codeBlockJob, encoded [][]byte, numBPS []int) []byte {
	type key struct{ c, r int }
	bandsFor := map[key][]*t2Band{}
	maxRes, maxComp := 0, 0

	// Size each band's code-block grid from the jobs that fall in it.
	dims := map[key]map[int][2]int{}
	for _, j := range jobs {
		k := key{j.comp, j.res}
		if dims[k] == nil {
			dims[k] = map[int][2]int{}
		}
		d := dims[k][j.bandIdx]
		if j.cbx+1 > d[0] {
			d[0] = j.cbx + 1
		}
		if j.cby+1 > d[1] {
			d[1] = j.cby + 1
		}
		dims[k][j.bandIdx] = d
		if j.res > maxRes {
			maxRes = j.res
		}
		if j.comp > maxComp {
			maxComp = j.comp
		}
	}
	for k, bd := range dims {
		nb := 1
		if k.r > 0 {
			nb = 3
		}
		bands := make([]*t2Band, nb)
		for b := 0; b < nb; b++ {
			d := bd[b]
			nx, ny := d[0], d[1]
			if nx < 1 {
				nx = 1
			}
			if ny < 1 {
				ny = 1
			}
			bands[b] = &t2Band{
				cbX: nx, cbY: ny,
				blocks: make([]*t2Block, nx*ny),
				incl:   newTagTreeEnc(nx, ny),
				imsb:   newTagTreeEnc(nx, ny),
			}
		}
		bandsFor[k] = bands
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
		bg.blocks[j.cby*bg.cbX+j.cbx] = &t2Block{
			data:       encoded[i],
			zeroPlanes: zp,
			numPasses:  1, // HT cleanup, and one MQ pass segment
		}
	}

	var out []byte
	for res := 0; res <= maxRes; res++ {
		for c := 0; c <= maxComp; c++ {
			bands := bandsFor[key{c, res}]
			if bands == nil {
				continue
			}
			out = append(out, encodePacket(bands, 0)...)
		}
	}
	return out
}
