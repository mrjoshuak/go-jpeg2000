# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.4.0] - 2026-08-18

This release makes the library interoperable. Before it, no other implementation
could read what this encoder produced, and this decoder could not read anything
another implementation produced — in either direction, for every codec.

Every defect below shares one signature: the encoder and decoder deviated from
the standard in exactly the same way, so each was the other's only witness and
every round-trip test passed throughout. They were found by comparing against
OpenJPH and OpenJPEG, never by round-tripping. `scripts/validate.sh` now runs
those comparisons and fails the build on regression.

### Changed
- **`Encode` with `Options.HighThroughput` now emits conforming T2 packets**
  instead of a private tile container. Output written by earlier versions is
  unreadable by this one and vice versa. This is the reason for the minor
  version bump: the exported API is unchanged, but an existing call now behaves
  differently in a way callers must know about.
- `NumResolutions` is clamped, on the HighThroughput path only, to the levels
  the image can actually carry. A 16x16 image cannot supply six.
- Two guard bits are signalled in QCD. With none, Mb fell short of the U_q the
  HT coder produces and conforming decoders rejected the code-blocks outright.

- **Tiling was declared but never implemented.** `generateSIZ` wrote the tile
  grid into the SIZ marker while `generateTiles` emitted one tile-part holding
  the whole image, so a decoder partitioned by a grid the data did not follow.
  Subband coordinates now derive from absolute tile-component coordinates per
  ISO/IEC 15444-1 B.5, because two tiles of the same size split differently
  when their origins differ in parity.
- **The irreversible path signalled quantization it did not perform.** QCD
  carried Sqcd style 1 (scalar derived) with a five-byte segment holding
  `(100 - Quality) * 256`, which is neither an exponent nor a mantissa. Now
  style 2 with an explicit step size per subband, and the coefficients are
  actually quantized.
- **The float path computed in int32 and wrapped.** After the NLT Type 3 point
  transform a binary32 sample fills the int32 range; one 5/3 level then needs
  33 magnitude bits and the RCT pushes chrominance to 35. Both wrapped, and
  both wrapped invertibly, so round trips passed while OpenJPH decoded 877 of
  1024 samples differently. The transform chain is now 64-bit where the
  magnitude budget requires it.
- The magnitude budget was under-signalled in three places at once: QCD guard
  bits, the `Ccap^15` B_p field (a constant declaring at most 10 bit-planes for
  every file this library ever wrote), and a second independent copy of the QCD
  exponent rule in the packet-header path that disagreed with the first.
- The u-VLC never emitted the four-bit extension the standard reserves for
  u above 32, which 32-bit samples are the first content to reach.
- `Cnlt = 0xFFFF`, the "all components" form OpenJPH writes, was rejected as an
  out-of-range component index, so no OpenJPH float codestream could be read.

### Fixed
- **The 2D wavelet ran its separable passes in the wrong order**, rows-then-
  columns forward and columns-then-rows inverse. ISO/IEC 15444-1 F.3.8.1 fixes
  the inverse as HOR_SR then VER_SR, so the forward must be VER_SD then HOR_SD.
  Because the 5/3 lifting steps floor their intermediates the orders are not
  interchangeable — but they are exact inverses of each other, which is why the
  round trip never noticed.
- **Subband dimensions used ceil for all three detail bands.** A split of an
  odd-length signal yields ceil(n/2) lowpass and floor(n/2) highpass samples,
  and HL, LH and HH do not all take the same one. Images with an odd dimension
  were rejected outright by conforming decoders; even ones hid the defect.
- **The multi-level DWT drivers passed the shrunken level width as the row
  stride**, so from the second decomposition level they addressed the wrong
  memory.
- **The MQ decoder seeded only the uniform context.** ISO/IEC 15444-1 Table D.7
  requires three non-zero initial states (UNIFORM 46, RUN-LENGTH 3, ZC-0 4).
  The first decision of nearly every code-block is a run-length decision, so the
  arithmetic decoder desynchronised immediately.
- **The HT block coder was non-functional in both directions**, recovering
  99.5-100% of coefficients wrongly. Ported from the OpenJPEG and OpenJPH
  references: Scup packing, the VLC codeword-length mask, quad geometry, sample
  placement, MEL run coding, the magnitude reconstruction, and the initial-only
  u-coding rules that were being applied to every stripe.
- **Neither `Encode` nor `Decode` invoked the HT block coder at all.**
  `HighThroughput` set the COD style, wrote CAP and set Rsiz bit 14, then
  emitted Part 1 MQ data under all of it.
- `Rsiz` bit 14 was never set; `Pcap` used `0x00008000` where Part 15 is
  `0x00020000`; the CAP marker omitted its mandatory `Ccap` field; and the NLT
  segment was written short.
- The multiple component transform was signalled for 1- and 2-component images,
  which conforming decoders reject.
- Pooled HT decoders leaked a previous subband's coefficients into positions the
  cleanup pass leaves untouched.
- The tag-tree encoder lacked a `known` flag and set leaf values during coding
  rather than before it, corrupting every packet with more than one code-block.

