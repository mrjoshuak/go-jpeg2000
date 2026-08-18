# go-jpeg2000 Progress

## Interoperability audit, 2026-08-18

Branch: `feature/codec-validation`

### Summary

This library does not produce or consume interoperable JPEG 2000. It writes a
correct-looking codestream header followed by a **private tile payload**, and it
reads only that private payload. Every codec test in the repository passes
because encoder and decoder share the same private conventions; no test compared
output against another implementation.

Established with OpenJPH (`ojph_compress`/`ojph_expand`) as an external oracle,
after confirming the oracle round-trips its own output bit-exactly.

### Fixed and verified against OpenJPH

- [x] `Rsiz` did not set bit 14 in HTJ2K mode. OpenJPH reported our files as
      "not a JPH file". Now `0x4000`, byte-identical to OpenJPH's SIZ.
- [x] `CapPcapHTJ2K` was `0x00008000`. Pcap bit *i* has value `1<<(32-i)`, so
      Part 15 is `1<<17 = 0x00020000` — the constant was two bit positions off.
- [x] The CAP marker omitted the mandatory `Ccap` field, declaring length 6.
      Part 15 requires one 16-bit `Ccap` per Pcap bit set. Now length 8 with
      `Ccap = 0x0022`, matching OpenJPH.
- [x] The multiple component transform was signalled unconditionally, including
      for 1- and 2-component images, contradicting its own comment. OpenJPH
      rejected the tile. Now gated on `numComponents >= 3`.
- [x] The MEL encoder buffer was allocated with `make([]byte, n)` while both
      writers append, prefixing the MEL stream with `n` zero bytes.
- [x] `Encode`/`Decode` never invoked the HT block coder at all. `HighThroughput`
      set the COD HT flag, wrote CAP and set Rsiz bit 14, then emitted Part 1
      MQ-coded data. Both directions used MQ, so round-trips passed and no
      conforming decoder could read the result. Now routed through
      `encodeCodeBlock`, which selects the coder the markers declare.

### Open — the library is not interoperable until these are done

- [ ] **The HT block coder does not work.** `TestHTCleanupIsExact` (added here)
      shows 99.5–100% of coefficients wrong on a plain round-trip. The
      pre-existing `TestHTEncoderDecoder` passes because it asserts only that
      signs match, excused by a comment claiming the cleanup pass is lossy. It
      is not: the cleanup pass codes every magnitude bit down to the coded
      bitplane, so decode(encode(x)) must equal x.
- [ ] **There is no T2 packet coding on the live path.** `buildTileData` writes
      a 2-byte code-block count, a 5-byte-per-block table, then raw block data.
      Real tile data is packets with tag-tree-coded headers. `decodeTileData`
      checks that count and, on any real file, does `return nil` — yielding a
      blank image with no error. This is why `opj_decompress` parses our
      codestreams and recovers nothing, and why OpenJPH decodes our files to
      flat mid-grey.
- [ ] **Precincts are never constructed.** `InitTile` builds no `Precinct`,
      no `InclusionTree` and no `IMSBTree`, so there is nothing to feed T2.
- [ ] `internal/tcd`'s `PacketEncoder`/`PacketDecoder` and `TileEncoder`/
      `TileDecoder.EncodeCodeBlock` are referenced only by their own tests —
      dead code with respect to the public API.
- [ ] `PacketDecoder.decodeTagTreeValue` is a unary decode labelled "Simplified".
      It is correct only for a 1x1 tree (a single code-block per precinct).

### Consequence for go-openexr

`CompressionHTJ2K256` and `CompressionHTJ2K32` produce EXR files no other tool
can read. The earlier result "all 65536 half bit patterns survive exactly" was a
round-trip measurement on the MQ path and is not evidence of HTJ2K support.

### Method note

Two near-misses worth recording. A first comparison against OpenJPH used its
default lossy settings, so the control failed and the measurement would have
been meaningless; `-reversible true` was required before the oracle was sound.
An 8x8 fixture whose first sample was 0 tripped an OpenJPH edge case and failed
its own round-trip control. In both cases the control, not the measurement,
caught the problem.
