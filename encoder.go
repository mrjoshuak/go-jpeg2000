package jpeg2000

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"runtime"
	"sync"

	"github.com/mrjoshuak/go-jpeg2000/internal/box"
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/mct"
)

// encoder handles JPEG 2000 encoding.
type encoder struct {
	w       io.Writer
	img     image.Image
	options *Options

	// Image parameters
	width              int
	height             int
	numComponents      int
	componentPrecision []int
	componentSigned    []bool

	// Component data
	componentData [][]int32

	// Float encoding state
	isFloat  bool
	floatImg *FloatImage
}

// maxPrecision returns the maximum precision across all components.
func (e *encoder) maxPrecision() int {
	m := 0
	for _, p := range e.componentPrecision {
		if p > m {
			m = p
		}
	}
	return m
}

// newEncoder creates a new encoder.
func newEncoder(w io.Writer, img image.Image, options *Options) *encoder {
	bounds := img.Bounds()
	return &encoder{
		w:       w,
		img:     img,
		options: options,
		width:   bounds.Dx(),
		height:  bounds.Dy(),
	}
}

// encode encodes the image.
func (e *encoder) encode() error {
	// Extract image data
	if err := e.extractImageData(); err != nil {
		return fmt.Errorf("extracting image data: %w", err)
	}

	// Apply preprocessing
	if err := e.preprocess(); err != nil {
		return fmt.Errorf("preprocessing: %w", err)
	}

	// Generate codestream
	codestream, err := e.generateCodestream()
	if err != nil {
		return fmt.Errorf("generating codestream: %w", err)
	}

	// Write output based on format
	switch e.options.Format {
	case FormatJP2:
		return e.writeJP2(codestream)
	case FormatJ2K:
		_, err := e.w.Write(codestream)
		return err
	default:
		return fmt.Errorf("unsupported format: %s", e.options.Format)
	}
}

// extractImageData extracts pixel data from the source image.
func (e *encoder) extractImageData() error {
	bounds := e.img.Bounds()

	// Determine image properties based on type
	switch img := e.img.(type) {
	case *image.Gray:
		e.numComponents = 1
		e.componentPrecision = []int{8}
		e.componentSigned = []bool{false}
		e.componentData = make([][]int32, 1)
		e.componentData[0] = make([]int32, e.width*e.height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				e.componentData[0][idx] = int32(img.GrayAt(x, y).Y)
			}
		}

	case *image.Gray16:
		e.numComponents = 1
		e.componentPrecision = []int{16}
		e.componentSigned = []bool{false}
		e.componentData = make([][]int32, 1)
		e.componentData[0] = make([]int32, e.width*e.height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				e.componentData[0][idx] = int32(img.Gray16At(x, y).Y)
			}
		}

	case *image.RGBA:
		e.numComponents = 4
		e.componentPrecision = []int{8, 8, 8, 8}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBAAt(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.RGBA64:
		e.numComponents = 4
		e.componentPrecision = []int{16, 16, 16, 16}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBA64At(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.NRGBA:
		e.numComponents = 4
		e.componentPrecision = []int{8, 8, 8, 8}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.NRGBAAt(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.NRGBA64:
		e.numComponents = 4
		e.componentPrecision = []int{16, 16, 16, 16}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.NRGBA64At(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	default:
		// Generic fallback - check if color model supports alpha by testing
		// whether it can represent a fully transparent pixel.
		_, _, _, testA := e.img.ColorModel().Convert(color.Transparent).RGBA()
		hasAlpha := testA == 0

		if hasAlpha {
			e.numComponents = 4
			e.componentPrecision = []int{8, 8, 8, 8}
			e.componentSigned = []bool{false, false, false, false}
		} else {
			e.numComponents = 3
			e.componentPrecision = []int{8, 8, 8}
			e.componentSigned = []bool{false, false, false}
		}
		e.componentData = make([][]int32, e.numComponents)
		for c := 0; c < e.numComponents; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				r, g, b, a := e.img.At(x, y).RGBA()
				e.componentData[0][idx] = int32(r >> 8)
				e.componentData[1][idx] = int32(g >> 8)
				e.componentData[2][idx] = int32(b >> 8)
				if hasAlpha {
					e.componentData[3][idx] = int32(a >> 8)
				}
			}
		}
	}

	// Apply precision override if specified
	if e.options.Precision > 0 && e.options.Precision <= 16 {
		targetPrecision := e.options.Precision
		dstMax := int32((1 << targetPrecision) - 1)
		for c := 0; c < e.numComponents; c++ {
			if e.componentPrecision[c] == targetPrecision {
				continue
			}
			srcMax := int32((1 << e.componentPrecision[c]) - 1)
			for i := range e.componentData[c] {
				e.componentData[c][i] = e.componentData[c][i] * dstMax / srcMax
			}
			e.componentPrecision[c] = targetPrecision
		}
	}

	return nil
}

