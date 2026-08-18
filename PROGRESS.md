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

### Progress since (same day)

- [x] HT block **decoder** now decodes an OpenJPH-written code-block exactly
      (`TestHTDecodeOpenJPHBlock`, 64/64). Five ported defects: Scup packing,
      the VLC length mask, quad geometry, sample placement, and the complete
      absence of MEL run decoding.
- [x] HT block **encoder** ported from OpenJPH with the standard VLC tables
      generated from its `table0.h`/`table1.h`. Round-trips exactly at every
      block size (`TestHTCleanupIsExact`).
- [x] Standard **T2 packet decoding** with real multi-level tag trees. A
      single-resolution OpenJPH HTJ2K codestream now decodes exactly, 0/64
      samples different — the first conforming decode this library has managed.
- [x] Standard **T2 packet encoding** written. OpenJPH parses the resulting
      markers and packet headers and reaches block decoding.

### Interoperability achieved (verified against OpenJPH and OpenJPEG)

- [x] **OpenJPH decodes our HTJ2K output exactly** at 32, 64, 128 and 200 px
      square, at 1, 2, 3 and 4 resolutions — 0 samples different. Conforming T2
      packets are emitted for the HighThroughput path.
- [x] **We decode OpenJPH's HTJ2K output exactly**, at zero through three
      decomposition levels.
- [x] **We decode OpenJPEG's Part 1 MQ output exactly**, at one, two and three
      resolutions. The MQ block decoder had four independent defects, the first
      being that only the uniform context was seeded (Table D.7 requires
      UNIFORM=46, RUN-LENGTH=3, ZC context 0=4), so the arithmetic decoder
      desynchronised on the first decision of nearly every code-block.
- [x] The DWT multi-level drivers passed the shrunken level width as the row
      stride, so from the second decomposition level they walked the wrong
      memory. A round trip could not see it because both directions made the
      same mistake.
- [x] **The 2D DWT ran its two passes in the wrong order.** The forward filtered
      rows then columns and the inverse undid columns then rows. ISO/IEC 15444-1
      F.3.8.1 defines the inverse 2D_SR as HOR_SR then VER_SR, so the forward
      2D_SD must be VER_SD then HOR_SD. The 5/3 lifting steps floor their
      intermediates, so the orders are not interchangeable — but they are exact
      inverses of each other, which is why every round-trip test passed while no
      conforming decoder could read the output. OpenJPH now decodes our output
      exactly at 1, 2, 3 and 4 resolutions at 32, 64, 128 and 200 px, we decode
      OpenJPH's streams exactly at 0-3 decompositions, and we decode OpenJPEG's
      Part 1 MQ streams exactly at 1-3 resolutions.
- [x] `internal/dwt/conformance_test.go` is the test that could catch it:
      `TestForward53MatchesSpec` compares against a literal transcription of
      Annex F written from the equations, and `TestForward53MatchesOpenJPHCoefs`
      compares against the coefficient arrays carried in codestreams OpenJPH
      produced (`internal/dwt/testdata/ojph_*.coef`). Both fail on the old code.
- [x] **The NLT marker segment was written one byte short.** `generateNLT`
      emitted Lnlt = 5 with a one-byte Cnlt. ISO/IEC 15444-2 A.3.10 (and
      15444-15 Annex A) fix the segment at Lnlt(2) + Cnlt(2) + BDnlt(1) +
      Tnlt(1), so Lnlt is 6 and Cnlt is sixteen bits wide. OpenJPH rejects
      anything else outright — `param_nlt::read` requires `length == 6` and
      reports "Unsupported NLT type" (ojph_params.cpp:2256). This repository's
      own parser already required 6, so `EncodeFloat`/`EncodeHalf` output could
      not be read back by anything, including this decoder. Measured: with the
      fix, OpenEXR/OpenJPH reads go-openexr's HALF, FLOAT and UINT HTJ2K
      scanline chunks bit-identically to their uncompressed twins; without it,
      every one fails with "Unsupported NLT type 255".
- [x] The HT encoder applied initial-stripe u-coding rules to every stripe,
      emitting a MEL event and the "u > 2" UVLC modes where a conforming
      decoder expects neither. `TestHTEncodeMatchesOpenJPH` is now byte-exact.
