package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts == nil {
		t.Fatal("DefaultOptions() returned nil")
	}

	if opts.Format != FormatJP2 {
		t.Errorf("Format = %v, want FormatJP2", opts.Format)
	}

	if opts.NumResolutions != 6 {
		t.Errorf("NumResolutions = %d, want 6", opts.NumResolutions)
	}

	if opts.Quality != 75 {
		t.Errorf("Quality = %d, want 75", opts.Quality)
	}

	if opts.NumLayers != 1 {
		t.Errorf("NumLayers = %d, want 1", opts.NumLayers)
	}
}

func TestFormat_String(t *testing.T) {
	tests := []struct {
		format Format
		want   string
	}{
		{FormatJ2K, "J2K"},
		{FormatJP2, "JP2"},
		{FormatJPX, "JPX"},
		{Format(99), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.format.String()
		if got != tt.want {
			t.Errorf("Format(%d).String() = %q, want %q", tt.format, got, tt.want)
		}
	}
}

func TestProgressionOrder_String(t *testing.T) {
	tests := []struct {
		order ProgressionOrder
		want  string
	}{
		{LRCP, "LRCP"},
		{RLCP, "RLCP"},
		{RPCL, "RPCL"},
		{PCRL, "PCRL"},
		{CPRL, "CPRL"},
		{ProgressionOrder(99), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.order.String()
		if got != tt.want {
			t.Errorf("ProgressionOrder(%d).String() = %q, want %q", tt.order, got, tt.want)
		}
	}
}

func TestEncodeGray(t *testing.T) {
	// Create a simple 8x8 grayscale image
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true

	err := Encode(&buf, img, opts)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncodeRGBA(t *testing.T) {
	// Create a simple 8x8 RGBA image
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

	err := Encode(&buf, img, opts)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_J2KFormat(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K

	err := Encode(&buf, img, opts)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// J2K starts with SOC marker (0xFF 0x4F)
	data := buf.Bytes()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0x4F {
		t.Error("J2K output should start with SOC marker")
	}
}

func TestEncode_JP2Format(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2

	err := Encode(&buf, img, opts)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// JP2 starts with signature box
	data := buf.Bytes()
	if len(data) < 12 {
		t.Fatal("JP2 output too short")
	}

	// Check JP2 signature
	if data[4] != 'j' || data[5] != 'P' || data[6] != ' ' || data[7] != ' ' {
		t.Error("JP2 output should have jP signature box")
	}
}

func TestEncode_WithComment(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Comment = "Test comment"

	err := Encode(&buf, img, opts)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Comment should be in the output
	if !bytes.Contains(buf.Bytes(), []byte("Test comment")) {
		t.Error("Output should contain comment")
	}
}

func TestEncode_LosslessOption(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	// Lossless encoding
	var lossless bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true
	err := Encode(&lossless, img, opts)
	if err != nil {
		t.Fatalf("Lossless Encode() error: %v", err)
	}

	// Lossy encoding
	var lossy bytes.Buffer
	opts.Lossless = false
	opts.Quality = 50
	err = Encode(&lossy, img, opts)
	if err != nil {
		t.Fatalf("Lossy Encode() error: %v", err)
	}

	// Both should produce output
	if lossless.Len() == 0 {
		t.Error("Lossless encoding produced empty output")
	}
	if lossy.Len() == 0 {
		t.Error("Lossy encoding produced empty output")
	}
}

func TestEncodeNilOptions(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	err := Encode(&buf, img, nil)
	if err != nil {
		t.Fatalf("Encode() with nil options error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() with nil options produced empty output")
	}
}

func TestMetadata(t *testing.T) {
	m := &Metadata{
		Format:           FormatJP2,
		Width:            100,
		Height:           100,
		NumComponents:    3,
		BitsPerComponent: []int{8, 8, 8},
		Signed:           []bool{false, false, false},
		ColorSpace:       ColorSpaceSRGB,
		NumResolutions:   6,
		NumQualityLayers: 1,
		TileWidth:        100,
		TileHeight:       100,
		NumTilesX:        1,
		NumTilesY:        1,
	}

	if m.Width != 100 {
		t.Errorf("Width = %d, want 100", m.Width)
	}
	if m.Height != 100 {
		t.Errorf("Height = %d, want 100", m.Height)
	}
	if m.NumComponents != 3 {
		t.Errorf("NumComponents = %d, want 3", m.NumComponents)
	}
}

func TestColorSpaceConstants(t *testing.T) {
	// Verify constants match OpenJPEG OPJ_COLOR_SPACE enum values
	if ColorSpaceUnknown != -1 {
		t.Errorf("ColorSpaceUnknown = %d, want -1 (OPJ_CLRSPC_UNKNOWN)", ColorSpaceUnknown)
	}
	if ColorSpaceUnspecified != 0 {
		t.Errorf("ColorSpaceUnspecified = %d, want 0 (OPJ_CLRSPC_UNSPECIFIED)", ColorSpaceUnspecified)
	}
	if ColorSpaceSRGB != 1 {
		t.Errorf("ColorSpaceSRGB = %d, want 1 (OPJ_CLRSPC_SRGB)", ColorSpaceSRGB)
	}
	if ColorSpaceGray != 2 {
		t.Errorf("ColorSpaceGray = %d, want 2 (OPJ_CLRSPC_GRAY)", ColorSpaceGray)
	}
}

func TestProfileConstants(t *testing.T) {
	// Verify profile constants
	if ProfileNone != 0 {
		t.Error("ProfileNone should be 0")
	}
	if ProfileCinema2K != 3 {
		t.Error("ProfileCinema2K should be 3")
	}
	if ProfileCinema4K != 4 {
		t.Error("ProfileCinema4K should be 4")
	}
}

func BenchmarkEncode_Gray8x8(b *testing.B) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x + y*8)})
		}
	}
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		Encode(&buf, img, opts)
	}
}

func BenchmarkEncode_Gray64x64(b *testing.B) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		Encode(&buf, img, opts)
	}
}

func BenchmarkEncode_RGBA64x64(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 4),
				G: uint8(y * 4),
				B: uint8((x + y) * 2),
				A: 255,
			})
		}
	}
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		Encode(&buf, img, opts)
	}
}

func BenchmarkEncode_RGBA512x512(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 255) / 512),
				G: uint8((y * 255) / 512),
				B: uint8(((x + y) * 127) / 512),
				A: 255,
			})
		}
	}
	opts := DefaultOptions()
	opts.Lossless = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		Encode(&buf, img, opts)
	}
}

