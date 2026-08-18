package jpeg2000

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/mct"
	"github.com/mrjoshuak/go-jpeg2000/internal/tcd"
)

// DecoderOption configures a ProgressiveDecoder.
type DecoderOption func(*ProgressiveDecoder)

// ProgressiveDecoder accepts wavelet packets incrementally and produces
// a continuously improving FloatImage. Packets can arrive in any order;
// each call to Reconstruct produces the best image possible from the
// packets received so far.
//
// The current implementation tracks packet reception and determines the
// appropriate resolution reduction level. Code-block entropy decoding
// will be added when the T2 packet encode pipeline produces proper
// per-packet framing with code-block boundaries.
type ProgressiveDecoder struct {
	header        *codestream.Header
	receivedPkts  map[PacketAddress][]byte
	totalExpected int

	// sampleLimit bounds the samples Reconstruct will allocate. When the
	// decoder was built from a codestream it is derived from that
	// codestream's length, so a corrupt SIZ cannot outgrow its input.
	sampleLimit int
}

// NewProgressiveDecoderFromCodestream creates a progressive decoder from a raw
// J2K codestream. It parses the main header and initializes the decoder.
func NewProgressiveDecoderFromCodestream(cs []byte) (*ProgressiveDecoder, error) {
	header, _, err := parseMainHeader(cs)
	if err != nil {
		return nil, fmt.Errorf("parsing codestream header: %w", err)
	}
	pd, err := NewProgressiveDecoder(header)
	if err != nil {
		return nil, err
	}
	pd.sampleLimit = sampleLimitForInput(len(cs))
	return pd, nil
}

// NewProgressiveDecoder creates a progressive decoder from a parsed
// codestream header. The header must contain SIZ, COD, and QCD markers.
func NewProgressiveDecoder(header *codestream.Header, opts ...DecoderOption) (*ProgressiveDecoder, error) {
	if header == nil {
		return nil, fmt.Errorf("header is nil")
	}
	if err := header.Validate(); err != nil {
		return nil, fmt.Errorf("invalid header: %w", err)
	}

	numComp := int(header.NumComponents)
	numRes := header.CodingStyle.NumResolutions()
	numLayers := header.CodingStyle.Layers()
	numTiles := int(uint64(header.NumTilesX) * uint64(header.NumTilesY))

	// Computed in 64-bit: all four factors come from the file and their
	// product overflows a 32-bit int for perfectly legal-looking values.
	total64 := uint64(numTiles) * uint64(numRes) * uint64(numComp) * uint64(numLayers)
	if total64 > maxPackets {
		return nil, fmt.Errorf("codestream describes %d packets, above the %d limit",
			total64, uint64(maxPackets))
	}

	pd := &ProgressiveDecoder{
		header:        header,
		receivedPkts:  make(map[PacketAddress][]byte),
		totalExpected: int(total64),
		sampleLimit:   tcd.DefaultSampleLimit,
	}

	for _, opt := range opts {
		opt(pd)
	}

	return pd, nil
}

// FeedPacket accepts a single packet. Packets can arrive in any order.
func (d *ProgressiveDecoder) FeedPacket(p Packet) error {
	data := make([]byte, len(p.Data))
	copy(data, p.Data)
	d.receivedPkts[p.Address] = data
	return nil
}

// ReceivedPackets returns addresses of all received packets.
func (d *ProgressiveDecoder) ReceivedPackets() []PacketAddress {
	addrs := make([]PacketAddress, 0, len(d.receivedPkts))
	for addr := range d.receivedPkts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		a, b := addrs[i], addrs[j]
		if a.Tile != b.Tile {
			return a.Tile < b.Tile
		}
		if a.Resolution != b.Resolution {
			return a.Resolution < b.Resolution
		}
		if a.Layer != b.Layer {
			return a.Layer < b.Layer
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Precinct < b.Precinct
	})
	return addrs
}

// Complete returns true when all expected packets have been received.
func (d *ProgressiveDecoder) Complete() bool {
	return len(d.receivedPkts) >= d.totalExpected
}

