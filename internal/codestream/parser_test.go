package codestream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// errorReader returns an error after reading n bytes
type errorReader struct {
	data []byte
	pos  int
	errAt int
}

func newErrorReader(data []byte, errAt int) *errorReader {
	return &errorReader{data: data, pos: 0, errAt: errAt}
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	if r.pos >= r.errAt {
		return 0, errors.New("simulated read error")
	}
	remaining := r.errAt - r.pos
	if remaining > len(p) {
		remaining = len(p)
	}
	if r.pos+remaining > len(r.data) {
		remaining = len(r.data) - r.pos
		if remaining <= 0 {
			return 0, io.EOF
		}
	}
	n = copy(p, r.data[r.pos:r.pos+remaining])
	r.pos += n
	return n, nil
}

func TestMarker_String(t *testing.T) {
	tests := []struct {
		marker Marker
		want   string
	}{
		{SOC, "SOC"},
		{SOT, "SOT"},
		{SOD, "SOD"},
		{EOC, "EOC"},
		{SIZ, "SIZ"},
		{COD, "COD"},
		{COC, "COC"},
		{RGN, "RGN"},
		{QCD, "QCD"},
		{QCC, "QCC"},
		{POC, "POC"},
		{TLM, "TLM"},
		{PLM, "PLM"},
		{PLT, "PLT"},
		{PPM, "PPM"},
		{PPT, "PPT"},
		{SOP, "SOP"},
		{EPH, "EPH"},
		{CRG, "CRG"},
		{COM, "COM"},
		{CAP, "CAP"},
		{CBD, "CBD"},
		{MCT, "MCT"},
		{MCC, "MCC"},
		{MCO, "MCO"},
		{0x0000, "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.marker.String()
		if got != tt.want {
			t.Errorf("Marker(%04X).String() = %q, want %q", tt.marker, got, tt.want)
		}
	}
}

func TestMarker_HasLength(t *testing.T) {
	tests := []struct {
		marker Marker
		want   bool
	}{
		{SOC, false},
		{SOD, false},
		{EOC, false},
		{EPH, false},
		{SIZ, true},
		{COD, true},
		{QCD, true},
		{COM, true},
	}

	for _, tt := range tests {
		got := tt.marker.HasLength()
		if got != tt.want {
			t.Errorf("Marker(%s).HasLength() = %v, want %v", tt.marker, got, tt.want)
		}
	}
}

func TestMarker_IsDelimiter(t *testing.T) {
	tests := []struct {
		marker Marker
		want   bool
	}{
		{SOC, true},
		{SOT, true},
		{SOD, true},
		{EOC, true},
		{SIZ, false},
		{COD, false},
	}

	for _, tt := range tests {
		got := tt.marker.IsDelimiter()
		if got != tt.want {
			t.Errorf("Marker(%s).IsDelimiter() = %v, want %v", tt.marker, got, tt.want)
		}
	}
}

func TestComponentInfo_Precision(t *testing.T) {
	tests := []struct {
		bitDepth uint8
		want     int
	}{
		{0, 1},
		{7, 8},
		{15, 16},
		{0x87, 8}, // signed 8-bit
	}

	for _, tt := range tests {
		c := ComponentInfo{BitDepth: tt.bitDepth}
		got := c.Precision()
		if got != tt.want {
			t.Errorf("ComponentInfo{BitDepth: %d}.Precision() = %d, want %d", tt.bitDepth, got, tt.want)
		}
	}
}

func TestComponentInfo_IsSigned(t *testing.T) {
	tests := []struct {
		bitDepth uint8
		want     bool
	}{
		{7, false},
		{0x87, true},
		{0xFF, true},
		{0x00, false},
	}

	for _, tt := range tests {
		c := ComponentInfo{BitDepth: tt.bitDepth}
		got := c.IsSigned()
		if got != tt.want {
			t.Errorf("ComponentInfo{BitDepth: %02X}.IsSigned() = %v, want %v", tt.bitDepth, got, tt.want)
		}
	}
}

func TestCodingStyleDefault_CodeBlockWidth(t *testing.T) {
	c := CodingStyleDefault{CodeBlockWidthExp: 4} // 2^(4+2) = 64
	if got := c.CodeBlockWidth(); got != 64 {
		t.Errorf("CodeBlockWidth() = %d, want 64", got)
	}
}

func TestCodingStyleDefault_CodeBlockHeight(t *testing.T) {
	c := CodingStyleDefault{CodeBlockHeightExp: 4} // 2^(4+2) = 64
	if got := c.CodeBlockHeight(); got != 64 {
		t.Errorf("CodeBlockHeight() = %d, want 64", got)
	}
}

func TestQuantizationDefault_Style(t *testing.T) {
	tests := []struct {
		qstyle uint8
		want   uint8
	}{
		{0x00, 0},
		{0x01, 1},
		{0x02, 2},
		{0x40, 0}, // guard bits set, style 0
		{0x61, 1}, // guard bits set, style 1
	}
	for _, tt := range tests {
		q := QuantizationDefault{QuantizationStyle: tt.qstyle}
		if got := q.Style(); got != tt.want {
			t.Errorf("QuantizationDefault{%02X}.Style() = %d, want %d", tt.qstyle, got, tt.want)
		}
	}
}

func TestQuantizationDefault_GuardBits(t *testing.T) {
	tests := []struct {
		guardBits uint8
		want      int
	}{
		{0x00, 0},
		{0x20, 1},
		{0x40, 2},
		{0x60, 3},
		{0xE0, 7},
	}
	for _, tt := range tests {
		q := QuantizationDefault{NumGuardBits: tt.guardBits}
		if got := q.GuardBits(); got != tt.want {
			t.Errorf("QuantizationDefault{NumGuardBits: %02X}.GuardBits() = %d, want %d", tt.guardBits, got, tt.want)
		}
	}
}

func TestCodingStyleDefault_NumResolutions(t *testing.T) {
	c := CodingStyleDefault{NumDecompositions: 5}
	if got := c.NumResolutions(); got != 6 {
		t.Errorf("NumResolutions() = %d, want 6", got)
	}
}

func TestCodingStyleDefault_IsReversible(t *testing.T) {
	c := CodingStyleDefault{WaveletTransform: 1}
	if !c.IsReversible() {
		t.Error("expected reversible for WaveletTransform=1")
	}

	c.WaveletTransform = 0
	if c.IsReversible() {
		t.Error("expected irreversible for WaveletTransform=0")
	}
}

func TestPrecinctSize_Width_Height(t *testing.T) {
	p := PrecinctSize{WidthExp: 6, HeightExp: 5}
	if got := p.Width(); got != 64 {
		t.Errorf("Width() = %d, want 64", got)
	}
	if got := p.Height(); got != 32 {
		t.Errorf("Height() = %d, want 32", got)
	}
}

func TestStepSize_Value(t *testing.T) {
	s := StepSize{Mantissa: 0, Exponent: 0}
	val := s.Value()
	if val <= 0 {
		t.Errorf("StepSize{0, 0}.Value() = %v, expected positive", val)
	}
}

func TestHeader_Validate(t *testing.T) {
	tests := []struct {
		name    string
		header  *Header
		wantErr bool
	}{
		{
			name: "valid",
			header: &Header{
				ImageWidth:    100,
				ImageHeight:   100,
				TileWidth:     100,
				TileHeight:    100,
				NumComponents: 3,
				ComponentInfo: []ComponentInfo{
					{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1},
					{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1},
					{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1},
				},
			},
			wantErr: false,
		},
		{
			name: "zero width",
			header: &Header{
				ImageWidth:  0,
				ImageHeight: 100,
				TileWidth:   100,
				TileHeight:  100,
			},
			wantErr: true,
		},
		{
			name: "zero components",
			header: &Header{
				ImageWidth:    100,
				ImageHeight:   100,
				TileWidth:     100,
				TileHeight:    100,
				NumComponents: 0,
			},
			wantErr: true,
		},
		{
			name: "component mismatch",
			header: &Header{
				ImageWidth:    100,
				ImageHeight:   100,
				TileWidth:     100,
				TileHeight:    100,
				NumComponents: 2,
				ComponentInfo: []ComponentInfo{
					{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1},
				},
			},
			wantErr: true,
		},
		{
			name: "zero subsampling",
			header: &Header{
				ImageWidth:    100,
				ImageHeight:   100,
				TileWidth:     100,
				TileHeight:    100,
				NumComponents: 1,
				ComponentInfo: []ComponentInfo{
					{BitDepth: 7, SubsamplingX: 0, SubsamplingY: 1},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.header.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHeader_CalculateDerivedValues(t *testing.T) {
	h := &Header{
		ImageWidth:  100,
		ImageHeight: 100,
		TileWidth:   32,
		TileHeight:  32,
	}
	h.CalculateDerivedValues()

	if h.NumTilesX != 4 {
		t.Errorf("NumTilesX = %d, want 4", h.NumTilesX)
	}
	if h.NumTilesY != 4 {
		t.Errorf("NumTilesY = %d, want 4", h.NumTilesY)
	}
}

func TestProgressionOrder(t *testing.T) {
	tests := []struct {
		order ProgressionOrder
		want  uint8
	}{
		{LRCP, 0},
		{RLCP, 1},
		{RPCL, 2},
		{PCRL, 3},
		{CPRL, 4},
	}

	for _, tt := range tests {
		if uint8(tt.order) != tt.want {
			t.Errorf("ProgressionOrder %d = %d, want %d", tt.order, uint8(tt.order), tt.want)
		}
	}
}

// Helper to create a minimal valid codestream for parsing tests
func createMinimalCodestream() []byte {
	var buf bytes.Buffer

	// SOC
	binary.Write(&buf, binary.BigEndian, uint16(SOC))

	// SIZ
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41)) // Length for 1 component
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, uint16(1))  // Csiz
	buf.WriteByte(7)                                 // Ssiz (8-bit unsigned)
	buf.WriteByte(1)                                 // XRsiz
	buf.WriteByte(1)                                 // YRsiz

	// COD
	binary.Write(&buf, binary.BigEndian, uint16(COD))
	binary.Write(&buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(&buf, binary.BigEndian, uint16(1)) // Layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Decomposition levels
	buf.WriteByte(4)                                // Code-block width
	buf.WriteByte(4)                                // Code-block height
	buf.WriteByte(0)                                // Code-block style
	buf.WriteByte(1)                                // Wavelet transform

	// QCD
	binary.Write(&buf, binary.BigEndian, uint16(QCD))
	binary.Write(&buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x40) // Sqcd
	binary.Write(&buf, binary.BigEndian, uint16(0x4000))

	// SOT (to end main header)
	binary.Write(&buf, binary.BigEndian, uint16(SOT))

	return buf.Bytes()
}

func TestParser_ReadHeader(t *testing.T) {
	data := createMinimalCodestream()
	parser := NewParser(bytes.NewReader(data))

	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if header.ImageWidth != 8 {
		t.Errorf("ImageWidth = %d, want 8", header.ImageWidth)
	}
	if header.ImageHeight != 8 {
		t.Errorf("ImageHeight = %d, want 8", header.ImageHeight)
	}
	if header.NumComponents != 1 {
		t.Errorf("NumComponents = %d, want 1", header.NumComponents)
	}
}

func TestParser_Header(t *testing.T) {
	parser := NewParser(bytes.NewReader(createMinimalCodestream()))
	parser.ReadHeader()
	h := parser.Header()
	if h == nil {
		t.Error("Header() returned nil")
	}
}

// Helper to create base codestream with SOC and SIZ
func createBaseCodestream(numComponents uint16) *bytes.Buffer {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(38+3*numComponents))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, numComponents)
	for i := uint16(0); i < numComponents; i++ {
		buf.WriteByte(7) // Ssiz
		buf.WriteByte(1) // XRsiz
		buf.WriteByte(1) // YRsiz
	}
	return &buf
}

// Helper to add COD marker
func addCOD(buf *bytes.Buffer, withPrecincts bool) {
	binary.Write(buf, binary.BigEndian, uint16(COD))
	if withPrecincts {
		binary.Write(buf, binary.BigEndian, uint16(14)) // Length with 2 precinct sizes
		buf.WriteByte(CodingStylePrecincts)             // Scod with precincts
	} else {
		binary.Write(buf, binary.BigEndian, uint16(12))
		buf.WriteByte(0) // Scod
	}
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Decomposition levels
	buf.WriteByte(4)                                // Code-block width
	buf.WriteByte(4)                                // Code-block height
	buf.WriteByte(0)                                // Code-block style
	buf.WriteByte(1)                                // Wavelet transform
	if withPrecincts {
		buf.WriteByte(0x55) // PPx=5, PPy=5
		buf.WriteByte(0x66) // PPx=6, PPy=6
	}
}

// Helper to add QCD marker with different styles
func addQCD(buf *bytes.Buffer, style uint8) {
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	switch style {
	case QuantizationNone:
		binary.Write(buf, binary.BigEndian, uint16(5)) // Length
		buf.WriteByte(0x40 | style)                    // Sqcd
		buf.WriteByte(0x48)                            // exponent
		buf.WriteByte(0x50)                            // exponent
	case QuantizationScalarDerived:
		binary.Write(buf, binary.BigEndian, uint16(5)) // Length
		buf.WriteByte(0x40 | style)                    // Sqcd
		binary.Write(buf, binary.BigEndian, uint16(0x4800))
	case QuantizationScalarExpounded:
		binary.Write(buf, binary.BigEndian, uint16(7)) // Length
		buf.WriteByte(0x40 | style)                    // Sqcd
		binary.Write(buf, binary.BigEndian, uint16(0x4800))
		binary.Write(buf, binary.BigEndian, uint16(0x5000))
	}
}

func TestParser_ReadCOC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COC marker
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9)) // Length
	buf.WriteByte(1)                                // Component index (1 byte for <257 components)
	buf.WriteByte(0)                                // Scoc
	buf.WriteByte(4)                                // NumDecompositions
	buf.WriteByte(3)                                // CodeBlockWidthExp
	buf.WriteByte(3)                                // CodeBlockHeightExp
	buf.WriteByte(0)                                // CodeBlockStyle
	buf.WriteByte(0)                                // WaveletTransform

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.ComponentCodingStyles) != 1 {
		t.Errorf("Expected 1 COC entry, got %d", len(header.ComponentCodingStyles))
	}
	coc, ok := header.ComponentCodingStyles[1]
	if !ok {
		t.Fatal("COC for component 1 not found")
	}
	if coc.NumDecompositions != 4 {
		t.Errorf("COC NumDecompositions = %d, want 4", coc.NumDecompositions)
	}
}

