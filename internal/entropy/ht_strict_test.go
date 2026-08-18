package entropy

import "testing"

// TestHTCleanupIsExact pins the property the HT block coder actually has to
// satisfy: the cleanup pass codes every magnitude bit down to the coded
// bitplane, so a decode of an encode must return the coefficients unchanged.
//
// The pre-existing TestHTEncoderDecoder asserts only that signs survive,
// justified by a comment stating the cleanup pass is lossy. It is not. That
// assertion cannot distinguish a correct coder from one that reproduces sign
// and nothing else, which is why a non-conforming block coder sat here green.
func TestHTCleanupIsExact(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		bandType      int
	}{
		{"4x4_LL", 4, 4, 0},
		{"8x8_LL", 8, 8, 0},
		{"16x16_LL", 16, 16, 0},
		{"32x32_LL", 32, 32, 0},
		{"32x32_HH", 32, 32, 3},
		{"64x64_LL", 64, 64, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]int32, tc.width*tc.height)
			for i := range data {
				v := int32((i*37)%211 - 105)
				if i%7 == 0 {
					v = -v
				}
				data[i] = v
			}

			maxMag := int32(0)
			for _, v := range data {
				if v < 0 {
					v = -v
				}
				if v > maxMag {
					maxMag = v
				}
			}
			numBitplanes := 0
			for m := maxMag; m > 0; m >>= 1 {
				numBitplanes++
			}

			enc := NewHTEncoder(tc.width, tc.height)
			enc.SetData(data)
			encoded := enc.Encode(tc.bandType)
			if encoded == nil {
				t.Fatal("Encode returned nil for non-zero data")
			}

			dec := NewHTDecoder(tc.width, tc.height)
			decoded := dec.Decode(encoded, numBitplanes, tc.bandType)
			if len(decoded) != len(data) {
				t.Fatalf("length: got %d, want %d", len(decoded), len(data))
			}

			mismatch := 0
			firstAt, firstGot, firstWant := -1, int32(0), int32(0)
			for i := range data {
				if decoded[i] != data[i] {
					if mismatch == 0 {
						firstAt, firstGot, firstWant = i, decoded[i], data[i]
					}
					mismatch++
				}
			}
			// Report the count, not just that something differed: a coder that
			// is one coefficient off and one that recovers nothing are very
			// different defects and the number is what tells them apart.
			if mismatch != 0 {
				t.Errorf("%d/%d coefficients wrong (%.1f%%); first at index %d: got %d, want %d",
					mismatch, len(data), 100*float64(mismatch)/float64(len(data)),
					firstAt, firstGot, firstWant)
			}
		})
	}
}
