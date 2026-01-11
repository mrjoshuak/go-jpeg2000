package codestream

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Parser reads JPEG 2000 codestreams.
type Parser struct {
	r      io.Reader
	buf    []byte
	header *Header
	state  parserState
}

// parserState tracks the parser state machine.
type parserState int

const (
	stateInit parserState = iota
	stateSOC
	stateSIZ
	stateMainHeader
	stateTilePartHeader
	stateData
	stateEOC
)

// NewParser creates a new codestream parser.
func NewParser(r io.Reader) *Parser {
	return &Parser{
		r:      r,
		buf:    make([]byte, 4096),
		header: &Header{
			ComponentCodingStyles: make(map[uint16]CodingStyleComponent),
			ComponentQuantization: make(map[uint16]QuantizationComponent),
		},
		state: stateInit,
	}
}

// ReadHeader reads and parses the main header.
func (p *Parser) ReadHeader() (*Header, error) {
	// Read SOC marker
	if err := p.expectMarker(SOC); err != nil {
		return nil, fmt.Errorf("expected SOC marker: %w", err)
	}
	p.state = stateSOC

	// Read SIZ marker
	if err := p.readSIZ(); err != nil {
		return nil, fmt.Errorf("failed to read SIZ marker: %w", err)
	}
	p.state = stateSIZ

	// Read remaining main header markers
	for {
		marker, err := p.readMarker()
		if err != nil {
			return nil, fmt.Errorf("failed to read marker: %w", err)
		}

		switch marker {
		case COD:
			if err := p.readCOD(); err != nil {
				return nil, fmt.Errorf("failed to read COD marker: %w", err)
			}
		case COC:
			if err := p.readCOC(); err != nil {
				return nil, fmt.Errorf("failed to read COC marker: %w", err)
			}
		case QCD:
			if err := p.readQCD(); err != nil {
				return nil, fmt.Errorf("failed to read QCD marker: %w", err)
			}
		case QCC:
			if err := p.readQCC(); err != nil {
				return nil, fmt.Errorf("failed to read QCC marker: %w", err)
			}
		case POC:
			if err := p.readPOC(); err != nil {
				return nil, fmt.Errorf("failed to read POC marker: %w", err)
			}
		case TLM:
			if err := p.readTLM(); err != nil {
				return nil, fmt.Errorf("failed to read TLM marker: %w", err)
			}
		case PLM:
			if err := p.readPLM(); err != nil {
				return nil, fmt.Errorf("failed to read PLM marker: %w", err)
			}
		case PPM:
			if err := p.readPPM(); err != nil {
				return nil, fmt.Errorf("failed to read PPM marker: %w", err)
			}
		case CRG:
			if err := p.readCRG(); err != nil {
				return nil, fmt.Errorf("failed to read CRG marker: %w", err)
			}
		case COM:
			if err := p.readCOM(); err != nil {
				return nil, fmt.Errorf("failed to read COM marker: %w", err)
			}
		case CAP:
			if err := p.readCAP(); err != nil {
				return nil, fmt.Errorf("failed to read CAP marker: %w", err)
			}
		case SOT:
			// Start of tile-part header - main header is complete
			p.state = stateMainHeader
			p.header.CalculateDerivedValues()
			if err := p.header.Validate(); err != nil {
				return nil, fmt.Errorf("invalid header: %w", err)
			}
			return p.header, nil
		default:
			// Skip unknown markers
			if err := p.skipMarkerSegment(); err != nil {
				return nil, fmt.Errorf("failed to skip marker 0x%04X: %w", marker, err)
			}
		}
	}
}

// expectMarker reads and verifies the next marker.
func (p *Parser) expectMarker(expected Marker) error {
	marker, err := p.readMarker()
	if err != nil {
		return err
	}
	if marker != expected {
		return fmt.Errorf("expected marker 0x%04X, got 0x%04X", expected, marker)
	}
	return nil
}

// readMarker reads the next marker.
func (p *Parser) readMarker() (Marker, error) {
	if _, err := io.ReadFull(p.r, p.buf[:2]); err != nil {
		return 0, err
	}
	return Marker(binary.BigEndian.Uint16(p.buf[:2])), nil
}

