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

## Next

### ~~Multiple quality layers~~ — done, both directions, gated

Real quality layers are a T2 concept: a code-block contributes different numbers
of coding passes to successive layers, each with its own length in its own
packet. That is what this encoder writes — `NumLayers > 1` has been on the
conforming path since Part 1 write landed, and this section's claim that it
"stays on the private path" was stale.

Both directions are gated, and both are stated as progression rather than as
exactness, because exactness alone is a weak claim: a codestream whose first
layer held everything would satisfy it.

Writing: OpenJPEG's reconstruction of our eight-layer codestream improves with
every prefix — mean squared error 2078, 1039, 61, 4.3, 0 at one, two, four, six
and eight layers.

Reading: our reconstruction of OpenJPEG's five-layer codestream improves with
every prefix — 1240, 883, 344, 0, 0 — and every prefix yields the whole image
rather than part of one.

What is deliberately not asserted is that the two agree at intermediate layer
counts. Reconstructing a coefficient whose low bits were never transmitted is
implementation-defined, and the two libraries choose differently; requiring
agreement there would be requiring OpenJPEG's choice, not the standard's.

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

### ~~Component subsampling~~ — done, read and written, gated both ways

`XRsiz`/`YRsiz` above 1 put each component on its own grid, where one sample
covers `XRsiz` by `YRsiz` samples of the reference grid (A.5.1). This library
wrote every component into the output plane at its own index, so a
half-resolution component landed in a quarter of the plane and the rest stayed
zero: 4091 of 4096 chroma samples wrong on a 4:2:0 fixture, while the
full-resolution component beside it was exact. Samples are now written across
the footprint they cover, and 4:2:0 and 4:2:2 are gated.

Worth recording how nearly this was misdiagnosed. Compared against
`opj_decompress`'s output the *correct* component came back 4075 of 4096 wrong,
because the reference writes a subsampled image through its own upsampling and
layout convention — a measurement of the convention, not of the codec. The check
compares against the fixture's own planes instead, and runs a control on the
fixture's declared geometry first.

Writing is done too, through `Options.ComponentSubsampling`. Samples are taken
by decimation rather than averaging, because the format specifies no filter and
decimation is the one choice a decoder can invert exactly. 4:2:0, 4:2:2, 4:1:1,
tiled and lossy are gated, and OpenJPEG places every component exactly in all of
them.

Three things had to become component-aware rather than image-aware: the wavelet
and the quantiser walk each component's own plane; a tile cuts each component's
own rectangle and anchors the transform at that component's origin; and the
multiple component transform is skipped when the first three components do not
share a grid, which the format requires and which had been read past the end of
the shorter plane.

## Later


### HT refinement passes (SPP/MRP) — blocked on an oracle, and measured

The encoder emits the cleanup pass only. That is conformant: a single pass is a
legal HT code-block, and every codestream this package writes is one another
implementation reads. What it gives up is truncating a code-block partway, and
quality layers already provide truncation at packet granularity, in both
directions.

This sits here rather than in Now because one clause of its completion bar
cannot be evaluated with any tool available. `ojph_compress` has no layers
option and writes `numlayers=1` cleanup-only blocks, and OpenJPEG does not
encode HT at all, so nothing can produce a multi-pass HT code-block for this
library to read. "We decode theirs" has no *theirs*.

The write half is testable, through OpenJPH's decoder, and was tested. An
implementation of SPP/MRP encoding lived in `internal/entropy` and was never
reachable from the live encoder. It round-tripped through this package's own
decoder, which is what kept it looking correct; wiring it up and handing the
result to OpenJPH produced `ojph error 0x000300A1 ... Error decoding a
codeblock`. Both halves shared one deviation from the standard — the same shape
of defect as the wavelet pass order and the 20-byte deep chunk header — so the
encoder was deleted rather than left in place looking usable.

Closing this means writing SPP and MRP from ISO/IEC 15444-15 rather than
adapting what was there, and it can be held to: OpenJPH's decoder must read the
result exactly, and a decoder given a truncated prefix must produce a complete
image at reduced quality. The read direction stays unverifiable until an encoder
exists that emits multi-pass HT blocks.

