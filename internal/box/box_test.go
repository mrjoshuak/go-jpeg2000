package box

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestType_String(t *testing.T) {
	tests := []struct {
		typ  Type
		want string
	}{
		{TypeJP2Signature, "jP  "},
		{TypeFileType, "ftyp"},
		{TypeJP2Header, "jp2h"},
		{TypeImageHeader, "ihdr"},
		{TypeColorSpec, "colr"},
		{TypeContCodestream, "jp2c"},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("Type(%08X).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestBox_Header(t *testing.T) {
	// Normal length box
	b := &Box{
		Type:     TypeFileType,
		Length:   20,
		Contents: make([]byte, 12),
	}
	header := b.Header()
	if len(header) != 8 {
		t.Errorf("Header length = %d, want 8", len(header))
	}

	length := binary.BigEndian.Uint32(header[0:4])
	if length != 20 {
		t.Errorf("Header length field = %d, want 20", length)
	}

	typ := Type(binary.BigEndian.Uint32(header[4:8]))
	if typ != TypeFileType {
		t.Errorf("Header type = %v, want %v", typ, TypeFileType)
	}
}

func TestBox_Header_Extended(t *testing.T) {
	// Extended length box
	b := &Box{
		Type:     TypeContCodestream,
		Length:   0x100000001, // > 32-bit
		Contents: make([]byte, 100),
	}
	header := b.Header()
	if len(header) != 16 {
		t.Errorf("Extended header length = %d, want 16", len(header))
	}

	length := binary.BigEndian.Uint32(header[0:4])
	if length != 1 {
		t.Errorf("Extended header length field = %d, want 1", length)
	}
}

func TestBox_Bytes(t *testing.T) {
	b := &Box{
		Type:     TypeFileType,
		Length:   12,
		Contents: []byte{0x01, 0x02, 0x03, 0x04},
	}
	data := b.Bytes()
	if len(data) != 12 {
		t.Errorf("Bytes length = %d, want 12", len(data))
	}
}

func TestReader_ReadBox(t *testing.T) {
	// Create a simple box
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(12)) // Length
	binary.Write(&buf, binary.BigEndian, uint32(TypeFileType))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) // Contents

	r := NewReader(&buf)
	box, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() error: %v", err)
	}

	if box.Type != TypeFileType {
		t.Errorf("Type = %v, want %v", box.Type, TypeFileType)
	}
	if box.Length != 12 {
		t.Errorf("Length = %d, want 12", box.Length)
	}
	if len(box.Contents) != 4 {
		t.Errorf("Contents length = %d, want 4", len(box.Contents))
	}
}

func TestReader_ReadBox_Extended(t *testing.T) {
	// Create an extended length box
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1)) // Extended length marker
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))
	binary.Write(&buf, binary.BigEndian, uint64(24)) // Extended length
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	r := NewReader(&buf)
	box, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() error: %v", err)
	}

	if box.Type != TypeContCodestream {
		t.Errorf("Type = %v, want %v", box.Type, TypeContCodestream)
	}
	if box.Length != 24 {
		t.Errorf("Length = %d, want 24", box.Length)
	}
}

func TestReader_ReadBox_EOF(t *testing.T) {
	r := NewReader(bytes.NewReader(nil))
	_, err := r.ReadBox()
	if err != io.EOF {
		t.Errorf("ReadBox() error = %v, want EOF", err)
	}
}

func TestWriter_WriteBox(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	box := &Box{
		Type:     TypeFileType,
		Length:   16,
		Contents: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	err := w.WriteBox(box)
	if err != nil {
		t.Fatalf("WriteBox() error: %v", err)
	}

	if buf.Len() != 16 {
		t.Errorf("Written length = %d, want 16", buf.Len())
	}
}

func TestWriter_WriteSignature(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	err := w.WriteSignature()
	if err != nil {
		t.Fatalf("WriteSignature() error: %v", err)
	}

	data := buf.Bytes()
	if len(data) != 12 {
		t.Fatalf("Signature length = %d, want 12", len(data))
	}

	// Check signature bytes
	expected := []byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}
	if !bytes.Equal(data, expected) {
		t.Errorf("Signature = %v, want %v", data, expected)
	}
}

