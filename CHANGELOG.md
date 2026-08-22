# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.5.10] - 2026-08-22

### Added
- **The RGN region-of-interest marker is parsed** rather than skipped. Until now
  it appeared only in a table of marker names and the parser fell through to the
  default branch. An ROI style the standard does not define is refused by name
  instead of being assumed to be the one it does, and an RGN naming a component
  the image does not have is refused too.

### Not done, and why
- **The max-shift reconstruction is not implemented**, because no encoder
  available here writes a stream where the shift is applied.
  `opj_compress -ROI c=0,U=8` emits the marker with SPrgn=8 and shifts nothing:
  its ROI and non-ROI streams decode to identical samples — 0 of 1536 differ —
  and the files differ by exactly seven bytes, the RGN segment itself. Kakadu
  and Grok are not installed; OpenJPH has no ROI support.

  An attempt was made and reverted. A threshold downshift of the shape
  OpenJPEG's decoder uses left the lossy case unchanged and broke a case that
  worked — a lossless stream that decoded exactly went to 1528 of 1536 samples
  wrong, because our coefficients are already dequantised where the threshold
  belongs in the raw-magnitude domain. Writing an unverifiable scaling rule
  into a decoder is the failure this project exists to prevent.

  The gate re-measures the limitation every run, so the day an encoder does
  produce a real ROI the check fails and says the oracle now exists.

### Corrected
- An earlier reading of this was wrong and is retracted here. A lossy ROI
  stream differing from the reference on 1439 of 1536 samples was attributed to
  the missing reconstruction. The control — the same lossy stream with no ROI —
  differs by exactly the same 1439. That divergence is about lossy
  rate-truncated decoding of noise and has nothing to do with RGN. The
  measurement was run without a control; the control is what corrected it.

### Gate
- 255 checks, 0 failures, 0 known gaps.

## [1.5.9] - 2026-08-22

### Fixed
- **`EnableSOP` and `EnableEPH` declared markers and wrote none.** Both set
  their bits in the COD's Scod field; neither emitted a single marker. The
  codestream therefore declared a structure it did not have, and **OpenJPEG
  refused an `EnableEPH` file outright** — it expects EPH after each packet
  header when Scod says so and finds code-block data instead. This library
  round-tripped the same file perfectly, because our decoder skips the marker
  only when present and never minded that it never was: both directions agreed
  on a file no other implementation could read.

  Both markers are now written where the standard puts them — SOP before the
  packet with its 16-bit wrapping counter, EPH between the packet header and
  the code-block bodies, including after an empty packet's single-bit header.
  All four combinations decode through `opj_decompress` to exactly the fixture.

### Added
- **Resynchronisation from a damaged packet header**, using SOP as a positive
  check rather than an error fallback. Recovering on a parse error recovers
  nothing here: a packet header is a bit stream with no self-delimiting
  structure, so damaged bits produce a different-but-readable header instead of
  a failure. Measured before the fix — two flipped bytes, four 0xFFs, sixteen
  0xFFs, sixteen zeros over a packet header — every case produced no error, no
  recovery and wrong pixels. A stream that has been writing SOP before every
  packet and then does not is demonstrably out of position, which is checkable
  before parsing anything; all four patterns now recover.

- `DecodeCost.Resyncs`, the number of packets recovered by scanning to the next
  SOP marker. It exists because "it still decoded" is not evidence of
  resynchronisation: the first version of the test asserted exactly that,
  passed, and was recovering nothing. Zero for an undamaged stream, which the
  test also asserts — a decoder that silently recovers from nothing is hiding a
  parse it got wrong.

### Gate
- 253 checks, 0 failures, 0 known gaps. 7 mutations, 0 mismatches. Mutation 64
  stops the EPH marker being written; the pre-existing round trip survives it
  and only the reference-backed check kills it.

## [1.5.8] - 2026-08-22

### Added
- **The JP2 container is now checked by another implementation.** The
  codestreams this library writes have been verified against two oracles across
  the whole capability matrix; the boxes wrapping them had been read only by the
  parser in this same repository. Four wrappers — greyscale 8 and 16 bit, sRGB,
  and a one-resolution RGB case whose codestream is trivial so anything the
  reference objects to is the container — now decode through `opj_decompress`
  to exactly the fixture on every gate run.

  The boxes were already correct; nothing here was broken. What the exercise
  added is a check that can see the class of defect. Mutation 63 swaps the colr
  box's enumerated colourspace, and **the pre-existing JP2 round-trip test
  survives it** — our decoder takes the component count from the codestream's
  SIZ marker and never consults the box, so a wrong enumeration round-trips
  perfectly here while telling every other implementation that a
  three-component image is greyscale.