### ~~Region decode~~ — done, and reduced resolution followed in v1.5.6

The second half of this heading used to say reduced resolution for NLT
codestreams remained. It does not: the refusal turned out to rest on comparing
a reduced decode against a downsample — the wrong oracle — and against
`ojph_expand -skip_res` and `opj_decompress -r` this library was already
bit-exact on both the float and half paths. See v1.5.6.

`Config.DecodeArea` is implemented. It was declared, documented as "specifies a
region to decode", and read by nothing: a caller asking for a 32x16 region
received the whole 64x32 image and no indication.

Both halves hold. The samples are exactly the ones a full decode produces for
that rectangle — not an approximation, because every coefficient that can reach
the region is decoded and synthesised as usual. And it costs less: a 64x64
region of a 256x256 image with 32x32 precincts entropy-decodes 5468 of 24913
code-block bytes, 22%, skipping the rest. `DecodeConfigCost` reports both
figures so the saving is a measurement rather than a claim, and the gate prints
it every run. The returned image is allocated for the region, so a band of a
large image costs a band-sized buffer.

The geometry is where this went wrong first and is worth recording. A code-block
is skipped when the region cannot reach it, which needs the block's band
rectangle mapped into output coordinates. Detail bands sit at half their
resolution's own grid, so they scale by one factor more than the LL band does;
treating them alike skipped blocks that did reach the region — 254 of 256
samples wrong in a corner while the middle of the image was fine. Fixing it also
tightened the skip from 54% of the data to 22%, because the mis-scaled blocks
had been landing spuriously near the origin.

What remains is `ReduceResolution` for codestreams carrying an NLT point
transform, which still refuses: a partial synthesis leaves values in the
sign-magnitude domain NLT maps back from rather than samples, and reinterpreting
them gave results off by 175 on a ramp spanning 0 to 2. Ordinary integer samples
are correct to within a count or two and are gated.

### ~~Error resilience markers~~ — all three conditions met, and the item understated the defect

The item said SOP and EPH were "never written". They were worse than absent:
`Options.EnableSOP` and `EnableEPH` set their bits in the COD's Scod field and
emitted no markers at all, so the codestream declared a structure it did not
have. **OpenJPEG refused an `EnableEPH` file outright** — it expects EPH after
each packet header when Scod says so — while this library round-tripped the
same file perfectly, because our decoder skips the marker only when present and
never minded that it never was. A shipped option produced files no other
implementation could read, and nothing inside the repository could tell.

All three completion conditions hold in v1.5.9. We write both, in the places
the standard puts them — SOP before the packet with its wrapping 16-bit
counter, EPH between the packet header and the code-block bodies, including
after an empty packet's single-bit header. All four combinations decode through
`opj_decompress` to exactly the fixture, and the reference's own SOP/EPH stream
decodes here sample for sample.

Resynchronisation needed a different design from the obvious one, and finding
that out was the useful part. Recovering on a parse *error* recovers nothing: a
packet header is a bit stream with no self-delimiting structure, so damaged bits
produce a different-but-readable header rather than a failure. Measured — two
flipped bytes, four 0xFFs, sixteen 0xFFs and sixteen zeros over a packet header
all produced no error, no recovery, and wrong pixels. SOP is instead a
*positive* check: a stream that has been writing the marker before every packet
and then does not is demonstrably out of position, and that is testable before
parsing anything. All four patterns now recover. `DecodeCost.Resyncs` reports
the count, because "it still decoded" is not evidence of resynchronisation —
the first version of the test asserted exactly that and passed while recovering
nothing, which is why the counter exists.

### Region of interest — the marker is parsed; the reconstruction is blocked on an oracle, and measured

RGN is read rather than skipped as of v1.5.10, and an ROI style the standard
does not define is refused by name instead of being assumed to be the one it
does. An out-of-range component is refused too. Until then the marker appeared
only in a table of names and the parser skipped the segment.

