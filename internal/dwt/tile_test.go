package dwt

import (
	"math"
	"math/rand"
	"testing"
)

// TestTileMatchesOriginZero checks that the origin-aware transform reproduces
// the origin-zero one exactly at an even origin. The origin-zero transform is
// the one validated against OpenJPH, so agreement here is what licenses using
// the general form for tiles.
func TestTileMatchesOriginZero(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 1; n <= 64; n++ {
		for _, i0 := range []int{0, 2, 8, 100} {
			a := make([]int32, n)
			for i := range a {
				a[i] = int32(rng.Intn(512) - 256)
			}
			b := append([]int32(nil), a...)
			Forward53(a, n)
			Forward53Line(b, n, i0)
			if n < 2 {
				// Forward53 declines lengths below two; the general form
				// returns the sample unchanged at an even coordinate.
				if b[0] != a[0] {
					t.Fatalf("n=%d i0=%d: single sample %d, want %d", n, i0, b[0], a[0])
				}
				continue
			}
			for i := range a {
				if a[i] != b[i] {
					t.Fatalf("n=%d i0=%d: coefficient %d = %d, origin-zero gives %d", n, i0, i, b[i], a[i])
				}
			}
		}
	}
}

func TestTile97MatchesOriginZero(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for n := 2; n <= 64; n++ {
		a := make([]float64, n)
		for i := range a {
			a[i] = float64(rng.Intn(512) - 256)
		}
		b := append([]float64(nil), a...)
		Forward97(a, n)
		Forward97Line(b, n, 4)
		for i := range a {
			if math.Abs(a[i]-b[i]) > 1e-9 {
				t.Fatalf("n=%d: coefficient %d = %g, origin-zero gives %g", n, i, b[i], a[i])
			}
		}
	}
}

// TestTileRoundTrip covers odd origins, which no origin-zero transform can
// express: the split assigns the first sample to the highpass band.
func TestTileRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for n := 1; n <= 40; n++ {
		for i0 := 0; i0 < 8; i0++ {
			a := make([]int32, n)
			for i := range a {
				a[i] = int32(rng.Intn(2001) - 1000)
			}
			want := append([]int32(nil), a...)
			Forward53Line(a, n, i0)
			Inverse53Line(a, n, i0)
			for i := range a {
				if a[i] != want[i] {
					t.Fatalf("n=%d i0=%d: sample %d = %d, want %d", n, i0, i, a[i], want[i])
				}
			}
		}
	}
}

func TestTile97RoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for n := 2; n <= 40; n++ {
		for i0 := 0; i0 < 4; i0++ {
			a := make([]float64, n)
			for i := range a {
				a[i] = float64(rng.Intn(2001) - 1000)
			}
			want := append([]float64(nil), a...)
			Forward97Line(a, n, i0)
			Inverse97Line(a, n, i0)
			for i := range a {
				if math.Abs(a[i]-want[i]) > 1e-6 {
					t.Fatalf("n=%d i0=%d: sample %d = %g, want %g", n, i0, i, a[i], want[i])
				}
			}
		}
	}
}

// TestLowpassCount states the subband size rule the packet geometry depends
// on: the lowpass band holds the even coordinates of the interval.
func TestLowpassCount(t *testing.T) {
	cases := []struct{ i0, i1, want int }{
		{0, 8, 4}, {0, 9, 5}, {1, 9, 4}, {5, 8, 1}, {5, 6, 0}, {4, 5, 1},
	}
	for _, c := range cases {
		if got := LowpassCount(c.i0, c.i1); got != c.want {
			t.Errorf("LowpassCount(%d, %d) = %d, want %d", c.i0, c.i1, got, c.want)
		}
	}
}

// TestMultiLevelTileRoundTrip exercises the 2D multi-level path at origins
// that are odd at several decomposition levels.
func TestMultiLevelTileRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for _, dim := range [][4]int{
		{16, 16, 0, 0}, {12, 12, 20, 20}, {7, 9, 5, 3}, {13, 5, 19, 27}, {1, 1, 3, 3},
	} {
		w, h, x0, y0 := dim[0], dim[1], dim[2], dim[3]
		for levels := 0; levels <= 3; levels++ {
			a := make([]int32, w*h)
			for i := range a {
				a[i] = int32(rng.Intn(511) - 255)
			}
			want := append([]int32(nil), a...)
			DecomposeMultiLevel53Tile(a, w, h, x0, y0, levels)
			ReconstructMultiLevel53Tile(a, w, h, x0, y0, levels)
			for i := range a {
				if a[i] != want[i] {
					t.Fatalf("%dx%d at (%d,%d) levels=%d: sample %d = %d, want %d",
						w, h, x0, y0, levels, i, a[i], want[i])
				}
			}
		}
	}
}

// TestMultiLevelTileMatchesOriginZero pins the tiled 2D transform to the
// origin-zero one for a tile that does start at the origin.
func TestMultiLevelTileMatchesOriginZero(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	for _, dim := range [][2]int{{16, 16}, {17, 9}, {32, 5}, {13, 13}} {
		w, h := dim[0], dim[1]
		a := make([]int32, w*h)
		for i := range a {
			a[i] = int32(rng.Intn(511) - 255)
		}
		b := append([]int32(nil), a...)
		DecomposeMultiLevel53(a, w, h, 2)
		DecomposeMultiLevel53Tile(b, w, h, 0, 0, 2)
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("%dx%d: coefficient %d = %d, origin-zero gives %d", w, h, i, b[i], a[i])
			}
		}
	}
}
