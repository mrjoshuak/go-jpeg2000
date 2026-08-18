package codestream

import (
	"fmt"
)

// Header represents the main header of a JPEG 2000 codestream.
type Header struct {
	// SIZ marker data
	Profile       uint16
	ImageWidth    uint32
	ImageHeight   uint32
	ImageXOffset  uint32
	ImageYOffset  uint32
	TileWidth     uint32
	TileHeight    uint32
	TileXOffset   uint32
	TileYOffset   uint32
	NumComponents uint16
	ComponentInfo []ComponentInfo

	// Derived values
	NumTilesX uint32
	NumTilesY uint32

	// COD marker data (default coding style)
	CodingStyle CodingStyleDefault

	// QCD marker data (default quantization)
	Quantization QuantizationDefault

	// Optional per-component coding styles (COC markers)
	ComponentCodingStyles map[uint16]CodingStyleComponent

	// Optional per-component quantization (QCC markers)
	ComponentQuantization map[uint16]QuantizationComponent

	// CAP marker data (extended capabilities)
	Capabilities *CapabilitiesMarker

	// NLT markers (non-linearity point transform)
	NLTMarkers []NLTMarker

	// Optional markers
	ProgressionOrderChanges []ProgressionOrderChange
	TileLengths             []TileLength
	PacketLengths           []uint32
	PackedPacketHeaders     []byte
	Comment                 string
	CommentType             uint16
}

// ComponentInfo holds per-component size information from the SIZ marker.
type ComponentInfo struct {
	// Bit depth of the component (Ssiz).
	// If bit 7 is set, the component is signed.
	BitDepth uint8

	// Horizontal subsampling factor (XRsiz).
	SubsamplingX uint8

	// Vertical subsampling factor (YRsiz).
	SubsamplingY uint8
}

// Precision returns the bit precision (1-38).
func (c ComponentInfo) Precision() int {
	return int(c.BitDepth&0x7F) + 1
}

// IsSigned returns true if the component values are signed.
func (c ComponentInfo) IsSigned() bool {
	return c.BitDepth&0x80 != 0
}

// CodingStyleDefault holds data from the COD marker.
type CodingStyleDefault struct {
	// Scod: Coding style flags
	CodingStyle uint8

	// SGcod: Style for progressions
	ProgressionOrder    uint8
	NumLayers           uint16
	MultipleComponentXf uint8

	// SPcod: Coding parameters
	NumDecompositions  uint8
	CodeBlockWidthExp  uint8
	CodeBlockHeightExp uint8
	CodeBlockStyle     uint8
	WaveletTransform   uint8

	// Precinct sizes (if CodingStylePrecincts is set)
	PrecinctSizes []PrecinctSize
}

// codeBlockDim converts a code-block exponent (xcb-2 / ycb-2) to a dimension.
// The exponent is clamped to the range the standard allows so that the result
// is always in [4, 1024]. An unclamped shift would silently produce 0 for a
// file-supplied exponent of 62 or more (Go defines an over-wide shift as 0),
// and every caller divides by this value.
func codeBlockDim(exp uint8) int {
	if exp > MaxCodeBlockExp {
		exp = MaxCodeBlockExp
	}
	return 1 << (exp + 2)
}

// CodeBlockWidth returns the code block width.
func (c CodingStyleDefault) CodeBlockWidth() int {
	return codeBlockDim(c.CodeBlockWidthExp)
}

// CodeBlockHeight returns the code block height.
func (c CodingStyleDefault) CodeBlockHeight() int {
	return codeBlockDim(c.CodeBlockHeightExp)
}

// Layers returns the number of quality layers, treating an unset (zero) SGcod
// layer count as a single layer. Callers must never use the raw NumLayers as a
// loop bound or an array size without this normalisation.
func (c CodingStyleDefault) Layers() int {
	if c.NumLayers == 0 {
		return 1
	}
	return int(c.NumLayers)
}

// CodeBlockWidth returns the code block width for a per-component style.
func (c CodingStyleComponent) CodeBlockWidth() int {
	return codeBlockDim(c.CodeBlockWidthExp)
}

// CodeBlockHeight returns the code block height for a per-component style.
func (c CodingStyleComponent) CodeBlockHeight() int {
	return codeBlockDim(c.CodeBlockHeightExp)
}

