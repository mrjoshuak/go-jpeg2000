// Package jpeg2000 provides a pure Go implementation of the JPEG 2000 image codec.
//
// JPEG 2000 (ISO/IEC 15444-1) is a wavelet-based image compression standard that
// provides both lossless and lossy compression. This package aims for 100% parity
// with the OpenJPEG reference implementation.
//
// Basic usage for decoding:
//
//	file, _ := os.Open("image.jp2")
//	img, err := jpeg2000.Decode(file)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Basic usage for encoding:
//
//	file, _ := os.Create("output.jp2")
//	err := jpeg2000.Encode(file, img, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
package jpeg2000

import (
	"image"
	"io"
)

// Format constants for JPEG 2000 file formats.
const (
	// FormatJ2K is the raw codestream format (no file wrapper).
	FormatJ2K Format = iota
	// FormatJP2 is the standard JP2 file format with metadata boxes.
	FormatJP2
	// FormatJPX is the extended JP2 format (Part 2).
	FormatJPX
)

// Format represents a JPEG 2000 file format.
type Format int

// String returns the string representation of the format.
func (f Format) String() string {
	switch f {
	case FormatJ2K:
		return "J2K"
	case FormatJP2:
		return "JP2"
	case FormatJPX:
		return "JPX"
	default:
		return "Unknown"
	}
}

// Profile constants for JPEG 2000 profiles (RSIZ parameter).
const (
	// ProfileNone indicates no profile restrictions.
	ProfileNone Profile = 0x0000
	// ProfilePart2 indicates Part 2 extensions are used.
	ProfilePart2 Profile = 0x8000
	// ProfileCinema2K is the 2K Digital Cinema profile.
	ProfileCinema2K Profile = 0x0003
	// ProfileCinema4K is the 4K Digital Cinema profile.
	ProfileCinema4K Profile = 0x0004
	// ProfileCinemaS2K is the 2K scalable Digital Cinema profile.
	ProfileCinemaS2K Profile = 0x0005
	// ProfileCinemaS4K is the 4K scalable Digital Cinema profile.
	ProfileCinemaS4K Profile = 0x0006
	// ProfileCinemaSLTE is the Long-term extension Digital Cinema profile.
	ProfileCinemaSLTE Profile = 0x0007
	// ProfileBroadcastSingle is single-tile broadcast profile.
	ProfileBroadcastSingle Profile = 0x0100
	// ProfileBroadcastMulti is multi-tile broadcast profile.
	ProfileBroadcastMulti Profile = 0x0200
	// ProfileIMF2K is 2K Interoperable Master Format profile.
	ProfileIMF2K Profile = 0x0400
	// ProfileIMF4K is 4K Interoperable Master Format profile.
	ProfileIMF4K Profile = 0x0500
	// ProfileIMF8K is 8K Interoperable Master Format profile.
	ProfileIMF8K Profile = 0x0600
)

// Profile represents a JPEG 2000 profile (RSIZ parameter).
type Profile uint16

// ProgressionOrder defines the order in which packets are encoded/decoded.
type ProgressionOrder int

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

// String returns the string representation of the progression order.
func (p ProgressionOrder) String() string {
	switch p {
	case LRCP:
		return "LRCP"
	case RLCP:
		return "RLCP"
	case RPCL:
		return "RPCL"
	case PCRL:
		return "PCRL"
	case CPRL:
		return "CPRL"
	default:
		return "Unknown"
	}
}

// ColorSpace represents the color space of an image.
// Values 0-5 match the OpenJPEG OPJ_COLOR_SPACE enum for compatibility.
// Additional colorspaces from ISO/IEC 15444-1 are assigned values 6+.
type ColorSpace int