**The max-shift reconstruction is not implemented, and the reason is a
measurement rather than a preference.** No encoder available here writes a
stream where the shift is applied. `opj_compress -ROI c=0,U=8` emits the marker
with SPrgn=8 and shifts nothing: its ROI and non-ROI streams decode to identical
samples — 0 of 1536 differ — and the two files differ by exactly seven bytes,
which is the RGN segment itself. Kakadu and Grok, which do implement real ROI
encoding, are not installed; OpenJPH does not support ROI at all.

So the reconstruction could be written but not verified, and writing an
unverifiable scaling rule into a decoder is the failure this whole exercise
exists to prevent. That is not hypothetical here: an attempt was made and
reverted. A threshold downshift of the shape OpenJPEG's decoder uses left the
lossy case unchanged and **broke a case that worked** — a lossless stream that
had decoded exactly went to 1528 of 1536 samples wrong, because our
coefficients are already dequantised at that point and the threshold belongs in
the raw-magnitude domain.

Worth recording plainly: an earlier reading of this measurement was wrong. A
lossy ROI stream differing from the reference on 1439 of 1536 samples was
attributed to the missing reconstruction; the control — the same lossy stream
with no ROI at all — differs by exactly the same 1439. That divergence is about
lossy rate-truncated decoding on noise, not about RGN, and the ROI had nothing
to do with it.

**What would lift the block:** an encoder that actually applies the max-shift,
so the reconstruction has something to be right against. The gate re-measures
the current limitation on every run, so the day `opj_compress -ROI` starts
changing samples — or another encoder is installed — the check fails and says
the oracle now exists.

### ~~JP2 container conformance~~ — the reference reads our JP2 files, in v1.5.8

Four wrappers — greyscale 8 and 16 bit, sRGB, and a one-resolution RGB case
whose codestream is trivial so anything the reference objects to is the
container — decode through `opj_decompress` to exactly the fixture, sample for
sample. The comparison recomputes the expected samples from the ramp rather
than reading a file the generator wrote, so a generator that built its fixture
and its image from one wrong expression cannot agree with itself.

The completion bar was "a reference tool reads our JP2 files, not just our J2K
codestreams", and that is what the gate now does on every run.

Two notes on what this did and did not find. The boxes were already correct —
nothing here was broken — which is worth stating plainly rather than implying a
save. And the paragraph's guess at the likely gaps was off: palette, channel
definition and component mapping boxes are written for palettised images, which
this library does not produce, so they were never candidates.

What the exercise did add is the check that can see the class. Mutation 63
swaps the colr box's enumerated colourspace — sRGB for greyscale and back —
and **the pre-existing JP2 round-trip test survives it**, because our decoder
takes the component count from the codestream's SIZ marker and never consults
the box. A wrong enumeration round-trips here perfectly while telling every
other implementation that a three-component image is greyscale. That is the
container-shaped version of the defect this whole goal has been about, and
until now nothing in the repository could have detected it.

## Standing work

### ~~Widen the matrix before widening the claim~~ — widened; four axes clean, one found something

`scripts/matrixgen` covers pixel types, component counts, resolution levels,
tilings and both transforms. The day it was widened past 8-bit greyscale it
found three whole capabilities broken — lossy, tiling and float — each of which
had been shipping non-conformant output while the suite was green. Every new
axis is likely to find something.

All five of those axes have now been addressed, in v1.5.8.

**Non-square dimensions**, both orientations (40x24 and 24x40), on the integer
and float paths. This one mattered most on paper: every index in a wavelet, a
code-block grid and a packet header is a function of width and height, and on a
square image a transposed one is indistinguishable from a correct one — the
matrix had been square in every case. The reference decodes all four exactly.

**Non-zero image offset.** Our encoder does not write `XOsiz`/`YOsiz`, so a
round trip cannot reach this at all; the gate has `opj_compress -d 7,5` write
one and compares our decode against the reference's own. Exact. A decoder that
ignored the offset would return a plausible image of the right size holding the
wrong samples.

**Tile origin offset from the image origin.** `Options.TileOffset`, which makes
the first tile partial in a way a zero origin never does. Exact.

