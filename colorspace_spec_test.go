// Spec-based colorspace conversion tests
//
// These tests use reference values derived from the ISO/IEC 15444-1 specification
// and standard color science formulas to verify correctness.

package jpeg2000

import (
	"math"
	"testing"
)

// TestSpecYCbCrToRGB tests YCbCr conversions against ITU-R BT.601/709 spec values.
func TestSpecYCbCrToRGB(t *testing.T) {
	// Test vectors for 8-bit YCbCr to RGB conversion
	// Based on ITU-R BT.601-5 and BT.709-5 specifications
	//
	// For BT.601: R = Y + 1.402*(Cr-128)
	//             G = Y - 0.344136*(Cb-128) - 0.714136*(Cr-128)
	//             B = Y + 1.772*(Cb-128)
	//
	// For BT.709: R = Y + 1.5748*(Cr-128)
	//             G = Y - 0.1873*(Cb-128) - 0.4681*(Cr-128)
	//             B = Y + 1.8556*(Cb-128)

	tests := []struct {
		name       string
		conv       colorConversion
		y, cb, cr  int32 // Input YCbCr
		r, g, b    int32 // Expected RGB (approximate)
		tolerance  int32
	}{
		// BT.709 (sYCC) - neutral gray
		{"sYCC_gray", convertSYCCToRGB, 128, 128, 128, 128, 128, 128, 2},
		// BT.709 - black
		{"sYCC_black", convertSYCCToRGB, 0, 128, 128, 0, 0, 0, 2},
		// BT.709 - white
		{"sYCC_white", convertSYCCToRGB, 255, 128, 128, 255, 255, 255, 2},
		// BT.709 - pure red: R=255, G=0, B=0
		// Y = 0.2126*255 = 54.2, Cb = 128 - 0.1146*255 = 98.8, Cr = 128 + 0.5*255 = 255.5
		{"sYCC_red", convertSYCCToRGB, 54, 99, 255, 255, 0, 0, 15},
		// BT.709 - pure green: R=0, G=255, B=0
		// Y = 0.7152*255 = 182.4, Cb = 128 - 0.3854*255 = 29.7, Cr = 128 - 0.4542*255 = 12.2
		{"sYCC_green", convertSYCCToRGB, 182, 30, 12, 0, 255, 0, 15},
		// BT.709 - pure blue: R=0, G=0, B=255
		// Y = 0.0722*255 = 18.4, Cb = 128 + 0.5*255 = 255.5, Cr = 128 - 0.0458*255 = 116.3
		{"sYCC_blue", convertSYCCToRGB, 18, 255, 116, 0, 0, 255, 15},

		// BT.601 - neutral gray
		{"BT601_gray", convertYCbCr601ToRGB, 128, 128, 128, 128, 128, 128, 2},
		// BT.601 - black
		{"BT601_black", convertYCbCr601ToRGB, 0, 128, 128, 0, 0, 0, 2},
		// BT.601 - white
		{"BT601_white", convertYCbCr601ToRGB, 255, 128, 128, 255, 255, 255, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.y}, {tt.cb}, {tt.cr}}
			tt.conv(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if abs32(r-tt.r) > tt.tolerance {
				t.Errorf("R = %d, want %d (±%d)", r, tt.r, tt.tolerance)
			}
			if abs32(g-tt.g) > tt.tolerance {
				t.Errorf("G = %d, want %d (±%d)", g, tt.g, tt.tolerance)
			}
			if abs32(b-tt.b) > tt.tolerance {
				t.Errorf("B = %d, want %d (±%d)", b, tt.b, tt.tolerance)
			}
		})
	}
}

