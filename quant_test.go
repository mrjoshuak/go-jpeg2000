package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
)

// TestSynthesisL1GainsBoundMeasurement re-measures the 1D synthesis L1 norms
// the step-size allocation is derived from, rather than trusting the constants
// in quant.go. The bound the encoder states is only as good as these.
//
// For n levels the norm is measured by putting a unit impulse well inside the
// subband of a long signal and summing the magnitude of the reconstruction, so
// no boundary reflection is folded in; the boundaryFold factor covers that
// separately.
func TestSynthesisL1GainsBoundMeasurement(t *testing.T) {
	const n = 1 << 14
	measure := func(levels, band int) float64 {
		d := make([]float64, n)
		dims := n
		for i := 0; i < levels; i++ {
			dims = (dims + 1) / 2
		}
		if band == 0 {
			d[dims/2] = 1
		} else {
			d[dims+dims/2] = 1
		}
		dwt.ReconstructMultiLevel97(d, n, 1, levels)
		s := 0.0
		for _, v := range d {
			s += math.Abs(v)
		}
		return s
	}
	for levels := 1; levels <= 10; levels++ {
		lo := measure(levels, 0)
		hi := measure(levels, 1)
		wantLo := lowSynthL1(levels) / boundaryFold
		wantHi := highSynthL1(levels) / boundaryFold
		if lo > wantLo {
			t.Errorf("lowpass synthesis L1 at %d levels is %.6f, above the bound %.6f the encoder assumes",
				levels, lo, wantLo)
		}
		if hi > wantHi {
			t.Errorf("highpass synthesis L1 at %d levels is %.6f, above the bound %.6f the encoder assumes",
				levels, hi, wantHi)
		}
		// A bound this loose would silently cost bits, so hold it to within a
		// factor of two of the measurement as well.
		if lo*2 < wantLo || hi*2 < wantHi {
			t.Errorf("synthesis L1 bounds at %d levels are more than twice the measurement: lo %.4f vs %.4f, hi %.4f vs %.4f",
				levels, lo, wantLo, hi, wantHi)
		}
	}
}

// TestPackStepSizeRepresentation checks Equation E-3 round trips: the packed
// step never exceeds the requested one, and is never more than one mantissa
// step below it.
func TestPackStepSizeRepresentation(t *testing.T) {
	for _, rb := range []int{1, 8, 9, 10, 16, 18, 32} {
		for e := -20; e <= 4; e++ {
			for _, m := range []float64{1, 1.0005, 1.3, 1.7, 1.99951} {
				want := math.Ldexp(m, e)
				s, ok := codestream.PackStepSize(want, rb)
				if !ok {
					continue
				}
				got := s.Delta(rb)
				if got > want*(1+1e-12) {
					t.Fatalf("rb=%d delta=%g packed to %g, which is larger", rb, want, got)
				}
				if got < want/(1+1.0/2048) {
					t.Fatalf("rb=%d delta=%g packed to %g, more than one mantissa step below", rb, want, got)
				}
				if s.Exponent < 1 || s.Exponent > 31 {
					t.Fatalf("rb=%d delta=%g gave exponent %d, outside the five-bit field", rb, want, s.Exponent)
				}
			}
		}
	}
}

// parseQCD pulls the QCD marker segment out of a raw codestream.
func parseQCD(t *testing.T, cs []byte) (style, guard int, steps []codestream.StepSize) {
	t.Helper()
	i := 2
	for i < len(cs)-3 {
		m := binary.BigEndian.Uint16(cs[i : i+2])
		if m == 0xFF93 || m == 0xFFD9 || m == 0xFF90 {
			break
		}
		l := int(binary.BigEndian.Uint16(cs[i+2 : i+4]))
		seg := cs[i+4 : i+2+l]
		if m == uint16(codestream.QCD) {
			style = int(seg[0] & 0x1F)
			guard = int(seg[0] >> 5)
			switch style {
			case int(codestream.QuantizationNone):
				for _, b := range seg[1:] {
					steps = append(steps, codestream.StepSize{Exponent: b >> 3})
				}
			case int(codestream.QuantizationScalarExpounded):
				for k := 0; k+1 < len(seg)-1; k += 2 {
					v := binary.BigEndian.Uint16(seg[1+k : 3+k])
					steps = append(steps, codestream.StepSize{
						Exponent: uint8(v >> 11),
						Mantissa: v & 0x07FF,
					})
				}
			}
			return
		}
		i += 2 + l
	}
	t.Fatal("no QCD marker in codestream")
	return
}

