package dwt

import (
	"bufio"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file holds the two checks that can actually catch a non-conformant
// forward 5/3 transform.
//
// A round-trip test cannot. Every wavelet defect this package has carried was
// self-inverse: the forward and inverse passes shared a convention (here, the
// order of the horizontal and vertical passes) that no other implementation
// uses, so Reconstruct(Decompose(x)) == x held exactly while the coefficients
// in between matched nothing. Both checks below compare against something this
// package did not produce:
//
//   TestForward53MatchesSpec         a literal transcription of ISO/IEC
//                                    15444-1 Annex F, written from the
//                                    equations rather than from this code.
//   TestForward53MatchesOpenJPHCoefs coefficients carried in a codestream that
//                                    OpenJPH, the HTJ2K reference encoder,
//                                    produced for the same samples.

// specForward53 is a deliberately naive transcription of ISO/IEC 15444-1
// Annex F.4.8.1 (the 1D_SD procedure) for the reversible 5/3 filter over the
// interval [0, len(x)), using period-symmetric whole-point extension, followed
// by the F.3.5 deinterleave into low-pass then high-pass. It is written for
// obviousness, not speed, and shares no code with Forward53.
func specForward53(x []int32) []int32 {
	n := len(x)
	if n == 1 {
		return []int32{x[0]}
	}
	// Period-symmetric extension about sample 0 and sample n-1.
	at := func(i int) int32 {
		p := 2 * (n - 1)
		i = ((i % p) + p) % p
		if i >= n {
			i = p - i
		}
		return x[i]
	}
	y := make([]int32, n)
	// Step 1: high-pass coefficients at odd positions.
	for i := 1; i < n; i += 2 {
		y[i] = at(i) - floorDiv(at(i-1)+at(i+1), 2)
	}
	// The high-pass array obeys the same whole-point symmetry.
	hp := func(i int) int32 {
		if i < 0 {
			i = -i
		}
		if i > n-1 {
			i = 2*(n-1) - i
		}
		if i >= 0 && i < n && i%2 == 1 {
			return y[i]
		}
		return 0
	}
	// Step 2: low-pass coefficients at even positions.
	for i := 0; i < n; i += 2 {
		y[i] = at(i) + floorDiv(hp(i-1)+hp(i+1)+2, 4)
	}
	out := make([]int32, n)
	half := (n + 1) / 2
	for i, j := 0, 0; i < n; i, j = i+2, j+1 {
		out[j] = y[i]
	}
	for i, j := 1, half; i < n; i, j = i+2, j+1 {
		out[j] = y[i]
	}
	return out
}

// floorDiv rounds toward negative infinity, as Annex F requires.
func floorDiv(a, b int32) int32 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// specDecompose53 is a literal multi-level 2D forward transform: at each level
// the vertical pass (VER_SD) runs over every column of the current LL
// rectangle, then the horizontal pass (HOR_SD) over every row. That order is
// forced by F.3.8.1, which defines the inverse as HOR_SR followed by VER_SR.
func specDecompose53(data []int32, stride, width, height, levels int) []int32 {
	out := append([]int32(nil), data...)
	w, h := width, height
	for level := 0; level < levels; level++ {
		if w < 1 || h < 1 {
			break
		}
		col := make([]int32, h)
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				col[y] = out[y*stride+x]
			}
			var t []int32
			if h >= 2 {
				t = specForward53(col)
			} else {
				t = append([]int32(nil), col...)
			}
			for y := 0; y < h; y++ {
				out[y*stride+x] = t[y]
			}
		}
		row := make([]int32, w)
		for y := 0; y < h; y++ {
			copy(row, out[y*stride:y*stride+w])
			var t []int32
			if w >= 2 {
				t = specForward53(row)
			} else {
				t = append([]int32(nil), row...)
			}
			copy(out[y*stride:y*stride+w], t)
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	return out
}

// TestForward53MatchesSpec pins the 1D lifting steps and the 2D pass order
// against Annex F for every length and rectangle the tile geometry can produce.
func TestForward53MatchesSpec(t *testing.T) {
	rng := rand.New(rand.NewSource(20260818))

	t.Run("1D", func(t *testing.T) {
		for n := 2; n <= 70; n++ {
			for trial := 0; trial < 100; trial++ {
				src := make([]int32, n)
				for i := range src {
					src[i] = int32(rng.Intn(512) - 256)
				}
				want := specForward53(src)
				got := append([]int32(nil), src...)
				Forward53(got, n)
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("Forward53 n=%d index %d: got %d, want %d\nsrc  %v\ngot  %v\nwant %v",
							n, i, got[i], want[i], src, got, want)
					}
				}
				got32 := append([]int32(nil), src...)
				Forward53_32bit(got32, n)
				for i := range want {
					if got32[i] != want[i] {
						t.Fatalf("Forward53_32bit n=%d index %d: got %d, want %d",
							n, i, got32[i], want[i])
					}
				}
			}
		}
	})

	t.Run("2D", func(t *testing.T) {
		sizes := []int{1, 2, 3, 4, 5, 7, 8, 9, 12, 15, 16, 17, 23, 31, 32, 33, 45, 64}
		for _, w := range sizes {
			for _, h := range sizes {
				for levels := 1; levels <= 5; levels++ {
					src := make([]int32, w*h)
					for i := range src {
						src[i] = int32(rng.Intn(512) - 256)
					}
					want := specDecompose53(src, w, w, h, levels)
					got := append([]int32(nil), src...)
					DecomposeMultiLevel53(got, w, h, levels)
					for i := range want {
						if got[i] != want[i] {
							t.Fatalf("DecomposeMultiLevel53 %dx%d levels=%d: index %d (x=%d,y=%d) got %d, want %d",
								w, h, levels, i, i%w, i/w, got[i], want[i])
						}
					}
					got32 := append([]int32(nil), src...)
					DecomposeMultiLevel53_32bit(got32, w, h, levels)
					for i := range want {
						if got32[i] != want[i] {
							t.Fatalf("DecomposeMultiLevel53_32bit %dx%d levels=%d: index %d got %d, want %d",
								w, h, levels, i, got32[i], want[i])
						}
					}
				}
			}
		}
	})
}

