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

## Why this order

The ordering below follows a use this codec is unusually well suited to, and
which the wrapping formats are not: serving one artifact at many resolutions and
regions straight out of object storage.

`BuildPacketIndex` already locates every packet as a byte range in the
codestream without copying it. Given precincts, those ranges are spatially
addressable, so a viewport at a chosen resolution becomes an HTTP range request
against the original file — no proxy pyramid to generate, no transcode service,
no second copy to keep in sync. Quality layers add the other axis: a usable
image early on a poor connection, refining as bytes arrive. That is what JPIP
was designed around, and it is inherent to JPEG 2000 rather than bolted on.

Two consequences for what follows.

**Precincts are the addressability unit.** Without them there is one packet per
(layer, resolution, component), covering the whole image at that resolution.
Region-of-interest decode does not exist without precincts; it is not an
optimisation on top of them. That is why a defect filed as rare now sits first.

**Progressive means sharper, not cleaner, until SPP/MRP lands.** The HT encoder
emits the cleanup pass only, so there is one coding pass per block and nothing
to distribute across layers. Resolution progression works today; quality
progression does not, and quality is the axis that matters over a network.

Neither item was near the top when this file was written. Both are load-bearing
for the use above, which is the reason they moved.

## Now

### Precinct partitions

`Scod` bit 0 signals explicit precinct sizes; we always write the maximal
default, and we **mis-read** any codestream that declares more than one precinct
per resolution — measured at 65217 of 65536 samples wrong against an OpenJPEG
fixture, and reproducible before any of this release's work. `validate.sh`
measures it every run and reports it as a gap rather than skipping it.

Closing it needs a precinct dimension in the progression walk (a real coordinate
walk for RPCL/PCRL/CPRL, B.12.1.3-4), per-precinct inclusion and zero-bit-plane
tag trees, and the code-block partition clipped to precinct boundaries. It also
gives `PacketAddress.Precinct` a meaning; it is currently always 0.

Done when we read and write explicit precinct partitions and a reference decoder
agrees, including precincts smaller than a code-block, and when a packet index
over a multi-precinct file resolves a given image region to the byte ranges that
cover it.

### HT refinement passes (SPP/MRP)

The encoder emits the cleanup pass only. That is conformant — a single pass is a
legal HT code-block — but it leaves no room for quality-layer truncation within
a block. `EncodeWithRefinement` exists and nothing on the live path calls it.

Depends on quality layers being real. Done when OpenJPH decodes multi-pass HT
blocks from us exactly, we decode theirs, and a decoder given a truncated prefix
of one of our codestreams produces a complete image at reduced quality rather
than a partial one.


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


### Component subsampling

`XRsiz`/`YRsiz` above 1. The subband geometry already derives from absolute
coordinates, which is most of the work, but nothing exercises unequal component
grids. `PIZChannel` in the sibling go-openexr repository has a matching latent
defect, which suggests the pattern is easy to get wrong.

Done when 4:2:0 and 4:2:2 images round-trip through a reference decoder.

## Later


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