const (
	// ColorSpaceUnknown indicates the colorspace is not supported.
	// This is returned when the JP2 file specifies an unrecognized enumcs value.
	ColorSpaceUnknown ColorSpace = iota - 1 // -1 matches OPJ_CLRSPC_UNKNOWN

	// ColorSpaceUnspecified indicates no colorspace was specified in the file.
	// This is returned for raw J2K codestreams without a JP2 container.
	ColorSpaceUnspecified // 0 matches OPJ_CLRSPC_UNSPECIFIED

	// ColorSpaceSRGB is standard RGB (enumcs 16).
	ColorSpaceSRGB // 1 matches OPJ_CLRSPC_SRGB

	// ColorSpaceGray is grayscale (enumcs 17).
	ColorSpaceGray // 2 matches OPJ_CLRSPC_GRAY

	// ColorSpaceSYCC is sRGB-based YCbCr (enumcs 1, 18).
	// Uses ITU-R BT.709-5 matrix with sRGB primaries.
	ColorSpaceSYCC // 3 matches OPJ_CLRSPC_SYCC

	// ColorSpaceEYCC is extended sYCC (enumcs 24).
	// Extended gamut YCbCr based on sRGB.
	ColorSpaceEYCC // 4 matches OPJ_CLRSPC_EYCC

	// ColorSpaceCMYK is CMYK color space (enumcs 12).
	ColorSpaceCMYK // 5 matches OPJ_CLRSPC_CMYK

	// ColorSpaceBilevel is bi-level/binary (enumcs 0, 15).
	// Note: OpenJPEG maps bilevel to unknown.
	ColorSpaceBilevel // 6 (extension beyond OpenJPEG)

	// ColorSpaceYCbCr2 is YCbCr for 625-line systems (enumcs 3).
	// Uses ITU-R BT.601-5 matrix for PAL/SECAM.
	ColorSpaceYCbCr2 // 7

	// ColorSpaceYCbCr3 is YCbCr for 525-line systems (enumcs 4).
	// Uses ITU-R BT.601-5 matrix for NTSC.
	ColorSpaceYCbCr3 // 8

	// ColorSpacePhotoYCC is Kodak PhotoYCC (enumcs 9).
	// Used in Kodak Photo CD format.
	ColorSpacePhotoYCC // 9

	// ColorSpaceCMY is CMY without black (enumcs 11).
	ColorSpaceCMY // 10

	// ColorSpaceYCCK is YCCK (enumcs 13).
	// PhotoYCC-based CMYK representation.
	ColorSpaceYCCK // 11

	// ColorSpaceCIELab is CIE L*a*b* (enumcs 14).
	// Device-independent color space with D50 illuminant.
	ColorSpaceCIELab // 12

	// ColorSpaceCIEJab is CIE J*a*b* (enumcs 19).
	// CIECAM02-based appearance model.
	ColorSpaceCIEJab // 13

	// ColorSpaceESRGB is extended sRGB (enumcs 20).
	// Extended gamut sRGB per IEC 61966-2-1 Amendment 1.
	ColorSpaceESRGB // 14

	// ColorSpaceROMMRGB is ROMM-RGB/ProPhoto RGB (enumcs 21).
	// Wide gamut RGB per ISO 22028-2.
	ColorSpaceROMMRGB // 15

	// ColorSpaceYPbPr60 is YPbPr for 1125/60 systems (enumcs 22).
	// HD video per SMPTE 274M.
	ColorSpaceYPbPr60 // 16

	// ColorSpaceYPbPr50 is YPbPr for 1250/50 systems (enumcs 23).
	// HD video per ITU-R BT.1361.
	ColorSpaceYPbPr50 // 17
)

// Config holds the decoding configuration.
type Config struct {
	// DecodeArea limits the decode to a region, in full-resolution image
	// coordinates, and is clipped to the image. Nil decodes all of it.
	//
	// The returned image covers the region and is allocated for it, so a band
	// of a large image costs a band-sized buffer. Precincts that fall outside
	// the region are not entropy-decoded, and with an explicit precinct
	// partition their packets are not read either — see
	// PacketIndex.PacketsForRegion for the byte ranges that implies.
	DecodeArea *image.Rectangle

	// ReduceResolution specifies the number of resolution levels to skip.
	// 0 means full resolution, 1 means half resolution, etc.
	ReduceResolution int

	// QualityLayers specifies the number of quality layers to decode.
	// 0 means all layers.
	QualityLayers int
}

// PrecinctSize is one resolution's precinct partition, as base-2 exponents.
// {WidthExp: 7, HeightExp: 7} is a 128x128 precinct.
type PrecinctSize struct {
	WidthExp  uint8
	HeightExp uint8
}

