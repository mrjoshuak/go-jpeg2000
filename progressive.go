package jpeg2000

import (
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
// Each packet is decoded on its own: a conforming packet header names the
// code-blocks that contribute and the bytes each contributes, so a packet
// carries everything needed to place its coefficients. The resolutions that
// have arrived fix the reduction level the reconstruction is produced at.
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

	// The header alone cannot say how many packets are plausible; the byte
	// count can. Applied here rather than in NewProgressiveDecoder, which is
	// handed a header with no input to measure against.
	if limit := maxPacketsForInput(len(cs)); uint64(pd.totalExpected) > limit {
		return nil, fmt.Errorf("codestream describes %d packets, more than the %d a %d-byte codestream can carry",
			pd.totalExpected, limit, len(cs))
	}
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
				decodePacketIntoTile(tc.Data, tcWidth, tcHeight,
					tc.FullX0, tc.FullY0, tc.FullX1, tc.FullY1,
					numRes, res, pktData,
					h.CodingStyle.CodeBlockWidth(), h.CodingStyle.CodeBlockHeight(),
					func(band int) int { return h.BandMb(res, band) }, h.IsHTJ2K())
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

// decodePacketIntoTile decodes one conforming packet into tile component data.
//
// The packet is self-contained: its header names the contributing code-blocks,
// their zero bit-planes and the byte count each contributes, so it can be
// decoded on its own with no reference to the packets around it. That holds for
// a single quality layer, which is what this decoder is fed; a later layer of a
// multi-layer codestream continues state its predecessors established.
//
// x0, y0, x1, y1 are the tile component's absolute coordinates, which is what
// the subband geometry is derived from; tcWidth and tcHeight bound the array
// being written into. mb reports the magnitude bit-planes the codestream
// declares for a band of this resolution, which is what the zero-bit-plane
// count in the header is measured against.
func decodePacketIntoTile(
	tileData []int32,
	tcWidth, tcHeight int,
	x0, y0, x1, y1 int,
	numRes, res int,
	pktData []byte,
	cbWidth, cbHeight int,
	mb func(band int) int,
	ht bool,
) {
	if len(pktData) == 0 {
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

	bands := bandGridFor(x0, y0, x1, y1, numRes, res, cbWidth, cbHeight)
	if bands == nil {
		return
	}
	r := newPktReader(pktData)
	if err := readPacket(r, bands, 0, true); err != nil {
		return
	}

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
				numbps := mb(b) - cb.zeroPlanes
				if numbps < 1 {
					continue
				}
				var coeffs []int32
				if ht {
					dec := entropy.GetHTDecoder(w, hh)
					coeffs = append([]int32(nil), dec.Decode(cb.data, numbps, bg.bandType)...)
					entropy.PutHTDecoder(dec)
				} else {
					t1 := entropy.NewT1(w, hh)
					coeffs = t1.Decode(cb.data, numbps, bg.bandType)
				}
				for yy := 0; yy < hh; yy++ {
					for xx := 0; xx < w; xx++ {
						dx := sb.ox + bx0 - sb.x0 + xx
						dy := sb.oy + by0 - sb.y0 + yy
						if dx < 0 || dy < 0 || dx >= tcWidth || dy >= tcHeight {
							continue
						}
						tileData[dy*tcWidth+dx] = coeffs[yy*w+xx]
					}
				}
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