- **The conformance matrix gained five axes.** Non-square dimensions in both
  orientations on the integer and float paths; a non-zero image offset, which
  our encoder cannot write at all so the gate has `opj_compress -d 7,5` write
  one and compares our decode against the reference's own; and a tile grid
  origin offset from the image origin. All clean, and they stay in the matrix —
  an axis that passes is evidence, and an axis removed after passing is
  evidence thrown away.

### Fixed
- **`FloatImage.BitDepth` and `Signed` were accepted and ignored.** `BitDepth:
  12` produced a codestream declaring 32-bit signed with no indication — the
  same shape as `Config.DecodeArea` before v1.5.2, a field declared,
  documented, and read by nothing. They cannot be honoured: the float path
  reinterprets binary32 bit patterns as signed 32-bit samples, so Ssiz can only
  say 32-bit signed.

  A refusal was written first and reverted. Three existing tests pass
  `BitDepth: 16` and `Signed: false` and their files are correct, so refusing
  would have broken working callers to make a point. The fields are documented
  as decoder output instead, and `TestFloatImageBitDepthIsOutputOnly` asserts
  every value produces Ssiz 0x9F, so the asymmetry fails loudly if it changes.

### Gate
- 250 checks, 0 failures, 0 known gaps. 6 mutations, 0 mismatches.

## [1.5.7] - 2026-08-20

### Added
- **A mutation harness**, `scripts/mutation/`, ported from the sibling
  go-openexr repository, closing the ROADMAP item "Retire the remaining
  self-referential tests" with a measurement instead of a retirement. The
  manifest carries five deliberate defects, each a bug this library actually
  shipped or the exact shape of one. The measurement: **every pre-existing
  round-trip test survives every one of the five.** Each is killed only by an
  anchored test — cost-anchored for the two savings, wire-anchored for PLT, and
  for the NLT mask a new `TestNLTType3MatchesTheDefinition` whose expected
  values are literals. `nltType3` is one involution shared by encoder and
  decoder, so a wrong mask still undoes itself perfectly and every round trip
  passes while the wire means something else to every other reader. No test was
  retired: a surviving round trip still guards what it guards, and the harness
  now states exactly what that is and is not.
- **CI runs the conformance gate on both architectures**, with the mutation
  harness beside it. This package carries per-architecture assembly
  (`internal/dwt` for amd64 and arm64, `internal/entropy` for amd64), and the
  sibling repository's first amd64 gate run found two assembly declaration
  errors its arm64 development machine had never built.

## [1.5.6] - 2026-08-19

### Fixed
- **`ReduceResolution` was refused, and the refusal was a measurement error.**
  It rested on comparing a reduced decode against a downsample of the full
  decode; the LL band at resolution r is the image the wavelet reconstructs at
  that scale, not an arithmetic average of the finer one, so the two disagree
  by construction. Against the reference's own reduced decode this library was
  already bit-exact: 8 configurations against `ojph_expand -skip_res` on the
  float path, 4 against `opj_decompress -r` on the half path.
- **A reduced decode cost exactly what a full one did**, entropy-decoding every
  resolution and discarding the ones above the target. Now: reduce 1 decodes
  65.7% of the code-block bytes, reduce 3 15.0%, reduce 5 2.2% (256x256, 6
  resolutions).
- **The wide-sample narrowing wrapped silently.** A reduced decode of binary32
  content using the extremes of the NLT word produces an LL coefficient outside
  the range the word maps from; `finish` truncated it with a plain int32
  conversion — 9 of 256 samples wrong against OpenJPH, each by exactly the
  sign-magnitude complement, nothing reported. It returns
  `ErrSampleNotRepresentable` now, naming component, sample and value.

### Gate
- 243 checks. New: float reduced decodes against `ojph_expand -skip_res` (on a
  magnitude-bounded fixture, because unbounded content leaves the range the
  format defines an answer for), half reduced decodes against
  `opj_decompress -r` via the new `scripts/halfpgm`, the non-representable case
  reported rather than wrapped, and the measured cost.

## [1.5.5] - 2026-08-19

### Fixed
- **A codestream written with `WritePacketLengths` could not be read back.**
  `indexTileParts` required SOD immediately after SOT, which holds only when
  the tile-part header is empty; a PLT marker sits between the two, so every
  tile was indexed as absent and the decode produced DC-shifted nothing —
  **99.6% of samples wrong** while OpenJPEG read the same bytes correctly. It
  walks the marker segments to SOD now, as `packets.go` already did. The gate
  never caught it because PLT was validated against OpenJPEG only, and the
  encoder was right the whole time: a defect on this side of the round trip is
  invisible to an external oracle by construction.