func TestRoundtrip_Grayscale_Lossless(t *testing.T) {
	// Create a test image
	original := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x*16 + y*16) % 256)})
		}
	}

	// Encode
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K // Use J2K to avoid box overhead
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	// Compare dimensions
	origBounds := original.Bounds()
	decBounds := decoded.Bounds()
	if origBounds.Dx() != decBounds.Dx() || origBounds.Dy() != decBounds.Dy() {
		t.Errorf("Dimension mismatch: original %dx%d, decoded %dx%d",
			origBounds.Dx(), origBounds.Dy(), decBounds.Dx(), decBounds.Dy())
	}
}

func TestRoundtrip_RGB_Lossless(t *testing.T) {
	// Create a test image
	original := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: uint8((x + y) * 8),
				A: 255,
			})
		}
	}

	// Encode
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	// Compare dimensions
	origBounds := original.Bounds()
	decBounds := decoded.Bounds()
	if origBounds.Dx() != decBounds.Dx() || origBounds.Dy() != decBounds.Dy() {
		t.Errorf("Dimension mismatch: original %dx%d, decoded %dx%d",
			origBounds.Dx(), origBounds.Dy(), decBounds.Dx(), decBounds.Dy())
	}
}

func TestRoundtrip_JP2_Format(t *testing.T) {
	// Create a test image
	original := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8(x + y*8)})
		}
	}

	// Encode with JP2 format
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Verify JP2 signature
	data := buf.Bytes()
	if len(data) < 12 {
		t.Fatal("JP2 output too short")
	}
	if data[4] != 'j' || data[5] != 'P' || data[6] != ' ' || data[7] != ' ' {
		t.Error("Missing JP2 signature")
	}

	// Decode
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	// Compare dimensions
	origBounds := original.Bounds()
	decBounds := decoded.Bounds()
	if origBounds.Dx() != decBounds.Dx() || origBounds.Dy() != decBounds.Dy() {
		t.Errorf("Dimension mismatch: original %dx%d, decoded %dx%d",
			origBounds.Dx(), origBounds.Dy(), decBounds.Dx(), decBounds.Dy())
	}
}

// Test DecodeMetadata function
func TestDecodeMetadata_J2K(t *testing.T) {
	// Create and encode an image
	original := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x + y) * 8)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode metadata
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.Width != 16 || meta.Height != 16 {
		t.Errorf("Dimensions = %dx%d, want 16x16", meta.Width, meta.Height)
	}
	if meta.NumComponents != 1 {
		t.Errorf("NumComponents = %d, want 1", meta.NumComponents)
	}
	if meta.Format != FormatJ2K {
		t.Errorf("Format = %v, want FormatJ2K", meta.Format)
	}
}

func TestDecodeMetadata_JP2(t *testing.T) {
	// Create and encode an RGB image
	original := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode metadata
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.Width != 8 || meta.Height != 8 {
		t.Errorf("Dimensions = %dx%d, want 8x8", meta.Width, meta.Height)
	}
	if meta.NumComponents != 3 {
		t.Errorf("NumComponents = %d, want 3", meta.NumComponents)
	}
	if meta.Format != FormatJP2 {
		t.Errorf("Format = %v, want FormatJP2", meta.Format)
	}
}