func TestParser_ReadCOCWithPrecincts(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COC marker with precincts
	// baseLen = 7 for <257 components (2 length + 1 comp + 1 scoc + 5 params = 9, but baseLen used is 7)
	// We need numPrecinct = length - baseLen = 9 - 7 = 2, so length = 9
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9)) // Length
	buf.WriteByte(2)                                // Component index
	buf.WriteByte(CodingStylePrecincts)             // Scoc with precincts
	buf.WriteByte(4)                                // NumDecompositions
	buf.WriteByte(3)                                // CodeBlockWidthExp
	buf.WriteByte(3)                                // CodeBlockHeightExp
	buf.WriteByte(0)                                // CodeBlockStyle
	buf.WriteByte(0)                                // WaveletTransform
	buf.WriteByte(0x44)                             // Precinct size 1
	buf.WriteByte(0x55)                             // Precinct size 2

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	coc := header.ComponentCodingStyles[2]
	if len(coc.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(coc.PrecinctSizes))
	}
}

func TestParser_ReadQCC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker with no quantization
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(6)) // Length
	buf.WriteByte(1)                                // Component index
	buf.WriteByte(0x40 | QuantizationNone)          // Sqcc
	buf.WriteByte(0x48)                             // exponent
	buf.WriteByte(0x50)                             // exponent

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.ComponentQuantization) != 1 {
		t.Errorf("Expected 1 QCC entry, got %d", len(header.ComponentQuantization))
	}
}

func TestParser_ReadQCCScalarDerived(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker with scalar derived
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(5)) // Length
	buf.WriteByte(0)                                // Component index
	buf.WriteByte(0x40 | QuantizationScalarDerived) // Sqcc
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	qcc := header.ComponentQuantization[0]
	if len(qcc.StepSizes) != 1 {
		t.Errorf("Expected 1 step size, got %d", len(qcc.StepSizes))
	}
}

