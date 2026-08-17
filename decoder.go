package jpeg2000

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"

	"github.com/mrjoshuak/go-jpeg2000/internal/box"
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/mct"
	"github.com/mrjoshuak/go-jpeg2000/internal/tcd"
)

// decoder handles JPEG 2000 decoding.
type decoder struct {
	r          *bufio.Reader
	format     Format
	header     *codestream.Header
	jp2Header  *box.JP2Header
	codestream []byte
}

// newDecoder creates a new decoder.
func newDecoder(r io.Reader) *decoder {
	return &decoder{
		r: bufio.NewReader(r),
	}
}

// decode decodes the image.
func (d *decoder) decode(cfg *Config) (image.Image, error) {
	// Detect format and read headers
	if err := d.readFormat(); err != nil {
		return nil, fmt.Errorf("reading format: %w", err)
	}

	// Parse codestream header
	if err := d.parseCodestream(); err != nil {
		return nil, fmt.Errorf("parsing codestream: %w", err)
	}

	// Decode tiles
	img, err := d.decodeTiles(cfg)
	if err != nil {
		return nil, fmt.Errorf("decoding tiles: %w", err)
	}

	return img, nil
}

// readMetadata reads only the metadata without decoding.
func (d *decoder) readMetadata() (*Metadata, error) {
	if err := d.readFormat(); err != nil {
		return nil, err
	}

	if err := d.parseCodestream(); err != nil {
		return nil, err
	}

	h := d.header
	m := &Metadata{
		Format:           d.format,
		Width:            int(h.ImageWidth - h.ImageXOffset),
		Height:           int(h.ImageHeight - h.ImageYOffset),
		NumComponents:    int(h.NumComponents),
		BitsPerComponent: make([]int, h.NumComponents),
		Signed:           make([]bool, h.NumComponents),
		Profile:          Profile(h.Profile),
		NumResolutions:   int(h.CodingStyle.NumDecompositions) + 1,
		NumQualityLayers: int(h.CodingStyle.NumLayers),
		TileWidth:        int(h.TileWidth),
		TileHeight:       int(h.TileHeight),
		NumTilesX:        int(h.NumTilesX),
		NumTilesY:        int(h.NumTilesY),
		Comment:          h.Comment,
		ColorSpace:       ColorSpaceUnspecified, // Default for J2K without JP2 container
	}

	for i, c := range h.ComponentInfo {
		m.BitsPerComponent[i] = c.Precision()
		m.Signed[i] = c.IsSigned()
	}

	// Get color space from JP2 header if available
	m.ColorSpace = d.getColorSpace()
	if d.jp2Header != nil && d.jp2Header.ColorSpec != nil {
		m.ICCProfile = d.jp2Header.ColorSpec.ICCProfile
	}

	return m, nil
}

// getColorSpace returns the ColorSpace from the JP2 header.
func (d *decoder) getColorSpace() ColorSpace {
	if d.jp2Header == nil || d.jp2Header.ColorSpec == nil {
		return ColorSpaceUnspecified
	}

	switch d.jp2Header.ColorSpec.EnumeratedColorspace {
	case box.CSBilevel1, box.CSBilevel2:
		return ColorSpaceBilevel
	case box.CSGray:
		return ColorSpaceGray
	case box.CSSRGB:
		return ColorSpaceSRGB
	case box.CSYCbCr1, box.CSsYCC:
		return ColorSpaceSYCC
	case box.CSYCbCr2:
		return ColorSpaceYCbCr2
	case box.CSYCbCr3:
		return ColorSpaceYCbCr3
	case box.CSPhotoYCC:
		return ColorSpacePhotoYCC
	case box.CSCMY:
		return ColorSpaceCMY
	case box.CSCMYK:
		return ColorSpaceCMYK
	case box.CSYCCK:
		return ColorSpaceYCCK
	case box.CSCIELab:
		return ColorSpaceCIELab
	case box.CSCIEJab:
		return ColorSpaceCIEJab
	case box.CSeSRGB:
		return ColorSpaceESRGB
	case box.CSROMMRGB:
		return ColorSpaceROMMRGB
	case box.CSYPbPr1125:
		return ColorSpaceYPbPr60
	case box.CSYPbPr1250:
		return ColorSpaceYPbPr50
	case box.CSeSYCC:
		return ColorSpaceEYCC
	default:
		return ColorSpaceUnknown
	}
}