// Options holds the encoding options.
type Options struct {
	// Format specifies the output format (J2K, JP2, or JPX).
	Format Format

	// Profile specifies the JPEG 2000 profile to use.
	Profile Profile

	// Lossless specifies whether to use lossless compression.
	// If true, the 5-3 reversible wavelet transform is used.
	// If false, the 9-7 irreversible wavelet transform is used.
	Lossless bool

	// Quality specifies the compression quality (1-100).
	// Only used when Lossless is false. Zero means 100.
	//
	// It is an error budget rather than a scale factor: it fixes the largest
	// difference, in sample counts, between a decoded sample and the source.
	// 100 asks for half a count at 8-bit precision and each ten points below
	// doubles that, with the budget scaling with the sample range so a 16-bit
	// image is held to the same relative accuracy. The quantization step sizes
	// the QCD marker carries are derived from that budget and the synthesis
	// gain of each subband, so the guarantee holds for any conforming decoder,
	// not only for this one. See "Lossy 9/7" in the README.
	//
	// Half a count cannot survive rounding to an integer sample, so 100
	// reproduces 8-bit input exactly. Two limits can widen the steps past what
	// was asked at deep decompositions: the five-bit QCD exponent field, and
	// the 30 bit-plane ceiling the block coder imposes.
	Quality int

	// CompressionRatio specifies the target compression ratio.
	// Only used when Lossless is false and Quality is 0.
	// For example, 20 means 20:1 compression.
	CompressionRatio float64

	// NumResolutions specifies the number of resolution levels.
	// Default is 6 (5 decomposition levels + 1).
	NumResolutions int

	// CodeBlockSize specifies the code block dimensions (log2).
	// Default is (6, 6) for 64x64 code blocks.
	CodeBlockSize image.Point

	// PrecinctSize specifies the precinct dimensions per resolution level.
	// If nil, default sizes are used.
	PrecinctSize []image.Point

	// ProgressionOrder specifies the packet ordering.
	ProgressionOrder ProgressionOrder

	// NumLayers specifies the number of quality layers.
	NumLayers int

	// TileSize specifies the tile dimensions.
	// If zero, the entire image is one tile.
	TileSize image.Point

	// TileOffset specifies the tile grid origin offset.
	TileOffset image.Point

	// ImageOffset specifies the image area offset.
	ImageOffset image.Point

	// ColorSpace specifies the color space.
	ColorSpace ColorSpace

	// ICCProfile specifies an optional ICC color profile.
	ICCProfile []byte

	// Comment specifies an optional comment string.
	Comment string

	// EnableSOP enables Start of Packet markers.
	EnableSOP bool

	// EnableEPH enables End of Packet Header markers.
	EnableEPH bool

	// ComponentSubsampling gives each component's XRsiz and YRsiz, the factors
	// by which its grid is coarser than the image's reference grid. One entry
	// per component; an empty list, or any entry of {0,0} or {1,1}, leaves that
	// component at full resolution.
	//
	// {{1,1},{2,2},{2,2}} is 4:2:0 and {{1,1},{2,1},{2,1}} is 4:2:2. The
	// samples of a subsampled component are taken by decimation — every
	// XRsiz-th column of every YRsiz-th row — rather than by averaging, because
	// the format specifies no filter and decimation is the one choice a
	// decoder can invert exactly.
	//
	// The multi-component transform requires every component to share a grid,
	// so it is not applied when the factors differ.
	ComponentSubsampling []image.Point

	// WritePacketLengths writes the packet length markers: PLT in every
	// tile-part header and TLM in the main header.
	//
	// They change nothing about the image. What they change is the cost of
	// finding a packet: without them a reader learns where packet N begins
	// only by parsing packets 0 to N-1, which over a network is a chain of
	// small dependent round trips. With them the offsets follow by summation
	// from a few kilobytes near the front of the file, so a rolling prefetch
	// across a frame sequence becomes one ranged read per frame.
	//
	// The cost is a few bytes per packet, and any conforming decoder that does
	// not want them skips both markers.
	WritePacketLengths bool

	// PrecinctSizes gives the precinct partition, one entry per resolution
	// from the lowest upward, as base-2 exponents of width and height. A
	// shorter list repeats its last entry for the resolutions above it, and an
	// empty list writes the maximal precinct, which is one packet per
	// resolution and what the format assumes when Scod bit 0 is clear.
	//
	// Precincts are what make a codestream spatially addressable: without them
	// a resolution is one packet covering the whole image, so a region of
	// interest cannot be located without decoding everything. With them each
	// packet covers one rectangle, and a packet index resolves a viewport to
	// byte ranges.
	//
	// Each exponent must be in [1, 15], except that the lowest resolution may
	// use 0. A precinct smaller than the code-block clips the code-block
	// partition to it, which is legal and is how small precincts behave.
	PrecinctSizes []PrecinctSize

	// Precision overrides the bit depth for encoding.
	// If 0, uses the natural precision of the input image (8 or 16).
	// Valid values are 1-16. Values other than 8 or 16 will exercise
	// precision scaling in the decoder.
	Precision int

	// HighThroughput enables HTJ2K (High-Throughput JPEG 2000) mode.
	// When enabled, the FBCS (Fast Block Coding Stream) entropy coder
	// is used instead of the standard MQ arithmetic coder.
	// This provides significantly higher encoding/decoding throughput
	// at a modest cost in compression efficiency.
	HighThroughput bool

	// HTBlockWidth specifies the code block width for HTJ2K mode.
	// Valid values are 32 and 128. Default is 128.
	// Only used when HighThroughput is true.
	HTBlockWidth int

	// HTBlockHeight specifies the code block height for HTJ2K mode.
	// Valid values are 32 and 128. Default is 128.
	// Only used when HighThroughput is true.
	HTBlockHeight int
}