- [x] Guard bits were 0, making Mb one or two short of the U_q the detail bands
      produce, so conforming decoders rejected the code-blocks outright.
- [x] The tag-tree encoder lacked a `known` flag and set leaf values during
      coding rather than before it, corrupting every packet with more than one
      code-block per band.
- [x] `scripts/validate.sh` gates all of the above, including `go test -race`.

### Open — remaining gaps

- [x] The HT block coder was 99.5-100% wrong in both directions; it is now
      correct, verified against OpenJPH rather than by round-trip. The
      pre-existing `TestHTEncoderDecoder` had passed throughout because it
      asserts only that signs match, excused by a comment claiming the cleanup
      pass is lossy. It is not: the cleanup pass codes every magnitude bit down
      to the coded bitplane, so decode(encode(x)) must equal x. That test
      remains in the false-assurance backlog.
- [x] T2 packet decoding now exists (`t2_packets.go`) and is wired into
      `decodeTileData`. The encoder side is written but not yet enabled.
      Previously:
- [ ] **There was no T2 packet coding on the live path.** `buildTileData` writes
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
      (`t2_packets.go` has a correct tag tree; `internal/tcd/t2.go` remains dead.)
- [ ] **Subbands with an odd width are not conformant.** Not the wavelet: the
      forward transform now matches Annex F and the OpenJPH coefficient
      fixtures at 17x17, 23x17, 33x33 and 45x31 up to five levels. But a
      17 px or 25 px image at two resolutions, whose LL band comes out 9 or 13
      wide, differs on 76/289 and 135/625 samples through OpenJPH, and reading
      OpenJPH's own 45x31 stream at one decomposition is 719/1395 wrong with a
      max delta of 234 — a gross mis-assignment, not a rounding error. Present
      before the DWT pass-order fix and unchanged by it. Recorded as the two
      remaining known gaps in `scripts/validate.sh`.
- [ ] **Conforming T2 output is enabled only for HighThroughput.** The Part 1
      MQ path still writes the private container, because its packet headers
      need a length per coding pass and only a single length is emitted.
- [ ] SUPERSEDED: multi-resolution decoding was broken. Isolated with a 2x2 matrix:
      HT at one resolution is exact, MQ at one resolution is 63/64 wrong, HT at
      two resolutions is 63/64 wrong. So T2 and the HT block coder are correct,
      and there are two further defects — in the MQ block decoder and in the
      wavelet path — that are unrelated to the HT work.
- [ ] **Our own decoder cannot read our (now conforming) multi-resolution
      output.** Enabling the T2 encoder turned five previously passing tests
      red, all of them multi-resolution round-trips. The files themselves are
      correct — OpenJPH reads them exactly — so this is a decoder defect that
      writing a private format had been hiding.

      Localised: for an 8x8 image at two resolutions the LL band is recovered
      exactly and the HL band comes back holding a copy of LL's values, so
      packet bodies are mis-assigned across bands at res > 0. The wavelet is
      not at fault: `TestInverse53IsInverseOfForward` passes, and the forward
      transform is confirmed correct from outside by OpenJPH.
- [x] **OpenJPH decodes our HTJ2K output to the exact source samples**, at one
      and at two resolution levels (0/64 samples different in both). The T2
      packet encoder is wired into both the sequential and parallel paths. The
      remaining piece was the signalled bit-plane count: a conforming decoder
      reconstructs (v_n + 2) << (numbps - 1), and since `encodeCleanupHT` emits
      v_n = 2*mu - 2 + s with no shift of its own, numbps must be 2. At 1 the
      decoded samples come back exactly halved, which is how it was found.
- [ ] SUPERSEDED, kept for the record: **the `p = 0` convention is self-inverse.** `encodeCleanupHT` encodes with
      no bitplane shift and the decoder inverts with `(v_n + 2) >> 1`, ignoring
      p entirely. The two agree, which is why the round-trip is exact, but the
      reference positions magnitudes by `p = numbps` and signals that count in
      the packet header. OpenJPH reads our header, then fails in
      `ojph_codeblock.cpp:221` decoding the block. `TestHTEncodeMatchesOpenJPH`
      is at 69/70 bytes, the one difference being the MEL terminate byte.
      Note the U_q values our encoder computes match the decoder's ground truth
      exactly, so the significance and magnitude coding is right; it is the
      bitplane positioning that is not.

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