// readUint16 reads a big-endian uint16.
func (p *Parser) readUint16() (uint16, error) {
	if _, err := io.ReadFull(p.r, p.buf[:2]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(p.buf[:2]), nil
}

// readUint32 reads a big-endian uint32.
func (p *Parser) readUint32() (uint32, error) {
	if _, err := io.ReadFull(p.r, p.buf[:4]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(p.buf[:4]), nil
}

// readByte reads a single byte.
func (p *Parser) readByte() (byte, error) {
	if _, err := io.ReadFull(p.r, p.buf[:1]); err != nil {
		return 0, err
	}
	return p.buf[0], nil
}

// readBytes reads n bytes.
func (p *Parser) readBytes(n int) ([]byte, error) {
	data := make([]byte, n)
	if _, err := io.ReadFull(p.r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// skipMarkerSegment skips the current marker segment.
func (p *Parser) skipMarkerSegment() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}
	if length < 2 {
		return fmt.Errorf("invalid marker segment length: %d", length)
	}
	_, err = io.CopyN(io.Discard, p.r, int64(length-2))
	return err
}

// readSIZ reads the SIZ (image and tile size) marker segment.
func (p *Parser) readSIZ() error {
	if err := p.expectMarker(SIZ); err != nil {
		return err
	}

	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Read capability (Rsiz)
	rsiz, err := p.readUint16()
	if err != nil {
		return err
	}
	p.header.Profile = rsiz

	// Read image size
	p.header.ImageWidth, err = p.readUint32()
	if err != nil {
		return err
	}
	p.header.ImageHeight, err = p.readUint32()
	if err != nil {
		return err
	}

	// Read image offset
	p.header.ImageXOffset, err = p.readUint32()
	if err != nil {
		return err
	}
	p.header.ImageYOffset, err = p.readUint32()
	if err != nil {
		return err
	}

	// Read tile size
	p.header.TileWidth, err = p.readUint32()
	if err != nil {
		return err
	}
	p.header.TileHeight, err = p.readUint32()
	if err != nil {
		return err
	}

	// Read tile offset
	p.header.TileXOffset, err = p.readUint32()
	if err != nil {
		return err
	}
	p.header.TileYOffset, err = p.readUint32()
	if err != nil {
		return err
	}

	// Read number of components
	p.header.NumComponents, err = p.readUint16()
	if err != nil {
		return err
	}

	// Validate length
	expectedLen := 38 + 3*int(p.header.NumComponents)
	if int(length) != expectedLen {
		return fmt.Errorf("SIZ length mismatch: expected %d, got %d", expectedLen, length)
	}

	// Read component info
	p.header.ComponentInfo = make([]ComponentInfo, p.header.NumComponents)
	for i := range p.header.ComponentInfo {
		ssiz, err := p.readByte()
		if err != nil {
			return err
		}
		xrsiz, err := p.readByte()
		if err != nil {
			return err
		}
		yrsiz, err := p.readByte()
		if err != nil {
			return err
		}
		p.header.ComponentInfo[i] = ComponentInfo{
			BitDepth:     ssiz,
			SubsamplingX: xrsiz,
			SubsamplingY: yrsiz,
		}
	}

	return nil
}

// readCOD reads the COD (coding style default) marker segment.
func (p *Parser) readCOD() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Read Scod
	scod, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.CodingStyle = scod

	// Read SGcod
	progOrder, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.ProgressionOrder = progOrder

	numLayers, err := p.readUint16()
	if err != nil {
		return err
	}
	p.header.CodingStyle.NumLayers = numLayers

	mct, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.MultipleComponentXf = mct

	// Read SPcod
	numDecomp, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.NumDecompositions = numDecomp

	cbWidth, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.CodeBlockWidthExp = cbWidth

	cbHeight, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.CodeBlockHeightExp = cbHeight

	cbStyle, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.CodeBlockStyle = cbStyle

	wavelet, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.CodingStyle.WaveletTransform = wavelet

	// Read precinct sizes if present
	if scod&CodingStylePrecincts != 0 {
		numPrecinct := int(length) - 12
		if numPrecinct > 0 {
			p.header.CodingStyle.PrecinctSizes = make([]PrecinctSize, numPrecinct)
			for i := 0; i < numPrecinct; i++ {
				pp, err := p.readByte()
				if err != nil {
					return err
				}
				p.header.CodingStyle.PrecinctSizes[i] = PrecinctSize{
					WidthExp:  pp & 0x0F,
					HeightExp: (pp >> 4) & 0x0F,
				}
			}
		}
	}

	return nil
}

// readCOC reads the COC (coding style component) marker segment.
func (p *Parser) readCOC() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	var compIndex uint16
	if p.header.NumComponents < 257 {
		b, err := p.readByte()
		if err != nil {
			return err
		}
		compIndex = uint16(b)
	} else {
		compIndex, err = p.readUint16()
		if err != nil {
			return err
		}
	}

	coc := CodingStyleComponent{ComponentIndex: compIndex}

	scoc, err := p.readByte()
	if err != nil {
		return err
	}
	coc.CodingStyle = scoc

	numDecomp, err := p.readByte()
	if err != nil {
		return err
	}
	coc.NumDecompositions = numDecomp

	cbWidth, err := p.readByte()
	if err != nil {
		return err
	}
	coc.CodeBlockWidthExp = cbWidth

	cbHeight, err := p.readByte()
	if err != nil {
		return err
	}
	coc.CodeBlockHeightExp = cbHeight

	cbStyle, err := p.readByte()
	if err != nil {
		return err
	}
	coc.CodeBlockStyle = cbStyle

	wavelet, err := p.readByte()
	if err != nil {
		return err
	}
	coc.WaveletTransform = wavelet

	// Calculate remaining bytes for precinct sizes
	baseLen := 7
	if p.header.NumComponents >= 257 {
		baseLen = 8
	}

	if scoc&CodingStylePrecincts != 0 {
		numPrecinct := int(length) - baseLen
		if numPrecinct > 0 {
			coc.PrecinctSizes = make([]PrecinctSize, numPrecinct)
			for i := 0; i < numPrecinct; i++ {
				pp, err := p.readByte()
				if err != nil {
					return err
				}
				coc.PrecinctSizes[i] = PrecinctSize{
					WidthExp:  pp & 0x0F,
					HeightExp: (pp >> 4) & 0x0F,
				}
			}
		}
	}

	p.header.ComponentCodingStyles[compIndex] = coc
	return nil
}

