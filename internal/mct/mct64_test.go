package mct

import (
	"math"
	"math/rand"
	"testing"
)

// TestWideRCTMatchesNarrow pins the 64-bit reversible colour transform to the
// 32-bit one wherever the 32-bit one is valid.
func TestWideRCTMatchesNarrow(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	const n = 256
	r32 := make([]int32, n)
	g32 := make([]int32, n)
	b32 := make([]int32, n)
	r64 := make([]int64, n)
	g64 := make([]int64, n)
	b64 := make([]int64, n)
	for i := 0; i < n; i++ {
		r32[i] = int32(r.Intn(1 << 20))
		g32[i] = int32(r.Intn(1 << 20))
		b32[i] = int32(r.Intn(1 << 20))
		r64[i], g64[i], b64[i] = int64(r32[i]), int64(g32[i]), int64(b32[i])
	}
	ForwardRCT32(r32, g32, b32)
	ForwardRCT64(r64, g64, b64)
	for i := 0; i < n; i++ {
		if int64(r32[i]) != r64[i] || int64(g32[i]) != g64[i] || int64(b32[i]) != b64[i] {
			t.Fatalf("sample %d: narrow (%d,%d,%d), wide (%d,%d,%d)",
				i, r32[i], g32[i], b32[i], r64[i], g64[i], b64[i])
		}
	}
}

// TestWideRCTRoundTripsFullRange checks the transform on the signal the float
// path produces, and records why the wide form is needed: the chrominance
// differences of two full-range int32 samples do not fit int32, and
// ForwardRCT32 wraps them. That wrap is invisible to a round trip, because
// InverseRCT32 wraps by exactly the same amount.
func TestWideRCTRoundTripsFullRange(t *testing.T) {
	src := [3][]int64{
		{math.MaxInt32, math.MinInt32, math.MaxInt32, 0, math.MinInt32},
		{math.MinInt32, math.MaxInt32, 0, math.MinInt32, math.MaxInt32},
		{0, math.MaxInt32, math.MinInt32, math.MaxInt32, math.MinInt32},
	}
	n := len(src[0])
	wide := [3][]int64{}
	narrow := [3][]int32{}
	for c := 0; c < 3; c++ {
		wide[c] = append([]int64(nil), src[c]...)
		narrow[c] = make([]int32, n)
		for i, v := range src[c] {
			narrow[c][i] = int32(v)
		}
	}

	ForwardRCT64(wide[0], wide[1], wide[2])
	ForwardRCT32(narrow[0], narrow[1], narrow[2])

	overflowed := false
	for c := 0; c < 3; c++ {
		for i := 0; i < n; i++ {
			if wide[c][i] > math.MaxInt32 || wide[c][i] < math.MinInt32 {
				overflowed = true
			}
			if int32(wide[c][i]) != narrow[c][i] {
				t.Fatalf("component %d sample %d: the 32-bit transform is not the 64-bit one truncated (%d vs %d)",
					c, i, narrow[c][i], wide[c][i])
			}
		}
	}
	if !overflowed {
		t.Fatal("no difference left the int32 range: the fixture does not exercise the case")
	}

	InverseRCT64(wide[0], wide[1], wide[2])
	for c := 0; c < 3; c++ {
		for i := 0; i < n; i++ {
			if wide[c][i] != src[c][i] {
				t.Fatalf("component %d sample %d: round trip gave %d, want %d",
					c, i, wide[c][i], src[c][i])
			}
		}
	}
}