func lossyCodestream(t *testing.T, size, numRes, quality int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x*13 + y*29 + (x^y)*7) % 256)})
		}
	}
	var buf bytes.Buffer
	err := Encode(&buf, img, &Options{
		Format:         FormatJ2K,
		HighThroughput: true,
		Lossless:       false,
		NumResolutions: numRes,
		Quality:        quality,
	})
	if err != nil {
		t.Fatalf("encoding %dx%d numRes=%d quality=%d: %v", size, size, numRes, quality, err)
	}
	return buf.Bytes()
}

// TestIrreversibleQCDIsScalarExpounded pins the marker A.6.4 requires for a
// 9/7 codestream. The encoder used to declare style 1, scalar derived, which
// OpenJPH rejects outright ("Scalar derived quantization is not supported yet
// in QCD marker", ojph_params.cpp:1926), and to fill the single step-size field
// with (100 - quality) * 256, which is neither an exponent nor a mantissa.
func TestIrreversibleQCDIsScalarExpounded(t *testing.T) {
	for _, numRes := range []int{1, 2, 3, 6} {
		for _, quality := range []int{0, 100, 75, 25} {
			name := fmt.Sprintf("res%d_q%d", numRes, quality)
			t.Run(name, func(t *testing.T) {
				cs := lossyCodestream(t, 40, numRes, quality)
				style, guard, steps := parseQCD(t, cs)
				if style != int(codestream.QuantizationScalarExpounded) {
					t.Fatalf("QCD style is %d, want %d (scalar expounded)",
						style, codestream.QuantizationScalarExpounded)
				}
				wantBands := 3*(numRes-1) + 1
				if len(steps) != wantBands {
					t.Fatalf("QCD carries %d step sizes, want one per subband (%d)", len(steps), wantBands)
				}
				if guard < 1 || guard > 7 {
					t.Fatalf("QCD declares %d guard bits", guard)
				}
				for i, s := range steps {
					if s.Exponent < 1 || s.Exponent > 31 {
						t.Fatalf("subband %d exponent %d is outside the five-bit field", i, s.Exponent)
					}
				}
			})
		}
	}
}

// TestIrreversibleMbCoversIndices checks the constraint that ties the QCD
// exponent to the block coder: a decoder rejects a code-block whose per-quad
// exponent exceeds Mb = guard + exponent - 1, and the HT coder emits an
// exponent one bit wider than the index magnitude because it codes twice the
// magnitude.
func TestIrreversibleMbCoversIndices(t *testing.T) {
	for _, numRes := range []int{1, 3, 6} {
		for _, quality := range []int{0, 100, 50} {
			size := 47
			img := image.NewGray(image.Rect(0, 0, size, size))
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					v := 0
					if (x/3+y/3)%2 == 0 {
						v = 255
					}
					img.SetGray(x, y, color.Gray{Y: uint8(v)})
				}
			}
			e := newEncoder(nil, img, &Options{
				Format:         FormatJ2K,
				HighThroughput: true,
				Lossless:       false,
				NumResolutions: numRes,
				Quality:        quality,
			})
			if err := e.extractImageData(); err != nil {
				t.Fatal(err)
			}
			if err := e.preprocess(); err != nil {
				t.Fatal(err)
			}
			guard, steps := e.quantizationParameters()
			codestream.ForEachSubband(size, size, numRes, func(sb codestream.SubbandRect) {
				maxMag := int32(0)
				for _, data := range e.componentData {
					for y := 0; y < sb.H; y++ {
						row := (sb.Y0 + y) * size
						for x := 0; x < sb.W; x++ {
							v := data[row+sb.X0+x]
							if v < 0 {
								v = -v
							}
							if v > maxMag {
								maxMag = v
							}
						}
					}
				}
				bits := magnitudeBits(float64(maxMag))
				mb := guard + int(steps[sb.Index].Exponent) - 1
				if bits+1 > mb {
					t.Errorf("numRes=%d quality=%d subband %d: indices need %d bits, so the block coder emits an exponent of %d, above Mb=%d",
						numRes, quality, sb.Index, bits, bits+1, mb)
				}
			})
		}
	}
}

