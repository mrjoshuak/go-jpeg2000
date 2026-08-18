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
	"fmt"
	"math"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
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

	// Component bounds (may differ due to subsampling). With a resolution
	// reduction in force these are the bounds of the reduced grid, which is
	// what Data is sized for.
	X0, Y0, X1, Y1 int

	// Component bounds at full resolution, before any reduction. Every
	// subband coordinate in the codestream is derived from these
	// (ISO/IEC 15444-1 B.5), so a decoder that has discarded resolutions
	// still has to partition with them.
	FullX0, FullY0, FullX1, FullY1 int

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
	value int
	low   int
	known bool
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

// DefaultSampleLimit is the ceiling on the number of coefficient samples a
// single tile-component may occupy when the caller has not set an explicit
// limit. Callers that know how many bytes the input actually contains should
// call SetSampleLimit with a bound derived from that length.
const DefaultSampleLimit = 1 << 28 // 256M samples = 1 GiB of int32

// TileDecoder decodes a single tile.
type TileDecoder struct {
	header            *codestream.Header
	tile              *Tile
	htj2k             bool // True if using High-Throughput mode
	qualityLayerLimit int  // 0 means all layers
	reduceResolution  int  // number of finest resolution levels to skip
	sampleLimit       int  // ceiling on samples allocated for one tile
}

// NewTileDecoder creates a new tile decoder.
func NewTileDecoder(header *codestream.Header) *TileDecoder {
	return &TileDecoder{
		header:      header,
		htj2k:       header.IsHTJ2K(),
		sampleLimit: DefaultSampleLimit,
	}
}

// SetSampleLimit bounds the total number of coefficient samples InitTile will
// allocate for one tile across all components. A non-positive limit restores
// the default. This is the guard that keeps a corrupt SIZ marker in a small
// file from driving a multi-gigabyte allocation.
func (d *TileDecoder) SetSampleLimit(n int) {
	if n <= 0 {
		n = DefaultSampleLimit
	}
	d.sampleLimit = n
}

// SampleLimit returns the current per-tile sample allocation limit.
func (d *TileDecoder) SampleLimit() int {
	if d.sampleLimit <= 0 {
		return DefaultSampleLimit
	}
	return d.sampleLimit
}

// SetQualityLayerLimit sets the maximum number of quality layers to decode.
// 0 means decode all layers.
func (d *TileDecoder) SetQualityLayerLimit(limit int) {
	d.qualityLayerLimit = limit
}

// QualityLayerLimit returns the current quality layer limit.
func (d *TileDecoder) QualityLayerLimit() int {
	return d.qualityLayerLimit
}

// SetReduceResolution sets the number of finest resolution levels to skip.
// 0 means full resolution. N means skip the N finest levels.
func (d *TileDecoder) SetReduceResolution(n int) {
	d.reduceResolution = n
}

