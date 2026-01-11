// Package tcd implements the Tile Coder/Decoder for JPEG 2000.
//
// The TCD orchestrates the encoding and decoding of individual tiles,
// including:
// - Wavelet transform (DWT)
// - Quantization
// - Code-block entropy coding (T1)
// - Packet assembly (T2)
package tcd

import (
	"github.com/aswf/go-jpeg2000/internal/codestream"
	"github.com/aswf/go-jpeg2000/internal/dwt"
	"github.com/aswf/go-jpeg2000/internal/entropy"
)

// Tile represents a single tile in the image.
type Tile struct {
	// Tile index
	Index int

	// Tile bounds in image coordinates
	X0, Y0, X1, Y1 int

	// Components
	Components []*TileComponent
}

// TileComponent represents a single component within a tile.
type TileComponent struct {
	// Component index
	Index int

	// Component bounds (may differ due to subsampling)
	X0, Y0, X1, Y1 int

	// Resolution levels
	Resolutions []*Resolution

	// Coefficient data
	Data []int32

	// Floating point data for 9-7 transform
	DataFloat []float64
}

// Resolution represents a resolution level within a tile-component.
type Resolution struct {
	// Resolution level (0 = finest)
	Level int

	// Bounds at this resolution
	X0, Y0, X1, Y1 int

	// Number of bands (1 for LL, 3 for others)
	NumBands int

	// Bands at this resolution
	Bands []*Band

	// Precincts
	Precincts []*Precinct

	// Precinct grid dimensions
	PrecinctsX, PrecinctsY int
}

// Band represents a subband within a resolution level.
type Band struct {
	// Band type (LL, HL, LH, HH)
	Type int

	// Band bounds
	X0, Y0, X1, Y1 int

	// Quantization step size
	StepSize float64

	// Code-blocks
	CodeBlocks []*CodeBlock

	// Code-block grid dimensions
	CodeBlocksX, CodeBlocksY int
}

// Precinct represents a precinct for packet organization.
type Precinct struct {
	// Precinct index
	Index int

	// Bounds
	X0, Y0, X1, Y1 int

	// Code-blocks in this precinct, per band
	CodeBlocks [][]*CodeBlock

	// Tag trees for inclusion and IMSB
	InclusionTree *TagTree
	IMSBTree      *TagTree
}

// CodeBlock represents a code-block for entropy coding.
type CodeBlock struct {
	// Code-block index
	Index int

	// Bounds
	X0, Y0, X1, Y1 int

	// Encoded data
	Data []byte

	// Coding passes
	Passes []CodingPass

	// Number of zero bit-planes
	ZeroBitPlanes int

	// Total number of bit-planes
	TotalBitPlanes int

	// Included in previous layers
	IncludedInLayers int

	// Decoded coefficient data
	Coefficients []int32
}

// CodingPass represents a single coding pass.
type CodingPass struct {
	// Pass type (significance, refinement, cleanup)
	Type int

	// Length in bytes
	Length int

	// Cumulative length
	CumulativeLength int

	// Rate-distortion slope
	Slope float64

	// Terminated flag
	Terminated bool
}

// Pass type constants.
const (
	PassSignificance = iota
	PassRefinement
	PassCleanup
)

// TagTree implements a tag tree for incremental coding.
type TagTree struct {
	width  int
	height int
	levels int
	nodes  [][]tagNode
}

type tagNode struct {
	value    int
	low      int
	known    bool
}

