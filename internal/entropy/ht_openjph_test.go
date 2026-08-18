package entropy

import "testing"

// The HT block decoder tested against data this library did not produce.
//
// openjphBlock is the code-block segment from a codestream written by OpenJPH
// (ojph_compress -num_decomps 0 -reversible true) for an 8x8 grayscale image.
// With zero decomposition levels there is no wavelet, so the coefficients are
// exactly the DC level shifted samples and openjphCoefficients is known
// independently of anything in this repository.
//
// The packet header was decoded by hand and is self-consistent: 22 bits, whose
// length field reads 70, matching the observed body length exactly.
//
// This is the test the HT coder never had. Every existing HT test encodes with
// this library and decodes with this library, so any convention the two share
// is invisible to them, however wrong it is.
var openjphBlock = []byte{
	0xd7, 0xe8, 0xde, 0xdb, 0xa8, 0x93, 0x18, 0xbc, 0x69, 0xf5, 0xec, 0x1a,
	0xf6, 0xb2, 0x38, 0x76, 0x75, 0x89, 0xf4, 0xef, 0xe3, 0x6e, 0x72, 0xf8,
	0xa6, 0xd5, 0xf3, 0xcb, 0x2d, 0xfd, 0x2c, 0x0a, 0xf1, 0xf5, 0x15, 0xf5,
	0xf7, 0xd8, 0x25, 0xc3, 0xac, 0x35, 0x73, 0xf2, 0xcf, 0x97, 0xbf, 0x4b,
	0x11, 0xaf, 0x37, 0x02, 0xf3, 0x00, 0x42, 0x30, 0x5e, 0x11, 0xd1, 0x2e,
	0x06, 0x89, 0x76, 0x4a, 0xa6, 0x00, 0x00, 0x31, 0xb1, 0x01,
}

var openjphCoefficients = []int32{
	-108, -95, -82, -69, -56, -43, -30, -17,
	-105, -92, -79, -66, -53, -40, -27, -14,
	-102, -89, -76, -63, -50, -37, -24, -11,
	-99, -86, -73, -60, -47, -34, -21, -8,
	-96, -83, -70, -57, -44, -31, -18, -5,
	-93, -80, -67, -54, -41, -28, -15, -2,
	-90, -77, -64, -51, -38, -25, -12, 1,
	-87, -74, -61, -48, -35, -22, -9, 4,
}

func TestHTDecodeOpenJPHBlock(t *testing.T) {
	// p = cblk->numbps = Mb + 1 - zero_bplanes. The QCD marker gives
	// Mb = guard(1) + exponent(7) - 1 = 7, and the packet header's IMSB tag
	// tree gives 6 zero bitplanes, so p = 7 + 1 - 6 = 2.
	const mb = 2

	dec := NewHTDecoder(8, 8)
	got := dec.Decode(openjphBlock, mb, BandLL)

	if len(got) != len(openjphCoefficients) {
		t.Fatalf("length: got %d, want %d", len(got), len(openjphCoefficients))
	}

	wrong := 0
	firstAt, firstGot, firstWant := -1, int32(0), int32(0)
	for i := range openjphCoefficients {
		if got[i] != openjphCoefficients[i] {
			if wrong == 0 {
				firstAt, firstGot, firstWant = i, got[i], openjphCoefficients[i]
			}
			wrong++
		}
	}
	if wrong != 0 {
		t.Errorf("%d/%d coefficients wrong (%.1f%%); first at %d: got %d, want %d",
			wrong, len(openjphCoefficients),
			100*float64(wrong)/float64(len(openjphCoefficients)),
			firstAt, firstGot, firstWant)
	}
}

// TestHTEncodeMatchesOpenJPH encodes the coefficients that OpenJPH encoded and
// requires the same bytes back.
//
// This is a far sharper test than a round-trip: the HT cleanup pass is a
// deterministic algorithm, so a conforming encoder given these coefficients has
// exactly one correct output, and openjphBlock is it. A round-trip through our
// own decoder cannot distinguish a correct encoder from one whose deviations
// our decoder happens to mirror.
func TestHTEncodeMatchesOpenJPH(t *testing.T) {
	got := encodeCleanupHT(openjphCoefficients, 8, 8, 0)
	if got == nil {
		t.Fatal("encoder returned nil for non-zero data")
	}
	if len(got) != len(openjphBlock) {
		t.Errorf("length: got %d bytes, want %d", len(got), len(openjphBlock))
	}
	n := len(got)
	if len(openjphBlock) < n {
		n = len(openjphBlock)
	}
	diff, firstAt := 0, -1
	for i := 0; i < n; i++ {
		if got[i] != openjphBlock[i] {
			if firstAt < 0 {
				firstAt = i
			}
			diff++
		}
	}
	if diff != 0 || len(got) != len(openjphBlock) {
		t.Errorf("%d/%d bytes differ, first at %d", diff, n, firstAt)
		if firstAt >= 0 {
			lo := firstAt - 4
			if lo < 0 {
				lo = 0
			}
			hi := firstAt + 8
			if hi > n {
				hi = n
			}
			t.Logf("got  [%d:%d] % x", lo, hi, got[lo:hi])
			t.Logf("want [%d:%d] % x", lo, hi, openjphBlock[lo:hi])
		}
	}
}