// NumResolutions returns the number of resolution levels.
func (c CodingStyleDefault) NumResolutions() int {
	return int(c.NumDecompositions) + 1
}

// IsReversible returns true if the 5-3 reversible wavelet is used.
func (c CodingStyleDefault) IsReversible() bool {
	return c.WaveletTransform == 1
}

// PrecinctSize holds the precinct dimensions for a resolution level.
type PrecinctSize struct {
	WidthExp  uint8 // PPx: width exponent
	HeightExp uint8 // PPy: height exponent
}

// Width returns the precinct width.
func (p PrecinctSize) Width() int {
	return 1 << p.WidthExp
}

// Height returns the precinct height.
func (p PrecinctSize) Height() int {
	return 1 << p.HeightExp
}

// CodingStyleComponent holds data from a COC marker.
type CodingStyleComponent struct {
	ComponentIndex     uint16
	CodingStyle        uint8
	NumDecompositions  uint8
	CodeBlockWidthExp  uint8
	CodeBlockHeightExp uint8
	CodeBlockStyle     uint8
	WaveletTransform   uint8
	PrecinctSizes      []PrecinctSize
}

// QuantizationDefault holds data from the QCD marker.
type QuantizationDefault struct {
	// Sqcd: Quantization style and guard bits
	QuantizationStyle uint8
	// NumGuardBits holds the raw Sqcd byte; the guard-bit count lives in its
	// top three bits and is returned by GuardBits. The parser used to store
	// the already-shifted count here, so GuardBits shifted a second time and
	// reported zero guard bits for every codestream, which in turn made the
	// per-band Mb (and so the decoded bit-plane count) too small.
	NumGuardBits uint8

	// SPqcd: Step sizes
	// For no quantization: only exponents
	// For scalar: mantissa and exponent pairs
	StepSizes []StepSize
}

// Style returns the quantization style (0, 1, or 2).
func (q QuantizationDefault) Style() uint8 {
	return q.QuantizationStyle & 0x1F
}

// GuardBits returns the number of guard bits.
func (q QuantizationDefault) GuardBits() int {
	return int(q.NumGuardBits >> 5)
}

// StepSize represents a quantization step size.
type StepSize struct {
	Mantissa uint16 // 11-bit mantissa
	Exponent uint8  // 5-bit exponent
}

// Value returns the step size as a float64. The exponent is a five-bit field
// in the codestream, so it can never exceed 31; the guard keeps a directly
// constructed StepSize from turning the shift into an undefined-width one.
func (s StepSize) Value() float64 {
	exp := s.Exponent
	if exp > 31 {
		exp = 31
	}
	return float64(1+float64(s.Mantissa)/2048.0) * float64(uint64(1)<<(31-exp))
}

// QuantizationComponent holds data from a QCC marker.
type QuantizationComponent struct {
	ComponentIndex    uint16
	QuantizationStyle uint8
	NumGuardBits      uint8
	StepSizes         []StepSize
}

// ProgressionOrderChange holds data from a POC marker.
type ProgressionOrderChange struct {
	ResolutionStart  uint8
	ComponentStart   uint16
	LayerEnd         uint16
	ResolutionEnd    uint8
	ComponentEnd     uint16
	ProgressionOrder uint8
}

// TileLength holds tile-part length information from TLM marker.
type TileLength struct {
	TileIndex uint16
	Length    uint32
}

// CapabilitiesMarker holds data from the CAP marker (extended capabilities).
// This marker is used to signal HTJ2K (Part 15) and other extended features.
type CapabilitiesMarker struct {
	// Pcap is a 32-bit field indicating which extended capabilities are used.
	// Bit 15 (0x00020000) indicates HTJ2K (Part 15) is used when set.
	Pcap uint32

	// CCAPi contains extended component capabilities.
	// Each pair of bytes provides additional information for components.
	CCAPi []uint16
}