// TestIrreversibleRoundTripWithinBound measures the reconstruction error of a
// full encode/decode against the bound the emitted step sizes imply.
//
// The bound is Σ_b Δ_b·A_b: truncating quantization leaves a coefficient error
// below Δ_b, and A_b is how far one unit of error in subband b can move a
// reconstructed sample. Half a count of slack is allowed for the decoder's
// final rounding to an integer sample.
func TestIrreversibleRoundTripWithinBound(t *testing.T) {
	for _, size := range []int{16, 33, 64} {
		for _, numRes := range []int{1, 3, 5} {
			for _, quality := range []int{0, 100, 90, 60} {
				name := fmt.Sprintf("s%d_res%d_q%d", size, numRes, quality)
				t.Run(name, func(t *testing.T) {
					img := image.NewGray(image.Rect(0, 0, size, size))
					want := make([]int, size*size)
					for y := 0; y < size; y++ {
						for x := 0; x < size; x++ {
							v := (x*13 + y*29 + (x^y)*7) % 256
							want[y*size+x] = v
							img.SetGray(x, y, color.Gray{Y: uint8(v)})
						}
					}
					var buf bytes.Buffer
					if err := Encode(&buf, img, &Options{
						Format:         FormatJ2K,
						HighThroughput: true,
						Lossless:       false,
						NumResolutions: numRes,
						Quality:        quality,
					}); err != nil {
						t.Fatal(err)
					}
					_, _, steps := parseQCD(t, buf.Bytes())
					bound := reconstructionBound(steps, numRes, 8)

					got, err := Decode(bytes.NewReader(buf.Bytes()))
					if err != nil {
						t.Fatal(err)
					}
					worst := 0
					for y := 0; y < size; y++ {
						for x := 0; x < size; x++ {
							r, _, _, _ := got.At(x, y).RGBA()
							if d := int(math.Abs(float64(int(r>>8) - want[y*size+x]))); d > worst {
								worst = d
							}
						}
					}
					if float64(worst) > bound+0.5 {
						t.Fatalf("worst sample error %d exceeds the bound %.4f the step sizes imply", worst, bound)
					}
					// Quality 100 sizes the steps for half a count of error,
					// which after rounding has to reproduce the source exactly
					// or be one off.
					if (quality == 100 || quality == 0) && worst > 1 {
						t.Fatalf("worst sample error %d at quality %d, want at most 1", worst, quality)
					}
				})
			}
		}
	}
}

// TestErrorBudgetScaling documents the meaning attached to Options.Quality:
// half a sample count at quality 100 and eight bits of precision, doubling
// every ten points below, and scaling with the sample range.
func TestErrorBudgetScaling(t *testing.T) {
	if got := errorBudget(100, 8); got != 0.5 {
		t.Errorf("errorBudget(100, 8) = %v, want 0.5", got)
	}
	if got := errorBudget(0, 8); got != 0.5 {
		t.Errorf("errorBudget(0, 8) = %v, want 0.5 (unset quality means best)", got)
	}
	if got := errorBudget(90, 8); math.Abs(got-1) > 1e-12 {
		t.Errorf("errorBudget(90, 8) = %v, want 1", got)
	}
	if got := errorBudget(100, 16); got != 128 {
		t.Errorf("errorBudget(100, 16) = %v, want 128", got)
	}
	for q := 1; q < 100; q++ {
		if errorBudget(q, 8) <= errorBudget(q+1, 8) {
			t.Fatalf("error budget is not decreasing in quality at %d", q)
		}
	}
}