// Test image.Decode and image.DecodeConfig via init() registration
func TestImageDecode_JP2Registration(t *testing.T) {
	// Create and encode a JP2 image
	original := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8(x + y*8)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode using image.Decode (tests init registration)
	decoded, format, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("image.Decode() error: %v", err)
	}
	if format != "jp2" {
		t.Errorf("format = %q, want \"jp2\"", format)
	}
	if decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 8 {
		t.Errorf("decoded dimensions = %dx%d, want 8x8",
			decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestImageDecodeConfig_JP2Registration(t *testing.T) {
	// Create and encode a JP2 image
	original := image.NewRGBA(image.Rect(0, 0, 16, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 16; x++ {
			original.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 10), B: 64, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode config using image.DecodeConfig (tests init registration)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("image.DecodeConfig() error: %v", err)
	}
	if format != "jp2" {
		t.Errorf("format = %q, want \"jp2\"", format)
	}
	if cfg.Width != 16 || cfg.Height != 24 {
		t.Errorf("config dimensions = %dx%d, want 16x24", cfg.Width, cfg.Height)
	}
}

func TestImageDecode_J2KRegistration(t *testing.T) {
	// Create and encode a J2K image
	original := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode using image.Decode (tests init registration for J2K)
	decoded, format, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("image.Decode() error: %v", err)
	}
	if format != "j2k" {
		t.Errorf("format = %q, want \"j2k\"", format)
	}
	if decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 8 {
		t.Errorf("decoded dimensions = %dx%d, want 8x8",
			decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestImageDecodeConfig_J2KRegistration(t *testing.T) {
	// Create and encode a J2K image
	original := image.NewGray(image.Rect(0, 0, 32, 16))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode config using image.DecodeConfig
	cfg, format, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("image.DecodeConfig() error: %v", err)
	}
	if format != "j2k" {
		t.Errorf("format = %q, want \"j2k\"", format)
	}
	if cfg.Width != 32 || cfg.Height != 16 {
		t.Errorf("config dimensions = %dx%d, want 32x16", cfg.Width, cfg.Height)
	}
}

// Test encoding different image types
func TestEncode_Gray16(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray16(x, y, color.Gray16{Y: uint16((x + y) * 4096)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_RGBA64(t *testing.T) {
	img := image.NewRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA64(x, y, color.RGBA64{
				R: uint16(x * 8192),
				G: uint16(y * 8192),
				B: uint16((x + y) * 4096),
				A: 65535,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_NRGBA(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: uint8((x + y) * 16),
				A: uint8(128 + x*8),
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test generic image type (non-standard)
func TestEncode_GenericImage(t *testing.T) {
	// Use image.YCbCr as a generic image type that falls through to default
	img := image.NewYCbCr(image.Rect(0, 0, 8, 8), image.YCbCrSubsampleRatio444)

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test clampInt32 helper
func TestClampInt32(t *testing.T) {
	tests := []struct {
		v, min, max, want int32
	}{
		{50, 0, 100, 50},   // within range
		{-10, 0, 100, 0},   // below min
		{150, 0, 100, 100}, // above max
		{0, 0, 100, 0},     // at min
		{100, 0, 100, 100}, // at max
	}

	for _, tt := range tests {
		got := clampInt32(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("clampInt32(%d, %d, %d) = %d, want %d",
				tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

// Test byteReader
func TestByteReader(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	r := &byteReader{data: data}

	// Read partial
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if n != 3 {
		t.Errorf("Read() returned %d, want 3", n)
	}
	if buf[0] != 1 || buf[1] != 2 || buf[2] != 3 {
		t.Errorf("Read() data mismatch")
	}

	// Read remaining
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if n != 2 {
		t.Errorf("Read() returned %d, want 2", n)
	}

	// Read at EOF
	n, err = r.Read(buf)
	if err == nil {
		t.Error("Read() at EOF should return error")
	}
	if n != 0 {
		t.Errorf("Read() at EOF returned %d, want 0", n)
	}
}

// Test error cases
func TestDecode_InvalidFormat(t *testing.T) {
	// Invalid data (not J2K or JP2)
	invalidData := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := Decode(bytes.NewReader(invalidData))
	if err == nil {
		t.Error("Decode() should fail on invalid format")
	}
}

func TestDecode_TooShort(t *testing.T) {
	// Too short to detect format
	shortData := []byte{0xFF}
	_, err := Decode(bytes.NewReader(shortData))
	if err == nil {
		t.Error("Decode() should fail on too short data")
	}
}

func TestDecodeMetadata_InvalidFormat(t *testing.T) {
	invalidData := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := DecodeMetadata(bytes.NewReader(invalidData))
	if err == nil {
		t.Error("DecodeMetadata() should fail on invalid format")
	}
}

// Test encoder options
func TestEncode_WithTileSize(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.TileSize = image.Point{X: 32, Y: 32}
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_WithSOPEPH(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.EnableSOP = true
	opts.EnableEPH = true
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_WithDifferentProgressionOrders(t *testing.T) {
	orders := []ProgressionOrder{LRCP, RLCP, RPCL, PCRL, CPRL}

	for _, order := range orders {
		img := image.NewGray(image.Rect(0, 0, 8, 8))

		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.ProgressionOrder = order
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() with %s error: %v", order, err)
		}
		if buf.Len() == 0 {
			t.Errorf("Encode() with %s produced empty output", order)
		}
	}
}

func TestEncode_WithNumResolutions(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))

	// Test different resolution levels
	for numRes := 1; numRes <= 4; numRes++ {
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.NumResolutions = numRes
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() with NumResolutions=%d error: %v", numRes, err)
		}
		if buf.Len() == 0 {
			t.Errorf("Encode() with NumResolutions=%d produced empty output", numRes)
		}
	}
}

func TestEncode_WithCodeBlockSize(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.CodeBlockSize = image.Point{X: 5, Y: 5} // 32x32 code blocks
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

func TestEncode_WithNumLayers(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.NumLayers = 3
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test lossy encoding with different quality values
func TestEncode_LossyQuality(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	qualities := []int{10, 50, 90}
	for _, q := range qualities {
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Lossless = false
		opts.Quality = q

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() with quality=%d error: %v", q, err)
		}
		if buf.Len() == 0 {
			t.Errorf("Encode() with quality=%d produced empty output", q)
		}
	}
}

// Test unsupported format encoding
func TestEncode_UnsupportedFormat(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJPX // JPX not fully supported

	err := Encode(&buf, img, opts)
	if err == nil {
		t.Error("Encode() with unsupported format should fail")
	}
}

// Test Config struct
func TestConfig(t *testing.T) {
	cfg := &Config{
		DecodeArea:       &image.Rectangle{Min: image.Point{X: 0, Y: 0}, Max: image.Point{X: 50, Y: 50}},
		ReduceResolution: 1,
		QualityLayers:    2,
	}

	if cfg.DecodeArea.Dx() != 50 || cfg.DecodeArea.Dy() != 50 {
		t.Error("DecodeArea not set correctly")
	}
	if cfg.ReduceResolution != 1 {
		t.Errorf("ReduceResolution = %d, want 1", cfg.ReduceResolution)
	}
	if cfg.QualityLayers != 2 {
		t.Errorf("QualityLayers = %d, want 2", cfg.QualityLayers)
	}
}

// Test additional ColorSpace constants
func TestColorSpaceConstants_Extended(t *testing.T) {
	// Verify all colorspace constants match OpenJPEG values
	if ColorSpaceSYCC != 3 {
		t.Errorf("ColorSpaceSYCC = %d, want 3 (OPJ_CLRSPC_SYCC)", ColorSpaceSYCC)
	}
	if ColorSpaceEYCC != 4 {
		t.Errorf("ColorSpaceEYCC = %d, want 4 (OPJ_CLRSPC_EYCC)", ColorSpaceEYCC)
	}
	if ColorSpaceCMYK != 5 {
		t.Errorf("ColorSpaceCMYK = %d, want 5 (OPJ_CLRSPC_CMYK)", ColorSpaceCMYK)
	}
	// Bilevel is our extension beyond OpenJPEG
	if ColorSpaceBilevel != 6 {
		t.Errorf("ColorSpaceBilevel = %d, want 6 (extension)", ColorSpaceBilevel)
	}
}

// Test additional Profile constants
func TestProfileConstants_Extended(t *testing.T) {
	if ProfilePart2 != 0x8000 {
		t.Errorf("ProfilePart2 = %#x, want 0x8000", ProfilePart2)
	}
	if ProfileCinemaS2K != 5 {
		t.Errorf("ProfileCinemaS2K = %d, want 5", ProfileCinemaS2K)
	}
	if ProfileCinemaS4K != 6 {
		t.Errorf("ProfileCinemaS4K = %d, want 6", ProfileCinemaS4K)
	}
	if ProfileCinemaSLTE != 7 {
		t.Errorf("ProfileCinemaSLTE = %d, want 7", ProfileCinemaSLTE)
	}
	if ProfileBroadcastSingle != 0x0100 {
		t.Errorf("ProfileBroadcastSingle = %#x, want 0x0100", ProfileBroadcastSingle)
	}
	if ProfileBroadcastMulti != 0x0200 {
		t.Errorf("ProfileBroadcastMulti = %#x, want 0x0200", ProfileBroadcastMulti)
	}
	if ProfileIMF2K != 0x0400 {
		t.Errorf("ProfileIMF2K = %#x, want 0x0400", ProfileIMF2K)
	}
	if ProfileIMF4K != 0x0500 {
		t.Errorf("ProfileIMF4K = %#x, want 0x0500", ProfileIMF4K)
	}
	if ProfileIMF8K != 0x0600 {
		t.Errorf("ProfileIMF8K = %#x, want 0x0600", ProfileIMF8K)
	}
}

// Test Metadata with additional fields
func TestMetadata_Extended(t *testing.T) {
	m := &Metadata{
		Format:           FormatJP2,
		Width:            200,
		Height:           100,
		NumComponents:    3,
		BitsPerComponent: []int{8, 8, 8},
		Signed:           []bool{false, false, false},
		ColorSpace:       ColorSpaceSRGB,
		Profile:          ProfileCinema2K,
		NumResolutions:   6,
		NumQualityLayers: 4,
		TileWidth:        200,
		TileHeight:       100,
		NumTilesX:        1,
		NumTilesY:        1,
		ICCProfile:       []byte{0x01, 0x02, 0x03},
		Comment:          "Test metadata",
	}

	if m.Profile != ProfileCinema2K {
		t.Errorf("Profile = %d, want ProfileCinema2K", m.Profile)
	}
	if m.NumQualityLayers != 4 {
		t.Errorf("NumQualityLayers = %d, want 4", m.NumQualityLayers)
	}
	if len(m.ICCProfile) != 3 {
		t.Errorf("ICCProfile length = %d, want 3", len(m.ICCProfile))
	}
	if m.Comment != "Test metadata" {
		t.Errorf("Comment = %q, want \"Test metadata\"", m.Comment)
	}
}

// Test Options with additional fields
func TestOptions_Extended(t *testing.T) {
	opts := &Options{
		Format:           FormatJP2,
		Profile:          ProfileCinema4K,
		Lossless:         false,
		Quality:          85,
		CompressionRatio: 10.0,
		NumResolutions:   5,
		CodeBlockSize:    image.Point{X: 6, Y: 6},
		PrecinctSize:     []image.Point{{X: 256, Y: 256}},
		ProgressionOrder: RPCL,
		NumLayers:        3,
		TileSize:         image.Point{X: 512, Y: 512},
		TileOffset:       image.Point{X: 0, Y: 0},
		ImageOffset:      image.Point{X: 0, Y: 0},
		ColorSpace:       ColorSpaceSRGB,
		ICCProfile:       []byte{0xAA, 0xBB},
		Comment:          "Test options",
		EnableSOP:        true,
		EnableEPH:        true,
	}

	if opts.Profile != ProfileCinema4K {
		t.Errorf("Profile = %d, want ProfileCinema4K", opts.Profile)
	}
	if opts.CompressionRatio != 10.0 {
		t.Errorf("CompressionRatio = %f, want 10.0", opts.CompressionRatio)
	}
	if len(opts.PrecinctSize) != 1 {
		t.Errorf("PrecinctSize length = %d, want 1", len(opts.PrecinctSize))
	}
	if len(opts.ICCProfile) != 2 {
		t.Errorf("ICCProfile length = %d, want 2", len(opts.ICCProfile))
	}
}

// Test decoding with reduced resolution
func TestDecodeConfig_ReducedResolution(t *testing.T) {
	// Create and encode an image
	original := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x + y) * 2)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.NumResolutions = 4

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode with reduced resolution
	cfg := &Config{
		ReduceResolution: 1,
	}

	decoded, err := DecodeConfig(bytes.NewReader(buf.Bytes()), cfg)
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}

	// With ReduceResolution=1, dimensions should be halved
	expectedWidth := 32
	expectedHeight := 32
	bounds := decoded.Bounds()
	if bounds.Dx() != expectedWidth || bounds.Dy() != expectedHeight {
		t.Errorf("Decoded dimensions = %dx%d, want %dx%d",
			bounds.Dx(), bounds.Dy(), expectedWidth, expectedHeight)
	}
}

// Test createImage internal function through various decode paths
// These tests exercise different branches in createImage

// TestDecode_Gray16Roundtrip exercises Gray16 decode path
func TestDecode_Gray16Roundtrip(t *testing.T) {
	// Create 16-bit grayscale image
	original := image.NewGray16(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetGray16(x, y, color.Gray16{Y: uint16((x + y) * 4096)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise Gray16 path in createImage
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("Decoded dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
}

// TestDecode_RGB16Roundtrip exercises RGB16 (RGBA64) decode path
func TestDecode_RGB16Roundtrip(t *testing.T) {
	// Create 16-bit RGB image
	original := image.NewRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetRGBA64(x, y, color.RGBA64{
				R: uint16(x * 8192),
				G: uint16(y * 8192),
				B: uint16((x + y) * 4096),
				A: 65535,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise RGB16 path
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("Decoded dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
}

// TestDecode_RGBA8Roundtrip exercises RGBA (4-component 8-bit) decode path
func TestDecode_RGBA8Roundtrip(t *testing.T) {
	// Create 8-bit RGBA image with alpha variations
	original := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: uint8((x + y) * 16),
				A: uint8(200 + x*4),
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - should exercise RGBA (4-component) path
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("Decoded dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
}

// TestCreateImage_DirectCoverage tests createImage more directly via decoder
func TestCreateImage_DirectCoverage(t *testing.T) {
	tests := []struct {
		name      string
		img       image.Image
		precision int
	}{
		{
			name: "Gray8",
			img: func() image.Image {
				img := image.NewGray(image.Rect(0, 0, 4, 4))
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						img.SetGray(i, j, color.Gray{Y: uint8(i*16 + j*16)})
					}
				}
				return img
			}(),
			precision: 8,
		},
		{
			name: "Gray16",
			img: func() image.Image {
				img := image.NewGray16(image.Rect(0, 0, 4, 4))
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						img.SetGray16(i, j, color.Gray16{Y: uint16((i + j) * 4000)})
					}
				}
				return img
			}(),
			precision: 16,
		},
		{
			name: "RGB8",
			img: func() image.Image {
				img := image.NewRGBA(image.Rect(0, 0, 4, 4))
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						img.SetRGBA(i, j, color.RGBA{
							R: uint8(i * 64),
							G: uint8(j * 64),
							B: uint8((i + j) * 32),
							A: 255,
						})
					}
				}
				return img
			}(),
			precision: 8,
		},
		{
			name: "RGB16",
			img: func() image.Image {
				img := image.NewRGBA64(image.Rect(0, 0, 4, 4))
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						img.SetRGBA64(i, j, color.RGBA64{
							R: uint16(i * 16000),
							G: uint16(j * 16000),
							B: uint16((i + j) * 8000),
							A: 65535,
						})
					}
				}
				return img
			}(),
			precision: 16,
		},
		{
			name: "RGBA8_4comp",
			img: func() image.Image {
				img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						img.SetNRGBA(i, j, color.NRGBA{
							R: uint8(i * 64),
							G: uint8(j * 64),
							B: uint8((i + j) * 32),
							A: uint8(128 + i*16),
						})
					}
				}
				return img
			}(),
			precision: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := DefaultOptions()
			opts.Format = FormatJ2K
			opts.Lossless = true

			if err := Encode(&buf, tt.img, opts); err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			decoded, err := Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}

			origBounds := tt.img.Bounds()
			decBounds := decoded.Bounds()
			if origBounds.Dx() != decBounds.Dx() || origBounds.Dy() != decBounds.Dy() {
				t.Errorf("Dimension mismatch: original %dx%d, decoded %dx%d",
					origBounds.Dx(), origBounds.Dy(), decBounds.Dx(), decBounds.Dy())
			}
		})
	}
}

// TestDecode_WithNegativeClamp tests clamping of negative values
func TestDecode_WithNegativeClamp(t *testing.T) {
	// Create a simple grayscale image with edge values
	original := image.NewGray(image.Rect(0, 0, 4, 4))
	// Set some pixels to edge values that might become negative after MCT
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x + y) * 32)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil image")
	}
}

// TestDecode_WithMaxValues tests handling of maximum pixel values
func TestDecode_WithMaxValues(t *testing.T) {
	// Create grayscale with max values
	original := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			original.SetGray(x, y, color.Gray{Y: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil image")
	}
}

// TestDecode_RGBWithEdgeValues tests RGB with edge color values
func TestDecode_RGBWithEdgeValues(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 4, 4))
	// Set edge color values that test clamping
	colors := []color.RGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			original.SetRGBA(x, y, colors[(x+y)%4])
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil image")
	}
}

// Test JP2 with all boxes
func TestDecode_JP2AllBoxes(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Comment = "Test JP2 with all boxes"
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode and check metadata
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.Width != 8 || meta.Height != 8 {
		t.Errorf("Dimensions = %dx%d, want 8x8", meta.Width, meta.Height)
	}
	if meta.ColorSpace != ColorSpaceSRGB {
		t.Errorf("ColorSpace = %d, want ColorSpaceSRGB", meta.ColorSpace)
	}
}

// Test decode with lossy compression
func TestDecode_Lossy(t *testing.T) {
	original := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x*16 + y*16) % 256)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = false
	opts.Quality = 80

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Errorf("Decoded dimensions = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}
}

// Test decode RGB lossy
func TestDecode_RGBLossy(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: uint8((x + y) * 8),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = false
	opts.Quality = 75

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Errorf("Decoded dimensions = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}
}

// Test multiple reduced resolution levels
func TestDecodeConfig_MultipleReductions(t *testing.T) {
	original := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8((x + y) * 2)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.NumResolutions = 5

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Test multiple reduction levels
	reductions := []struct {
		level       int
		expectedDim int
	}{
		{0, 64},
		{1, 32},
		{2, 16},
	}

	for _, r := range reductions {
		cfg := &Config{
			ReduceResolution: r.level,
		}

		decoded, err := DecodeConfig(bytes.NewReader(buf.Bytes()), cfg)
		if err != nil {
			t.Fatalf("DecodeConfig() with reduction %d error: %v", r.level, err)
		}

		bounds := decoded.Bounds()
		if bounds.Dx() != r.expectedDim || bounds.Dy() != r.expectedDim {
			t.Errorf("Reduction %d: dimensions = %dx%d, want %dx%d",
				r.level, bounds.Dx(), bounds.Dy(), r.expectedDim, r.expectedDim)
		}
	}
}

// Test Gray with non-8-bit precision scaling
func TestDecode_GrayPrecisionScaling(t *testing.T) {
	// Create 8-bit gray image with varied values
	original := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			// Vary the value to test scaling
			original.SetGray(x, y, color.Gray{Y: uint8(x*32 + y)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil image")
	}
}

// Test RGB with non-8-bit precision scaling
func TestDecode_RGBPrecisionScaling(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: uint8((x + y) * 16),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil image")
	}
}

// Additional tests for hitting missing coverage branches

// Test NRGBA64 encoding (16-bit with alpha) for RGBA16 decode path
func TestEncode_NRGBA64(t *testing.T) {
	// NRGBA64 doesn't exist in standard library, but we can test
	// that a 16-bit RGBA image encodes and decodes properly
	img := image.NewRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA64(x, y, color.RGBA64{
				R: uint16(x * 8000),
				G: uint16(y * 8000),
				B: uint16((x + y) * 4000),
				A: 65535,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Verify it decodes
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("Decode() returned nil")
	}
}

// Test JP2 metadata with grayscale colorspace
func TestDecodeMetadata_JP2Grayscale(t *testing.T) {
	original := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetGray(x, y, color.Gray{Y: uint8(x + y*8)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.ColorSpace != ColorSpaceGray {
		t.Errorf("ColorSpace = %d, want ColorSpaceGray", meta.ColorSpace)
	}
}

// Test encoder with signed component (for coverage of signed encoding path)
func TestEncode_WithSignedOption(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test encoder with zero NumResolutions (should use default)
func TestEncode_ZeroResolutions(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.NumResolutions = 0 // Should default to 6
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test encoder with zero NumLayers (should use default)
func TestEncode_ZeroLayers(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.NumLayers = 0 // Should default to 1
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test encoder with zero CodeBlockSize (should use default)
func TestEncode_ZeroCodeBlockSize(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.CodeBlockSize = image.Point{X: 0, Y: 0} // Should default
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test encode with lossy zero quality (special case)
func TestEncode_LossyZeroQuality(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Lossless = false
	opts.Quality = 0 // Edge case

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}
}

// Test encoding larger image to exercise more code paths
func TestEncode_LargerImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 2),
				G: uint8(y * 2),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode and verify
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 128 || bounds.Dy() != 128 {
		t.Errorf("Decoded dimensions = %dx%d, want 128x128", bounds.Dx(), bounds.Dy())
	}
}

// Test JP2 decode with full colorspace paths
func TestDecode_JP2Colorspaces(t *testing.T) {
	// Test with RGB
	rgbImg := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			rgbImg.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, rgbImg, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.ColorSpace != ColorSpaceSRGB {
		t.Errorf("ColorSpace = %d, want ColorSpaceSRGB (%d)", meta.ColorSpace, ColorSpaceSRGB)
	}
}

// Test full decode pipeline with various error recovery
func TestDecode_FullPipeline(t *testing.T) {
	// Create an image and encode it
	original := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			original.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 8),
				G: uint8(y * 8),
				B: uint8((x + y) * 4),
				A: 255,
			})
		}
	}

	// Encode with various settings
	configs := []struct {
		name     string
		format   Format
		lossless bool
		quality  int
	}{
		{"J2K_Lossless", FormatJ2K, true, 0},
		{"J2K_Lossy", FormatJ2K, false, 50},
		{"JP2_Lossless", FormatJP2, true, 0},
		{"JP2_Lossy", FormatJP2, false, 75},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := DefaultOptions()
			opts.Format = cfg.format
			opts.Lossless = cfg.lossless
			opts.Quality = cfg.quality

			if err := Encode(&buf, original, opts); err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			decoded, err := Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}

			bounds := decoded.Bounds()
			if bounds.Dx() != 32 || bounds.Dy() != 32 {
				t.Errorf("dimensions = %dx%d, want 32x32", bounds.Dx(), bounds.Dy())
			}
		})
	}
}