func TestImageHeaderBox_Parse(t *testing.T) {
	data := make([]byte, 14)
	binary.BigEndian.PutUint32(data[0:4], 100)  // Height
	binary.BigEndian.PutUint32(data[4:8], 200)  // Width
	binary.BigEndian.PutUint16(data[8:10], 3)   // Components
	data[10] = 7                                 // BPC
	data[11] = 7                                 // Compression
	data[12] = 0                                 // Unknown colorspace
	data[13] = 0                                 // IPR

	b := &ImageHeaderBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if b.Height != 100 {
		t.Errorf("Height = %d, want 100", b.Height)
	}
	if b.Width != 200 {
		t.Errorf("Width = %d, want 200", b.Width)
	}
	if b.NumComponents != 3 {
		t.Errorf("NumComponents = %d, want 3", b.NumComponents)
	}
}

func TestImageHeaderBox_Bytes(t *testing.T) {
	b := &ImageHeaderBox{
		Height:           100,
		Width:            200,
		NumComponents:    3,
		BitsPerComponent: 7,
		CompressionType:  7,
	}
	data := b.Bytes()

	if len(data) != 14 {
		t.Fatalf("Bytes length = %d, want 14", len(data))
	}

	height := binary.BigEndian.Uint32(data[0:4])
	if height != 100 {
		t.Errorf("Height = %d, want 100", height)
	}
}

func TestColorSpecBox_Parse_Enumerated(t *testing.T) {
	data := make([]byte, 7)
	data[0] = 1                                    // Method = enumerated
	data[1] = 0                                    // Precedence
	data[2] = 0                                    // Approximation
	binary.BigEndian.PutUint32(data[3:7], CSSRGB) // sRGB

	b := &ColorSpecBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if b.Method != 1 {
		t.Errorf("Method = %d, want 1", b.Method)
	}
	if b.EnumeratedColorspace != CSSRGB {
		t.Errorf("EnumeratedColorspace = %d, want %d", b.EnumeratedColorspace, CSSRGB)
	}
}

func TestColorSpecBox_Parse_ICC(t *testing.T) {
	profile := []byte{0x01, 0x02, 0x03, 0x04}
	data := make([]byte, 3+len(profile))
	data[0] = 2 // Method = ICC
	data[1] = 0
	data[2] = 0
	copy(data[3:], profile)

	b := &ColorSpecBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if b.Method != 2 {
		t.Errorf("Method = %d, want 2", b.Method)
	}
	if !bytes.Equal(b.ICCProfile, profile) {
		t.Errorf("ICCProfile = %v, want %v", b.ICCProfile, profile)
	}
}

func TestColorSpecBox_Bytes(t *testing.T) {
	b := &ColorSpecBox{
		Method:             1,
		EnumeratedColorspace: CSSRGB,
	}
	data := b.Bytes()

	if len(data) != 7 {
		t.Fatalf("Bytes length = %d, want 7", len(data))
	}
	if data[0] != 1 {
		t.Errorf("Method = %d, want 1", data[0])
	}
}

func TestFileTypeBox_Parse(t *testing.T) {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[0:4], uint32(0x6A703220)) // "jp2 "
	binary.BigEndian.PutUint32(data[4:8], 0)                  // Minor version
	binary.BigEndian.PutUint32(data[8:12], uint32(0x6A703220)) // Compatibility

	b := &FileTypeBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if b.Brand != 0x6A703220 {
		t.Errorf("Brand = %08X, want %08X", b.Brand, 0x6A703220)
	}
	if len(b.Compatibility) != 1 {
		t.Errorf("Compatibility count = %d, want 1", len(b.Compatibility))
	}
}

func TestFileTypeBox_Bytes(t *testing.T) {
	b := &FileTypeBox{
		Brand:        0x6A703220,
		MinorVersion: 0,
		Compatibility: []Type{0x6A703220},
	}
	data := b.Bytes()

	if len(data) != 12 {
		t.Fatalf("Bytes length = %d, want 12", len(data))
	}
}

func TestCreateJP2Header(t *testing.T) {
	box := CreateJP2Header(100, 200, 3, 7, CSSRGB)

	if box.Type != TypeJP2Header {
		t.Errorf("Type = %v, want %v", box.Type, TypeJP2Header)
	}

	// Should contain ihdr and colr boxes
	if len(box.Contents) < 20 {
		t.Error("JP2 header too short")
	}
}

func TestCreateFileTypeBox(t *testing.T) {
	box := CreateFileTypeBox()

	if box.Type != TypeFileType {
		t.Errorf("Type = %v, want %v", box.Type, TypeFileType)
	}
}

