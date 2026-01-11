// Color space conversion functions for JPEG 2000
//
// This file implements color space conversions from the 19 enumerated colorspaces
// defined in ISO/IEC 15444-1 Annex M to sRGB for display. The conversions are
// applied automatically during decoding when a JP2 file specifies a non-sRGB
// colorspace in its Color Specification Box (colr).
//
// # Supported Colorspaces
//
// The following colorspace families are supported:
//
//   - YCbCr variants (enumcs 1, 3, 4, 18, 24): ITU-R BT.601 and BT.709 matrices
//   - CMY/CMYK (enumcs 11, 12): Subtractive color models
//   - YCCK (enumcs 13): PhotoYCC-based CMYK
//   - CIE colorspaces (enumcs 14, 19): L*a*b* and J*a*b* with D50 illuminant
//   - Extended gamut (enumcs 20, 21): e-sRGB and ROMM-RGB (ProPhoto)
//   - Video colorspaces (enumcs 9, 22, 23): PhotoYCC and YPbPr
//
// # Color Conversion Pipeline
//
// For images with non-sRGB colorspaces, the decoder applies:
//
//  1. Inverse wavelet transform (DWT)
//  2. Inverse multi-component transform (MCT) if used during encoding
//  3. DC level shift
//  4. Colorspace conversion to sRGB (this file)
//  5. Output image creation
//
// # Precision Handling
//
// All conversion functions work with arbitrary bit precision (1-16 bits).
// The precision parameter indicates the number of bits per component, and
// values are scaled appropriately. For example, at 8-bit precision, the
// maximum value is 255; at 12-bit, it's 4095.
//
// # References
//
//   - ISO/IEC 15444-1:2019 Annex M - Enumerated colorspace definitions
//   - ITU-R BT.601-7 - Studio encoding for standard definition
//   - ITU-R BT.709-6 - Studio encoding for high definition
//   - IEC 61966-2-1 - sRGB color space
//   - ISO 22028-2 - ROMM RGB color space (ProPhoto)

package jpeg2000

import "math"

// colorConversion defines a function that converts component data in-place from
// a source color space to sRGB.
type colorConversion func(componentData [][]int32, precision int)

// getColorConversion returns the appropriate conversion function for a color space.
// Returns nil if no conversion is needed (already sRGB or gray).
func getColorConversion(cs ColorSpace) colorConversion {
	switch cs {
	case ColorSpaceSYCC:
		return convertSYCCToRGB
	case ColorSpaceYCbCr2:
		return convertYCbCr601ToRGB // BT.601-5 625-line
	case ColorSpaceYCbCr3:
		return convertYCbCr601ToRGB // BT.601-5 525-line (same matrix)
	case ColorSpacePhotoYCC:
		return convertPhotoYCCToRGB
	case ColorSpaceCMY:
		return convertCMYToRGB
	case ColorSpaceCMYK:
		return convertCMYKToRGB
	case ColorSpaceYCCK:
		return convertYCCKToRGB
	case ColorSpaceCIELab:
		return convertCIELabToRGB
	case ColorSpaceCIEJab:
		return convertCIEJabToRGB
	case ColorSpaceESRGB:
		return convertESRGBToRGB
	case ColorSpaceROMMRGB:
		return convertROMMRGBToRGB
	case ColorSpaceYPbPr60:
		return convertYPbPr709ToRGB
	case ColorSpaceYPbPr50:
		return convertYPbPr709ToRGB // Same matrix
	case ColorSpaceEYCC:
		return convertEYCCToRGB
	default:
		// sRGB, Gray, Bilevel, Unknown, Unspecified - no conversion
		return nil
	}
}