// readFormat detects the file format and reads file-level structures.
func (d *decoder) readFormat() error {
	// Peek at first bytes to detect format
	magic, err := d.r.Peek(12)
	if err != nil {
		return err
	}

	// Check for JP2 signature
	if len(magic) >= 12 &&
		magic[0] == 0x00 && magic[1] == 0x00 && magic[2] == 0x00 && magic[3] == 0x0C &&
		magic[4] == 'j' && magic[5] == 'P' && magic[6] == ' ' && magic[7] == ' ' {
		d.format = FormatJP2
		return d.readJP2()
	}

	// Check for J2K codestream (SOC marker)
	if len(magic) >= 2 && magic[0] == 0xFF && magic[1] == 0x4F {
		d.format = FormatJ2K
		return d.readJ2K()
	}

	return fmt.Errorf("unrecognized file format")
}

// readJP2 reads a JP2 file.
func (d *decoder) readJP2() error {
	boxReader := box.NewReader(d.r)

	for {
		b, err := boxReader.ReadBox()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		switch b.Type {
		case box.TypeJP2Signature:
			// Verify signature
			if len(b.Contents) < 4 ||
				b.Contents[0] != 0x0D || b.Contents[1] != 0x0A ||
				b.Contents[2] != 0x87 || b.Contents[3] != 0x0A {
				return fmt.Errorf("invalid JP2 signature")
			}

		case box.TypeFileType:
			// Parse file type box
			ftyp := &box.FileTypeBox{}
			if err := ftyp.Parse(b.Contents); err != nil {
				return err
			}

		case box.TypeJP2Header:
			// Parse JP2 header
			var err error
			d.jp2Header, err = box.ParseJP2Header(b.Contents)
			if err != nil {
				return err
			}

		case box.TypeContCodestream:
			// Store codestream for later parsing
			d.codestream = b.Contents
			return nil
		}
	}

	if d.codestream == nil {
		return fmt.Errorf("no codestream found in JP2 file")
	}
	return nil
}

// readJ2K reads a raw J2K codestream.
func (d *decoder) readJ2K() error {
	// Read entire codestream
	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	d.codestream = data
	return nil
}

// parseCodestream parses the codestream header.
func (d *decoder) parseCodestream() error {
	if d.codestream == nil {
		return fmt.Errorf("no codestream available")
	}

	parser := codestream.NewParser(bytes.NewReader(d.codestream))
	header, err := parser.ReadHeader()
	if err != nil {
		return err
	}
	d.header = header
	return nil
}

// reducedDimension computes the output dimension after reducing N resolution levels.
func reducedDimension(size, reduce int) int {
	for i := 0; i < reduce; i++ {
		size = (size + 1) / 2
	}
	return size
}

