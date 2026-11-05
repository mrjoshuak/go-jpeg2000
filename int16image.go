package jpeg2000

import "image"

// Int16Image holds multi-component signed 16-bit integer samples, stored
// planar (one slice per component) like HalfImage.
//
// It is the way to write signed samples, which image.Image cannot carry: Go's
// image types are unsigned, so a signed source such as CT data in Hounsfield
// units (DICOM PixelRepresentation 1) would otherwise have to be offset into
// an unsigned range, which changes what the codestream says the samples are.
// EncodeInt16 declares each component 16-bit signed in SIZ (Ssiz 0x8F) and
// codes the samples as they are, without the DC level shift an unsigned
// component gets (ISO/IEC 15444-1 G.1.2).
type Int16Image struct {
	Width, Height int
	Components    [][]int16 // one slice per component, row-major

	// Cost reports what the decode spent, as on HalfImage.
	Cost DecodeCost
}

// Bounds returns the image rectangle.
func (m *Int16Image) Bounds() image.Rectangle {
	return image.Rect(0, 0, m.Width, m.Height)
}

// ComponentCount returns the number of components.
func (m *Int16Image) ComponentCount() int {
	return len(m.Components)
}

// At returns all component values at pixel (x, y), or nil if the coordinates
// are outside the image.
func (m *Int16Image) At(x, y int) []int16 {
	if x < 0 || x >= m.Width || y < 0 || y >= m.Height {
		return nil
	}
	idx := y*m.Width + x
	vals := make([]int16, len(m.Components))
	for c := range m.Components {
		// A caller-built image can have short or ragged component slices;
		// report zero for the missing samples rather than panicking.
		if idx < len(m.Components[c]) {
			vals[c] = m.Components[c][idx]
		}
	}
	return vals
}
