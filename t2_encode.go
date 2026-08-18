package jpeg2000

// Standard T2 packet encoding.
//
// The inverse of t2_packets.go: emit conforming packets so that other
// implementations can read what this library writes. The encoder used to write
// a private container instead — a two-byte code-block count and a fixed-width
// table — which no conforming decoder can interpret. Nothing writes it now:
// both block coders and every quality layer go through the packets below.

import (
	"math/bits"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

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

// t2Contribution is what one code-block gives to one quality layer: the bytes
// of the coding passes that fall in it, and how many passes those are.
type t2Contribution struct {
	data   []byte
	passes int
}

// t2Block is one code-block across every quality layer.
type t2Block struct {
	zeroPlanes int
	layers     []t2Contribution

	// State the packet headers carry from one layer to the next, mirroring
	// cbState on the decoding side. A block is named in the inclusion tag tree
	// once, by the layer it first appears in; every later layer signals its
	// inclusion with a single bit, and the length field keeps growing from the
	// Lblock the earlier layers established.
	included bool
	lblock   int
}

// contributes reports whether this block gives the layer any bytes.
func (b *t2Block) contributes(layer int) bool {
	return b != nil && layer < len(b.layers) && len(b.layers[layer].data) > 0
}

// t2Band groups the code-blocks of one subband.
type t2Band struct {
	cbX, cbY int
	blocks   []*t2Block
	incl     *tagTreeEnc
	imsb     *tagTreeEnc
}

// setTagTreeLeaves records, for every code-block of a band, the layer it first
// contributes to and how many of its magnitude bit-planes are all zero.
//
// Every leaf value must be in place before any of them is coded: setLeaf
// recomputes each parent as the minimum of its children, so setting leaves as
// the packets are written would change a parent after earlier blocks had
// already been written against the old minimum. That is invisible with one
// code-block per band and corrupts every packet with more than one.
func (bg *t2Band) setTagTreeLeaves(numLayers int) {
	for cby := 0; cby < bg.cbY; cby++ {
		for cbx := 0; cbx < bg.cbX; cbx++ {
			cb := bg.blocks[cby*bg.cbX+cbx]
			first := numLayers
			for l := 0; l < numLayers; l++ {
				if cb.contributes(l) {
					first = l
					break
				}
			}
			bg.incl.setLeaf(cbx, cby, first)
			if first < numLayers {
				bg.imsb.setLeaf(cbx, cby, cb.zeroPlanes)
			}
		}
	}
}

// encodePacket writes one packet: header then bodies, for the given bands.
func encodePacket(bands []*t2Band, layer int) []byte {
	w := &pktWriter{}

	anyIncluded := false
	for _, bg := range bands {
		for _, cb := range bg.blocks {
			if cb.contributes(layer) {
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
				included := cb.contributes(layer)

				if !cb.included {
					// First inclusion is coded in the tag tree, whose value is
					// the layer the block first appears in.
					bg.incl.encode(w, cbx, cby, layer+1)
					if !included {
						continue
					}
					// Zero bit-planes, coded until fully determined.
					for th := 1; th <= cb.zeroPlanes+1; th++ {
						bg.imsb.encode(w, cbx, cby, th)
					}
					cb.included = true
				} else {
					// Already included: one bit says whether this layer adds
					// anything.
					if included {
						w.writeBit(1)
					} else {
						w.writeBit(0)
						continue
					}
				}

				ct := cb.layers[layer]
				passes := ct.passes
				if passes < 1 {
					passes = 1
				}
				writeNumPasses(w, passes)

				// Lblock: emit one bit per increment the length needs, then a
				// zero, then the length in lblock + floor(log2(passes)) bits.
				// Lblock persists across layers, which is why it lives on the
				// block rather than here.
				nbits := uint(cb.lblock) + uint(bits.Len(uint(passes))-1)
				for len(ct.data) >= 1<<nbits {
					cb.lblock++
					nbits++
					w.writeBit(1)
				}
				w.writeBit(0)
				w.writeBits(uint32(len(ct.data)), nbits)
				bodies = append(bodies, ct.data)
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
	if !e.options.Lossless {
		guard, steps := e.quantizationParameters()
		exp := 0
		if idx < len(steps) {
			exp = int(steps[idx].Exponent)
		}
		mb := guard + exp - 1
		if mb < 1 {
			mb = 1
		}
		return mb
	}
	if e.wide {
		// Must match generateQCD's wide branch, which signals the measured
		// magnitude budget rather than one derived from the precision.
		if idx < len(e.wideMb) {
			return e.wideMb[idx]
		}
		return 1
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

// layerContributions splits one code-block's coded bytes into quality layers.
//
// truncPoints holds the cumulative byte count after each of the block's coding
// units — one per bit-plane for the MQ coder, a single entry for the HT cleanup
// pass — so a layer boundary can only fall on one of them. Layer l carries the
// units up to (l+1)*n/numLayers, which puts the most significant bit-planes in
// the first layer and refines from there, and the last layer always carries the
// rest so that decoding every layer reproduces the whole block.
//
// The coding-pass count differs between the two block coders. The HT cleanup
// pass is one pass whatever the magnitude budget; the MQ coder emits three
// passes per bit-plane except the most significant, which carries a cleanup
// pass alone, so k bit-planes are 3k-2 passes (ISO/IEC 15444-1 D.3).
func layerContributions(encoded []byte, truncPoints []int, numLayers int, ht bool) []t2Contribution {
	out := make([]t2Contribution, numLayers)
	n := len(truncPoints)
	if len(encoded) == 0 {
		return out
	}
	if n == 0 {
		out[0] = t2Contribution{data: encoded, passes: 1}
		return out
	}
	prevBytes, prevPasses := 0, 0
	for lay := 0; lay < numLayers; lay++ {
		k := (lay + 1) * n / numLayers
		if k < 1 {
			k = 1
		}
		if k > n {
			k = n
		}
		cum := truncPoints[k-1]
		if lay == numLayers-1 {
			k, cum = n, len(encoded)
		}
		if cum > len(encoded) {
			cum = len(encoded)
		}
		if cum <= prevBytes {
			// This layer adds no bytes, so it names no contribution; the
			// passes it would have carried fall to the next layer that does.
			continue
		}
		cumPasses := 1
		if !ht {
			cumPasses = 3*k - 2
		}
		if cumPasses <= prevPasses {
			cumPasses = prevPasses + 1
		}
		out[lay] = t2Contribution{
			data:   encoded[prevBytes:cum],
			passes: cumPasses - prevPasses,
		}
		prevBytes, prevPasses = cum, cumPasses
	}
	return out
}

// buildStandardTileData assembles conforming T2 packets for one tile from the
// per-code-block encoded data.
//
// Packets are emitted in the progression order the COD marker declares, one per
// (layer, resolution, component), which is every packet there is when the
// codestream has a single precinct per resolution. A resolution that has no
// samples carries no precinct, so it carries no packet either: the layout says
// which resolutions are present.
func (e *encoder) buildStandardTileData(layout *tileLayout, jobs []codeBlockJob, encoded [][]byte, numBPS []int, truncPoints [][]int) []byte {
	numRes := layout.numRes
	numComp := 0
	if numRes > 0 {
		numComp = len(layout.res) / numRes
	}
	numLayers := e.options.NumLayers
	if numLayers < 1 {
		numLayers = 1
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
				for i := range bands[b].blocks {
					bands[b].blocks[i] = &t2Block{lblock: 3, layers: make([]t2Contribution, numLayers)}
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
		//
		// The MQ coder is the other case: it codes real magnitude bit-planes,
		// so the count that positions them is the block's own numBPS and the
		// zero bit-planes are the planes of Mb it leaves untouched. Using the
		// HT constant there told every decoder to start two planes down, which
		// is why OpenJPEG read our packets without complaint and reconstructed
		// the wrong samples.
		zp := e.bandMb(j.res, j.bandIdx) - numBPS[i]
		if e.htCoder() {
			const htNumBps = 2
			zp = e.bandMb(j.res, j.bandIdx) + 1 - htNumBps
		}
		if zp < 0 {
			zp = 0
		}
		var tp []int
		if i < len(truncPoints) {
			tp = truncPoints[i]
		}
		cb := bg.blocks[j.cby*bg.cbX+j.cbx]
		cb.zeroPlanes = zp
		cb.layers = layerContributions(encoded[i], tp, numLayers, e.htCoder())
	}

	for _, bands := range bandsFor {
		for _, bg := range bands {
			bg.setTagTreeLeaves(numLayers)
		}
	}

	var out []byte
	order := codestream.ProgressionOrder(e.options.ProgressionOrder)
	forEachPacket(order, numLayers, numRes, numComp, func(layer, res, c int) bool {
		bands := bandsFor[key{c, res}]
		if bands == nil {
			return true
		}
		out = append(out, encodePacket(bands, layer)...)
		return true
	})
	return out
}