// decodeTiles decodes all tiles and assembles the output image.
func (d *decoder) decodeTiles(cfg *Config) (image.Image, error) {
	h := d.header

	reduce := 0
	if cfg != nil && cfg.ReduceResolution > 0 {
		reduce = cfg.ReduceResolution
	}

	// Calculate output dimensions at reduced resolution
	width := reducedDimension(int(h.ImageWidth-h.ImageXOffset), reduce)
	height := reducedDimension(int(h.ImageHeight-h.ImageYOffset), reduce)

	// Create output image based on number of components
	numComp := int(h.NumComponents)
	if numComp == 0 || len(h.ComponentInfo) == 0 {
		return nil, fmt.Errorf("invalid image: no components")
	}
	precision := h.ComponentInfo[0].Precision()
	signed := h.ComponentInfo[0].IsSigned()

	// Allocate component data
	componentData := make([][]int32, numComp)
	for c := 0; c < numComp; c++ {
		componentData[c] = make([]int32, width*height)
	}

	// Decode each tile
	tileDecoder := tcd.NewTileDecoder(h)
	if cfg != nil && cfg.QualityLayers > 0 {
		tileDecoder.SetQualityLayerLimit(cfg.QualityLayers)
	}
	if reduce > 0 {
		tileDecoder.SetReduceResolution(reduce)
	}
	numTiles := int(h.NumTilesX * h.NumTilesY)

	for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
		if err := d.decodeTile(tileDecoder, tileIdx, componentData, width, height, cfg); err != nil {
			return nil, fmt.Errorf("decoding tile %d: %w", tileIdx, err)
		}
	}

	// Check if any component has NLT (float mode)
	hasNLT := false
	for c := 0; c < numComp; c++ {
		if h.HasNLT(c) {
			hasNLT = true
			break
		}
	}

	// Apply inverse MCT if needed
	if h.CodingStyle.MultipleComponentXf != 0 && numComp >= 3 {
		if h.CodingStyle.IsReversible() {
			if hasNLT && precision > 16 {
				mct.InverseRCT32(componentData[0], componentData[1], componentData[2])
			} else {
				mct.InverseRCT(componentData[0], componentData[1], componentData[2])
			}
		} else {
			// Convert to float for ICT
			compFloat := make([][]float64, 3)
			for c := 0; c < 3; c++ {
				compFloat[c] = make([]float64, len(componentData[c]))
				for i, v := range componentData[c] {
					compFloat[c][i] = float64(v)
				}
			}
			mct.InverseICT(compFloat[0], compFloat[1], compFloat[2])
			for c := 0; c < 3; c++ {
				for i, v := range compFloat[c] {
					componentData[c][i] = int32(v + 0.5)
				}
			}
		}
	}

	// Apply DC level shift
	for c := 0; c < numComp; c++ {
		if !h.ComponentInfo[c].IsSigned() {
			mct.DCLevelShiftInverse(componentData[c], h.ComponentInfo[c].Precision())
		}
	}

	// Apply inverse NLT after DC shift
	if hasNLT {
		if err := d.inverseNLT(componentData); err != nil {
			return nil, err
		}
	}

	// Apply color space conversion if needed
	if d.jp2Header != nil && d.jp2Header.ColorSpec != nil {
		cs := d.getColorSpace()
		if conv := getColorConversion(cs); conv != nil {
			conv(componentData, precision)
		}
	}

	// Create output image
	return d.createImage(componentData, width, height, numComp, precision, signed)
}

// decodeTile decodes a single tile.
func (d *decoder) decodeTile(
	tileDecoder *tcd.TileDecoder,
	tileIdx int,
	componentData [][]int32,
	imgWidth, imgHeight int,
	cfg *Config,
) error {
	h := d.header

	// Initialize tile (TileDecoder handles resolution reduction internally)
	tileDecoder.InitTile(tileIdx)

	tile := tileDecoder.Tile()
	if tile == nil {
		return fmt.Errorf("tile %d not initialized", tileIdx)
	}

	// Decode tile data from codestream (fill subband coefficients)
	qualityLimit := 0
	if cfg != nil && cfg.QualityLayers > 0 {
		qualityLimit = cfg.QualityLayers
	}
	if err := d.decodeTileData(tile, tileIdx, qualityLimit); err != nil {
		return fmt.Errorf("decoding tile data: %w", err)
	}

	// Compute image offset at reduced resolution
	reduce := tileDecoder.ReduceResolution()
	imgXOff := reducedDimension(int(h.ImageXOffset), reduce)
	imgYOff := reducedDimension(int(h.ImageYOffset), reduce)

	// Apply inverse DWT and copy tile data to output
	for c := 0; c < len(tile.Components) && c < len(componentData); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}

		// Apply inverse DWT (uses reduced number of levels)
		tileDecoder.ApplyInverseDWT(tc)

		// Copy to output
		for y := tc.Y0; y < tc.Y1 && y-imgYOff < imgHeight; y++ {
			for x := tc.X0; x < tc.X1 && x-imgXOff < imgWidth; x++ {
				srcIdx := (y-tc.Y0)*(tc.X1-tc.X0) + (x - tc.X0)
				dstX := x - imgXOff
				dstY := y - imgYOff
				if dstX >= 0 && dstY >= 0 && dstX < imgWidth && dstY < imgHeight {
					dstIdx := dstY*imgWidth + dstX
					if srcIdx < len(tc.Data) {
						componentData[c][dstIdx] = tc.Data[srcIdx]
					}
				}
			}
		}
	}

	return nil
}