func TestParser_ReadQCCScalarExpounded(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker with scalar expounded
	// For <257 components: headerBytes=3, then 1 byte sqcc, then step sizes
	// Length = headerBytes(3) + 1(sqcc) + 4(2 step sizes) = 8
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(8)) // Length
	buf.WriteByte(2)                                 // Component index
	buf.WriteByte(0x40 | QuantizationScalarExpounded) // Sqcc
	binary.Write(buf, binary.BigEndian, uint16(0x4800))
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	qcc := header.ComponentQuantization[2]
	if len(qcc.StepSizes) != 2 {
		t.Errorf("Expected 2 step sizes, got %d", len(qcc.StepSizes))
	}
}

func TestParser_ReadPOC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add POC marker
	// For <257 components: entrySize = 7, Length includes itself so Length = 2 + 7*entries
	binary.Write(buf, binary.BigEndian, uint16(POC))
	binary.Write(buf, binary.BigEndian, uint16(9)) // Length = 2 + 7*1
	// Entry 1
	buf.WriteByte(0)                                // ResolutionStart
	buf.WriteByte(0)                                // ComponentStart (1 byte for <257 components)
	binary.Write(buf, binary.BigEndian, uint16(1)) // LayerEnd
	buf.WriteByte(5)                                // ResolutionEnd
	buf.WriteByte(3)                                // ComponentEnd
	buf.WriteByte(uint8(RLCP))                      // ProgressionOrder

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.ProgressionOrderChanges) != 1 {
		t.Errorf("Expected 1 POC entry, got %d", len(header.ProgressionOrderChanges))
	}
	poc := header.ProgressionOrderChanges[0]
	if poc.ResolutionEnd != 5 {
		t.Errorf("POC ResolutionEnd = %d, want 5", poc.ResolutionEnd)
	}
}

func TestParser_ReadTLM(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add TLM marker
	// Length = 4 + entries * entrySize
	// ST=1 (1 byte tile index), SP=0 (2 byte length)
	binary.Write(buf, binary.BigEndian, uint16(TLM))
	binary.Write(buf, binary.BigEndian, uint16(7)) // Length = 4 + 3*1
	buf.WriteByte(0)                                // Ztlm
	buf.WriteByte(0x10)                             // Stlm: ST=1, SP=0
	// Entry
	buf.WriteByte(0)                                // Tile index
	binary.Write(buf, binary.BigEndian, uint16(100)) // Length

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.TileLengths) != 1 {
		t.Errorf("Expected 1 TLM entry, got %d", len(header.TileLengths))
	}
	if header.TileLengths[0].Length != 100 {
		t.Errorf("TileLengths[0].Length = %d, want 100", header.TileLengths[0].Length)
	}
}

func TestParser_ReadTLMWithDifferentSizes(t *testing.T) {
	// Test with ST=2 (2-byte tile index) and SP=1 (4-byte length)
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(TLM))
	binary.Write(buf, binary.BigEndian, uint16(10)) // Length = 4 + 6*1
	buf.WriteByte(0)                                 // Ztlm
	buf.WriteByte(0x60)                              // Stlm: ST=2, SP=1
	// Entry
	binary.Write(buf, binary.BigEndian, uint16(1))      // Tile index (2 bytes)
	binary.Write(buf, binary.BigEndian, uint32(100000)) // Length (4 bytes)

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.TileLengths) != 1 {
		t.Errorf("Expected 1 TLM entry, got %d", len(header.TileLengths))
	}
	if header.TileLengths[0].TileIndex != 1 {
		t.Errorf("TileIndex = %d, want 1", header.TileLengths[0].TileIndex)
	}
	if header.TileLengths[0].Length != 100000 {
		t.Errorf("Length = %d, want 100000", header.TileLengths[0].Length)
	}
}

func TestParser_ReadTLMImplicitIndex(t *testing.T) {
	// Test with ST=0 (no tile index)
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(TLM))
	binary.Write(buf, binary.BigEndian, uint16(8)) // Length = 4 + 2*2
	buf.WriteByte(0)                                // Ztlm
	buf.WriteByte(0x00)                             // Stlm: ST=0, SP=0
	// Two entries with implicit indexes
	binary.Write(buf, binary.BigEndian, uint16(50))
	binary.Write(buf, binary.BigEndian, uint16(60))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.TileLengths) != 2 {
		t.Errorf("Expected 2 TLM entries, got %d", len(header.TileLengths))
	}
	if header.TileLengths[0].TileIndex != 0 {
		t.Errorf("TileLengths[0].TileIndex = %d, want 0", header.TileLengths[0].TileIndex)
	}
	if header.TileLengths[1].TileIndex != 1 {
		t.Errorf("TileLengths[1].TileIndex = %d, want 1", header.TileLengths[1].TileIndex)
	}
}

func TestParser_ReadPLM(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add PLM marker with variable-length encoded values
	binary.Write(buf, binary.BigEndian, uint16(PLM))
	binary.Write(buf, binary.BigEndian, uint16(6)) // Length
	buf.WriteByte(0)                                // Zplm
	// Variable length encoded values: 0x05 (single byte), 0x81 0x00 (two bytes = 128)
	buf.WriteByte(0x05)
	buf.WriteByte(0x81)
	buf.WriteByte(0x00)

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.PacketLengths) != 2 {
		t.Errorf("Expected 2 packet lengths, got %d", len(header.PacketLengths))
	}
	if header.PacketLengths[0] != 5 {
		t.Errorf("PacketLengths[0] = %d, want 5", header.PacketLengths[0])
	}
	if header.PacketLengths[1] != 128 {
		t.Errorf("PacketLengths[1] = %d, want 128", header.PacketLengths[1])
	}
}

func TestParser_ReadPPM(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add PPM marker
	binary.Write(buf, binary.BigEndian, uint16(PPM))
	binary.Write(buf, binary.BigEndian, uint16(7)) // Length
	buf.WriteByte(0)                                // Zppm
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04})      // Packed data

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.PackedPacketHeaders) != 4 {
		t.Errorf("Expected 4 bytes of packed headers, got %d", len(header.PackedPacketHeaders))
	}
}

func TestParser_ReadCOM(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COM marker with Latin-1 text
	comment := "Test comment"
	binary.Write(buf, binary.BigEndian, uint16(COM))
	binary.Write(buf, binary.BigEndian, uint16(4+len(comment))) // Length
	binary.Write(buf, binary.BigEndian, uint16(CommentLatin1))  // Rcom
	buf.WriteString(comment)

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if header.Comment != comment {
		t.Errorf("Comment = %q, want %q", header.Comment, comment)
	}
	if header.CommentType != CommentLatin1 {
		t.Errorf("CommentType = %d, want %d", header.CommentType, CommentLatin1)
	}
}

func TestParser_ReadCOMBinary(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COM marker with binary data
	binary.Write(buf, binary.BigEndian, uint16(COM))
	binary.Write(buf, binary.BigEndian, uint16(8))              // Length
	binary.Write(buf, binary.BigEndian, uint16(CommentBinary))  // Rcom
	buf.Write([]byte{0x00, 0x01, 0x02, 0x03})

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	// Binary comments should not be stored as string
	if header.Comment != "" {
		t.Errorf("Binary comment should not be stored, got %q", header.Comment)
	}
	if header.CommentType != CommentBinary {
		t.Errorf("CommentType = %d, want %d", header.CommentType, CommentBinary)
	}
}

func TestParser_ReadCRG(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add CRG marker (component registration - we skip it)
	binary.Write(buf, binary.BigEndian, uint16(CRG))
	binary.Write(buf, binary.BigEndian, uint16(6)) // Length
	binary.Write(buf, binary.BigEndian, uint16(0)) // Xcrg
	binary.Write(buf, binary.BigEndian, uint16(0)) // Ycrg

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	// CRG is skipped, just verify we can parse past it
	if header == nil {
		t.Error("Header should not be nil")
	}
}

