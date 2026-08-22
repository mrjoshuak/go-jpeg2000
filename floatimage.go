package jpeg2000

import "image"

// FloatImage holds multi-component float pixel data.
// Components are stored separately (planar) to match the wavelet
// decomposition's component structure. This preserves the full
// precision of DWT coefficients without quantizing to integers.
type FloatImage struct {
	Width, Height int
	Components    [][]float32 // one slice per component (R, G, B, ...)

	// BitDepth and Signed are filled in by the decoder from the codestream's
	// Ssiz field. They are NOT inputs: EncodeFloat ignores whatever they hold.
	//
	// That is not an oversight but it was undocumented, which is worse. The
	// float path reinterprets binary32 bit patterns as signed 32-bit samples,
	// so Ssiz must say 32-bit signed for the samples to mean anything to
	// another reader; a caller setting BitDepth to 12 cannot be given a 12-bit
	// codestream, and until this was written down they were given a 32-bit one
	// with no indication. Encoding refuses nothing here on purpose — callers
	// already pass 16 and 32 and their files are correct — but the asymmetry
	// is now stated rather than discovered.
	//
	// This came out of widening the conformance matrix past its existing axes,
	// which is what that exercise is for.
	BitDepth int
	Signed   bool

	// Cost reports what the decode spent. DecodeConfigCost exists because
	// image.Image is an interface with nowhere to hang this; a FloatImage is
	// a concrete type, so it carries the same figures directly.
	Cost DecodeCost
}

// Bounds returns the image rectangle.
func (f *FloatImage) Bounds() image.Rectangle {
	return image.Rect(0, 0, f.Width, f.Height)
}

// ComponentCount returns the number of components.
func (f *FloatImage) ComponentCount() int {
	return len(f.Components)
}

// At returns all component values at pixel (x, y).
func (f *FloatImage) At(x, y int) []float32 {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return nil
	}
	idx := y*f.Width + x
	vals := make([]float32, len(f.Components))
	for c := range f.Components {
		vals[c] = f.Components[c][idx]
	}
	return vals
}
