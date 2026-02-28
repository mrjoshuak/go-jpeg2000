package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestFloatImage_Bounds(t *testing.T) {
	fi := &FloatImage{
		Width:  100,
		Height: 50,
	}
	want := image.Rect(0, 0, 100, 50)
	if got := fi.Bounds(); got != want {
		t.Errorf("Bounds() = %v, want %v", got, want)
	}
}

func TestFloatImage_ComponentCount(t *testing.T) {
	fi := &FloatImage{
		Components: make([][]float32, 3),
	}
	if got := fi.ComponentCount(); got != 3 {
		t.Errorf("ComponentCount() = %d, want 3", got)
	}
}

func TestFloatImage_At(t *testing.T) {
	fi := &FloatImage{
		Width:  2,
		Height: 2,
		Components: [][]float32{
			{1.0, 2.0, 3.0, 4.0},
			{5.0, 6.0, 7.0, 8.0},
			{9.0, 10.0, 11.0, 12.0},
		},
	}

	// Test valid pixel
	vals := fi.At(1, 0)
	if len(vals) != 3 {
		t.Fatalf("At(1,0) returned %d components, want 3", len(vals))
	}
	if vals[0] != 2.0 || vals[1] != 6.0 || vals[2] != 10.0 {
		t.Errorf("At(1,0) = %v, want [2.0 6.0 10.0]", vals)
	}

	// Test corner pixel
	vals = fi.At(1, 1)
	if vals[0] != 4.0 || vals[1] != 8.0 || vals[2] != 12.0 {
		t.Errorf("At(1,1) = %v, want [4.0 8.0 12.0]", vals)
	}

	// Test out of bounds
	if got := fi.At(-1, 0); got != nil {
		t.Errorf("At(-1,0) = %v, want nil", got)
	}
	if got := fi.At(0, 2); got != nil {
		t.Errorf("At(0,2) = %v, want nil", got)
	}
}

func TestFloatImage_AtEmpty(t *testing.T) {
	fi := &FloatImage{
		Width:      1,
		Height:     1,
		Components: [][]float32{{42.5}},
	}
	vals := fi.At(0, 0)
	if len(vals) != 1 || vals[0] != 42.5 {
		t.Errorf("At(0,0) = %v, want [42.5]", vals)
	}
}

func TestDecodeFloat_Gray(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	fi, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat() error: %v", err)
	}

	if fi.Width != 8 || fi.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", fi.Width, fi.Height)
	}
	if fi.ComponentCount() != 1 {
		t.Errorf("ComponentCount() = %d, want 1", fi.ComponentCount())
	}
	if fi.BitDepth != 8 {
		t.Errorf("BitDepth = %d, want 8", fi.BitDepth)
	}
}

func TestDecodeFloat_RGB(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: uint8((x + y) * 16),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	fi, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat() error: %v", err)
	}

	if fi.Width != 8 || fi.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", fi.Width, fi.Height)
	}
	if fi.ComponentCount() != 4 {
		t.Errorf("ComponentCount() = %d, want 4", fi.ComponentCount())
	}
}

func TestDecodeFloat_Lossy(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: 128,
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = false
	opts.Quality = 90
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	fi, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat() error: %v", err)
	}

	if fi.Width != 16 || fi.Height != 16 {
		t.Errorf("dimensions = %dx%d, want 16x16", fi.Width, fi.Height)
	}
	if fi.ComponentCount() != 4 {
		t.Errorf("ComponentCount() = %d, want 4", fi.ComponentCount())
	}
}

func TestDecodeFloat_16bit(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray16(x, y, color.Gray16{Y: uint16(x*4096 + y*4096)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	fi, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat() error: %v", err)
	}

	if fi.BitDepth != 16 {
		t.Errorf("BitDepth = %d, want 16", fi.BitDepth)
	}
	if fi.ComponentCount() != 1 {
		t.Errorf("ComponentCount() = %d, want 1", fi.ComponentCount())
	}
}

func TestDecodeFloat_MatchesIntegerPath(t *testing.T) {
	// Encode a lossless image and verify float and integer paths give same values
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*30 + y)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	data := buf.Bytes()

	// Decode as integer
	intImg, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	// Decode as float
	fi, err := DecodeFloat(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeFloat() error: %v", err)
	}

	grayImg, ok := intImg.(*image.Gray)
	if !ok {
		t.Fatalf("expected *image.Gray, got %T", intImg)
	}

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			intVal := grayImg.GrayAt(x, y).Y
			floatVal := fi.At(x, y)[0]
			// For lossless, float and integer should match closely
			diff := float32(intVal) - floatVal
			if diff < -1.0 || diff > 1.0 {
				t.Errorf("pixel (%d,%d): int=%d, float=%f, diff=%f",
					x, y, intVal, floatVal, diff)
			}
		}
	}
}

func TestDecodeFloatConfig(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: 128})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	fi, err := DecodeFloatConfig(bytes.NewReader(buf.Bytes()), &Config{})
	if err != nil {
		t.Fatalf("DecodeFloatConfig() error: %v", err)
	}

	if fi.Width != 8 || fi.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", fi.Width, fi.Height)
	}
}