// findTileData locates the tile data bytes for a given tile index
// within the codestream. Returns nil if not found.
func (d *decoder) findTileData(tileIdx int) []byte {
	cs := d.codestream
	for i := 0; i < len(cs)-13; i++ {
		// Look for SOT marker (0xFF90)
		if cs[i] != 0xFF || cs[i+1] != 0x90 {
			continue
		}
		if i+14 > len(cs) {
			break
		}
		// Verify Lsot = 10
		lsot := binary.BigEndian.Uint16(cs[i+2 : i+4])
		if lsot != 10 {
			continue
		}
		// Check tile index
		isot := binary.BigEndian.Uint16(cs[i+4 : i+6])
		if int(isot) != tileIdx {
			continue
		}
		// Read tile-part length
		psot := binary.BigEndian.Uint32(cs[i+6 : i+10])
		// Verify SOD marker at i+12
		if cs[i+12] != 0xFF || cs[i+13] != 0x93 {
			continue
		}
		dataStart := i + 14
		dataEnd := dataStart
		if psot > 0 {
			dataEnd = i + int(psot)
		} else {
			dataEnd = len(cs)
		}
		if dataEnd > len(cs) {
			dataEnd = len(cs)
		}
		if dataStart >= dataEnd {
			return nil
		}
		return cs[dataStart:dataEnd]
	}
	return nil
}

