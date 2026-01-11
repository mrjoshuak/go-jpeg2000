package codestream

import (
	"fmt"
)

// Header represents the main header of a JPEG 2000 codestream.
type Header struct {
	// SIZ marker data
	Profile            uint16
	ImageWidth         uint32
	ImageHeight        uint32
	ImageXOffset       uint32
	ImageYOffset       uint32
	TileWidth          uint32
	TileHeight         uint32
	TileXOffset        uint32
	TileYOffset        uint32
	NumComponents      uint16
	ComponentInfo      []ComponentInfo

	// Derived values
	NumTilesX uint32
	NumTilesY uint32

	// COD marker data (default coding style)
	CodingStyle        CodingStyleDefault

	// QCD marker data (default quantization)
	Quantization       QuantizationDefault

	// Optional per-component coding styles (COC markers)
	ComponentCodingStyles map[uint16]CodingStyleComponent

	// Optional per-component quantization (QCC markers)
	ComponentQuantization map[uint16]QuantizationComponent

	// CAP marker data (extended capabilities)
	Capabilities *CapabilitiesMarker

	// Optional markers
	ProgressionOrderChanges []ProgressionOrderChange
	TileLengths            []TileLength
	PacketLengths          []uint32
	PackedPacketHeaders    []byte
	Comment                string
	CommentType            uint16
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

// CodeBlockWidth returns the code block width.
func (c CodingStyleDefault) CodeBlockWidth() int {
	return 1 << (c.CodeBlockWidthExp + 2)
}

// CodeBlockHeight returns the code block height.
func (c CodingStyleDefault) CodeBlockHeight() int {
	return 1 << (c.CodeBlockHeightExp + 2)
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
	NumGuardBits      uint8

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

// Value returns the step size as a float64.
func (s StepSize) Value() float64 {
	return float64(1+float64(s.Mantissa)/2048.0) * float64(uint64(1)<<(31-s.Exponent))
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
	ResolutionStart   uint8
	ComponentStart    uint16
	LayerEnd          uint16
	ResolutionEnd     uint8
	ComponentEnd      uint16
	ProgressionOrder  uint8
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
	// Bit 15 (0x00008000) indicates HTJ2K is used when set.
	Pcap uint32

	// CCAPi contains extended component capabilities.
	// Each pair of bytes provides additional information for components.
	CCAPi []uint16
}

// CapPcapHTJ2K is the bit in Pcap indicating HTJ2K (Part 15) is used.
// When this bit is set, the codestream uses the High-Throughput block coder.
const CapPcapHTJ2K uint32 = 0x00008000 // Bit 15

// IsHTJ2K returns true if the CAP marker indicates HTJ2K mode.
func (c *CapabilitiesMarker) IsHTJ2K() bool {
	if c == nil {
		return false
	}
	return c.Pcap&CapPcapHTJ2K != 0
}

// TilePartHeader represents a tile-part header.
type TilePartHeader struct {
	TileIndex       uint16
	TilePartLength  uint32
	TilePartIndex   uint8
	NumTileParts    uint8

	// Optional tile-specific coding parameters
	CodingStyle           *CodingStyleDefault
	ComponentCodingStyles map[uint16]CodingStyleComponent
	Quantization          *QuantizationDefault
	ComponentQuantization map[uint16]QuantizationComponent
	ProgressionOrderChanges []ProgressionOrderChange
	PackedPacketHeaders   []byte
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
func (h *Header) Validate() error {
	if h.ImageWidth == 0 || h.ImageHeight == 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", h.ImageWidth, h.ImageHeight)
	}

	if h.TileWidth == 0 || h.TileHeight == 0 {
		return fmt.Errorf("invalid tile dimensions: %dx%d", h.TileWidth, h.TileHeight)
	}

	if h.NumComponents == 0 || h.NumComponents > 16384 {
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
		if prec < 1 || prec > 38 {
			return fmt.Errorf("component %d: invalid precision: %d", i, prec)
		}
	}

	return nil
}

// CalculateDerivedValues computes values derived from the main header.
func (h *Header) CalculateDerivedValues() {
	// Calculate number of tiles
	if h.TileWidth > 0 {
		h.NumTilesX = (h.ImageWidth - h.TileXOffset + h.TileWidth - 1) / h.TileWidth
	}
	if h.TileHeight > 0 {
		h.NumTilesY = (h.ImageHeight - h.TileYOffset + h.TileHeight - 1) / h.TileHeight
	}
}
