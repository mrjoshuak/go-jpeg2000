package jpeg2000

import (
	"image"
	"math"
)

// halfToFloat32 widens an IEEE 754 binary16 bit pattern to float32. Every
// binary16 value is exactly representable in binary32, so the conversion is
// exact for finite values; infinities and NaNs keep their class, and NaN
// payload bits are shifted into the binary32 mantissa.
func halfToFloat32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1F
	mant := uint32(h & 0x03FF)

	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign) // ±0
		}
		// Subnormal half: renormalise into a normal float32. Shifting the
		// magnitude left until bit 10 is set costs 10-k shifts for a value
		// whose top set bit is k, leaving exponent (k-24)+127 == 113-shifts.
		shifts := 0
		for mant&0x0400 == 0 {
			mant <<= 1
			shifts++
		}
		mant &= 0x03FF
		return math.Float32frombits(sign | uint32(113-shifts)<<23 | mant<<13)
	case 0x1F:
		// Inf or NaN.
		return math.Float32frombits(sign | 0xFF<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
	}
}

// HalfImage holds multi-component IEEE 754 binary16 ("half") pixel data as
// raw 16-bit patterns, stored planar (one slice per component) to match the
// wavelet decomposition's component structure.
//
// Samples are carried as the exact 16-bit encoding, never as a widened
// float32, so every bit pattern — including signalling NaNs, negative zero
// and subnormals — survives an encode/decode round trip unchanged.
type HalfImage struct {
	Width, Height int
	Components    [][]uint16 // one slice per component (R, G, B, ...)

	// Cost reports what the decode spent. DecodeConfigCost exists because
	// image.Image is an interface with nowhere to hang this; a HalfImage is
	// a concrete type, so it carries the same figures directly.
	Cost DecodeCost
}

// Bounds returns the image rectangle.
func (h *HalfImage) Bounds() image.Rectangle {
	return image.Rect(0, 0, h.Width, h.Height)
}

// ComponentCount returns the number of components.
func (h *HalfImage) ComponentCount() int {
	return len(h.Components)
}

// At returns all component values at pixel (x, y), or nil if the coordinates
// are outside the image.
func (h *HalfImage) At(x, y int) []uint16 {
	if x < 0 || x >= h.Width || y < 0 || y >= h.Height {
		return nil
	}
	idx := y*h.Width + x
	vals := make([]uint16, len(h.Components))
	for c := range h.Components {
		// A caller-built HalfImage can have short or ragged component slices;
		// report zero for the missing samples rather than panicking.
		if idx < len(h.Components[c]) {
			vals[c] = h.Components[c][idx]
		}
	}
	return vals
}