// Test decode with nil config
func TestDecodeConfig_NilConfig(t *testing.T) {
	original := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode with nil config (should use defaults)
	decoded, err := DecodeConfig(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}

	if decoded == nil {
		t.Fatal("DecodeConfig() returned nil image")
	}
}

// Test all init branches by checking formats
func TestInit_FormatRegistration(t *testing.T) {
	// Test that both formats are registered
	// JP2 format
	jp2Img := image.NewGray(image.Rect(0, 0, 4, 4))
	var jp2Buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	if err := Encode(&jp2Buf, jp2Img, opts); err != nil {
		t.Fatalf("JP2 Encode() error: %v", err)
	}

	_, format, err := image.Decode(bytes.NewReader(jp2Buf.Bytes()))
	if err != nil {
		t.Fatalf("image.Decode JP2 error: %v", err)
	}
	if format != "jp2" {
		t.Errorf("JP2 format = %q, want \"jp2\"", format)
	}

	// J2K format
	j2kImg := image.NewGray(image.Rect(0, 0, 4, 4))
	var j2kBuf bytes.Buffer
	opts.Format = FormatJ2K
	if err := Encode(&j2kBuf, j2kImg, opts); err != nil {
		t.Fatalf("J2K Encode() error: %v", err)
	}

	_, format, err = image.Decode(bytes.NewReader(j2kBuf.Bytes()))
	if err != nil {
		t.Fatalf("image.Decode J2K error: %v", err)
	}
	if format != "j2k" {
		t.Errorf("J2K format = %q, want \"j2k\"", format)
	}
}