// CapPcapHTJ2K is the bit in Pcap indicating HTJ2K (Part 15) is used.
// When this bit is set, the codestream uses the High-Throughput block coder.
//
// Pcap bits are numbered from the most significant bit: bit i corresponds to
// Part i+1 of the standard, so bit i has value 1<<(32-i). Part 15 is therefore
// bit 15 with value 1<<17 = 0x00020000. This constant previously held
// 0x00008000, which is 1<<15 and denotes bit 17 — two positions off. Streams
// written with the old value are not recognised as HTJ2K by any conforming
// decoder, and this library did not recognise conforming streams as HTJ2K.
// Verified against a codestream produced by OpenJPH, which emits 0x00020000.
const CapPcapHTJ2K uint32 = 0x00020000 // Bit 15, i.e. 1<<(32-15)

// CapCcapHTDefault is the Ccap^15 value for a codestream in which every
// code-block uses the HT block coder with default parameters. Part 15 requires
// one 16-bit Ccap field to follow Pcap for each capability bit set in Pcap; a
// CAP marker carrying Pcap alone is malformed. Matches OpenJPH's output.
const CapCcapHTDefault uint16 = 0x0022

// IsHTJ2K returns true if the CAP marker indicates HTJ2K mode.
func (c *CapabilitiesMarker) IsHTJ2K() bool {
	if c == nil {
		return false
	}
	return c.Pcap&CapPcapHTJ2K != 0
}

// NLTMarker holds data from an NLT (Non-Linearity point Transform) marker.
// NLT Type 3 is used for IEEE 754 float data encoded as int32 bit patterns,
// applying a sign-magnitude to two's complement transform.
type NLTMarker struct {
	ComponentIndex uint16
	BitDepth       uint8 // BDnlt: bit 7 = signed, bits 0-6 = depth-1
	TransformType  uint8 // Tnlt: 3 = DC level shift (type used for float)
}

// HasNLT returns true if the given component has an NLT marker.
func (h *Header) HasNLT(component int) bool {
	for _, nlt := range h.NLTMarkers {
		if int(nlt.ComponentIndex) == component {
			return true
		}
	}
	return false
}

// NLTPrecision returns the sample precision declared by the NLT marker for
// the given component, and whether that component has an NLT marker at all.
// The point transform is defined over samples of this width, which is not
// necessarily the same as the component precision in SIZ.
func (h *Header) NLTPrecision(component int) (int, bool) {
	for _, nlt := range h.NLTMarkers {
		if int(nlt.ComponentIndex) == component {
			return int(nlt.BitDepth&0x7F) + 1, true
		}
	}
	return 0, false
}

// TilePartHeader represents a tile-part header.
type TilePartHeader struct {
	TileIndex      uint16
	TilePartLength uint32
	TilePartIndex  uint8
	NumTileParts   uint8

	// Optional tile-specific coding parameters
	CodingStyle             *CodingStyleDefault
	ComponentCodingStyles   map[uint16]CodingStyleComponent
	Quantization            *QuantizationDefault
	ComponentQuantization   map[uint16]QuantizationComponent
	ProgressionOrderChanges []ProgressionOrderChange
	PackedPacketHeaders     []byte
}

// IsHTJ2K returns true if this header indicates HTJ2K (High-Throughput) mode.
// HTJ2K is detected via the CAP marker or the CodeBlockHT flag in COD/COC.
func (h *Header) IsHTJ2K() bool {
	// Check CAP marker
	if h.Capabilities != nil && h.Capabilities.IsHTJ2K() {
		return true
	}
	// Check CodeBlockHT flag in default coding style
	if h.CodingStyle.CodeBlockStyle&CodeBlockHT != 0 {
		return true
	}
	// Check per-component coding styles
	for _, coc := range h.ComponentCodingStyles {
		if coc.CodeBlockStyle&CodeBlockHT != 0 {
			return true
		}
	}
	return false
}

// Validate checks the header for consistency.
//
// Every field checked here is read verbatim from the file and later used to
// size an allocation, index a slice, or divide, so a header that does not pass
// this function must never reach the tile decoder. The checks are structural
// only: limits that depend on how many bytes the input actually contains are
// applied separately by the decoder, so that metadata-only reads of a
// truncated file still work.
func (h *Header) Validate() error {
	if err := h.validateSIZ(); err != nil {
		return err
	}
	if err := h.validateCodingStyles(); err != nil {
		return err
	}
	if err := h.validateQuantization(); err != nil {
		return err
	}
	return h.validateAuxMarkers()
}

