package dwt

import (
	"math"
	"math/rand"
	"testing"
)

// Conformance checks for the irreversible 9/7 transform.
//
// Like the 5/3 checks in conformance_test.go, none of these compares the
// forward transform against its own inverse. A round trip cannot see a defect
// the two directions share, and the 5/3 transform in this package carried
// exactly such a defect until it was measured against the standard instead.
//
// Two independent formulations of the same filter bank are used:
//
//	specForward97   the lifting steps of ISO/IEC 15444-1 F.4.8.2, transcribed
//	                from the equations, run over an explicitly extended signal
//	                rather than with the boundary special cases the production
//	                code folds in.
//	filterForward97 direct convolution with the tabulated 9/7 analysis filter
//	                taps of Table F.7. This shares nothing with lifting at all,
//	                so it is what catches a scaling factor applied to the wrong
//	                half of the coefficients.
//
// Two properties are pinned separately, because the quantizer depends on them:
// the lowpass analysis has DC gain 1 and the highpass has Nyquist gain 2, which
// is what makes the Table E.1 nominal dynamic range R_b = precision + gain
// correct for this implementation.

// extendSym returns x extended by margin samples on each side using
// period-symmetric whole-point extension, the PSE of ISO/IEC 15444-1 F.3.3.
func extendSym(x []float64, margin int) []float64 {
	n := len(x)
	out := make([]float64, n+2*margin)
	at := func(i int) float64 {
		if n == 1 {
			return x[0]
		}
		p := 2 * (n - 1)
		i = ((i % p) + p) % p
		if i >= n {
			i = p - i
		}
		return x[i]
	}
	for i := range out {
		out[i] = at(i - margin)
	}
	return out
}

// specForward97 is a transcription of the 1D_FILTD_9-7I procedure of ISO/IEC
// 15444-1 F.4.8.2 for an interval starting at an even coordinate, followed by
// the F.3.5 deinterleave into lowpass then highpass. It shares no code with
// Forward97.
func specForward97(x []float64) []float64 {
	n := len(x)
	if n < 2 {
		return append([]float64(nil), x...)
	}
	const (
		a = -1.586134342059924
		b = -0.052980118572961
		c = 0.882911075530934
		d = 0.443506852043971
		k = 1.230174104914001
	)
	// A margin of eight leaves room for the four lifting steps, each of which
	// consumes one sample at each end.
	const margin = 8
	y := extendSym(x, margin)
	m := len(y)
	// Only positions with both neighbours inside the array can be updated, and
	// the usable range shrinks by one at each end per step; margin 8 keeps
	// [margin-4, margin+n+4) valid, which covers the whole interval.
	// A lifting step must read the values the previous step wrote, so each one
	// runs over a copy.
	lift := func(parity int, coef float64) {
		prev := append([]float64(nil), y...)
		for i := 1; i < m-1; i++ {
			if (i-margin)&1 == parity {
				y[i] = prev[i] + coef*(prev[i-1]+prev[i+1])
			}
		}
	}
	lift(1, a) // Y(2n+1) += a (Y(2n) + Y(2n+2))
	lift(0, b) // Y(2n)   += b (Y(2n-1) + Y(2n+1))
	lift(1, c)
	lift(0, d)

	out := make([]float64, n)
	half := (n + 1) / 2
	for i := 0; i < n; i++ {
		v := y[margin+i]
		if i&1 == 0 {
			out[i/2] = v / k
		} else {
			out[half+i/2] = v * k
		}
	}
	return out
}

// Analysis filter taps of the 9/7 filter, ISO/IEC 15444-1 Table F.7. Index i
// holds the tap at offset i from the centre; the filters are symmetric.
var (
	lowAnalysis97 = []float64{
		0.6029490182363579,
		0.2668641184428723,
		-0.07822326652898785,
		-0.01686411844287495,
		0.02674875741080976,
	}
	highAnalysis97 = []float64{
		1.115087052456994,
		-0.5912717631142470,
		-0.05754352622849957,
		0.09127176311424948,
	}
)

// filterForward97 computes the same transform by direct convolution with the
// tabulated taps, over a symmetrically extended signal.
func filterForward97(x []float64) []float64 {
	n := len(x)
	if n < 2 {
		return append([]float64(nil), x...)
	}
	const margin = 8
	e := extendSym(x, margin)
	out := make([]float64, n)
	half := (n + 1) / 2
	for i := 0; i < n; i++ {
		taps := lowAnalysis97
		if i&1 == 1 {
			taps = highAnalysis97
		}
		s := taps[0] * e[margin+i]
		for k := 1; k < len(taps); k++ {
			s += taps[k] * (e[margin+i-k] + e[margin+i+k])
		}
		if i&1 == 0 {
			out[i/2] = s
		} else {
			out[half+i/2] = s
		}
	}
	return out
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) <= 1e-9*(1+math.Abs(want))
}

