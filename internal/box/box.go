// Package box implements JP2 file format box parsing and generation.
//
// JP2 files consist of a sequence of boxes, where each box has:
// - 4-byte length (or 1 for extended length)
// - 4-byte type code
// - Optional 8-byte extended length
// - Box contents
package box

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Box type codes
const (
	// Signature and file type
	TypeJP2Signature  Type = 0x6A502020 // "jP  " - JP2 signature box
	TypeFileType      Type = 0x66747970 // "ftyp" - File type box

	// JP2 header
	TypeJP2Header     Type = 0x6A703268 // "jp2h" - JP2 header super-box
	TypeImageHeader   Type = 0x69686472 // "ihdr" - Image header box
	TypeBitsPerComp   Type = 0x62706363 // "bpcc" - Bits per component box
	TypeColorSpec     Type = 0x636F6C72 // "colr" - Color specification box
	TypePalette       Type = 0x70636C72 // "pclr" - Palette box
	TypeComponentMap  Type = 0x636D6170 // "cmap" - Component mapping box
	TypeChannelDef    Type = 0x63646566 // "cdef" - Channel definition box
	TypeResolution    Type = 0x72657320 // "res " - Resolution super-box
	TypeCaptureRes    Type = 0x72657363 // "resc" - Capture resolution box
	TypeDisplayRes    Type = 0x72657364 // "resd" - Default display resolution box

	// Codestream
	TypeContCodestream Type = 0x6A703263 // "jp2c" - Contiguous codestream box
	TypeCodestreamH   Type = 0x6A706368 // "jpch" - Codestream header box
	TypeTilePartH     Type = 0x6A707468 // "jpth" - Tile-part header box

	// Metadata
	TypeXML           Type = 0x786D6C20 // "xml " - XML box
	TypeUUID          Type = 0x75756964 // "uuid" - UUID box
	TypeUUIDInfo      Type = 0x75696E66 // "uinf" - UUID info super-box
	TypeUUIDList      Type = 0x756C7374 // "ulst" - UUID list box
	TypeURL           Type = 0x75726C20 // "url " - URL box

	// IPR
	TypeIPR           Type = 0x6A703269 // "jp2i" - IPR box
)

// Type represents a 4-byte box type code.
type Type uint32

// String returns the 4-character type code.
func (t Type) String() string {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(t))
	return string(b)
}

// Box represents a JP2 box.
type Box struct {
	Type     Type
	Length   uint64 // Total box length including header
	Contents []byte // Box contents (excluding header)
}

// Header returns the box header bytes.
func (b *Box) Header() []byte {
	if b.Length <= 0xFFFFFFFF {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], uint32(b.Length))
		binary.BigEndian.PutUint32(header[4:8], uint32(b.Type))
		return header
	}
	// Extended length
	header := make([]byte, 16)
	binary.BigEndian.PutUint32(header[0:4], 1)
	binary.BigEndian.PutUint32(header[4:8], uint32(b.Type))
	binary.BigEndian.PutUint64(header[8:16], b.Length)
	return header
}

// Bytes returns the complete box as bytes.
func (b *Box) Bytes() []byte {
	header := b.Header()
	result := make([]byte, len(header)+len(b.Contents))
	copy(result, header)
	copy(result[len(header):], b.Contents)
	return result
}

// Reader reads JP2 boxes from a stream.
type Reader struct {
	r      io.Reader
	offset int64
}