// validateSIZ checks the image grid: dimensions, origins, the tile grid and
// the per-component sample grid.
func (h *Header) validateSIZ() error {
	if h.ImageWidth == 0 || h.ImageHeight == 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", h.ImageWidth, h.ImageHeight)
	}
	if h.ImageWidth > MaxDimension || h.ImageHeight > MaxDimension {
		return fmt.Errorf("SIZ: image dimensions %dx%d exceed the %d limit",
			h.ImageWidth, h.ImageHeight, uint32(MaxDimension))
	}

	// Xsiz > XOsiz and Ysiz > YOsiz (ISO/IEC 15444-1 A.5.1). Without this the
	// width computation Xsiz-XOsiz wraps around and yields a multi-gigabyte
	// allocation for a tiny file.
	if h.ImageXOffset >= h.ImageWidth {
		return fmt.Errorf("SIZ: image X offset %d must be less than image width %d",
			h.ImageXOffset, h.ImageWidth)
	}
	if h.ImageYOffset >= h.ImageHeight {
		return fmt.Errorf("SIZ: image Y offset %d must be less than image height %d",
			h.ImageYOffset, h.ImageHeight)
	}

	if h.TileWidth == 0 || h.TileHeight == 0 {
		return fmt.Errorf("invalid tile dimensions: %dx%d", h.TileWidth, h.TileHeight)
	}
	if h.TileWidth > MaxDimension || h.TileHeight > MaxDimension {
		return fmt.Errorf("SIZ: tile dimensions %dx%d exceed the %d limit",
			h.TileWidth, h.TileHeight, uint32(MaxDimension))
	}

	// XTOsiz <= XOsiz and XTOsiz + XTsiz > XOsiz (A.5.1). A tile origin past
	// the image origin makes tile bounds run backwards, which then sizes a
	// negative-length coefficient slice.
	if h.TileXOffset > h.ImageXOffset {
		return fmt.Errorf("SIZ: tile X offset %d must not exceed image X offset %d",
			h.TileXOffset, h.ImageXOffset)
	}
	if h.TileYOffset > h.ImageYOffset {
		return fmt.Errorf("SIZ: tile Y offset %d must not exceed image Y offset %d",
			h.TileYOffset, h.ImageYOffset)
	}
	if uint64(h.TileXOffset)+uint64(h.TileWidth) <= uint64(h.ImageXOffset) {
		return fmt.Errorf("SIZ: tile X offset %d plus tile width %d must exceed image X offset %d",
			h.TileXOffset, h.TileWidth, h.ImageXOffset)
	}
	if uint64(h.TileYOffset)+uint64(h.TileHeight) <= uint64(h.ImageYOffset) {
		return fmt.Errorf("SIZ: tile Y offset %d plus tile height %d must exceed image Y offset %d",
			h.TileYOffset, h.TileHeight, h.ImageYOffset)
	}

	// The tile grid must be describable: Isot is a 16-bit field, so an image
	// can hold at most 65535 tiles.
	tilesX := ceilDivU32(h.ImageWidth-h.TileXOffset, h.TileWidth)
	tilesY := ceilDivU32(h.ImageHeight-h.TileYOffset, h.TileHeight)
	if tilesX == 0 || tilesY == 0 {
		return fmt.Errorf("SIZ: degenerate tile grid %dx%d", tilesX, tilesY)
	}
	if tilesX > MaxTiles || tilesY > MaxTiles || tilesX*tilesY > MaxTiles {
		return fmt.Errorf("SIZ: tile grid %dx%d exceeds the %d tile limit",
			tilesX, tilesY, MaxTiles)
	}

	if h.NumComponents == 0 || h.NumComponents > MaxComponents {
		return fmt.Errorf("invalid number of components: %d", h.NumComponents)
	}
	if len(h.ComponentInfo) != int(h.NumComponents) {
		return fmt.Errorf("component info mismatch: expected %d, got %d",
			h.NumComponents, len(h.ComponentInfo))
	}

	for i, comp := range h.ComponentInfo {
		if comp.SubsamplingX == 0 || comp.SubsamplingY == 0 {
			return fmt.Errorf("component %d: invalid subsampling: %dx%d",
				i, comp.SubsamplingX, comp.SubsamplingY)
		}
		prec := comp.Precision()
		if prec < 1 || prec > MaxPrecision {
			return fmt.Errorf("component %d: invalid precision: %d", i, prec)
		}
		// The component sample grid must be non-empty, otherwise the tile
		// component bounds collapse or invert.
		if ceilDivU32(h.ImageWidth, uint32(comp.SubsamplingX)) <= ceilDivU32(h.ImageXOffset, uint32(comp.SubsamplingX)) {
			return fmt.Errorf("component %d: subsampling %d leaves an empty sample grid in X",
				i, comp.SubsamplingX)
		}
		if ceilDivU32(h.ImageHeight, uint32(comp.SubsamplingY)) <= ceilDivU32(h.ImageYOffset, uint32(comp.SubsamplingY)) {
			return fmt.Errorf("component %d: subsampling %d leaves an empty sample grid in Y",
				i, comp.SubsamplingY)
		}
	}

	return nil
}

