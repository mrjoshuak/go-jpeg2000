package jpeg2000

import (
	"bytes"
	"testing"
)

// TestCodeBlockSizeSignalledMatchesEncoding checks that the code-block size
// written into the COD marker is the one the encoder actually partitioned
// with. When they disagreed, every image whose subbands were larger than one
// code-block decoded to garbage while small images still round-tripped, so
// this uses an image large enough to need several code-blocks.
func TestCodeBlockSizeSignalledMatchesEncoding(t *testing.T) {
	const width, height = 256, 256
	plane := make([]uint16, width*height)
	for i := range plane {
		plane[i] = uint16(i * 7919)
	}

	cases := []struct {
		name    string
		opts    *Options
		wantXcb int
		wantYcb int
	}{
		{"default", &Options{Format: FormatJ2K, Lossless: true, NumResolutions: 6}, 6, 6},
		{"ht 32", &Options{Format: FormatJ2K, Lossless: true, HighThroughput: true, HTBlockWidth: 32, HTBlockHeight: 32, NumResolutions: 6}, 5, 5},
		// 128x128 exceeds the 4096-sample code-block limit and is reduced.
		{"ht 128", &Options{Format: FormatJ2K, Lossless: true, HighThroughput: true, HTBlockWidth: 128, HTBlockHeight: 128, NumResolutions: 6}, 6, 6},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := EncodeHalf(&buf, &HalfImage{Width: width, Height: height, Components: [][]uint16{plane}}, tc.opts); err != nil {
				t.Fatalf("EncodeHalf: %v", err)
			}

			xcb, ycb := codeBlockExponentsFromCOD(t, buf.Bytes())
			if xcb != tc.wantXcb || ycb != tc.wantYcb {
				t.Fatalf("COD code-block exponents = %d,%d, want %d,%d", xcb, ycb, tc.wantXcb, tc.wantYcb)
			}
			if xcb+ycb > 12 || xcb < 2 || ycb < 2 || xcb > 10 || ycb > 10 {
				t.Fatalf("COD code-block exponents %d,%d are outside the legal range", xcb, ycb)
			}

			out, err := DecodeHalf(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("DecodeHalf: %v", err)
			}
			for i, want := range plane {
				if out.Components[0][i] != want {
					t.Fatalf("sample %d: got 0x%04X, want 0x%04X", i, out.Components[0][i], want)
				}
			}
		})
	}
}

// codeBlockExponentsFromCOD finds the COD marker in a raw codestream and
// returns the code-block width and height exponents it declares.
func codeBlockExponentsFromCOD(t *testing.T, cs []byte) (int, int) {
	t.Helper()
	for i := 0; i+2 < len(cs); i++ {
		if cs[i] != 0xFF || cs[i+1] != 0x52 { // COD
			continue
		}
		// COD: marker(2) Lcod(2) Scod(1) SGcod(4), then SPcod, whose first
		// byte is the decomposition count and whose next two are the
		// code-block exponents minus two.
		spcod := i + 2 + 2 + 1 + 4
		if spcod+3 > len(cs) {
			break
		}
		return int(cs[spcod+1]) + 2, int(cs[spcod+2]) + 2
	}
	t.Fatal("no COD marker in codestream")
	return 0, 0
}

// TestNumResolutionsHonoured checks that the encoder applies exactly the
// number of decomposition levels it declares. A single resolution level (no
// decomposition at all) used to fall back to five levels while still
// declaring zero, which decoded to garbage.
func TestNumResolutionsHonoured(t *testing.T) {
	const width, height = 40, 24
	plane := make([]uint16, width*height)
	for i := range plane {
		plane[i] = uint16(i * 7919)
	}

	for numRes := 1; numRes <= 6; numRes++ {
		var buf bytes.Buffer
		opts := &Options{Format: FormatJ2K, Lossless: true, NumResolutions: numRes}
		if err := EncodeHalf(&buf, &HalfImage{Width: width, Height: height, Components: [][]uint16{plane}}, opts); err != nil {
			t.Fatalf("numRes %d: EncodeHalf: %v", numRes, err)
		}
		meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("numRes %d: DecodeMetadata: %v", numRes, err)
		}
		if meta.NumResolutions != numRes {
			t.Fatalf("numRes %d: codestream declares %d", numRes, meta.NumResolutions)
		}
		out, err := DecodeHalf(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("numRes %d: DecodeHalf: %v", numRes, err)
		}
		for i, want := range plane {
			if out.Components[0][i] != want {
				t.Fatalf("numRes %d: sample %d: got 0x%04X, want 0x%04X", numRes, i, out.Components[0][i], want)
			}
		}
	}
}