// decodeTileData parses the tile data metadata table, decodes each
// code-block via T1, and places decoded coefficients into the tile
// component data arrays at the correct subband positions.
//
// This only works for codestreams produced by our encoder, which uses a
// custom metadata table format. For external codestreams (T2 packets),
// the function validates the format and returns nil if it doesn't match.
//
// qualityLimit > 0 limits decoding to that many quality layers (V2 format
// only; V1 format ignores this since it has no layer structure).
func (d *decoder) decodeTileData(tile *tcd.Tile, tileIdx int, qualityLimit int) error {
	tileData := d.findTileData(tileIdx)
	if len(tileData) < 2 {
		return nil // No tile data
	}

	h := d.header
	numRes := int(h.CodingStyle.NumDecompositions) + 1
	// Use same code-block size as encoder writes to COD
	cbWidth := h.CodingStyle.CodeBlockWidth()
	cbHeight := h.CodingStyle.CodeBlockHeight()

	// Compute expected number of code blocks to validate format
	expectedCB := 0
	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		tcWidth := tc.X1 - tc.X0
		tcHeight := tc.Y1 - tc.Y0
		for r := 0; r < numRes; r++ {
			numBands := 1
			if r > 0 {
				numBands = 3
			}
			for b := 0; b < numBands; b++ {
				scale := 1 << (numRes - 1 - r)
				bandW := (tcWidth + scale - 1) / scale
				bandH := (tcHeight + scale - 1) / scale
				if r > 0 {
					bandW = (bandW + 1) / 2
					bandH = (bandH + 1) / 2
				}
				for cby := 0; cby*cbHeight < bandH; cby++ {
					for cbx := 0; cbx*cbWidth < bandW; cbx++ {
						expectedCB++
					}
				}
			}
		}
	}

	// Parse metadata table — detect V1 vs V2 format.
	numCB := int(binary.BigEndian.Uint16(tileData[0:2]))
	if numCB != expectedCB {
		return nil // Not our format (likely T2 packets from external encoder)
	}

	// Check if this is V2 format (multi-layer): the header's NumLayers > 1
	// and a numLayers byte follows numCB.
	headerNumLayers := int(h.CodingStyle.NumLayers)
	isV2 := headerNumLayers > 1

	type decodeMeta struct {
		numBPS  int
		dataLen int // bytes to decode (may be truncated by quality limit)
		fullLen int // total bytes in the stream (for advancing dataPos)
	}
	metas := make([]decodeMeta, numCB)
	var metaSize int

	if isV2 {
		if len(tileData) < 3 {
			return nil
		}
		numLayers := int(tileData[2])
		metaSize = 3 + numCB*(1+numLayers*4)
		if len(tileData) < metaSize {
			return nil
		}
		// Determine effective layer limit
		effLayer := numLayers
		if qualityLimit > 0 && qualityLimit < numLayers {
			effLayer = qualityLimit
		}
		for i := 0; i < numCB; i++ {
			off := 3 + i*(1+numLayers*4)
			metas[i].numBPS = int(tileData[off])
			// Effective layer cumulative length (what we decode)
			loff := off + 1 + (effLayer-1)*4
			metas[i].dataLen = int(binary.BigEndian.Uint32(tileData[loff : loff+4]))
			// Full data length (last layer, for advancing dataPos)
			foff := off + 1 + (numLayers-1)*4
			metas[i].fullLen = int(binary.BigEndian.Uint32(tileData[foff : foff+4]))
		}
	} else {
		metaSize = 2 + numCB*5
		if len(tileData) < metaSize {
			return nil
		}
		for i := 0; i < numCB; i++ {
			off := 2 + i*5
			metas[i].numBPS = int(tileData[off])
			dl := int(binary.BigEndian.Uint32(tileData[off+1 : off+5]))
			metas[i].dataLen = dl
			metas[i].fullLen = dl
		}
	}

	// Iterate code-blocks in the same order as the encoder:
	// component → resolution → band → code-block
	cbIdx := 0
	dataPos := metaSize

	for c := 0; c < len(tile.Components); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}
		tcWidth := tc.X1 - tc.X0
		tcHeight := tc.Y1 - tc.Y0

		for r := 0; r < numRes; r++ {
			numBands := 1
			if r > 0 {
				numBands = 3
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

				// Compute band dimensions (same formula as encoder)
				scale := 1 << (numRes - 1 - r)
				bandW := (tcWidth + scale - 1) / scale
				bandH := (tcHeight + scale - 1) / scale
				if r > 0 {
					bandW = (bandW + 1) / 2
					bandH = (bandH + 1) / 2
				}

				xOff, yOff := computeSubbandOffset(tcWidth, tcHeight, numRes, r, bandType)

				for cby := 0; cby*cbHeight < bandH; cby++ {
					for cbx := 0; cbx*cbWidth < bandW; cbx++ {
						if cbIdx >= numCB {
							return nil
						}
						meta := metas[cbIdx]

						startX := cbx * cbWidth
						startY := cby * cbHeight
						actualW := cbWidth
						actualH := cbHeight
						if startX+actualW > bandW {
							actualW = bandW - startX
						}
						if startY+actualH > bandH {
							actualH = bandH - startY
						}

						if meta.numBPS > 0 && meta.dataLen > 0 && dataPos+meta.dataLen <= len(tileData) {
							cbData := tileData[dataPos : dataPos+meta.dataLen]
							t1 := entropy.NewT1(actualW, actualH)
							decoded := t1.Decode(cbData, meta.numBPS, bandType)

							for y := 0; y < actualH; y++ {
								for x := 0; x < actualW; x++ {
									dstX := xOff + startX + x
									dstY := yOff + startY + y
									if dstX < tcWidth && dstY < tcHeight {
										tc.Data[dstY*tcWidth+dstX] = decoded[y*actualW+x]
									}
								}
							}
						}

						dataPos += meta.fullLen
						cbIdx++
					}
				}
			}
		}
	}

	return nil
}