// NewReader creates a new box reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// ReadBox reads the next box from the stream.
func (r *Reader) ReadBox() (*Box, error) {
	// Read box length and type
	header := make([]byte, 8)
	n, err := io.ReadFull(r.r, header)
	if err != nil {
		if err == io.EOF && n == 0 {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("reading box header: %w", err)
	}
	r.offset += 8

	length := uint64(binary.BigEndian.Uint32(header[0:4]))
	boxType := Type(binary.BigEndian.Uint32(header[4:8]))

	headerLen := uint64(8)

	// Handle extended length
	if length == 1 {
		extLen := make([]byte, 8)
		if _, err := io.ReadFull(r.r, extLen); err != nil {
			return nil, fmt.Errorf("reading extended length: %w", err)
		}
		length = binary.BigEndian.Uint64(extLen)
		headerLen = 16
		r.offset += 8
	} else if length == 0 {
		// Box extends to end of file - we can't handle this without seeking
		return nil, errors.New("box extends to EOF not supported")
	}

	if length < headerLen {
		return nil, fmt.Errorf("invalid box length: %d", length)
	}

	// Read contents
	contentLen := length - headerLen
	if contentLen > 1<<30 { // 1GB limit
		return nil, fmt.Errorf("box too large: %d bytes", contentLen)
	}

	contents := make([]byte, contentLen)
	if _, err := io.ReadFull(r.r, contents); err != nil {
		return nil, fmt.Errorf("reading box contents: %w", err)
	}
	r.offset += int64(contentLen)

	return &Box{
		Type:     boxType,
		Length:   length,
		Contents: contents,
	}, nil
}

// Offset returns the current stream offset.
func (r *Reader) Offset() int64 {
	return r.offset
}

// Writer writes JP2 boxes to a stream.
type Writer struct {
	w io.Writer
}

// NewWriter creates a new box writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteBox writes a box to the stream.
func (w *Writer) WriteBox(b *Box) error {
	_, err := w.w.Write(b.Bytes())
	return err
}

// WriteSignature writes the JP2 signature.
func (w *Writer) WriteSignature() error {
	// JP2 signature: 12 bytes
	sig := []byte{
		0x00, 0x00, 0x00, 0x0C, // Length = 12
		0x6A, 0x50, 0x20, 0x20, // Type = "jP  "
		0x0D, 0x0A, 0x87, 0x0A, // Signature content
	}
	_, err := w.w.Write(sig)
	return err
}

// JP2Header represents the JP2 header box contents.
type JP2Header struct {
	ImageHeader *ImageHeaderBox
	BitsPerComp *BitsPerCompBox
	ColorSpec   *ColorSpecBox
	Palette     *PaletteBox
	ComponentMap *ComponentMapBox
	ChannelDef  *ChannelDefBox
	Resolution  *ResolutionBox
}

// ImageHeaderBox represents the image header box.
type ImageHeaderBox struct {
	Height           uint32
	Width            uint32
	NumComponents    uint16
	BitsPerComponent uint8 // 7-bit value or 0xFF for BPC box
	CompressionType  uint8 // Always 7 for JP2
	UnknownColorspace uint8
	IPR              uint8
}

// Parse parses the image header box contents.
func (b *ImageHeaderBox) Parse(data []byte) error {
	if len(data) < 14 {
		return errors.New("image header box too short")
	}
	b.Height = binary.BigEndian.Uint32(data[0:4])
	b.Width = binary.BigEndian.Uint32(data[4:8])
	b.NumComponents = binary.BigEndian.Uint16(data[8:10])
	b.BitsPerComponent = data[10]
	b.CompressionType = data[11]
	b.UnknownColorspace = data[12]
	b.IPR = data[13]
	return nil
}

// Bytes returns the box contents.
func (b *ImageHeaderBox) Bytes() []byte {
	data := make([]byte, 14)
	binary.BigEndian.PutUint32(data[0:4], b.Height)
	binary.BigEndian.PutUint32(data[4:8], b.Width)
	binary.BigEndian.PutUint16(data[8:10], b.NumComponents)
	data[10] = b.BitsPerComponent
	data[11] = b.CompressionType
	data[12] = b.UnknownColorspace
	data[13] = b.IPR
	return data
}

// BitsPerCompBox represents per-component bit depth.
type BitsPerCompBox struct {
	BitsPerComponent []uint8
}

// Parse parses the bits per component box.
func (b *BitsPerCompBox) Parse(data []byte) error {
	b.BitsPerComponent = make([]uint8, len(data))
	copy(b.BitsPerComponent, data)
	return nil
}

// ColorSpecBox represents color specification.
type ColorSpecBox struct {
	Method             uint8
	Precedence         uint8
	Approximation      uint8
	EnumeratedColorspace uint32
	ICCProfile         []byte
}

// Enumerated colorspace values per ISO/IEC 15444-1 Annex M
const (
	CSBilevel1    = 0  // Bi-level (black and white)
	CSYCbCr1      = 1  // YCbCr(1) - ITU-R BT.709-5 based (sRGB primaries)
	CSYCbCr2      = 3  // YCbCr(2) - ITU-R BT.601-5 for 625-line systems
	CSYCbCr3      = 4  // YCbCr(3) - ITU-R BT.601-5 for 525-line systems
	CSPhotoYCC    = 9  // PhotoYCC (Kodak Photo CD)
	CSCMY         = 11 // CMY (Cyan, Magenta, Yellow)
	CSCMYK        = 12 // CMYK (Cyan, Magenta, Yellow, Key/Black)
	CSYCCK        = 13 // YCCK (PhotoYCC with Key/Black)
	CSCIELab      = 14 // CIELab (D50 illuminant)
	CSBilevel2    = 15 // Bi-level(2) - alternative bi-level encoding
	CSSRGB        = 16 // sRGB (IEC 61966-2-1)
	CSGray        = 17 // Grayscale
	CSsYCC        = 18 // sYCC (IEC 61966-2-1 Annex G)
	CSCIEJab      = 19 // CIEJab (CIECAM02-based)
	CSeSRGB       = 20 // e-sRGB (extended sRGB, IEC 61966-2-1 Amendment 1)
	CSROMMRGB     = 21 // ROMM-RGB (Reference Output Medium Metric, ISO 22028-2)
	CSYPbPr1125   = 22 // YPbPr for 1125/60 systems (SMPTE 274M)
	CSYPbPr1250   = 23 // YPbPr for 1250/50 systems (ITU-R BT.1361)
	CSeSYCC       = 24 // e-sYCC (extended sYCC gamut)
)

// Parse parses the color specification box.
func (b *ColorSpecBox) Parse(data []byte) error {
	if len(data) < 3 {
		return errors.New("color specification box too short")
	}
	b.Method = data[0]
	b.Precedence = data[1]
	b.Approximation = data[2]

	switch b.Method {
	case 1: // Enumerated colorspace
		if len(data) < 7 {
			return errors.New("color specification box too short for enumerated CS")
		}
		b.EnumeratedColorspace = binary.BigEndian.Uint32(data[3:7])
	case 2: // Restricted ICC profile
		b.ICCProfile = data[3:]
	case 3: // Any ICC method (full profile)
		b.ICCProfile = data[3:]
	}
	return nil
}

// Bytes returns the box contents.
func (b *ColorSpecBox) Bytes() []byte {
	if b.Method == 1 {
		data := make([]byte, 7)
		data[0] = b.Method
		data[1] = b.Precedence
		data[2] = b.Approximation
		binary.BigEndian.PutUint32(data[3:7], b.EnumeratedColorspace)
		return data
	}
	data := make([]byte, 3+len(b.ICCProfile))
	data[0] = b.Method
	data[1] = b.Precedence
	data[2] = b.Approximation
	copy(data[3:], b.ICCProfile)
	return data
}

// PaletteBox represents a color palette.
type PaletteBox struct {
	NumEntries   uint16
	NumColumns   uint8
	BitsPerEntry []uint8
	Entries      [][]uint32
}

// ComponentMapBox represents component mapping.
type ComponentMapBox struct {
	Mappings []ComponentMapping
}

// ComponentMapping maps a channel to a component.
type ComponentMapping struct {
	Component uint16
	MappingType uint8
	PaletteColumn uint8
}

// ChannelDefBox defines channel meanings.
type ChannelDefBox struct {
	Definitions []ChannelDefinition
}

// ChannelDefinition describes a channel.
type ChannelDefinition struct {
	Channel    uint16
	Type       uint16 // 0=color, 1=opacity, 2=premultiplied opacity
	Association uint16 // Component association
}

// ResolutionBox contains resolution information.
type ResolutionBox struct {
	CaptureResX    uint32
	CaptureResY    uint32
	DisplayResX    uint32
	DisplayResY    uint32
}

// FileTypeBox represents the ftyp box.
type FileTypeBox struct {
	Brand         Type
	MinorVersion  uint32
	Compatibility []Type
}

// Parse parses the file type box.
func (b *FileTypeBox) Parse(data []byte) error {
	if len(data) < 8 {
		return errors.New("file type box too short")
	}
	b.Brand = Type(binary.BigEndian.Uint32(data[0:4]))
	b.MinorVersion = binary.BigEndian.Uint32(data[4:8])

	// Read compatibility list
	numCompat := (len(data) - 8) / 4
	b.Compatibility = make([]Type, numCompat)
	for i := 0; i < numCompat; i++ {
		b.Compatibility[i] = Type(binary.BigEndian.Uint32(data[8+i*4:]))
	}
	return nil
}

// Bytes returns the box contents.
func (b *FileTypeBox) Bytes() []byte {
	data := make([]byte, 8+4*len(b.Compatibility))
	binary.BigEndian.PutUint32(data[0:4], uint32(b.Brand))
	binary.BigEndian.PutUint32(data[4:8], b.MinorVersion)
	for i, c := range b.Compatibility {
		binary.BigEndian.PutUint32(data[8+i*4:], uint32(c))
	}
	return data
}

// ParseJP2Header parses a JP2 header super-box.
func ParseJP2Header(data []byte) (*JP2Header, error) {
	h := &JP2Header{}
	r := NewReader(&byteReader{data: data})

	for {
		box, err := r.ReadBox()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch box.Type {
		case TypeImageHeader:
			h.ImageHeader = &ImageHeaderBox{}
			if err := h.ImageHeader.Parse(box.Contents); err != nil {
				return nil, err
			}
		case TypeBitsPerComp:
			h.BitsPerComp = &BitsPerCompBox{}
			if err := h.BitsPerComp.Parse(box.Contents); err != nil {
				return nil, err
			}
		case TypeColorSpec:
			h.ColorSpec = &ColorSpecBox{}
			if err := h.ColorSpec.Parse(box.Contents); err != nil {
				return nil, err
			}
		case TypeChannelDef:
			// Parse channel definition
		case TypePalette:
			// Parse palette
		case TypeComponentMap:
			// Parse component map
		case TypeResolution:
			// Parse resolution
		}
	}

	return h, nil
}

// byteReader wraps a byte slice as an io.Reader.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// CreateJP2Header creates a JP2 header box.
func CreateJP2Header(width, height uint32, numComponents uint16, bitsPerComponent uint8, colorspace uint32) *Box {
	// Create image header
	ihdr := &ImageHeaderBox{
		Width:            width,
		Height:           height,
		NumComponents:    numComponents,
		BitsPerComponent: bitsPerComponent,
		CompressionType:  7, // JP2
		UnknownColorspace: 0,
		IPR:              0,
	}
	ihdrBox := &Box{
		Type:     TypeImageHeader,
		Contents: ihdr.Bytes(),
	}
	ihdrBox.Length = uint64(8 + len(ihdrBox.Contents))

	// Create color specification
	colr := &ColorSpecBox{
		Method:             1, // Enumerated
		Precedence:         0,
		Approximation:      0,
		EnumeratedColorspace: colorspace,
	}
	colrBox := &Box{
		Type:     TypeColorSpec,
		Contents: colr.Bytes(),
	}
	colrBox.Length = uint64(8 + len(colrBox.Contents))

	// Combine into JP2 header super-box
	contents := append(ihdrBox.Bytes(), colrBox.Bytes()...)
	return &Box{
		Type:     TypeJP2Header,
		Length:   uint64(8 + len(contents)),
		Contents: contents,
	}
}

// CreateFileTypeBox creates a file type box for JP2.
func CreateFileTypeBox() *Box {
	ftyp := &FileTypeBox{
		Brand:        0x6A703220, // "jp2 "
		MinorVersion: 0,
		Compatibility: []Type{
			0x6A703220, // "jp2 "
		},
	}
	return &Box{
		Type:     TypeFileType,
		Length:   uint64(8 + len(ftyp.Bytes())),
		Contents: ftyp.Bytes(),
	}
}

// CreateCodestreamBox creates a contiguous codestream box.
func CreateCodestreamBox(codestream []byte) *Box {
	return &Box{
		Type:     TypeContCodestream,
		Length:   uint64(8 + len(codestream)),
		Contents: codestream,
	}
}