// Helper to create a codestream ending with a tile-part header
func createCodestreamWithTilePart() []byte {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))   // Lsot
	binary.Write(buf, binary.BigEndian, uint16(0))    // Isot (tile index)
	binary.Write(buf, binary.BigEndian, uint32(1000)) // Psot (tile-part length)
	buf.WriteByte(0)                                   // TPsot (tile-part index)
	buf.WriteByte(1)                                   // TNsot (number of tile-parts)

	// Add SOD marker (start of data)
	binary.Write(buf, binary.BigEndian, uint16(SOD))

	return buf.Bytes()
}

func TestParser_ReadTilePartHeader(t *testing.T) {
	data := createCodestreamWithTilePart()
	parser := NewParser(bytes.NewReader(data))

	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}
	if header == nil {
		t.Fatal("Header is nil")
	}

	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.TileIndex != 0 {
		t.Errorf("TileIndex = %d, want 0", tph.TileIndex)
	}
	if tph.TilePartLength != 1000 {
		t.Errorf("TilePartLength = %d, want 1000", tph.TilePartLength)
	}
	if tph.TilePartIndex != 0 {
		t.Errorf("TilePartIndex = %d, want 0", tph.TilePartIndex)
	}
	if tph.NumTileParts != 1 {
		t.Errorf("NumTileParts = %d, want 1", tph.NumTileParts)
	}
}

func TestParser_ReadTilePartHeaderWithCOD(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add tile-specific COD
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(1)                                // Progression order (different from default)
	binary.Write(buf, binary.BigEndian, uint16(2)) // Layers
	buf.WriteByte(1)                                // MCT
	buf.WriteByte(4)                                // Decomposition levels
	buf.WriteByte(3)                                // Code-block width
	buf.WriteByte(3)                                // Code-block height
	buf.WriteByte(0)                                // Code-block style
	buf.WriteByte(0)                                // Wavelet transform

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.CodingStyle == nil {
		t.Fatal("Tile-part CodingStyle should not be nil")
	}
	if tph.CodingStyle.ProgressionOrder != 1 {
		t.Errorf("Tile CodingStyle.ProgressionOrder = %d, want 1", tph.CodingStyle.ProgressionOrder)
	}
}

func TestParser_ReadTilePartHeaderWithQCD(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add tile-specific QCD
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x60 | QuantizationScalarDerived) // Different guard bits
	binary.Write(buf, binary.BigEndian, uint16(0x6000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.Quantization == nil {
		t.Fatal("Tile-part Quantization should not be nil")
	}
}

func TestParser_ReadTilePartHeaderWithCOC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add tile-specific COC
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	buf.WriteByte(3) // NumDecompositions
	buf.WriteByte(2) // CodeBlockWidthExp
	buf.WriteByte(2) // CodeBlockHeightExp
	buf.WriteByte(0) // CodeBlockStyle
	buf.WriteByte(1) // WaveletTransform

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if len(tph.ComponentCodingStyles) != 1 {
		t.Errorf("Expected 1 tile COC entry, got %d", len(tph.ComponentCodingStyles))
	}
}

func TestParser_ReadTilePartHeaderWithQCC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add tile-specific QCC
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(1)                                // Component index
	buf.WriteByte(0x40 | QuantizationScalarDerived) // Sqcc
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if len(tph.ComponentQuantization) != 1 {
		t.Errorf("Expected 1 tile QCC entry, got %d", len(tph.ComponentQuantization))
	}
}

func TestParser_ReadTilePartHeaderWithPOC(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add tile-specific POC
	binary.Write(buf, binary.BigEndian, uint16(POC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0)                                // ResolutionStart
	buf.WriteByte(0)                                // ComponentStart
	binary.Write(buf, binary.BigEndian, uint16(1)) // LayerEnd
	buf.WriteByte(3)                                // ResolutionEnd
	buf.WriteByte(3)                                // ComponentEnd
	buf.WriteByte(uint8(RPCL))                      // ProgressionOrder

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if len(tph.ProgressionOrderChanges) != 1 {
		t.Errorf("Expected 1 tile POC entry, got %d", len(tph.ProgressionOrderChanges))
	}
}

func TestParser_ReadTilePartHeaderWithPPT(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add SOT marker
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add PPT marker
	binary.Write(buf, binary.BigEndian, uint16(PPT))
	binary.Write(buf, binary.BigEndian, uint16(6))
	buf.WriteByte(0)                           // Zppt
	buf.Write([]byte{0xAA, 0xBB, 0xCC})       // Packed data

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if len(tph.PackedPacketHeaders) != 3 {
		t.Errorf("Expected 3 bytes of packed headers, got %d", len(tph.PackedPacketHeaders))
	}
}

// Tests for different quantization styles in QCD and tile QCD
func TestParser_ReadQCDNoQuantization(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationNone)
	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if header.Quantization.Style() != QuantizationNone {
		t.Errorf("Quantization.Style() = %d, want %d", header.Quantization.Style(), QuantizationNone)
	}
}

func TestParser_ReadQCDScalarExpounded(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarExpounded)
	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if header.Quantization.Style() != QuantizationScalarExpounded {
		t.Errorf("Quantization.Style() = %d, want %d", header.Quantization.Style(), QuantizationScalarExpounded)
	}
	if len(header.Quantization.StepSizes) != 2 {
		t.Errorf("Expected 2 step sizes, got %d", len(header.Quantization.StepSizes))
	}
}

// Test COD with precincts
func TestParser_ReadCODWithPrecincts(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, true)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.CodingStyle.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(header.CodingStyle.PrecinctSizes))
	}
}

// Test tile QCD with different styles
func TestParser_TileQCDNoQuantization(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCD with no quantization
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x40 | QuantizationNone)
	buf.WriteByte(0x48)
	buf.WriteByte(0x50)

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.Quantization == nil {
		t.Fatal("Tile Quantization should not be nil")
	}
	if tph.Quantization.Style() != QuantizationNone {
		t.Errorf("Tile Quantization.Style() = %d, want %d", tph.Quantization.Style(), QuantizationNone)
	}
}

func TestParser_TileQCDScalarExpounded(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCD with scalar expounded
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(7))
	buf.WriteByte(0x40 | QuantizationScalarExpounded)
	binary.Write(buf, binary.BigEndian, uint16(0x4800))
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.Quantization.Style() != QuantizationScalarExpounded {
		t.Errorf("Tile Quantization.Style() = %d, want %d", tph.Quantization.Style(), QuantizationScalarExpounded)
	}
}

// Test tile QCC with different styles
func TestParser_TileQCCNoQuantization(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCC with no quantization
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(6))
	buf.WriteByte(0)
	buf.WriteByte(0x40 | QuantizationNone)
	buf.WriteByte(0x48)
	buf.WriteByte(0x50)

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	qcc := tph.ComponentQuantization[0]
	if (qcc.QuantizationStyle & 0x1F) != QuantizationNone {
		t.Errorf("Tile QCC style = %d, want %d", qcc.QuantizationStyle&0x1F, QuantizationNone)
	}
}

func TestParser_TileQCCScalarExpounded(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCC with scalar expounded
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(8))
	buf.WriteByte(1)
	buf.WriteByte(0x40 | QuantizationScalarExpounded)
	binary.Write(buf, binary.BigEndian, uint16(0x4800))
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	qcc := tph.ComponentQuantization[1]
	if (qcc.QuantizationStyle & 0x1F) != QuantizationScalarExpounded {
		t.Errorf("Tile QCC style = %d, want %d", qcc.QuantizationStyle&0x1F, QuantizationScalarExpounded)
	}
}

// Test tile COD with precincts
func TestParser_TileCODWithPrecincts(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile COD with precincts
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(14)) // 12 + 2 precinct sizes
	buf.WriteByte(CodingStylePrecincts)
	buf.WriteByte(0)
	binary.Write(buf, binary.BigEndian, uint16(1))
	buf.WriteByte(0)
	buf.WriteByte(5)
	buf.WriteByte(4)
	buf.WriteByte(4)
	buf.WriteByte(0)
	buf.WriteByte(1)
	buf.WriteByte(0x55) // Precinct 1
	buf.WriteByte(0x66) // Precinct 2

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if tph.CodingStyle == nil {
		t.Fatal("Tile CodingStyle should not be nil")
	}
	if len(tph.CodingStyle.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(tph.CodingStyle.PrecinctSizes))
	}
}