// convertSYCCToRGB converts sYCC (ITU-R BT.709-5) to sRGB.
// sYCC uses sRGB primaries with the BT.709 YCbCr matrix.
func convertSYCCToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	halfVal := float64(int32(1) << (precision - 1))

	for i := range componentData[0] {
		// Y is [0, maxVal], Cb and Cr are centered at halfVal
		y := float64(componentData[0][i])
		cb := float64(componentData[1][i]) - halfVal
		cr := float64(componentData[2][i]) - halfVal

		// ITU-R BT.709-5 inverse matrix
		r := y + 1.5748*cr
		g := y - 0.1873*cb - 0.4681*cr
		b := y + 1.8556*cb

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertYCbCr601ToRGB converts YCbCr (ITU-R BT.601-5) to sRGB.
// Used for YCbCr(2) (625-line) and YCbCr(3) (525-line).
func convertYCbCr601ToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	halfVal := float64(int32(1) << (precision - 1))

	for i := range componentData[0] {
		y := float64(componentData[0][i])
		cb := float64(componentData[1][i]) - halfVal
		cr := float64(componentData[2][i]) - halfVal

		// ITU-R BT.601-5 inverse matrix
		r := y + 1.402*cr
		g := y - 0.344136*cb - 0.714136*cr
		b := y + 1.772*cb

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertPhotoYCCToRGB converts Kodak PhotoYCC to sRGB.
// PhotoYCC uses a Rec. 709 like matrix but with different scaling.
func convertPhotoYCCToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	scale := maxVal / 255.0

	for i := range componentData[0] {
		// PhotoYCC has Y in [0, 255*1.402], C1/C2 offset at 156
		y := float64(componentData[0][i]) / scale
		c1 := float64(componentData[1][i])/scale - 156.0
		c2 := float64(componentData[2][i])/scale - 156.0

		// PhotoYCC inverse transform
		r := y + 1.3584*c2
		g := y - 0.4302*c1 - 0.7915*c2
		b := y + 2.2179*c1

		componentData[0][i] = clampToInt32(r*scale, 0, maxVal)
		componentData[1][i] = clampToInt32(g*scale, 0, maxVal)
		componentData[2][i] = clampToInt32(b*scale, 0, maxVal)
	}
}

// convertCMYToRGB converts CMY to sRGB.
// Simple subtractive color model: R = 1-C, G = 1-M, B = 1-Y
func convertCMYToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := int32(1)<<precision - 1

	for i := range componentData[0] {
		c := componentData[0][i]
		m := componentData[1][i]
		y := componentData[2][i]

		componentData[0][i] = maxVal - c // R
		componentData[1][i] = maxVal - m // G
		componentData[2][i] = maxVal - y // B
	}
}

// convertCMYKToRGB converts CMYK to sRGB.
// Uses the standard CMYK to RGB formula.
func convertCMYKToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 4 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)

	for i := range componentData[0] {
		c := float64(componentData[0][i]) / maxVal
		m := float64(componentData[1][i]) / maxVal
		y := float64(componentData[2][i]) / maxVal
		k := float64(componentData[3][i]) / maxVal

		r := (1 - c) * (1 - k) * maxVal
		g := (1 - m) * (1 - k) * maxVal
		b := (1 - y) * (1 - k) * maxVal

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
		// Note: 4th component is discarded after conversion
	}
}

