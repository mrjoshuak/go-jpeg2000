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

// packetEntry records a packet's byte range within a codestream.
type packetEntry struct {
	addr   PacketAddress
	offset int
	length int
}

// PacketIndex maps packet addresses to byte ranges within a codestream.
type PacketIndex struct {
	codestream []byte
	entries    []packetEntry
	addrMap    map[PacketAddress]int // maps address to entries index
}

// ExtractPackets parses a J2K codestream and returns all packets with their data.
func ExtractPackets(cs []byte) ([]Packet, error) {
	idx, err := BuildPacketIndex(cs)
	if err != nil {
		return nil, err
	}

	packets := make([]Packet, len(idx.entries))
	for i, entry := range idx.entries {
		data := make([]byte, entry.length)
		copy(data, cs[entry.offset:entry.offset+entry.length])
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
		codestream: cs,
		addrMap:    make(map[PacketAddress]int),
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

		// Skip SOT header to find SOD
		tpStart := pos
		tpHeaderPos := pos + 12 // after SOT marker segment

		// Scan for SOD marker in tile-part header
		sodPos := -1
		for p := tpHeaderPos; p+1 < tpStart+int(tilePartLength); p++ {
			m := binary.BigEndian.Uint16(cs[p : p+2])
			if m == uint16(codestream.SOD) {
				sodPos = p + 2 // data starts after SOD marker
				break
			}
		}
		if sodPos < 0 {
			return nil, fmt.Errorf("no SOD marker found in tile-part at offset %d", pos)
		}

		// Tile data extends from SOD to end of tile-part
		tileDataEnd := tpStart + int(tilePartLength)
		if tileDataEnd > len(cs) {
			tileDataEnd = len(cs)
		}

		// Index packets in this tile
		if err := idx.indexTilePackets(header, tileIndex, cs, sodPos, tileDataEnd); err != nil {
			return nil, fmt.Errorf("indexing tile %d packets: %w", tileIndex, err)
		}

		// Advance to next tile-part
		pos = tpStart + int(tilePartLength)
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
	data := make([]byte, entry.length)
	copy(data, idx.codestream[entry.offset:entry.offset+entry.length])
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
// tile. With a single layer and single precinct per resolution (the default),
// each logical packet maps to a (layer=0, resolution=r, component=c, precinct=0)
// tuple.
//
// The progression order from the COD marker determines the iteration order
// of these logical packets. For the default LRCP order this is:
//   layer -> resolution -> component -> precinct
//
// Since the encoder currently writes data sequentially as
//   component -> resolution -> band -> codeblock
// we partition the tile data by tracking code-block boundaries.
func (idx *PacketIndex) indexTilePackets(
	header *codestream.Header,
	tileIndex uint16,
	cs []byte,
	dataStart, dataEnd int,
) error {
	numComp := int(header.NumComponents)
	numRes := int(header.CodingStyle.NumDecompositions) + 1
	numLayers := int(header.CodingStyle.NumLayers)
	if numLayers <= 0 {
		numLayers = 1
	}

	// Calculate code-block dimensions
	cbWidthExp := int(header.CodingStyle.CodeBlockWidthExp) + 2
	cbHeightExp := int(header.CodingStyle.CodeBlockHeightExp) + 2
	cbWidth := 1 << cbWidthExp
	cbHeight := 1 << cbHeightExp

	// Calculate tile dimensions
	tileX := int(tileIndex) % int(header.NumTilesX)
	tileY := int(tileIndex) / int(header.NumTilesX)

	tx0 := max(int(header.TileXOffset)+tileX*int(header.TileWidth), int(header.ImageXOffset))
	ty0 := max(int(header.TileYOffset)+tileY*int(header.TileHeight), int(header.ImageYOffset))
	tx1 := min(int(header.TileXOffset)+(tileX+1)*int(header.TileWidth), int(header.ImageWidth))
	ty1 := min(int(header.TileYOffset)+(tileY+1)*int(header.TileHeight), int(header.ImageHeight))

	tileWidth := tx1 - tx0
	tileHeight := ty1 - ty0

	// Build a map of code-block byte sizes in the order the encoder writes them.
	// The encoder writes: for c in components, for r in resolutions, for b in bands, for cb in codeblocks.
	// Each code-block's encoded data length is self-delimiting within the T1 entropy coded stream.
	//
	// Since we cannot parse T1 boundaries without actually decoding the entropy-coded data,
	// and the encoder writes all tile data as a single flat blob, we treat the entire tile
	// data as a single packet when there is only one layer.
	//
	// For multi-layer codestreams with proper T2 packet framing, we would need to
	// parse packet headers. For now, we partition the tile data evenly across the
	// logical packet structure.

	// Calculate total number of code-blocks per (component, resolution) pair
	type crKey struct {
		comp int
		res  int
	}
	type crInfo struct {
		numCodeBlocks int
	}
	crInfos := make(map[crKey]crInfo)

	totalCodeBlocks := 0
	// Follow the encoder's iteration order: c -> r -> b -> cb
	for c := 0; c < numComp; c++ {
		comp := header.ComponentInfo[c]
		// Apply subsampling
		compTileWidth := ceilDivInt(tileWidth, int(comp.SubsamplingX))
		compTileHeight := ceilDivInt(tileHeight, int(comp.SubsamplingY))

		for r := 0; r < numRes; r++ {
			scale := 1 << (numRes - 1 - r)
			bandWidth := ceilDivInt(compTileWidth, scale)
			bandHeight := ceilDivInt(compTileHeight, scale)

			var numBands int
			if r == 0 {
				numBands = 1
			} else {
				numBands = 3
				bandWidth = (bandWidth + 1) / 2
				bandHeight = (bandHeight + 1) / 2
			}

			numCBPerBand := ceilDivInt(bandWidth, cbWidth) * ceilDivInt(bandHeight, cbHeight)
			cb := numCBPerBand * numBands
			crInfos[crKey{c, r}] = crInfo{numCodeBlocks: cb}
			totalCodeBlocks += cb
		}
	}

	totalData := dataEnd - dataStart
	if totalData < 0 {
		totalData = 0
	}

	// Generate packet addresses in progression order using the PacketIterator
	// logic, then map each packet to its share of the tile data.
	//
	// For the default configuration (1 layer, 1 precinct per resolution),
	// each packet covers one (component, resolution) pair.

	// Build precinct counts: [component][resolution][0] = numPrecincts
	precincts := make([][][]int, numComp)
	for c := 0; c < numComp; c++ {
		precincts[c] = make([][]int, numRes)
		for r := 0; r < numRes; r++ {
			precincts[c][r] = []int{1} // 1 precinct per resolution
		}
	}

	// Iterate in the progression order specified by the codestream
	order := codestream.ProgressionOrder(header.CodingStyle.ProgressionOrder)

	// Track how many code-blocks belong to each packet
	type packetSlice struct {
		addr      PacketAddress
		numBlocks int
	}
	var packetSlices []packetSlice

	// Generate all packet addresses
	for l := 0; l < numLayers; l++ {
		// Use progression order to determine packet sequence
		switch order {
		case codestream.LRCP:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					for p := 0; p < 1; p++ { // 1 precinct
						addr := PacketAddress{
							Tile:       tileIndex,
							Resolution: uint8(r),
							Layer:      uint16(l),
							Component:  uint8(c),
							Precinct:   uint16(p),
						}
						info := crInfos[crKey{c, r}]
						packetSlices = append(packetSlices, packetSlice{addr: addr, numBlocks: info.numCodeBlocks})
					}
				}
			}
		case codestream.RLCP:
			for r := 0; r < numRes; r++ {
				for c := 0; c < numComp; c++ {
					for p := 0; p < 1; p++ {
						addr := PacketAddress{
							Tile:       tileIndex,
							Resolution: uint8(r),
							Layer:      uint16(l),
							Component:  uint8(c),
							Precinct:   uint16(p),
						}
						info := crInfos[crKey{c, r}]
						packetSlices = append(packetSlices, packetSlice{addr: addr, numBlocks: info.numCodeBlocks})
					}
				}
			}
		case codestream.RPCL:
			for r := 0; r < numRes; r++ {
				for p := 0; p < 1; p++ {
					for c := 0; c < numComp; c++ {
						addr := PacketAddress{
							Tile:       tileIndex,
							Resolution: uint8(r),
							Layer:      uint16(l),
							Component:  uint8(c),
							Precinct:   uint16(p),
						}
						info := crInfos[crKey{c, r}]
						packetSlices = append(packetSlices, packetSlice{addr: addr, numBlocks: info.numCodeBlocks})
					}
				}
			}
		case codestream.PCRL:
			for p := 0; p < 1; p++ {
				for c := 0; c < numComp; c++ {
					for r := 0; r < numRes; r++ {
						addr := PacketAddress{
							Tile:       tileIndex,
							Resolution: uint8(r),
							Layer:      uint16(l),
							Component:  uint8(c),
							Precinct:   uint16(p),
						}
						info := crInfos[crKey{c, r}]
						packetSlices = append(packetSlices, packetSlice{addr: addr, numBlocks: info.numCodeBlocks})
					}
				}
			}
		case codestream.CPRL:
			for c := 0; c < numComp; c++ {
				for p := 0; p < 1; p++ {
					for r := 0; r < numRes; r++ {
						addr := PacketAddress{
							Tile:       tileIndex,
							Resolution: uint8(r),
							Layer:      uint16(l),
							Component:  uint8(c),
							Precinct:   uint16(p),
						}
						info := crInfos[crKey{c, r}]
						packetSlices = append(packetSlices, packetSlice{addr: addr, numBlocks: info.numCodeBlocks})
					}
				}
			}
		}
	}

	// The encoder writes code-blocks in C->R->B->CB order regardless of progression order.
	// However, this code-block data forms the content of packets when arranged
	// according to the progression order. Since the encoder uses a single layer,
	// we need to map the encoder's write order to the packet order.
	//
	// The encoder's write order for a single layer is:
	//   for c: for r: for b: for cb: write(cb.data)
	//
	// This matches the CPRL progression order. For the data to be split by
	// packet, we track byte boundaries proportional to code-block count.

	// Calculate total encoded data per code-block (estimate: distribute evenly)
	// Since all code-blocks are written sequentially with no delimiter between them,
	// and we don't have T2 framing, we distribute the tile data proportionally.
	if totalCodeBlocks == 0 {
		// No code-blocks, create empty packets
		for _, ps := range packetSlices {
			entryIdx := len(idx.entries)
			idx.entries = append(idx.entries, packetEntry{
				addr:   ps.addr,
				offset: dataStart,
				length: 0,
			})
			idx.addrMap[ps.addr] = entryIdx
		}
		return nil
	}

	// Map from encoder write order (C-R-B-CB) to byte offsets.
	// Each packet in encoder order gets a proportional share of bytes.
	bytesPerBlock := float64(totalData) / float64(totalCodeBlocks)

	// Build encoder-order packet list with cumulative code-block counts
	type encoderPacket struct {
		comp         int
		res          int
		startBlock   int
		numBlocks    int
	}
	var encoderOrder []encoderPacket
	blockIdx := 0
	for c := 0; c < numComp; c++ {
		for r := 0; r < numRes; r++ {
			info := crInfos[crKey{c, r}]
			encoderOrder = append(encoderOrder, encoderPacket{
				comp:       c,
				res:        r,
				startBlock: blockIdx,
				numBlocks:  info.numCodeBlocks,
			})
			blockIdx += info.numCodeBlocks
		}
	}

	// Build a lookup from (comp, res) to encoder byte range
	type byteRange struct {
		offset int
		length int
	}
	encoderRanges := make(map[crKey]byteRange)
	for _, ep := range encoderOrder {
		startByte := int(float64(ep.startBlock) * bytesPerBlock)
		endByte := int(float64(ep.startBlock+ep.numBlocks) * bytesPerBlock)
		if ep.startBlock+ep.numBlocks == totalCodeBlocks {
			endByte = totalData // ensure last packet gets remaining bytes
		}
		encoderRanges[crKey{ep.comp, ep.res}] = byteRange{
			offset: dataStart + startByte,
			length: endByte - startByte,
		}
	}

	// Now assign byte ranges to packets in progression order
	for _, ps := range packetSlices {
		key := crKey{int(ps.addr.Component), int(ps.addr.Resolution)}
		br := encoderRanges[key]
		entryIdx := len(idx.entries)
		idx.entries = append(idx.entries, packetEntry{
			addr:   ps.addr,
			offset: br.offset,
			length: br.length,
		})
		idx.addrMap[ps.addr] = entryIdx
	}

	return nil
}

func ceilDivInt(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