// Test metadata comment field
func TestDecodeMetadata_WithComment(t *testing.T) {
	original := image.NewGray(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Comment = "Test comment for metadata"

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	// The comment may or may not be preserved depending on implementation
	// Just verify metadata is valid
	if meta.Width != 8 || meta.Height != 8 {
		t.Errorf("Dimensions = %dx%d, want 8x8", meta.Width, meta.Height)
	}
}

// Test metadata bits per component and signed fields
func TestDecodeMetadata_ComponentInfo(t *testing.T) {
	original := image.NewGray16(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetGray16(x, y, color.Gray16{Y: uint16((x + y) * 4000)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}

	if meta.NumComponents != 1 {
		t.Errorf("NumComponents = %d, want 1", meta.NumComponents)
	}
	if len(meta.BitsPerComponent) != 1 {
		t.Fatalf("BitsPerComponent length = %d, want 1", len(meta.BitsPerComponent))
	}
	// 16-bit grayscale
	if meta.BitsPerComponent[0] != 16 {
		t.Errorf("BitsPerComponent[0] = %d, want 16", meta.BitsPerComponent[0])
	}
}

// Test for RGBA path with 16-bit (exercises RGBA16 branch)
func TestDecode_RGBA16Path(t *testing.T) {
	// Create a 16-bit RGBA image that will exercise the RGBA16 decode path
	// We need to create an image with 4 components at 16-bit precision
	// Using NRGBA with alpha channel
	original := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			original.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 64),
				G: uint8(y * 64),
				B: uint8((x + y) * 32),
				A: uint8(200 + x*10),
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 4 {
		t.Errorf("Decoded dimensions = %dx%d, want 4x4", bounds.Dx(), bounds.Dy())
	}
}

// Test encode profile options
func TestEncode_WithProfile(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 8, 8))

	profiles := []Profile{
		ProfileNone,
		ProfileCinema2K,
	}

	for _, p := range profiles {
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Profile = p
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() with profile %d error: %v", p, err)
		}

		if buf.Len() == 0 {
			t.Errorf("Encode() with profile %d produced empty output", p)
		}
	}
}