// ReduceResolution returns the current resolution reduction level.
func (d *TileDecoder) ReduceResolution() int {
	return d.reduceResolution
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
//
// Every quantity used here comes from the file: the tile grid, the tile and
// image origins, the per-component subsampling factors and the number of
// wavelet decomposition levels. Each is checked before it is used to divide,
// to shift, or to size the coefficient slice, and the total allocation is held
// under the decoder's sample limit. A header that cannot describe a decodable
// tile produces an error instead of a panic, a negative-length make, or an
// allocation the input could not justify.
func (d *TileDecoder) InitTile(tileIndex int) error {
	h := d.header

	numTilesX := int(h.NumTilesX)
	numTilesY := int(h.NumTilesY)
	if numTilesX <= 0 || numTilesY <= 0 {
		return fmt.Errorf("tcd: tile grid is %dx%d, must be at least 1x1", h.NumTilesX, h.NumTilesY)
	}
	if numTilesX > codestream.MaxTiles || numTilesY > codestream.MaxTiles ||
		numTilesX > codestream.MaxTiles/numTilesY {
		return fmt.Errorf("tcd: tile grid %dx%d exceeds the %d tile limit",
			numTilesX, numTilesY, codestream.MaxTiles)
	}
	if tileIndex < 0 || tileIndex >= numTilesX*numTilesY {
		return fmt.Errorf("tcd: tile index %d is out of range (%d tiles)",
			tileIndex, numTilesX*numTilesY)
	}
	if len(h.ComponentInfo) != int(h.NumComponents) {
		return fmt.Errorf("tcd: SIZ declares %d components but carries %d component records",
			h.NumComponents, len(h.ComponentInfo))
	}
	if h.NumComponents == 0 {
		return fmt.Errorf("tcd: SIZ declares no components")
	}

	numDecomp := int(h.CodingStyle.NumDecompositions)
	if numDecomp > codestream.MaxDecompositionLevels {
		return fmt.Errorf("tcd: COD declares %d decomposition levels, above the %d limit",
			numDecomp, codestream.MaxDecompositionLevels)
	}

	// Calculate tile bounds
	tileX := tileIndex % numTilesX
	tileY := tileIndex / numTilesX

	x0 := max(int(h.TileXOffset)+tileX*int(h.TileWidth), int(h.ImageXOffset))
	y0 := max(int(h.TileYOffset)+tileY*int(h.TileHeight), int(h.ImageYOffset))
	x1 := min(int(h.TileXOffset)+(tileX+1)*int(h.TileWidth), int(h.ImageWidth))
	y1 := min(int(h.TileYOffset)+(tileY+1)*int(h.TileHeight), int(h.ImageHeight))
	if x1 <= x0 || y1 <= y0 {
		return fmt.Errorf("tcd: tile %d has empty bounds [%d,%d)x[%d,%d)",
			tileIndex, x0, x1, y0, y1)
	}

	// Clamp reduceResolution to valid range
	reduce := d.reduceResolution
	if reduce < 0 {
		reduce = 0
	}
	if reduce > numDecomp {
		reduce = numDecomp
	}

	// The tile is built into a local and only published on success, so that a
	// caller which ignores the error can never observe a half-initialised tile
	// with nil components.
	d.tile = nil
	tile := &Tile{
		Index:      tileIndex,
		X0:         x0,
		Y0:         y0,
		X1:         x1,
		Y1:         y1,
		Components: make([]*TileComponent, h.NumComponents),
	}

	limit := d.SampleLimit()
	total := 0

	// Initialize components
	for c := 0; c < int(h.NumComponents); c++ {
		comp := h.ComponentInfo[c]
		if comp.SubsamplingX == 0 || comp.SubsamplingY == 0 {
			return fmt.Errorf("tcd: component %d has zero subsampling %dx%d",
				c, comp.SubsamplingX, comp.SubsamplingY)
		}

		// Apply subsampling
		cx0 := ceilDiv(x0, int(comp.SubsamplingX))
		cy0 := ceilDiv(y0, int(comp.SubsamplingY))
		cx1 := ceilDiv(x1, int(comp.SubsamplingX))
		cy1 := ceilDiv(y1, int(comp.SubsamplingY))
		fx0, fy0, fx1, fy1 := cx0, cy0, cx1, cy1

		// Apply resolution reduction to component bounds
		for i := 0; i < reduce; i++ {
			cx0 = ceilDiv(cx0, 2)
			cy0 = ceilDiv(cy0, 2)
			cx1 = ceilDiv(cx1, 2)
			cy1 = ceilDiv(cy1, 2)
		}

		tc := &TileComponent{
			Index:  c,
			X0:     cx0,
			Y0:     cy0,
			X1:     cx1,
			Y1:     cy1,
			FullX0: fx0,
			FullY0: fy0,
			FullX1: fx1,
			FullY1: fy1,
		}

		// Allocate data
		width := cx1 - cx0
		height := cy1 - cy0
		if width <= 0 || height <= 0 {
			return fmt.Errorf("tcd: tile %d component %d has empty bounds %dx%d",
				tileIndex, c, width, height)
		}
		if height > limit/width {
			return fmt.Errorf("tcd: tile %d component %d needs %dx%d samples, above the %d sample limit for this input",
				tileIndex, c, width, height, limit)
		}
		samples := width * height
		if total > limit-samples {
			return fmt.Errorf("tcd: tile %d needs more than the %d sample limit for this input",
				tileIndex, limit)
		}
		total += samples
		tc.Data = make([]int32, samples)

		// Initialize only the resolutions we need (skip finest N levels)
		numRes := numDecomp + 1 - reduce
		tc.Resolutions = make([]*Resolution, numRes)

		for r := 0; r < numRes; r++ {
			if err := d.initResolutionReduced(tc, r, reduce); err != nil {
				return fmt.Errorf("tcd: tile %d component %d resolution %d: %w", tileIndex, c, r, err)
			}
		}

		tile.Components[c] = tc
	}

	d.tile = tile
	return nil
}

// initResolutionReduced initializes a resolution level accounting for reduction.
// resLevel is the index in the reduced resolution set, reduce is the number of
// skipped finest levels. The effective decomposition level used for scale
// computation is (numDecomp - reduce - resLevel).
func (d *TileDecoder) initResolutionReduced(tc *TileComponent, resLevel int, reduce int) error {
	h := d.header.CodingStyle

	// With reduction, numDecomp levels exist but we only use (numDecomp - reduce).
	// resLevel 0 is the coarsest in the reduced set.
	// The scale factor for this level relative to the reduced component bounds:
	numDecompReduced := int(h.NumDecompositions) - reduce
	shift := numDecompReduced - resLevel
	// A shift of 64 or more evaluates to zero in Go, and every use of scale
	// below is a division, so an out-of-range decomposition count would divide
	// by zero rather than merely produce a wrong scale.
	if shift < 0 || shift > codestream.MaxDecompositionLevels {
		return fmt.Errorf("resolution scale shift %d is out of range (0..%d)",
			shift, codestream.MaxDecompositionLevels)
	}
	scale := 1 << shift
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
	return nil
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

	// Calculate code-block grid. CodeBlockWidth/Height clamp the file-supplied
	// exponent into the range the standard allows, so they are never zero and
	// the divisions below cannot fault.
	cbWidth := h.CodeBlockWidth()
	cbHeight := h.CodeBlockHeight()

	band.CodeBlocksX = ceilDiv(max(band.X1-band.X0, 0), cbWidth)
	band.CodeBlocksY = ceilDiv(max(band.Y1-band.Y0, 0), cbHeight)

	// Initialize code-blocks
	numCB := band.CodeBlocksX * band.CodeBlocksY
	if numCB <= 0 {
		band.CodeBlocksX, band.CodeBlocksY = 0, 0
		return band
	}
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
	if width <= 0 || height <= 0 {
		return fmt.Errorf("tcd: code-block %d has empty bounds %dx%d", cb.Index, width, height)
	}
	if width > codestream.MaxCodeBlockArea || height > codestream.MaxCodeBlockArea ||
		width*height > codestream.MaxCodeBlockArea {
		return fmt.Errorf("tcd: code-block %d is %dx%d, above the %d sample limit",
			cb.Index, width, height, codestream.MaxCodeBlockArea)
	}
	// Coefficients are int32, so bit-plane 31 is the last one that can carry a
	// magnitude bit. A larger count only multiplies decode time.
	numBitPlanes := cb.TotalBitPlanes
	if numBitPlanes < 0 {
		numBitPlanes = 0
	}
	if numBitPlanes > codestream.MaxBitPlanes {
		numBitPlanes = codestream.MaxBitPlanes
	}

	if d.htj2k {
		// Use HTJ2K decoder
		htDec := entropy.GetHTDecoder(width, height)
		cb.Coefficients = htDec.Decode(cb.Data, numBitPlanes, bandType)
		entropy.PutHTDecoder(htDec)
	} else {
		// Use standard EBCOT decoder
		t1 := entropy.NewT1(width, height)
		cb.Coefficients = t1.Decode(cb.Data, numBitPlanes, bandType)
	}

	return nil
}

// ApplyInverseDWT applies the inverse wavelet transform.
func (d *TileDecoder) ApplyInverseDWT(tc *TileComponent) {
	h := d.header.CodingStyle
	numLevels := int(h.NumDecompositions) - d.reduceResolution
	if numLevels < 0 {
		numLevels = 0
	}
	// The decomposition count comes from COD; a file may declare up to 255 of
	// them, and each level allocates a dimension record.
	if numLevels > codestream.MaxDecompositionLevels {
		numLevels = codestream.MaxDecompositionLevels
	}

	width := tc.X1 - tc.X0
	height := tc.Y1 - tc.Y0

	if width <= 0 || height <= 0 || width*height > len(tc.Data) {
		return
	}
	// Zero decomposition levels still leaves the LL band quantized, so the
	// irreversible path has work to do even when the transform does not. Only
	// the reversible path can return here.
	if numLevels == 0 && h.WaveletTransform == 1 {
		return
	}

	// The synthesis runs at the tile-component's own origin: which
	// coefficients are lowpass depends on whether the coordinate they sit at
	// is even, and only a tile at the image origin can assume they all are.
	// At the origin the two forms agree sample for sample, which
	// TestTileMatchesOriginZero in the dwt package checks.
	if h.WaveletTransform == 1 {
		// 5-3 reversible. The tiled form carries 64-bit intermediates
		// throughout, so it covers what ReconstructMultiLevel53_32bit exists
		// for: full-range int32 samples, as the float pipeline produces.
		if tc.X0 == 0 && tc.Y0 == 0 {
			if d.needs32BitDWT() {
				dwt.ReconstructMultiLevel53_32bit(tc.Data, width, height, numLevels)
			} else {
				dwt.ReconstructMultiLevel53(tc.Data, width, height, numLevels)
			}
		} else {
			dwt.ReconstructMultiLevel53Tile(tc.Data, width, height, tc.X0, tc.Y0, numLevels)
		}
	} else {
		// 9-7 irreversible. What the block coder carries is not a coefficient
		// but a quantization index, so it has to be scaled by the subband's
		// step size before the transform can undo anything. This step used to
		// be missing entirely: the indices went straight into the inverse
		// transform, which is only correct when every step size is one.
		tc.DataFloat = make([]float64, len(tc.Data))
		// Dequantise first — the irreversible path carries explicit step
		// sizes — then reconstruct with the tile-aware transform, since a tile
		// away from the image origin has a different subband parity.
		d.dequantize(tc, width, height, numLevels)
		if tc.X0 == 0 && tc.Y0 == 0 {
			dwt.ReconstructMultiLevel97(tc.DataFloat, width, height, numLevels)
		} else {
			dwt.ReconstructMultiLevel97Tile(tc.DataFloat, width, height, tc.X0, tc.Y0, numLevels)
		}
		for i, v := range tc.DataFloat {
			tc.Data[i] = int32(math.Floor(v + 0.5))
		}
	}
}

// dequantize expands the quantization indices in tc.Data into coefficients in
// tc.DataFloat, using the step sizes from QCD.
//
// ISO/IEC 15444-1 E.1.1 reconstructs the centre of the quantization bin: a
// nonzero index q becomes sign(q)·(|q| + 1/2)·Δ_b, and a zero index becomes
// zero. OpenJPH does the same, by construction — its block decoder sets bit 0
// of every decoded magnitude, which is the half, and scales by Δ_b/2^(31−K_max)
// to cancel the shift the block coder applied.
func (d *TileDecoder) dequantize(tc *TileComponent, width, height, numLevels int) {
	h := d.header
	prec := h.MaxPrecision()
	steps := h.Quantization.StepSizes
	codestream.ForEachSubband(width, height, numLevels+1, func(sb codestream.SubbandRect) {
		delta := 1.0
		if sb.Index < len(steps) {
			delta = steps[sb.Index].Delta(prec + codestream.BandGainLog2(sb.Res, sb.Detail))
		}
		for y := 0; y < sb.H; y++ {
			row := (sb.Y0 + y) * width
			for x := 0; x < sb.W; x++ {
				i := row + sb.X0 + x
				if i < 0 || i >= len(tc.Data) {
					continue
				}
				q := tc.Data[i]
				switch {
				case q > 0:
					tc.DataFloat[i] = (float64(q) + 0.5) * delta
				case q < 0:
					tc.DataFloat[i] = -(float64(-q) + 0.5) * delta
				default:
					tc.DataFloat[i] = 0
				}
			}
		}
	})
}

// needs32BitDWT returns true if 32-bit-safe DWT arithmetic is required.
func (d *TileDecoder) needs32BitDWT() bool {
	for _, ci := range d.header.ComponentInfo {
		if ci.Precision() > 16 {
			return true
		}
	}
	return false
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
			Index:  c,
			X0:     cx0,
			Y0:     cy0,
			X1:     cx1,
			Y1:     cy1,
			FullX0: cx0,
			FullY0: cy0,
			FullX1: cx1,
			FullY1: cy1,
			Data:   componentData[c],
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

// ceilDiv returns ceil(a/b). A non-positive divisor can only come from a
// header value that was not validated; returning 0 keeps the caller from
// faulting, and the caller's own bounds checks reject the resulting geometry.
func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