// extractFloatData extracts pixel data from a FloatImage, reinterpreting
// IEEE 754 float32 bits as int32 values for the integer wavelet pipeline.
func (e *encoder) extractFloatData() error {
	e.isFloat = true
	e.numComponents = len(e.floatImg.Components)
	e.componentPrecision = make([]int, e.numComponents)
	e.componentSigned = make([]bool, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentPrecision[c] = 32
		e.componentSigned[c] = true
	}
	e.width = e.floatImg.Width
	e.height = e.floatImg.Height

	e.componentData = make([][]int32, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentData[c] = make([]int32, e.width*e.height)
		for i, f := range e.floatImg.Components[c] {
			e.componentData[c][i] = int32(math.Float32bits(f))
		}
	}

	return nil
}

// encodeFloat encodes a FloatImage.
func (e *encoder) encodeFloat() error {
	if err := e.extractFloatData(); err != nil {
		return fmt.Errorf("extracting float data: %w", err)
	}

	if err := e.preprocess(); err != nil {
		return fmt.Errorf("preprocessing: %w", err)
	}

	cs, err := e.generateCodestream()
	if err != nil {
		return fmt.Errorf("generating codestream: %w", err)
	}

	switch e.options.Format {
	case FormatJP2:
		return e.writeJP2(cs)
	case FormatJ2K:
		_, err := e.w.Write(cs)
		return err
	default:
		return fmt.Errorf("unsupported format: %s", e.options.Format)
	}
}

// preprocess applies preprocessing transforms.
func (e *encoder) preprocess() error {
	// For float data, apply NLT Type 3 before anything else
	if e.isFloat {
		for c := 0; c < e.numComponents; c++ {
			nltType3(e.componentData[c])
		}
	}

	// Apply DC level shift per component (skip signed components)
	for c := 0; c < e.numComponents; c++ {
		if !e.componentSigned[c] {
			mct.DCLevelShiftForward(e.componentData[c], e.componentPrecision[c])
		}
	}

	// Apply MCT if we have 3+ components
	if e.numComponents >= 3 {
		if e.options.Lossless {
			if e.maxPrecision() > 16 {
				mct.ForwardRCT32(e.componentData[0], e.componentData[1], e.componentData[2])
			} else {
				mct.ForwardRCT(e.componentData[0], e.componentData[1], e.componentData[2])
			}
		} else {
			// Convert to float for ICT
			compFloat := make([][]float64, 3)
			for c := 0; c < 3; c++ {
				compFloat[c] = make([]float64, len(e.componentData[c]))
				for i, v := range e.componentData[c] {
					compFloat[c][i] = float64(v)
				}
			}
			mct.ForwardICT(compFloat[0], compFloat[1], compFloat[2])
			for c := 0; c < 3; c++ {
				for i, v := range compFloat[c] {
					if v >= 0 {
						e.componentData[c][i] = int32(v + 0.5)
					} else {
						e.componentData[c][i] = int32(v - 0.5)
					}
				}
			}
		}
	}

	// Apply DWT
	numLevels := e.options.NumResolutions - 1
	if numLevels <= 0 {
		numLevels = 5
	}

	for c := 0; c < e.numComponents; c++ {
		if e.options.Lossless {
			if e.maxPrecision() > 16 {
				dwt.DecomposeMultiLevel53_32bit(e.componentData[c], e.width, e.height, numLevels)
			} else {
				dwt.DecomposeMultiLevel53(e.componentData[c], e.width, e.height, numLevels)
			}
		} else {
			// Convert to float for 9-7 transform
			dataFloat := make([]float64, len(e.componentData[c]))
			for i, v := range e.componentData[c] {
				dataFloat[i] = float64(v)
			}
			dwt.DecomposeMultiLevel97(dataFloat, e.width, e.height, numLevels)
			// Convert back with quantization
			quality := e.options.Quality
			if quality <= 0 {
				quality = 100 // Default to lossless if quality not set
			}
			stepSize := 1.0 / float64(quality)
			for i, v := range dataFloat {
				if v >= 0 {
					e.componentData[c][i] = int32(v/stepSize + 0.5)
				} else {
					e.componentData[c][i] = int32(v/stepSize - 0.5)
				}
			}
		}
	}

	return nil
}

