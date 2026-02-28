# go-jpeg2000 Progressive Streaming Requirements

Companion document to [DESIGN-progressive-image-streaming.md](DESIGN-progressive-image-streaming.md). These are the changes needed in `github.com/mrjoshuak/go-jpeg2000` to support progressive image streaming for VFX content.

## Context

The progressive image streaming design requires go-jpeg2000 to serve as the codec layer: encoding EXR float data into HTJ2K codestreams with multiple quality layers, and progressively decoding those codestreams as packets arrive. The current implementation handles full-codestream encode/decode but lacks the incremental and float-aware capabilities needed for streaming.

## Current State

### What Works
- Full JPEG 2000 Part 1 encode/decode
- HTJ2K encode/decode (cleanup pass only)
- All 5 progression orders (LRCP, RLCP, RPCL, PCRL, CPRL)
- Quality layer support in the encoder (`Options.NumLayers`)
- Resolution reduction in config (`Config.ReduceResolution`)
- Region-of-interest decode (`Config.DecodeArea`)
- SIMD-optimized DWT (AVX on amd64, NEON on ARM64)
- CDF 9/7 (lossy) and Le Gall 5/3 (lossless) wavelet transforms

### What's Missing or Incomplete

| Gap | Location | Impact |
|---|---|---|
| Full codestream in memory | `decoder.go:257` — `io.ReadAll` | Blocks streaming; must hold entire file before any decode |
| No progressive/incremental decode API | — | Cannot decode as packets arrive |
| No float output path | Decoder outputs `image.Image` (integer) | Loses HDR dynamic range; DWT internally uses float64 but converts to integer |
| HTJ2K SPP/MRP refinement passes stubbed | `entropy/ht.go:867` | Only cleanup pass decoded; limits progressive quality ceiling |
| HTJ2K `len2` hardcoded to 0 | `entropy/ht.go:114` | Refinement pass data never parsed from code-block segments |
| `Config.QualityLayers` not wired up | Decoder ignores it | Cannot limit decode to N quality layers for progressive preview |
| `Config.ReduceResolution` incomplete | `decoder.go:289` | Only adjusts output dimensions; DWT still reconstructs all levels; T2 still reads all resolution packets |
| `PacketDecoder` requires full data | `tcd/t2.go:440` — `NewPacketDecoder(data []byte)` | Cannot consume packets incrementally |
| T2→T1 decode pipeline incomplete | `decoder.go:363-414` | Tile decode has a gap between codestream parsing and entropy decoding |

## Requirements

### R1: Progressive Decode API

A new decoder that accepts wavelet packets incrementally and produces a continuously improving image.

```go
// ProgressiveDecoder decodes a JPEG 2000 codestream incrementally.
// After each Feed call, Reconstruct returns the best image available
// from the data received so far.
type ProgressiveDecoder struct { ... }

// NewProgressiveDecoder creates a decoder. The header (SIZ, COD, QCD
// markers) must be provided upfront — it's small and arrives first
// in any JPEG 2000 codestream.
func NewProgressiveDecoder(header *codestream.Header, opts ...DecoderOption) (*ProgressiveDecoder, error)

// FeedPacket provides a single quality-layer packet to the decoder.
// Packets can arrive in any order. The decoder tracks which packets
// have been received.
func (d *ProgressiveDecoder) FeedPacket(p Packet) error

// Reconstruct produces the best image possible from packets received
// so far. Returns a float image (see R2). Safe to call after any
// number of FeedPacket calls — every intermediate state is valid.
// The resolution of the output depends on the highest complete
// resolution level received.
func (d *ProgressiveDecoder) Reconstruct() (*FloatImage, error)

// ReceivedPackets returns the set of packet addresses that have
// been received. Used for cache model tracking.
func (d *ProgressiveDecoder) ReceivedPackets() []PacketAddress

// Complete returns true when all packets have been received and the
// full-quality image can be reconstructed.
func (d *ProgressiveDecoder) Complete() bool
```

**Packet type:**

```go
// PacketAddress uniquely identifies a packet within a codestream.
type PacketAddress struct {
    Tile       uint16
    Resolution uint8
    Layer      uint16
    Component  uint8
    Precinct   uint16
}

// Packet is a wavelet packet: the atomic unit of progressive data.
type Packet struct {
    Address PacketAddress
    Data    []byte
}
```