// createImage creates the output image from component data.
func (d *decoder) createImage(
	componentData [][]int32,
	width, height int,
	numComp int,
	precision int,
	signed bool,
) (image.Image, error) {
	// Determine scaling factor
	maxVal := int32((1 << precision) - 1)

	switch numComp {
	case 1:
		// Grayscale
		if precision <= 8 {
			img := image.NewGray(image.Rect(0, 0, width, height))
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					idx := y*width + x
					v := componentData[0][idx]
					if v < 0 {
						v = 0
					}
					if v > maxVal {
						v = maxVal
					}
					// Scale to 8-bit
					if precision != 8 {
						v = v * 255 / maxVal
					}
					img.SetGray(x, y, color.Gray{Y: uint8(v)})
				}
			}
			return img, nil
		}
		// 16-bit grayscale
		img := image.NewGray16(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				idx := y*width + x
				v := componentData[0][idx]
				if v < 0 {
					v = 0
				}
				if v > maxVal {
					v = maxVal
				}
				// Scale to 16-bit
				v = v * 65535 / maxVal
				img.SetGray16(x, y, color.Gray16{Y: uint16(v)})
			}
		}
		return img, nil

	case 3:
		// RGB
		if precision <= 8 {
			img := image.NewRGBA(image.Rect(0, 0, width, height))
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					idx := y*width + x
					r := componentData[0][idx]
					g := componentData[1][idx]
					b := componentData[2][idx]

					// Clamp values
					r = clampInt32(r, 0, maxVal)
					g = clampInt32(g, 0, maxVal)
					b = clampInt32(b, 0, maxVal)

					// Scale to 8-bit
					if precision != 8 {
						r = r * 255 / maxVal
						g = g * 255 / maxVal
						b = b * 255 / maxVal
					}

					img.SetRGBA(x, y, color.RGBA{
						R: uint8(r),
						G: uint8(g),
						B: uint8(b),
						A: 255,
					})
				}
			}
			return img, nil
		}
		// 16-bit RGB
		img := image.NewRGBA64(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				idx := y*width + x
				r := componentData[0][idx]
				g := componentData[1][idx]
				b := componentData[2][idx]

				r = clampInt32(r, 0, maxVal)
				g = clampInt32(g, 0, maxVal)
				b = clampInt32(b, 0, maxVal)

				// Scale to 16-bit
				r = r * 65535 / maxVal
				g = g * 65535 / maxVal
				b = b * 65535 / maxVal

				img.SetRGBA64(x, y, color.RGBA64{
					R: uint16(r),
					G: uint16(g),
					B: uint16(b),
					A: 65535,
				})
			}
		}
		return img, nil

	case 4:
		// RGBA
		if precision <= 8 {
			img := image.NewRGBA(image.Rect(0, 0, width, height))
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					idx := y*width + x
					r := clampInt32(componentData[0][idx], 0, maxVal)
					g := clampInt32(componentData[1][idx], 0, maxVal)
					b := clampInt32(componentData[2][idx], 0, maxVal)
					a := clampInt32(componentData[3][idx], 0, maxVal)

					if precision != 8 {
						r = r * 255 / maxVal
						g = g * 255 / maxVal
						b = b * 255 / maxVal
						a = a * 255 / maxVal
					}

					img.SetRGBA(x, y, color.RGBA{
						R: uint8(r),
						G: uint8(g),
						B: uint8(b),
						A: uint8(a),
					})
				}
			}
			return img, nil
		}
		// 16-bit RGBA
		img := image.NewRGBA64(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				idx := y*width + x
				r := clampInt32(componentData[0][idx], 0, maxVal)
				g := clampInt32(componentData[1][idx], 0, maxVal)
				b := clampInt32(componentData[2][idx], 0, maxVal)
				a := clampInt32(componentData[3][idx], 0, maxVal)

				r = r * 65535 / maxVal
				g = g * 65535 / maxVal
				b = b * 65535 / maxVal
				a = a * 65535 / maxVal

				img.SetRGBA64(x, y, color.RGBA64{
					R: uint16(r),
					G: uint16(g),
					B: uint16(b),
					A: uint16(a),
				})
			}
		}
		return img, nil

	default:
		return nil, fmt.Errorf("unsupported number of components: %d", numComp)
	}
}

// Helper function
func clampInt32(v, min, max int32) int32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// inverseNLT applies the inverse NLT Type 3 point transform to every component
// that carries an NLT marker, at the sample width that marker declares. A
// precision the transform is not defined for is reported as an error rather
// than skipped, because skipping it would hand back plausible-looking but
// wrong samples.
func (d *decoder) inverseNLT(componentData [][]int32) error {
	for c := range componentData {
		precision, ok := d.header.NLTPrecision(c)
		if !ok {
			continue
		}
		if precision < 2 || precision > 32 {
			return fmt.Errorf("NLT component %d declares unsupported precision %d", c, precision)
		}
		nltType3(componentData[c], precision)
	}
	return nil
}