// generateCodestream generates the JPEG 2000 codestream.
func (e *encoder) generateCodestream() ([]byte, error) {
	var buf []byte

	// SOC marker
	buf = append(buf, 0xFF, 0x4F)

	// SIZ marker
	siz := e.generateSIZ()
	buf = append(buf, siz...)

	// CAP marker (required for HTJ2K mode)
	if e.options.HighThroughput {
		cap := e.generateCAP()
		buf = append(buf, cap...)
	}

	// COD marker
	cod := e.generateCOD()
	buf = append(buf, cod...)

	// QCD marker
	qcd := e.generateQCD()
	buf = append(buf, qcd...)

	// NLT markers (for float encoding)
	if e.isFloat {
		nlt := e.generateNLT()
		buf = append(buf, nlt...)
	}

	// Comment marker (optional)
	if e.options.Comment != "" {
		com := e.generateCOM()
		buf = append(buf, com...)
	}

	// Generate tile data
	tileData, err := e.generateTiles()
	if err != nil {
		return nil, err
	}
	buf = append(buf, tileData...)

	// EOC marker
	buf = append(buf, 0xFF, 0xD9)

	return buf, nil
}

// generateSIZ generates the SIZ marker segment.
func (e *encoder) generateSIZ() []byte {
	numComp := e.numComponents

	// Length = 38 + 3*numComponents
	length := 38 + 3*numComp

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.SIZ))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	// Rsiz (profile)
	binary.BigEndian.PutUint16(buf[4:6], uint16(e.options.Profile))

	// Image dimensions
	binary.BigEndian.PutUint32(buf[6:10], uint32(e.width))
	binary.BigEndian.PutUint32(buf[10:14], uint32(e.height))

	// Image offset (0, 0)
	binary.BigEndian.PutUint32(buf[14:18], 0)
	binary.BigEndian.PutUint32(buf[18:22], 0)

	// Tile size
	tileWidth := e.width
	tileHeight := e.height
	if e.options.TileSize.X > 0 {
		tileWidth = e.options.TileSize.X
	}
	if e.options.TileSize.Y > 0 {
		tileHeight = e.options.TileSize.Y
	}
	binary.BigEndian.PutUint32(buf[22:26], uint32(tileWidth))
	binary.BigEndian.PutUint32(buf[26:30], uint32(tileHeight))

	// Tile offset
	binary.BigEndian.PutUint32(buf[30:34], 0)
	binary.BigEndian.PutUint32(buf[34:38], 0)

	// Number of components
	binary.BigEndian.PutUint16(buf[38:40], uint16(numComp))

	// Component info
	for c := 0; c < numComp; c++ {
		offset := 40 + c*3
		// Ssiz: bit depth (precision - 1, with sign bit)
		ssiz := uint8(e.componentPrecision[c] - 1)
		if e.componentSigned[c] {
			ssiz |= 0x80
		}
		buf[offset] = ssiz
		// XRsiz, YRsiz: subsampling
		buf[offset+1] = 1
		buf[offset+2] = 1
	}

	return buf
}