// readQCD reads the QCD (quantization default) marker segment.
func (p *Parser) readQCD() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	sqcd, err := p.readByte()
	if err != nil {
		return err
	}
	p.header.Quantization.QuantizationStyle = sqcd & 0x1F
	p.header.Quantization.NumGuardBits = sqcd >> 5

	// Read step sizes based on quantization style
	remaining := int(length) - 3
	style := sqcd & 0x1F

	switch style {
	case QuantizationNone:
		// No quantization: one exponent byte per subband
		numBands := remaining
		p.header.Quantization.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			exp, err := p.readByte()
			if err != nil {
				return err
			}
			p.header.Quantization.StepSizes[i] = StepSize{
				Exponent: exp >> 3,
			}
		}

	case QuantizationScalarDerived:
		// Scalar derived: one base step size
		val, err := p.readUint16()
		if err != nil {
			return err
		}
		p.header.Quantization.StepSizes = []StepSize{{
			Mantissa: val & 0x07FF,
			Exponent: uint8(val >> 11),
		}}

	case QuantizationScalarExpounded:
		// Scalar expounded: step size per subband
		numBands := remaining / 2
		p.header.Quantization.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			val, err := p.readUint16()
			if err != nil {
				return err
			}
			p.header.Quantization.StepSizes[i] = StepSize{
				Mantissa: val & 0x07FF,
				Exponent: uint8(val >> 11),
			}
		}
	}

	return nil
}

// readQCC reads the QCC (quantization component) marker segment.
func (p *Parser) readQCC() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	var compIndex uint16
	var headerBytes int
	if p.header.NumComponents < 257 {
		b, err := p.readByte()
		if err != nil {
			return err
		}
		compIndex = uint16(b)
		headerBytes = 3
	} else {
		compIndex, err = p.readUint16()
		if err != nil {
			return err
		}
		headerBytes = 4
	}

	sqcc, err := p.readByte()
	if err != nil {
		return err
	}

	qcc := QuantizationComponent{
		ComponentIndex:    compIndex,
		QuantizationStyle: sqcc & 0x1F,
		NumGuardBits:      sqcc >> 5,
	}

	remaining := int(length) - headerBytes - 1
	style := sqcc & 0x1F

	switch style {
	case QuantizationNone:
		numBands := remaining
		qcc.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			exp, err := p.readByte()
			if err != nil {
				return err
			}
			qcc.StepSizes[i] = StepSize{
				Exponent: exp >> 3,
			}
		}

	case QuantizationScalarDerived:
		val, err := p.readUint16()
		if err != nil {
			return err
		}
		qcc.StepSizes = []StepSize{{
			Mantissa: val & 0x07FF,
			Exponent: uint8(val >> 11),
		}}

	case QuantizationScalarExpounded:
		numBands := remaining / 2
		qcc.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			val, err := p.readUint16()
			if err != nil {
				return err
			}
			qcc.StepSizes[i] = StepSize{
				Mantissa: val & 0x07FF,
				Exponent: uint8(val >> 11),
			}
		}
	}

	p.header.ComponentQuantization[compIndex] = qcc
	return nil
}