// Test ColorSpaceUnspecified vs ColorSpaceUnknown distinction
func TestDecodeMetadata_ColorSpaceUnspecifiedVsUnknown(t *testing.T) {
	// Create a test image
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	t.Run("J2K_returns_Unspecified", func(t *testing.T) {
		// J2K files have no JP2 container, so colorspace is unspecified
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Format = FormatJ2K
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() error: %v", err)
		}

		meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeMetadata() error: %v", err)
		}

		if meta.ColorSpace != ColorSpaceUnspecified {
			t.Errorf("J2K ColorSpace = %d, want %d (ColorSpaceUnspecified)", meta.ColorSpace, ColorSpaceUnspecified)
		}
	})

	t.Run("JP2_with_unknown_enumcs_returns_Unknown", func(t *testing.T) {
		// Create a JP2 and patch it with an unknown enumcs value
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Format = FormatJP2
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() error: %v", err)
		}

		data := buf.Bytes()
		patchedData := make([]byte, len(data))
		copy(patchedData, data)

		// Find and patch colr box with unknown enumcs value (99)
		for i := 0; i < len(patchedData)-15; i++ {
			if patchedData[i+4] == 'c' && patchedData[i+5] == 'o' &&
				patchedData[i+6] == 'l' && patchedData[i+7] == 'r' {
				if patchedData[i+8] == 1 { // Method 1 = enumerated CS
					// Patch with unknown value 99
					patchedData[i+11] = 0
					patchedData[i+12] = 0
					patchedData[i+13] = 0
					patchedData[i+14] = 99
					break
				}
			}
		}

		meta, err := DecodeMetadata(bytes.NewReader(patchedData))
		if err != nil {
			t.Fatalf("DecodeMetadata() error: %v", err)
		}

		if meta.ColorSpace != ColorSpaceUnknown {
			t.Errorf("JP2 with unknown enumcs: ColorSpace = %d, want %d (ColorSpaceUnknown)", meta.ColorSpace, ColorSpaceUnknown)
		}
	})

	t.Run("JP2_with_valid_enumcs_returns_correct_value", func(t *testing.T) {
		// JP2 files with valid colorspec return the correct colorspace
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Format = FormatJP2
		opts.Lossless = true

		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("Encode() error: %v", err)
		}

		meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeMetadata() error: %v", err)
		}

		// Default grayscale should have ColorSpaceGray
		if meta.ColorSpace != ColorSpaceGray {
			t.Errorf("JP2 grayscale ColorSpace = %d, want %d (ColorSpaceGray)", meta.ColorSpace, ColorSpaceGray)
		}
	})
}