// validateCodingStyles checks the COD marker and every COC override.
func (h *Header) validateCodingStyles() error {
	c := h.CodingStyle
	if err := validateCodingParams("COD", c.CodingStyle, c.NumDecompositions,
		c.CodeBlockWidthExp, c.CodeBlockHeightExp, c.PrecinctSizes); err != nil {
		return err
	}
	if c.ProgressionOrder > MaxProgressionOrder {
		return fmt.Errorf("COD: progression order %d is not defined (max %d)",
			c.ProgressionOrder, MaxProgressionOrder)
	}

	for idx, coc := range h.ComponentCodingStyles {
		if int(idx) >= int(h.NumComponents) {
			return fmt.Errorf("COC: component index %d is out of range (%d components)",
				idx, h.NumComponents)
		}
		if err := validateCodingParams(fmt.Sprintf("COC component %d", idx),
			coc.CodingStyle, coc.NumDecompositions,
			coc.CodeBlockWidthExp, coc.CodeBlockHeightExp, coc.PrecinctSizes); err != nil {
			return err
		}
	}
	return nil
}

// validateCodingParams checks the SPcod/SPcoc parameters shared by COD and COC.
func validateCodingParams(what string, style, numDecomp, cbwExp, cbhExp uint8, precincts []PrecinctSize) error {
	if numDecomp > MaxDecompositionLevels {
		return fmt.Errorf("%s: %d decomposition levels exceeds the %d limit",
			what, numDecomp, MaxDecompositionLevels)
	}
	if cbwExp > MaxCodeBlockExp {
		return fmt.Errorf("%s: code-block width exponent %d exceeds the %d limit",
			what, cbwExp, MaxCodeBlockExp)
	}
	if cbhExp > MaxCodeBlockExp {
		return fmt.Errorf("%s: code-block height exponent %d exceeds the %d limit",
			what, cbhExp, MaxCodeBlockExp)
	}
	if int(cbwExp)+int(cbhExp) > MaxCodeBlockExpSum {
		return fmt.Errorf("%s: code-block %dx%d exceeds the %d sample area limit",
			what, codeBlockDim(cbwExp), codeBlockDim(cbhExp), MaxCodeBlockArea)
	}
	if style&CodingStylePrecincts != 0 {
		if len(precincts) > MaxDecompositionLevels+1 {
			return fmt.Errorf("%s: %d precinct sizes for at most %d resolutions",
				what, len(precincts), MaxDecompositionLevels+1)
		}
		for i, p := range precincts {
			if p.WidthExp > MaxPrecinctExp || p.HeightExp > MaxPrecinctExp {
				return fmt.Errorf("%s: precinct %d exponent %d/%d exceeds the %d limit",
					what, i, p.WidthExp, p.HeightExp, MaxPrecinctExp)
			}
			// PPx=0 / PPy=0 is only legal for resolution level 0.
			if i > 0 && (p.WidthExp == 0 || p.HeightExp == 0) {
				return fmt.Errorf("%s: precinct %d has a zero exponent, which is only legal at resolution 0",
					what, i)
			}
		}
	}
	return nil
}

