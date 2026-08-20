package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// rampFloatImage is a smooth ramp: a reduced-resolution decode of noise has no
// relationship to the full one, so a comparison against it would say nothing.
func rampFloatImage(w, h int) *FloatImage {
	img := &FloatImage{
		Width: w, Height: h, BitDepth: 32,
		Components: [][]float32{make([]float32, w*h)},
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Components[0][y*w+x] = float32(x)/float32(w) + float32(y)/float32(h)
		}
	}
	return img
}

func encodeRamp(t *testing.T, w, h, nres int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, rampFloatImage(w, h), &Options{
		HighThroughput: true, Lossless: true, Format: FormatJ2K, NumResolutions: nres,
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	return buf.Bytes()
}

// TestReduceResolutionCostsLess is what a reduced decode is for.
//
// This replaces a test that pinned a refusal. The refusal was wrong, and it was
// wrong for an instructive reason: it rested on comparing a reduced decode
// against a downsample of the full decode — "samples off by 175 on a ramp
// spanning 0 to 2" — and that is not what a reduced decode produces. The LL
// band at resolution r is the image the wavelet reconstructs at that scale, not
// an arithmetic average of the finer one, so the two disagree by construction
// and the disagreement was read as a defect. Against the right oracle — the
// reference implementation's own reduced decode, which the gate runs — this
// library was already bit-exact at every reduction level, on both the float and
// the half path.
//
// What was genuinely missing is the saving. A reduced decode returned the right
// samples and entropy-decoded every code-block in the codestream to get them,
// including the resolutions it then discarded, so it cost exactly what a full
// decode cost. That is the shape of saving that isn't one, and it is what this
// test measures.
func TestReduceResolutionCostsLess(t *testing.T) {
	const w, h, nres = 256, 256, 6
	cs := encodeRamp(t, w, h, nres)

	whole, err := DecodeFloatConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full float decode: %v", err)
	}
	if whole.Cost.Decoded == 0 {
		t.Fatal("a full decode reports no code-block data; the measurement is not wired up")
	}
	if whole.Cost.Skipped != 0 {
		t.Errorf("a full decode skipped %d bytes; it should skip nothing", whole.Cost.Skipped)
	}

	prev := whole.Cost.Decoded
	for reduce := 1; reduce < nres; reduce++ {
		got, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{ReduceResolution: reduce})
		if err != nil {
			t.Fatalf("reduce %d: %v", reduce, err)
		}
		wantW, wantH := w>>uint(reduce), h>>uint(reduce)
		if got.Width != wantW || got.Height != wantH {
			t.Fatalf("reduce %d produced %dx%d, want %dx%d",
				reduce, got.Width, got.Height, wantW, wantH)
		}
		if got.Cost.Skipped == 0 {
			t.Fatalf("reduce %d skipped no code-blocks; it decoded every resolution and "+
				"discarded the ones it had just spent the time on", reduce)
		}
		if got.Cost.Decoded+got.Cost.Skipped != whole.Cost.Decoded {
			t.Errorf("reduce %d decoded %d and skipped %d, totalling %d; the whole codestream is %d",
				reduce, got.Cost.Decoded, got.Cost.Skipped,
				got.Cost.Decoded+got.Cost.Skipped, whole.Cost.Decoded)
		}
		// Each further level of reduction must cost strictly less than the
		// last, or the skip is not tracking the resolution it claims to.
		if got.Cost.Decoded >= prev {
			t.Errorf("reduce %d decoded %d code-block bytes, no fewer than reduce %d's %d",
				reduce, got.Cost.Decoded, reduce-1, prev)
		}
		prev = got.Cost.Decoded
		t.Logf("reduce %d: %dx%d, decoded %d of %d code-block bytes (%.1f%%)",
			reduce, got.Width, got.Height, got.Cost.Decoded, whole.Cost.Decoded,
			100*float64(got.Cost.Decoded)/float64(whole.Cost.Decoded))
	}
}