// TestForward97MatchesSpec pins the 1D lifting steps and their boundary
// handling against the standard, for every length the subband geometry can
// produce.
func TestForward97MatchesSpec(t *testing.T) {
	rng := rand.New(rand.NewSource(20260818))
	for n := 1; n <= 70; n++ {
		for trial := 0; trial < 20; trial++ {
			src := make([]float64, n)
			for i := range src {
				src[i] = float64(rng.Intn(512) - 256)
			}
			want := specForward97(src)
			wantFilt := filterForward97(src)
			for i := range want {
				if !closeEnough(want[i], wantFilt[i]) {
					t.Fatalf("the two reference formulations disagree at n=%d index %d: lifting %v, taps %v",
						n, i, want[i], wantFilt[i])
				}
			}
			got := append([]float64(nil), src...)
			Forward97(got, n)
			for i := range want {
				if !closeEnough(got[i], want[i]) {
					t.Fatalf("Forward97 n=%d index %d: got %v, want %v\nsrc  %v\ngot  %v\nwant %v",
						n, i, got[i], want[i], src, got, want)
				}
			}
		}
	}
}

// specDecompose97 is a literal multi-level 2D forward transform: at each level
// the vertical pass runs over every column of the current LL rectangle, then
// the horizontal pass over every row, which is the order F.4.8 gives for
// 2D_SD.
//
// Unlike the 5/3 case, the order of the two separable passes is not observable
// here and no test can pin it: the 9/7 lifting steps are linear, so the row and
// the column pass commute exactly. What this check does pin is the rectangle
// each level is taken over, which is where a multi-level transform on
// odd-sized images goes wrong.
func specDecompose97(data []float64, stride, width, height, levels int) []float64 {
	out := append([]float64(nil), data...)
	w, h := width, height
	for level := 0; level < levels; level++ {
		if w < 1 || h < 1 {
			break
		}
		col := make([]float64, h)
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				col[y] = out[y*stride+x]
			}
			t := specForward97(col)
			for y := 0; y < h; y++ {
				out[y*stride+x] = t[y]
			}
		}
		row := make([]float64, w)
		for y := 0; y < h; y++ {
			copy(row, out[y*stride:y*stride+w])
			t := specForward97(row)
			copy(out[y*stride:y*stride+w], t)
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	return out
}

// TestDecomposeMultiLevel97MatchesSpec checks the 2D pass structure and the
// rectangle each level is taken over.
func TestDecomposeMultiLevel97MatchesSpec(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	sizes := []int{1, 2, 3, 4, 5, 7, 8, 9, 12, 15, 16, 17, 23, 31, 32, 33, 45, 64}
	for _, w := range sizes {
		for _, h := range sizes {
			for levels := 1; levels <= 5; levels++ {
				src := make([]float64, w*h)
				for i := range src {
					src[i] = float64(rng.Intn(512) - 256)
				}
				want := specDecompose97(src, w, w, h, levels)
				got := append([]float64(nil), src...)
				DecomposeMultiLevel97(got, w, h, levels)
				for i := range want {
					if !closeEnough(got[i], want[i]) {
						t.Fatalf("DecomposeMultiLevel97 %dx%d levels=%d: index %d (x=%d,y=%d) got %v, want %v",
							w, h, levels, i, i%w, i/w, got[i], want[i])
					}
				}
			}
		}
	}
}

// TestForward97Normalization pins the two gains the quantizer's nominal dynamic
// range depends on: a constant signal has to pass the lowpass analysis with
// gain one, and a Nyquist signal the highpass with gain two. Those are the
// log2 gains 0 and 1 that ISO/IEC 15444-1 Table E.1 assigns to L and H, and so
// R_b = precision + gain for this transform.
func TestForward97Normalization(t *testing.T) {
	const n = 64
	x := make([]float64, n)
	for i := range x {
		x[i] = 100
	}
	Forward97(x, n)
	for i := 0; i < n/2; i++ {
		if !closeEnough(x[i], 100) {
			t.Fatalf("lowpass DC gain: coefficient %d is %v, want 100", i, x[i])
		}
		if math.Abs(x[n/2+i]) > 1e-9 {
			t.Fatalf("highpass DC leakage: coefficient %d is %v, want 0", i, x[n/2+i])
		}
	}
	for i := range x {
		if i%2 == 0 {
			x[i] = 100
		} else {
			x[i] = -100
		}
	}
	Forward97(x, n)
	for i := 0; i < n/2; i++ {
		if math.Abs(x[i]) > 1e-9 {
			t.Fatalf("lowpass Nyquist leakage: coefficient %d is %v, want 0", i, x[i])
		}
		if !closeEnough(x[n/2+i], -200) {
			t.Fatalf("highpass Nyquist gain: coefficient %d is %v, want -200", i, x[n/2+i])
		}
	}
}

// TestInverse97UndoesSpecForward checks the inverse against the spec-derived
// forward, so that a defect fixed in one direction cannot be silently
// reintroduced by the other.
func TestInverse97UndoesSpecForward(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	sizes := []int{2, 3, 5, 8, 9, 16, 17, 23, 32, 33, 45, 64}
	for _, w := range sizes {
		for _, h := range sizes {
			for levels := 1; levels <= 4; levels++ {
				src := make([]float64, w*h)
				for i := range src {
					src[i] = float64(rng.Intn(512) - 256)
				}
				coefs := specDecompose97(src, w, w, h, levels)
				got := append([]float64(nil), coefs...)
				ReconstructMultiLevel97(got, w, h, levels)
				for i := range src {
					if math.Abs(got[i]-src[i]) > 1e-6*(1+math.Abs(src[i])) {
						t.Fatalf("ReconstructMultiLevel97 %dx%d levels=%d: index %d got %v, want %v",
							w, h, levels, i, got[i], src[i])
					}
				}
			}
		}
	}
}
