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

Three consequences for what follows.

**Precincts are the addressability unit.** Without them there is one packet per
(layer, resolution, component), covering the whole image at that resolution.
Region-of-interest decode does not exist without precincts; it is not an
optimisation on top of them. That is why a defect filed as rare now sits first.

**Locating a packet must be cheap.** Packet offsets are content-dependent, so
an index built for one frame is worthless for the next — and building one by
walking the codestream is a chain of small dependent reads. `PLT` and `TLM`
carry the lengths in the headers, which turns index construction into a single
ranged read and makes a rolling prefetch across a frame sequence workable.

**Progressive means sharper, not cleaner, until SPP/MRP lands.** The HT encoder
emits the cleanup pass only, so there is one coding pass per block and nothing
to distribute across layers. Resolution progression works today; quality
progression does not, and quality is the axis that matters over a network.

None of the three was near the top when this file was written. All three are
load-bearing for the use above, which is the reason they moved.

## Now

### ~~Precinct partitions~~ — done, read and written, gated both ways

`Scod` bit 0 signals explicit precinct sizes. This library used to ignore the
declaration entirely and read every such codestream against one maximal
precinct, so from the second packet onward it parsed packet headers at the wrong
offsets: 65114 of 65536 samples wrong on the first fixture. `validate.sh` did
not measure it at all, while this file claimed it did every run.

Both directions are now gated. Reading covers precinct sizes from 2^3 to 2^7,
sizes that differ per resolution, all five progression orders, tiles and quality
layers; writing covers the same and OpenJPEG decodes every one of them exactly.
`Options.PrecinctSizes` selects the partition.

Closing it needed three things that each failed separately. The precinct
partition itself, anchored at zero in the resolution's own coordinates with the
band-space origin derived from the resolution origin rather than the band's. The
code-block partition clipped to the precinct (B.7), without which a precinct
smaller than the declared code-block overflows it. And a real coordinate walk
for PCRL and CPRL (B.12.1.4): those two put position outside resolution, so
precinct index *p* names a different region at every resolution and an index
walk decodes each one against the wrong precinct.

`PacketAddress.Precinct` now has a meaning, and `PacketIndex.Range` and
`PacketsForRegion` turn a viewport into byte ranges: a 64x64 region of a 256x256
image resolves to 7 of 85 packets and 2519 of 21127 bytes, measured in the gate
on every run.

### ~~Packet length markers (PLT, TLM)~~ — done, written, parsed and measured

`PLT` lists every packet's length in the tile-part header; `TLM` does the same
for tile-parts in the main header. Both are written when
`Options.WritePacketLengths` is set, and `BuildPacketIndex` uses `PLT` when it
is present instead of walking the packets.

The measurement the marker exists for, from the gate: indexing a 128x128 image
reads 152 of 5486 bytes (2.8%), and a 512x512 image 788 of 85048 (0.9%). The
fraction falls as the image grows, because the cost tracks the packet count
rather than the data — which is what "a small constant near the front of the
file" means in practice. Without the markers the whole codestream must be read,
since a packet's length is only known once its header is parsed.

The index built from the markers is checked against the index built by walking:
same packets, same byte ranges, byte-identical contents. The two share no
arithmetic, so their agreement is evidence rather than a round trip.

Worth recording how the one defect here was caught. Our first `TLM` set `Stlm`
to 0x60, which puts 2 in the ST field and promises a two-byte tile index where a
one-byte one was written. Every decode was still perfect, because a decoder that
ignores `TLM` gets the right pixels; the only sign was a line on stderr from
`opj_dump` — "TLM marker not of expected size". The gate now fails on any
diagnostic from the reference's header parse, not merely on wrong pixels.

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

### ~~Progression orders~~ — done, all five, both directions

The standard defines five (LRCP, RLCP, RPCL, PCRL, CPRL). This library used to
emit and expect one packet walk, which agrees with all five only in the
degenerate case of a single precinct, layer and component — which is exactly
what it always wrote, so nothing ever disagreed.

All five are now gated in both directions, crossed with precincts, components
and layers, because the orders differ only when all three have more than one
value. PCRL and CPRL needed the coordinate walk of B.12.1.4 rather than a
precinct-index walk; the other three are index walks with the precinct
dimension in the right place.

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
