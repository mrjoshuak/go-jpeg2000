package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// TestRegionDecodeReadsASubset is the other half of the region-decode bar: not
// only that the samples are right, but that getting them costs less than
// decoding everything.
//
// A region decode that produced the right pixels by decoding the whole image
// and cropping would satisfy every correctness check and none of the point.
// What is measured here is the code-block data actually put through the block
// coder, which is where a decode spends its time.
func TestRegionDecodeReadsASubset(t *testing.T) {
	const w, h, nres = 256, 256, 5

	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x*13 + y*7) % 251)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres,
		// Precincts are what make a region addressable: without them a
		// resolution is one packet covering everything.
		PrecinctSizes: []PrecinctSize{{5, 5}, {5, 5}, {5, 5}, {5, 5}, {5, 5}},
	}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cs := buf.Bytes()

	_, whole, err := DecodeConfigCost(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full decode: %v", err)
	}
	if whole.Decoded == 0 {
		t.Fatal("a full decode reports no code-block data; the measurement is not wired up")
	}
	if whole.Skipped != 0 {
		t.Errorf("a full decode skipped %d bytes; it should skip nothing", whole.Skipped)
	}

	region := image.Rect(0, 0, 64, 64) // a sixteenth of the image
	got, part, err := DecodeConfigCost(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("region decode: %v", err)
	}
	if b := got.Bounds(); b.Dx() != region.Dx() || b.Dy() != region.Dy() {
		t.Fatalf("region decode produced %dx%d, want %dx%d",
			b.Dx(), b.Dy(), region.Dx(), region.Dy())
	}
	if part.Skipped == 0 {
		t.Fatal("a 64x64 region of a 256x256 image skipped no code-blocks")
	}
	if part.Decoded >= whole.Decoded {
		t.Fatalf("the region decoded %d bytes of code-block data and the whole image %d; "+
			"a region must cost less", part.Decoded, whole.Decoded)
	}

	// The two must account for the same total, or blocks are being lost rather
	// than skipped.
	if part.Decoded+part.Skipped != whole.Decoded {
		t.Errorf("region decoded %d and skipped %d, totalling %d; the whole image is %d",
			part.Decoded, part.Skipped, part.Decoded+part.Skipped, whole.Decoded)
	}

	t.Logf("64x64 of %dx%d: decoded %d of %d code-block bytes (%.0f%%), skipped %d",
		w, h, part.Decoded, whole.Decoded,
		100*float64(part.Decoded)/float64(whole.Decoded), part.Skipped)
}
