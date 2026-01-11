# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- HTJ2K (High-Throughput JPEG 2000) encoding and decoding support (ISO/IEC 15444-15)
- Fuzz testing for decoder, codestream parser, and entropy coding
- Comprehensive test coverage for HTJ2K code paths
- CODE_OF_CONDUCT.md, CONTRIBUTING.md, SECURITY.md documentation

### Fixed
- Nil pointer check for ComponentInfo access in decoder
- Bounds checking for tile component data
- Division by zero when Quality option is 0
- Silent error handling for tile initialization failures

## [0.1.0] - 2024

### Added
- Pure Go implementation of JPEG 2000 codec (ISO/IEC 15444-1)
- Support for JP2 container format and raw J2K codestreams
- Discrete Wavelet Transform (DWT) with 5-3 reversible and 9-7 irreversible filters
- EBCOT Tier-1 and Tier-2 encoding/decoding
- MQ arithmetic coder
- Multi-component transform (MCT) for RGB/YCbCr conversion
- Multiple resolution levels and quality layers
- Lossless and lossy compression modes
- Integration with Go's `image` package (`image.Image` interface)
- Parallel tile encoding for improved performance
- Optimized T1 encoder with LUT-based context calculation

### Performance
- LUT-based significance context calculation
- Object pooling for reduced allocations
- Inline MQ coder operations
- Parallel tile processing support