// readTestPGM parses the binary grayscale PGM fixtures in testdata.
func readTestPGM(t *testing.T, path string) (int, int, []byte) {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	space := func(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }
	var tok []string
	i := 0
	for len(tok) < 4 {
		if i >= len(d) {
			t.Fatalf("%s: truncated PGM header", path)
		}
		for i < len(d) && space(d[i]) {
			i++
		}
		s := i
		for i < len(d) && !space(d[i]) {
			i++
		}
		tok = append(tok, string(d[s:i]))
	}
	w, _ := strconv.Atoi(tok[1])
	h, _ := strconv.Atoi(tok[2])
	px := d[i+1:]
	if len(px) < w*h {
		t.Fatalf("%s: raster is %d bytes, want %d", path, len(px), w*h)
	}
	return w, h, px[:w*h]
}

// readCoefFixture parses a coefficient fixture: a "COEF <w> <h> <levels>" line
// followed by width*height whitespace-separated values in Mallat order.
func readCoefFixture(t *testing.T, path string) (w, h, levels int, coefs []int32) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	if !sc.Scan() {
		t.Fatalf("%s: empty fixture", path)
	}
	hdr := strings.Fields(sc.Text())
	if len(hdr) != 4 || hdr[0] != "COEF" {
		t.Fatalf("%s: bad header %q", path, sc.Text())
	}
	w, _ = strconv.Atoi(hdr[1])
	h, _ = strconv.Atoi(hdr[2])
	levels, _ = strconv.Atoi(hdr[3])
	if !sc.Scan() {
		t.Fatalf("%s: fixture has no coefficient line", path)
	}
	for _, fld := range strings.Fields(sc.Text()) {
		v, err := strconv.Atoi(fld)
		if err != nil {
			t.Fatalf("%s: bad coefficient %q: %v", path, fld, err)
		}
		coefs = append(coefs, int32(v))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(coefs) != w*h {
		t.Fatalf("%s: %d coefficients, want %d", path, len(coefs), w*h)
	}
	return w, h, levels, coefs
}

// TestForward53MatchesOpenJPHCoefs compares this package's forward transform
// against the wavelet coefficients OpenJPH actually put in a codestream.
//
// Each fixture pair in testdata was produced by encoding the .pgm with
// "ojph_compress -reversible true -num_decomps N", confirming that ojph_expand
// reproduces the .pgm from that codestream bit-exactly (without which the
// measurement would prove nothing), then recovering the coefficient array from
// the codestream's code-blocks. The .coef file is therefore the reference
// encoder's answer, not this library's.
func TestForward53MatchesOpenJPHCoefs(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "ojph_*.coef"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no OpenJPH coefficient fixtures found in testdata")
	}
	for _, path := range fixtures {
		name := strings.TrimSuffix(filepath.Base(path), ".coef")
		t.Run(name, func(t *testing.T) {
			w, h, levels, want := readCoefFixture(t, path)
			pw, ph, px := readTestPGM(t, filepath.Join("testdata", name+".pgm"))
			if pw != w || ph != h {
				t.Fatalf("fixture geometry mismatch: coefs %dx%d, raster %dx%d", w, h, pw, ph)
			}

			// The encoder DC level shifts unsigned 8-bit samples by 2^(8-1)
			// before the wavelet transform (ISO/IEC 15444-1 G.1.2).
			got := make([]int32, w*h)
			for i := range got {
				got[i] = int32(px[i]) - 128
			}
			DecomposeMultiLevel53(got, w, h, levels)

			diff := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					i := y*w + x
					if got[i] == want[i] {
						continue
					}
					if diff < 8 {
						t.Errorf("coefficient (%d,%d): got %d, want %d", x, y, got[i], want[i])
					}
					diff++
				}
			}
			if diff != 0 {
				t.Fatalf("%d of %d coefficients differ from OpenJPH at %d decomposition levels",
					diff, w*h, levels)
			}
		})
	}
}

// TestInverse53UndoesSpecForward checks the inverse pass against the spec-derived
// forward, so that fixing one direction cannot be silently undone by leaving the
// other in the old pass order.
func TestInverse53UndoesSpecForward(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	sizes := []int{2, 3, 5, 8, 9, 16, 17, 23, 32, 33, 45, 64}
	for _, w := range sizes {
		for _, h := range sizes {
			for levels := 1; levels <= 4; levels++ {
				src := make([]int32, w*h)
				for i := range src {
					src[i] = int32(rng.Intn(512) - 256)
				}
				coefs := specDecompose53(src, w, w, h, levels)
				got := append([]int32(nil), coefs...)
				ReconstructMultiLevel53(got, w, h, levels)
				for i := range src {
					if got[i] != src[i] {
						t.Fatalf("ReconstructMultiLevel53 %dx%d levels=%d: index %d got %d, want %d",
							w, h, levels, i, got[i], src[i])
					}
				}
				got32 := append([]int32(nil), coefs...)
				ReconstructMultiLevel53_32bit(got32, w, h, levels)
				for i := range src {
					if got32[i] != src[i] {
						t.Fatalf("ReconstructMultiLevel53_32bit %dx%d levels=%d: index %d got %d, want %d",
							w, h, levels, i, got32[i], src[i])
					}
				}
			}
		}
	}
}