### Added
- `scripts/validate.sh`: build, vet, `go test -race`, and interoperability
  checks against OpenJPH and OpenJPEG. Each external check runs a control first,
  so a broken oracle is distinguishable from a real failure.
- `internal/dwt` conformance tests comparing the forward transform against a
  literal transcription of Annex F and against coefficients extracted from
  OpenJPH codestreams.
- Golden fixtures decoding OpenJPH and OpenJPEG code-blocks to exact
  coefficients.

### Known limitations
- Part 1 (non-HighThroughput) encoding still writes the private tile container
  and is not interoperable. See the Interoperability section of the README.
- Component subsampling, precinct partitions, multiple quality layers and
  progression orders other than the default are covered only by this library's
  own tests. They are unverified, not known-good.
- The HT encoder emits the cleanup pass only. That is conformant, but leaves no
  room for quality-layer truncation; `EncodeWithRefinement` is not called by the
  live encode path.
- At zero decomposition levels `ojph_compress` signals Mb = 31 for a signed
  32-bit component and loses the sample 0xFFFFFFFF from its own file. This
  encoder measures the transformed coefficients rather than trusting the
  nominal rule, signals 32, and OpenJPH reads our file back exactly. The gate
  records this as a known gap against the reference, not a defect here.

## [1.3.0] - 2026-08-17

### Added
- `HalfImage`, `EncodeHalf`, `DecodeHalf` and `DecodeHalfConfig`: IEEE 754
  binary16 support, carried as the exact 16-bit patterns rather than widened to
  float32. Verified lossless across all 65536 bit patterns, including
  subnormals, negative zero, infinities and signalling NaN payloads. The names
  and shapes mirror the existing `FloatImage` / `EncodeFloat` / `DecodeFloat`
  family.

### Fixed
- **HighThroughput mode emitted non-conforming code-block dimensions.** The COD
  segment signalled `xcb'=ycb'=5`, i.e. 128x128 blocks, whose exponents sum to
  14. ISO/IEC 15444-1 Table A.18 requires `xcb + ycb <= 12`, so conforming
  decoders rejected the codestream outright — OpenJPEG with "Invalid cblkw/cblkh
  combination", and OpenJPH refuses to even produce such a stream. Code-block
  exponents are now clamped to the legal range.
- **The COD marker disagreed with the actual tile partition.** `generateCOD` and
  `encodeTile` derived the code-block size independently, so in HighThroughput
  mode the header advertised a size the payload did not use and any image larger
  than one code-block decoded to a blank frame. Both now share a single
  `codeBlockExponents`.
- **Parallel encoding data race** (remainder of the 1.2.0 fix): the worker read
  `T1.TruncationPoints()` *after* handing its `*T1` back to the pool, so another
  worker could already be encoding into that same `*T1`. The truncation points are
  now copied before `PutT1`. Besides the race, this could silently corrupt the
  layer boundaries of multi-layer (`NumLayers > 1`) codestreams.

### Added
- `TestEncodeParallelMatchesSequential` and `TestEncodeConcurrentStable`, which pin
  the parallel code-block encoder to the sequential one byte-for-byte and would
  catch any future use of pooled `T1` state after it has been released.
- Marker segments declaring a length shorter than their own header no longer
  panic the decoder with `makeslice: len out of range`. A truncated or hostile
  codestream now returns an error from every public decode entry point.

### Security
- **`ExtractPackets` and `BuildPacketIndex` allocated without bound.** The
  packet count is a product of header fields, capped only by a flat constant, so
  a 165-byte codestream could claim four million packet records and allocate
  125 MB building them -- about 800,000x amplification, reachable from
  go-openexr's progressive HTJ2K API. The count is now bounded by the
  codestream's own length: a packet occupies at least one byte on the wire, so a
  file cannot describe more packets than it has bytes. Measured on the same
  input: 125 MB -> 0.3 MB.
- The decode allocation limit is an absolute cap (`maxDecodedSamples`, about
  1 GiB of coefficients), not a ratio to the input length. A ratio cannot tell a
  hostile claim from a legitimately tiny encoding of a large flat image: they
  are the same shape. OpenJPEG compresses a 4096x4096 black image to 190 bytes
  losslessly at default settings — 88,301 samples per input byte — so any ratio
  low enough to constrain an attacker also rejects real files. Amplification
  within the cap is therefore bounded by absolute size rather than by ratio.