// decodePlanes decodes all tiles and applies the inverse component transform,
// the inverse DC level shift and the inverse NLT point transform, returning
// the reconstructed component sample planes. It is shared by the FloatImage
// and HalfImage decode paths.
func (d *decoder) decodePlanes(cfg *Config) (componentData [][]int32, width, height int, err error) {
	h := d.header

	reduce := 0
	if cfg != nil && cfg.ReduceResolution > 0 {
		reduce = cfg.ReduceResolution
	}

	width = reducedDimension(int(h.ImageWidth-h.ImageXOffset), reduce)
	height = reducedDimension(int(h.ImageHeight-h.ImageYOffset), reduce)

	numComp := int(h.NumComponents)
	if numComp == 0 || len(h.ComponentInfo) == 0 {
		return nil, 0, 0, fmt.Errorf("invalid image: no components")
	}
	precision := h.ComponentInfo[0].Precision()

	// Check if any component has NLT (float mode)
	hasNLT := false
	for c := 0; c < numComp; c++ {
		if h.HasNLT(c) {
			hasNLT = true
			break
		}
	}

	// Decode tiles into int32 component data (same as integer path)
	componentData = make([][]int32, numComp)
	for c := 0; c < numComp; c++ {
		componentData[c] = make([]int32, width*height)
	}

	tileDecoder := tcd.NewTileDecoder(h)
	if cfg != nil && cfg.QualityLayers > 0 {
		tileDecoder.SetQualityLayerLimit(cfg.QualityLayers)
	}
	if reduce > 0 {
		tileDecoder.SetReduceResolution(reduce)
	}
	numTiles := int(h.NumTilesX * h.NumTilesY)

	for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
		if err := d.decodeTile(tileDecoder, tileIdx, componentData, width, height, cfg); err != nil {
			return nil, 0, 0, fmt.Errorf("decoding tile %d: %w", tileIdx, err)
		}
	}

	// Apply inverse MCT
	if h.CodingStyle.MultipleComponentXf != 0 && numComp >= 3 {
		if h.CodingStyle.IsReversible() {
			if hasNLT && precision > 16 {
				mct.InverseRCT32(componentData[0], componentData[1], componentData[2])
			} else {
				mct.InverseRCT(componentData[0], componentData[1], componentData[2])
			}
		} else {
			// ICT path
			compFloat := make([][]float64, 3)
			for c := 0; c < 3; c++ {
				compFloat[c] = make([]float64, len(componentData[c]))
				for i, v := range componentData[c] {
					compFloat[c][i] = float64(v)
				}
			}
			mct.InverseICT(compFloat[0], compFloat[1], compFloat[2])
			for c := 0; c < 3; c++ {
				for i, v := range compFloat[c] {
					componentData[c][i] = int32(v + 0.5)
				}
			}
		}
	}

	// Apply DC level shift (skip for signed/float data)
	for c := 0; c < numComp; c++ {
		if !h.ComponentInfo[c].IsSigned() {
			mct.DCLevelShiftInverse(componentData[c], h.ComponentInfo[c].Precision())
		}
	}

	// Apply inverse NLT after DC shift
	if hasNLT {
		if err := d.inverseNLT(componentData); err != nil {
			return nil, 0, 0, err
		}
	}

	return componentData, width, height, nil
}

// decodeHalf decodes the image as a HalfImage.
func (d *decoder) decodeHalf(cfg *Config) (*HalfImage, error) {
	// A reduced-resolution decode stops the inverse wavelet at an LL subband,
	// so the surviving int32 values are wavelet coefficients in the
	// sign-magnitude domain, not samples. Reinterpreting those as binary16
	// yields arbitrary bit patterns -- measured: a smooth 0..1 gradient comes
	// back with negative values. Refuse rather than return silent garbage.
	if cfg != nil && cfg.ReduceResolution > 0 {
		return nil, fmt.Errorf("jpeg2000: ReduceResolution is not supported for half decoding: " +
			"reduced-resolution output is wavelet-domain data, not half samples")
	}

	if err := d.readFormat(); err != nil {
		return nil, fmt.Errorf("reading format: %w", err)
	}

	if err := d.parseCodestream(); err != nil {
		return nil, fmt.Errorf("parsing codestream: %w", err)
	}

	h := d.header
	numComp := int(h.NumComponents)
	if numComp == 0 || len(h.ComponentInfo) == 0 {
		return nil, fmt.Errorf("invalid image: no components")
	}

	// Refuse anything that is not 16-bit half sample data. Decoding it as
	// half anyway would produce silent garbage.
	for c := 0; c < numComp; c++ {
		nltPrecision, isNLT := h.NLTPrecision(c)
		if !isNLT {
			return nil, fmt.Errorf("component %d has no NLT marker: not half float data", c)
		}
		if nltPrecision != 16 {
			return nil, fmt.Errorf("component %d declares %d-bit NLT samples, want 16", c, nltPrecision)
		}
		if p := h.ComponentInfo[c].Precision(); p != 16 {
			return nil, fmt.Errorf("component %d has %d-bit precision, want 16", c, p)
		}
		if !h.ComponentInfo[c].IsSigned() {
			return nil, fmt.Errorf("component %d is unsigned: not half float data", c)
		}
	}

	componentData, width, height, err := d.decodePlanes(cfg)
	if err != nil {
		return nil, err
	}

	components := make([][]uint16, numComp)
	for c := 0; c < numComp; c++ {
		components[c] = make([]uint16, width*height)
		for i, v := range componentData[c] {
			components[c][i] = uint16(v)
		}
	}

	return &HalfImage{
		Width:      width,
		Height:     height,
		Components: components,
	}, nil
}

