package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// sixteenBitProbes are the sample values the upper half of a 16-bit range is
// tested with. The decoder used to rescale every 16-bit sample as v*65535/max
// in int32, which wraps for any v above 32768, so the interesting values are
// the ones either side of 2^15 and the top of the range. The low ones are
// there so a fix that only works above the midpoint is caught too.
var sixteenBitProbes = []uint16{0, 1, 255, 256, 32767, 32768, 32769, 40000, 49152, 65534, 65535}

// sixteenBitPlane fills w*h samples with the probes followed by a ramp that
// walks the whole 16-bit range, so every high bit pattern has company.
func sixteenBitPlane(w, h int, seed uint32) []uint16 {
	out := make([]uint16, w*h)
	for i := range out {
		if i < len(sixteenBitProbes) {
			out[i] = sixteenBitProbes[i]
			continue
		}
		out[i] = uint16(uint32(i)*2654435761 + seed)
	}
	return out
}

// TestDecode16BitUpperHalf encodes 16-bit samples losslessly and checks that
// Decode returns them exactly, including every value at or above 32768.
//
// The pre-existing 16-bit round-trip tests check only the decoded dimensions,
// so a decoder that returned every sample above 2^15 wrong passed all of them. The codestream itself was right:
// OpenJPH decodes the same bytes exactly.
func TestDecode16BitUpperHalf(t *testing.T) {
	const w, h = 24, 16
	for _, ht := range []bool{false, true} {
		name := "Part1"
		if ht {
			name = "HT"
		}
		t.Run("Gray16/"+name, func(t *testing.T) {
			src := image.NewGray16(image.Rect(0, 0, w, h))
			plane := sixteenBitPlane(w, h, 0)
			for i, v := range plane {
				src.SetGray16(i%w, i/w, color.Gray16{Y: v})
			}
			got := roundTrip16(t, src, ht)
			g, ok := got.(*image.Gray16)
			if !ok {
				t.Fatalf("decoded %T, want *image.Gray16", got)
			}
			bad := 0
			for i, want := range plane {
				if v := g.Gray16At(i%w, i/w).Y; v != want {
					if bad < 5 {
						t.Errorf("sample %d: got %d, want %d", i, v, want)
					}
					bad++
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d samples wrong", bad, len(plane))
			}
		})
		t.Run("RGBA64/"+name, func(t *testing.T) {
			src := image.NewRGBA64(image.Rect(0, 0, w, h))
			planes := [4][]uint16{
				sixteenBitPlane(w, h, 0), sixteenBitPlane(w, h, 1),
				sixteenBitPlane(w, h, 2), sixteenBitPlane(w, h, 3),
			}
			for i := 0; i < w*h; i++ {
				src.SetRGBA64(i%w, i/w, color.RGBA64{
					R: planes[0][i], G: planes[1][i], B: planes[2][i], A: planes[3][i],
				})
			}
			got := roundTrip16(t, src, ht)
			g, ok := got.(*image.RGBA64)
			if !ok {
				t.Fatalf("decoded %T, want *image.RGBA64", got)
			}
			bad := 0
			for i := 0; i < w*h; i++ {
				c := g.RGBA64At(i%w, i/w)
				for k, v := range [4]uint16{c.R, c.G, c.B, c.A} {
					if v != planes[k][i] {
						if bad < 5 {
							t.Errorf("sample %d component %d: got %d, want %d", i, k, v, planes[k][i])
						}
						bad++
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d samples wrong", bad, 4*w*h)
			}
		})
	}
}

func roundTrip16(t *testing.T, src image.Image, ht bool) image.Image {
	t.Helper()
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.NumResolutions = 4
	opts.HighThroughput = ht
	var buf bytes.Buffer
	if err := Encode(&buf, src, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return got
}

// TestCreateImage16BitScaling drives the sample-to-image conversion directly,
// for every component count it handles at 16 bits, with the probes as
// reconstructed samples. The three-component 16-bit branch has no encoder
// input that reaches it (the encoder writes RGBA64 as four components), so an
// end-to-end test cannot cover it.
//
// Precisions below 16 are rescaled to the full 16-bit range; the expected
// value is computed in 64 bits, independently of the code under test.
func TestCreateImage16BitScaling(t *testing.T) {
	for _, prec := range []int{9, 12, 15, 16} {
		maxVal := int64(1)<<prec - 1
		var samples []int32
		for _, p := range sixteenBitProbes {
			if int64(p) <= maxVal {
				samples = append(samples, int32(p))
			}
		}
		samples = append(samples, int32(maxVal))
		want := func(v int32) uint16 { return uint16(int64(v) * 65535 / maxVal) }

		for _, numComp := range []int{1, 3, 4} {
			planes := make([][]int32, numComp)
			for c := range planes {
				planes[c] = samples
			}
			d := &decoder{}
			img, err := d.createImage(planes, len(samples), 1, numComp, prec, false)
			if err != nil {
				t.Fatalf("prec %d, %d components: %v", prec, numComp, err)
			}
			for x, v := range samples {
				var got []uint16
				switch m := img.(type) {
				case *image.Gray16:
					got = []uint16{m.Gray16At(x, 0).Y}
				case *image.RGBA64:
					c := m.RGBA64At(x, 0)
					got = []uint16{c.R, c.G, c.B}
					if numComp == 4 {
						got = append(got, c.A)
					}
				default:
					t.Fatalf("prec %d, %d components: decoded %T", prec, numComp, img)
				}
				for k, g := range got {
					if g != want(v) {
						t.Errorf("prec %d, %d components, sample %d component %d: got %d, want %d",
							prec, numComp, v, k, g, want(v))
					}
				}
			}
		}
	}
}