// DefaultOptions returns the default encoding options.
func DefaultOptions() *Options {
	return &Options{
		Format:           FormatJP2,
		Profile:          ProfileNone,
		Lossless:         false,
		Quality:          75,
		NumResolutions:   6,
		CodeBlockSize:    image.Point{6, 6}, // 64x64
		ProgressionOrder: LRCP,
		NumLayers:        1,
	}
}

// Decode reads a JPEG 2000 image from r and returns it as an image.Image.
func Decode(r io.Reader) (image.Image, error) {
	return DecodeConfig(r, nil)
}

// DecodeCost reports what a decode spent: the bytes of code-block data it
// entropy-decoded, and the bytes it skipped because a decode area excluded
// them. Both are zero for a decode with no area.
//
// It exists so "a region costs less than the whole" is a measurement rather
// than a claim.
type DecodeCost struct {
	Decoded int
	Skipped int
}

// DecodeConfigCost decodes with the given configuration and reports what the
// decode cost, for callers measuring the saving a region buys.
func DecodeConfigCost(r io.Reader, cfg *Config) (image.Image, DecodeCost, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, DecodeCost{}, err
	}
	d := newDecoder(r)
	img, err := d.decode(cfg)
	return img, DecodeCost{Decoded: d.regionBytes, Skipped: d.skippedBytes}, err
}

// DecodeConfig decodes a JPEG 2000 image with the specified configuration.
func DecodeConfig(r io.Reader, cfg *Config) (image.Image, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, err
	}
	d := newDecoder(r)
	return d.decode(cfg)
}

// checkConfig rejects a configuration this decoder cannot honour.
//
// A decoder that silently ignores an option is worse than one that lacks it: a
// caller asking for a 256-row region and receiving the whole image has no
// indication the request was dropped, and will read the wrong pixels from the
// buffer it sized for the answer it asked for. Nothing is refused here now,
// and the function stays as the one place to refuse from.
func checkConfig(cfg *Config) error {
	return nil
}

// Encode writes the image m to w in JPEG 2000 format with the given options.
func Encode(w io.Writer, m image.Image, o *Options) error {
	if o == nil {
		o = DefaultOptions()
	}
	e := newEncoder(w, m, o)
	return e.encode()
}

// EncodeFloat writes a FloatImage to w in JPEG 2000 format.
// Float encoding is always lossless (5/3 reversible wavelet).
// IEEE 754 float bits are reinterpreted as int32 and an NLT Type 3
// marker signals the codec to apply a sign-magnitude transform.
func EncodeFloat(w io.Writer, img *FloatImage, o *Options) error {
	if o == nil {
		o = DefaultOptions()
	}
	o.Lossless = true // float encoding is always lossless
	e := &encoder{
		w:        w,
		options:  o,
		floatImg: img,
	}
	return e.encodeFloat()
}