// TestReduceResolutionIsStableAcrossPaths checks that the half and float entry
// points agree with each other about what a reduced decode of the same samples
// is.
//
// They are different functions over a shared core, and the half path refused
// this outright until now while the float path did not, so they have never been
// compared. An oracle-free check is still worth having beside the gate's
// external one: it runs on every machine, including those without OpenJPH.
func TestReduceResolutionIsStableAcrossPaths(t *testing.T) {
	const w, h, nres = 64, 64, 4

	// A constant image is the one case where the answer is knowable without an
	// oracle: every resolution's LL band is that constant, so a reduced decode
	// must return it exactly, at every level, on both paths.
	const constant = 0.75
	planes := [][]float32{make([]float32, w*h)}
	for i := range planes[0] {
		planes[0][i] = constant
	}
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, &FloatImage{
		Width: w, Height: h, Components: planes, BitDepth: 32, Signed: true,
	}, &Options{Lossless: true, Format: FormatJ2K, NumResolutions: nres}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	cs := buf.Bytes()

	for reduce := 0; reduce < nres; reduce++ {
		var cfg *Config
		if reduce > 0 {
			cfg = &Config{ReduceResolution: reduce}
		}
		got, err := DecodeFloatConfig(bytes.NewReader(cs), cfg)
		if err != nil {
			t.Fatalf("reduce %d: %v", reduce, err)
		}
		for i, v := range got.Components[0] {
			if v != constant {
				t.Fatalf("reduce %d, sample %d = %v, want %v; every level of a constant "+
					"image is that constant", reduce, i, v, constant)
			}
		}
	}

	// The half path must accept the same request and produce the same shape.
	hplanes := [][]uint16{make([]uint16, w*h)}
	for i := range hplanes[0] {
		hplanes[0][i] = float32ToHalf(constant)
	}
	var hbuf bytes.Buffer
	if err := EncodeHalf(&hbuf, &HalfImage{Width: w, Height: h, Components: hplanes},
		&Options{Lossless: true, Format: FormatJ2K, NumResolutions: nres}); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	for reduce := 1; reduce < nres; reduce++ {
		got, err := DecodeHalfConfig(bytes.NewReader(hbuf.Bytes()),
			&Config{ReduceResolution: reduce})
		if err != nil {
			t.Fatalf("half reduce %d: %v", reduce, err)
		}
		if got.Width != w>>uint(reduce) || got.Height != h>>uint(reduce) {
			t.Fatalf("half reduce %d produced %dx%d, want %dx%d",
				reduce, got.Width, got.Height, w>>uint(reduce), h>>uint(reduce))
		}
		for i, v := range got.Components[0] {
			if halfToFloat32(v) != constant {
				t.Fatalf("half reduce %d, sample %d = %v, want %v",
					reduce, i, halfToFloat32(v), constant)
			}
		}
	}
}

// TestDecodeAreaMatchesTheFullDecode is the correctness half of region decode:
// the samples must be the same ones a full decode produces for that rectangle,
// exactly, and the returned image must cover the region rather than the image.
//
// Exactness is the right bar here and not a lucky one. A region decode stops
// short of entropy-decoding code-blocks that cannot reach the region, but every
// coefficient that can reach it is decoded and synthesised as usual, so the
// samples are not an approximation of the full decode — they are the same
// arithmetic.
func TestDecodeAreaMatchesTheFullDecode(t *testing.T) {
	const w, h, nres = 64, 48, 4

	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 251)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres,
		PrecinctSizes: []PrecinctSize{{4, 4}, {4, 4}, {4, 4}, {4, 4}},
	}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cs := buf.Bytes()

	full, err := DecodeConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full decode: %v", err)
	}
	at := func(im image.Image, x, y int) uint32 {
		r, _, _, _ := im.At(im.Bounds().Min.X+x, im.Bounds().Min.Y+y).RGBA()
		return r >> 8
	}

	for _, region := range []image.Rectangle{
		image.Rect(0, 0, 16, 16),
		image.Rect(16, 8, 48, 40),
		image.Rect(48, 32, 64, 48),
		image.Rect(0, 0, w, h),
		image.Rect(31, 23, 33, 25), // straddles every precinct boundary nearby
	} {
		got, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
		if err != nil {
			t.Errorf("region %v: %v", region, err)
			continue
		}
		b := got.Bounds()
		if b.Dx() != region.Dx() || b.Dy() != region.Dy() {
			t.Errorf("region %v produced %dx%d, want %dx%d",
				region, b.Dx(), b.Dy(), region.Dx(), region.Dy())
			continue
		}
		bad := 0
		for y := 0; y < region.Dy(); y++ {
			for x := 0; x < region.Dx(); x++ {
				if at(got, x, y) != at(full, region.Min.X+x, region.Min.Y+y) {
					bad++
				}
			}
		}
		if bad != 0 {
			t.Errorf("region %v: %d of %d samples differ from the full decode",
				region, bad, region.Dx()*region.Dy())
		}
	}
}

// TestDecodeAreaIsHonouredNotIgnored replaces a test that asserted the option
// was refused, which was true while it was unimplemented.
//
// A dropped request is worse than a missing feature: a caller sizing a buffer
// for a 32x16 region and receiving the whole 64x32 image has no indication, and
// reads the wrong pixels out of it. That is what this pinned before and what it
// pins now, from the other side.
func TestDecodeAreaIsHonouredNotIgnored(t *testing.T) {
	const w, h, nres = 64, 32, 4
	cs := encodeRamp(t, w, h, nres)
	region := image.Rect(16, 8, 48, 24)

	got, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("float path: %v", err)
	}
	if got.Width != region.Dx() || got.Height != region.Dy() {
		t.Errorf("float path produced %dx%d for region %v", got.Width, got.Height, region)
	}

	gi, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("integer path: %v", err)
	}
	if b := gi.Bounds(); b.Dx() != region.Dx() || b.Dy() != region.Dy() {
		t.Errorf("integer path produced %dx%d for region %v", b.Dx(), b.Dy(), region)
	}

	// A region outside the image is clipped to nothing, which decodes the
	// whole image rather than failing: an empty request is not an error.
	outside := image.Rect(1000, 1000, 1100, 1100)
	if _, err := DecodeConfig(bytes.NewReader(cs), &Config{DecodeArea: &outside}); err != nil {
		t.Errorf("a region outside the image was refused: %v", err)
	}
}