// readPOC reads the POC (progression order change) marker segment.
func (p *Parser) readPOC() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	entrySize := 7
	if p.header.NumComponents >= 257 {
		entrySize = 9
	}

	numEntries := (int(length) - 2) / entrySize
	for i := 0; i < numEntries; i++ {
		poc := ProgressionOrderChange{}

		poc.ResolutionStart, err = p.readByte()
		if err != nil {
			return err
		}

		if p.header.NumComponents < 257 {
			b, err := p.readByte()
			if err != nil {
				return err
			}
			poc.ComponentStart = uint16(b)
		} else {
			poc.ComponentStart, err = p.readUint16()
			if err != nil {
				return err
			}
		}

		poc.LayerEnd, err = p.readUint16()
		if err != nil {
			return err
		}

		poc.ResolutionEnd, err = p.readByte()
		if err != nil {
			return err
		}

		if p.header.NumComponents < 257 {
			b, err := p.readByte()
			if err != nil {
				return err
			}
			poc.ComponentEnd = uint16(b)
		} else {
			poc.ComponentEnd, err = p.readUint16()
			if err != nil {
				return err
			}
		}

		poc.ProgressionOrder, err = p.readByte()
		if err != nil {
			return err
		}

		p.header.ProgressionOrderChanges = append(p.header.ProgressionOrderChanges, poc)
	}

	return nil
}

// readTLM reads the TLM (tile-part lengths) marker segment.
func (p *Parser) readTLM() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Skip Ztlm (index)
	if _, err := p.readByte(); err != nil {
		return err
	}

	stlm, err := p.readByte()
	if err != nil {
		return err
	}

	// Determine sizes
	st := (stlm >> 4) & 0x03
	sp := (stlm >> 6) & 0x01

	var tileIndexSize int
	switch st {
	case 0:
		tileIndexSize = 0
	case 1:
		tileIndexSize = 1
	case 2:
		tileIndexSize = 2
	default:
		return fmt.Errorf("invalid ST value in TLM: %d", st)
	}

	lengthSize := 2
	if sp == 1 {
		lengthSize = 4
	}

	entrySize := tileIndexSize + lengthSize
	numEntries := (int(length) - 4) / entrySize

	for i := 0; i < numEntries; i++ {
		tl := TileLength{}

		switch tileIndexSize {
		case 0:
			// Implicit tile index
			tl.TileIndex = uint16(i)
		case 1:
			b, err := p.readByte()
			if err != nil {
				return err
			}
			tl.TileIndex = uint16(b)
		case 2:
			tl.TileIndex, err = p.readUint16()
			if err != nil {
				return err
			}
		}

		switch lengthSize {
		case 2:
			val, err := p.readUint16()
			if err != nil {
				return err
			}
			tl.Length = uint32(val)
		case 4:
			tl.Length, err = p.readUint32()
			if err != nil {
				return err
			}
		}

		p.header.TileLengths = append(p.header.TileLengths, tl)
	}

	return nil
}

// readPLM reads the PLM (packet lengths, main header) marker segment.
func (p *Parser) readPLM() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Skip Zplm (index)
	if _, err := p.readByte(); err != nil {
		return err
	}

	// Read packet lengths (variable length encoded)
	remaining := int(length) - 3
	for remaining > 0 {
		val, n, err := p.readVariableLength()
		if err != nil {
			return err
		}
		p.header.PacketLengths = append(p.header.PacketLengths, val)
		remaining -= n
	}

	return nil
}