// EncodeHalf writes a HalfImage to w in JPEG 2000 format.
// Half encoding is always lossless (5/3 reversible wavelet): the IEEE 754
// binary16 bit patterns are reinterpreted as signed 16-bit samples and an NLT
// Type 3 marker with a 16-bit depth signals the decoder to apply the
// sign-magnitude transform at that width.
func EncodeHalf(w io.Writer, img *HalfImage, o *Options) error {
	if o == nil {
		o = DefaultOptions()
	}
	// Copy before forcing Lossless: o belongs to the caller, and reusing it for
	// a later Encode must not silently inherit a setting made here.
	opts := *o
	opts.Lossless = true // half encoding is always lossless
	e := &encoder{
		w:       w,
		options: &opts,
		halfImg: img,
	}
	return e.encodeHalf()
}

// DecodeHalf reads a JPEG 2000 image written by EncodeHalf and returns the
// exact binary16 bit patterns. It returns an error if the codestream does not
// carry 16-bit NLT Type 3 samples, rather than silently reinterpreting data
// that is not half float.
func DecodeHalf(r io.Reader) (*HalfImage, error) {
	return DecodeHalfConfig(r, nil)
}

// DecodeHalfConfig decodes a half-float JPEG 2000 image with the specified
// configuration.
func DecodeHalfConfig(r io.Reader, cfg *Config) (*HalfImage, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, err
	}
	d := newDecoder(r)
	return d.decodeHalf(cfg)
}

// DecodeFloat reads a JPEG 2000 image and returns it as a FloatImage,
// preserving float precision from the wavelet transform pipeline.
func DecodeFloat(r io.Reader) (*FloatImage, error) {
	return DecodeFloatConfig(r, nil)
}

// DecodeFloatConfig decodes a JPEG 2000 image with the specified configuration,
// returning a FloatImage that preserves float precision.
func DecodeFloatConfig(r io.Reader, cfg *Config) (*FloatImage, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, err
	}
	d := newDecoder(r)
	return d.decodeFloat(cfg)
}

// DecodeMetadata reads only the header information without decoding the image.
func DecodeMetadata(r io.Reader) (*Metadata, error) {
	d := newDecoder(r)
	return d.readMetadata()
}

// Metadata contains image metadata extracted from the JPEG 2000 file.
type Metadata struct {
	// Format is the detected file format.
	Format Format

	// Width is the image width in pixels.
	Width int

	// Height is the image height in pixels.
	Height int

	// NumComponents is the number of color components.
	NumComponents int

	// BitsPerComponent is the bit depth for each component.
	BitsPerComponent []int

	// Signed indicates whether each component uses signed values.
	Signed []bool

	// ColorSpace is the detected color space.
	ColorSpace ColorSpace

	// Profile is the JPEG 2000 profile.
	Profile Profile

	// NumResolutions is the number of resolution levels.
	NumResolutions int

	// NumQualityLayers is the number of quality layers.
	NumQualityLayers int

	// TileWidth is the tile width.
	TileWidth int

	// TileHeight is the tile height.
	TileHeight int

	// NumTilesX is the number of tiles horizontally.
	NumTilesX int

	// NumTilesY is the number of tiles vertically.
	NumTilesY int

	// ICCProfile is the embedded ICC color profile, if any.
	ICCProfile []byte

	// Comment is the embedded comment string, if any.
	Comment string
}

// init registers the JPEG 2000 format with the image package.
func init() {
	// Register JP2 format (with signature box)
	image.RegisterFormat("jp2",
		"\x00\x00\x00\x0cjP  \r\n\x87\n",
		func(r io.Reader) (image.Image, error) {
			return Decode(r)
		},
		func(r io.Reader) (image.Config, error) {
			m, err := DecodeMetadata(r)
			if err != nil {
				return image.Config{}, err
			}
			return image.Config{
				Width:  m.Width,
				Height: m.Height,
			}, nil
		})

	// Register J2K format (raw codestream)
	image.RegisterFormat("j2k",
		"\xff\x4f\xff\x51",
		func(r io.Reader) (image.Image, error) {
			return Decode(r)
		},
		func(r io.Reader) (image.Config, error) {
			m, err := DecodeMetadata(r)
			if err != nil {
				return image.Config{}, err
			}
			return image.Config{
				Width:  m.Width,
				Height: m.Height,
			}, nil
		})
}