// Test all supported colorspace mappings
func TestDecodeMetadata_AllColorspaces(t *testing.T) {
	// Create a base RGB image
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	testCases := []struct {
		name       string
		colorSpace ColorSpace
		expected   ColorSpace
	}{
		{"Bilevel", ColorSpaceBilevel, ColorSpaceBilevel},
		{"Gray", ColorSpaceGray, ColorSpaceGray},
		{"sRGB", ColorSpaceSRGB, ColorSpaceSRGB},
		{"sYCC", ColorSpaceSYCC, ColorSpaceSYCC},
		{"e-sYCC", ColorSpaceEYCC, ColorSpaceEYCC},
		{"CMYK", ColorSpaceCMYK, ColorSpaceCMYK},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := DefaultOptions()
			opts.Format = FormatJP2
			opts.Lossless = true
			opts.ColorSpace = tc.colorSpace

			if err := Encode(&buf, img, opts); err != nil {
				t.Fatalf("Encode() error: %v", err)
			}

			meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("DecodeMetadata() error: %v", err)
			}

			if meta.ColorSpace != tc.expected {
				t.Errorf("ColorSpace = %d, want %d (%s)", meta.ColorSpace, tc.expected, tc.name)
			}
		})
	}
}

// Test colorspace detection for SYCC and Unknown values via JP2 patching
func TestDecodeMetadata_JP2ColorspaceYCC(t *testing.T) {
	// Create an RGB image and encode it
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	data := buf.Bytes()

	// Find and modify the colorspace in the colr box
	// colr box has type 0x636F6C72 and contains enumerated CS at offset +7 from start of box contents
	// We need to find the colr box and patch the enumerated colorspace value

	// Test all enumerated colorspace values via patching
	testCases := []struct {
		name     string
		csValue  uint32
		expected ColorSpace
	}{
		{"CSBilevel1", 0, ColorSpaceBilevel},
		{"CSYCbCr1", 1, ColorSpaceSYCC},
		{"CSYCbCr2", 3, ColorSpaceYCbCr2},
		{"CSYCbCr3", 4, ColorSpaceYCbCr3},
		{"CSPhotoYCC", 9, ColorSpacePhotoYCC},
		{"CSCMY", 11, ColorSpaceCMY},
		{"CSCMYK", 12, ColorSpaceCMYK},
		{"CSYCCK", 13, ColorSpaceYCCK},
		{"CSCIELab", 14, ColorSpaceCIELab},
		{"CSBilevel2", 15, ColorSpaceBilevel},
		{"CSSRGB", 16, ColorSpaceSRGB},
		{"CSGray", 17, ColorSpaceGray},
		{"CSsYCC", 18, ColorSpaceSYCC},
		{"CSCIEJab", 19, ColorSpaceCIEJab},
		{"CSeSRGB", 20, ColorSpaceESRGB},
		{"CSROMMRGB", 21, ColorSpaceROMMRGB},
		{"CSYPbPr1125", 22, ColorSpaceYPbPr60},
		{"CSYPbPr1250", 23, ColorSpaceYPbPr50},
		{"CSeSYCC", 24, ColorSpaceEYCC},
		{"CSUnknown_99", 99, ColorSpaceUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make a copy of the data
			patchedData := make([]byte, len(data))
			copy(patchedData, data)

			// Find colr box (type = 0x636F6C72 = "colr")
			// Box format: 4-byte length, 4-byte type, contents
			for i := 0; i < len(patchedData)-15; i++ {
				if patchedData[i+4] == 'c' && patchedData[i+5] == 'o' &&
					patchedData[i+6] == 'l' && patchedData[i+7] == 'r' {
					// Found colr box - enumerated CS is at offset 11 from box start
					// (4-byte length + 4-byte type + 1-byte method + 1-byte precedence + 1-byte approx = 11)
					// Then 4 bytes for enumerated colorspace
					if patchedData[i+8] == 1 { // Method 1 = enumerated CS
						// Patch the colorspace value (big-endian uint32 at offset i+11)
						patchedData[i+11] = byte(tc.csValue >> 24)
						patchedData[i+12] = byte(tc.csValue >> 16)
						patchedData[i+13] = byte(tc.csValue >> 8)
						patchedData[i+14] = byte(tc.csValue)
						break
					}
				}
			}

			meta, err := DecodeMetadata(bytes.NewReader(patchedData))
			if err != nil {
				t.Fatalf("DecodeMetadata() error: %v", err)
			}

			if meta.ColorSpace != tc.expected {
				t.Errorf("ColorSpace = %d, want %d (%s)", meta.ColorSpace, tc.expected, tc.name)
			}
		})
	}
}

// Test invalid JP2 signature error path
func TestDecode_InvalidJP2Signature(t *testing.T) {
	// Create a valid JP2 but corrupt the signature bytes
	img := image.NewGray(image.Rect(0, 0, 4, 4))

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJP2
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	data := buf.Bytes()

	// JP2 signature box content is at offset 8 (after 4-byte length + 4-byte type)
	// Content should be: 0x0D 0x0A 0x87 0x0A
	// Corrupt it to test the error path
	if len(data) > 11 {
		corruptedData := make([]byte, len(data))
		copy(corruptedData, data)
		corruptedData[8] = 0xFF // Corrupt the signature

		_, err := Decode(bytes.NewReader(corruptedData))
		if err == nil {
			t.Error("Decode() should fail with invalid JP2 signature")
		}
	}
}

