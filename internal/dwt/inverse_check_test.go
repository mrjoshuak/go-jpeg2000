package dwt

import "testing"

// TestInverse53IsInverseOfForward checks the property the pair must have.
//
// The forward transform is known correct from the outside: a codestream this
// library encodes is decoded exactly by OpenJPH. So if a round trip through
// forward then inverse does not return the original samples, the inverse is
// the one at fault.
func TestInverse53IsInverseOfForward(t *testing.T) {
	sizes := []struct{ w, h int }{
		{8, 8}, {16, 16}, {8, 4}, {7, 5}, {32, 32},
	}
	for _, s := range sizes {
		for _, levels := range []int{1, 2} {
			if s.w>>uint(levels) < 1 || s.h>>uint(levels) < 1 {
				continue
			}
			orig := make([]int32, s.w*s.h)
			for i := range orig {
				orig[i] = int32((i*13)%211) - 105
			}
			data := append([]int32(nil), orig...)

			DecomposeMultiLevel53_32bit(data, s.w, s.h, levels)
			ReconstructMultiLevel53_32bit(data, s.w, s.h, levels)

			wrong, firstAt := 0, -1
			for i := range orig {
				if data[i] != orig[i] {
					if firstAt < 0 {
						firstAt = i
					}
					wrong++
				}
			}
			if wrong != 0 {
				t.Errorf("%dx%d levels=%d: %d/%d samples wrong; first at %d: got %d, want %d",
					s.w, s.h, levels, wrong, len(orig), firstAt, data[firstAt], orig[firstAt])
			}
		}
	}
}
