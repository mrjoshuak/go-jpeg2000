package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// PacketAddress uniquely identifies a packet within a codestream.
type PacketAddress struct {
	Tile       uint16
	Resolution uint8
	Layer      uint16
	Component  uint8
	Precinct   uint16
}

// Packet is a wavelet packet: the atomic unit of progressive data.
type Packet struct {
	Address PacketAddress
	Data    []byte
}

// packetEntry records a packet's self-contained data.
type packetEntry struct {
	addr PacketAddress
	data []byte
	// offset is where this packet begins in the whole codestream, which is
	// what a ranged read over the network needs; data alone only says how long
	// it is and requires the codestream in hand to locate.
	offset int
	// The image-space rectangle this packet covers. With an explicit precinct
	// partition each packet holds one region rather than the whole image, so
	// this is what makes a viewport resolvable to byte ranges.
	x0, y0, x1, y1 int
}

// PacketRange locates one packet's bytes inside the codestream it came from.
// Offset is from the start of the codestream, so a pair of these is directly a
// HTTP Range header.
type PacketRange struct {
	Offset int
	Length int
}

// PacketIndex maps packet addresses to their self-contained data.
type PacketIndex struct {
	entries []packetEntry
	addrMap map[PacketAddress]int // maps address to entries index
}

// ExtractPackets parses a J2K codestream and returns all packets with their data.
func ExtractPackets(cs []byte) ([]Packet, error) {
	idx, err := BuildPacketIndex(cs)
	if err != nil {
		return nil, err
	}

	packets := make([]Packet, len(idx.entries))
	for i, entry := range idx.entries {
		data := make([]byte, len(entry.data))
		copy(data, entry.data)
		packets[i] = Packet{
			Address: entry.addr,
			Data:    data,
		}
	}
	return packets, nil
}

// BuildPacketIndex parses a J2K codestream and records byte offsets for each
// packet. More memory-efficient than ExtractPackets for large codestreams.
func BuildPacketIndex(cs []byte) (*PacketIndex, error) {
	if len(cs) < 2 {
		return nil, fmt.Errorf("codestream too short")
	}

	// Parse main header
	header, headerEnd, err := parseMainHeader(cs)
	if err != nil {
		return nil, fmt.Errorf("parsing main header: %w", err)
	}

	idx := &PacketIndex{
		addrMap: make(map[PacketAddress]int),
	}

	// Walk tile-parts
	pos := headerEnd
	for pos < len(cs) {
		if pos+2 > len(cs) {
			break
		}
		marker := binary.BigEndian.Uint16(cs[pos : pos+2])

		if marker == uint16(codestream.EOC) {
			break
		}

		if marker != uint16(codestream.SOT) {
			return nil, fmt.Errorf("expected SOT marker at offset %d, got 0x%04X", pos, marker)
		}

		if pos+12 > len(cs) {
			return nil, fmt.Errorf("truncated SOT at offset %d", pos)
		}

		// Parse SOT
		sotLen := binary.BigEndian.Uint16(cs[pos+2 : pos+4])
		if sotLen != 10 {
			return nil, fmt.Errorf("unexpected SOT length %d", sotLen)
		}
		tileIndex := binary.BigEndian.Uint16(cs[pos+4 : pos+6])
		tilePartLength := binary.BigEndian.Uint32(cs[pos+6 : pos+10])

		// Psot is the tile-part length including the SOT segment. Zero means
		// "to the end of the codestream" (ISO/IEC 15444-1 A.4.2); taking it
		// literally would leave pos unchanged and loop forever.
		tpStart := pos
		tpEnd := len(cs)
		if tilePartLength > 0 {
			if uint64(tpStart)+uint64(tilePartLength) < uint64(len(cs)) {
				tpEnd = tpStart + int(tilePartLength)
			}
			if tpEnd <= tpStart {
				return nil, fmt.Errorf("tile-part at offset %d declares length %d, which does not advance",
					pos, tilePartLength)
			}
		}

		// Skip SOT header to find SOD
		tpHeaderPos := pos + 12 // after SOT marker segment

		// Scan for SOD marker in tile-part header. The scan bound is clamped
		// to the real end of the buffer: Psot is file-supplied and routinely
		// larger than the bytes actually present in a truncated file.
		sodPos := -1
		for p := tpHeaderPos; p+1 < tpEnd; p++ {
			m := binary.BigEndian.Uint16(cs[p : p+2])
			if m == uint16(codestream.SOD) {
				sodPos = p + 2 // data starts after SOD marker
				break
			}
		}
		if sodPos < 0 || sodPos > tpEnd {
			return nil, fmt.Errorf("no SOD marker found in tile-part at offset %d", pos)
		}

		// Tile data extends from SOD to end of tile-part
		tileDataEnd := tpEnd

		// Index packets in this tile
		if err := idx.indexTilePackets(header, tileIndex, cs, sodPos, tileDataEnd); err != nil {
			return nil, fmt.Errorf("indexing tile %d packets: %w", tileIndex, err)
		}

		// Advance to next tile-part
		pos = tpEnd
	}

	return idx, nil
}