### Gate
- 239 checks. New: our own decoder reads our own PLT codestream, with and
  without precincts, against the source image rather than a PLT-free decode.

## [1.5.4] - 2026-08-19

### Added
- `HalfImage.Cost` and `FloatImage.Cost`, carrying the figures
  `DecodeConfigCost` returns, because those concrete types are the only entry
  points an EXR HTJ2K chunk ever reaches and a region gated on the
  `image.Image` path alone would leave them unchecked. Both paths are now gated
  for exactness and for skipping. Writing the checks measured the geometry: a
  code-block's influence is its band rectangle grown by the synthesis margin,
  about 64 samples at the lowest resolution of a five-level decode, so below
  roughly 256x256 nothing can be skipped and a skip assertion on a small
  fixture measures the image size rather than the code.

## [1.5.3] - 2026-08-19

### Fixed
- **`Config.DecodeArea` is implemented.** Declared, documented as "specifies a
  region to decode", and read by nothing; v1.5.2 made it refuse rather than
  mislead, and it now works. The samples are exactly the ones a full decode
  produces for that rectangle, the returned image is allocated for the region,
  and code-blocks the region cannot reach are not entropy-decoded: a 64x64
  region of a 256x256 image with 32x32 precincts decodes 5468 of 24913
  code-block bytes (22%). The skip geometry took two attempts — detail bands
  sit at half their resolution's grid and scale by one factor more than the LL
  band; treating them alike was 254 of 256 samples wrong in a corner while the
  middle of the image was fine.

### Added
- `DecodeConfigCost` and `DecodeCost`, so the saving a region buys is a
  measurement rather than a claim.

## [1.5.2] - 2026-08-19

### Fixed
- **`Config.DecodeArea` was ignored.** It was declared, documented as
  "specifies a region to decode", and referenced nowhere in the decoder, so a
  caller asking for a 32x16 region received the whole 64x32 image with no
  indication the request had been dropped — and would then read the wrong
  pixels out of a buffer sized for the answer it asked for. It returns an error
  until region decode is implemented.
- **`Config.ReduceResolution` returned wavelet-domain values as floats.** A
  reduced-resolution decode stops the inverse wavelet at an LL subband. For
  ordinary integer samples those are still samples, and the result is correct
  to within a count or two on a ramp. For a codestream carrying an NLT point
  transform they are what NLT maps back from rather than samples, and
  reinterpreting them gave values off by 175 on a ramp spanning 0 to 2 — with
  the dimensions correct, which is what made it look like it worked. The half
  path had refused this since it was measured there; the float path now refuses
  it too, and only where it applies.

Both refusals are gated, together with the integer case that must keep working,
so a guard that rejected everything would fail.

## [1.5.1] - 2026-08-19

Everything a codestream needs to be addressable — precincts, packet length
markers, the five progression orders — plus component subsampling and the read
direction of quality layers. The gate grew from 169 checks to 235 and now
reports no known gaps.

### Fixed
- **Explicit precinct partitions were ignored entirely.** `Scod` bit 0 declares
  a partition, which makes a resolution hold one packet per precinct instead of
  one packet in total. This library read every such codestream against a single
  maximal precinct, so from the second packet onward it parsed packet headers at
  the wrong offsets: 65114 of 65536 samples wrong against an OpenJPEG fixture.
  `ROADMAP.md` claimed `validate.sh` measured this every run; `validate.sh`
  contained no reference to precincts at all. Three things had to be right and
  each failed on its own — the partition anchored at zero in the resolution's
  coordinates with the band-space origin taken from the resolution origin, the
  code-block partition clipped to the precinct (B.7), and the coordinate walk of
  B.12.1.4 for PCRL and CPRL.
- **The five progression orders were one walk.** LRCP, RLCP, RPCL, PCRL and
  CPRL agree only when there is a single precinct, layer and component, which
  is all this library ever wrote, so nothing ever disagreed. PCRL and CPRL put
  position outside resolution, where precinct index *p* names a different region
  at every resolution; walking them by index decodes each against the wrong
  precinct.
- **Subsampled components landed in a corner of the image.** `XRsiz`/`YRsiz`
  above 1 put a component on a coarser grid, where one sample covers `XRsiz` by
  `YRsiz` of the reference grid (A.5.1). Every component was written to the
  output plane at its own index, so a half-resolution component filled a quarter
  of the plane and the rest stayed zero — 4091 of 4096 chroma samples wrong on a
  4:2:0 fixture, beside a full-resolution component that was exact.
- **The multiple component transform was applied to components that did not
  share a grid**, which the format forbids and which read past the end of the
  shorter plane.