// Test tile COC with precincts
func TestParser_TileCOCWithPrecincts(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile COC with precincts
	// baseLen=7 for <257 components, so numPrecinct = length - baseLen = 9 - 7 = 2
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9)) // baseLen=7 + 2 precincts
	buf.WriteByte(0)                                // Component index
	buf.WriteByte(CodingStylePrecincts)             // Scoc with precincts
	buf.WriteByte(3)                                // NumDecompositions
	buf.WriteByte(2)                                // CodeBlockWidthExp
	buf.WriteByte(2)                                // CodeBlockHeightExp
	buf.WriteByte(0)                                // CodeBlockStyle
	buf.WriteByte(1)                                // WaveletTransform
	buf.WriteByte(0x44)                             // Precinct size 1
	buf.WriteByte(0x55)                             // Precinct size 2

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	coc := tph.ComponentCodingStyles[0]
	if len(coc.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(coc.PrecinctSizes))
	}
}

// Error path tests
func TestParser_ReadHeader_NoSOC(t *testing.T) {
	data := []byte{0xFF, 0x00} // Invalid marker
	parser := NewParser(bytes.NewReader(data))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for missing SOC marker")
	}
}

func TestParser_ReadHeader_EmptyStream(t *testing.T) {
	parser := NewParser(bytes.NewReader([]byte{}))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for empty stream")
	}
}

func TestParser_ReadHeader_TruncatedSIZ(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41)) // Length
	// Missing rest of SIZ data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for truncated SIZ")
	}
}

func TestParser_ReadHeader_InvalidSIZLength(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(50)) // Wrong length for 1 component
	binary.Write(&buf, binary.BigEndian, uint16(0))
	binary.Write(&buf, binary.BigEndian, uint32(8))
	binary.Write(&buf, binary.BigEndian, uint32(8))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint32(8))
	binary.Write(&buf, binary.BigEndian, uint32(8))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint16(1))
	buf.WriteByte(7)
	buf.WriteByte(1)
	buf.WriteByte(1)

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for invalid SIZ length")
	}
}

func TestParser_ReadTilePartHeader_InvalidSOTLength(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(8)) // Invalid length (should be 10)
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(1000))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for invalid SOT length")
	}
}

func TestParser_ReadHeader_InvalidTLMST(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add TLM with invalid ST value (3)
	binary.Write(buf, binary.BigEndian, uint16(TLM))
	binary.Write(buf, binary.BigEndian, uint16(7))
	buf.WriteByte(0)
	buf.WriteByte(0x30) // ST=3 (invalid)
	buf.WriteByte(0)
	binary.Write(buf, binary.BigEndian, uint16(100))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for invalid TLM ST value")
	}
}

func TestParser_ReadHeader_UnknownMarker(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add unknown marker (should be skipped)
	binary.Write(buf, binary.BigEndian, uint16(0xFF99)) // Unknown marker
	binary.Write(buf, binary.BigEndian, uint16(4))      // Length
	buf.Write([]byte{0x00, 0x00})                       // Data

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}
	if header == nil {
		t.Error("Header should not be nil")
	}
}

func TestParser_ReadTilePartHeader_UnknownMarker(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add unknown marker in tile-part header
	binary.Write(buf, binary.BigEndian, uint16(0xFF99))
	binary.Write(buf, binary.BigEndian, uint16(4))
	buf.Write([]byte{0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}
	if tph == nil {
		t.Error("Tile-part header should not be nil")
	}
}

func TestParser_skipMarkerSegment_InvalidLength(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add marker with length < 2
	binary.Write(buf, binary.BigEndian, uint16(0xFF99))
	binary.Write(buf, binary.BigEndian, uint16(1)) // Invalid length

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for invalid marker segment length")
	}
}

// Test Header.Validate edge cases
func TestHeader_Validate_ZeroHeight(t *testing.T) {
	h := &Header{
		ImageWidth:  100,
		ImageHeight: 0,
		TileWidth:   100,
		TileHeight:  100,
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for zero height")
	}
}

func TestHeader_Validate_ZeroTileWidth(t *testing.T) {
	h := &Header{
		ImageWidth:  100,
		ImageHeight: 100,
		TileWidth:   0,
		TileHeight:  100,
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for zero tile width")
	}
}

func TestHeader_Validate_ZeroTileHeight(t *testing.T) {
	h := &Header{
		ImageWidth:  100,
		ImageHeight: 100,
		TileWidth:   100,
		TileHeight:  0,
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for zero tile height")
	}
}

func TestHeader_Validate_TooManyComponents(t *testing.T) {
	h := &Header{
		ImageWidth:    100,
		ImageHeight:   100,
		TileWidth:     100,
		TileHeight:    100,
		NumComponents: 16385, // > 16384
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for too many components")
	}
}

func TestHeader_Validate_InvalidPrecision(t *testing.T) {
	h := &Header{
		ImageWidth:    100,
		ImageHeight:   100,
		TileWidth:     100,
		TileHeight:    100,
		NumComponents: 1,
		ComponentInfo: []ComponentInfo{
			{BitDepth: 0x7F, SubsamplingX: 1, SubsamplingY: 1}, // Precision 128 > 38
		},
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for invalid precision")
	}
}

func TestHeader_Validate_ZeroSubsamplingY(t *testing.T) {
	h := &Header{
		ImageWidth:    100,
		ImageHeight:   100,
		TileWidth:     100,
		TileHeight:    100,
		NumComponents: 1,
		ComponentInfo: []ComponentInfo{
			{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 0},
		},
	}
	err := h.Validate()
	if err == nil {
		t.Error("Expected error for zero subsampling Y")
	}
}

// Tests for >256 component code paths
func createBaseCodestreamManyComponents(numComponents uint16) *bytes.Buffer {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(38+3*numComponents))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, numComponents)
	for i := uint16(0); i < numComponents; i++ {
		buf.WriteByte(7) // Ssiz
		buf.WriteByte(1) // XRsiz
		buf.WriteByte(1) // YRsiz
	}
	return &buf
}

func TestParser_ReadCOC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COC marker for >256 components (2-byte component index)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(10)) // Length (1 extra byte for 2-byte comp index)
	binary.Write(buf, binary.BigEndian, uint16(100)) // Component index (2 bytes for >=257 components)
	buf.WriteByte(0)                                 // Scoc
	buf.WriteByte(4)                                 // NumDecompositions
	buf.WriteByte(3)                                 // CodeBlockWidthExp
	buf.WriteByte(3)                                 // CodeBlockHeightExp
	buf.WriteByte(0)                                 // CodeBlockStyle
	buf.WriteByte(0)                                 // WaveletTransform

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	coc, ok := header.ComponentCodingStyles[100]
	if !ok {
		t.Fatal("COC for component 100 not found")
	}
	if coc.ComponentIndex != 100 {
		t.Errorf("COC ComponentIndex = %d, want 100", coc.ComponentIndex)
	}
}

func TestParser_ReadQCC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker for >256 components (2-byte component index)
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(6)) // Length (headerBytes=4 for >=257 comp)
	binary.Write(buf, binary.BigEndian, uint16(200)) // Component index (2 bytes)
	buf.WriteByte(0x40 | QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	qcc, ok := header.ComponentQuantization[200]
	if !ok {
		t.Fatal("QCC for component 200 not found")
	}
	if qcc.ComponentIndex != 200 {
		t.Errorf("QCC ComponentIndex = %d, want 200", qcc.ComponentIndex)
	}
}

func TestParser_ReadPOC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add POC marker for >256 components (entrySize=9)
	binary.Write(buf, binary.BigEndian, uint16(POC))
	binary.Write(buf, binary.BigEndian, uint16(11)) // Length = 2 + 9*1
	// Entry 1
	buf.WriteByte(0)                                  // ResolutionStart
	binary.Write(buf, binary.BigEndian, uint16(0))   // ComponentStart (2 bytes)
	binary.Write(buf, binary.BigEndian, uint16(1))   // LayerEnd
	buf.WriteByte(5)                                  // ResolutionEnd
	binary.Write(buf, binary.BigEndian, uint16(300)) // ComponentEnd (2 bytes)
	buf.WriteByte(uint8(RLCP))                        // ProgressionOrder

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	if len(header.ProgressionOrderChanges) != 1 {
		t.Errorf("Expected 1 POC entry, got %d", len(header.ProgressionOrderChanges))
	}
	poc := header.ProgressionOrderChanges[0]
	if poc.ComponentEnd != 300 {
		t.Errorf("POC ComponentEnd = %d, want 300", poc.ComponentEnd)
	}
}