// GetPacket returns the packet data for the given address.
func (idx *PacketIndex) GetPacket(addr PacketAddress) ([]byte, error) {
	i, ok := idx.addrMap[addr]
	if !ok {
		return nil, fmt.Errorf("packet not found: tile=%d res=%d layer=%d comp=%d prec=%d",
			addr.Tile, addr.Resolution, addr.Layer, addr.Component, addr.Precinct)
	}
	entry := idx.entries[i]
	data := make([]byte, len(entry.data))
	copy(data, entry.data)
	return data, nil
}

// AllAddresses returns all packet addresses in codestream order.
func (idx *PacketIndex) AllAddresses() []PacketAddress {
	addrs := make([]PacketAddress, len(idx.entries))
	for i, entry := range idx.entries {
		addrs[i] = entry.addr
	}
	return addrs
}

// Len returns the number of packets in the index.
func (idx *PacketIndex) Len() int {
	return len(idx.entries)
}

// parseMainHeader parses the main header from a raw J2K codestream byte slice.
// Returns the parsed header and the byte offset where the first tile-part begins.
func parseMainHeader(cs []byte) (*codestream.Header, int, error) {
	parser := codestream.NewParser(bytes.NewReader(cs))
	header, err := parser.ReadHeader()
	if err != nil {
		return nil, 0, err
	}

	// Calculate main header size by scanning for first SOT
	pos := 2 // skip SOC
	for pos+3 < len(cs) {
		marker := binary.BigEndian.Uint16(cs[pos : pos+2])
		if marker == uint16(codestream.SOT) {
			return header, pos, nil
		}
		// Skip marker segment
		if marker == uint16(codestream.SOC) || marker == uint16(codestream.SOD) || marker == uint16(codestream.EOC) || marker == uint16(codestream.EPH) {
			pos += 2
			continue
		}
		segLen := binary.BigEndian.Uint16(cs[pos+2 : pos+4])
		pos += 2 + int(segLen)
	}
	return nil, 0, fmt.Errorf("no SOT marker found in codestream")
}