// TestSpecCMYToRGB tests CMY conversion against the standard formula.
func TestSpecCMYToRGB(t *testing.T) {
	// CMY to RGB: R = 255 - C, G = 255 - M, B = 255 - Y
	// This is the exact formula, no tolerance needed.

	tests := []struct {
		name      string
		c, m, y   int32 // Input CMY
		r, g, b   int32 // Expected RGB
	}{
		{"white", 0, 0, 0, 255, 255, 255},
		{"black", 255, 255, 255, 0, 0, 0},
		{"red", 0, 255, 255, 255, 0, 0},
		{"green", 255, 0, 255, 0, 255, 0},
		{"blue", 255, 255, 0, 0, 0, 255},
		{"cyan", 255, 0, 0, 0, 255, 255},
		{"magenta", 0, 255, 0, 255, 0, 255},
		{"yellow", 0, 0, 255, 255, 255, 0},
		{"gray", 128, 128, 128, 127, 127, 127},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.c}, {tt.m}, {tt.y}}
			convertCMYToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if r != tt.r {
				t.Errorf("R = %d, want %d", r, tt.r)
			}
			if g != tt.g {
				t.Errorf("G = %d, want %d", g, tt.g)
			}
			if b != tt.b {
				t.Errorf("B = %d, want %d", b, tt.b)
			}
		})
	}
}

// TestSpecCMYKToRGB tests CMYK conversion against the standard formula.
func TestSpecCMYKToRGB(t *testing.T) {
	// CMYK to RGB: R = (1-C)*(1-K)*255, G = (1-M)*(1-K)*255, B = (1-Y)*(1-K)*255

	tests := []struct {
		name        string
		c, m, y, k  int32 // Input CMYK (0-255)
		r, g, b     int32 // Expected RGB
		tolerance   int32
	}{
		{"white", 0, 0, 0, 0, 255, 255, 255, 1},
		{"black_via_K", 0, 0, 0, 255, 0, 0, 0, 1},
		{"black_via_CMY", 255, 255, 255, 0, 0, 0, 0, 1},
		{"red", 0, 255, 255, 0, 255, 0, 0, 1},
		{"50%_gray", 0, 0, 0, 128, 127, 127, 127, 2},
		{"dark_red", 0, 255, 255, 128, 127, 0, 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.c}, {tt.m}, {tt.y}, {tt.k}}
			convertCMYKToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if abs32(r-tt.r) > tt.tolerance {
				t.Errorf("R = %d, want %d (±%d)", r, tt.r, tt.tolerance)
			}
			if abs32(g-tt.g) > tt.tolerance {
				t.Errorf("G = %d, want %d (±%d)", g, tt.g, tt.tolerance)
			}
			if abs32(b-tt.b) > tt.tolerance {
				t.Errorf("B = %d, want %d (±%d)", b, tt.b, tt.tolerance)
			}
		})
	}
}

// TestSpecCIELabToRGB tests CIE L*a*b* to RGB conversion.
func TestSpecCIELabToRGB(t *testing.T) {
	// CIE L*a*b* test values
	// L* = 0-100 (encoded as 0-255)
	// a* = -128 to 127 (encoded as 0-255 with 128 offset)
	// b* = -128 to 127 (encoded as 0-255 with 128 offset)
	//
	// Reference values from standard color science:
	// D50 white point: L*=100, a*=0, b*=0 -> R≈255, G≈255, B≈255
	// D50 black: L*=0, a*=0, b*=0 -> R=0, G=0, B=0

	tests := []struct {
		name      string
		L, a, b   int32 // Encoded L*a*b* (8-bit)
		minR      int32 // Minimum expected R
		maxR      int32 // Maximum expected R
		isGray    bool  // Should R≈G≈B?
	}{
		// Black: L*=0
		{"black", 0, 128, 128, 0, 5, true},
		// White: L*=100 (encoded as 255)
		// Note: D50->D65 chromatic adaptation causes slight color shift
		{"white", 255, 128, 128, 250, 255, false}, // Not perfectly gray due to adaptation
		// Mid gray: L*=50 (encoded as 128)
		{"midgray", 128, 128, 128, 80, 140, true},
		// Red-ish: positive a*
		{"red_shift", 128, 200, 128, 100, 255, false},
		// Blue-ish: negative b* - can cause clipping to 0
		{"blue_shift", 128, 128, 50, 0, 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.L}, {tt.a}, {tt.b}}
			convertCIELabToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if r < tt.minR || r > tt.maxR {
				t.Errorf("R = %d, want [%d, %d]", r, tt.minR, tt.maxR)
			}

			if tt.isGray {
				// For neutral Lab values, RGB should be approximately equal
				if abs32(r-g) > 25 || abs32(g-b) > 25 {
					t.Errorf("Expected gray, got R=%d, G=%d, B=%d", r, g, b)
				}
			}
		})
	}
}