- **The decoder trusted header values it read from the file.** Values from SIZ,
  COD, COC, QCD, QCC, POC and NLT flowed straight into `make`, into slice
  indices and into divisions without a range check, so a corrupt or hostile
  codestream could panic the process or hang it. This is remotely triggerable
  through `go-openexr`, which routes any `.exr` using compression 10 or 11
  (HTJ2K) into `Decode`/`DecodeFloat`/`DecodeHalf`. Measured on a valid
  372-byte half codestream, flipping a single byte produced:
  - `makeslice: len out of range` — `XOsiz >= Xsiz` and `YOsiz >= Ysiz` made
    the width computation `Xsiz-XOsiz` wrap to about four billion, and
    `XTOsiz > XOsiz` made the tile bounds run backwards into a negative length.
  - `integer divide by zero` — a decomposition count of 250, or a code-block
    exponent of 251, made a shift wider than the word size evaluate to zero,
    and that zero was then a divisor.
  - Unbounded allocation and non-termination — a wrapped tile count became a
    multi-billion iteration loop bound, and a zero code-block dimension made
    the code-block walk `for cbx := 0; cbx*cbWidth < bandW; cbx++` never
    advance.

  Every header value is now range-checked against ISO/IEC 15444-1 before use,
  and every allocation is additionally bounded by what the input length can
  justify: the image area, the per-tile coefficient area and the tile count are
  all capped relative to the number of bytes actually supplied. Errors name the
  field and the limit. `Decode`, `DecodeConfig`, `DecodeFloat`,
  `DecodeFloatConfig`, `DecodeHalf`, `DecodeHalfConfig`, `DecodeMetadata`,
  `ExtractPackets`, `BuildPacketIndex` and `NewProgressiveDecoderFromCodestream`
  all return an error instead of panicking, hanging or over-allocating. The
  encoder is unchanged and produces byte-identical output.
- **JP2 box contents were allocated from the declared length.** A twelve-byte
  file could name a one-gigabyte box and have it committed up front; the reader
  now grows its buffer as bytes actually arrive.
- **A tile-part declaring `Psot = 0` looped forever** in `BuildPacketIndex`,
  and the SOD scan read past the end of the buffer when `Psot` exceeded the
  bytes present.

### Added
- `FuzzDecodeHalf` and `FuzzDecodeCodestream` with checked-in seed corpora
  under `testdata/fuzz`, covering the historical crashers, plus
  `TestDecodeCorruptedHalfNeverPanics`,
  `TestDecodeCorruptedRawCodestreamNeverPanics`,
  `TestDecodeCorruptedHalfBoundedAllocation` and
  `TestDecodeRejectsOutOfRangeHeaderFields`, which sweep every single-byte
  corruption and truncation of a real codestream through every decode entry
  point and assert no panic, no hang and a bounded allocation.

### Known limitations
- OpenJPH-based decoders (including OpenEXR 3.4+) still reject this library's
  HighThroughput output: the encoder writes a CAP marker but does not set Rsiz
  bit 14 in SIZ, so OpenJPH reports "this is not a JPH file". The code-block fix
  above is necessary but not sufficient for that interoperability.
- `opj_decompress` parses this library's codestreams after the code-block fix but
  does not recover coefficients from them, decoding to a single flat value.
  Reading back with this library is exact; cross-decoder interoperability is not
  yet established.
- `DecodeHalfConfig` rejects `ReduceResolution > 0`. A reduced-resolution decode
  stops the inverse wavelet at an LL subband, so the values are wavelet
  coefficients rather than samples and reinterpreting them as binary16 would
  produce silent garbage.

## [1.2.1] - 2026-02-28

### Fixed
- Generic fallback now detects alpha support from the colour model.

## [1.2.0] - 2026-02-28

### Added
- `EncodeFloat()` API for encoding `FloatImage` with 32-bit float components (bitwise lossless)
- NLT Type 3 marker (0xFF73) generation and parsing for float-to-integer reinterpretation
- 32-bit overflow-safe DWT (`Forward53_32bit` / `Inverse53_32bit`) using int64 intermediates
- 32-bit overflow-safe RCT (`ForwardRCT32` / `InverseRCT32`) for multi-component float images
- Per-component precision: encoder supports mixed bit depths per component (SIZ marker Ssiz)
- Multi-layer encoding with EBCOT truncation points (V2 tile data format)
- Quality layer limiting in decoder: `Config.QualityLayers` truncates code-block data at decode time
- Alpha channel preservation in encoder for RGBA, RGBA64, and generic image paths

### Fixed
- Parallel encoding data race: T1 encoded bytes are now copied before returning T1 to pool
- External JP2 tile data format validation prevents misparse of T2 packet data
- MinInt32 edge case in T1 SetData
- Progressive decoder packet decoding: fed packet data is now decoded into tile coefficients
- Alpha silently dropped during encoding (was hardcoded to 3 components)

### Removed
- Unused HTJ2K decoder scaffolding (MEL decoder methods, VLC helpers, lookup tables)
- Unused struct fields and test helpers flagged by staticcheck

## [1.1.0] - 2026-02-27

### Added
- `ProgressiveDecoder` API for incremental decoding: `NewProgressiveDecoder`, `FeedPacket`, `Reconstruct`
- `NewProgressiveDecoderFromCodestream` convenience constructor
- `FloatImage` type for HDR float precision through the decode pipeline
- `DecodeFloat` / `DecodeFloatConfig` for float-valued image output
- `ExtractPackets` / `BuildPacketIndex` for server-side progressive streaming
- HTJ2K cleanup pass with SPP/MRP refinement support
- Quality layer limiting via `Config.QualityLayers`
- Resolution reduction via `Config.ReduceResolution`

### Fixed
- JP2 boxes with length=0 (extends to EOF) now parsed correctly

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