// generateCOD generates the COD marker segment.
func (e *encoder) generateCOD() []byte {
	numRes := e.options.NumResolutions
	if numRes <= 0 {
		numRes = 6
	}

	// Base length = 12 (without precinct sizes)
	length := 12

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.COD))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	// Scod: coding style
	scod := uint8(0)
	if e.options.EnableSOP {
		scod |= codestream.CodingStyleSOP
	}
	if e.options.EnableEPH {
		scod |= codestream.CodingStyleEPH
	}
	buf[4] = scod

	// SGcod
	buf[5] = uint8(e.options.ProgressionOrder) // Progression order
	numLayers := e.options.NumLayers
	if numLayers <= 0 {
		numLayers = 1
	}
	binary.BigEndian.PutUint16(buf[6:8], uint16(numLayers))
	buf[8] = 1 // MCT (enabled for 3 components)

	// SPcod
	buf[9] = uint8(numRes - 1) // Number of decomposition levels

	// Determine code block size
	cbWidth := e.options.CodeBlockSize.X
	cbHeight := e.options.CodeBlockSize.Y

	// In HTJ2K mode, use HTJ2K-specific block sizes if specified
	if e.options.HighThroughput {
		// HTJ2K defaults to 128x128 blocks, but OpenEXR also supports 32x32
		htWidth := e.options.HTBlockWidth
		htHeight := e.options.HTBlockHeight
		if htWidth == 0 {
			htWidth = 128 // Default HTJ2K block width
		}
		if htHeight == 0 {
			htHeight = 128 // Default HTJ2K block height
		}
		// Convert to log2 exponent (32->5, 64->6, 128->7)
		switch htWidth {
		case 32:
			cbWidth = 5
		case 128:
			cbWidth = 7
		default:
			cbWidth = 7 // Default to 128
		}
		switch htHeight {
		case 32:
			cbHeight = 5
		case 128:
			cbHeight = 7
		default:
			cbHeight = 7 // Default to 128
		}
	} else {
		// Standard mode defaults
		if cbWidth <= 0 {
			cbWidth = 6
		}
		if cbHeight <= 0 {
			cbHeight = 6
		}
	}

	buf[10] = uint8(cbWidth - 2)  // Code-block width exponent
	buf[11] = uint8(cbHeight - 2) // Code-block height exponent

	// Code-block style flags
	cbStyle := uint8(0)
	if e.options.HighThroughput {
		cbStyle |= codestream.CodeBlockHT // Set HTJ2K flag (0x40)
	}
	buf[12] = cbStyle

	if e.options.Lossless {
		buf[13] = 1 // 5-3 reversible wavelet
	} else {
		buf[13] = 0 // 9-7 irreversible wavelet
	}

	return buf
}

// generateQCD generates the QCD marker segment.
func (e *encoder) generateQCD() []byte {
	numRes := e.options.NumResolutions
	if numRes <= 0 {
		numRes = 6
	}

	// Calculate number of subbands
	numBands := 3*(numRes-1) + 1

	var buf []byte
	if e.options.Lossless {
		// No quantization
		length := 3 + numBands
		buf = make([]byte, 2+length)
		binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.QCD))
		binary.BigEndian.PutUint16(buf[2:4], uint16(length))

		// Sqcd: no quantization, guard bits
		maxPrec := e.maxPrecision()
		guardBits := uint8(0)
		if maxPrec > 16 {
			guardBits = 2 // need more guard bits for 32-bit
		}
		buf[4] = codestream.QuantizationNone | (guardBits << 5)

		// SPqcd: one exponent per subband
		for i := 0; i < numBands; i++ {
			// Default exponent based on subband level
			exp := maxPrec + i/3
			if exp > 31 {
				exp = 31 // clamp to 5-bit range
			}
			buf[5+i] = uint8(exp) << 3
		}
	} else {
		// Scalar derived quantization
		length := 5
		buf = make([]byte, 2+length)
		binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.QCD))
		binary.BigEndian.PutUint16(buf[2:4], uint16(length))

		// Sqcd: scalar derived, 1 guard bit
		buf[4] = codestream.QuantizationScalarDerived | (1 << 5)

		// Base step size
		stepSize := uint16(0x4000) // Default step size
		if e.options.Quality > 0 {
			// Adjust based on quality
			stepSize = uint16((100 - e.options.Quality) * 256)
		}
		binary.BigEndian.PutUint16(buf[5:7], stepSize)
	}

	return buf
}

// generateCOM generates the COM marker segment.
func (e *encoder) generateCOM() []byte {
	comment := []byte(e.options.Comment)
	length := 4 + len(comment)

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.COM))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))
	binary.BigEndian.PutUint16(buf[4:6], codestream.CommentLatin1)
	copy(buf[6:], comment)

	return buf
}

