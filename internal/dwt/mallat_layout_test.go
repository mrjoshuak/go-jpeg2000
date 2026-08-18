package dwt

import "testing"

// The multi-level decomposition helpers halve the width at every level and
// pass that halved width to the single-level 2D transform, which uses its
// width argument as the row stride. In a JPEG 2000 Mallat layout the LL
// subband of level N is the top-left sub-rectangle of the FULL-width array,
// so from the second level onward a halved stride addresses the wrong memory.
//
// A forward/inverse round trip cannot detect this because both directions make
// the same mistake. These tests compare against a reference decomposition that
// copies the LL sub-rectangle out into a compact buffer (where the compact
// width really is the stride), transforms it, and copies it back.

// llRect copies the top-left w x h sub-rectangle out of a stride-wide array.
func llRect(data []int32, stride, w, h int) []int32 {
	out := make([]int32, w*h)
	for y := 0; y < h; y++ {
		copy(out[y*w:(y+1)*w], data[y*stride:y*stride+w])
	}
	return out
}

func putLLRect(data []int32, stride int, sub []int32, w, h int) {
	for y := 0; y < h; y++ {
		copy(data[y*stride:y*stride+w], sub[y*w:(y+1)*w])
	}
}

func llRectF(data []float64, stride, w, h int) []float64 {
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		copy(out[y*w:(y+1)*w], data[y*stride:y*stride+w])
	}
	return out
}

func putLLRectF(data []float64, stride int, sub []float64, w, h int) {
	for y := 0; y < h; y++ {
		copy(data[y*stride:y*stride+w], sub[y*w:(y+1)*w])
	}
}

func testImage(w, h int) []int32 {
	d := make([]int32, w*h)
	for i := range d {
		d[i] = int32((i*37)%251) - 125
	}
	return d
}

func countDiff(got, want []int32) (int, int) {
	wrong, first := 0, -1
	for i := range want {
		if got[i] != want[i] {
			if first < 0 {
				first = i
			}
			wrong++
		}
	}
	return wrong, first
}

