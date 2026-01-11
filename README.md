# go-jpeg2000

[![Go Reference](https://pkg.go.dev/badge/github.com/mrjoshuak/go-jpeg2000.svg)](https://pkg.go.dev/github.com/mrjoshuak/go-jpeg2000)
[![Go Report Card](https://goreportcard.com/badge/github.com/mrjoshuak/go-jpeg2000)](https://goreportcard.com/report/github.com/mrjoshuak/go-jpeg2000)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

A pure Go implementation of the JPEG 2000 image codec (ISO/IEC 15444-1) with HTJ2K support (ISO/IEC 15444-15).

## Overview

This package provides a native Go implementation of JPEG 2000 encoding and decoding, aiming for 100% parity with the OpenJPEG reference implementation. It supports both lossless (5-3 reversible wavelet) and lossy (9-7 irreversible wavelet) compression.

## Features

- **Pure Go**: No CGO dependencies, works on all Go-supported platforms
- **Format Support**: JP2 file format and raw J2K codestream
- **HTJ2K Support**: High-Throughput JPEG 2000 (ISO/IEC 15444-15) encoding and decoding
- **Lossless & Lossy**: Both compression modes supported
- **Full Colorspace Support**: All 19 ISO/IEC 15444-1 colorspaces with automatic conversion to sRGB
- **Flexible Precision**: 1-16 bit component precision, including 4-bit, 10-bit, and 12-bit
- **Standard Library Integration**: Implements `image.Image` interface
- **Auto-registration**: Registers with Go's `image` package for transparent decode

## Installation

```bash
go get github.com/mrjoshuak/go-jpeg2000
```

## Usage

### Decoding

```go
package main

import (
    "image"
    "os"

    _ "github.com/mrjoshuak/go-jpeg2000" // Register format
)

func main() {
    file, _ := os.Open("image.jp2")
    defer file.Close()

    img, format, err := image.Decode(file)
    if err != nil {
        panic(err)
    }
    // Use img...
}
```

### Encoding

```go
package main

import (
    "image"
    "os"

    "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
    // Load or create an image
    img := createImage()

    file, _ := os.Create("output.jp2")
    defer file.Close()

    opts := jpeg2000.DefaultOptions()
    opts.Lossless = true

    err := jpeg2000.Encode(file, img, opts)
    if err != nil {
        panic(err)
    }
}
```

### Reading Metadata

```go
file, _ := os.Open("image.jp2")
meta, err := jpeg2000.DecodeMetadata(file)
if err != nil {
    panic(err)
}

fmt.Printf("Size: %dx%d\n", meta.Width, meta.Height)
fmt.Printf("Components: %d\n", meta.NumComponents)
fmt.Printf("ColorSpace: %v\n", meta.ColorSpace)
fmt.Printf("Tiles: %dx%d\n", meta.NumTilesX, meta.NumTilesY)
```

### Encoding Options

```go
opts := &jpeg2000.Options{
    Format:           jpeg2000.FormatJP2,      // or FormatJ2K, FormatJPX
    Lossless:         true,                     // Use 5-3 reversible wavelet
    Quality:          75,                       // 1-100, for lossy mode
    CompressionRatio: 20,                       // Alternative to Quality (20:1)
    NumResolutions:   6,                        // Decomposition levels + 1
    NumLayers:        1,                        // Quality layers
    ProgressionOrder: jpeg2000.LRCP,           // Packet ordering
    CodeBlockSize:    image.Point{6, 6},       // 64x64 code blocks
    TileSize:         image.Point{512, 512},   // Tile dimensions
    ColorSpace:       jpeg2000.ColorSpaceSRGB, // Output colorspace
    Precision:        12,                       // Override bit depth (1-16)
    EnableSOP:        true,                     // Start of packet markers
    EnableEPH:        true,                     // End of packet header markers
    Comment:          "Created with go-jpeg2000",
}
```

## Colorspace Support

Full support for all colorspaces defined in ISO/IEC 15444-1 Annex M:

| enumcs | Colorspace | API Constant | Description |
|--------|------------|--------------|-------------|
| 0 | Bi-level | `ColorSpaceBilevel` | Black and white |
| 1 | YCbCr(1) | `ColorSpaceSYCC` | ITU-R BT.709-5 (sRGB primaries) |
| 3 | YCbCr(2) | `ColorSpaceYCbCr2` | ITU-R BT.601-5 (625-line PAL/SECAM) |
| 4 | YCbCr(3) | `ColorSpaceYCbCr3` | ITU-R BT.601-5 (525-line NTSC) |
| 9 | PhotoYCC | `ColorSpacePhotoYCC` | Kodak Photo CD |
| 11 | CMY | `ColorSpaceCMY` | Cyan, Magenta, Yellow |
| 12 | CMYK | `ColorSpaceCMYK` | CMY + Key (Black) |
| 13 | YCCK | `ColorSpaceYCCK` | PhotoYCC + Key |
| 14 | CIELab | `ColorSpaceCIELab` | CIE L\*a\*b\* (D50) |
| 15 | Bi-level(2) | `ColorSpaceBilevel` | Alternative bi-level |
| 16 | sRGB | `ColorSpaceSRGB` | Standard RGB (IEC 61966-2-1) |
| 17 | Grayscale | `ColorSpaceGray` | Single component gray |
| 18 | sYCC | `ColorSpaceSYCC` | sRGB-based YCbCr |
| 19 | CIEJab | `ColorSpaceCIEJab` | CIECAM02 J\*a\*b\* |
| 20 | e-sRGB | `ColorSpaceESRGB` | Extended gamut sRGB |
| 21 | ROMM-RGB | `ColorSpaceROMMRGB` | ProPhoto RGB (ISO 22028-2) |
| 22 | YPbPr(60) | `ColorSpaceYPbPr60` | HD video 1125/60 (SMPTE 274M) |
| 23 | YPbPr(50) | `ColorSpaceYPbPr50` | HD video 1250/50 (ITU-R BT.1361) |
| 24 | e-sYCC | `ColorSpaceEYCC` | Extended gamut sYCC |

### Colorspace Handling

- **Automatic Conversion**: All colorspaces are automatically converted to sRGB during decode
- **OpenJPEG Compatible**: API values 0-5 match OpenJPEG's `OPJ_COLOR_SPACE` enum
- **Unspecified vs Unknown**:
  - `ColorSpaceUnspecified` (0): Returned for raw J2K codestreams without JP2 container
  - `ColorSpaceUnknown` (-1): Returned for unrecognized enumcs values

### Color Conversion Details

The decoder applies mathematically correct color transformations based on the specifications:

| Colorspace | Conversion Method |
|------------|-------------------|
| YCbCr variants | ITU-R BT.601/709 matrix inversion |
| CMY/CMYK | Subtractive color model |
| CIELab | Lab→XYZ→sRGB with D50→D65 adaptation |
| CIEJab | CIECAM02 inverse (simplified) |
| ROMM-RGB | Wide gamut to sRGB with clipping |
| PhotoYCC | Kodak-specific YCC matrix |

## JPEG 2000 Profiles

Supported profiles (RSIZ parameter):

| Profile | Constant | Description |
|---------|----------|-------------|
| None | `ProfileNone` | No restrictions |
| Cinema 2K | `ProfileCinema2K` | 2K Digital Cinema |
| Cinema 4K | `ProfileCinema4K` | 4K Digital Cinema |
| Cinema S2K | `ProfileCinemaS2K` | 2K Scalable Cinema |
| Cinema S4K | `ProfileCinemaS4K` | 4K Scalable Cinema |
| Cinema SLTE | `ProfileCinemaSLTE` | Long-term extension |
| Broadcast Single | `ProfileBroadcastSingle` | Single-tile broadcast |
| Broadcast Multi | `ProfileBroadcastMulti` | Multi-tile broadcast |
| IMF 2K/4K/8K | `ProfileIMF2K/4K/8K` | Interoperable Master Format |

## Progression Orders

| Order | Constant | Description |
|-------|----------|-------------|
| LRCP | `LRCP` | Layer-Resolution-Component-Position |
| RLCP | `RLCP` | Resolution-Layer-Component-Position |
| RPCL | `RPCL` | Resolution-Position-Component-Layer |
| PCRL | `PCRL` | Position-Component-Resolution-Layer |
| CPRL | `CPRL` | Component-Position-Resolution-Layer |

## Architecture

```
jpeg2000/
├── jpeg2000.go          # Public API, types, image registration
├── decoder.go           # JP2/J2K decoding, colorspace detection
├── encoder.go           # JP2/J2K encoding
├── colorspace.go        # Color conversion functions
└── internal/
    ├── bio/             # Bit I/O utilities
    ├── box/             # JP2 file format box handling
    ├── codestream/      # J2K codestream marker parsing
    ├── dwt/             # Discrete Wavelet Transform (5-3, 9-7)
    ├── entropy/         # MQ coder and EBCOT tier-1
    ├── mct/             # Multi-Component Transform (RCT, ICT)
    └── tcd/             # Tile Coder/Decoder, tier-2
```

## Implementation Status

| Component | Status | Coverage | Notes |
|-----------|--------|----------|-------|
| JP2 Box Parsing | ✅ Complete | 99.3% | All standard box types |
| Codestream Parsing | ✅ Complete | 91.0% | All main/tile-part markers |
| 5-3 DWT (Lossless) | ✅ Complete | 100% | Reversible wavelet |
| 9-7 DWT (Lossy) | ✅ Complete | 100% | Irreversible wavelet |
| MCT (Color Transform) | ✅ Complete | 100% | RCT and ICT |
| MQ Coder | ✅ Complete | 95.7% | Arithmetic coding |
| HTJ2K (Part 15) | ✅ Complete | 90%+ | High-Throughput encoding/decoding |
| EBCOT (Tier-1) | ✅ Complete | 91.9% | All coding passes |
| Packet Assembly (Tier-2) | ✅ Complete | 91.9% | All progression orders |
| Colorspace Conversion | ✅ Complete | 92.8% | All 19 colorspaces |
| Encoder | ✅ Complete | 92.8% | All image types |
| Decoder | ✅ Complete | 92.8% | Full colorspace support |

**Overall Test Coverage: 91-100% across all packages**

## Supported Image Types

### Decoding Output
- `image.Gray` / `image.Gray16` - Grayscale
- `image.RGBA` / `image.RGBA64` - RGB with alpha
- `image.NRGBA` / `image.NRGBA64` - Non-premultiplied RGBA

### Encoding Input
- `image.Gray` / `image.Gray16`
- `image.RGBA` / `image.RGBA64`
- `image.NRGBA` / `image.NRGBA64`
- `image.YCbCr`
- `image.Paletted`

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run with race detection
go test -race ./...

# Run benchmarks
go test -bench=. ./...

# Verbose output
go test -v ./...
```

## Performance

The implementation prioritizes correctness and Go idioms over raw performance. For performance-critical applications, consider:

- Using appropriate tile sizes for your workload
- Reducing resolution levels for preview generation
- Using lossless mode only when necessary

## Conformance

This implementation aims to conform to:

- **ISO/IEC 15444-1:2019** - JPEG 2000 Part 1 (Core)
- **ISO/IEC 15444-15:2019** - JPEG 2000 Part 15 (HTJ2K)
- **ITU-T Rec. T.800** - Equivalent ITU specification
- **OpenJPEG behavior** - API compatibility where applicable

## Known Limitations

- Part 2 (JPX) extensions are not fully supported
- Some advanced features (ROI, progression order changes mid-stream) are limited

## Standards Compliance

This library implements the JPEG 2000 standards as defined by ISO/IEC 15444. The [OpenJPEG](https://github.com/uclouvain/openjpeg) project serves as the official ISO/IEC reference implementation and was consulted for clarification of standard behavior.

## Contributing

Contributions are welcome! Please ensure:
- All tests pass (`go test ./...`)
- Code coverage remains above 90%
- New features include tests
- Documentation is updated

## License

Apache License 2.0. See [LICENSE](LICENSE) file for details.

## References

- [ITU-T Rec. T.800 | ISO/IEC 15444-1](https://www.itu.int/rec/T-REC-T.800) - JPEG 2000 Part 1: Core
- [ISO/IEC 15444-1:2019](https://www.iso.org/standard/78321.html) - Latest standard revision
- [ISO/IEC 15444-15:2019](https://www.iso.org/standard/76621.html) - HTJ2K (High-Throughput JPEG 2000)
- [OpenJPEG](https://github.com/uclouvain/openjpeg) - Reference implementation
- [ExifTool JPEG2000 Tags](https://exiftool.org/TagNames/Jpeg2000.html) - Metadata reference