// Reconstruct produces the best image possible from packets received so far.
// If no packets have been received, it returns a zero-valued FloatImage.
//
// The reconstruction follows the same pipeline as the standard decoder:
// initialize tile structures, apply inverse DWT, inverse MCT, and DC level
// shift. Resolution reduction is determined by which packets have been received.
func (d *ProgressiveDecoder) Reconstruct() (*FloatImage, error) {
	h := d.header
	numComp := int(h.NumComponents)

	if numComp == 0 || len(h.ComponentInfo) == 0 {
		return nil, fmt.Errorf("invalid image: no components")
	}

	if h.ImageXOffset >= h.ImageWidth || h.ImageYOffset >= h.ImageHeight {
		return nil, fmt.Errorf("jpeg2000: SIZ image offset %dx%d is outside image %dx%d",
			h.ImageXOffset, h.ImageYOffset, h.ImageWidth, h.ImageHeight)
	}

	reduce := d.bestReduction()

	width := reducedDimension(int(h.ImageWidth-h.ImageXOffset), reduce)
	height := reducedDimension(int(h.ImageHeight-h.ImageYOffset), reduce)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("jpeg2000: image reduces to %dx%d at reduction level %d",
			width, height, reduce)
	}
	limit := d.sampleLimit
	if limit <= 0 {
		limit = tcd.DefaultSampleLimit
	}
	if height > limit/width || numComp > limit/(width*height) {
		return nil, fmt.Errorf("jpeg2000: %d component planes of %dx%d exceed the %d sample limit",
			numComp, width, height, limit)
	}

	precision := h.ComponentInfo[0].Precision()
	signed := h.ComponentInfo[0].IsSigned()

	componentData := make([][]int32, numComp)
	for c := 0; c < numComp; c++ {
		componentData[c] = make([]int32, width*height)
	}

	if len(d.receivedPkts) == 0 {
		return d.buildFloatImage(componentData, width, height, numComp, precision, signed)
	}

	numTiles := int(uint64(h.NumTilesX) * uint64(h.NumTilesY))
	if h.NumTilesX == 0 || h.NumTilesY == 0 || uint64(h.NumTilesX)*uint64(h.NumTilesY) > codestream.MaxTiles {
		return nil, fmt.Errorf("jpeg2000: tile grid %dx%d is outside the 1..%d tile range",
			h.NumTilesX, h.NumTilesY, codestream.MaxTiles)
	}
	tileDec := tcd.NewTileDecoder(h)
	tileDec.SetReduceResolution(reduce)
	tileDec.SetSampleLimit(limit)

	for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
		if !d.hasTileData(uint16(tileIdx)) {
			continue
		}

		if err := tileDec.InitTile(tileIdx); err != nil {
			return nil, fmt.Errorf("jpeg2000: tile %d: %w", tileIdx, err)
		}
		tile := tileDec.Tile()
		if tile == nil {
			continue
		}

		// Decode received packets into tile component data
		numRes := int(h.CodingStyle.NumDecompositions) + 1
		bestRes := numRes - 1 - reduce
		for addr, pktData := range d.receivedPkts {
			if addr.Tile != uint16(tileIdx) {
				continue
			}
			if int(addr.Resolution) > bestRes {
				continue
			}
			comp := int(addr.Component)
			res := int(addr.Resolution)
			if comp < len(tile.Components) && tile.Components[comp] != nil {
				tc := tile.Components[comp]
				tcWidth := tc.X1 - tc.X0
				tcHeight := tc.Y1 - tc.Y0
				decodePacketIntoTile(tc.Data, tcWidth, tcHeight, numRes, comp, res, pktData, h.CodingStyle.CodeBlockWidth(), h.CodingStyle.CodeBlockHeight())
			}
		}

		imgXOff := reducedDimension(int(h.ImageXOffset), reduce)
		imgYOff := reducedDimension(int(h.ImageYOffset), reduce)

		for c := 0; c < len(tile.Components) && c < numComp; c++ {
			tc := tile.Components[c]
			if tc == nil {
				continue
			}

			tileDec.ApplyInverseDWT(tc)

			for y := tc.Y0; y < tc.Y1 && y-imgYOff < height; y++ {
				for x := tc.X0; x < tc.X1 && x-imgXOff < width; x++ {
					srcIdx := (y-tc.Y0)*(tc.X1-tc.X0) + (x - tc.X0)
					dstX := x - imgXOff
					dstY := y - imgYOff
					if dstX >= 0 && dstY >= 0 && dstX < width && dstY < height {
						dstIdx := dstY*width + dstX
						if srcIdx < len(tc.Data) {
							componentData[c][dstIdx] = tc.Data[srcIdx]
						}
					}
				}
			}
		}
	}

	// Apply inverse MCT
	if h.CodingStyle.MultipleComponentXf != 0 && numComp >= 3 {
		if h.CodingStyle.IsReversible() {
			mct.InverseRCT(componentData[0], componentData[1], componentData[2])
		} else {
			compFloat := make([][]float64, 3)
			for c := 0; c < 3; c++ {
				compFloat[c] = make([]float64, len(componentData[c]))
				for i, v := range componentData[c] {
					compFloat[c][i] = float64(v)
				}
			}
			mct.InverseICT(compFloat[0], compFloat[1], compFloat[2])
			for c := 0; c < 3; c++ {
				for i, v := range compFloat[c] {
					componentData[c][i] = int32(v + 0.5)
				}
			}
		}
	}

	// Apply DC level shift
	for c := 0; c < numComp; c++ {
		if !h.ComponentInfo[c].IsSigned() {
			mct.DCLevelShiftInverse(componentData[c], h.ComponentInfo[c].Precision())
		}
	}

	return d.buildFloatImage(componentData, width, height, numComp, precision, signed)
}