// Test tile headers with many components
func TestParser_TileCOC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile COC for >256 components
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(10))   // Length (baseLen=8 for >=257)
	binary.Write(buf, binary.BigEndian, uint16(150)) // Component index
	buf.WriteByte(0)
	buf.WriteByte(3)
	buf.WriteByte(2)
	buf.WriteByte(2)
	buf.WriteByte(0)
	buf.WriteByte(1)

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	coc, ok := tph.ComponentCodingStyles[150]
	if !ok {
		t.Fatal("Tile COC for component 150 not found")
	}
	if coc.ComponentIndex != 150 {
		t.Errorf("Tile COC ComponentIndex = %d, want 150", coc.ComponentIndex)
	}
}

func TestParser_TileQCC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCC for >256 components
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(6))    // Length (headerBytes=4 for >=257)
	binary.Write(buf, binary.BigEndian, uint16(250))  // Component index
	buf.WriteByte(0x40 | QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	qcc, ok := tph.ComponentQuantization[250]
	if !ok {
		t.Fatal("Tile QCC for component 250 not found")
	}
	if qcc.ComponentIndex != 250 {
		t.Errorf("Tile QCC ComponentIndex = %d, want 250", qcc.ComponentIndex)
	}
}

func TestParser_TilePOC_ManyComponents(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile POC for >256 components (entrySize=9)
	binary.Write(buf, binary.BigEndian, uint16(POC))
	binary.Write(buf, binary.BigEndian, uint16(11)) // Length = 2 + 9*1
	buf.WriteByte(0)                                  // ResolutionStart
	binary.Write(buf, binary.BigEndian, uint16(0))   // ComponentStart
	binary.Write(buf, binary.BigEndian, uint16(1))   // LayerEnd
	buf.WriteByte(3)                                  // ResolutionEnd
	binary.Write(buf, binary.BigEndian, uint16(300)) // ComponentEnd
	buf.WriteByte(uint8(RPCL))                        // ProgressionOrder

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	if len(tph.ProgressionOrderChanges) != 1 {
		t.Errorf("Expected 1 tile POC entry, got %d", len(tph.ProgressionOrderChanges))
	}
	poc := tph.ProgressionOrderChanges[0]
	if poc.ComponentEnd != 300 {
		t.Errorf("Tile POC ComponentEnd = %d, want 300", poc.ComponentEnd)
	}
}

// Test COC/QCC with precincts for many components
func TestParser_ReadCOC_ManyComponentsWithPrecincts(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add COC marker for >256 components with precincts
	// baseLen=8 for >=257 components, so numPrecinct = length - baseLen = 10 - 8 = 2
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(10))  // Length
	binary.Write(buf, binary.BigEndian, uint16(100)) // Component index (2 bytes)
	buf.WriteByte(CodingStylePrecincts)              // Scoc with precincts
	buf.WriteByte(4)                                 // NumDecompositions
	buf.WriteByte(3)                                 // CodeBlockWidthExp
	buf.WriteByte(3)                                 // CodeBlockHeightExp
	buf.WriteByte(0)                                 // CodeBlockStyle
	buf.WriteByte(0)                                 // WaveletTransform
	buf.WriteByte(0x44)                              // Precinct size 1
	buf.WriteByte(0x55)                              // Precinct size 2

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	coc := header.ComponentCodingStyles[100]
	if len(coc.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(coc.PrecinctSizes))
	}
}

func TestParser_ReadQCC_ManyComponentsNoQuantization(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker for >256 components with no quantization
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(7)) // Length
	binary.Write(buf, binary.BigEndian, uint16(200)) // Component index (2 bytes)
	buf.WriteByte(0x40 | QuantizationNone)
	buf.WriteByte(0x48)
	buf.WriteByte(0x50)

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	qcc := header.ComponentQuantization[200]
	if (qcc.QuantizationStyle & 0x1F) != QuantizationNone {
		t.Errorf("QCC style = %d, want %d", qcc.QuantizationStyle&0x1F, QuantizationNone)
	}
}

func TestParser_ReadQCC_ManyComponentsExpounded(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add QCC marker for >256 components with scalar expounded
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(9)) // Length = headerBytes(4) + 1(sqcc) + 4(2 step sizes)
	binary.Write(buf, binary.BigEndian, uint16(200)) // Component index (2 bytes)
	buf.WriteByte(0x40 | QuantizationScalarExpounded)
	binary.Write(buf, binary.BigEndian, uint16(0x4800))
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	header, err := parser.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader() error: %v", err)
	}

	qcc := header.ComponentQuantization[200]
	if len(qcc.StepSizes) != 2 {
		t.Errorf("Expected 2 step sizes, got %d", len(qcc.StepSizes))
	}
}

// Test tile QCC with different quantization for many components
func TestParser_TileQCC_ManyComponentsNoQuant(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCC with no quantization for >256 components
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(7))
	binary.Write(buf, binary.BigEndian, uint16(250))
	buf.WriteByte(0x40 | QuantizationNone)
	buf.WriteByte(0x48)
	buf.WriteByte(0x50)

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	qcc := tph.ComponentQuantization[250]
	if (qcc.QuantizationStyle & 0x1F) != QuantizationNone {
		t.Errorf("Tile QCC style = %d, want %d", qcc.QuantizationStyle&0x1F, QuantizationNone)
	}
}

func TestParser_TileQCC_ManyComponentsExpounded(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile QCC with scalar expounded for >256 components
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	binary.Write(buf, binary.BigEndian, uint16(250))
	buf.WriteByte(0x40 | QuantizationScalarExpounded)
	binary.Write(buf, binary.BigEndian, uint16(0x4800))
	binary.Write(buf, binary.BigEndian, uint16(0x5000))

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	qcc := tph.ComponentQuantization[250]
	if len(qcc.StepSizes) != 2 {
		t.Errorf("Expected 2 step sizes, got %d", len(qcc.StepSizes))
	}
}

// Test tile COC with precincts for many components
func TestParser_TileCOC_ManyComponentsWithPrecincts(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Tile COC for >256 components with precincts
	// baseLen=8 for >=257 components, so numPrecinct = length - baseLen = 10 - 8 = 2
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(150))
	buf.WriteByte(CodingStylePrecincts)
	buf.WriteByte(3)
	buf.WriteByte(2)
	buf.WriteByte(2)
	buf.WriteByte(0)
	buf.WriteByte(1)
	buf.WriteByte(0x44) // Precinct size 1
	buf.WriteByte(0x55) // Precinct size 2

	binary.Write(buf, binary.BigEndian, uint16(SOD))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	tph, err := parser.ReadTilePartHeader()
	if err != nil {
		t.Fatalf("ReadTilePartHeader() error: %v", err)
	}

	coc := tph.ComponentCodingStyles[150]
	if len(coc.PrecinctSizes) != 2 {
		t.Errorf("Expected 2 precinct sizes, got %d", len(coc.PrecinctSizes))
	}
}

// I/O error simulation tests
func TestParser_ReadHeader_IOError_SIZ(t *testing.T) {
	// Create valid codestream and introduce error at different points in SIZ
	data := createMinimalCodestream()
	// Error after SOC marker and SIZ marker, during SIZ data
	parser := NewParser(newErrorReader(data, 10))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error during SIZ reading")
	}
}

func TestParser_ReadHeader_IOError_COD(t *testing.T) {
	data := createMinimalCodestream()
	// Error during COD marker reading (after SIZ completes)
	parser := NewParser(newErrorReader(data, 50))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error during COD reading")
	}
}

func TestParser_ReadHeader_IOError_QCD(t *testing.T) {
	data := createMinimalCodestream()
	// Error during QCD marker reading - need to adjust position
	parser := NewParser(newErrorReader(data, 65))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error during QCD reading")
	}
}

