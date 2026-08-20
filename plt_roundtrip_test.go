package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// TestPacketLengthMarkersRoundTrip is the check that was missing.
//
// PLT and TLM were gated against OpenJPEG — opj_dump parsed them and
// opj_decompress produced the right pixels — and never against this library's
// own decoder. It could not read them: indexTileParts required SOD immediately
// after SOT, which is true only of an empty tile-part header, so a codestream
// carrying PLT had every tile indexed as absent and decoded to DC-shifted
// nothing. 99.6% of samples wrong, while the reference read the same bytes
// correctly.
//
// An external oracle cannot see a defect that lives only on this side of the
// round trip. That is what this test is for, and it is why it asserts against
// the source image rather than against a decode of a PLT-free codestream —
// the two could agree on being wrong.
func TestPacketLengthMarkersRoundTrip(t *testing.T) {
	const w, h = 256, 256
	src := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8((x*13 + y*7) % 251)})
		}
	}

	// Every combination that puts markers in a tile-part header: PLT alone,
	// and PLT beside a precinct partition, which is the pairing a viewport
	// index actually wants.
	cases := []struct {
		name     string
		precinct []PrecinctSize
	}{
		{"plt", nil},
		{"plt with precincts", []PrecinctSize{{5, 5}, {5, 5}, {5, 5}, {5, 5}, {5, 5}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, src, &Options{
				Lossless: true, Format: FormatJ2K, NumResolutions: 5,
				WritePacketLengths: true,
				PrecinctSizes:      c.precinct,
			}); err != nil {
				t.Fatalf("Encode: %v", err)
			}

			got, err := Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			b := got.Bounds()
			if b.Dx() != w || b.Dy() != h {
				t.Fatalf("decoded %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
			}
			bad := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					r, _, _, _ := got.At(x, y).RGBA()
					want := uint32(uint8((x*13+y*7)%251)) * 257
					if r != want {
						bad++
					}
				}
			}
			if bad != 0 {
				t.Fatalf("%d of %d samples wrong (%.1f%%); a codestream this library "+
					"wrote must be one it can read", bad, w*h, 100*float64(bad)/float64(w*h))
			}
		})
	}
}

// TestPacketLengthMarkersDoNotChangeTheSamples pins the other half: PLT is
// signalling, so turning it on must not alter a single decoded value.
func TestPacketLengthMarkersDoNotChangeTheSamples(t *testing.T) {
	const w, h = 128, 128
	src := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8((x*29 + y*17) % 253)})
		}
	}

	decode := func(plt bool) image.Image {
		t.Helper()
		var buf bytes.Buffer
		if err := Encode(&buf, src, &Options{
			Lossless: true, Format: FormatJ2K, NumResolutions: 4,
			WritePacketLengths: plt,
		}); err != nil {
			t.Fatalf("Encode(plt=%v): %v", plt, err)
		}
		img, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("Decode(plt=%v): %v", plt, err)
		}
		return img
	}

	with, without := decode(true), decode(false)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a, _, _, _ := with.At(x, y).RGBA()
			b, _, _, _ := without.At(x, y).RGBA()
			if a != b {
				t.Fatalf("(%d,%d): with PLT %d, without %d; the markers are signalling "+
					"and must not change a sample", x, y, a, b)
			}
		}
	}
}
