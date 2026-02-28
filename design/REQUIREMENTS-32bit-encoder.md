# go-jpeg2000 32-Bit Encoder Requirements

Full feature parity with the C++ OpenEXR HTJ2K implementation. The C++ reference (OpenEXR 3.4 + OpenJPH) supports 32-bit float and uint channels via direct bitwise reinterpretation plus the JPEG 2000 NLT (Non-Linearity) marker. go-jpeg2000 must match this capability.

## Context

The C++ OpenEXR HTJ2K compressor handles FLOAT (32-bit) channels by:

1. **Bitwise reinterpretation** — IEEE 754 float bits are cast directly to `int32`. No numeric conversion, no float-to-integer mapping. The raw 4-byte pattern is treated as a signed 32-bit integer sample.
2. **NLT type 3 marker** — A codestream marker (Non-Linearity Point Transformation, Binary Complement to Sign-Magnitude) tells the codec that these integer samples are actually sign-magnitude values (as IEEE 754 is), not two's complement. The wavelet engine applies the appropriate transform internally.
3. **SIZ marker** — Components are declared as 32-bit signed in the SIZ marker header (`Ssiz = 0x80 | 31`).

The decoder side of go-jpeg2000 already handles arbitrary bit depths — `FloatImage` output works, `ComponentInfo.Precision()` reads up to 38 bits. The gap is entirely in the encoder.

## Current State

### What Works
- Decoder handles 32-bit component bit depths (SIZ parsing, coefficient storage as `int32`)
- `DecodeFloat()` returns `*FloatImage` with `float32` components
- `ProgressiveDecoder` produces `*FloatImage` output
- Encoder pipeline internally uses `[][]int32` — wide enough for 32-bit samples
- DWT operates on `float64` (lossy) or `int32` (lossless) — both handle 32-bit values

### What's Missing

| Gap | Location | Impact |
|---|---|---|
| Encoder only accepts `image.Image` | `jpeg2000.go:330` — `Encode(w, m image.Image, o)` | No way to provide 32-bit samples; Go's image types cap at 16 bits |
| `extractImageData()` max 16-bit | `encoder.go:79-195` | Type switch only handles Gray/Gray16/RGBA/RGBA64/NRGBA/NRGBA64 |
| Precision override capped at 16 | `encoder.go:198` — `Precision <= 16` | Even if 32-bit data reached the pipeline, precision would be clamped |
| No NLT marker support | Nowhere in codebase | Cannot signal sign-magnitude vs two's complement for float bit patterns |
| SIZ encoder may not handle 32-bit | `encoder.go` SIZ generation | Needs verification that `Ssiz` byte is written correctly for 32-bit signed components |
| No `EncodeFloat()` API | — | No public function that accepts `*FloatImage` |

## Requirements

### R8: FloatImage Encode API

A new public function that accepts `*FloatImage` directly, bypassing `image.Image`.

```go
// EncodeFloat encodes a FloatImage to a JPEG 2000 codestream.
// Each component's float32 values are bitwise-reinterpreted as int32
// samples (matching the C++ OpenEXR approach). The NLT type 3 marker
// is automatically included for float components.
//
// For integer data stored in FloatImage (BitDepth <= 16, Signed false),
// values are rounded to integers and encoded without NLT.
func EncodeFloat(w io.Writer, img *FloatImage, o *Options) error
```

**Implementation:**

1. Add a new `floatEncoder` (or extend `encoder`) that populates `componentData [][]int32` from `FloatImage.Components` via `math.Float32bits()` reinterpretation.
2. Set `precision = 32`, `signed = true`, `numComponents = img.ComponentCount()`.
3. Feed into the existing `preprocess()` → `generateCodestream()` pipeline.
4. The DC level shift for 32-bit signed is 0 (data is already centered around zero for float bit patterns).

**Key detail — bitwise reinterpretation, not numeric conversion:**

```go
// This is what we do (correct):
bits := math.Float32bits(floatVal)  // e.g., 1.0 → 0x3F800000
sample := int32(bits)               // treat those bits as signed int32

// This is what we do NOT do (wrong):
sample := int32(floatVal)           // numeric conversion, loses mantissa
```

### R9: NLT Marker (Non-Linearity Point Transformation)

Implement the NLT marker as defined in JPEG 2000 Part 2 (ISO/IEC 15444-2) and referenced by Part 15 (HTJ2K).

**Marker structure:**

```
NLT marker: 0xFF5C
  Lnlt    (2 bytes) — marker segment length
  Cnlt    (1 byte)  — component index (0-based)
  BDnlt   (1 byte)  — bit depth of component (Ssiz format: high bit = signed)
  Tnlt    (1 byte)  — transform type:
                       1 = gamma-style (not needed)
                       2 = table lookup (not needed)
                       3 = binary complement to sign-magnitude
```

**Type 3 behavior:**

The NLT type 3 transform converts between two's complement and sign-magnitude representation. For encoding, the wavelet engine must apply this before the forward DWT. For decoding, it's applied after the inverse DWT.

