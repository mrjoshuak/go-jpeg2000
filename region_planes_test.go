package jpeg2000

import (
	"bytes"
	"image"
	"testing"
)

// TestRegionDecodeOnTheFloatPath checks the entry point EXR HTJ2K chunks
// actually use.
//
// DecodeConfigCost covers the image.Image path, and go-openexr never calls it:
// an HTJ2K chunk is half or float samples, so it goes through
// DecodeFloatConfig and DecodeHalfConfig. A region that worked on one path and
// not the other would pass every check written against image.Image and fail
// the only caller there is.
//
// The image is 256x256 rather than something smaller for a reason worth
// recording: a code-block's influence is its band rectangle grown by the
// synthesis margin, 4<<(numRes-1-res) samples, and at the lowest resolution of
// a five-level decode that is 64 samples in every direction. Below roughly
// 256x256 every block reaches every part of the image, so nothing can be
// skipped and a skip assertion measures the image size instead of the code.
func TestRegionDecodeOnTheFloatPath(t *testing.T) {
	const w, h = 256, 256

	planes := [][]float32{make([]float32, w*h), make([]float32, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			planes[0][y*w+x] = float32(x)*0.25 + float32(y)*0.5
			planes[1][y*w+x] = float32(x*y%97) * 0.125
		}
	}
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, &FloatImage{
		Width: w, Height: h, Components: planes, BitDepth: 16, Signed: true,
	}, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: 5,
		PrecinctSizes: []PrecinctSize{{5, 5}, {5, 5}, {5, 5}, {5, 5}, {5, 5}},
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	cs := buf.Bytes()

	whole, err := DecodeFloatConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full float decode: %v", err)
	}
	if whole.Cost.Decoded == 0 {
		t.Fatal("a full float decode reports no code-block data; the cost is not wired up on this path")
	}

	region := image.Rect(64, 64, 128, 128)
	got, err := DecodeFloatConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("region float decode: %v", err)
	}
	if got.Width != region.Dx() || got.Height != region.Dy() {
		t.Fatalf("region decode produced %dx%d, want %dx%d",
			got.Width, got.Height, region.Dx(), region.Dy())
	}
	if got.ComponentCount() != 2 {
		t.Fatalf("region decode produced %d components, want 2", got.ComponentCount())
	}
	for c := range got.Components {
		for y := 0; y < got.Height; y++ {
			for x := 0; x < got.Width; x++ {
				g := got.Components[c][y*got.Width+x]
				want := whole.Components[c][(region.Min.Y+y)*w+(region.Min.X+x)]
				if g != want {
					t.Fatalf("component %d sample (%d,%d) = %v, the full decode has %v at (%d,%d)",
						c, x, y, g, want, region.Min.X+x, region.Min.Y+y)
				}
			}
		}
	}
	if got.Cost.Skipped == 0 {
		t.Error("a region of the float path skipped no code-blocks; it decoded everything and cropped")
	}
	if got.Cost.Decoded >= whole.Cost.Decoded {
		t.Errorf("the region decoded %d code-block bytes and the whole image %d; a region must cost less",
			got.Cost.Decoded, whole.Cost.Decoded)
	}
	t.Logf("float path, %v of %dx%d: decoded %d of %d code-block bytes, skipped %d",
		region, w, h, got.Cost.Decoded, whole.Cost.Decoded, got.Cost.Skipped)
}

// TestRegionDecodeOnTheHalfPath is the same for half samples, which is what a
// half EXR chunk carries.
func TestRegionDecodeOnTheHalfPath(t *testing.T) {
	const w, h = 256, 256

	planes := [][]uint16{make([]uint16, w*h), make([]uint16, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			planes[0][y*w+x] = float32ToHalf(float32(x)*0.25 + float32(y)*0.5)
			planes[1][y*w+x] = float32ToHalf(float32(x*y%97) * 0.125)
		}
	}
	var buf bytes.Buffer
	if err := EncodeHalf(&buf, &HalfImage{Width: w, Height: h, Components: planes}, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: 5,
		PrecinctSizes: []PrecinctSize{{5, 5}, {5, 5}, {5, 5}, {5, 5}, {5, 5}},
	}); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	cs := buf.Bytes()

	whole, err := DecodeHalfConfig(bytes.NewReader(cs), nil)
	if err != nil {
		t.Fatalf("full half decode: %v", err)
	}
	region := image.Rect(64, 64, 128, 128)
	got, err := DecodeHalfConfig(bytes.NewReader(cs), &Config{DecodeArea: &region})
	if err != nil {
		t.Fatalf("region half decode: %v", err)
	}
	if got.Width != region.Dx() || got.Height != region.Dy() {
		t.Fatalf("region decode produced %dx%d, want %dx%d",
			got.Width, got.Height, region.Dx(), region.Dy())
	}
	for c := range got.Components {
		for y := 0; y < got.Height; y++ {
			for x := 0; x < got.Width; x++ {
				g := got.Components[c][y*got.Width+x]
				want := whole.Components[c][(region.Min.Y+y)*w+(region.Min.X+x)]
				if g != want {
					t.Fatalf("component %d sample (%d,%d) = %v (%v), the full decode has %v (%v)",
						c, x, y, g, halfToFloat32(g), want, halfToFloat32(want))
				}
			}
		}
	}
	if got.Cost.Skipped == 0 {
		t.Error("a region of the half path skipped no code-blocks; it decoded everything and cropped")
	}
	if got.Cost.Decoded >= whole.Cost.Decoded {
		t.Errorf("the region decoded %d code-block bytes and the whole image %d; a region must cost less",
			got.Cost.Decoded, whole.Cost.Decoded)
	}
	t.Logf("half path, %v of %dx%d: decoded %d of %d code-block bytes, skipped %d",
		region, w, h, got.Cost.Decoded, whole.Cost.Decoded, got.Cost.Skipped)
}