func TestCreateCodestreamBox(t *testing.T) {
	codestream := []byte{0xFF, 0x4F, 0xFF, 0xD9} // SOC + EOC
	box := CreateCodestreamBox(codestream)

	if box.Type != TypeContCodestream {
		t.Errorf("Type = %v, want %v", box.Type, TypeContCodestream)
	}
	if !bytes.Equal(box.Contents, codestream) {
		t.Error("Contents mismatch")
	}
}

func TestColorspaceConstants(t *testing.T) {
	// Verify colorspace constants match JP2 spec
	if CSGray != 17 {
		t.Errorf("CSGray = %d, want 17", CSGray)
	}
	if CSSRGB != 16 {
		t.Errorf("CSSRGB = %d, want 16", CSSRGB)
	}
	if CSYCbCr1 != 1 {
		t.Errorf("CSYCbCr1 = %d, want 1", CSYCbCr1)
	}
}

func BenchmarkReader_ReadBox(b *testing.B) {
	// Create a box to read
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1024))
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))
	buf.Write(make([]byte, 1016))
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(data))
		r.ReadBox()
	}
}

func BenchmarkWriter_WriteBox(b *testing.B) {
	box := &Box{
		Type:     TypeContCodestream,
		Length:   1024,
		Contents: make([]byte, 1016),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := NewWriter(&buf)
		w.WriteBox(box)
	}
}

// Additional tests for improved coverage

func TestReader_Offset(t *testing.T) {
	// Create a simple box
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(12)) // Length
	binary.Write(&buf, binary.BigEndian, uint32(TypeFileType))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) // Contents

	r := NewReader(&buf)

	// Initially offset should be 0
	if r.Offset() != 0 {
		t.Errorf("Initial Offset() = %d, want 0", r.Offset())
	}

	_, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() error: %v", err)
	}

	// After reading a 12-byte box, offset should be 12
	if r.Offset() != 12 {
		t.Errorf("Offset() after read = %d, want 12", r.Offset())
	}
}

func TestReader_ReadBox_TruncatedHeader(t *testing.T) {
	// Only 4 bytes, not enough for a full 8-byte header
	data := []byte{0x00, 0x00, 0x00, 0x0C}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for truncated header")
	}
}

func TestReader_ReadBox_ZeroLength(t *testing.T) {
	// Length = 0 means box extends to EOF (not supported)
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(0)) // Length = 0
	binary.Write(&buf, binary.BigEndian, uint32(TypeFileType))

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for zero length box")
	}
}

func TestReader_ReadBox_InvalidLength(t *testing.T) {
	// Length < 8 (header size) is invalid
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(4)) // Length = 4 (too short)
	binary.Write(&buf, binary.BigEndian, uint32(TypeFileType))

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for invalid length")
	}
}

func TestReader_ReadBox_TruncatedExtendedLength(t *testing.T) {
	// Extended length marker but no extended length value
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1)) // Extended length marker
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))
	// Missing the 8-byte extended length

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for truncated extended length")
	}
}

func TestReader_ReadBox_TruncatedContents(t *testing.T) {
	// Box claims 100 bytes but only has 4
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(100)) // Length
	binary.Write(&buf, binary.BigEndian, uint32(TypeFileType))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) // Only 4 bytes of contents

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for truncated contents")
	}
}

func TestReader_ReadBox_ExtendedInvalidLength(t *testing.T) {
	// Extended length but actual length < 16
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1)) // Extended length marker
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))
	binary.Write(&buf, binary.BigEndian, uint64(10)) // Length = 10, but minimum is 16 for extended

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for invalid extended length")
	}
}

func TestImageHeaderBox_Parse_TooShort(t *testing.T) {
	data := make([]byte, 10) // Needs 14 bytes
	b := &ImageHeaderBox{}
	err := b.Parse(data)
	if err == nil {
		t.Error("Parse() expected error for short data")
	}
}

func TestBitsPerCompBox_Parse(t *testing.T) {
	data := []byte{7, 7, 7} // 3 components, 7 bits each
	b := &BitsPerCompBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(b.BitsPerComponent) != 3 {
		t.Errorf("BitsPerComponent length = %d, want 3", len(b.BitsPerComponent))
	}
	for i, bpc := range b.BitsPerComponent {
		if bpc != 7 {
			t.Errorf("BitsPerComponent[%d] = %d, want 7", i, bpc)
		}
	}
}

func TestBitsPerCompBox_Parse_Empty(t *testing.T) {
	data := []byte{}
	b := &BitsPerCompBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(b.BitsPerComponent) != 0 {
		t.Errorf("BitsPerComponent length = %d, want 0", len(b.BitsPerComponent))
	}
}

