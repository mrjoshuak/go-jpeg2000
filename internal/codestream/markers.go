// Package codestream handles JPEG 2000 codestream parsing and generation.
package codestream

// Marker codes for JPEG 2000 codestreams.
// These are defined in ISO/IEC 15444-1 Annex A.
const (
	// Delimiting markers and marker segments
	SOC Marker = 0xFF4F // Start of codestream
	SOT Marker = 0xFF90 // Start of tile-part
	SOD Marker = 0xFF93 // Start of data
	EOC Marker = 0xFFD9 // End of codestream

	// Fixed information marker segments
	SIZ Marker = 0xFF51 // Image and tile size

	// Functional marker segments
	COD Marker = 0xFF52 // Coding style default
	COC Marker = 0xFF53 // Coding style component
	RGN Marker = 0xFF5E // Region-of-interest
	QCD Marker = 0xFF5C // Quantization default
	QCC Marker = 0xFF5D // Quantization component
	POC Marker = 0xFF5F // Progression order change

	// Pointer marker segments
	TLM Marker = 0xFF55 // Tile-part lengths
	PLM Marker = 0xFF57 // Packet length, main header
	PLT Marker = 0xFF58 // Packet length, tile-part header
	PPM Marker = 0xFF60 // Packed packet headers, main header
	PPT Marker = 0xFF61 // Packed packet headers, tile-part header

	// In bit stream markers and marker segments
	SOP Marker = 0xFF91 // Start of packet
	EPH Marker = 0xFF92 // End of packet header

	// Informational marker segments
	CRG Marker = 0xFF63 // Component registration
	COM Marker = 0xFF64 // Comment

	// Part 2 extensions
	CAP Marker = 0xFF50 // Extended capabilities
	CBD Marker = 0xFF78 // Component bit depth
	MCT Marker = 0xFF74 // Multiple component transform collection
	MCC Marker = 0xFF75 // Multiple component transform component
	MCO Marker = 0xFF77 // Multiple component transform ordering
)

// Marker represents a JPEG 2000 marker code.
type Marker uint16

// String returns the string representation of a marker.
func (m Marker) String() string {
	switch m {
	case SOC:
		return "SOC"
	case SOT:
		return "SOT"
	case SOD:
		return "SOD"
	case EOC:
		return "EOC"
	case SIZ:
		return "SIZ"
	case COD:
		return "COD"
	case COC:
		return "COC"
	case RGN:
		return "RGN"
	case QCD:
		return "QCD"
	case QCC:
		return "QCC"
	case POC:
		return "POC"
	case TLM:
		return "TLM"
	case PLM:
		return "PLM"
	case PLT:
		return "PLT"
	case PPM:
		return "PPM"
	case PPT:
		return "PPT"
	case SOP:
		return "SOP"
	case EPH:
		return "EPH"
	case CRG:
		return "CRG"
	case COM:
		return "COM"
	case CAP:
		return "CAP"
	case CBD:
		return "CBD"
	case MCT:
		return "MCT"
	case MCC:
		return "MCC"
	case MCO:
		return "MCO"
	default:
		return "UNKNOWN"
	}
}

// HasLength returns true if this marker has a length field following it.
func (m Marker) HasLength() bool {
	switch m {
	case SOC, SOD, EOC, EPH:
		return false
	default:
		return true
	}
}

// IsDelimiter returns true if this is a delimiting marker.
func (m Marker) IsDelimiter() bool {
	switch m {
	case SOC, SOT, SOD, EOC:
		return true
	default:
		return false
	}
}

// Coding style flags (from COD/COC markers).
const (
	// CodingStylePrecincts indicates custom precinct sizes are used.
	CodingStylePrecincts uint8 = 0x01
	// CodingStyleSOP indicates SOP markers are used.
	CodingStyleSOP uint8 = 0x02
	// CodingStyleEPH indicates EPH markers are used.
	CodingStyleEPH uint8 = 0x04
)

// Code block style flags.
const (
	// CodeBlockBypass enables selective arithmetic coding bypass.
	CodeBlockBypass uint8 = 0x01
	// CodeBlockReset resets context probabilities on each coding pass.
	CodeBlockReset uint8 = 0x02
	// CodeBlockTermination enables termination on each coding pass.
	CodeBlockTermination uint8 = 0x04
	// CodeBlockVerticalCausal enables vertically causal context formation.
	CodeBlockVerticalCausal uint8 = 0x08
	// CodeBlockPredictableTermination enables predictable termination.
	CodeBlockPredictableTermination uint8 = 0x10
	// CodeBlockSegmentationSymbols enables segmentation symbols.
	CodeBlockSegmentationSymbols uint8 = 0x20
	// CodeBlockHT enables high-throughput mode (HTJ2K).
	CodeBlockHT uint8 = 0x40
)

// Quantization style values.
const (
	// QuantizationNone indicates no quantization.
	QuantizationNone uint8 = 0x00
	// QuantizationScalarDerived indicates scalar derived quantization.
	QuantizationScalarDerived uint8 = 0x01
	// QuantizationScalarExpounded indicates scalar expounded quantization.
	QuantizationScalarExpounded uint8 = 0x02
)

// Comment registration values for COM marker.
const (
	// CommentBinary indicates binary data.
	CommentBinary uint16 = 0
	// CommentLatin1 indicates Latin-1 (ISO 8859-1) text.
	CommentLatin1 uint16 = 1
)

// ProgressionOrder defines the order in which packets are encoded/decoded.
type ProgressionOrder uint8

const (
	// LRCP is Layer-Resolution-Component-Position order.
	LRCP ProgressionOrder = iota
	// RLCP is Resolution-Layer-Component-Position order.
	RLCP
	// RPCL is Resolution-Position-Component-Layer order.
	RPCL
	// PCRL is Position-Component-Resolution-Layer order.
	PCRL
	// CPRL is Component-Position-Resolution-Layer order.
	CPRL
)