// readVariableLength reads a variable-length encoded value.
func (p *Parser) readVariableLength() (uint32, int, error) {
	var value uint32
	n := 0
	for {
		b, err := p.readByte()
		if err != nil {
			return 0, n, err
		}
		n++
		value = (value << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	return value, n, nil
}

// readPPM reads the PPM (packed packet headers, main header) marker segment.
func (p *Parser) readPPM() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Skip Zppm (index)
	if _, err := p.readByte(); err != nil {
		return err
	}

	// Read packed data
	data, err := p.readBytes(int(length) - 3)
	if err != nil {
		return err
	}
	p.header.PackedPacketHeaders = append(p.header.PackedPacketHeaders, data...)

	return nil
}

// readCRG reads the CRG (component registration) marker segment.
func (p *Parser) readCRG() error {
	// We don't currently use CRG data, just skip it
	return p.skipMarkerSegment()
}

// readCOM reads the COM (comment) marker segment.
func (p *Parser) readCOM() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	rcom, err := p.readUint16()
	if err != nil {
		return err
	}
	p.header.CommentType = rcom

	data, err := p.readBytes(int(length) - 4)
	if err != nil {
		return err
	}

	if rcom == CommentLatin1 {
		p.header.Comment = string(data)
	}

	return nil
}

// readCAP reads the CAP (extended capabilities) marker segment.
// The CAP marker is defined in ISO/IEC 15444-2 (Part 2) and is required
// for HTJ2K (Part 15) to indicate High-Throughput mode.
func (p *Parser) readCAP() error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	// Minimum length is 6 bytes: 2 for length + 4 for Pcap
	if length < 6 {
		return fmt.Errorf("CAP marker too short: %d bytes", length)
	}

	cap := &CapabilitiesMarker{}

	// Read Pcap (32-bit capabilities flags)
	cap.Pcap, err = p.readUint32()
	if err != nil {
		return err
	}

	// Read CCAPi entries if present (remaining bytes after length and Pcap)
	// Each CCAPi is 2 bytes
	remaining := int(length) - 6
	if remaining > 0 {
		numCCAPi := remaining / 2
		cap.CCAPi = make([]uint16, numCCAPi)
		for i := 0; i < numCCAPi; i++ {
			cap.CCAPi[i], err = p.readUint16()
			if err != nil {
				return err
			}
		}
	}

	p.header.Capabilities = cap
	return nil
}

// Header returns the parsed header.
func (p *Parser) Header() *Header {
	return p.header
}

// ReadTilePartHeader reads a tile-part header.
func (p *Parser) ReadTilePartHeader() (*TilePartHeader, error) {
	// Read SOT marker length
	length, err := p.readUint16()
	if err != nil {
		return nil, err
	}
	if length != 10 {
		return nil, fmt.Errorf("invalid SOT length: %d", length)
	}

	tph := &TilePartHeader{
		ComponentCodingStyles: make(map[uint16]CodingStyleComponent),
		ComponentQuantization: make(map[uint16]QuantizationComponent),
	}

	// Read tile index
	tph.TileIndex, err = p.readUint16()
	if err != nil {
		return nil, err
	}

	// Read tile-part length
	tph.TilePartLength, err = p.readUint32()
	if err != nil {
		return nil, err
	}

	// Read tile-part index
	tph.TilePartIndex, err = p.readByte()
	if err != nil {
		return nil, err
	}

	// Read number of tile-parts
	tph.NumTileParts, err = p.readByte()
	if err != nil {
		return nil, err
	}

	// Read tile-part header markers until SOD
	for {
		marker, err := p.readMarker()
		if err != nil {
			return nil, err
		}

		switch marker {
		case COD:
			cod := &CodingStyleDefault{}
			if err := p.readCODInto(cod); err != nil {
				return nil, err
			}
			tph.CodingStyle = cod
		case COC:
			if err := p.readCOCInto(tph.ComponentCodingStyles); err != nil {
				return nil, err
			}
		case QCD:
			qcd := &QuantizationDefault{}
			if err := p.readQCDInto(qcd); err != nil {
				return nil, err
			}
			tph.Quantization = qcd
		case QCC:
			if err := p.readQCCInto(tph.ComponentQuantization); err != nil {
				return nil, err
			}
		case POC:
			poc, err := p.readPOCEntries()
			if err != nil {
				return nil, err
			}
			tph.ProgressionOrderChanges = poc
		case PPT:
			data, err := p.readPPT()
			if err != nil {
				return nil, err
			}
			tph.PackedPacketHeaders = append(tph.PackedPacketHeaders, data...)
		case SOD:
			p.state = stateData
			return tph, nil
		default:
			if err := p.skipMarkerSegment(); err != nil {
				return nil, err
			}
		}
	}
}