func TestColorSpecBox_Parse_TooShort(t *testing.T) {
	data := []byte{0x01, 0x00} // Only 2 bytes, needs at least 3
	b := &ColorSpecBox{}
	err := b.Parse(data)
	if err == nil {
		t.Error("Parse() expected error for short data")
	}
}

func TestColorSpecBox_Parse_EnumeratedTooShort(t *testing.T) {
	data := []byte{0x01, 0x00, 0x00, 0x01, 0x02} // Method=1 but only 5 bytes (needs 7)
	b := &ColorSpecBox{}
	err := b.Parse(data)
	if err == nil {
		t.Error("Parse() expected error for short enumerated colorspace data")
	}
}

func TestColorSpecBox_Parse_FullICC(t *testing.T) {
	// Test method=3 (full ICC profile)
	profile := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	data := make([]byte, 3+len(profile))
	data[0] = 3 // Method = full ICC
	data[1] = 0
	data[2] = 0
	copy(data[3:], profile)

	b := &ColorSpecBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if b.Method != 3 {
		t.Errorf("Method = %d, want 3", b.Method)
	}
	if !bytes.Equal(b.ICCProfile, profile) {
		t.Errorf("ICCProfile = %v, want %v", b.ICCProfile, profile)
	}
}

func TestColorSpecBox_Bytes_ICC(t *testing.T) {
	profile := []byte{0x01, 0x02, 0x03, 0x04}
	b := &ColorSpecBox{
		Method:        2,
		Precedence:    1,
		Approximation: 2,
		ICCProfile:    profile,
	}
	data := b.Bytes()

	expectedLen := 3 + len(profile)
	if len(data) != expectedLen {
		t.Fatalf("Bytes length = %d, want %d", len(data), expectedLen)
	}
	if data[0] != 2 {
		t.Errorf("Method = %d, want 2", data[0])
	}
	if data[1] != 1 {
		t.Errorf("Precedence = %d, want 1", data[1])
	}
	if data[2] != 2 {
		t.Errorf("Approximation = %d, want 2", data[2])
	}
	if !bytes.Equal(data[3:], profile) {
		t.Errorf("ICCProfile = %v, want %v", data[3:], profile)
	}
}

func TestFileTypeBox_Parse_TooShort(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04} // Only 4 bytes, needs at least 8
	b := &FileTypeBox{}
	err := b.Parse(data)
	if err == nil {
		t.Error("Parse() expected error for short data")
	}
}

func TestFileTypeBox_Parse_NoCompatibility(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], uint32(0x6A703220)) // "jp2 "
	binary.BigEndian.PutUint32(data[4:8], 0)                  // Minor version

	b := &FileTypeBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(b.Compatibility) != 0 {
		t.Errorf("Compatibility count = %d, want 0", len(b.Compatibility))
	}
}

func TestFileTypeBox_Parse_MultipleCompatibility(t *testing.T) {
	data := make([]byte, 16)
	binary.BigEndian.PutUint32(data[0:4], uint32(0x6A703220))  // "jp2 "
	binary.BigEndian.PutUint32(data[4:8], 0)                   // Minor version
	binary.BigEndian.PutUint32(data[8:12], uint32(0x6A703220)) // "jp2 "
	binary.BigEndian.PutUint32(data[12:16], uint32(0x6A707820)) // "jpx "

	b := &FileTypeBox{}
	err := b.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(b.Compatibility) != 2 {
		t.Errorf("Compatibility count = %d, want 2", len(b.Compatibility))
	}
}

func TestParseJP2Header(t *testing.T) {
	// Build a JP2 header super-box containing ihdr and colr boxes
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           480,
		Width:            640,
		NumComponents:    3,
		BitsPerComponent: 8,
		CompressionType:  7,
		UnknownColorspace: 0,
		IPR:              0,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Color specification box
	colr := &ColorSpecBox{
		Method:             1,
		EnumeratedColorspace: CSSRGB,
	}
	colrBox := &Box{
		Type:     TypeColorSpec,
		Contents: colr.Bytes(),
	}
	colrBox.Length = uint64(8 + len(colrBox.Contents))
	jp2hContents.Write(colrBox.Bytes())

	// Parse the JP2 header
	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader == nil {
		t.Fatal("ImageHeader is nil")
	}
	if h.ImageHeader.Width != 640 {
		t.Errorf("Width = %d, want 640", h.ImageHeader.Width)
	}
	if h.ImageHeader.Height != 480 {
		t.Errorf("Height = %d, want 480", h.ImageHeader.Height)
	}

	if h.ColorSpec == nil {
		t.Fatal("ColorSpec is nil")
	}
	if h.ColorSpec.EnumeratedColorspace != CSSRGB {
		t.Errorf("Colorspace = %d, want %d", h.ColorSpec.EnumeratedColorspace, CSSRGB)
	}
}