// generateCAP generates the CAP (extended capabilities) marker segment.
// This marker is required for HTJ2K mode to signal the use of the
// High-Throughput block coder.
func (e *encoder) generateCAP() []byte {
	// CAP marker format:
	// - Marker (2 bytes): 0xFF50
	// - Length (2 bytes): 6 (length field + Pcap)
	// - Pcap (4 bytes): capabilities flags
	// Total: 8 bytes

	length := 6 // Length includes itself and Pcap

	buf := make([]byte, 8)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.CAP))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	// Set Pcap with HTJ2K capability flag (bit 15)
	pcap := codestream.CapPcapHTJ2K
	binary.BigEndian.PutUint32(buf[4:8], pcap)

	return buf
}

// generateNLT generates NLT marker segments for float encoding.
// One NLT marker is written per component.
func (e *encoder) generateNLT() []byte {
	var buf []byte
	for c := 0; c < e.numComponents; c++ {
		// NLT marker: 0xFF73
		// Length: 5 (includes length field itself)
		// Cnlt: component index (1 byte)
		// BDnlt: bit depth (1 byte) - 0x9F = signed (bit 7) + 31 (32-1)
		// Tnlt: transform type (1 byte) - 3 = type 3
		marker := make([]byte, 7)
		binary.BigEndian.PutUint16(marker[0:2], uint16(codestream.NLT))
		binary.BigEndian.PutUint16(marker[2:4], 5) // length
		marker[4] = uint8(c)                       // component index
		bdnlt := uint8(e.componentPrecision[c] - 1)
		if e.componentSigned[c] {
			bdnlt |= 0x80
		}
		marker[5] = bdnlt
		marker[6] = 3                              // NLT type 3
		buf = append(buf, marker...)
	}
	return buf
}

// generateTiles generates tile data.
func (e *encoder) generateTiles() ([]byte, error) {
	var buf []byte

	// For now, single tile (entire image)
	tileData, err := e.encodeTile(0)
	if err != nil {
		return nil, err
	}
	buf = append(buf, tileData...)

	return buf, nil
}

// codeBlockJob represents a code-block encoding job for parallel processing.
type codeBlockJob struct {
	index    int // Order in output
	data     []int32
	width    int
	height   int
	bandType int
}

// codeBlockResult holds the encoded result.
type codeBlockResult struct {
	index       int
	encoded     []byte
	numBPS      int
	truncPoints []int // byte position after each complete bit-plane
}

// cbMeta holds per-code-block metadata for the tile data table.
type cbMeta struct {
	numBPS  uint8
	dataLen uint32
}

// computeNumBPS computes the number of bit-planes from absolute values.
func computeNumBPS(data []int32) int {
	maxVal := int32(0)
	for _, v := range data {
		abs := v
		if abs < 0 {
			if abs == math.MinInt32 {
				abs = math.MaxInt32
			} else {
				abs = -abs
			}
		}
		if abs > maxVal {
			maxVal = abs
		}
	}
	if maxVal == 0 {
		return 0
	}
	numBPS := 0
	for maxVal > 0 {
		numBPS++
		maxVal >>= 1
	}
	return numBPS
}

// subbandOffset computes the (x, y) offset of a subband within the
// DWT-decomposed data array for a given resolution and band type.
func (e *encoder) subbandOffset(res, bandType int) (int, int) {
	numRes := e.options.NumResolutions
	if numRes <= 0 {
		numRes = 6
	}
	return computeSubbandOffset(e.width, e.height, numRes, res, bandType)
}

