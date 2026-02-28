# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0] - 2026-02-28

### Added
- `EncodeFloat()` API for encoding `FloatImage` with 32-bit float components (bitwise lossless)
- NLT Type 3 marker (0xFF73) generation and parsing for float-to-integer reinterpretation
- 32-bit overflow-safe DWT (`Forward53_32bit` / `Inverse53_32bit`) using int64 intermediates
- 32-bit overflow-safe RCT (`ForwardRCT32` / `InverseRCT32`) for multi-component float images
- Float roundtrip tests covering NaN, Inf, subnormals, and edge cases

### Fixed
- Parallel encoding data race: T1 encoded bytes are now copied before returning T1 to pool
- External JP2 tile data format validation prevents misparse of T2 packet data
- MinInt32 edge case in T1 SetData

## [1.0.0] - 2026-01-11

### Added
- Pure Go implementation of JPEG 2000 codec (ISO/IEC 15444-1)
- HTJ2K (High-Throughput JPEG 2000) encoding and decoding support (ISO/IEC 15444-15)
- Support for JP2 container format and raw J2K codestreams
- Discrete Wavelet Transform (DWT) with 5-3 reversible and 9-7 irreversible filters
- EBCOT Tier-1 and Tier-2 encoding/decoding
- MQ arithmetic coder
- Multi-component transform (MCT) for RGB/YCbCr conversion
- Multiple resolution levels and quality layers
- Lossless and lossy compression modes
- Full colorspace support (all 19 ISO/IEC 15444-1 colorspaces)
- Integration with Go's `image` package (`image.Image` interface)
- Parallel tile encoding for improved performance
- Optimized T1 encoder with LUT-based context calculation
- Fuzz testing for decoder, codestream parser, and entropy coding
- CODE_OF_CONDUCT.md, CONTRIBUTING.md, SECURITY.md documentation

### Performance
- LUT-based significance context calculation
- Object pooling for reduced allocations
- Inline MQ coder operations
- Parallel tile processing support
