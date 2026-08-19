# go-jpeg2000

[![Go Reference](https://pkg.go.dev/badge/github.com/mrjoshuak/go-jpeg2000.svg)](https://pkg.go.dev/github.com/mrjoshuak/go-jpeg2000)
[![Go Report Card](https://goreportcard.com/badge/github.com/mrjoshuak/go-jpeg2000)](https://goreportcard.com/report/github.com/mrjoshuak/go-jpeg2000)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

A pure Go implementation of the JPEG 2000 image codec (ISO/IEC 15444-1) with HTJ2K support (ISO/IEC 15444-15), verified interoperable with the reference implementations in both directions.

## Overview

**Every codestream this library writes is decoded to the exact source samples by
OpenJPEG and OpenJPH, and every codestream they write is decoded to the exact
source samples by this library.** That holds for Part 1 and for HTJ2K, lossless
5/3 and irreversible 9/7, 8- and 16-bit integer and binary32 float, one and
three components, tiled and untiled, across resolution levels and quality
layers. 169 checks against those two implementations run in
`scripts/validate.sh` and fail the build on any regression.

No cgo. The codec, both wavelets, the MQ arithmetic coder, the HT block coder
and the packet layer are all Go.

That claim is the point of the project rather than a footnote, because it is the
one a round-trip test cannot make — and this library passed its own tests for
three releases while no other implementation could read a single file it wrote.
See [Interoperability](#interoperability) for exactly what has been measured,
and [ROADMAP.md](ROADMAP.md) for what has not.

## Features

- **Pure Go**: No CGO dependencies, works on all Go-supported platforms
- **Format Support**: JP2 file format and raw J2K codestream
- **HTJ2K Support**: High-Throughput JPEG 2000 (ISO/IEC 15444-15). The encoder emits the cleanup pass; the decoder reads cleanup, SPP and MRP
- **Interoperable Output**: HTJ2K codestreams are decoded bit-exactly by OpenJPH, and OpenJPH's and OpenJPEG's codestreams are decoded bit-exactly by this library. Verified by `scripts/validate.sh` against both reference implementations on every release
- **Tiling**: `Options.TileSize` partitions the image into independently coded tiles, with subband geometry derived from absolute tile coordinates (ISO/IEC 15444-1 B.5) so tiles at any origin split correctly
- **Progressive Decode**: Incremental decoding via `ProgressiveDecoder` — feed packets as they arrive, reconstruct at any point
- **Float Image Output**: `FloatImage` type preserves HDR float precision through the decode pipeline
- **Packet Extraction**: `ExtractPackets` / `BuildPacketIndex` for server-side progressive streaming
- **Lossless & Lossy**: Reversible 5/3 and irreversible 9/7, both verified against the reference implementations
- **Full Colorspace Support**: All 19 ISO/IEC 15444-1 colorspaces with automatic conversion to sRGB
- **32-bit Float Encoding**: `EncodeFloat` preserves IEEE 754 float32 values via NLT Type 3 markers (bitwise lossless), including infinities, NaN payloads, denormals and both zeros. The transform chain widens to 64 bits where the magnitude budget requires it, since a binary32 pattern fills the int32 range after the point transform
- **Flexible Precision**: 1-32 bit component precision, including 4-bit, 10-bit, 12-bit, and 32-bit float
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

### Float Encoding (32-bit HDR)

```go
// Encode float32 data with bitwise-lossless preservation
floatImg := &jpeg2000.FloatImage{
    Width:      width,
    Height:     height,
    Components: [][]float32{rChannel, gChannel, bChannel},
    BitDepth:   32,
    Signed:     true,
}

opts := jpeg2000.DefaultOptions()
opts.Format = jpeg2000.FormatJ2K

err := jpeg2000.EncodeFloat(file, floatImg, opts)
```

Float encoding uses NLT Type 3 markers to reinterpret IEEE 754 float32 bits as int32 with a sign-magnitude transform, preserving all values including NaN, Inf, and -0.0.

### Float Decoding (HDR)

```go
// Preserve float precision through the decode pipeline
floatImg, err := jpeg2000.DecodeFloat(file)
// floatImg.Components[0] is []float32 for the first component (e.g. R)
// floatImg.Components[1] is []float32 for G, etc.

// With config options
cfg := &jpeg2000.Config{ReduceResolution: 2}
floatImg, err = jpeg2000.DecodeFloatConfig(file, cfg)
```

### Progressive Decoding

```go
// Parse the codestream header, then feed packets incrementally
decoder, err := jpeg2000.NewProgressiveDecoderFromCodestream(codestreamBytes)

// Feed packets as they arrive (any order)
for _, pkt := range packets {
    decoder.FeedPacket(pkt)
}

// Reconstruct the best image from data received so far
floatImg, err := decoder.Reconstruct()

// Check progress
fmt.Printf("Received %d packets, complete: %v\n",
    len(decoder.ReceivedPackets()), decoder.Complete())
```

### Packet Extraction (Server-Side)

```go
// Extract all packets from an encoded codestream
packets, err := jpeg2000.ExtractPackets(codestream)

// Or build a zero-copy index for memory-mapped files
index, err := jpeg2000.BuildPacketIndex(codestream)
pkt, err := index.GetPacket(addr)
addrs := index.AllAddresses()
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

| enumcs | Colorspace  | API Constant         | Description                         |
| ------ | ----------- | -------------------- | ----------------------------------- |
| 0      | Bi-level    | `ColorSpaceBilevel`  | Black and white                     |
| 1      | YCbCr(1)    | `ColorSpaceSYCC`     | ITU-R BT.709-5 (sRGB primaries)     |
| 3      | YCbCr(2)    | `ColorSpaceYCbCr2`   | ITU-R BT.601-5 (625-line PAL/SECAM) |
| 4      | YCbCr(3)    | `ColorSpaceYCbCr3`   | ITU-R BT.601-5 (525-line NTSC)      |
| 9      | PhotoYCC    | `ColorSpacePhotoYCC` | Kodak Photo CD                      |
| 11     | CMY         | `ColorSpaceCMY`      | Cyan, Magenta, Yellow               |
| 12     | CMYK        | `ColorSpaceCMYK`     | CMY + Key (Black)                   |
| 13     | YCCK        | `ColorSpaceYCCK`     | PhotoYCC + Key                      |
| 14     | CIELab      | `ColorSpaceCIELab`   | CIE L\*a\*b\* (D50)                 |
| 15     | Bi-level(2) | `ColorSpaceBilevel`  | Alternative bi-level                |
| 16     | sRGB        | `ColorSpaceSRGB`     | Standard RGB (IEC 61966-2-1)        |
| 17     | Grayscale   | `ColorSpaceGray`     | Single component gray               |
| 18     | sYCC        | `ColorSpaceSYCC`     | sRGB-based YCbCr                    |
| 19     | CIEJab      | `ColorSpaceCIEJab`   | CIECAM02 J\*a\*b\*                  |
| 20     | e-sRGB      | `ColorSpaceESRGB`    | Extended gamut sRGB                 |
| 21     | ROMM-RGB    | `ColorSpaceROMMRGB`  | ProPhoto RGB (ISO 22028-2)          |
| 22     | YPbPr(60)   | `ColorSpaceYPbPr60`  | HD video 1125/60 (SMPTE 274M)       |
| 23     | YPbPr(50)   | `ColorSpaceYPbPr50`  | HD video 1250/50 (ITU-R BT.1361)    |
| 24     | e-sYCC      | `ColorSpaceEYCC`     | Extended gamut sYCC                 |

### Colorspace Handling

- **Automatic Conversion**: All colorspaces are automatically converted to sRGB during decode
- **OpenJPEG Compatible**: API values 0-5 match OpenJPEG's `OPJ_COLOR_SPACE` enum
- **Unspecified vs Unknown**:
  - `ColorSpaceUnspecified` (0): Returned for raw J2K codestreams without JP2 container
  - `ColorSpaceUnknown` (-1): Returned for unrecognized enumcs values

### Color Conversion Details

The decoder applies mathematically correct color transformations based on the specifications:

| Colorspace     | Conversion Method                    |
| -------------- | ------------------------------------ |
| YCbCr variants | ITU-R BT.601/709 matrix inversion    |
| CMY/CMYK       | Subtractive color model              |
| CIELab         | Lab→XYZ→sRGB with D50→D65 adaptation |
| CIEJab         | CIECAM02 inverse (simplified)        |
| ROMM-RGB       | Wide gamut to sRGB with clipping     |
| PhotoYCC       | Kodak-specific YCC matrix            |

## JPEG 2000 Profiles

Supported profiles (RSIZ parameter):

| Profile          | Constant                 | Description                 |
| ---------------- | ------------------------ | --------------------------- |
| None             | `ProfileNone`            | No restrictions             |
| Cinema 2K        | `ProfileCinema2K`        | 2K Digital Cinema           |
| Cinema 4K        | `ProfileCinema4K`        | 4K Digital Cinema           |
| Cinema S2K       | `ProfileCinemaS2K`       | 2K Scalable Cinema          |
| Cinema S4K       | `ProfileCinemaS4K`       | 4K Scalable Cinema          |
| Cinema SLTE      | `ProfileCinemaSLTE`      | Long-term extension         |
| Broadcast Single | `ProfileBroadcastSingle` | Single-tile broadcast       |
| Broadcast Multi  | `ProfileBroadcastMulti`  | Multi-tile broadcast        |
| IMF 2K/4K/8K     | `ProfileIMF2K/4K/8K`     | Interoperable Master Format |

## Progression Orders

| Order | Constant | Description                         |
| ----- | -------- | ----------------------------------- |
| LRCP  | `LRCP`   | Layer-Resolution-Component-Position |
| RLCP  | `RLCP`   | Resolution-Layer-Component-Position |
| RPCL  | `RPCL`   | Resolution-Position-Component-Layer |
| PCRL  | `PCRL`   | Position-Component-Resolution-Layer |
| CPRL  | `CPRL`   | Component-Position-Resolution-Layer |

## Architecture

```
jpeg2000/
├── jpeg2000.go          # Public API, types, image registration
├── decoder.go           # JP2/J2K decoding, colorspace detection
├── encoder.go           # JP2/J2K encoding (integer + float32)
├── nlt.go               # Non-Linearity Transform (NLT Type 3 for float)
├── colorspace.go        # Color conversion functions
├── floatimage.go        # FloatImage type for HDR output
├── progressive.go       # ProgressiveDecoder for incremental decode
├── packets.go           # Packet extraction and indexing
└── internal/
    ├── bio/             # Bit I/O utilities
    ├── box/             # JP2 file format box handling
    ├── codestream/      # J2K codestream marker parsing
    ├── dwt/             # Discrete Wavelet Transform (5-3, 9-7)
    ├── entropy/         # MQ coder, EBCOT tier-1, HTJ2K (cleanup; decoder also SPP/MRP)
    ├── mct/             # Multi-Component Transform (RCT, ICT)
    └── tcd/             # Tile Coder/Decoder, tier-2
```

## Interoperability

Interoperability is what this library is for, and it is the one property a
round-trip test cannot establish. Until v1.4.0 every codec test here passed
while nothing else could read the output and nothing else's output could be
read: encoder and decoder shared conventions no other implementation uses, so
each was the other's only witness. What follows is therefore stated in terms of
what an *external* decoder was measured doing, not in terms of test coverage.

`scripts/validate.sh` runs these checks against separately installed reference
tools and fails the build if any regresses.

| Direction                              | Status                                                                                                                                                                                                                      |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HTJ2K write (`Options.HighThroughput`) | **Conforming.** OpenJPH decodes our codestreams to the exact source samples: 17-200 px, 1-6 resolution levels, tiled and untiled, 8/16-bit integer, binary32 float, 1 and 3 components, reversible 5/3 and irreversible 9/7 |
| HTJ2K read                             | **Conforming.** We decode OpenJPH's codestreams exactly, 0-5 decomposition levels, tiled and untiled, integer and float                                                                                                     |
| Part 1 read (MQ / EBCOT)               | **Conforming.** We decode OpenJPEG's codestreams exactly, including tiled streams and SOP/EPH packet markers                                                                                                                |
| Part 1 write (MQ / EBCOT)              | **Conforming.** OpenJPEG decodes our codestreams to the exact source samples: 17-200 px, 1-6 resolution levels, tiled and untiled, 8/16-bit, colour, 2-8 quality layers, all five progression orders, 5/3 and 9/7           |


### What these measurements cover

`scripts/validate.sh` runs 169 external checks against OpenJPEG and OpenJPH and
fails the build on any regression. The matrix spans both block coders, 8- and
16-bit integer greyscale, three-component colour, binary32 float, reversible and
irreversible transforms, 1 to 6 resolution levels, tile sizes that both do and
do not divide the image, 2 to 8 quality layers, all five progression orders, and
SOP/EPH packet markers.

Float cases are compared through PFM: `ojph_expand` cannot write a 32-bit
component to PGM or PPM — it emits an all-zero raster with maxval 0, for its own
codestreams as readily as for ours — so a float case checked through PGM
measures nothing. Every read-side check runs the oracle's own round trip first,
so a broken oracle stays distinguishable from a real defect.

Float fixtures use the whole binary32 encoding — both zeros, both infinities,
quiet NaNs, smallest and largest denormals, FLT_MAX — not small positive
integers, which is what hid a 32-bit overflow in the wavelet: they never needed
the 33rd magnitude bit.

Still exercised only by this library's own tests, and so unverified rather than
known-good. [ROADMAP.md](ROADMAP.md) tracks all of it:

- precinct partitions: a codestream declaring more than one precinct per
  resolution is currently mis-read, and the gate measures this rather than
  hiding it
- component subsampling
- the HT SPP/MRP refinement passes: `HTEncoder.Encode` emits the cleanup pass
  only, which is conformant but leaves no room for quality-layer truncation

## Implementation Status

| Component                | Status      | Coverage | Notes                                                                                                      |
| ------------------------ | ----------- | -------- | ---------------------------------------------------------------------------------------------------------- |
| JP2 Box Parsing          | ✅ Complete | 99.3%    | All standard box types                                                                                     |
| Codestream Parsing       | ✅ Complete | 91.0%    | All main/tile-part markers                                                                                 |
| 5-3 DWT (Lossless)       | ✅ Complete | 100%     | Reversible wavelet                                                                                         |
| 9-7 DWT (Lossy)          | ✅ Complete | 100%     | Irreversible wavelet                                                                                       |
| MCT (Color Transform)    | ✅ Complete | 100%     | RCT and ICT                                                                                                |
| MQ Coder                 | ✅ Complete | 95.7%    | Arithmetic coding                                                                                          |
| HTJ2K (Part 15)          | ✅ Complete | 90%+     | Cleanup pass, verified against OpenJPH in both directions; encoder does not emit SPP/MRP                   |
| EBCOT (Tier-1)           | ✅ Complete | 91.9%    | All coding passes                                                                                          |
| Packet Assembly (Tier-2) | ✅ Complete | 91.9%    | Conforming packets for both block coders, including tiling, quality layers and all five progression orders |
| Colorspace Conversion    | ✅ Complete | 92.8%    | All 19 colorspaces                                                                                         |
| Encoder                  | ✅ Complete | 92.8%    | All image types                                                                                            |
| Decoder                  | ✅ Complete | 92.8%    | Full colorspace support                                                                                    |

**Overall Test Coverage: 91-100% across all packages**

## Supported Image Types

### Decoding Output
- `image.Gray` / `image.Gray16` - Grayscale
- `image.RGBA` / `image.RGBA64` - RGB with alpha
- `image.NRGBA` / `image.NRGBA64` - Non-premultiplied RGBA
- `jpeg2000.FloatImage` - Planar float32 components (via `DecodeFloat`)

### Encoding Input
- `image.Gray` / `image.Gray16`
- `image.RGBA` / `image.RGBA64`
- `image.NRGBA` / `image.NRGBA64`
- `image.YCbCr`
- `image.Paletted`
- `jpeg2000.FloatImage` - Planar float32 components (via `EncodeFloat`)

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