**Implementation approach:**

The progressive decoder maintains partial tile state. When `FeedPacket` is called:
1. Parse the packet header (inclusion flags, code-block contribution lengths)
2. Store code-block data segments indexed by (resolution, band, code-block position)
3. Mark which quality layers of which code-blocks are available

When `Reconstruct` is called:
1. For each tile component, decode available code-blocks (T1/HT entropy decode)
2. Zero any code-blocks with no data (they contribute no detail)
3. Apply inverse DWT for the highest complete resolution level
4. Apply inverse MCT and DC level shift
5. Return float image

The existing `TileDecoder`, entropy decoders, and DWT are already compatible with this approach — they operate on individual tiles and code-blocks. The change is in how data is fed to them (packet-at-a-time instead of full-codestream).

### R2: Float Image Output

A new image type that preserves float precision through the decode pipeline.

```go
// FloatImage holds multi-component float pixel data.
// Components are stored separately (planar) to match the wavelet
// decomposition's component structure.
type FloatImage struct {
    Width, Height int
    Components    [][]float32  // one slice per component (R, G, B, ...)
    BitDepth      int          // original bit depth (16 for half, 32 for float)
    Signed        bool
}
```

**Where the change happens:**

The DWT reconstruction (`dwt.ReconstructMultiLevel97` and `dwt.ReconstructMultiLevel53`) already operates on `float64` internally. Currently the decode path converts to `int32` coefficients in `TileComponent.Data`. The float path would keep coefficients in `float64` (or `float32` for memory) through to the output.

The change is in `decoder.go`'s `decodeTile` → `buildImage` path: instead of mapping coefficients to Go `image.Image` pixel types (which clamp to integer ranges), map them to `FloatImage` components.

**Interaction with go-openexr:**

go-openexr already has `half.Half` (16-bit float) and `float32` pixel types. The `FloatImage` should be convertible to/from go-openexr's `FrameBuffer` without precision loss.

### R3: HTJ2K Refinement Passes (SPP/MRP)

Complete the implementation of `decodeSPPMRP()` in `entropy/ht.go`.

**What exists:**
- `initSPP()` and `initMRP()` initialize the forward/reverse bitstream readers
- The SPP and MRP data segments are defined in the code-block structure
- `len2` is hardcoded to 0, preventing refinement data from being parsed