// bestReduction returns the number of resolution levels to skip based on
// which resolutions have complete data across all components and tiles.
func (d *ProgressiveDecoder) bestReduction() int {
	h := d.header
	numComp := int(h.NumComponents)
	numRes := int(h.CodingStyle.NumDecompositions) + 1
	numTiles := int(h.NumTilesX * h.NumTilesY)

	bestRes := 0
	for r := 0; r < numRes; r++ {
		allHave := true
		for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
			if !d.hasTileData(uint16(tileIdx)) {
				continue
			}
			for c := 0; c < numComp; c++ {
				addr := PacketAddress{
					Tile:       uint16(tileIdx),
					Resolution: uint8(r),
					Layer:      0,
					Component:  uint8(c),
					Precinct:   0,
				}
				if _, ok := d.receivedPkts[addr]; !ok {
					allHave = false
					break
				}
			}
			if !allHave {
				break
			}
		}
		if allHave {
			bestRes = r
		} else {
			break
		}
	}

	reduce := numRes - 1 - bestRes
	if reduce < 0 {
		reduce = 0
	}
	return reduce
}

// hasTileData returns true if any packet for the given tile has been received.
func (d *ProgressiveDecoder) hasTileData(tile uint16) bool {
	for addr := range d.receivedPkts {
		if addr.Tile == tile {
			return true
		}
	}
	return false
}

// decodePacketIntoTile decodes a single (component, resolution) group's
// mini-table into tile component data. The mini-table has the same format
// as the full tile data: [2: numCB][per CB: 1 numBPS + 4 dataLen][encoded bytes].
func decodePacketIntoTile(
	tileData []int32,
	tcWidth, tcHeight int,
	numRes, comp, res int,
	pktData []byte,
	cbWidth, cbHeight int,
) {
	if len(pktData) < 2 {
		return
	}
	// Every dimension below divides or bounds a loop; a zero would spin
	// forever and a negative would index out of range.
	if tcWidth <= 0 || tcHeight <= 0 || cbWidth <= 0 || cbHeight <= 0 {
		return
	}
	if numRes < 1 || res < 0 || res >= numRes || tcWidth*tcHeight > len(tileData) {
		return
	}

	numCB := int(binary.BigEndian.Uint16(pktData[0:2]))
	metaSize := 2 + numCB*5
	if len(pktData) < metaSize {
		return
	}

	type decodeMeta struct {
		numBPS  int
		dataLen int
	}
	metas := make([]decodeMeta, numCB)
	for i := 0; i < numCB; i++ {
		off := 2 + i*5
		metas[i].numBPS = clampBitPlanes(int(pktData[off]))
		metas[i].dataLen = int(binary.BigEndian.Uint32(pktData[off+1 : off+5]))
	}

	// Iterate bands and code-blocks for this single resolution
	numBands := 1
	if res > 0 {
		numBands = 3
	}

	cbIdx := 0
	dataPos := metaSize

	for b := 0; b < numBands; b++ {
		bandType := entropy.BandLL
		if res > 0 {
			switch b {
			case 0:
				bandType = entropy.BandHL
			case 1:
				bandType = entropy.BandLH
			case 2:
				bandType = entropy.BandHH
			}
		}

		// Compute band dimensions
		scale := 1 << (numRes - 1 - res)
		bandW := (tcWidth + scale - 1) / scale
		bandH := (tcHeight + scale - 1) / scale
		if res > 0 {
			bandW = (bandW + 1) / 2
			bandH = (bandH + 1) / 2
		}

		xOff, yOff := computeSubbandOffset(tcWidth, tcHeight, numRes, res, bandType)

		for cby := 0; cby*cbHeight < bandH; cby++ {
			for cbx := 0; cbx*cbWidth < bandW; cbx++ {
				if cbIdx >= numCB {
					return
				}
				meta := metas[cbIdx]

				startX := cbx * cbWidth
				startY := cby * cbHeight
				actualW := cbWidth
				actualH := cbHeight
				if startX+actualW > bandW {
					actualW = bandW - startX
				}
				if startY+actualH > bandH {
					actualH = bandH - startY
				}

				if actualW > 0 && actualH > 0 && meta.numBPS > 0 && meta.dataLen > 0 &&
					dataPos >= 0 && meta.dataLen <= len(pktData)-dataPos {
					cbData := pktData[dataPos : dataPos+meta.dataLen]
					t1 := entropy.NewT1(actualW, actualH)
					decoded := t1.Decode(cbData, meta.numBPS, bandType)

					for y := 0; y < actualH; y++ {
						for x := 0; x < actualW; x++ {
							dstX := xOff + startX + x
							dstY := yOff + startY + y
							if dstX >= 0 && dstY >= 0 && dstX < tcWidth && dstY < tcHeight {
								tileData[dstY*tcWidth+dstX] = decoded[y*actualW+x]
							}
						}
					}
				}

				dataPos += meta.dataLen
				cbIdx++
			}
		}
	}
}

// buildFloatImage converts int32 component data to a FloatImage.
func (d *ProgressiveDecoder) buildFloatImage(
	componentData [][]int32,
	width, height, numComp, precision int,
	signed bool,
) (*FloatImage, error) {
	components := make([][]float32, numComp)
	for c := 0; c < numComp; c++ {
		components[c] = make([]float32, width*height)
		for i, v := range componentData[c] {
			components[c][i] = float32(v)
		}
	}

	return &FloatImage{
		Width:      width,
		Height:     height,
		Components: components,
		BitDepth:   precision,
		Signed:     signed,
	}, nil
}
