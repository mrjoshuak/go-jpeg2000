package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// rgnStream builds a codestream and splices an RGN marker segment into its main
// header, immediately before SOT.
//
// It is built by hand because no encoder available here writes a meaningful
// one: opj_compress's -ROI emits the marker and shifts no coefficients, which
// is established in the gate and in the ROADMAP. Hand-splicing lets the parser
// be tested against the bytes the standard defines even though the
// reconstruction cannot be.
func rgnStream(t *testing.T, comp uint8, srgn, sprgn uint8) []byte {
	t.Helper()
	const size = 16
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.Gray{Y: uint8(20 + (x*13+y*3)%200)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		HighThroughput: true, Lossless: true,
		Format: FormatJ2K, NumResolutions: 2,
	}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cs := buf.Bytes()

	sot := bytes.Index(cs, []byte{0xFF, 0x90})
	if sot < 0 {
		t.Fatal("no SOT in the fixture")
	}
	seg := []byte{0xFF, 0x5E, 0, 0, comp, srgn, sprgn}
	binary.BigEndian.PutUint16(seg[2:4], 5) // Lrgn: itself plus Crgn, Srgn, SPrgn

	out := make([]byte, 0, len(cs)+len(seg))
	out = append(out, cs[:sot]...)
	out = append(out, seg...)
	out = append(out, cs[sot:]...)
	return out
}

// TestRGNMarkerIsParsed checks that the region-of-interest marker is read
// rather than skipped, and that an ROI style the standard does not define is
// refused rather than assumed to be the one it does.
//
// Until v1.5.10 RGN appeared only in a table of marker names: the parser fell
// through to the default branch and skipped the segment. A decoder that skips
// it cannot apply the max-shift reconstruction, and the standard's own warning
// is that such a decoder produces a wrongly scaled image rather than an error.
//
// What this test does NOT establish is that the reconstruction is right —
// nothing here can, because no encoder available produces a stream where the
// shift is actually applied. That is recorded in the ROADMAP as an oracle
// block, with the measurement, rather than papered over with an assertion
// against our own arithmetic.
func TestRGNMarkerIsParsed(t *testing.T) {
	t.Run("max-shift style is accepted", func(t *testing.T) {
		cs := rgnStream(t, 0, 0, 8) // Srgn 0 is the max-shift method
		img, err := Decode(bytes.NewReader(cs))
		if err != nil {
			t.Fatalf("a codestream with a well-formed RGN marker failed to "+
				"decode: %v", err)
		}
		if got := img.Bounds().Dx(); got != 16 {
			t.Fatalf("decoded %d wide, want 16", got)
		}
	})

	t.Run("undefined style is refused", func(t *testing.T) {
		cs := rgnStream(t, 0, 3, 8) // Srgn 3 is not a defined ROI style
		_, err := Decode(bytes.NewReader(cs))
		if err == nil {
			t.Fatal("a codestream declaring ROI style 3 decoded; only style 0, " +
				"the max-shift method, is defined, and decoding with a scaling " +
				"rule we do not understand returns plausible wrong samples")
		}
		if !bytes.Contains([]byte(err.Error()), []byte("RGN")) {
			t.Errorf("the refusal does not name the marker: %v", err)
		}
	})

	t.Run("a component that does not exist is refused", func(t *testing.T) {
		cs := rgnStream(t, 9, 0, 8) // the fixture has one component
		_, err := Decode(bytes.NewReader(cs))
		if err == nil {
			t.Fatal("an RGN naming component 9 of a one-component image decoded")
		}
	})
}