// readCODInto reads COD marker data into the provided struct.
func (p *Parser) readCODInto(cod *CodingStyleDefault) error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	scod, err := p.readByte()
	if err != nil {
		return err
	}
	cod.CodingStyle = scod

	cod.ProgressionOrder, err = p.readByte()
	if err != nil {
		return err
	}

	cod.NumLayers, err = p.readUint16()
	if err != nil {
		return err
	}

	cod.MultipleComponentXf, err = p.readByte()
	if err != nil {
		return err
	}

	cod.NumDecompositions, err = p.readByte()
	if err != nil {
		return err
	}

	cod.CodeBlockWidthExp, err = p.readByte()
	if err != nil {
		return err
	}

	cod.CodeBlockHeightExp, err = p.readByte()
	if err != nil {
		return err
	}

	cod.CodeBlockStyle, err = p.readByte()
	if err != nil {
		return err
	}

	cod.WaveletTransform, err = p.readByte()
	if err != nil {
		return err
	}

	if scod&CodingStylePrecincts != 0 {
		numPrecinct := int(length) - 12
		if numPrecinct > 0 {
			cod.PrecinctSizes = make([]PrecinctSize, numPrecinct)
			for i := 0; i < numPrecinct; i++ {
				pp, err := p.readByte()
				if err != nil {
					return err
				}
				cod.PrecinctSizes[i] = PrecinctSize{
					WidthExp:  pp & 0x0F,
					HeightExp: (pp >> 4) & 0x0F,
				}
			}
		}
	}

	return nil
}

// readCOCInto reads a COC marker into the provided map.
func (p *Parser) readCOCInto(m map[uint16]CodingStyleComponent) error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	var compIndex uint16
	if p.header.NumComponents < 257 {
		b, err := p.readByte()
		if err != nil {
			return err
		}
		compIndex = uint16(b)
	} else {
		compIndex, err = p.readUint16()
		if err != nil {
			return err
		}
	}

	coc := CodingStyleComponent{ComponentIndex: compIndex}

	coc.CodingStyle, err = p.readByte()
	if err != nil {
		return err
	}

	coc.NumDecompositions, err = p.readByte()
	if err != nil {
		return err
	}

	coc.CodeBlockWidthExp, err = p.readByte()
	if err != nil {
		return err
	}

	coc.CodeBlockHeightExp, err = p.readByte()
	if err != nil {
		return err
	}

	coc.CodeBlockStyle, err = p.readByte()
	if err != nil {
		return err
	}

	coc.WaveletTransform, err = p.readByte()
	if err != nil {
		return err
	}

	baseLen := 7
	if p.header.NumComponents >= 257 {
		baseLen = 8
	}

	if coc.CodingStyle&CodingStylePrecincts != 0 {
		numPrecinct := int(length) - baseLen
		if numPrecinct > 0 {
			coc.PrecinctSizes = make([]PrecinctSize, numPrecinct)
			for i := 0; i < numPrecinct; i++ {
				pp, err := p.readByte()
				if err != nil {
					return err
				}
				coc.PrecinctSizes[i] = PrecinctSize{
					WidthExp:  pp & 0x0F,
					HeightExp: (pp >> 4) & 0x0F,
				}
			}
		}
	}

	m[compIndex] = coc
	return nil
}

// readQCDInto reads QCD marker data into the provided struct.
func (p *Parser) readQCDInto(qcd *QuantizationDefault) error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	sqcd, err := p.readByte()
	if err != nil {
		return err
	}
	qcd.QuantizationStyle = sqcd & 0x1F
	qcd.NumGuardBits = sqcd >> 5

	remaining := int(length) - 3
	style := sqcd & 0x1F

	switch style {
	case QuantizationNone:
		numBands := remaining
		qcd.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			exp, err := p.readByte()
			if err != nil {
				return err
			}
			qcd.StepSizes[i] = StepSize{Exponent: exp >> 3}
		}

	case QuantizationScalarDerived:
		val, err := p.readUint16()
		if err != nil {
			return err
		}
		qcd.StepSizes = []StepSize{{
			Mantissa: val & 0x07FF,
			Exponent: uint8(val >> 11),
		}}

	case QuantizationScalarExpounded:
		numBands := remaining / 2
		qcd.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			val, err := p.readUint16()
			if err != nil {
				return err
			}
			qcd.StepSizes[i] = StepSize{
				Mantissa: val & 0x07FF,
				Exponent: uint8(val >> 11),
			}
		}
	}

	return nil
}