// computeSubbandOffset computes the (x, y) offset of a subband within the
// DWT-decomposed data array. Used by both encoder and decoder.
func computeSubbandOffset(width, height, numRes, res, bandType int) (int, int) {
	if res == 0 {
		return 0, 0
	}
	decompLevel := numRes - 1 - res
	w, h := width, height
	for i := 0; i < decompLevel; i++ {
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	halfW := (w + 1) / 2
	halfH := (h + 1) / 2
	switch bandType {
	case entropy.BandHL:
		return halfW, 0
	case entropy.BandLH:
		return 0, halfH
	case entropy.BandHH:
		return halfW, halfH
	default:
		return 0, 0
	}
}

// buildTileData constructs tile data with a metadata table followed by
// concatenated code-block encoded data. Format:
//
//	uint16:  numCodeBlocks
//	Per CB:  uint8 numBPS + uint32 dataLen
//	Then:    concatenated encoded bytes
func buildTileData(metas []cbMeta, encoded []byte) []byte {
	numCB := len(metas)
	tableSize := 2 + numCB*5
	tileData := make([]byte, tableSize+len(encoded))
	tileData[0] = byte(numCB >> 8)
	tileData[1] = byte(numCB)
	for i, m := range metas {
		off := 2 + i*5
		tileData[off] = m.numBPS
		tileData[off+1] = byte(m.dataLen >> 24)
		tileData[off+2] = byte(m.dataLen >> 16)
		tileData[off+3] = byte(m.dataLen >> 8)
		tileData[off+4] = byte(m.dataLen)
	}
	copy(tileData[tableSize:], encoded)
	return tileData
}

// buildMultiLayerTileData constructs tile data with per-layer cumulative byte
// counts for each code-block. Format:
//
//	uint16:  numCodeBlocks
//	uint8:   numLayers
//	Per CB:  uint8 numBPS + numLayers*uint32 cumulativeLen
//	Then:    concatenated encoded bytes
//
// Each cumulativeLen[i] gives the number of bytes from this code-block
// that belong to layers 0..i. Bit-planes are distributed evenly across
// layers, with earlier layers getting the most significant bit-planes.
func buildMultiLayerTileData(metas []cbMeta, truncPoints [][]int, encoded []byte, numLayers int) []byte {
	numCB := len(metas)
	tableSize := 2 + 1 + numCB*(1+numLayers*4)
	tileData := make([]byte, tableSize+len(encoded))
	tileData[0] = byte(numCB >> 8)
	tileData[1] = byte(numCB)
	tileData[2] = byte(numLayers)
	for i, m := range metas {
		off := 3 + i*(1+numLayers*4)
		tileData[off] = m.numBPS
		nbps := int(m.numBPS)
		tp := truncPoints[i]
		for lay := 0; lay < numLayers; lay++ {
			var cumLen uint32
			if nbps == 0 {
				cumLen = 0
			} else {
				// Distribute bit-planes across layers proportionally.
				bpCount := (lay + 1) * nbps / numLayers
				if bpCount < 1 {
					bpCount = 1 // always include at least the MSB
				}
				if bpCount > nbps {
					bpCount = nbps
				}
				if bpCount > 0 && len(tp) >= bpCount {
					cumLen = uint32(tp[bpCount-1])
				} else if bpCount > 0 {
					cumLen = m.dataLen
				}
			}
			loff := off + 1 + lay*4
			tileData[loff] = byte(cumLen >> 24)
			tileData[loff+1] = byte(cumLen >> 16)
			tileData[loff+2] = byte(cumLen >> 8)
			tileData[loff+3] = byte(cumLen)
		}
	}
	copy(tileData[tableSize:], encoded)
	return tileData
}

// encodeTile encodes a single tile using parallel code-block encoding.
func (e *encoder) encodeTile(tileIdx int) ([]byte, error) {
	// Collect all code-block jobs
	var jobs []codeBlockJob

	numRes := e.options.NumResolutions
	if numRes <= 0 {
		numRes = 6
	}

	// Compute code-block size from options (must match generateCOD).
	// CodeBlockSize.X/Y are the log2 exponent of the actual block size.
	cbWidthExp := e.options.CodeBlockSize.X
	cbHeightExp := e.options.CodeBlockSize.Y
	if cbWidthExp <= 0 {
		cbWidthExp = 6 // default: 2^6 = 64
	}
	if cbHeightExp <= 0 {
		cbHeightExp = 6
	}
	cbWidth := 1 << cbWidthExp
	cbHeight := 1 << cbHeightExp

	// First pass: collect all code-block jobs
	for c := 0; c < e.numComponents; c++ {
		for r := 0; r < numRes; r++ {
			var numBands int
			if r == 0 {
				numBands = 1 // LL only
			} else {
				numBands = 3 // HL, LH, HH
			}

			for b := 0; b < numBands; b++ {
				bandType := entropy.BandLL
				if r > 0 {
					switch b {
					case 0:
						bandType = entropy.BandHL
					case 1:
						bandType = entropy.BandLH
					case 2:
						bandType = entropy.BandHH
					}
				}

				scale := 1 << (numRes - 1 - r)
				bandWidth := (e.width + scale - 1) / scale
				bandHeight := (e.height + scale - 1) / scale

				if r > 0 {
					bandWidth = (bandWidth + 1) / 2
					bandHeight = (bandHeight + 1) / 2
				}

				for cby := 0; cby*cbHeight < bandHeight; cby++ {
					for cbx := 0; cbx*cbWidth < bandWidth; cbx++ {
						actualWidth := cbWidth
						actualHeight := cbHeight
						startX := cbx * cbWidth
						startY := cby * cbHeight
						if startX+actualWidth > bandWidth {
							actualWidth = bandWidth - startX
						}
						if startY+actualHeight > bandHeight {
							actualHeight = bandHeight - startY
						}

						cbData := e.extractCodeBlockData(c, r, bandType, cbx, cby, cbWidth, cbHeight, bandWidth, bandHeight)

						jobs = append(jobs, codeBlockJob{
							index:    len(jobs),
							data:     cbData,
							width:    actualWidth,
							height:   actualHeight,
							bandType: bandType,
						})
					}
				}
			}
		}
	}

	numLayers := e.options.NumLayers
	if numLayers <= 0 {
		numLayers = 1
	}

	// Sequential encoding for small job counts or single-threaded mode
	// Set GOMAXPROCS=1 to force single-threaded encoding
	if len(jobs) <= 4 || runtime.GOMAXPROCS(0) == 1 {
		var metas []cbMeta
		var allTruncPoints [][]int
		var allEncoded []byte
		t1 := entropy.GetT1(64, 64)
		for _, job := range jobs {
			numBPS := computeNumBPS(job.data)
			t1.Resize(job.width, job.height)
			t1.SetData(job.data)
			encoded := t1.Encode(job.bandType)
			metas = append(metas, cbMeta{numBPS: uint8(numBPS), dataLen: uint32(len(encoded))})
			tp := t1.TruncationPoints()
			tpCopy := make([]int, len(tp))
			copy(tpCopy, tp)
			allTruncPoints = append(allTruncPoints, tpCopy)
			allEncoded = append(allEncoded, encoded...)
		}
		entropy.PutT1(t1)
		var tileData []byte
		if numLayers > 1 {
			tileData = buildMultiLayerTileData(metas, allTruncPoints, allEncoded, numLayers)
		} else {
			tileData = buildTileData(metas, allEncoded)
		}
		return e.createTileHeader(tileIdx, tileData), nil
	}

	// Parallel encoding - use all available cores
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	// Pre-fill job channel before starting workers to reduce contention
	jobChan := make(chan codeBlockJob, len(jobs))
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	resultChan := make(chan codeBlockResult, len(jobs))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				numBPS := computeNumBPS(job.data)
				t1 := entropy.GetT1(job.width, job.height)
				t1.SetData(job.data)
				encoded := t1.Encode(job.bandType)
				// Copy encoded bytes before returning T1 to pool.
				// Encode returns a slice of the T1's internal mqBuf,
				// which would be overwritten when the T1 is reused.
				encodedCopy := make([]byte, len(encoded))
				copy(encodedCopy, encoded)
				entropy.PutT1(t1)
				tp := t1.TruncationPoints()
				tpCopy := make([]int, len(tp))
				copy(tpCopy, tp)
				resultChan <- codeBlockResult{
					index:       job.index,
					encoded:     encodedCopy,
					numBPS:      numBPS,
					truncPoints: tpCopy,
				}
			}
		}()
	}

	// Wait for completion
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results in order
	metas := make([]cbMeta, len(jobs))
	encodedBlocks := make([][]byte, len(jobs))
	allTruncPoints := make([][]int, len(jobs))
	for result := range resultChan {
		metas[result.index] = cbMeta{numBPS: uint8(result.numBPS), dataLen: uint32(len(result.encoded))}
		encodedBlocks[result.index] = result.encoded
		allTruncPoints[result.index] = result.truncPoints
	}

	// Build tile data with metadata table + encoded bytes
	var allEncoded []byte
	for _, encoded := range encodedBlocks {
		allEncoded = append(allEncoded, encoded...)
	}
	var tileData []byte
	if numLayers > 1 {
		tileData = buildMultiLayerTileData(metas, allTruncPoints, allEncoded, numLayers)
	} else {
		tileData = buildTileData(metas, allEncoded)
	}
	return e.createTileHeader(tileIdx, tileData), nil
}