func TestParser_ReadTilePartHeader_IOError(t *testing.T) {
	data := createCodestreamWithTilePart()
	// Let ReadHeader succeed, then error during tile part header reading
	parser := NewParser(newErrorReader(data, len(data)-5))
	_, err := parser.ReadHeader()
	if err != nil {
		// May fail at different points, that's ok
		return
	}
	_, err = parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error during tile part header reading")
	}
}

func TestParser_ReadCOC_IOError(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial COC marker (missing data)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	// Missing COC data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC")
	}
}

func TestParser_ReadQCC_IOError(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial QCC marker (missing data)
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	binary.Write(buf, binary.BigEndian, uint16(6))
	// Missing QCC data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCC")
	}
}

func TestParser_ReadPOC_IOError(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial POC marker
	binary.Write(buf, binary.BigEndian, uint16(POC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Just one byte, missing the rest

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete POC")
	}
}

func TestParser_ReadTLM_IOError(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial TLM marker
	binary.Write(buf, binary.BigEndian, uint16(TLM))
	binary.Write(buf, binary.BigEndian, uint16(7))
	// Missing TLM data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete TLM")
	}
}

func TestParser_ReadPLM_IOError(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial PLM marker
	binary.Write(buf, binary.BigEndian, uint16(PLM))
	binary.Write(buf, binary.BigEndian, uint16(6))
	// Missing PLM data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete PLM")
	}
}

func TestParser_ReadPPM_IOError(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial PPM marker
	binary.Write(buf, binary.BigEndian, uint16(PPM))
	binary.Write(buf, binary.BigEndian, uint16(7))
	// Missing PPM data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete PPM")
	}
}

func TestParser_ReadCOM_IOError(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add partial COM marker
	binary.Write(buf, binary.BigEndian, uint16(COM))
	binary.Write(buf, binary.BigEndian, uint16(10))
	// Missing COM data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COM")
	}
}

func TestParser_TilePPT_IOError(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add partial PPT marker
	binary.Write(buf, binary.BigEndian, uint16(PPT))
	binary.Write(buf, binary.BigEndian, uint16(6))
	// Missing PPT data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete PPT")
	}
}

// Additional error path tests for improved coverage

// Test readSIZ error at expectMarker
func TestParser_readSIZ_ExpectMarkerError(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(COD)) // Wrong marker, should be SIZ

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for missing SIZ marker")
	}
}

// Test Header.Validate with invalid header that fails validation during ReadHeader
func TestParser_ReadHeader_ValidationFails(t *testing.T) {
	// Create a codestream with zero image width (invalid)
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41)) // Length for 1 component
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // Xsiz = 0 (invalid!)
	binary.Write(&buf, binary.BigEndian, uint32(8))  // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(8))  // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, uint16(1))  // Csiz
	buf.WriteByte(7)                                 // Ssiz
	buf.WriteByte(1)                                 // XRsiz
	buf.WriteByte(1)                                 // YRsiz

	// Add COD
	binary.Write(&buf, binary.BigEndian, uint16(COD))
	binary.Write(&buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)
	buf.WriteByte(0)
	binary.Write(&buf, binary.BigEndian, uint16(1))
	buf.WriteByte(0)
	buf.WriteByte(5)
	buf.WriteByte(4)
	buf.WriteByte(4)
	buf.WriteByte(0)
	buf.WriteByte(1)

	// Add QCD
	binary.Write(&buf, binary.BigEndian, uint16(QCD))
	binary.Write(&buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x41)
	binary.Write(&buf, binary.BigEndian, uint16(0x4000))

	// Add SOT
	binary.Write(&buf, binary.BigEndian, uint16(SOT))

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for invalid header (zero image width)")
	}
}

// Test error during readMarker in main loop of ReadHeader
func TestParser_ReadHeader_ErrorReadingMarker(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	// Don't add SOT - stream ends abruptly

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error when marker read fails")
	}
}

// Test CRG marker error path
func TestParser_ReadCRG_Error(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add CRG marker with insufficient data
	binary.Write(buf, binary.BigEndian, uint16(CRG))
	binary.Write(buf, binary.BigEndian, uint16(10)) // Length claiming 10 bytes
	// But don't provide the data - EOF will occur

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete CRG marker")
	}
}

// Test readBytes error path
func TestParser_readBytes_Error(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add PPM marker that claims more data than available
	binary.Write(buf, binary.BigEndian, uint16(PPM))
	binary.Write(buf, binary.BigEndian, uint16(100)) // Length claiming 100 bytes
	buf.WriteByte(0)                                  // Zppm
	// But only provide a few more bytes - not the full 97 bytes needed

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error when readBytes fails")
	}
}

// Test skipMarkerSegment length read error
func TestParser_skipMarkerSegment_LengthReadError(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)

	// Add unknown marker without length field (stream ends after marker)
	binary.Write(buf, binary.BigEndian, uint16(0xFF99))
	// No length field - EOF

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error when skipMarkerSegment cannot read length")
	}
}

// Test readSIZ error paths at different read points
func TestParser_readSIZ_ErrorReadingProfile(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	// Missing profile data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (profile)")
	}
}

func TestParser_readSIZ_ErrorReadingImageWidth(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0)) // Rsiz
	// Missing image width

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (image width)")
	}
}

func TestParser_readSIZ_ErrorReadingImageHeight(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	// Missing image height

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (image height)")
	}
}

func TestParser_readSIZ_ErrorReadingImageXOffset(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	// Missing image X offset

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (image X offset)")
	}
}

func TestParser_readSIZ_ErrorReadingImageYOffset(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	// Missing image Y offset

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (image Y offset)")
	}
}

func TestParser_readSIZ_ErrorReadingTileWidth(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	// Missing tile width

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (tile width)")
	}
}

func TestParser_readSIZ_ErrorReadingTileHeight(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	// Missing tile height

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (tile height)")
	}
}

func TestParser_readSIZ_ErrorReadingTileXOffset(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	// Missing tile X offset

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (tile X offset)")
	}
}

func TestParser_readSIZ_ErrorReadingTileYOffset(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	// Missing tile Y offset

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (tile Y offset)")
	}
}

func TestParser_readSIZ_ErrorReadingNumComponents(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	// Missing num components

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (num components)")
	}
}

func TestParser_readSIZ_ErrorReadingComponentBitDepth(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, uint16(1))  // Csiz
	// Missing component info (Ssiz)

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (component bit depth)")
	}
}

func TestParser_readSIZ_ErrorReadingComponentSubsamplingX(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, uint16(1))  // Csiz
	buf.WriteByte(7)                                 // Ssiz
	// Missing XRsiz

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (component subsampling X)")
	}
}

func TestParser_readSIZ_ErrorReadingComponentSubsamplingY(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint16(SOC))
	binary.Write(&buf, binary.BigEndian, uint16(SIZ))
	binary.Write(&buf, binary.BigEndian, uint16(41))
	binary.Write(&buf, binary.BigEndian, uint16(0))  // Rsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Xsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // Ysiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YOsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // XTsiz
	binary.Write(&buf, binary.BigEndian, uint32(64)) // YTsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // XTOsiz
	binary.Write(&buf, binary.BigEndian, uint32(0))  // YTOsiz
	binary.Write(&buf, binary.BigEndian, uint16(1))  // Csiz
	buf.WriteByte(7)                                 // Ssiz
	buf.WriteByte(1)                                 // XRsiz
	// Missing YRsiz

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete SIZ (component subsampling Y)")
	}
}

// Test readCOD error paths
func TestParser_readCOD_ErrorReadingLength(t *testing.T) {
	buf := createBaseCodestream(1)
	// Add COD marker without length
	binary.Write(buf, binary.BigEndian, uint16(COD))
	// Missing length

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (length)")
	}
}

func TestParser_readCOD_ErrorReadingScod(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	// Missing Scod

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (Scod)")
	}
}

func TestParser_readCOD_ErrorReadingProgressionOrder(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0) // Scod
	// Missing progression order

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (progression order)")
	}
}

func TestParser_readCOD_ErrorReadingNumLayers(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0) // Scod
	buf.WriteByte(0) // Progression order
	// Missing num layers

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (num layers)")
	}
}

func TestParser_readCOD_ErrorReadingMCT(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	// Missing MCT

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (MCT)")
	}
}