**What's needed:**
1. Parse actual `len2` from the code-block segment headers (remove the `len2 := 0` override)
2. Implement `decodeSPPMRP()` to refine coefficient magnitudes using the SPP and MRP bitstreams
3. The SPP pass propagates significance from neighbors (similar to EBCOT's significance propagation)
4. The MRP pass refines magnitude values for already-significant coefficients

**Why it matters for progressive streaming:**

The cleanup pass provides the base quality layer. SPP/MRP add refinement detail that takes subsequent quality layers from "good" to "pixel-perfect." Without refinement passes, the quality ceiling for HTJ2K progressive decode is limited to cleanup-pass fidelity — roughly equivalent to a single quality layer. With refinement passes, each additional quality layer adds meaningful visual improvement.

### R4: Quality Layer Limiting

Wire up `Config.QualityLayers` so the decoder actually respects it.

**Current state:** The `Config.QualityLayers` field exists but is ignored in the decode path. The `PacketIterator`'s `layEnd` is always set from the header's `NumLayers`.

**Required behavior:**
- When `Config.QualityLayers > 0`, cap `PacketIterator.layEnd` to `min(header.NumLayers, Config.QualityLayers)`
- T2 packet iteration stops after the specified number of layers
- Code-blocks receive contributions only from the decoded layers
- This enables quick low-quality previews without processing all data

**Where it changes:**
- `tcd/t2.go`: `PacketIterator` initialization — use `Config.QualityLayers` to set `layEnd`
- Possibly `tcd/tcd.go`: `TileDecoder` initialization — pass quality layer limit through

### R5: Resolution Reduction (Complete)

Complete the `Config.ReduceResolution` implementation so it actually reduces computation.

**Current state:** Only adjusts output image dimensions. DWT still reconstructs all levels. T2 still reads all resolution-level packets.

**Required behavior:**
- When `Config.ReduceResolution = N`:
  1. T2 packet iteration skips packets for the finest N resolution levels
  2. DWT reconstruction only runs `NumDecompositions - N` inverse levels
  3. The output is the LL subband at the reduced level
  4. Tile component initialization skips the finest N resolution levels

**Where it changes:**
- `tcd/t2.go`: Skip packets where resolution > `numResolutions - reduceResolution`
- `dwt/dwt.go`: `ReconstructMultiLevel53/97` accept a `levels` parameter instead of always using `NumDecompositions`
- `tcd/tcd.go`: `InitTile` creates only `numResolutions - reduceResolution` resolution levels
- `decoder.go`: Remove the current dimension-only adjustment; let the DWT output size be the natural reduced size

### R6: Packet Extraction from Codestream

A new API to extract individual packets from an encoded codestream, for use by the streaming server.

```go
// ExtractPackets parses a JPEG 2000 codestream and returns its
// constituent packets as individually addressable units.
// The codestream must be fully available (this is used on the
// server side where the full data exists).
func ExtractPackets(codestream []byte) ([]Packet, error)

// PacketIndex is a lightweight index that maps packet addresses to
// byte ranges within a codestream. This avoids copying packet data
// when the codestream is memory-mapped.
type PacketIndex struct { ... }

func BuildPacketIndex(codestream []byte) (*PacketIndex, error)
func (idx *PacketIndex) GetPacket(addr PacketAddress) ([]byte, error)
func (idx *PacketIndex) AllAddresses() []PacketAddress
```

**Why:** The progressive streaming server needs to serve individual packets in arbitrary order (priority-ordered, not codestream order). This requires parsing the codestream into addressable packets once, then serving them on demand.

**Implementation:** Walk the T2 packet structure, recording byte offsets and lengths for each packet. The `PacketIterator` already knows how to iterate in all progression orders — this extends it to record byte positions.

### R7: Multi-Quality-Layer Encoding

Ensure the encoder produces codestreams with meaningful multiple quality layers.

**Current state:** `Options.NumLayers` is supported, but the rate-distortion allocation across layers needs verification for VFX content.

**Required behavior:**
- With `NumLayers = 16`, each successive layer should add visible quality improvement
- Rate allocation should distribute quality roughly logarithmically (large improvement in early layers, refinement in later layers)
- The LRCP progression order must produce a valid progressive bitstream: any prefix of the codestream should decode to a valid lower-quality image

**Testing:** Encode a 4K EXR frame with 16 quality layers. Decode at each layer count (1-16). Verify:
- Layer 1 produces a recognizable image
- Layer 4 produces a comfortable preview (roughly 25% of data)
- Layer 8 produces high quality (roughly 50% of data)
- Layer 16 is visually identical to the source (lossless with 5/3 wavelet)

## Priority Order

These requirements build on each other:

1. **R3** (SPP/MRP refinement) — unblocks multi-layer quality progression for HTJ2K
2. **R4** (quality layer limiting) — enables partial decode for progressive preview
3. **R5** (resolution reduction) — enables fast low-res preview
4. **R2** (float output) — preserves HDR through the pipeline
5. **R7** (multi-layer encoding) — produces progressive codestreams
6. **R6** (packet extraction) — enables server-side packet serving
7. **R1** (progressive decode API) — the capstone; depends on R2-R6

R3 and R4 are small, bounded changes. R5 is moderate. R2 is a new output path but doesn't change the core codec. R6 is new API surface over existing parsing. R7 is mostly verification. R1 is the largest change — a new decoder mode — but it's architecturally clean once R2-R6 are in place.

## Non-Requirements

- **Streaming encode**: The encoder can continue to produce full codestreams in memory. The server encodes once and serves packets from the cached codestream. There's no need for streaming encode.

- **JP2 container changes**: The progressive streaming uses raw J2K codestreams (no JP2 box structure). Container handling doesn't need changes.

- **New wavelet transforms**: CDF 9/7 and Le Gall 5/3 are sufficient. No need for additional wavelet families.

- **GPU acceleration**: Out of scope. The SIMD-optimized DWT is sufficient for the encode side (server, done once). The decode side (client, progressive) will be fast because it's decoding partial data — a 4-layer decode is roughly 4x cheaper than a 16-layer decode.