func TestParseJP2Header_WithBitsPerComp(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box with BPC=0xFF (use BitsPerComp box)
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    3,
		BitsPerComponent: 0xFF, // Indicates BPC box is used
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Bits per component box
	bpcData := []byte{8, 8, 8} // 3 components, 8 bits each
	bpcBox := &Box{
		Type:     TypeBitsPerComp,
		Contents: bpcData,
	}
	bpcBox.Length = uint64(8 + len(bpcBox.Contents))
	jp2hContents.Write(bpcBox.Bytes())

	// Color specification box
	colr := &ColorSpecBox{
		Method:             1,
		EnumeratedColorspace: CSSRGB,
	}
	colrBox := &Box{
		Type:     TypeColorSpec,
		Contents: colr.Bytes(),
	}
	colrBox.Length = uint64(8 + len(colrBox.Contents))
	jp2hContents.Write(colrBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.BitsPerComp == nil {
		t.Fatal("BitsPerComp is nil")
	}
	if len(h.BitsPerComp.BitsPerComponent) != 3 {
		t.Errorf("BitsPerComponent length = %d, want 3", len(h.BitsPerComp.BitsPerComponent))
	}
}

func TestParseJP2Header_Empty(t *testing.T) {
	h, err := ParseJP2Header([]byte{})
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader != nil {
		t.Error("ImageHeader should be nil for empty data")
	}
}

func TestParseJP2Header_InvalidBox(t *testing.T) {
	// Truncated box header
	data := []byte{0x00, 0x00, 0x00, 0x10, 0x69} // Partial header
	_, err := ParseJP2Header(data)
	if err == nil {
		t.Error("ParseJP2Header() expected error for invalid box")
	}
}

func TestRoundtrip_Box(t *testing.T) {
	// Test writing and reading a box
	original := &Box{
		Type:     TypeContCodestream,
		Length:   28,
		Contents: []byte{0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x0A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	// Write
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteBox(original); err != nil {
		t.Fatalf("WriteBox() error: %v", err)
	}

	// Read
	r := NewReader(&buf)
	read, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() error: %v", err)
	}

	if read.Type != original.Type {
		t.Errorf("Type = %v, want %v", read.Type, original.Type)
	}
	if read.Length != original.Length {
		t.Errorf("Length = %d, want %d", read.Length, original.Length)
	}
	if !bytes.Equal(read.Contents, original.Contents) {
		t.Errorf("Contents mismatch")
	}
}

func TestRoundtrip_ImageHeaderBox(t *testing.T) {
	original := &ImageHeaderBox{
		Height:           1080,
		Width:            1920,
		NumComponents:    4,
		BitsPerComponent: 16,
		CompressionType:  7,
		UnknownColorspace: 1,
		IPR:              1,
	}

	data := original.Bytes()

	parsed := &ImageHeaderBox{}
	if err := parsed.Parse(data); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if parsed.Height != original.Height {
		t.Errorf("Height = %d, want %d", parsed.Height, original.Height)
	}
	if parsed.Width != original.Width {
		t.Errorf("Width = %d, want %d", parsed.Width, original.Width)
	}
	if parsed.NumComponents != original.NumComponents {
		t.Errorf("NumComponents = %d, want %d", parsed.NumComponents, original.NumComponents)
	}
	if parsed.BitsPerComponent != original.BitsPerComponent {
		t.Errorf("BitsPerComponent = %d, want %d", parsed.BitsPerComponent, original.BitsPerComponent)
	}
	if parsed.CompressionType != original.CompressionType {
		t.Errorf("CompressionType = %d, want %d", parsed.CompressionType, original.CompressionType)
	}
	if parsed.UnknownColorspace != original.UnknownColorspace {
		t.Errorf("UnknownColorspace = %d, want %d", parsed.UnknownColorspace, original.UnknownColorspace)
	}
	if parsed.IPR != original.IPR {
		t.Errorf("IPR = %d, want %d", parsed.IPR, original.IPR)
	}
}

func TestRoundtrip_ColorSpecBox_Enumerated(t *testing.T) {
	original := &ColorSpecBox{
		Method:             1,
		Precedence:         0,
		Approximation:      1,
		EnumeratedColorspace: CSGray,
	}

	data := original.Bytes()

	parsed := &ColorSpecBox{}
	if err := parsed.Parse(data); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if parsed.Method != original.Method {
		t.Errorf("Method = %d, want %d", parsed.Method, original.Method)
	}
	if parsed.Precedence != original.Precedence {
		t.Errorf("Precedence = %d, want %d", parsed.Precedence, original.Precedence)
	}
	if parsed.Approximation != original.Approximation {
		t.Errorf("Approximation = %d, want %d", parsed.Approximation, original.Approximation)
	}
	if parsed.EnumeratedColorspace != original.EnumeratedColorspace {
		t.Errorf("EnumeratedColorspace = %d, want %d", parsed.EnumeratedColorspace, original.EnumeratedColorspace)
	}
}

func TestRoundtrip_ColorSpecBox_ICC(t *testing.T) {
	original := &ColorSpecBox{
		Method:        2,
		Precedence:    1,
		Approximation: 0,
		ICCProfile:    []byte{0x00, 0x00, 0x02, 0x30, 0x6C, 0x63, 0x6D, 0x73}, // Partial ICC header
	}

	data := original.Bytes()

	parsed := &ColorSpecBox{}
	if err := parsed.Parse(data); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if parsed.Method != original.Method {
		t.Errorf("Method = %d, want %d", parsed.Method, original.Method)
	}
	if !bytes.Equal(parsed.ICCProfile, original.ICCProfile) {
		t.Errorf("ICCProfile = %v, want %v", parsed.ICCProfile, original.ICCProfile)
	}
}

func TestRoundtrip_FileTypeBox(t *testing.T) {
	original := &FileTypeBox{
		Brand:        0x6A703220,
		MinorVersion: 1,
		Compatibility: []Type{0x6A703220, 0x6A707820, 0x6A703268},
	}

	data := original.Bytes()

	parsed := &FileTypeBox{}
	if err := parsed.Parse(data); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if parsed.Brand != original.Brand {
		t.Errorf("Brand = %08X, want %08X", parsed.Brand, original.Brand)
	}
	if parsed.MinorVersion != original.MinorVersion {
		t.Errorf("MinorVersion = %d, want %d", parsed.MinorVersion, original.MinorVersion)
	}
	if len(parsed.Compatibility) != len(original.Compatibility) {
		t.Fatalf("Compatibility count = %d, want %d", len(parsed.Compatibility), len(original.Compatibility))
	}
	for i := range original.Compatibility {
		if parsed.Compatibility[i] != original.Compatibility[i] {
			t.Errorf("Compatibility[%d] = %08X, want %08X", i, parsed.Compatibility[i], original.Compatibility[i])
		}
	}
}

func TestReader_ReadMultipleBoxes(t *testing.T) {
	var buf bytes.Buffer

	// Write two boxes
	box1 := &Box{
		Type:     TypeFileType,
		Length:   16,
		Contents: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	box2 := &Box{
		Type:     TypeJP2Header,
		Length:   12,
		Contents: []byte{0xAA, 0xBB, 0xCC, 0xDD},
	}

	w := NewWriter(&buf)
	w.WriteBox(box1)
	w.WriteBox(box2)

	// Read both boxes
	r := NewReader(&buf)

	read1, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() 1 error: %v", err)
	}
	if read1.Type != TypeFileType {
		t.Errorf("Box 1 Type = %v, want %v", read1.Type, TypeFileType)
	}

	read2, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() 2 error: %v", err)
	}
	if read2.Type != TypeJP2Header {
		t.Errorf("Box 2 Type = %v, want %v", read2.Type, TypeJP2Header)
	}

	// Third read should be EOF
	_, err = r.ReadBox()
	if err != io.EOF {
		t.Errorf("ReadBox() 3 error = %v, want EOF", err)
	}
}

func TestBox_Bytes_Extended(t *testing.T) {
	// Test Bytes() for extended length box
	b := &Box{
		Type:     TypeContCodestream,
		Length:   0x100000010, // > 32-bit
		Contents: make([]byte, 8),
	}
	data := b.Bytes()

	// Extended header (16 bytes) + contents (8 bytes) = 24 bytes
	if len(data) != 24 {
		t.Errorf("Bytes length = %d, want 24", len(data))
	}

	// Check extended length marker
	marker := binary.BigEndian.Uint32(data[0:4])
	if marker != 1 {
		t.Errorf("Extended length marker = %d, want 1", marker)
	}

	// Check type
	typ := Type(binary.BigEndian.Uint32(data[4:8]))
	if typ != TypeContCodestream {
		t.Errorf("Type = %v, want %v", typ, TypeContCodestream)
	}

	// Check extended length value
	extLen := binary.BigEndian.Uint64(data[8:16])
	if extLen != 0x100000010 {
		t.Errorf("Extended length = %016X, want %016X", extLen, 0x100000010)
	}
}

func TestCreateJP2Header_ParseRoundtrip(t *testing.T) {
	// Create a JP2 header and then parse it
	box := CreateJP2Header(800, 600, 3, 8, CSSRGB)

	h, err := ParseJP2Header(box.Contents)
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader == nil {
		t.Fatal("ImageHeader is nil")
	}
	if h.ImageHeader.Width != 800 {
		t.Errorf("Width = %d, want 800", h.ImageHeader.Width)
	}
	if h.ImageHeader.Height != 600 {
		t.Errorf("Height = %d, want 600", h.ImageHeader.Height)
	}
	if h.ImageHeader.NumComponents != 3 {
		t.Errorf("NumComponents = %d, want 3", h.ImageHeader.NumComponents)
	}
	if h.ImageHeader.BitsPerComponent != 8 {
		t.Errorf("BitsPerComponent = %d, want 8", h.ImageHeader.BitsPerComponent)
	}

	if h.ColorSpec == nil {
		t.Fatal("ColorSpec is nil")
	}
	if h.ColorSpec.EnumeratedColorspace != CSSRGB {
		t.Errorf("EnumeratedColorspace = %d, want %d", h.ColorSpec.EnumeratedColorspace, CSSRGB)
	}
}

func TestType_String_AllTypes(t *testing.T) {
	tests := []struct {
		typ  Type
		want string
	}{
		{TypeBitsPerComp, "bpcc"},
		{TypePalette, "pclr"},
		{TypeComponentMap, "cmap"},
		{TypeChannelDef, "cdef"},
		{TypeResolution, "res "},
		{TypeCaptureRes, "resc"},
		{TypeDisplayRes, "resd"},
		{TypeCodestreamH, "jpch"},
		{TypeTilePartH, "jpth"},
		{TypeXML, "xml "},
		{TypeUUID, "uuid"},
		{TypeUUIDInfo, "uinf"},
		{TypeUUIDList, "ulst"},
		{TypeURL, "url "},
		{TypeIPR, "jp2i"},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("Type(%08X).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestColorspaceConstants_All(t *testing.T) {
	tests := []struct {
		cs   uint32
		name string
		want uint32
	}{
		{CSBilevel1, "CSBilevel1", 0},
		{CSYCbCr1, "CSYCbCr1", 1},
		{CSYCbCr2, "CSYCbCr2", 3},
		{CSYCbCr3, "CSYCbCr3", 4},
		{CSPhotoYCC, "CSPhotoYCC", 9},
		{CSCMY, "CSCMY", 11},
		{CSCMYK, "CSCMYK", 12},
		{CSYCCK, "CSYCCK", 13},
		{CSCIELab, "CSCIELab", 14},
		{CSBilevel2, "CSBilevel2", 15},
		{CSSRGB, "CSSRGB", 16},
		{CSGray, "CSGray", 17},
		{CSsYCC, "CSsYCC", 18},
		{CSCIEJab, "CSCIEJab", 19},
		{CSeSRGB, "CSeSRGB", 20},
		{CSROMMRGB, "CSROMMRGB", 21},
		{CSYPbPr1125, "CSYPbPr1125", 22},
		{CSYPbPr1250, "CSYPbPr1250", 23},
		{CSeSYCC, "CSeSYCC", 24},
	}

	for _, tt := range tests {
		if tt.cs != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.cs, tt.want)
		}
	}
}

func TestReader_OffsetAfterExtended(t *testing.T) {
	// Create an extended length box
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1)) // Extended length marker
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))
	binary.Write(&buf, binary.BigEndian, uint64(24)) // Extended length
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	r := NewReader(&buf)

	if r.Offset() != 0 {
		t.Errorf("Initial Offset() = %d, want 0", r.Offset())
	}

	_, err := r.ReadBox()
	if err != nil {
		t.Fatalf("ReadBox() error: %v", err)
	}

	// Extended length: 8 (header) + 8 (extended length) + 8 (content) = 24
	if r.Offset() != 24 {
		t.Errorf("Offset() = %d, want 24", r.Offset())
	}
}

