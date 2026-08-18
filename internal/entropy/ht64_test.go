package entropy

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestWideAgreesWithNarrow pins the two instantiations of the cleanup-pass
// encoder together: widening a code-block must not change a byte of the
// segment for coefficients the narrow word can hold.
func TestWideAgreesWithNarrow(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for _, dim := range [][2]int{{4, 4}, {8, 8}, {16, 16}, {7, 5}, {32, 3}} {
		w, h := dim[0], dim[1]
		narrow := make([]int32, w*h)
		wide := make([]int64, w*h)
		for i := range narrow {
			v := int32(r.Intn(1 << 18))
			if r.Intn(3) == 0 {
				v = -v
			}
			if r.Intn(4) == 0 {
				v = 0
			}
			narrow[i] = v
			wide[i] = int64(v)
		}
		a := encodeCleanupHT(narrow, w, h, 0)
		b := encodeCleanupHT(wide, w, h, 0)
		if !bytes.Equal(a, b) {
			t.Fatalf("%dx%d: the 32-bit and 64-bit encoders produced different segments (%d vs %d bytes)",
				w, h, len(a), len(b))
		}
	}
}

// TestWideBlockRoundTrip runs coefficients that need more than 32 bits through
// the block coder and back.
//
// The magnitudes here are what a binary32 component produces after the NLT Type
// 3 point transform and one or more reversible 5/3 levels: 33 to 35 magnitude
// bits. Nothing narrower reaches the code paths this exercises — the 64-bit
// MagSgn fetch and the four-bit u-VLC extension both only trigger above 32.
func TestWideBlockRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for _, shift := range []uint{1, 8, 20, 31, 32, 33, 34, 35, 36} {
		for _, dim := range [][2]int{{4, 4}, {8, 8}, {16, 16}, {13, 9}, {64, 64}, {5, 61}} {
			w, h := dim[0], dim[1]
			src := make([]int64, w*h)
			for i := range src {
				v := int64(r.Uint64() & (1<<shift - 1))
				if r.Intn(3) == 0 {
					v = -v
				}
				if r.Intn(5) == 0 {
					v = 0
				}
				src[i] = v
			}
			// Guarantee the extreme is present rather than hoping for it.
			src[0] = 1<<shift - 1
			if len(src) > 1 {
				src[1] = -(1<<shift - 1)
			}

			seg := EncodeCleanup64(src, w, h)
			if seg == nil {
				t.Fatalf("shift %d %dx%d: encoder produced no segment", shift, w, h)
			}
			dec := NewHTDecoder(w, h)
			got := dec.Decode64(seg, NumBitPlanes64(src), BandLL)
			for i := range src {
				if got[i] != src[i] {
					t.Fatalf("shift %d %dx%d sample %d: got %d, want %d",
						shift, w, h, i, got[i], src[i])
				}
			}
		}
	}
}

// TestWideBlockUVLCExtension drives the u-VLC extension specifically.
//
// u above 32 is coded as a prefix, a saturated five-bit suffix and a four-bit
// extension. The encoder used to omit the extension, so such a quad decoded
// with the wrong number of magnitude bits and the MagSgn stream desynchronised
// from that quad onward — every sample after it in the code-block came back
// wrong. A quad reaches u > 32 only when its magnitude exponent does, which
// takes a coefficient of at least 2^32.
func TestWideBlockUVLCExtension(t *testing.T) {
	const w, h = 8, 8
	src := make([]int64, w*h)
	for i := range src {
		// Alternate a maximal-exponent coefficient with a tiny one, so every
		// quad has both a large u and a neighbour whose magnitude bits would
		// be misplaced if u were decoded wrong.
		if i%2 == 0 {
			src[i] = int64(1)<<35 - 1
			if i%4 == 0 {
				src[i] = -src[i]
			}
		} else {
			src[i] = int64(i%7) - 3
		}
	}
	seg := EncodeCleanup64(src, w, h)
	if seg == nil {
		t.Fatal("encoder produced no segment")
	}
	dec := NewHTDecoder(w, h)
	got := dec.Decode64(seg, NumBitPlanes64(src), BandHH)
	for i := range src {
		if got[i] != src[i] {
			t.Fatalf("sample %d: got %d, want %d", i, got[i], src[i])
		}
	}
}

// TestNumBitPlanes64 covers the magnitude bit count, including the extreme
// negative value whose negation is not representable.
func TestNumBitPlanes64(t *testing.T) {
	cases := []struct {
		in   []int64
		want int
	}{
		{[]int64{0, 0}, 0},
		{[]int64{1}, 1},
		{[]int64{-1}, 1},
		{[]int64{1 << 32}, 33},
		{[]int64{-(1 << 34)}, 35},
		{[]int64{3, -9, 5}, 4},
	}
	for _, c := range cases {
		if got := NumBitPlanes64(c.in); got != c.want {
			t.Errorf("NumBitPlanes64(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