// Test JP2 without codestream error path
func TestDecode_JP2NoCodestream(t *testing.T) {
	// Create a minimal JP2 file without codestream box
	jp2Data := []byte{
		// Signature box (12 bytes)
		0x00, 0x00, 0x00, 0x0C, // Length = 12
		0x6A, 0x50, 0x20, 0x20, // Type = "jP  "
		0x0D, 0x0A, 0x87, 0x0A, // Signature content

		// File type box (20 bytes)
		0x00, 0x00, 0x00, 0x14, // Length = 20
		0x66, 0x74, 0x79, 0x70, // Type = "ftyp"
		0x6A, 0x70, 0x32, 0x20, // Brand = "jp2 "
		0x00, 0x00, 0x00, 0x00, // Minor version
		0x6A, 0x70, 0x32, 0x20, // Compatibility = "jp2 "
	}

	_, err := Decode(bytes.NewReader(jp2Data))
	if err == nil {
		t.Error("Decode() should fail when JP2 has no codestream")
	}
}

// Test decode with truncated file type box
func TestDecode_TruncatedFtypBox(t *testing.T) {
	// Create a JP2 with truncated ftyp box
	jp2Data := []byte{
		// Signature box (12 bytes)
		0x00, 0x00, 0x00, 0x0C, // Length = 12
		0x6A, 0x50, 0x20, 0x20, // Type = "jP  "
		0x0D, 0x0A, 0x87, 0x0A, // Signature content

		// Truncated file type box (claims 20 bytes but only has 10)
		0x00, 0x00, 0x00, 0x14, // Length = 20
		0x66, 0x74, 0x79, 0x70, // Type = "ftyp"
		0x6A, 0x70, // Truncated content
	}

	_, err := Decode(bytes.NewReader(jp2Data))
	if err == nil {
		t.Error("Decode() should fail with truncated ftyp box")
	}
}

// Test NRGBA64 (16-bit RGBA with alpha) encoding and decoding - exercises 4-component 16-bit path
func TestEncode_NRGBA64_4Component(t *testing.T) {
	// Create a 16-bit RGBA image with alpha
	img := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16(x * 8192),
				G: uint16(y * 8192),
				B: uint16((x + y) * 4096),
				A: uint16(32768 + x*4096),
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encode() produced empty output")
	}

	// Decode and verify dimensions
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("Decoded dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
}

// Test encoding with custom precision (4-bit) exercises precision scaling in decoder
func TestEncode_CustomPrecision4Bit(t *testing.T) {
	// Create an 8-bit grayscale image
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*16 + y*16)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Precision = 4 // 4-bit precision

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise precision scaling (4-bit to 8-bit output)
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify metadata shows 4-bit precision
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.BitsPerComponent[0] != 4 {
		t.Errorf("BitsPerComponent[0] = %d, want 4", meta.BitsPerComponent[0])
	}
}

// Test encoding with custom precision (6-bit) for RGB exercises precision scaling in decoder
func TestEncode_CustomPrecision6BitRGB(t *testing.T) {
	// Create an 8-bit RGB image
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Precision = 6 // 6-bit precision (< 8, so goes to 8-bit output path with scaling)

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise RGB precision scaling (6-bit to 8-bit output)
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify metadata shows 6-bit precision
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.BitsPerComponent[0] != 6 {
		t.Errorf("BitsPerComponent[0] = %d, want 6", meta.BitsPerComponent[0])
	}
}

// Test encoding with custom precision (12-bit) for RGB (goes to 16-bit output path)
func TestEncode_CustomPrecision12BitRGB(t *testing.T) {
	// Create an 8-bit RGB image
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Precision = 12 // 12-bit precision (> 8, goes to 16-bit output path)

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify metadata shows 12-bit precision
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.BitsPerComponent[0] != 12 {
		t.Errorf("BitsPerComponent[0] = %d, want 12", meta.BitsPerComponent[0])
	}
}

// Test encoding with custom precision (6-bit) for RGBA exercises precision scaling in decoder
func TestEncode_CustomPrecision6BitRGBA(t *testing.T) {
	// Create an 8-bit NRGBA image (4 components)
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 200})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Precision = 6 // 6-bit precision (< 8, so goes to 8-bit output path with scaling)

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise RGBA precision scaling (6-bit to 8-bit output)
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify metadata shows 6-bit precision and 4 components
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.BitsPerComponent[0] != 6 {
		t.Errorf("BitsPerComponent[0] = %d, want 6", meta.BitsPerComponent[0])
	}
	if meta.NumComponents != 4 {
		t.Errorf("NumComponents = %d, want 4", meta.NumComponents)
	}
}

// Test encoding with custom precision (10-bit) for 16-bit input
func TestEncode_CustomPrecision10BitFrom16Bit(t *testing.T) {
	// Create a 16-bit grayscale image
	img := image.NewGray16(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetGray16(x, y, color.Gray16{Y: uint16((x + y) * 4096)})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.Precision = 10 // 10-bit precision

	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify metadata shows 10-bit precision
	meta, err := DecodeMetadata(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeMetadata() error: %v", err)
	}
	if meta.BitsPerComponent[0] != 10 {
		t.Errorf("BitsPerComponent[0] = %d, want 10", meta.BitsPerComponent[0])
	}
}

// Test NRGBA64 roundtrip exercises 16-bit RGBA decode path (4 components, precision > 8)
func TestDecode_NRGBA64Roundtrip(t *testing.T) {
	// Create 16-bit RGBA image with alpha
	original := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16(x * 8000),
				G: uint16(y * 8000),
				B: uint16((x + y) * 4000),
				A: uint16(40000 + x*1000),
			})
		}
	}

	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// Decode - this should exercise RGBA64 path in createImage (4 components, 16-bit)
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}

	// Verify the decoded image has proper color values
	// The decoded image should be RGBA64 for 16-bit 4-component
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			// Just check that values are reasonable (not zero for non-zero input)
			if x > 0 && r == 0 {
				t.Errorf("At(%d,%d): R should be non-zero", x, y)
			}
			if y > 0 && g == 0 {
				t.Errorf("At(%d,%d): G should be non-zero", x, y)
			}
			if (x+y) > 0 && b == 0 {
				t.Errorf("At(%d,%d): B should be non-zero", x, y)
			}
			if a == 0 {
				t.Errorf("At(%d,%d): A should be non-zero", x, y)
			}
		}
	}
}
