# Roadmap

The goal is a complete, interoperable JPEG 2000 implementation in pure Go:
every feature the standard defines, and every one of them demonstrated against
another implementation rather than against ourselves.

## How anything gets marked done here

This project spent its first three releases believing it worked. Every codec
test passed while no other implementation could read its output and it could
read nobody else's, because the encoder and the decoder deviated from the
standard in exactly the same way and each was the other's only witness. The 2D
wavelet ran its two separable passes in the wrong order — an exact inverse of
itself. The float path wrapped in int32, invertibly. Neither was visible to a
round trip at any image size.

So an item below is done when, and only when:

1. A reference implementation reads what we write, bit-exactly where the coding
   is lossless, or inside a tolerance derived from the format where it is not.
2. We read what that reference writes, to the same standard.
3. Both directions run in `scripts/validate.sh`, so the claim cannot rot.
4. The check runs the oracle's own round trip first. A failed measurement and a
   broken oracle look identical otherwise, and this project has been fooled by
   both a broken oracle and a fixture with no signal.

A passing round-trip test is not evidence. Neither is a green suite.

## Now

### Part 1 write interoperability

The only path that still emits a private tile container: a code-block count, a
fixed-width metadata table, then raw block data, which nothing but this library
can read. `HighThroughput` output is already conforming T2.

Two things are known. The comment claiming this is blocked on "a length per
coding pass" is false — both sides already implement the standard
single-length-per-contribution rule. And `buildStandardTileData` hardcoded the
HT bit-plane constant while discarding the real per-block `numBPS`, which is a
genuine defect but not the whole cause: `opj_decompress` parses our packets
without error and still recovers the wrong coefficients.

Done when: `opj_decompress` decodes our Part 1 output to the exact source
samples, `buildTileData` and the decoder's private-container detection are
deleted, and the gate covers Part 1 write the way it covers HTJ2K write.

## Next

### Multiple quality layers

`buildMultiLayerTileData` writes per-layer truncation points into the private
container. Real quality layers are a T2 concept: a code-block contributes
different numbers of coding passes to successive layers, each with its own
length in its own packet. Until this lands, `NumLayers > 1` stays on the private
path and progressive transmission is not interoperable.

Depends on Part 1 write. Done when a reference decoder reconstructs from a
truncated prefix of our codestream, and we reconstruct from a truncated prefix
of theirs, at several layer counts.

### Progression orders

The standard defines five (LRCP, RLCP, RPCL, PCRL, CPRL). We emit and expect one
packet walk, which happens to agree with all five in the degenerate case of a
single precinct, layer and component. Anything else is untested and probably
wrong.

Done when every order round-trips through a reference decoder on an image with
several precincts, layers, components and resolutions — the case where the five
orders actually differ.

### Precinct partitions

`Scod` bit 0 signals explicit precinct sizes; we always use the maximal default.
Precincts are what make packets addressable for streaming, so this matters for
`ExtractPackets` and `ProgressiveDecoder` being useful against foreign files.

Done when we read and write explicit precinct partitions and a reference decoder
agrees, including precincts smaller than a code-block.

### Component subsampling

`XRsiz`/`YRsiz` above 1. The subband geometry already derives from absolute
coordinates, which is most of the work, but nothing exercises unequal component
grids. `PIZChannel` in the sibling go-openexr repository has a matching latent
defect, which suggests the pattern is easy to get wrong.

Done when 4:2:0 and 4:2:2 images round-trip through a reference decoder.

## Later

### HT refinement passes (SPP/MRP)

The encoder emits the cleanup pass only. That is conformant — a single pass is a
legal HT code-block — but it leaves no room for quality-layer truncation within
a block. `EncodeWithRefinement` exists and nothing on the live path calls it.

Depends on quality layers being real. Done when OpenJPH decodes multi-pass HT
blocks from us exactly, and we decode theirs.

### Error resilience markers

SOP and EPH are skipped on read and never written. They cost little and make a
corrupt stream recoverable.

Done when we write both, a reference decoder reads them, and we resynchronise
from a deliberately corrupted stream.

### Region of interest

The RGN marker and the max-shift method. Not implemented in either direction. A
decoder that ignores RGN silently produces a wrongly scaled image, so reading it
matters more than writing it.

### JP2 container conformance

Box parsing is thorough, but the boxes we *write* have never been checked by
another implementation — only the raw codestream has. Palette, channel
definition and component mapping boxes are the likely gaps.

Done when a reference tool reads our JP2 files, not just our J2K codestreams.

## Standing work

### Widen the matrix before widening the claim

`scripts/matrixgen` covers pixel types, component counts, resolution levels,
tilings and both transforms. The day it was widened past 8-bit greyscale it
found three whole capabilities broken — lossy, tiling and float — each of which
had been shipping non-conformant output while the suite was green. Every new
axis is likely to find something.

Axes not yet in it: signed components, bit depths other than 8/16/32, images
whose dimensions are not square, non-zero image offsets (`XOsiz`/`YOsiz`), and
tile origins offset from the image origin.

### Retire the remaining self-referential tests

Several tests still assert this library against itself. They are not worthless,
but they cannot fail in the way that matters. `scripts/mutation/` in the sibling
go-openexr repository shows the shape of the check: break the subject
deliberately and confirm the test dies.

### Performance

No optimisation has been attempted since correctness work began, and some was
undone — the float path now runs a 64-bit transform chain where the magnitude
budget requires it. Worth measuring against OpenJPEG and OpenJPH once the
feature set is complete, not before.
