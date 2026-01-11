package jpeg2000

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/mrjoshuak/go-jpeg2000/internal/box"
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
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
	if d.jp2Header != nil && d.jp2Header.ColorSpec != nil {
		switch d.jp2Header.ColorSpec.EnumeratedColorspace {
		case box.CSBilevel1, box.CSBilevel2:
			m.ColorSpace = ColorSpaceBilevel
		case box.CSGray:
			m.ColorSpace = ColorSpaceGray
		case box.CSSRGB:
			m.ColorSpace = ColorSpaceSRGB
		case box.CSYCbCr1, box.CSsYCC:
			m.ColorSpace = ColorSpaceSYCC
		case box.CSYCbCr2:
			m.ColorSpace = ColorSpaceYCbCr2
		case box.CSYCbCr3:
			m.ColorSpace = ColorSpaceYCbCr3
		case box.CSPhotoYCC:
			m.ColorSpace = ColorSpacePhotoYCC
		case box.CSCMY:
			m.ColorSpace = ColorSpaceCMY
		case box.CSCMYK:
			m.ColorSpace = ColorSpaceCMYK
		case box.CSYCCK:
			m.ColorSpace = ColorSpaceYCCK
		case box.CSCIELab:
			m.ColorSpace = ColorSpaceCIELab
		case box.CSCIEJab:
			m.ColorSpace = ColorSpaceCIEJab
		case box.CSeSRGB:
			m.ColorSpace = ColorSpaceESRGB
		case box.CSROMMRGB:
			m.ColorSpace = ColorSpaceROMMRGB
		case box.CSYPbPr1125:
			m.ColorSpace = ColorSpaceYPbPr60
		case box.CSYPbPr1250:
			m.ColorSpace = ColorSpaceYPbPr50
		case box.CSeSYCC:
			m.ColorSpace = ColorSpaceEYCC
		default:
			// Unknown enumcs value - not supported
			m.ColorSpace = ColorSpaceUnknown
		}
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

	parser := codestream.NewParser(&byteReader{data: d.codestream})
	header, err := parser.ReadHeader()
	if err != nil {
		return err
	}
	d.header = header
	return nil
}

// decodeTiles decodes all tiles and assembles the output image.
func (d *decoder) decodeTiles(cfg *Config) (image.Image, error) {
	h := d.header

	// Calculate output dimensions
	width := int(h.ImageWidth - h.ImageXOffset)
	height := int(h.ImageHeight - h.ImageYOffset)

	if cfg != nil && cfg.ReduceResolution > 0 {
		// Reduce resolution
		for i := 0; i < cfg.ReduceResolution; i++ {
			width = (width + 1) / 2
			height = (height + 1) / 2
		}
	}

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
	numTiles := int(h.NumTilesX * h.NumTilesY)

	for tileIdx := 0; tileIdx < numTiles; tileIdx++ {
		if err := d.decodeTile(tileDecoder, tileIdx, componentData, width, height, cfg); err != nil {
			return nil, fmt.Errorf("decoding tile %d: %w", tileIdx, err)
		}
	}

	// Apply inverse MCT if needed
	if h.CodingStyle.MultipleComponentXf != 0 && numComp >= 3 {
		if h.CodingStyle.IsReversible() {
			mct.InverseRCT(componentData[0], componentData[1], componentData[2])
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

	// Initialize tile
	tileDecoder.InitTile(tileIdx)

	// For now, we'll do a simplified decode
	// A full implementation would:
	// 1. Read tile-part headers and data from codestream
	// 2. Decode T2 packets
	// 3. Decode T1 code-blocks
	// 4. Apply inverse DWT

	tile := tileDecoder.Tile()
	if tile == nil {
		return fmt.Errorf("tile %d not initialized", tileIdx)
	}

	// Copy tile data to output (placeholder - actual decode would happen here)
	for c := 0; c < len(tile.Components) && c < len(componentData); c++ {
		tc := tile.Components[c]
		if tc == nil {
			continue
		}

		// Apply inverse DWT
		tileDecoder.ApplyInverseDWT(tc)

		// Copy to output
		for y := tc.Y0; y < tc.Y1 && y-int(h.ImageYOffset) < imgHeight; y++ {
			for x := tc.X0; x < tc.X1 && x-int(h.ImageXOffset) < imgWidth; x++ {
				srcIdx := (y-tc.Y0)*(tc.X1-tc.X0) + (x - tc.X0)
				dstX := x - int(h.ImageXOffset)
				dstY := y - int(h.ImageYOffset)
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