**Signed components and bit depths other than 8/16/32 are not expressible**, and
finding that out was the axis that paid. The integer API is Go's `image.Image`,
which reaches 8- and 16-bit unsigned and no further. `FloatImage` carries
`BitDepth` and `Signed` — and `EncodeFloat` ignores both, writing `Ssiz` as
32-bit signed whatever they hold, because binary32 bit patterns reinterpreted as
samples can only be described that way. So `BitDepth: 12` produced a 32-bit
codestream with no indication: the same shape as `Config.DecodeArea` before
v1.5.2, a field declared, documented, and read by nothing.

The fix is documentation and a test, not a refusal. Refusing was written first
and reverted: three existing tests pass `BitDepth: 16` and `Signed: false` and
their files are correct, so refusing would have broken working callers to make a
point. The fields are now documented as decoder output, and
`TestFloatImageBitDepthIsOutputOnly` asserts every value produces `Ssiz` 0x9F,
so the asymmetry fails loudly if it ever changes.

Four axes clean, one finding — and the four clean ones stay in the matrix.
An axis that passes is evidence; an axis removed after passing is evidence
thrown away.

### ~~Retire the remaining self-referential tests~~ — measured instead, which is better

"Cannot fail in the way that matters" was an opinion until this repository had
an instrument for it. It has one now: `scripts/mutation/run.py`, ported from the
sibling go-openexr repository, with a manifest of five deliberate defects — the
NLT mask narrowed symmetrically, SOD required at a fixed offset (the v1.5.5
defect), a reduced decode that decodes everything, a region decode that crops
instead of skipping, and DecodeArea read and ignored (the pre-v1.5.2 defect).

The measurement: **every pre-existing round-trip test survives every one of the
five.** That is the self-referentiality this item alleged, demonstrated per
defect rather than asserted in general. Each mutation is killed only by an
anchored test — cost-anchored for the two savings, wire-anchored for PLT, and
for the NLT mask a new `TestNLTType3MatchesTheDefinition` whose vectors are
literals, because `nltType3` is one involution shared by both codec directions
and a wrong mask still undoes itself perfectly. Nothing was retired: a
surviving round-trip test still guards what it does guard, and the harness now
says exactly what that is and is not. CI runs it on both architectures beside
the gate.

The manifest is small and meant to grow the way go-openexr's did — a mutation
per defect found, so the count only rises with evidence.

### ~~Performance~~ — measured against both, in v1.5.11

The bar was to measure once the feature set was complete. Milliseconds, best of
three, 2048x2048 uniform noise, this machine. The reference figures are
command-line wall clock and carry about 5.5 ms of process start and file I/O
that an in-process Go call does not pay; that is stated rather than subtracted,
because subtracting an estimate would be inventing a number.

| implementation      | encode | decode |
| :------------------ | -----: | -----: |
| this library, HT    |   48.4 |  106.4 |
| OpenJPH, HT         |   85.0 |   72.0 |
| this library, Part 1|   84.6 | 1029.2 |
| OpenJPEG, Part 1    |  521.0 |  502.0 |

Compressed sizes are equivalent where the algorithms are — ours 263059 bytes
against OpenJPH's 263082 on the 512x512 fixture — so this is the same work,
not a faster encoder producing a worse file.

**Against OpenJPH, the only fair HT comparison: encode is about 1.6x faster,
decode about 1.6x slower.** Against OpenJPEG the encode is roughly six times
faster, but Part 1 and HT are different algorithms and that number should not
be quoted as a like-for-like win.

**The outlier is our Part 1 decode: 1029 ms, twice OpenJPEG's and ten times our
own HT decode of the same image.** The MQ arithmetic decoder is where that
lives. It is the one figure here that looks like a defect rather than a
trade-off, and it is recorded for a goal about speed rather than fixed under one
about measurement.

One methodological note worth keeping. At 512x512 this comparison made the
library look eleven times faster than OpenJPH on encode — because the reference
timings were dominated by process start at that size. The ratio reversed at
2048x2048. A benchmark small enough to be quick is a benchmark measuring the
harness.