// NewTagTree creates a new tag tree.
func NewTagTree(width, height int) *TagTree {
	t := &TagTree{
		width:  width,
		height: height,
	}

	// Calculate number of levels
	w, h := width, height
	for w > 1 || h > 1 {
		t.levels++
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	t.levels++

	// Allocate nodes
	t.nodes = make([][]tagNode, t.levels)
	w, h = width, height
	for level := 0; level < t.levels; level++ {
		t.nodes[level] = make([]tagNode, w*h)
		for i := range t.nodes[level] {
			t.nodes[level][i].value = int(^uint(0) >> 1) // MaxInt
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}

	return t
}

// SetValue sets the value at a leaf node.
func (t *TagTree) SetValue(x, y, value int) {
	t.nodes[0][y*t.width+x].value = value
}

// Reset resets the tree for a new encoding/decoding session.
func (t *TagTree) Reset() {
	for level := range t.nodes {
		for i := range t.nodes[level] {
			t.nodes[level][i].low = 0
			t.nodes[level][i].known = false
		}
	}
}

// TileDecoder decodes a single tile.
type TileDecoder struct {
	header     *codestream.Header
	tileHeader *codestream.TilePartHeader
	tile       *Tile
	htj2k      bool // True if using High-Throughput mode
}

// NewTileDecoder creates a new tile decoder.
func NewTileDecoder(header *codestream.Header) *TileDecoder {
	return &TileDecoder{
		header: header,
		htj2k:  header.IsHTJ2K(),
	}
}

// SetHTJ2K sets whether this decoder uses High-Throughput mode.
func (d *TileDecoder) SetHTJ2K(htj2k bool) {
	d.htj2k = htj2k
}

// Tile returns the current tile being decoded.
func (d *TileDecoder) Tile() *Tile {
	return d.tile
}

// InitTile initializes a tile for decoding.
func (d *TileDecoder) InitTile(tileIndex int) {
	h := d.header

	// Calculate tile bounds
	tileX := tileIndex % int(h.NumTilesX)
	tileY := tileIndex / int(h.NumTilesX)

	x0 := max(int(h.TileXOffset)+tileX*int(h.TileWidth), int(h.ImageXOffset))
	y0 := max(int(h.TileYOffset)+tileY*int(h.TileHeight), int(h.ImageYOffset))
	x1 := min(int(h.TileXOffset)+(tileX+1)*int(h.TileWidth), int(h.ImageWidth))
	y1 := min(int(h.TileYOffset)+(tileY+1)*int(h.TileHeight), int(h.ImageHeight))

	d.tile = &Tile{
		Index:      tileIndex,
		X0:         x0,
		Y0:         y0,
		X1:         x1,
		Y1:         y1,
		Components: make([]*TileComponent, h.NumComponents),
	}

	// Initialize components
	for c := 0; c < int(h.NumComponents); c++ {
		comp := h.ComponentInfo[c]

		// Apply subsampling
		cx0 := ceilDiv(x0, int(comp.SubsamplingX))
		cy0 := ceilDiv(y0, int(comp.SubsamplingY))
		cx1 := ceilDiv(x1, int(comp.SubsamplingX))
		cy1 := ceilDiv(y1, int(comp.SubsamplingY))

		tc := &TileComponent{
			Index: c,
			X0:    cx0,
			Y0:    cy0,
			X1:    cx1,
			Y1:    cy1,
		}

		// Allocate data
		width := cx1 - cx0
		height := cy1 - cy0
		tc.Data = make([]int32, width*height)

		// Initialize resolutions
		numRes := int(h.CodingStyle.NumDecompositions) + 1
		tc.Resolutions = make([]*Resolution, numRes)

		for r := 0; r < numRes; r++ {
			d.initResolution(tc, r)
		}

		d.tile.Components[c] = tc
	}
}

// initResolution initializes a resolution level.
func (d *TileDecoder) initResolution(tc *TileComponent, resLevel int) {
	h := d.header.CodingStyle

	// Calculate resolution bounds
	scale := 1 << (int(h.NumDecompositions) - resLevel)
	rx0 := ceilDiv(tc.X0, scale)
	ry0 := ceilDiv(tc.Y0, scale)
	rx1 := ceilDiv(tc.X1, scale)
	ry1 := ceilDiv(tc.Y1, scale)

	res := &Resolution{
		Level: resLevel,
		X0:    rx0,
		Y0:    ry0,
		X1:    rx1,
		Y1:    ry1,
	}

	// Initialize bands
	if resLevel == 0 {
		res.NumBands = 1
		res.Bands = []*Band{d.initBand(res, entropy.BandLL)}
	} else {
		res.NumBands = 3
		res.Bands = []*Band{
			d.initBand(res, entropy.BandHL),
			d.initBand(res, entropy.BandLH),
			d.initBand(res, entropy.BandHH),
		}
	}

	tc.Resolutions[resLevel] = res
}

// initBand initializes a band.
func (d *TileDecoder) initBand(res *Resolution, bandType int) *Band {
	h := d.header.CodingStyle

	band := &Band{
		Type: bandType,
	}

	// Calculate band bounds based on type
	switch bandType {
	case entropy.BandLL:
		band.X0 = res.X0
		band.Y0 = res.Y0
		band.X1 = res.X1
		band.Y1 = res.Y1
	case entropy.BandHL:
		band.X0 = res.X0
		band.Y0 = res.Y0
		band.X1 = res.X1
		band.Y1 = (res.Y0 + res.Y1) / 2
	case entropy.BandLH:
		band.X0 = res.X0
		band.Y0 = res.Y0
		band.X1 = (res.X0 + res.X1) / 2
		band.Y1 = res.Y1
	case entropy.BandHH:
		band.X0 = (res.X0 + res.X1) / 2
		band.Y0 = (res.Y0 + res.Y1) / 2
		band.X1 = res.X1
		band.Y1 = res.Y1
	}

	// Calculate code-block grid
	cbWidth := 1 << (h.CodeBlockWidthExp + 2)
	cbHeight := 1 << (h.CodeBlockHeightExp + 2)

	band.CodeBlocksX = ceilDiv(band.X1-band.X0, cbWidth)
	band.CodeBlocksY = ceilDiv(band.Y1-band.Y0, cbHeight)

	// Initialize code-blocks
	numCB := band.CodeBlocksX * band.CodeBlocksY
	band.CodeBlocks = make([]*CodeBlock, numCB)

	for i := 0; i < numCB; i++ {
		cbX := i % band.CodeBlocksX
		cbY := i / band.CodeBlocksX

		cb := &CodeBlock{
			Index: i,
			X0:    band.X0 + cbX*cbWidth,
			Y0:    band.Y0 + cbY*cbHeight,
			X1:    min(band.X0+(cbX+1)*cbWidth, band.X1),
			Y1:    min(band.Y0+(cbY+1)*cbHeight, band.Y1),
		}
		band.CodeBlocks[i] = cb
	}

	return band
}

// DecodeCodeBlock decodes a single code-block.
func (d *TileDecoder) DecodeCodeBlock(cb *CodeBlock, bandType int) error {
	if len(cb.Data) == 0 {
		return nil
	}

	width := cb.X1 - cb.X0
	height := cb.Y1 - cb.Y0

	if d.htj2k {
		// Use HTJ2K decoder
		htDec := entropy.GetHTDecoder(width, height)
		cb.Coefficients = htDec.Decode(cb.Data, cb.TotalBitPlanes, bandType)
		entropy.PutHTDecoder(htDec)
	} else {
		// Use standard EBCOT decoder
		t1 := entropy.NewT1(width, height)
		cb.Coefficients = t1.Decode(cb.Data, cb.TotalBitPlanes, bandType)
	}

	return nil
}

// ApplyInverseDWT applies the inverse wavelet transform.
func (d *TileDecoder) ApplyInverseDWT(tc *TileComponent) {
	h := d.header.CodingStyle
	numLevels := int(h.NumDecompositions)

	width := tc.X1 - tc.X0
	height := tc.Y1 - tc.Y0

	if h.WaveletTransform == 1 {
		// 5-3 reversible
		dwt.ReconstructMultiLevel53(tc.Data, width, height, numLevels)
	} else {
		// 9-7 irreversible
		tc.DataFloat = make([]float64, len(tc.Data))
		for i, v := range tc.Data {
			tc.DataFloat[i] = float64(v)
		}
		dwt.ReconstructMultiLevel97(tc.DataFloat, width, height, numLevels)
		for i, v := range tc.DataFloat {
			tc.Data[i] = int32(v + 0.5)
		}
	}
}

// TileEncoder encodes a single tile.
type TileEncoder struct {
	header *codestream.Header
	tile   *Tile
	htj2k  bool // True if using High-Throughput mode
}

// NewTileEncoder creates a new tile encoder.
func NewTileEncoder(header *codestream.Header) *TileEncoder {
	return &TileEncoder{
		header: header,
		htj2k:  header.IsHTJ2K(),
	}
}

// SetHTJ2K sets whether this encoder uses High-Throughput mode.
func (e *TileEncoder) SetHTJ2K(htj2k bool) {
	e.htj2k = htj2k
}

// InitTile initializes a tile for encoding.
func (e *TileEncoder) InitTile(tileIndex int, componentData [][]int32) {
	h := e.header

	// Calculate tile bounds (same as decoder)
	tileX := tileIndex % int(h.NumTilesX)
	tileY := tileIndex / int(h.NumTilesX)

	x0 := max(int(h.TileXOffset)+tileX*int(h.TileWidth), int(h.ImageXOffset))
	y0 := max(int(h.TileYOffset)+tileY*int(h.TileHeight), int(h.ImageYOffset))
	x1 := min(int(h.TileXOffset)+(tileX+1)*int(h.TileWidth), int(h.ImageWidth))
	y1 := min(int(h.TileYOffset)+(tileY+1)*int(h.TileHeight), int(h.ImageHeight))

	e.tile = &Tile{
		Index:      tileIndex,
		X0:         x0,
		Y0:         y0,
		X1:         x1,
		Y1:         y1,
		Components: make([]*TileComponent, h.NumComponents),
	}

	// Initialize components with provided data
	for c := 0; c < int(h.NumComponents); c++ {
		comp := h.ComponentInfo[c]

		cx0 := ceilDiv(x0, int(comp.SubsamplingX))
		cy0 := ceilDiv(y0, int(comp.SubsamplingY))
		cx1 := ceilDiv(x1, int(comp.SubsamplingX))
		cy1 := ceilDiv(y1, int(comp.SubsamplingY))

		tc := &TileComponent{
			Index: c,
			X0:    cx0,
			Y0:    cy0,
			X1:    cx1,
			Y1:    cy1,
			Data:  componentData[c],
		}

		// Initialize resolutions (similar to decoder)
		numRes := int(h.CodingStyle.NumDecompositions) + 1
		tc.Resolutions = make([]*Resolution, numRes)

		e.tile.Components[c] = tc
	}
}

// ApplyForwardDWT applies the forward wavelet transform.
func (e *TileEncoder) ApplyForwardDWT(tc *TileComponent) {
	h := e.header.CodingStyle
	numLevels := int(h.NumDecompositions)

	width := tc.X1 - tc.X0
	height := tc.Y1 - tc.Y0

	if h.WaveletTransform == 1 {
		// 5-3 reversible
		dwt.DecomposeMultiLevel53(tc.Data, width, height, numLevels)
	} else {
		// 9-7 irreversible
		tc.DataFloat = make([]float64, len(tc.Data))
		for i, v := range tc.Data {
			tc.DataFloat[i] = float64(v)
		}
		dwt.DecomposeMultiLevel97(tc.DataFloat, width, height, numLevels)
		// Quantize back to integers
		for i, v := range tc.DataFloat {
			if v >= 0 {
				tc.Data[i] = int32(v + 0.5)
			} else {
				tc.Data[i] = int32(v - 0.5)
			}
		}
	}
}

// EncodeCodeBlock encodes a single code-block.
func (e *TileEncoder) EncodeCodeBlock(cb *CodeBlock, data []int32, bandType int) {
	width := cb.X1 - cb.X0
	height := cb.Y1 - cb.Y0

	if e.htj2k {
		// Use HTJ2K encoder
		htEnc := entropy.GetHTEncoder(width, height)
		htEnc.SetData(data)
		cb.Data = htEnc.Encode(bandType)
		entropy.PutHTEncoder(htEnc)
	} else {
		// Use standard EBCOT encoder
		t1 := entropy.NewT1(width, height)
		t1.SetData(data)
		cb.Data = t1.Encode(bandType)
	}
}

// Helper functions

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