// TestSpecYPbPrToRGB tests YPbPr (HD video) to RGB conversion.
func TestSpecYPbPrToRGB(t *testing.T) {
	// YPbPr uses the same matrix as sYCC (ITU-R BT.709)
	// This is used for HD video signals (1080i/p, 720p)

	tests := []struct {
		name       string
		y, pb, pr  int32
		r, g, b    int32
		tolerance  int32
	}{
		{"gray", 128, 128, 128, 128, 128, 128, 2},
		{"black", 0, 128, 128, 0, 0, 0, 2},
		{"white", 255, 128, 128, 255, 255, 255, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.y}, {tt.pb}, {tt.pr}}
			convertYPbPr709ToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if abs32(r-tt.r) > tt.tolerance {
				t.Errorf("R = %d, want %d (±%d)", r, tt.r, tt.tolerance)
			}
			if abs32(g-tt.g) > tt.tolerance {
				t.Errorf("G = %d, want %d (±%d)", g, tt.g, tt.tolerance)
			}
			if abs32(b-tt.b) > tt.tolerance {
				t.Errorf("B = %d, want %d (±%d)", b, tt.b, tt.tolerance)
			}
		})
	}
}

// TestSpecPhotoYCCToRGB tests Kodak PhotoYCC conversion.
func TestSpecPhotoYCCToRGB(t *testing.T) {
	// PhotoYCC uses a modified YCbCr with different scaling
	// Y is in [0, 255*1.402], C1/C2 are offset at 156

	tests := []struct {
		name       string
		y, c1, c2  int32
		minR, maxR int32
		isNeutral  bool
	}{
		// Neutral (Y at mid, C1=C2=156)
		{"neutral", 128, 156, 156, 100, 160, true},
		// Black
		{"black", 0, 156, 156, 0, 20, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.y}, {tt.c1}, {tt.c2}}
			convertPhotoYCCToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			if r < tt.minR || r > tt.maxR {
				t.Errorf("R = %d, want [%d, %d]", r, tt.minR, tt.maxR)
			}

			if tt.isNeutral && (abs32(r-g) > 20 || abs32(g-b) > 20) {
				t.Errorf("Expected neutral, got R=%d, G=%d, B=%d", r, g, b)
			}
		})
	}
}

// TestSpecROMMRGBToRGB tests ROMM-RGB (ProPhoto RGB) to sRGB conversion.
func TestSpecROMMRGBToRGB(t *testing.T) {
	// ROMM-RGB has a wider gamut than sRGB
	// Some ROMM colors are outside the sRGB gamut and will be clipped

	tests := []struct {
		name      string
		rIn, gIn, bIn int32
		isValid   bool // Is within sRGB gamut?
	}{
		{"black", 0, 0, 0, true},
		{"white", 255, 255, 255, true},
		{"midgray", 128, 128, 128, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			componentData := [][]int32{{tt.rIn}, {tt.gIn}, {tt.bIn}}
			convertROMMRGBToRGB(componentData, 8)

			r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

			// All outputs should be valid 8-bit values
			if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
				t.Errorf("Invalid RGB: R=%d, G=%d, B=%d", r, g, b)
			}
		})
	}
}

// TestSpecExtendedColorspaces tests e-sRGB and e-sYCC extended gamut handling.
func TestSpecExtendedColorspaces(t *testing.T) {
	t.Run("eSRGB_neutral", func(t *testing.T) {
		// Mid-range e-sRGB should map to approximately mid sRGB
		componentData := [][]int32{{128}, {128}, {128}}
		convertESRGBToRGB(componentData, 8)

		r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]
		// Should produce valid sRGB in mid-range
		if r < 50 || r > 200 || g < 50 || g > 200 || b < 50 || b > 200 {
			t.Errorf("Unexpected output: R=%d, G=%d, B=%d", r, g, b)
		}
	})

	t.Run("eYCC_neutral", func(t *testing.T) {
		// Neutral e-sYCC (Y=128, Cb=Cr=128) should give gray
		componentData := [][]int32{{128}, {128}, {128}}
		convertEYCCToRGB(componentData, 8)

		r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]
		if abs32(r-128) > 5 || abs32(g-128) > 5 || abs32(b-128) > 5 {
			t.Errorf("Expected gray ~128, got R=%d, G=%d, B=%d", r, g, b)
		}
	})
}