// indexTilePackets indexes all packets within a tile's data region.
//
// A conforming packet is self-describing: a tag-tree coded header naming the
// code-blocks that contribute, their zero bit-planes and the byte count of
// each, followed by those bytes. Its extent is therefore found by parsing it,
// and the bytes from one packet's start to the next are exactly that packet.
//
// This used to parse a private table that only this library ever wrote, and
// returned empty packets for every conforming codestream.
func (idx *PacketIndex) indexTilePackets(
	header *codestream.Header,
	tileIndex uint16,
	cs []byte,
	dataStart, dataEnd int,
) error {
	numComp := int(header.NumComponents)
	numRes := header.CodingStyle.NumResolutions()
	numLayers := header.CodingStyle.Layers()

	if numComp <= 0 || len(header.ComponentInfo) != numComp {
		return fmt.Errorf("SIZ declares %d components but carries %d component records",
			header.NumComponents, len(header.ComponentInfo))
	}
	if numRes > codestream.MaxDecompositionLevels+1 {
		return fmt.Errorf("COD declares %d resolution levels, above the %d limit",
			numRes, codestream.MaxDecompositionLevels+1)
	}
	if header.NumTilesX == 0 || header.NumTilesY == 0 {
		return fmt.Errorf("tile grid is %dx%d, must be at least 1x1",
			header.NumTilesX, header.NumTilesY)
	}
	// numComp, numRes and numLayers are three independent header fields whose
	// product is the number of packet records appended below.
	// Bound by the codestream's own length: a packet costs at least one byte on
	// the wire, so a file cannot describe more packets than it has bytes.
	if n := uint64(numComp) * uint64(numRes) * uint64(numLayers); n > maxPacketsForInput(len(cs)) {
		return fmt.Errorf("tile describes %d packets, more than the %d a %d-byte codestream can carry",
			n, maxPacketsForInput(len(cs)), len(cs))
	}
	if dataStart < 0 || dataEnd > len(cs) || dataStart > dataEnd {
		return fmt.Errorf("tile data range [%d,%d) is outside the %d-byte codestream",
			dataStart, dataEnd, len(cs))
	}

	// Calculate code-block dimensions. These are clamped by CodeBlockWidth /
	// CodeBlockHeight to the 4..1024 range the standard allows, so the
	// divisions below cannot fault.
	cbWidth := header.CodingStyle.CodeBlockWidth()
	cbHeight := header.CodingStyle.CodeBlockHeight()

	// Calculate tile dimensions
	tileX := int(tileIndex) % int(header.NumTilesX)
	tileY := int(tileIndex) / int(header.NumTilesX)

	tx0 := max(int(header.TileXOffset)+tileX*int(header.TileWidth), int(header.ImageXOffset))
	ty0 := max(int(header.TileYOffset)+tileY*int(header.TileHeight), int(header.ImageYOffset))
	tx1 := min(int(header.TileXOffset)+(tileX+1)*int(header.TileWidth), int(header.ImageWidth))
	ty1 := min(int(header.TileYOffset)+(tileY+1)*int(header.TileHeight), int(header.ImageHeight))

	tileData := cs[dataStart:dataEnd]

	// Walk the tile's packets. A conforming packet is self-describing — a
	// tag-tree coded header naming the contributing code-blocks and the byte
	// count of each — so its extent is found by parsing it, and the bytes
	// between one packet's start and the next are exactly that packet.
	//
	// This used to parse a private table this library wrote and no other
	// implementation produces, and returned empty packets for everything else.
	type crKey struct {
		comp int
		res  int
	}
	grids := make(map[crKey][][]*bandGeometry, numComp*numRes)
	for c := 0; c < numComp; c++ {
		comp := header.ComponentInfo[c]
		// The subband partition is derived from the tile component's absolute
		// coordinates, so subsampling is applied to the tile's corners rather
		// than to its size. See tileBands in subband.go, which the encoder and
		// the tile decoder walk as well.
		cx0 := ceilDivInt(tx0, int(comp.SubsamplingX))
		cy0 := ceilDivInt(ty0, int(comp.SubsamplingY))
		cx1 := ceilDivInt(tx1, int(comp.SubsamplingX))
		cy1 := ceilDivInt(ty1, int(comp.SubsamplingY))
		for r := 0; r < numRes; r++ {
			ppx, ppy := precinctExps(header.CodingStyle, r)
			grids[crKey{c, r}] = precinctsFor(cx0, cy0, cx1, cy1, numRes, r,
				cbWidth, cbHeight, ppx, ppy)
		}
	}

	sop, eph := packetMarkers(header.CodingStyle.CodingStyle)
	reader := newPktReader(tileData, sop, eph)

	// Generate packet addresses in progression order and record each packet's
	// own bytes.
	order := codestream.ProgressionOrder(header.CodingStyle.ProgressionOrder)

	pos := &posInfo{tx0: tx0, ty0: ty0, tx1: tx1, ty1: ty1}
	for c := 0; c < numComp; c++ {
		sx, sy := 1, 1
		if c < len(header.ComponentInfo) {
			sx, sy = int(header.ComponentInfo[c].SubsamplingX), int(header.ComponentInfo[c].SubsamplingY)
		}
		pos.dx, pos.dy = append(pos.dx, max(sx, 1)), append(pos.dy, max(sy, 1))
	}
	for r := 0; r < numRes; r++ {
		px, py := precinctExps(header.CodingStyle, r)
		pos.ppx, pos.ppy = append(pos.ppx, px), append(pos.ppy, py)
	}
	pos.pw = func(r, c int) int {
		comp := header.ComponentInfo[c]
		w, _ := precinctGridDims(
			ceilDivInt(tx0, int(comp.SubsamplingX)), ceilDivInt(ty0, int(comp.SubsamplingY)),
			ceilDivInt(tx1, int(comp.SubsamplingX)), ceilDivInt(ty1, int(comp.SubsamplingY)),
			numRes, r, pos.ppx[r], pos.ppy[r])
		return w
	}

	addPacket := func(l, r, c, p int) {
		addr := PacketAddress{
			Tile:       tileIndex,
			Resolution: uint8(r),
			Layer:      uint16(l),
			Component:  uint8(c),
			Precinct:   uint16(p),
		}
		var data []byte
		var off int
		precincts := grids[crKey{c, r}]
		var bands []*bandGeometry
		if p < len(precincts) {
			bands = precincts[p]
		}
		if bands != nil && !reader.overrun && reader.pos < len(tileData) {
			from := reader.pos
			if err := readPacket(reader, bands, l, true); err == nil && !reader.overrun {
				data = tileData[from:reader.pos]
				off = dataStart + from
			} else {
				// A packet that does not parse ends the walk: the position of
				// every packet after it is unknown.
				reader.overrun = true
			}
		}
		px0, py0, px1, py1 := packetImageRect(header, tx0, ty0, tx1, ty1, numRes, r, c, p,
			pos.ppx[r], pos.ppy[r], pos.pw(r, c))
		entryIdx := len(idx.entries)
		idx.entries = append(idx.entries, packetEntry{
			addr: addr, data: data, offset: off,
			x0: px0, y0: py0, x1: px1, y1: py1,
		})
		idx.addrMap[addr] = entryIdx
	}

	// Precinct is a real coordinate now: with an explicit partition a
	// resolution holds many packets, each covering one region of the image,
	// which is what makes a byte range spatially addressable.
	numPrec := func(r, c int) int { return len(grids[crKey{c, r}]) }

	forEachPacket(order, numLayers, numRes, numComp, numPrec, pos, func(l, r, c, p int) bool {
		addPacket(l, r, c, p)
		return true
	})

	return nil
}