// convertYCCKToRGB converts YCCK (PhotoYCC + K) to sRGB.
// First converts YCC to CMY, then applies K.
func convertYCCKToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 4 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	scale := maxVal / 255.0

	for i := range componentData[0] {
		// Convert YCC to RGB first (PhotoYCC transform)
		y := float64(componentData[0][i]) / scale
		c1 := float64(componentData[1][i])/scale - 156.0
		c2 := float64(componentData[2][i])/scale - 156.0
		k := float64(componentData[3][i]) / maxVal

		r := y + 1.3584*c2
		g := y - 0.4302*c1 - 0.7915*c2
		b := y + 2.2179*c1

		// Apply K (black) channel
		r = r * scale * (1 - k)
		g = g * scale * (1 - k)
		b = b * scale * (1 - k)

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertCIELabToRGB converts CIE L*a*b* (D50) to sRGB.
// Goes through XYZ as intermediate.
func convertCIELabToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)

	// D50 white point
	const xn, yn, zn = 0.96422, 1.0, 0.82521

	for i := range componentData[0] {
		// L* is [0, 100], a* and b* are approximately [-128, 127]
		L := float64(componentData[0][i]) / maxVal * 100.0
		a := float64(componentData[1][i])/maxVal*255.0 - 128.0
		b := float64(componentData[2][i])/maxVal*255.0 - 128.0

		// Lab to XYZ
		fy := (L + 16.0) / 116.0
		fx := a/500.0 + fy
		fz := fy - b/200.0

		x := xn * labInverseF(fx)
		y := yn * labInverseF(fy)
		z := zn * labInverseF(fz)

		// XYZ (D50) to linear sRGB (D65) via Bradford transform
		// Simplified: using direct XYZ to sRGB matrix (approximation)
		rLin := 3.2404542*x - 1.5371385*y - 0.4985314*z
		gLin := -0.9692660*x + 1.8760108*y + 0.0415560*z
		bLin := 0.0556434*x - 0.2040259*y + 1.0572252*z

		// Apply sRGB gamma
		r := srgbGamma(rLin) * maxVal
		g := srgbGamma(gLin) * maxVal
		bVal := srgbGamma(bLin) * maxVal

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(bVal, 0, maxVal)
	}
}

// labInverseF is the inverse of the Lab f function.
func labInverseF(t float64) float64 {
	const delta = 6.0 / 29.0
	if t > delta {
		return t * t * t
	}
	return 3 * delta * delta * (t - 4.0/29.0)
}

// srgbGamma applies the sRGB gamma curve.
func srgbGamma(linear float64) float64 {
	if linear <= 0.0031308 {
		return 12.92 * linear
	}
	return 1.055*math.Pow(linear, 1.0/2.4) - 0.055
}

// srgbInverseGamma removes the sRGB gamma curve.
func srgbInverseGamma(encoded float64) float64 {
	if encoded <= 0.04045 {
		return encoded / 12.92
	}
	return math.Pow((encoded+0.055)/1.055, 2.4)
}

// convertCIEJabToRGB converts CIE J*a*b* (CIECAM02) to sRGB.
// This is a simplified implementation.
func convertCIEJabToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)

	// Simplified CIECAM02 inverse - treating as Lab-like
	// A full implementation would require viewing conditions
	for i := range componentData[0] {
		J := float64(componentData[0][i]) / maxVal * 100.0
		a := float64(componentData[1][i])/maxVal*255.0 - 128.0
		b := float64(componentData[2][i])/maxVal*255.0 - 128.0

		// Simplified: treat J as L*, a and b similarly to Lab
		// This is an approximation - true CIECAM02 is more complex
		L := J // Approximate J ≈ L* for viewing conditions

		// Use Lab to RGB conversion
		fy := (L + 16.0) / 116.0
		fx := a/500.0 + fy
		fz := fy - b/200.0

		x := 0.96422 * labInverseF(fx)
		y := 1.0 * labInverseF(fy)
		z := 0.82521 * labInverseF(fz)

		rLin := 3.2404542*x - 1.5371385*y - 0.4985314*z
		gLin := -0.9692660*x + 1.8760108*y + 0.0415560*z
		bLin := 0.0556434*x - 0.2040259*y + 1.0572252*z

		r := srgbGamma(rLin) * maxVal
		g := srgbGamma(gLin) * maxVal
		bVal := srgbGamma(bLin) * maxVal

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(bVal, 0, maxVal)
	}
}

