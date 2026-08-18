package jpeg2000

import (
	"math"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// Quantization for the irreversible (9/7) path.
//
// ISO/IEC 15444-1 Annex E defines the only quantizer the format has: every
// coefficient of subband b is divided by a step size Δ_b and truncated toward
// zero, and Δ_b is carried in QCD/QCC as an exponent/mantissa pair,
//
//	Δ_b = 2^(R_b − ε_b) · (1 + μ_b / 2^11)                          (E-3)
//
// where R_b = precision + Table E.1 gain is the nominal dynamic range of the
// subband. A decoder reconstructs the bin centre, sign(q)·(|q| + 1/2)·Δ_b.
// That is what OpenJPH does: ojph_subband.cpp forms
// delta = get_irrev_delta(...) / 2^(31 − K_max), the block coder places the
// magnitude at (v_n + 2) << (30 − K_max), and the product of the two is
// (|q| + 1/2)·Δ_b in units of the sample range.
//
// This file chooses the Δ_b, which the standard leaves entirely to the encoder,
// and states the reconstruction error that choice bounds.

// Peak synthesis gains of the 9/7 filter bank.
//
// Reconstruction is linear: x̂ = Σ_b Σ_k c_{b,k}·g_{b,k}, so an error e in the
// coefficients of subband b moves a reconstructed sample by at most
//
//	|e|_∞ · max_n Σ_k |g_{b,k}(n)|
//
// and Σ_k |g_{b,k}(n)| ≤ ‖g_b‖₁, because the basis functions of one subband are
// translates of a single impulse response on a lattice. Symmetric extension
// folds the basis functions near an edge back into the interval, which can put
// two lattice points on top of one output sample, so the bound doubles per
// filtered dimension.
//
// The 1D L1 norms are measured, not assumed: ‖g‖₁ for n levels of lowpass
// synthesis is 1.3945·2^n and for a detail band at level n it is 0.6964·2^n,
// converging from below (TestSynthesisL1Gains97 in quant_test.go re-measures
// them against the inverse transform). The constants below round those up.
const (
	lowSynthL1Coef  = 1.4
	highSynthL1Coef = 0.7
	// boundaryFold is the factor by which period-symmetric extension can
	// increase the overlap of synthesis basis functions at one output sample.
	boundaryFold = 2.0
)

// lowSynthL1 bounds ‖g‖₁ for n levels of 1D lowpass synthesis, including the
// boundary allowance. Zero levels is the identity, which has no boundary.
func lowSynthL1(n int) float64 {
	if n <= 0 {
		return 1
	}
	return boundaryFold * lowSynthL1Coef * math.Ldexp(1, n)
}

// highSynthL1 bounds ‖g‖₁ for a detail band at decomposition level n, that is
// one highpass synthesis followed by n−1 lowpass ones, with the boundary
// allowance.
func highSynthL1(n int) float64 {
	if n <= 0 {
		return 1
	}
	return boundaryFold * highSynthL1Coef * math.Ldexp(1, n)
}

// synthesisGain97 returns the bound on how far one unit of coefficient error in
// the subband at (res, detail) can move a reconstructed sample, for a transform
// with numRes resolution levels.
func synthesisGain97(numRes, res, detail int) float64 {
	levels := numRes - 1
	if res == 0 {
		g := lowSynthL1(levels)
		return g * g
	}
	d := numRes - res // decomposition level, 1 = finest
	switch detail {
	case codestream.DetailHL, codestream.DetailLH:
		return highSynthL1(d) * lowSynthL1(d)
	default:
		return highSynthL1(d) * highSynthL1(d)
	}
}

// errorBudget returns the reconstruction error, in DC level shifted sample
// units, that the step sizes are sized to stay within.
//
// At quality 100 an eight-bit image is held to half a sample: after the
// decoder rounds to an integer the result can differ from the source by at most
// one count. Each ten points of quality below that doubles the budget, and the
// budget scales with the sample range so that a sixteen-bit image is held to
// the same relative accuracy rather than to the same absolute one — the latter
// would need quantization indices wider than the 32-bit coefficients the block
// coder carries.
func errorBudget(quality, precision int) float64 {
	q := quality
	if q <= 0 {
		q = 100
	}
	if q > 100 {
		q = 100
	}
	if q < 1 {
		q = 1
	}
	if precision < 1 {
		precision = 1
	}
	return math.Ldexp(0.5, precision-8) * math.Pow(2, float64(100-q)/10)
}

// idealStepSizes returns the step size for each subband, in QCD order, that
// spreads the error budget evenly over the subbands.
//
// Truncating quantization puts a coefficient of magnitude below Δ_b at zero, so
// the coefficient error is bounded by Δ_b rather than Δ_b/2, and the
// reconstruction error by Σ_b Δ_b·A_b where A_b is the synthesis gain above.
// Setting Δ_b = budget / (N·A_b) makes that sum the budget.
func idealStepSizes(numRes, quality, precision int) []float64 {
	numBands := 3*(numRes-1) + 1
	budget := errorBudget(quality, precision)
	deltas := make([]float64, numBands)
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			idx := codestream.BandIndex(res, detail)
			deltas[idx] = budget / (float64(numBands) * synthesisGain97(numRes, res, detail))
		}
	}
	return deltas
}

// packStepSizes converts step sizes in sample units into the QCD
// exponent/mantissa pairs, given the image precision.
func packStepSizes(deltas []float64, numRes, precision int) []codestream.StepSize {
	steps := make([]codestream.StepSize, len(deltas))
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			idx := codestream.BandIndex(res, detail)
			if idx >= len(deltas) {
				continue
			}
			rb := precision + codestream.BandGainLog2(res, detail)
			steps[idx], _ = codestream.PackStepSize(deltas[idx], rb)
		}
	}
	return steps
}

// reconstructionBound returns the largest error, in DC level shifted sample
// units, that a decoder reconstructing bin centres can produce from the given
// step sizes. It reads only what the codestream carries, so it can be evaluated
// for any file rather than only for one this encoder wrote.
func reconstructionBound(steps []codestream.StepSize, numRes, precision int) float64 {
	total := 0.0
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			idx := codestream.BandIndex(res, detail)
			if idx >= len(steps) {
				continue
			}
			rb := precision + codestream.BandGainLog2(res, detail)
			total += steps[idx].Delta(rb) * synthesisGain97(numRes, res, detail)
		}
	}
	return total
}

// quantizeIndex applies the Annex E quantizer: divide by the step size and
// truncate toward zero.
func quantizeIndex(v, delta float64) int32 {
	q := v / delta
	if q >= 0 {
		return int32(math.Floor(q))
	}
	return -int32(math.Floor(-q))
}