func TestParseJP2Header_InvalidImageHeader(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box with invalid (too short) contents
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: []byte{0x01, 0x02}, // Too short - needs 14 bytes
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	_, err := ParseJP2Header(jp2hContents.Bytes())
	if err == nil {
		t.Error("ParseJP2Header() expected error for invalid image header")
	}
}

func TestParseJP2Header_InvalidColorSpec(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Valid image header
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    3,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Color specification box with invalid (too short) contents
	colrBox := &Box{
		Type:     TypeColorSpec,
		Contents: []byte{0x01}, // Too short - needs at least 3 bytes
	}
	colrBox.Length = uint64(8 + len(colrBox.Contents))
	jp2hContents.Write(colrBox.Bytes())

	_, err := ParseJP2Header(jp2hContents.Bytes())
	if err == nil {
		t.Error("ParseJP2Header() expected error for invalid color spec")
	}
}

func TestParseJP2Header_WithChannelDef(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    3,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Channel definition box (stub content)
	cdefBox := &Box{
		Type:     TypeChannelDef,
		Contents: []byte{0x00, 0x03}, // 3 channels
	}
	cdefBox.Length = uint64(8 + len(cdefBox.Contents))
	jp2hContents.Write(cdefBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	// Should parse without error even though channel def parsing is not implemented
	if h.ImageHeader == nil {
		t.Error("ImageHeader should not be nil")
	}
}

func TestParseJP2Header_WithPalette(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    1,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Palette box (stub content)
	pclrBox := &Box{
		Type:     TypePalette,
		Contents: []byte{0x00, 0x10, 0x03, 0x08, 0x08, 0x08}, // 16 entries, 3 columns, 8 bits each
	}
	pclrBox.Length = uint64(8 + len(pclrBox.Contents))
	jp2hContents.Write(pclrBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader == nil {
		t.Error("ImageHeader should not be nil")
	}
}

func TestParseJP2Header_WithComponentMap(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    1,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Component mapping box (stub content)
	cmapBox := &Box{
		Type:     TypeComponentMap,
		Contents: []byte{0x00, 0x00, 0x01, 0x00}, // mapping for 1 component
	}
	cmapBox.Length = uint64(8 + len(cmapBox.Contents))
	jp2hContents.Write(cmapBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader == nil {
		t.Error("ImageHeader should not be nil")
	}
}

func TestParseJP2Header_WithResolution(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    3,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Resolution box (stub content - it's a superbox so just placeholder)
	resBox := &Box{
		Type:     TypeResolution,
		Contents: []byte{0x00, 0x00, 0x00, 0x00},
	}
	resBox.Length = uint64(8 + len(resBox.Contents))
	jp2hContents.Write(resBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	if h.ImageHeader == nil {
		t.Error("ImageHeader should not be nil")
	}
}

func TestParseJP2Header_UnknownBoxType(t *testing.T) {
	var jp2hContents bytes.Buffer

	// Image header box
	ihdr := &ImageHeaderBox{
		Height:           100,
		Width:            100,
		NumComponents:    3,
		BitsPerComponent: 8,
		CompressionType:  7,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))
	jp2hContents.Write(ihdrBox.Bytes())

	// Unknown box type
	unknownBox := &Box{
		Type:     Type(0x12345678),
		Contents: []byte{0x00, 0x01, 0x02, 0x03},
	}
	unknownBox.Length = uint64(8 + len(unknownBox.Contents))
	jp2hContents.Write(unknownBox.Bytes())

	h, err := ParseJP2Header(jp2hContents.Bytes())
	if err != nil {
		t.Fatalf("ParseJP2Header() error: %v", err)
	}

	// Should skip unknown box and still parse image header
	if h.ImageHeader == nil {
		t.Error("ImageHeader should not be nil")
	}
}

func TestReader_ReadBox_TooLarge(t *testing.T) {
	// Create a box header that claims the content is > 1GB
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(1))                    // Extended length marker
	binary.Write(&buf, binary.BigEndian, uint32(TypeContCodestream))   // Type
	binary.Write(&buf, binary.BigEndian, uint64(0x50000000))           // Extended length: ~1.3GB

	r := NewReader(&buf)
	_, err := r.ReadBox()
	if err == nil {
		t.Error("ReadBox() expected error for box too large")
	}
}