// createTileHeader creates the tile-part header.
func (e *encoder) createTileHeader(tileIdx int, tileData []byte) []byte {
	sotLength := 10
	tilePartLength := uint32(14 + len(tileData))

	header := make([]byte, 14)
	binary.BigEndian.PutUint16(header[0:2], uint16(codestream.SOT))
	binary.BigEndian.PutUint16(header[2:4], uint16(sotLength))
	binary.BigEndian.PutUint16(header[4:6], uint16(tileIdx))
	binary.BigEndian.PutUint32(header[6:10], tilePartLength)
	header[10] = 0 // Tile-part index
	header[11] = 1 // Number of tile-parts
	binary.BigEndian.PutUint16(header[12:14], uint16(codestream.SOD))

	return append(header, tileData...)
}

// extractCodeBlockData extracts data for a code-block from the
// DWT-decomposed component data, accounting for subband offsets.
func (e *encoder) extractCodeBlockData(comp, res, bandType, cbx, cby, cbWidth, cbHeight, bandWidth, bandHeight int) []int32 {
	actualWidth := cbWidth
	actualHeight := cbHeight
	startX := cbx * cbWidth
	startY := cby * cbHeight

	if startX+actualWidth > bandWidth {
		actualWidth = bandWidth - startX
	}
	if startY+actualHeight > bandHeight {
		actualHeight = bandHeight - startY
	}

	// Get subband offset in the DWT-decomposed data array
	xOff, yOff := e.subbandOffset(res, bandType)

	data := make([]int32, actualWidth*actualHeight)
	for y := 0; y < actualHeight; y++ {
		for x := 0; x < actualWidth; x++ {
			srcX := xOff + startX + x
			srcY := yOff + startY + y
			if srcX < e.width && srcY < e.height {
				srcIdx := srcY*e.width + srcX
				if srcIdx < len(e.componentData[comp]) {
					data[y*actualWidth+x] = e.componentData[comp][srcIdx]
				}
			}
		}
	}

	return data
}

