package jpeg2000

import "image"

// FloatImage holds multi-component float pixel data.
// Components are stored separately (planar) to match the wavelet
// decomposition's component structure. This preserves the full
// precision of DWT coefficients without quantizing to integers.
type FloatImage struct {
	Width, Height int
	Components    [][]float32 // one slice per component (R, G, B, ...)
	BitDepth      int         // original bit depth
	Signed        bool
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