// decodeFloat decodes the image as a FloatImage.
func (d *decoder) decodeFloat(cfg *Config) (*FloatImage, error) {
	if err := d.readFormat(); err != nil {
		return nil, fmt.Errorf("reading format: %w", err)
	}

	if err := d.parseCodestream(); err != nil {
		return nil, fmt.Errorf("parsing codestream: %w", err)
	}

	return d.decodeTilesFloat(cfg)
}

// decodeTilesFloat decodes all tiles and assembles a FloatImage output.
func (d *decoder) decodeTilesFloat(cfg *Config) (*FloatImage, error) {
	h := d.header

	numComp := int(h.NumComponents)
	if numComp == 0 || len(h.ComponentInfo) == 0 {
		return nil, fmt.Errorf("invalid image: no components")
	}
	precision := h.ComponentInfo[0].Precision()
	signed := h.ComponentInfo[0].IsSigned()

	// Check if any component has NLT (float mode)
	hasNLT := false
	for c := 0; c < numComp; c++ {
		if h.HasNLT(c) {
			hasNLT = true
			break
		}
	}

	componentData, width, height, err := d.decodePlanes(cfg)
	if err != nil {
		return nil, err
	}

	// Reinterpret as float
	if hasNLT {
		// Reinterpret the sample bits as floating point at the width the NLT
		// marker declares: binary32 bit patterns directly, binary16 patterns
		// widened to float32.
		components := make([][]float32, numComp)
		for c := 0; c < numComp; c++ {
			components[c] = make([]float32, width*height)
			nltPrecision, isNLT := h.NLTPrecision(c)
			switch {
			case isNLT && nltPrecision == 32:
				for i, v := range componentData[c] {
					components[c][i] = math.Float32frombits(uint32(v))
				}
			case isNLT && nltPrecision == 16:
				for i, v := range componentData[c] {
					components[c][i] = halfToFloat32(uint16(v))
				}
			case isNLT:
				return nil, fmt.Errorf("NLT component %d has unsupported float width %d", c, nltPrecision)
			default:
				for i, v := range componentData[c] {
					components[c][i] = float32(v)
				}
			}
		}

		return &FloatImage{
			Width:      width,
			Height:     height,
			Components: components,
			BitDepth:   32,
			Signed:     true,
		}, nil
	}

	// Non-NLT path: standard float conversion
	compFloat := make([][]float64, numComp)
	for c := 0; c < numComp; c++ {
		compFloat[c] = make([]float64, len(componentData[c]))
		for i, v := range componentData[c] {
			compFloat[c][i] = float64(v)
		}
	}

	return d.createFloatImage(compFloat, width, height, numComp, precision, signed)
}

// createFloatImage creates a FloatImage from float64 component data.
func (d *decoder) createFloatImage(
	compFloat [][]float64,
	width, height int,
	numComp int,
	precision int,
	signed bool,
) (*FloatImage, error) {
	components := make([][]float32, numComp)
	for c := 0; c < numComp; c++ {
		components[c] = make([]float32, width*height)
		for i, v := range compFloat[c] {
			components[c][i] = float32(v)
		}
	}

	return &FloatImage{
		Width:      width,
		Height:     height,
		Components: components,
		BitDepth:   precision,
		Signed:     signed,
	}, nil
}