### Added
- `Options.PrecinctSizes` writes an explicit precinct partition, and
  `PacketAddress.Precinct` now means something. `PacketIndex.Range` gives a
  packet's byte range in the codestream and `PacketIndex.PacketsForRegion`
  resolves an image rectangle to the packets covering it: a 64x64 viewport of a
  256x256 image is 7 of 85 packets and 2519 of 21127 bytes.
- `Options.WritePacketLengths` writes `PLT` in every tile-part header and `TLM`
  in the main header, and `BuildPacketIndex` uses them instead of walking the
  packets. `PacketIndex.IndexCost` reports what that saved: indexing a 128x128
  image reads 152 of 5486 bytes, a 512x512 image 788 of 85048 — the fraction
  falls as the image grows, because the cost follows the packet count rather
  than the data.
- `Options.ComponentSubsampling` writes subsampled components, by decimation
  rather than averaging, since the format specifies no filter and decimation is
  the one choice a decoder inverts exactly. 4:2:0, 4:2:2 and 4:1:1 are gated,
  tiled and lossy included.
- The read direction of quality layers is gated: each prefix of OpenJPEG's
  five-layer codestream improves this library's reconstruction — 1240, 883, 344,
  0, 0 — and every prefix yields the whole image rather than part of one.

### Removed
- The HT SPP/MRP encoder. It was never reachable from the live encoder and
  round-tripped only through this package's own decoder; handed to OpenJPH its
  output produced `ojph error 0x000300A1 ... Error decoding a codeblock`. Both
  halves shared one deviation from the standard, so it was deleted rather than
  left looking usable. A single cleanup pass is a conforming HT code-block, and
  quality layers already truncate at packet granularity.

## [1.5.0] - 2026-08-18

Part 1 output is now interoperable, which was the last path this library wrote
that nothing else could read. Both block coders emit conforming T2 packets and
the private tile container is gone.

### Changed
- **`Encode` without `HighThroughput` now emits conforming T2 packets** instead
  of a private tile container, so Part 1 output is readable by other
  implementations for the first time. Files written by earlier versions are not
  readable by this one, and vice versa. This is the reason for the minor bump:
  the exported API is unchanged, but an existing call behaves differently.
- Quality layers are real T2 layers rather than truncation points in a private
  table. A reference decoder reconstructs from a truncated prefix of our
  codestream, improving monotonically with each layer.

### Fixed
- **The coding-pass count in the packet header was the bit-plane count.** A Part
  1 code-block with n magnitude bit-planes contributes 3n-2 coding passes
  (D.3); we signalled n. Because the pass count only sizes the length field,
  OpenJPEG parsed our packets without error, ran its block decoder for n of the
  3n-2 passes, stopped early and produced wrong pixels — zero errors reported,
  nothing correct decoded.
- The zero-bit-plane count was computed with the HT bit-plane constant and
  applied to MQ blocks, discarding the real per-block magnitude count.
- **SOP and EPH packet markers were read as packet-header bits.** A decoder that
  reads through an SOP segment takes six bytes of marker as header and recovers
  nothing after it; 99.6% of samples came back wrong on any codestream using
  them. Both markers are now skipped where the coding style declares them.

### Added
- `scripts/validate.sh` grew from 118 to 169 external checks: Part 1 write
  against OpenJPEG across sizes, resolution counts, tile grids, colour, quality
  layers and all five progression orders; and SOP/EPH read fixtures.
- `ROADMAP.md`, listing what the format supports that this library does not yet,
  with the acceptance standard for each.

### Known limitations
- A codestream declaring more than one precinct per resolution is mis-read. The
  gate measures this and reports it as a gap rather than hiding it.
- Component subsampling is unverified against a reference.
- The HT encoder emits the cleanup pass only. Conformant, but it leaves no room
  for quality-layer truncation within a block.

## [1.4.1] - 2026-08-18

### Documentation
- The feature list contradicted the rest of the README: it advertised "full
  cleanup + refinement passes (SPP/MRP)" while the implementation-status table
  and the interoperability section both recorded that the encoder emits the
  cleanup pass only. Corrected to say what the code does — the encoder writes
  cleanup, the decoder reads cleanup, SPP and MRP.
- Tiling, interoperable output and the widened float range were implemented in
  1.4.0 but never listed as features. Added, with the guarantees each one
  actually carries.
- `HTJ2K_REQUIREMENTS.md` still described the pre-implementation state as
  current. Marked superseded, with the original assessment retained as the
  specification that was worked to.
- `CONTRIBUTING.md` now documents `scripts/validate.sh` and why a round-trip
  test is not sufficient evidence in this codebase.

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