The transform is:
```
// Two's complement → sign-magnitude (encode, pre-DWT):
if value < 0:
    sign_magnitude = (1 << (bitdepth-1)) | (-value - 1)
else:
    sign_magnitude = value

// Sign-magnitude → two's complement (decode, post-DWT):
if value & (1 << (bitdepth-1)):
    twos_complement = -(value & ((1 << (bitdepth-1)) - 1)) - 1
else:
    twos_complement = value
```

**Where it changes:**

- `internal/codestream/markers.go` — Add NLT marker constant (0xFF5C)
- `internal/codestream/header.go` — Add NLT info to `Header`, parse NLT in reader
- `internal/codestream/parser.go` — Parse NLT marker segment
- Encoder SIZ/marker generation — Write NLT marker for float components
- `encoder.go` / `decoder.go` — Apply NLT transform pre-DWT (encode) / post-DWT (decode)

### R10: 32-Bit SIZ Marker Encoding

Verify and fix the SIZ marker encoder to correctly write 32-bit signed components.

**Required:**
- `Ssiz` byte for 32-bit signed float: `0x80 | 31` = `0x9F` (signed flag + bit depth minus 1)
- `Ssiz` byte for 32-bit unsigned int: `31` = `0x1F`
- `Ssiz` byte for 16-bit signed half: `0x80 | 15` = `0x8F`

The decoder already reads these correctly (`ComponentInfo.Precision()` and `ComponentInfo.IsSigned()`). The encoder must write them symmetrically.

### R11: Per-Component Precision

Support per-component bit depth and signedness in the encoder, matching the SIZ marker's per-component `Ssiz` design.

**Current state:** `encoder.precision` is a single int — all components share one precision value.

**Required:** Components can have different precisions. This is needed for EXR images with mixed channel types (e.g., HALF RGB + FLOAT depth). Each component in the SIZ marker has its own `Ssiz` byte.

```go
// In the encoder struct:
type encoder struct {
    // Replace:
    //   precision int
    //   signed    bool
    // With:
    componentPrecision []int
    componentSigned    []bool
}
```

The DWT, entropy coding, and tier-2 packaging must respect per-component precision. The existing code passes `precision` uniformly — each of those call sites needs to use the component-specific value.

### R12: DC Level Shift for 32-Bit

Adjust DC level shift handling for 32-bit components.

**Current behavior:** DC level shift subtracts `2^(precision-1)` for unsigned data to center values around zero. For 8-bit: subtract 128. For 16-bit: subtract 32768.

**For float-as-int32 (NLT type 3):** The data is signed after NLT transformation, so DC level shift should be 0. The NLT transform itself handles the centering.

**For uint32 (no NLT):** DC level shift = `2^31` = 2147483648, which overflows `int32`. This requires using `int64` arithmetic for the shift operation, or treating the shift as a bit flip of the MSB.

## Priority Order

1. **R10** (SIZ 32-bit) — Small, bounded. Verify/fix the SIZ marker writer.
2. **R9** (NLT marker) — Required before float encoding works. Moderate scope: new marker type, transform logic.
3. **R12** (DC level shift) — Small change, but must be correct for 32-bit to work.
4. **R11** (per-component precision) — Moderate refactor, needed for mixed-type EXR images.
5. **R8** (EncodeFloat API) — The capstone. Depends on R9-R12.

R10 is a one-line verification. R9 is the most significant new code. R11 touches the most files but is mechanical. R8 ties it all together.

## Testing

### Roundtrip Tests

1. **Float32 single channel** — Encode a `FloatImage` with known float32 values (including negative, subnormal, NaN, Inf, zero). Decode via `DecodeFloat()`. Verify bitwise equality of the float32 patterns.

2. **Float32 RGB** — Encode 3-component float32 image. Decode. Verify all three components match bitwise.

3. **Mixed precision** — Encode an image with HALF (16-bit) RGB + FLOAT (32-bit) depth channel. Decode. Verify each channel's precision is preserved independently.

4. **UINT32** — Encode a `FloatImage` with integer values in 32-bit range (using `math.Float32frombits` to set exact bit patterns). Decode. Verify roundtrip.

### Interop Tests

5. **C++ compatibility** — Encode with go-jpeg2000, decode with OpenJPH (via test binary or reference output). Encode with OpenJPH, decode with go-jpeg2000. Verify bit-exact match.

6. **go-openexr integration** — Write an EXR file with FLOAT channels using HTJ2K compression via go-openexr. Read it back. Verify float values are preserved.

### Edge Cases

7. **All-zero image** — 32-bit, all zeros. Roundtrip must preserve exact zero bit pattern (both +0.0 and -0.0).

8. **Extreme values** — Float32 min/max, smallest subnormal, largest finite. Roundtrip preserves bits.

9. **NLT marker presence** — Verify the encoded codestream contains an NLT marker for float components and does not contain one for integer components.

## Non-Requirements

- **Lossy float encoding** — Out of scope. The C++ implementation only uses lossless (reversible 5/3 wavelet) for float data. Lossy float encoding via irreversible 9/7 wavelet and quantization is not needed.

- **Float64 (double) support** — JPEG 2000 SIZ supports up to 38-bit precision, but OpenEXR doesn't use 64-bit float channels. Out of scope.

- **Backward-compatible `Encode()` changes** — The existing `Encode(w, image.Image, opts)` function stays as-is. `EncodeFloat` is a new function, not a modification.