// referenceDecompose53_32bit applies the single-level transform to the correct
// sub-rectangle at every level, using a compact copy so the stride is right.
func referenceDecompose53_32bit(data []int32, width, height, levels int) {
	w, h := width, height
	for level := 0; level < levels; level++ {
		if level == 0 {
			Forward2D53_32bit(data, w, h)
		} else {
			sub := llRect(data, width, w, h)
			Forward2D53_32bit(sub, w, h)
			putLLRect(data, width, sub, w, h)
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
}

func referenceDecompose53(data []int32, width, height, levels int) {
	w, h := width, height
	for level := 0; level < levels; level++ {
		if level == 0 {
			Forward2D53(data, w, h)
		} else {
			sub := llRect(data, width, w, h)
			Forward2D53(sub, w, h)
			putLLRect(data, width, sub, w, h)
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
}

func referenceDecompose97(data []float64, width, height, levels int) {
	w, h := width, height
	for level := 0; level < levels; level++ {
		if level == 0 {
			Forward2D97(data, w, h)
		} else {
			sub := llRectF(data, width, w, h)
			Forward2D97(sub, w, h)
			putLLRectF(data, width, sub, w, h)
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
}

var mallatSizes = []struct{ w, h int }{
	{8, 8}, {16, 16}, {32, 32}, {32, 16}, {12, 10}, {64, 64},
}

func TestDecomposeMultiLevel53_32bitUsesFullWidthStride(t *testing.T) {
	for _, s := range mallatSizes {
		for _, levels := range []int{2, 3} {
			if s.w>>uint(levels) < 1 || s.h>>uint(levels) < 1 {
				continue
			}
			got := testImage(s.w, s.h)
			want := append([]int32(nil), got...)

			DecomposeMultiLevel53_32bit(got, s.w, s.h, levels)
			referenceDecompose53_32bit(want, s.w, s.h, levels)

			if wrong, first := countDiff(got, want); wrong != 0 {
				t.Errorf("%dx%d levels=%d: %d/%d coefficients differ from the "+
					"stride-correct reference; first at %d (row %d col %d): got %d, want %d",
					s.w, s.h, levels, wrong, len(want), first, first/s.w, first%s.w,
					got[first], want[first])
			}
		}
	}
}

func TestDecomposeMultiLevel53UsesFullWidthStride(t *testing.T) {
	for _, s := range mallatSizes {
		for _, levels := range []int{2, 3} {
			if s.w>>uint(levels) < 1 || s.h>>uint(levels) < 1 {
				continue
			}
			got := testImage(s.w, s.h)
			want := append([]int32(nil), got...)

			DecomposeMultiLevel53(got, s.w, s.h, levels)
			referenceDecompose53(want, s.w, s.h, levels)

			if wrong, first := countDiff(got, want); wrong != 0 {
				t.Errorf("%dx%d levels=%d: %d/%d coefficients differ from the "+
					"stride-correct reference; first at %d: got %d, want %d",
					s.w, s.h, levels, wrong, len(want), first, got[first], want[first])
			}
		}
	}
}

func TestDecomposeMultiLevel97UsesFullWidthStride(t *testing.T) {
	for _, s := range mallatSizes {
		for _, levels := range []int{2, 3} {
			if s.w>>uint(levels) < 1 || s.h>>uint(levels) < 1 {
				continue
			}
			src := testImage(s.w, s.h)
			got := make([]float64, len(src))
			want := make([]float64, len(src))
			for i, v := range src {
				got[i] = float64(v)
				want[i] = float64(v)
			}

			DecomposeMultiLevel97(got, s.w, s.h, levels)
			referenceDecompose97(want, s.w, s.h, levels)

			wrong, first := 0, -1
			for i := range want {
				if diff := got[i] - want[i]; diff > 1e-9 || diff < -1e-9 {
					if first < 0 {
						first = i
					}
					wrong++
				}
			}
			if wrong != 0 {
				t.Errorf("%dx%d levels=%d: %d/%d coefficients differ from the "+
					"stride-correct reference; first at %d: got %g, want %g",
					s.w, s.h, levels, wrong, len(want), first, got[first], want[first])
			}
		}
	}
}

// TestLLOfLLIsLLAfterTwoLevels states the same property structurally: the LL
// subband after two levels of decomposition must equal a single-level
// decomposition applied to the LL subband produced by the first level.
func TestLLOfLLIsLLAfterTwoLevels(t *testing.T) {
	const w, h = 32, 32
	const w1, h1 = (w + 1) / 2, (h + 1) / 2
	const w2, h2 = (w1 + 1) / 2, (h1 + 1) / 2

	two := testImage(w, h)
	DecomposeMultiLevel53_32bit(two, w, h, 2)

	one := testImage(w, h)
	DecomposeMultiLevel53_32bit(one, w, h, 1)
	sub := llRect(one, w, w1, h1) // LL of level 1, compactly packed
	Forward2D53_32bit(sub, w1, h1)

	wrong, first := 0, -1
	for y := 0; y < h2; y++ {
		for x := 0; x < w2; x++ {
			if two[y*w+x] != sub[y*w1+x] {
				if first < 0 {
					first = y*w1 + x
				}
				wrong++
			}
		}
	}
	if wrong != 0 {
		t.Errorf("LL after 2 levels differs from LL-of-LL in %d/%d samples (first at index %d: got %d, want %d)",
			wrong, w2*h2, first, two[(first/w1)*w+first%w1], sub[first])
	}
}

// TestMultiLevelRoundTripStillExact guards the property the existing round-trip
// test covers, at the deeper level counts the stride bug affects.
func TestMultiLevelRoundTripStillExact(t *testing.T) {
	for _, s := range mallatSizes {
		for _, levels := range []int{1, 2, 3} {
			if s.w>>uint(levels) < 1 || s.h>>uint(levels) < 1 {
				continue
			}
			orig := testImage(s.w, s.h)
			data := append([]int32(nil), orig...)
			DecomposeMultiLevel53_32bit(data, s.w, s.h, levels)
			ReconstructMultiLevel53_32bit(data, s.w, s.h, levels)
			if wrong, first := countDiff(data, orig); wrong != 0 {
				t.Errorf("53_32bit %dx%d levels=%d: %d samples wrong, first at %d",
					s.w, s.h, levels, wrong, first)
			}

			data = append([]int32(nil), orig...)
			DecomposeMultiLevel53(data, s.w, s.h, levels)
			ReconstructMultiLevel53(data, s.w, s.h, levels)
			if wrong, first := countDiff(data, orig); wrong != 0 {
				t.Errorf("53 %dx%d levels=%d: %d samples wrong, first at %d",
					s.w, s.h, levels, wrong, first)
			}

			f := make([]float64, len(orig))
			for i, v := range orig {
				f[i] = float64(v)
			}
			DecomposeMultiLevel97(f, s.w, s.h, levels)
			ReconstructMultiLevel97(f, s.w, s.h, levels)
			maxErr := 0.0
			for i := range orig {
				if d := f[i] - float64(orig[i]); d > maxErr {
					maxErr = d
				} else if -d > maxErr {
					maxErr = -d
				}
			}
			if maxErr > 1e-6 {
				t.Errorf("97 %dx%d levels=%d: max round-trip error %g", s.w, s.h, levels, maxErr)
			}
		}
	}
}
