package jpeg2000

import (
	"fmt"
	"sort"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
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
	numRes := int(header.CodingStyle.NumDecompositions) + 1
	numLayers := int(header.CodingStyle.NumLayers)
	if numLayers <= 0 {
		numLayers = 1
	}
	numTiles := int(header.NumTilesX * header.NumTilesY)

	total := numTiles * numRes * numComp * numLayers

	pd := &ProgressiveDecoder{
		header:        header,
		receivedPkts:  make(map[PacketAddress][]byte),
		totalExpected: total,
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

	reduce := d.bestReduction()

	width := reducedDimension(int(h.ImageWidth-h.ImageXOffset), reduce)
	height := reducedDimension(int(h.ImageHeight-h.ImageYOffset), reduce)

	precision := h.ComponentInfo[0].Precision()
	signed := h.ComponentInfo[0].IsSigned()

	componentData := make([][]int32, numComp)
	for c := 0; c < numComp; c++ {
		componentData[c] = make([]int32, width*height)
	}

	if len(d.receivedPkts) == 0 {
		return d.buildFloatImage(componentData, width, height, numComp, precision, signed)
	}

	numTiles := int(h.NumTilesX * h.NumTilesY)
	tileDec := tcd.NewTileDecoder(h)
	tileDec.SetReduceResolution(reduce)

	for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
		if !d.hasTileData(uint16(tileIdx)) {
			continue
		}

		tileDec.InitTile(tileIdx)
		tile := tileDec.Tile()
		if tile == nil {
			continue
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