// TestSpec16BitPrecision tests colorspace conversions at 16-bit precision.
func TestSpec16BitPrecision(t *testing.T) {
	// 16-bit versions of the same tests
	precision := 16
	halfVal := int32(32768) // 2^15
	maxVal := int32(65535)  // 2^16 - 1

	t.Run("sYCC_gray_16bit", func(t *testing.T) {
		componentData := [][]int32{{halfVal}, {halfVal}, {halfVal}}
		convertSYCCToRGB(componentData, precision)

		r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]
		tolerance := int32(200)
		if abs32(r-halfVal) > tolerance || abs32(g-halfVal) > tolerance || abs32(b-halfVal) > tolerance {
			t.Errorf("Expected ~32768, got R=%d, G=%d, B=%d", r, g, b)
		}
	})

	t.Run("CMY_white_16bit", func(t *testing.T) {
		componentData := [][]int32{{0}, {0}, {0}}
		convertCMYToRGB(componentData, precision)

		r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]
		if r != maxVal || g != maxVal || b != maxVal {
			t.Errorf("Expected 65535, got R=%d, G=%d, B=%d", r, g, b)
		}
	})

	t.Run("CMYK_black_16bit", func(t *testing.T) {
		componentData := [][]int32{{0}, {0}, {0}, {maxVal}}
		convertCMYKToRGB(componentData, precision)

		r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]
		if r != 0 || g != 0 || b != 0 {
			t.Errorf("Expected 0, got R=%d, G=%d, B=%d", r, g, b)
		}
	})
}

// TestSpecRoundTrip tests that colorspace conversions are mathematically consistent.
func TestSpecRoundTrip(t *testing.T) {
	// For CMY, the conversion is exact and reversible
	t.Run("CMY_roundtrip", func(t *testing.T) {
		for c := int32(0); c <= 255; c += 51 { // Test several values
			for m := int32(0); m <= 255; m += 51 {
				for y := int32(0); y <= 255; y += 51 {
					componentData := [][]int32{{c}, {m}, {y}}
					convertCMYToRGB(componentData, 8)

					r, g, b := componentData[0][0], componentData[1][0], componentData[2][0]

					// Reverse: CMY = 255 - RGB
					cBack := 255 - r
					mBack := 255 - g
					yBack := 255 - b

					if cBack != c || mBack != m || yBack != y {
						t.Errorf("CMY(%d,%d,%d) -> RGB(%d,%d,%d) -> CMY(%d,%d,%d)",
							c, m, y, r, g, b, cBack, mBack, yBack)
					}
				}
			}
		}
	})
}

// TestSpecGammaFunctions tests sRGB gamma encoding/decoding accuracy.
func TestSpecGammaFunctions(t *testing.T) {
	// Test that gamma and inverse gamma are true inverses
	for i := 0; i <= 100; i++ {
		linear := float64(i) / 100.0
		encoded := srgbGamma(linear)
		decoded := srgbInverseGamma(encoded)

		if math.Abs(decoded-linear) > 0.0001 {
			t.Errorf("Gamma roundtrip failed: %f -> %f -> %f", linear, encoded, decoded)
		}
	}

	// Test specific sRGB spec values
	// Linear 0.0031308 is the transition point
	transition := 0.0031308
	encodedTransition := srgbGamma(transition)
	// At transition, both formulas should give same result
	linearResult := 12.92 * transition
	if math.Abs(encodedTransition-linearResult) > 0.0001 {
		t.Errorf("sRGB transition point: got %f, want %f", encodedTransition, linearResult)
	}
}

func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
