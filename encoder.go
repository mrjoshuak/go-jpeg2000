package jpeg2000

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
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
	width         int
	height        int
	numComponents int
	precision     int
	signed        bool

	// Component data
	componentData [][]int32
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
		e.precision = 8
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
		e.precision = 16
		e.componentData = make([][]int32, 1)
		e.componentData[0] = make([]int32, e.width*e.height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				e.componentData[0][idx] = int32(img.Gray16At(x, y).Y)
			}
		}

	case *image.RGBA:
		e.numComponents = 3 // We'll ignore alpha for now
		e.precision = 8
		e.componentData = make([][]int32, 3)
		for c := 0; c < 3; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBAAt(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
			}
		}

	case *image.RGBA64:
		e.numComponents = 3
		e.precision = 16
		e.componentData = make([][]int32, 3)
		for c := 0; c < 3; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBA64At(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
			}
		}

	case *image.NRGBA:
		e.numComponents = 4
		e.precision = 8
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
		e.precision = 16
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
		// Generic fallback - convert to RGBA
		e.numComponents = 3
		e.precision = 8
		e.componentData = make([][]int32, 3)
		for c := 0; c < 3; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				r, g, b, _ := e.img.At(x, y).RGBA()
				e.componentData[0][idx] = int32(r >> 8)
				e.componentData[1][idx] = int32(g >> 8)
				e.componentData[2][idx] = int32(b >> 8)
			}
		}
	}

	// Apply precision override if specified
	if e.options.Precision > 0 && e.options.Precision <= 16 && e.options.Precision != e.precision {
		targetPrecision := e.options.Precision
		srcMax := int32((1 << e.precision) - 1)
		dstMax := int32((1 << targetPrecision) - 1)

		for c := 0; c < e.numComponents; c++ {
			for i := range e.componentData[c] {
				// Scale from source precision to target precision
				e.componentData[c][i] = e.componentData[c][i] * dstMax / srcMax
			}
		}
		e.precision = targetPrecision
	}

	return nil
}

// preprocess applies preprocessing transforms.
func (e *encoder) preprocess() error {
	// Apply DC level shift
	for c := 0; c < e.numComponents; c++ {
		mct.DCLevelShiftForward(e.componentData[c], e.precision)
	}

	// Apply MCT if we have 3+ components
	if e.numComponents >= 3 {
		if e.options.Lossless {
			mct.ForwardRCT(e.componentData[0], e.componentData[1], e.componentData[2])
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
			dwt.DecomposeMultiLevel53(e.componentData[c], e.width, e.height, numLevels)
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
		ssiz := uint8(e.precision - 1)
		if e.signed {
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

		// Sqcd: no quantization, 0 guard bits
		buf[4] = codestream.QuantizationNone

		// SPqcd: one exponent per subband
		for i := 0; i < numBands; i++ {
			// Default exponent based on subband level
			buf[5+i] = uint8(e.precision + i/3) << 3
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
	index       int    // Order in output
	data        []int32
	width       int
	height      int
	bandType    int
}

// codeBlockResult holds the encoded result.
type codeBlockResult struct {
	index   int
	encoded []byte
}

// encodeTile encodes a single tile using parallel code-block encoding.
func (e *encoder) encodeTile(tileIdx int) ([]byte, error) {
	// Collect all code-block jobs
	var jobs []codeBlockJob

	numRes := e.options.NumResolutions
	if numRes <= 0 {
		numRes = 6
	}

	cbWidth := 1 << (e.options.CodeBlockSize.X + 2)
	cbHeight := 1 << (e.options.CodeBlockSize.Y + 2)
	if cbWidth <= 0 {
		cbWidth = 64
	}
	if cbHeight <= 0 {
		cbHeight = 64
	}

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

	// Sequential encoding for small job counts or single-threaded mode
	// Set GOMAXPROCS=1 to force single-threaded encoding
	if len(jobs) <= 4 || runtime.GOMAXPROCS(0) == 1 {
		var tileData []byte
		t1 := entropy.GetT1(64, 64)
		for _, job := range jobs {
			t1.Resize(job.width, job.height)
			t1.SetData(job.data)
			encoded := t1.Encode(job.bandType)
			tileData = append(tileData, encoded...)
		}
		entropy.PutT1(t1)
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
				t1 := entropy.GetT1(job.width, job.height)
				t1.SetData(job.data)
				encoded := t1.Encode(job.bandType)
				entropy.PutT1(t1)
				resultChan <- codeBlockResult{
					index:   job.index,
					encoded: encoded,
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
	results := make([][]byte, len(jobs))
	for result := range resultChan {
		results[result.index] = result.encoded
	}

	// Combine results in order
	var tileData []byte
	for _, encoded := range results {
		tileData = append(tileData, encoded...)
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

// extractCodeBlockData extracts data for a code-block.
func (e *encoder) extractCodeBlockData(comp, res, bandType, cbx, cby, cbWidth, cbHeight, bandWidth, bandHeight int) []int32 {
	// Calculate actual code-block size (may be smaller at edges)
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

	data := make([]int32, actualWidth*actualHeight)

	// Extract from component data
	// This is simplified - actual implementation would need proper subband addressing
	for y := 0; y < actualHeight; y++ {
		for x := 0; x < actualWidth; x++ {
			srcX := startX + x
			srcY := startY + y
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
		uint8(e.precision-1),
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

// Ensure encoder implements required interfaces
var _ color.Model = (*encoder)(nil).colorModel()

func (e *encoder) colorModel() color.Model {
	return nil
}