// readQCCInto reads a QCC marker into the provided map.
func (p *Parser) readQCCInto(m map[uint16]QuantizationComponent) error {
	length, err := p.readUint16()
	if err != nil {
		return err
	}

	var compIndex uint16
	var headerBytes int
	if p.header.NumComponents < 257 {
		b, err := p.readByte()
		if err != nil {
			return err
		}
		compIndex = uint16(b)
		headerBytes = 3
	} else {
		compIndex, err = p.readUint16()
		if err != nil {
			return err
		}
		headerBytes = 4
	}

	sqcc, err := p.readByte()
	if err != nil {
		return err
	}

	qcc := QuantizationComponent{
		ComponentIndex:    compIndex,
		QuantizationStyle: sqcc & 0x1F,
		NumGuardBits:      sqcc >> 5,
	}

	remaining := int(length) - headerBytes - 1
	style := sqcc & 0x1F

	switch style {
	case QuantizationNone:
		numBands := remaining
		qcc.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			exp, err := p.readByte()
			if err != nil {
				return err
			}
			qcc.StepSizes[i] = StepSize{Exponent: exp >> 3}
		}

	case QuantizationScalarDerived:
		val, err := p.readUint16()
		if err != nil {
			return err
		}
		qcc.StepSizes = []StepSize{{
			Mantissa: val & 0x07FF,
			Exponent: uint8(val >> 11),
		}}

	case QuantizationScalarExpounded:
		numBands := remaining / 2
		qcc.StepSizes = make([]StepSize, numBands)
		for i := 0; i < numBands; i++ {
			val, err := p.readUint16()
			if err != nil {
				return err
			}
			qcc.StepSizes[i] = StepSize{
				Mantissa: val & 0x07FF,
				Exponent: uint8(val >> 11),
			}
		}
	}

	m[compIndex] = qcc
	return nil
}

// readPOCEntries reads POC marker entries.
func (p *Parser) readPOCEntries() ([]ProgressionOrderChange, error) {
	length, err := p.readUint16()
	if err != nil {
		return nil, err
	}

	entrySize := 7
	if p.header.NumComponents >= 257 {
		entrySize = 9
	}

	numEntries := (int(length) - 2) / entrySize
	entries := make([]ProgressionOrderChange, 0, numEntries)

	for i := 0; i < numEntries; i++ {
		poc := ProgressionOrderChange{}

		poc.ResolutionStart, err = p.readByte()
		if err != nil {
			return nil, err
		}

		if p.header.NumComponents < 257 {
			b, err := p.readByte()
			if err != nil {
				return nil, err
			}
			poc.ComponentStart = uint16(b)
		} else {
			poc.ComponentStart, err = p.readUint16()
			if err != nil {
				return nil, err
			}
		}

		poc.LayerEnd, err = p.readUint16()
		if err != nil {
			return nil, err
		}

		poc.ResolutionEnd, err = p.readByte()
		if err != nil {
			return nil, err
		}

		if p.header.NumComponents < 257 {
			b, err := p.readByte()
			if err != nil {
				return nil, err
			}
			poc.ComponentEnd = uint16(b)
		} else {
			poc.ComponentEnd, err = p.readUint16()
			if err != nil {
				return nil, err
			}
		}

		poc.ProgressionOrder, err = p.readByte()
		if err != nil {
			return nil, err
		}

		entries = append(entries, poc)
	}

	return entries, nil
}

// readPPT reads the PPT (packed packet headers, tile-part) marker segment.
func (p *Parser) readPPT() ([]byte, error) {
	length, err := p.readUint16()
	if err != nil {
		return nil, err
	}

	// Skip Zppt (index)
	if _, err := p.readByte(); err != nil {
		return nil, err
	}

	return p.readBytes(int(length) - 3)
}