// writeJP2 writes a JP2 file.
func (e *encoder) writeJP2(codestream []byte) error {
	boxWriter := box.NewWriter(e.w)

	// Write signature
	if err := boxWriter.WriteSignature(); err != nil {
		return err
	}

	// Write file type box
	ftypBox := box.CreateFileTypeBox()
	if err := boxWriter.WriteBox(ftypBox); err != nil {
		return err
	}

	// Determine colorspace from options or default based on components
	var colorspace uint32
	switch e.options.ColorSpace {
	case ColorSpaceBilevel:
		colorspace = box.CSBilevel1
	case ColorSpaceGray:
		colorspace = box.CSGray
	case ColorSpaceSRGB:
		colorspace = box.CSSRGB
	case ColorSpaceSYCC:
		colorspace = box.CSYCbCr1
	case ColorSpaceYCbCr2:
		colorspace = box.CSYCbCr2
	case ColorSpaceYCbCr3:
		colorspace = box.CSYCbCr3
	case ColorSpacePhotoYCC:
		colorspace = box.CSPhotoYCC
	case ColorSpaceCMY:
		colorspace = box.CSCMY
	case ColorSpaceCMYK:
		colorspace = box.CSCMYK
	case ColorSpaceYCCK:
		colorspace = box.CSYCCK
	case ColorSpaceCIELab:
		colorspace = box.CSCIELab
	case ColorSpaceCIEJab:
		colorspace = box.CSCIEJab
	case ColorSpaceESRGB:
		colorspace = box.CSeSRGB
	case ColorSpaceROMMRGB:
		colorspace = box.CSROMMRGB
	case ColorSpaceYPbPr60:
		colorspace = box.CSYPbPr1125
	case ColorSpaceYPbPr50:
		colorspace = box.CSYPbPr1250
	case ColorSpaceEYCC:
		colorspace = box.CSeSYCC
	default:
		// Default based on number of components
		if e.numComponents == 1 {
			colorspace = box.CSGray
		} else {
			// 3 or 4 components default to sRGB (4th component is alpha)
			colorspace = box.CSSRGB
		}
	}

	// Write JP2 header
	jp2hBox := box.CreateJP2Header(
		uint32(e.width),
		uint32(e.height),
		uint16(e.numComponents),
		uint8(e.maxPrecision()-1),
		colorspace,
	)
	if err := boxWriter.WriteBox(jp2hBox); err != nil {
		return err
	}

	// Write codestream
	jp2cBox := box.CreateCodestreamBox(codestream)
	if err := boxWriter.WriteBox(jp2cBox); err != nil {
		return err
	}

	return nil
}

