package dwt

import (
	"math"
	"math/rand"
	"testing"
)

// TestWide53MatchesNarrow pins the 64-bit reversible transform to the 32-bit
// one wherever the 32-bit one is valid, so that widening a component changes
// nothing about the coefficients it produces.
func TestWide53MatchesNarrow(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 1; n <= 64; n++ {
		for i0 := 0; i0 < 4; i0++ {
			narrow := make([]int32, n)
			wide := make([]int64, n)
			for i := range narrow {
				v := int32(r.Intn(1 << 20))
				if r.Intn(2) == 0 {
					v = -v
				}
				narrow[i] = v
				wide[i] = int64(v)
			}
			Forward53Line(narrow, n, i0)
			Forward53Line64(wide, n, i0)
			for i := range narrow {
				if int64(narrow[i]) != wide[i] {
					t.Fatalf("n=%d i0=%d sample %d: narrow %d, wide %d", n, i0, i, narrow[i], wide[i])
				}
			}
			Inverse53Line(narrow, n, i0)
			Inverse53Line64(wide, n, i0)
			for i := range narrow {
				if int64(narrow[i]) != wide[i] {
					t.Fatalf("inverse n=%d i0=%d sample %d: narrow %d, wide %d", n, i0, i, narrow[i], wide[i])
				}
			}
		}
	}
}

// TestWide53RoundTripsFullRange checks the transform on the signal the float
// path actually produces: after the NLT Type 3 point transform a binary32
// sample occupies the whole int32 range.
//
// It also pins the reason the wide form has to exist. The 32-bit transform
// round-trips this signal too, because it truncates identically on the way back
// — which is why no round-trip test could ever see the defect — but the
// coefficients it leaves in the codestream are not the ones the standard
// defines, and a decoder that reads the magnitude budget the codestream signals
// reconstructs from the ones that are.
func TestWide53RoundTripsFullRange(t *testing.T) {
	const n = 64
	r := rand.New(rand.NewSource(7))
	src := make([]int32, n)
	for i := range src {
		src[i] = int32(r.Uint32())
	}
	src[0] = math.MinInt32
	src[1] = math.MaxInt32
	src[2] = math.MinInt32

	wide := make([]int64, n)
	narrow := make([]int32, n)
	for i, v := range src {
		wide[i] = int64(v)
		narrow[i] = v
	}

	Forward53Line64(wide, n, 0)
	Forward53Line(narrow, n, 0)

	overflowed, diverged := false, false
	for i := range wide {
		if wide[i] > math.MaxInt32 || wide[i] < math.MinInt32 {
			overflowed = true
		}
		if int64(narrow[i]) != wide[i] {
			diverged = true
		}
	}
	if !overflowed {
		t.Fatal("no coefficient left the int32 range: the fixture does not exercise the case")
	}
	if !diverged {
		t.Fatal("the 32-bit transform agreed with the 64-bit one: the fixture does not exercise the case")
	}

	// The 32-bit form still round-trips, which is exactly why this was invisible.
	Inverse53Line(narrow, n, 0)
	for i := range src {
		if narrow[i] != src[i] {
			t.Fatalf("sample %d: the 32-bit round trip is not self-inverse (%d, want %d)",
				i, narrow[i], src[i])
		}
	}

	Inverse53Line64(wide, n, 0)
	for i := range src {
		if wide[i] != int64(src[i]) {
			t.Fatalf("sample %d: round trip gave %d, want %d", i, wide[i], src[i])
		}
	}
}

// TestWide53TileRoundTrip exercises the multi-level 2D form at tile origins
// whose parity differs, which is where the subband split changes.
func TestWide53TileRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for _, origin := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {13, 7}, {20, 20}} {
		for _, size := range [][2]int{{16, 16}, {17, 13}, {31, 5}} {
			w, h := size[0], size[1]
			src := make([]int64, w*h)
			for i := range src {
				src[i] = int64(int32(r.Uint32()))
			}
			data := append([]int64(nil), src...)
			DecomposeMultiLevel53Tile64(data, w, h, origin[0], origin[1], 3)
			ReconstructMultiLevel53Tile64(data, w, h, origin[0], origin[1], 3)
			for i := range src {
				if data[i] != src[i] {
					t.Fatalf("origin %v size %dx%d sample %d: got %d, want %d",
						origin, w, h, i, data[i], src[i])
				}
			}
		}
	}
}