// validateQuantization checks the QCD marker and every QCC override.
func (h *Header) validateQuantization() error {
	if s := h.Quantization.Style(); s > MaxQuantizationStyle {
		return fmt.Errorf("QCD: quantization style %d is not defined (max %d)",
			s, MaxQuantizationStyle)
	}
	for idx, qcc := range h.ComponentQuantization {
		if int(idx) >= int(h.NumComponents) {
			return fmt.Errorf("QCC: component index %d is out of range (%d components)",
				idx, h.NumComponents)
		}
		if s := qcc.QuantizationStyle & 0x1F; s > MaxQuantizationStyle {
			return fmt.Errorf("QCC component %d: quantization style %d is not defined (max %d)",
				idx, s, MaxQuantizationStyle)
		}
	}
	return nil
}

// validateAuxMarkers checks markers that carry component indices or
// resolution indices which are later used to select a component.
func (h *Header) validateAuxMarkers() error {
	for i, nlt := range h.NLTMarkers {
		if int(nlt.ComponentIndex) >= int(h.NumComponents) {
			return fmt.Errorf("NLT %d: component index %d is out of range (%d components)",
				i, nlt.ComponentIndex, h.NumComponents)
		}
		if p := int(nlt.BitDepth&0x7F) + 1; p > MaxPrecision {
			return fmt.Errorf("NLT %d: sample precision %d exceeds the %d limit",
				i, p, MaxPrecision)
		}
	}
	numRes := h.CodingStyle.NumResolutions()
	for i, poc := range h.ProgressionOrderChanges {
		if poc.ProgressionOrder > MaxProgressionOrder {
			return fmt.Errorf("POC %d: progression order %d is not defined (max %d)",
				i, poc.ProgressionOrder, MaxProgressionOrder)
		}
		if int(poc.ComponentStart) > int(h.NumComponents) || int(poc.ComponentEnd) > int(h.NumComponents) {
			return fmt.Errorf("POC %d: component range %d..%d is out of range (%d components)",
				i, poc.ComponentStart, poc.ComponentEnd, h.NumComponents)
		}
		if int(poc.ResolutionStart) > numRes || int(poc.ResolutionEnd) > numRes {
			return fmt.Errorf("POC %d: resolution range %d..%d is out of range (%d resolutions)",
				i, poc.ResolutionStart, poc.ResolutionEnd, numRes)
		}
	}
	return nil
}

// CalculateDerivedValues computes values derived from the main header.
//
// The tile counts are computed in 64-bit arithmetic and saturate rather than
// wrap: Xsiz-XTOsiz underflows to roughly four billion when a corrupt file
// puts the tile origin outside the image, and the result is used directly as a
// loop bound. Validate rejects such a header, but the derived values are
// computed first and must be harmless on their own.
func (h *Header) CalculateDerivedValues() {
	h.NumTilesX = saturateTileCount(ceilDivU32(subFloor(h.ImageWidth, h.TileXOffset), h.TileWidth))
	h.NumTilesY = saturateTileCount(ceilDivU32(subFloor(h.ImageHeight, h.TileYOffset), h.TileHeight))
}

// subFloor returns a-b, or 0 if b > a.
func subFloor(a, b uint32) uint32 {
	if b > a {
		return 0
	}
	return a - b
}

// saturateTileCount narrows a 64-bit tile count to uint32 without wrapping.
func saturateTileCount(n uint64) uint32 {
	if n > 1<<32-1 {
		return 1<<32 - 1
	}
	return uint32(n)
}

// BandMb returns Mb for a subband: the number of bit-planes the band's
// coefficients may occupy, which is the quantisation exponent plus the guard
// bits less one.
//
// A code-block's coded bit-plane count is Mb + 1 - (its zero bit-planes), the
// value the packet header carries in the IMSB tag tree. The block decoder needs
// it to place the magnitude bits, so reading it wrong yields coefficients that
// are scaled by a power of two rather than obviously broken.
func (h *Header) BandMb(res, band int) int {
	q := h.Quantization
	// Subbands are ordered LL, then (HL, LH, HH) per resolution level.
	idx := 0
	if res > 0 {
		idx = 1 + (res-1)*3 + band
	}
	exp := 0
	if idx < len(q.StepSizes) {
		exp = int(q.StepSizes[idx].Exponent)
	} else if len(q.StepSizes) > 0 {
		exp = int(q.StepSizes[len(q.StepSizes)-1].Exponent)
	}
	mb := q.GuardBits() + exp - 1
	if mb < 1 {
		mb = 1
	}
	return mb
}
