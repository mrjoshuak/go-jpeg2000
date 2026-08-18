package entropy

import "testing"

// TestHTLastQuadSignificanceSurvives covers a code-block whose only significant
// sample sits in its last quad. Its significance is carried by the final bits
// of a MEL segment two bytes long, and the decoder used to lose them: the
// initial fill applied the "last byte's low nibble reads as ones" rule to the
// byte after the one it belongs to, corrupting the first MEL byte, and then
// stopped instead of feeding the 0xFF padding the run decoder is entitled to.
// The block decoded to all zeros.
//
// A whole-image decode hides this — the block is one of hundreds and its
// coefficient is small — but small subbands make it common: a 12x12 tile with
// two decomposition levels has 3x3 detail bands, and a single significant
// coefficient in one of them is an ordinary thing for a smooth image.
func TestHTLastQuadSignificanceSurvives(t *testing.T) {
	for _, dim := range [][2]int{{3, 3}, {4, 4}, {2, 2}, {5, 7}, {6, 4}, {8, 8}} {
		w, h := dim[0], dim[1]
		for pos := 0; pos < w*h; pos++ {
			for _, v := range []int32{-12, 1, 300} {
				data := make([]int32, w*h)
				data[pos] = v

				enc := GetHTEncoder(w, h)
				enc.SetData(data)
				seg := append([]byte(nil), enc.Encode(BandHH)...)
				PutHTEncoder(enc)

				dec := GetHTDecoder(w, h)
				out := dec.Decode(seg, 2, BandHH)
				got := append([]int32(nil), out...)
				PutHTDecoder(dec)

				for i := range data {
					if got[i] != data[i] {
						t.Fatalf("%dx%d block with %d at index %d: decoded %v, want %v",
							w, h, v, pos, got, data)
					}
				}
			}
		}
	}
}