func TestParser_readCOD_ErrorReadingNumDecomp(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	buf.WriteByte(0)                                // MCT
	// Missing num decomposition levels

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (num decomp)")
	}
}

func TestParser_readCOD_ErrorReadingCodeBlockWidth(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Num decomposition levels
	// Missing code block width

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (code block width)")
	}
}

func TestParser_readCOD_ErrorReadingCodeBlockHeight(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Num decomposition levels
	buf.WriteByte(4)                                // Code block width
	// Missing code block height

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (code block height)")
	}
}

func TestParser_readCOD_ErrorReadingCodeBlockStyle(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Num decomposition levels
	buf.WriteByte(4)                                // Code block width
	buf.WriteByte(4)                                // Code block height
	// Missing code block style

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (code block style)")
	}
}

func TestParser_readCOD_ErrorReadingWavelet(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.WriteByte(0)                                // Scod
	buf.WriteByte(0)                                // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1)) // Num layers
	buf.WriteByte(0)                                // MCT
	buf.WriteByte(5)                                // Num decomposition levels
	buf.WriteByte(4)                                // Code block width
	buf.WriteByte(4)                                // Code block height
	buf.WriteByte(0)                                // Code block style
	// Missing wavelet transform

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (wavelet)")
	}
}

func TestParser_readCOD_ErrorReadingPrecincts(t *testing.T) {
	buf := createBaseCodestream(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	binary.Write(buf, binary.BigEndian, uint16(14))  // Length with 2 precinct sizes
	buf.WriteByte(CodingStylePrecincts)              // Scod with precincts
	buf.WriteByte(0)                                 // Progression order
	binary.Write(buf, binary.BigEndian, uint16(1))  // Num layers
	buf.WriteByte(0)                                 // MCT
	buf.WriteByte(5)                                 // Num decomposition levels
	buf.WriteByte(4)                                 // Code block width
	buf.WriteByte(4)                                 // Code block height
	buf.WriteByte(0)                                 // Code block style
	buf.WriteByte(1)                                 // Wavelet transform
	// Missing precinct sizes

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COD (precincts)")
	}
}

// Test readCOC error paths
func TestParser_readCOC_ErrorReadingLength(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	// Missing length

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (length)")
	}
}

func TestParser_readCOC_ErrorReadingComponentIndex(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	// Missing component index

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (component index)")
	}
}

func TestParser_readCOC_ManyComponents_ErrorReadingComponentIndex(t *testing.T) {
	buf := createBaseCodestreamManyComponents(300)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(10))
	// Missing component index (2 bytes for >=257 components)

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (component index for many components)")
	}
}

func TestParser_readCOC_ErrorReadingScoc(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	// Missing Scoc

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (Scoc)")
	}
}

func TestParser_readCOC_ErrorReadingNumDecomp(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	// Missing num decomp

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (num decomp)")
	}
}

func TestParser_readCOC_ErrorReadingCodeBlockWidth(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	buf.WriteByte(4) // Num decomp
	// Missing code block width

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (code block width)")
	}
}

func TestParser_readCOC_ErrorReadingCodeBlockHeight(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	buf.WriteByte(4) // Num decomp
	buf.WriteByte(3) // Code block width
	// Missing code block height

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (code block height)")
	}
}

func TestParser_readCOC_ErrorReadingCodeBlockStyle(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	buf.WriteByte(4) // Num decomp
	buf.WriteByte(3) // Code block width
	buf.WriteByte(3) // Code block height
	// Missing code block style

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (code block style)")
	}
}

func TestParser_readCOC_ErrorReadingWavelet(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0) // Component index
	buf.WriteByte(0) // Scoc
	buf.WriteByte(4) // Num decomp
	buf.WriteByte(3) // Code block width
	buf.WriteByte(3) // Code block height
	buf.WriteByte(0) // Code block style
	// Missing wavelet

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (wavelet)")
	}
}

func TestParser_readCOC_ErrorReadingPrecincts(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	binary.Write(buf, binary.BigEndian, uint16(9))
	buf.WriteByte(0)                    // Component index
	buf.WriteByte(CodingStylePrecincts) // Scoc with precincts
	buf.WriteByte(4)                    // Num decomp
	buf.WriteByte(3)                    // Code block width
	buf.WriteByte(3)                    // Code block height
	buf.WriteByte(0)                    // Code block style
	buf.WriteByte(0)                    // Wavelet
	// Missing precinct sizes

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete COC (precincts)")
	}
}

// Test readQCD error paths
func TestParser_readQCD_ErrorReadingLength(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	// Missing length

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCD (length)")
	}
}

func TestParser_readQCD_ErrorReadingSqcd(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(5))
	// Missing Sqcd

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCD (Sqcd)")
	}
}

func TestParser_readQCD_NoQuant_ErrorReadingExponent(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x40 | QuantizationNone)
	// Missing exponent bytes

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCD (exponent)")
	}
}

func TestParser_readQCD_ScalarDerived_ErrorReadingStepSize(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x40 | QuantizationScalarDerived)
	// Missing step size

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCD (scalar derived step size)")
	}
}

func TestParser_readQCD_ScalarExpounded_ErrorReadingStepSizes(t *testing.T) {
	buf := createBaseCodestream(1)
	addCOD(buf, false)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	binary.Write(buf, binary.BigEndian, uint16(7))
	buf.WriteByte(0x40 | QuantizationScalarExpounded)
	// Missing step sizes

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	_, err := parser.ReadHeader()
	if err == nil {
		t.Error("Expected error for incomplete QCD (scalar expounded step sizes)")
	}
}

// Test ReadTilePartHeader error paths
func TestParser_ReadTilePartHeader_ErrorReadingTileIndex(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	// Missing tile index

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete SOT (tile index)")
	}
}

func TestParser_ReadTilePartHeader_ErrorReadingTilePartLength(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0)) // Tile index
	// Missing tile-part length

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete SOT (tile-part length)")
	}
}

func TestParser_ReadTilePartHeader_ErrorReadingTilePartIndex(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))    // Tile index
	binary.Write(buf, binary.BigEndian, uint32(1000)) // Tile-part length
	// Missing tile-part index

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete SOT (tile-part index)")
	}
}

func TestParser_ReadTilePartHeader_ErrorReadingNumTileParts(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))    // Tile index
	binary.Write(buf, binary.BigEndian, uint32(1000)) // Tile-part length
	buf.WriteByte(0)                                   // Tile-part index
	// Missing num tile-parts

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete SOT (num tile-parts)")
	}
}

func TestParser_ReadTilePartHeader_ErrorReadingMarker(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(1000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	// Missing marker after SOT data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile-part header (marker)")
	}
}

// Test tile-part header marker error paths
func TestParser_ReadTilePartHeader_COD_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(COD))
	// Missing COD data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile COD")
	}
}

func TestParser_ReadTilePartHeader_COC_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(COC))
	// Missing COC data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile COC")
	}
}

func TestParser_ReadTilePartHeader_QCD_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(QCD))
	// Missing QCD data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile QCD")
	}
}

func TestParser_ReadTilePartHeader_QCC_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(QCC))
	// Missing QCC data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile QCC")
	}
}

func TestParser_ReadTilePartHeader_POC_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(POC))
	// Missing POC data

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error for incomplete tile POC")
	}
}

func TestParser_ReadTilePartHeader_SkipUnknown_Error(t *testing.T) {
	buf := createBaseCodestream(3)
	addCOD(buf, false)
	addQCD(buf, QuantizationScalarDerived)
	binary.Write(buf, binary.BigEndian, uint16(SOT))
	binary.Write(buf, binary.BigEndian, uint16(10))
	binary.Write(buf, binary.BigEndian, uint16(0))
	binary.Write(buf, binary.BigEndian, uint32(2000))
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.BigEndian, uint16(0xFF99)) // Unknown marker
	binary.Write(buf, binary.BigEndian, uint16(100))    // Length claiming 100 bytes
	// Missing data to skip

	parser := NewParser(bytes.NewReader(buf.Bytes()))
	parser.ReadHeader()
	_, err := parser.ReadTilePartHeader()
	if err == nil {
		t.Error("Expected error when skipping unknown marker in tile-part header")
	}
}