func ceilDivInt(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

// packetImageRect returns the image-space rectangle one packet covers: the
// precinct's rectangle at its own resolution, scaled back up to full
// resolution and to the component's grid.
//
// A precinct at resolution r covers 2^(numRes-1-r) times its own extent in
// image coordinates, so the same precinct index names a larger region at a
// lower resolution. That is the property a viewport query depends on, and the
// reason a region cannot be resolved by precinct index alone.
func packetImageRect(header *codestream.Header, tx0, ty0, tx1, ty1, numRes, res, comp, prec, ppx, ppy, pw int) (int, int, int, int) {
	if pw <= 0 {
		return 0, 0, 0, 0
	}
	ci := header.ComponentInfo[comp]
	sx, sy := max(int(ci.SubsamplingX), 1), max(int(ci.SubsamplingY), 1)
	cx0, cy0 := ceilDivInt(tx0, sx), ceilDivInt(ty0, sy)
	cx1, cy1 := ceilDivInt(tx1, sx), ceilDivInt(ty1, sy)

	rx0, ry0, rx1, ry1 := tileResCoords(cx0, cy0, cx1, cy1, numRes, res)
	if rx1 <= rx0 || ry1 <= ry0 {
		return 0, 0, 0, 0
	}
	pi, pj := prec%pw, prec/pw

	bx0 := ((rx0 >> uint(ppx)) + pi) << uint(ppx)
	by0 := ((ry0 >> uint(ppy)) + pj) << uint(ppy)
	ax0, ay0 := max(rx0, bx0), max(ry0, by0)
	ax1, ay1 := min(rx1, bx0+(1<<uint(ppx))), min(ry1, by0+(1<<uint(ppy)))
	if ax1 <= ax0 || ay1 <= ay0 {
		return 0, 0, 0, 0
	}

	// Back to full resolution, then onto the image grid. Parenthesised because
	// << and * share a precedence level in Go and the grouping is the whole
	// meaning of the expression.
	n := uint(numRes - 1 - res)
	return (ax0 << n) * sx, (ay0 << n) * sy, (ax1 << n) * sx, (ay1 << n) * sy
}

// Range returns where one packet sits in the codestream, without copying it.
// A caller planning reads over a network wants this rather than GetPacket.
func (idx *PacketIndex) Range(addr PacketAddress) (PacketRange, bool) {
	i, ok := idx.addrMap[addr]
	if !ok {
		return PacketRange{}, false
	}
	e := idx.entries[i]
	if len(e.data) == 0 {
		return PacketRange{}, false
	}
	return PacketRange{Offset: e.offset, Length: len(e.data)}, true
}

// PacketsForRegion returns the packets that cover the image rectangle
// [x0, x1) x [y0, y1) at resolution levels up to and including maxRes, with
// their byte ranges, in codestream order.
//
// This is what a precinct partition is for. Without one a resolution holds a
// single packet spanning the whole image, so every query returns everything
// and the answer is useless; with one the returned ranges are a small subset
// and a viewport can be fetched without reading the rest of the file.
//
// maxRes below zero means every resolution.
func (idx *PacketIndex) PacketsForRegion(x0, y0, x1, y1, maxRes int) []PacketAddress {
	var out []PacketAddress
	for _, e := range idx.entries {
		if maxRes >= 0 && int(e.addr.Resolution) > maxRes {
			continue
		}
		if len(e.data) == 0 || e.x1 <= e.x0 || e.y1 <= e.y0 {
			continue
		}
		if e.x0 >= x1 || e.x1 <= x0 || e.y0 >= y1 || e.y1 <= y0 {
			continue
		}
		out = append(out, e.addr)
	}
	return out
}
