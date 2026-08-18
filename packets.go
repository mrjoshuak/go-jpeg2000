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
// The encoder writes code-block data in a flat C->R->B->CB order within each
// tile, preceded by a metadata table: [2: numCB][per CB: 1 numBPS + 4 dataLen].
// This function parses that table to determine exact per-code-block sizes,
// then groups code-blocks by (component, resolution) to build self-contained
// per-packet mini-tables that can be independently decoded.
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
	if n := uint64(numComp) * uint64(numRes) * uint64(numLayers); n > maxPackets {
		return fmt.Errorf("tile describes %d packets, above the %d limit", n, uint64(maxPackets))
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

	tileWidth := tx1 - tx0
	tileHeight := ty1 - ty0

	tileData := cs[dataStart:dataEnd]

	// Compute expected number of code-blocks (same logic as decodeTileData)
	type crKey struct {
		comp int
		res  int
	}
	type crCodeBlockInfo struct {
		numCodeBlocks int
	}
	var crOrder []crKey // encoder write order: C→R
	crInfos := make(map[crKey]crCodeBlockInfo)
	expectedCB := 0

	for c := 0; c < numComp; c++ {
		comp := header.ComponentInfo[c]
		compTileWidth := ceilDivInt(tileWidth, int(comp.SubsamplingX))
		compTileHeight := ceilDivInt(tileHeight, int(comp.SubsamplingY))

		for r := 0; r < numRes; r++ {
			scale := 1 << (numRes - 1 - r)
			bandW := ceilDivInt(compTileWidth, scale)
			bandH := ceilDivInt(compTileHeight, scale)

			numBands := 1
			if r > 0 {
				numBands = 3
				bandW = (bandW + 1) / 2
				bandH = (bandH + 1) / 2
			}

			numCBPerBand := ceilDivInt(bandW, cbWidth) * ceilDivInt(bandH, cbHeight)
			cb := numCBPerBand * numBands
			key := crKey{c, r}
			crOrder = append(crOrder, key)
			crInfos[key] = crCodeBlockInfo{numCodeBlocks: cb}
			expectedCB += cb
		}
	}

	// Parse metadata table
	if len(tileData) < 2 {
		return idx.addEmptyPackets(header, tileIndex, numComp, numRes, numLayers)
	}

	numCB := int(binary.BigEndian.Uint16(tileData[0:2]))
	if numCB != expectedCB {
		// Not our format — fall back to empty packets
		return idx.addEmptyPackets(header, tileIndex, numComp, numRes, numLayers)
	}

	metaSize := 2 + numCB*5
	if len(tileData) < metaSize {
		return idx.addEmptyPackets(header, tileIndex, numComp, numRes, numLayers)
	}

	// Read per-code-block metadata
	type cbMeta struct {
		numBPS  uint8
		dataLen uint32
	}
	metas := make([]cbMeta, numCB)
	for i := 0; i < numCB; i++ {
		off := 2 + i*5
		metas[i].numBPS = tileData[off]
		metas[i].dataLen = binary.BigEndian.Uint32(tileData[off+1 : off+5])
	}

	// Build self-contained mini-tables per (C,R) group.
	// Each mini-table has the same format as the full tile data:
	// [2: groupNumCB][per CB: 1 numBPS + 4 dataLen][encoded bytes for these CBs]
	crData := make(map[crKey][]byte)
	cbIdx := 0
	dataPos := metaSize

	for _, key := range crOrder {
		info := crInfos[key]
		groupNum := info.numCodeBlocks
		// groupNum is derived from tile geometry; only its sum is known to
		// equal numCB, so an individual group is bounded explicitly before it
		// sizes the two slices below.
		if groupNum < 0 || groupNum > numCB {
			return fmt.Errorf("tile %d: code-block group of %d is outside the %d code-blocks the tile declares",
				tileIndex, groupNum, numCB)
		}

		// Collect metadata and encoded bytes for this group
		groupMetaSize := 2 + groupNum*5
		var groupEncoded []byte
		groupMetas := make([]cbMeta, groupNum)

		for i := 0; i < groupNum; i++ {
			if cbIdx >= numCB {
				break
			}
			m := metas[cbIdx]
			groupMetas[i] = m
			if int(m.dataLen) > 0 && dataPos+int(m.dataLen) <= len(tileData) {
				groupEncoded = append(groupEncoded, tileData[dataPos:dataPos+int(m.dataLen)]...)
			}
			dataPos += int(m.dataLen)
			cbIdx++
		}

		// Build mini-table
		miniTable := make([]byte, groupMetaSize+len(groupEncoded))
		miniTable[0] = byte(groupNum >> 8)
		miniTable[1] = byte(groupNum)
		for i, m := range groupMetas {
			off := 2 + i*5
			miniTable[off] = m.numBPS
			miniTable[off+1] = byte(m.dataLen >> 24)
			miniTable[off+2] = byte(m.dataLen >> 16)
			miniTable[off+3] = byte(m.dataLen >> 8)
			miniTable[off+4] = byte(m.dataLen)
		}
		copy(miniTable[groupMetaSize:], groupEncoded)
		crData[key] = miniTable
	}

	// Generate packet addresses in progression order and assign data
	order := codestream.ProgressionOrder(header.CodingStyle.ProgressionOrder)

	addPacket := func(l, r, c, p int) {
		addr := PacketAddress{
			Tile:       tileIndex,
			Resolution: uint8(r),
			Layer:      uint16(l),
			Component:  uint8(c),
			Precinct:   uint16(p),
		}
		data := crData[crKey{c, r}]
		entryIdx := len(idx.entries)
		idx.entries = append(idx.entries, packetEntry{addr: addr, data: data})
		idx.addrMap[addr] = entryIdx
	}

	for l := 0; l < numLayers; l++ {
		switch order {
		case codestream.LRCP:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.RLCP:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.RPCL:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.PCRL:
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.CPRL:
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					addPacket(l, r, c, 0)
				}
			}
		}
	}

	return nil
}

// addEmptyPackets adds empty packet entries for all expected packets.
func (idx *PacketIndex) addEmptyPackets(
	header *codestream.Header,
	tileIndex uint16,
	numComp, numRes, numLayers int,
) error {
	order := codestream.ProgressionOrder(header.CodingStyle.ProgressionOrder)

	addPacket := func(l, r, c, p int) {
		addr := PacketAddress{
			Tile:       tileIndex,
			Resolution: uint8(r),
			Layer:      uint16(l),
			Component:  uint8(c),
			Precinct:   uint16(p),
		}
		entryIdx := len(idx.entries)
		idx.entries = append(idx.entries, packetEntry{addr: addr})
		idx.addrMap[addr] = entryIdx
	}

	for l := 0; l < numLayers; l++ {
		switch order {
		case codestream.LRCP, codestream.RLCP, codestream.RPCL:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.PCRL:
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					addPacket(l, r, c, 0)
				}
			}
		case codestream.CPRL:
			for c := 0; c < numComp; c++ {
				for r := 0; r < numRes; r++ {
					addPacket(l, r, c, 0)
				}
			}
		}
	}
	return nil
}

func ceilDivInt(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