// convertESRGBToRGB converts e-sRGB (extended sRGB) to sRGB.
// e-sRGB allows values outside [0,1] for wider gamut.
func convertESRGBToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)

	for i := range componentData[0] {
		// e-sRGB uses the same primaries as sRGB but allows extended range
		// Values are encoded with offset to allow negatives
		// The encoding uses: encoded = (linear + 0.25) / 1.25 for extended range

		r := float64(componentData[0][i])/maxVal*1.25 - 0.25
		g := float64(componentData[1][i])/maxVal*1.25 - 0.25
		b := float64(componentData[2][i])/maxVal*1.25 - 0.25

		// Clamp to sRGB range and apply gamma
		r = srgbGamma(clampFloat64(r, 0, 1)) * maxVal
		g = srgbGamma(clampFloat64(g, 0, 1)) * maxVal
		b = srgbGamma(clampFloat64(b, 0, 1)) * maxVal

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertROMMRGBToRGB converts ROMM-RGB (ProPhoto RGB) to sRGB.
// ROMM-RGB has a wider gamut than sRGB.
func convertROMMRGBToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)

	// ROMM-RGB to XYZ matrix (D50)
	// Then XYZ to sRGB
	for i := range componentData[0] {
		// Remove ROMM gamma (gamma = 1.8 simplified)
		rRomm := math.Pow(float64(componentData[0][i])/maxVal, 1.8)
		gRomm := math.Pow(float64(componentData[1][i])/maxVal, 1.8)
		bRomm := math.Pow(float64(componentData[2][i])/maxVal, 1.8)

		// ROMM-RGB to XYZ (D50)
		x := 0.7977*rRomm + 0.1352*gRomm + 0.0313*bRomm
		y := 0.2880*rRomm + 0.7119*gRomm + 0.0001*bRomm
		z := 0.0000*rRomm + 0.0000*gRomm + 0.8249*bRomm

		// XYZ to linear sRGB (with D50 to D65 adaptation approximation)
		rLin := 3.2404542*x - 1.5371385*y - 0.4985314*z
		gLin := -0.9692660*x + 1.8760108*y + 0.0415560*z
		bLin := 0.0556434*x - 0.2040259*y + 1.0572252*z

		// Apply sRGB gamma
		r := srgbGamma(clampFloat64(rLin, 0, 1)) * maxVal
		g := srgbGamma(clampFloat64(gLin, 0, 1)) * maxVal
		b := srgbGamma(clampFloat64(bLin, 0, 1)) * maxVal

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertYPbPr709ToRGB converts YPbPr (HD video) to sRGB.
// Uses ITU-R BT.709 matrix (same as HDTV).
func convertYPbPr709ToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	halfVal := float64(int32(1) << (precision - 1))

	for i := range componentData[0] {
		// Y is [0, maxVal], Pb and Pr are centered at halfVal
		y := float64(componentData[0][i])
		pb := float64(componentData[1][i]) - halfVal
		pr := float64(componentData[2][i]) - halfVal

		// ITU-R BT.709 inverse matrix (same as sYCC)
		r := y + 1.5748*pr
		g := y - 0.1873*pb - 0.4681*pr
		b := y + 1.8556*pb

		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// convertEYCCToRGB converts e-sYCC (extended sYCC) to sRGB.
// e-sYCC allows extended gamut YCbCr values.
func convertEYCCToRGB(componentData [][]int32, precision int) {
	if len(componentData) < 3 {
		return
	}

	maxVal := float64(int32(1)<<precision - 1)
	halfVal := float64(int32(1) << (precision - 1))

	for i := range componentData[0] {
		// Extended YCbCr - Y can exceed normal range
		y := float64(componentData[0][i])
		cb := float64(componentData[1][i]) - halfVal
		cr := float64(componentData[2][i]) - halfVal

		// Same matrix as sYCC but allowing extended values
		r := y + 1.5748*cr
		g := y - 0.1873*cb - 0.4681*cr
		b := y + 1.8556*cb

		// Clamp to displayable range
		componentData[0][i] = clampToInt32(r, 0, maxVal)
		componentData[1][i] = clampToInt32(g, 0, maxVal)
		componentData[2][i] = clampToInt32(b, 0, maxVal)
	}
}

// clampToInt32 clamps a float64 to the given range and converts to int32.
func clampToInt32(v, min, max float64) int32 {
	if v < min {
		return int32(min)
	}
	if v > max {
		return int32(max)
	}
	return int32(v + 0.5) // Round
}

// clampFloat64 clamps a float64 to the given range.
func clampFloat64(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
