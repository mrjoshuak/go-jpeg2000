package codestream

import "math"

// Subband geometry and quantization arithmetic.
//
// These live here rather than in the encoder or the decoder because both need
// them and must agree exactly: the encoder places quantized coefficients into a
// Mallat-ordered array using these rectangles, and the decoder reads them back
// out of the same array with the same step sizes.

// Band orientation codes, matching the band type the block coder is told about
// (ISO/IEC 15444-1 Table D.1 context assignment order).
const (
	BandLL = 0
	BandHL = 1
	BandLH = 2
	BandHH = 3
)

// Detail-band indices in the order the three bands of a resolution level
// appear in a packet.
const (
	DetailHL = 0
	DetailLH = 1
	DetailHH = 2
)

// SubbandDims returns the dimensions of one detail subband, given the
// dimensions of the resolution level it is split from. band is a detail index
// (DetailHL, DetailLH, DetailHH).
//
// A 5/3 or 9/7 split of a signal of length W produces ceil(W/2) lowpass samples
// and floor(W/2) highpass samples. Those differ whenever W is odd, and which of
// the two applies depends on the orientation: HL is highpass horizontally and
// lowpass vertically, LH is the reverse, HH is highpass in both directions.
func SubbandDims(resW, resH, band int) (int, int) {
	lowW, highW := (resW+1)/2, resW/2
	lowH, highH := (resH+1)/2, resH/2
	switch band {
	case DetailHL:
		return highW, lowH
	case DetailLH:
		return lowW, highH
	default: // DetailHH
		return highW, highH
	}
}

// ResolutionDims returns the dimensions of resolution level r for a tile
// component of the given size, i.e. ceil(size / 2^(numRes-1-r)).
func ResolutionDims(tcW, tcH, numRes, r int) (int, int) {
	shift := uint(numRes - 1 - r)
	if shift > 62 {
		shift = 62
	}
	scale := 1 << shift
	return (tcW + scale - 1) / scale, (tcH + scale - 1) / scale
}

// BandDims returns the dimensions of the subband identified by (r, band): the
// LL band at resolution 0, or one of the three detail bands above it. band is a
// detail index.
func BandDims(tcW, tcH, numRes, r, band int) (int, int) {
	w, h := ResolutionDims(tcW, tcH, numRes, r)
	if r == 0 {
		return w, h
	}
	return SubbandDims(w, h, band)
}

// SubbandOffset returns the (x, y) offset of a subband within the Mallat-
// ordered array of wavelet coefficients. bandType is BandLL, BandHL, BandLH or
// BandHH.
func SubbandOffset(tcW, tcH, numRes, res, bandType int) (int, int) {
	if res == 0 {
		return 0, 0
	}
	decompLevel := numRes - 1 - res
	w, h := tcW, tcH
	for i := 0; i < decompLevel; i++ {
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	halfW := (w + 1) / 2
	halfH := (h + 1) / 2
	switch bandType {
	case BandHL:
		return halfW, 0
	case BandLH:
		return 0, halfH
	case BandHH:
		return halfW, halfH
	default:
		return 0, 0
	}
}

// BandTypeOf maps a resolution level and detail index to a band type code.
func BandTypeOf(res, detail int) int {
	if res == 0 {
		return BandLL
	}
	switch detail {
	case DetailHL:
		return BandHL
	case DetailLH:
		return BandLH
	default:
		return BandHH
	}
}

// BandIndex returns the position of a subband in the QCD/QCC step-size list:
// the LL band first, then the three detail bands of each resolution level in
// increasing order (ISO/IEC 15444-1 A.6.4).
func BandIndex(res, detail int) int {
	if res == 0 {
		return 0
	}
	return 1 + (res-1)*3 + detail
}

// SubbandRect describes one subband's rectangle inside a Mallat-ordered array
// of wavelet coefficients.
type SubbandRect struct {
	Index  int // position in the QCD/QCC step-size list
	Res    int // resolution level
	Detail int // DetailHL, DetailLH or DetailHH; 0 and meaningless at Res 0
	X0, Y0 int // offset of the rectangle within the array
	W, H   int // dimensions of the rectangle
}

// ForEachSubband calls fn once for every subband of a numRes-level
// decomposition of a width×height tile component, in QCD order.
func ForEachSubband(width, height, numRes int, fn func(SubbandRect)) {
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			w, h := BandDims(width, height, numRes, res, detail)
			x0, y0 := SubbandOffset(width, height, numRes, res, BandTypeOf(res, detail))
			fn(SubbandRect{
				Index:  BandIndex(res, detail),
				Res:    res,
				Detail: detail,
				X0:     x0,
				Y0:     y0,
				W:      w,
				H:      h,
			})
		}
	}
}

// BandGainLog2 returns log2 of the nominal dynamic-range gain of a subband,
// the quantity ISO/IEC 15444-1 Table E.1 tabulates: 0 for LL, 1 for HL and LH,
// 2 for HH. The 9/7 analysis filters this library implements have exactly those
// gains — a constant signal passes through the lowpass with gain 1 and a
// Nyquist signal through the highpass with gain 2 — so the nominal dynamic
// range of subband b is R_b = precision + BandGainLog2(b).
func BandGainLog2(res, detail int) int {
	if res == 0 {
		return 0
	}
	if detail == DetailHH {
		return 2
	}
	return 1
}

// Delta returns the quantization step size Δ_b of ISO/IEC 15444-1 Equation
// E-3,
//
//	Δ_b = 2^(R_b − ε_b) · (1 + μ_b / 2^11)
//
// where rb is the nominal dynamic range R_b of the subband. The result is in
// the units of the wavelet coefficients themselves, that is DC level shifted
// sample units.
func (s StepSize) Delta(rb int) float64 {
	return math.Ldexp(1+float64(s.Mantissa)/2048.0, rb-int(s.Exponent))
}

// PackStepSize expresses a step size as the (ε_b, μ_b) pair Equation E-3
// defines for a subband of nominal dynamic range rb, rounding the mantissa
// down so the packed step never exceeds delta.
//
// ε_b is a five-bit field and μ_b an eleven-bit one. A step size too small for
// that range saturates at ε_b = 31, μ_b = 0 and one too large at ε_b = 1,
// μ_b = 2047; the returned flag is false in both cases, because the packed step
// is then larger than the one asked for and any error bound derived from the
// request no longer holds. Compute the bound from the packed values instead.
func PackStepSize(delta float64, rb int) (StepSize, bool) {
	if !(delta > 0) || math.IsInf(delta, 0) {
		return StepSize{Exponent: 31}, false
	}
	// Write delta as 2^(rb-exp) * m with m in [1, 2).
	frac, e := math.Frexp(delta) // delta = frac * 2^e, frac in [0.5, 1)
	m := frac * 2                // m in [1, 2)
	exp := rb - (e - 1)
	if exp > 31 {
		return StepSize{Exponent: 31}, false
	}
	if exp < 1 {
		return StepSize{Exponent: 1, Mantissa: 2047}, false
	}
	mant := int(math.Floor((m - 1) * 2048))
	if mant < 0 {
		mant = 0
	}
	if mant > 2047 {
		mant = 2047
	}
	return StepSize{Exponent: uint8(exp), Mantissa: uint16(mant)}, true
}
