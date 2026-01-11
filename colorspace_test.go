package jpeg2000

import (
	"math"
	"testing"
)

func TestGetColorConversion(t *testing.T) {
	tests := []struct {
		cs       ColorSpace
		hasConv  bool
		name     string
	}{
		{ColorSpaceUnknown, false, "Unknown"},
		{ColorSpaceUnspecified, false, "Unspecified"},
		{ColorSpaceSRGB, false, "sRGB"},
		{ColorSpaceGray, false, "Gray"},
		{ColorSpaceBilevel, false, "Bilevel"},
		{ColorSpaceSYCC, true, "sYCC"},
		{ColorSpaceYCbCr2, true, "YCbCr2"},
		{ColorSpaceYCbCr3, true, "YCbCr3"},
		{ColorSpacePhotoYCC, true, "PhotoYCC"},
		{ColorSpaceCMY, true, "CMY"},
		{ColorSpaceCMYK, true, "CMYK"},
		{ColorSpaceYCCK, true, "YCCK"},
		{ColorSpaceCIELab, true, "CIELab"},
		{ColorSpaceCIEJab, true, "CIEJab"},
		{ColorSpaceESRGB, true, "eSRGB"},
		{ColorSpaceROMMRGB, true, "ROMMRGB"},
		{ColorSpaceYPbPr60, true, "YPbPr60"},
		{ColorSpaceYPbPr50, true, "YPbPr50"},
		{ColorSpaceEYCC, true, "EYCC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := getColorConversion(tt.cs)
			if tt.hasConv && conv == nil {
				t.Errorf("getColorConversion(%v) = nil, want non-nil", tt.cs)
			}
			if !tt.hasConv && conv != nil {
				t.Errorf("getColorConversion(%v) = non-nil, want nil", tt.cs)
			}
		})
	}
}

func TestConvertSYCCToRGB(t *testing.T) {
	// Test sYCC to RGB conversion with known values
	// sYCC: Y=128, Cb=128, Cr=128 should be neutral gray R=G=B=128 (approximately)
	precision := 8
	componentData := [][]int32{
		{128}, // Y
		{128}, // Cb (centered)
		{128}, // Cr (centered)
	}

	convertSYCCToRGB(componentData, precision)

	// After conversion, should be approximately gray
	// Allow some tolerance due to rounding
	if diff := abs(componentData[0][0] - 128); diff > 2 {
		t.Errorf("R = %d, want ~128", componentData[0][0])
	}
	if diff := abs(componentData[1][0] - 128); diff > 2 {
		t.Errorf("G = %d, want ~128", componentData[1][0])
	}
	if diff := abs(componentData[2][0] - 128); diff > 2 {
		t.Errorf("B = %d, want ~128", componentData[2][0])
	}
}

func TestConvertYCbCr601ToRGB(t *testing.T) {
	// BT.601 YCbCr with neutral gray
	precision := 8
	componentData := [][]int32{
		{128}, // Y
		{128}, // Cb (centered)
		{128}, // Cr (centered)
	}

	convertYCbCr601ToRGB(componentData, precision)

	// Should be approximately gray
	if diff := abs(componentData[0][0] - 128); diff > 2 {
		t.Errorf("R = %d, want ~128", componentData[0][0])
	}
}

func TestConvertCMYToRGB(t *testing.T) {
	precision := 8
	maxVal := int32(255)

	// Test: C=0, M=0, Y=0 should give R=255, G=255, B=255 (white)
	componentData := [][]int32{
		{0}, // C
		{0}, // M
		{0}, // Y
	}

	convertCMYToRGB(componentData, precision)

	if componentData[0][0] != maxVal {
		t.Errorf("R = %d, want %d", componentData[0][0], maxVal)
	}
	if componentData[1][0] != maxVal {
		t.Errorf("G = %d, want %d", componentData[1][0], maxVal)
	}
	if componentData[2][0] != maxVal {
		t.Errorf("B = %d, want %d", componentData[2][0], maxVal)
	}

	// Test: C=255, M=255, Y=255 should give R=0, G=0, B=0 (black)
	componentData = [][]int32{
		{255}, // C
		{255}, // M
		{255}, // Y
	}

	convertCMYToRGB(componentData, precision)

	if componentData[0][0] != 0 {
		t.Errorf("R = %d, want 0", componentData[0][0])
	}
	if componentData[1][0] != 0 {
		t.Errorf("G = %d, want 0", componentData[1][0])
	}
	if componentData[2][0] != 0 {
		t.Errorf("B = %d, want 0", componentData[2][0])
	}
}

func TestConvertCMYKToRGB(t *testing.T) {
	precision := 8

	// C=0, M=0, Y=0, K=0 should give white
	componentData := [][]int32{
		{0}, // C
		{0}, // M
		{0}, // Y
		{0}, // K
	}

	convertCMYKToRGB(componentData, precision)

	if componentData[0][0] != 255 {
		t.Errorf("R = %d, want 255", componentData[0][0])
	}
	if componentData[1][0] != 255 {
		t.Errorf("G = %d, want 255", componentData[1][0])
	}
	if componentData[2][0] != 255 {
		t.Errorf("B = %d, want 255", componentData[2][0])
	}

	// K=255 (full black) should give black regardless of CMY
	componentData = [][]int32{
		{0},   // C
		{0},   // M
		{0},   // Y
		{255}, // K
	}

	convertCMYKToRGB(componentData, precision)

	if componentData[0][0] != 0 {
		t.Errorf("R = %d, want 0", componentData[0][0])
	}
}

func TestConvertCIELabToRGB(t *testing.T) {
	precision := 8

	// L*=50 (mid gray), a*=0, b*=0 should give approximately mid gray
	// L*=50 maps to 0.5*255 = 127.5 -> 128
	// a*=0 maps to (0+128)/255*255 = 128
	// b*=0 maps to (0+128)/255*255 = 128
	componentData := [][]int32{
		{128}, // L* = 50 (encoded)
		{128}, // a* = 0 (encoded with offset)
		{128}, // b* = 0 (encoded with offset)
	}

	convertCIELabToRGB(componentData, precision)

	// The result should be approximately neutral gray
	// Allow tolerance for the complex Lab->XYZ->RGB transformation
	r := componentData[0][0]
	g := componentData[1][0]
	b := componentData[2][0]

	// R, G, B should be similar (gray)
	// Lab to RGB involves D50->D65 chromatic adaptation which can shift neutrals slightly
	if absDiff := abs(r - g); absDiff > 20 {
		t.Errorf("R-G difference = %d, want < 20 for gray", absDiff)
	}
	if absDiff := abs(g - b); absDiff > 20 {
		t.Errorf("G-B difference = %d, want < 20 for gray", absDiff)
	}
}

func TestConvertESRGBToRGB(t *testing.T) {
	precision := 8

	// Mid-range e-sRGB should give approximately mid-range sRGB
	componentData := [][]int32{
		{128},
		{128},
		{128},
	}

	convertESRGBToRGB(componentData, precision)

	// Should be clamped and converted to valid sRGB
	if componentData[0][0] < 0 || componentData[0][0] > 255 {
		t.Errorf("R = %d, want 0-255", componentData[0][0])
	}
}

func TestConvertROMMRGBToRGB(t *testing.T) {
	precision := 8

	// ROMM-RGB mid gray
	componentData := [][]int32{
		{128},
		{128},
		{128},
	}

	convertROMMRGBToRGB(componentData, precision)

	// Result should be clamped to valid range
	if componentData[0][0] < 0 || componentData[0][0] > 255 {
		t.Errorf("R = %d, want 0-255", componentData[0][0])
	}
}

func TestConvertYPbPr709ToRGB(t *testing.T) {
	precision := 8

	// Y=128, Pb=Pr=128 (neutral) should give gray
	componentData := [][]int32{
		{128},
		{128},
		{128},
	}

	convertYPbPr709ToRGB(componentData, precision)

	if diff := abs(componentData[0][0] - 128); diff > 2 {
		t.Errorf("R = %d, want ~128", componentData[0][0])
	}
}

func TestConvertEYCCToRGB(t *testing.T) {
	precision := 8

	// Extended YCC with neutral values
	componentData := [][]int32{
		{128},
		{128},
		{128},
	}

	convertEYCCToRGB(componentData, precision)

	if diff := abs(componentData[0][0] - 128); diff > 2 {
		t.Errorf("R = %d, want ~128", componentData[0][0])
	}
}

func TestColorConversionEdgeCases(t *testing.T) {
	t.Run("empty_data", func(t *testing.T) {
		componentData := [][]int32{}
		// Should not panic
		convertSYCCToRGB(componentData, 8)
		convertCMYToRGB(componentData, 8)
		convertCMYKToRGB(componentData, 8)
	})

	t.Run("insufficient_components", func(t *testing.T) {
		componentData := [][]int32{{128}, {128}} // Only 2 components
		// Should not panic, just return without conversion
		convertSYCCToRGB(componentData, 8)
		convertCMYToRGB(componentData, 8)
	})

	t.Run("16bit_precision", func(t *testing.T) {
		componentData := [][]int32{
			{32768}, // Mid value for 16-bit
			{32768},
			{32768},
		}
		convertSYCCToRGB(componentData, 16)
		// Should handle 16-bit precision
		if componentData[0][0] < 0 || componentData[0][0] > 65535 {
			t.Errorf("R = %d, want 0-65535", componentData[0][0])
		}
	})
}

func TestClampToInt32(t *testing.T) {
	tests := []struct {
		v, min, max float64
		want        int32
	}{
		{50.0, 0, 255, 50},
		{-10.0, 0, 255, 0},
		{300.0, 0, 255, 255},
		{127.5, 0, 255, 128}, // Tests rounding
	}

	for _, tt := range tests {
		got := clampToInt32(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("clampToInt32(%v, %v, %v) = %d, want %d",
				tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestClampFloat64(t *testing.T) {
	tests := []struct {
		v, min, max float64
		want        float64
	}{
		{0.5, 0, 1, 0.5},
		{-0.5, 0, 1, 0},
		{1.5, 0, 1, 1},
	}

	for _, tt := range tests {
		got := clampFloat64(tt.v, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("clampFloat64(%v, %v, %v) = %v, want %v",
				tt.v, tt.min, tt.max, got, tt.want)
		}
	}
}

func TestLabInverseF(t *testing.T) {
	// Test the Lab f inverse function
	delta := 6.0 / 29.0

	// For t > delta, result is t^3
	result := labInverseF(0.5)
	if math.Abs(result-0.125) > 0.001 {
		t.Errorf("labInverseF(0.5) = %v, want 0.125", result)
	}

	// For t <= delta, result is 3*delta^2*(t - 4/29)
	result = labInverseF(0.1)
	expected := 3 * delta * delta * (0.1 - 4.0/29.0)
	if math.Abs(result-expected) > 0.001 {
		t.Errorf("labInverseF(0.1) = %v, want %v", result, expected)
	}
}

func TestSRGBGamma(t *testing.T) {
	// Test sRGB gamma function
	// Linear 0 should give 0
	if srgbGamma(0) != 0 {
		t.Errorf("srgbGamma(0) = %v, want 0", srgbGamma(0))
	}

	// Linear 1 should give 1
	if math.Abs(srgbGamma(1)-1) > 0.001 {
		t.Errorf("srgbGamma(1) = %v, want 1", srgbGamma(1))
	}

	// Test the linear region
	linear := 0.001
	expected := 12.92 * linear
	if math.Abs(srgbGamma(linear)-expected) > 0.001 {
		t.Errorf("srgbGamma(%v) = %v, want %v", linear, srgbGamma(linear), expected)
	}
}

func TestSRGBInverseGamma(t *testing.T) {
	// Test that inverse gamma is inverse of gamma
	testVals := []float64{0, 0.01, 0.1, 0.5, 0.9, 1.0}
	for _, v := range testVals {
		encoded := srgbGamma(v)
		decoded := srgbInverseGamma(encoded)
		if math.Abs(decoded-v) > 0.001 {
			t.Errorf("srgbInverseGamma(srgbGamma(%v)) = %v, want %v", v, decoded, v)
		}
	}
}

func TestConvertPhotoYCCToRGB(t *testing.T) {
	precision := 8

	// PhotoYCC with neutral values (Y=128, C1=156, C2=156 is neutral)
	componentData := [][]int32{
		{128}, // Y
		{156}, // C1 (neutral offset)
		{156}, // C2 (neutral offset)
	}

	convertPhotoYCCToRGB(componentData, precision)

	// Result should be valid RGB values
	if componentData[0][0] < 0 || componentData[0][0] > 255 {
		t.Errorf("R = %d, want 0-255", componentData[0][0])
	}
	if componentData[1][0] < 0 || componentData[1][0] > 255 {
		t.Errorf("G = %d, want 0-255", componentData[1][0])
	}
	if componentData[2][0] < 0 || componentData[2][0] > 255 {
		t.Errorf("B = %d, want 0-255", componentData[2][0])
	}
}

func TestConvertYCCKToRGB(t *testing.T) {
	precision := 8

	// YCCK with neutral YCC and no black
	componentData := [][]int32{
		{128}, // Y
		{156}, // C1
		{156}, // C2
		{0},   // K (no black)
	}

	convertYCCKToRGB(componentData, precision)

	// Result should be valid RGB values
	if componentData[0][0] < 0 || componentData[0][0] > 255 {
		t.Errorf("R = %d, want 0-255", componentData[0][0])
	}

	// With K=255 (full black), should give black
	componentData = [][]int32{
		{128},
		{156},
		{156},
		{255}, // Full black
	}

	convertYCCKToRGB(componentData, precision)

	if componentData[0][0] != 0 {
		t.Errorf("R = %d, want 0 (black)", componentData[0][0])
	}
}

func TestConvertCIEJabToRGB(t *testing.T) {
	precision := 8

	// CIEJab with neutral values
	componentData := [][]int32{
		{128}, // J
		{128}, // a (neutral)
		{128}, // b (neutral)
	}

	convertCIEJabToRGB(componentData, precision)

	// Result should be approximately gray
	r := componentData[0][0]
	g := componentData[1][0]
	b := componentData[2][0]

	if r < 0 || r > 255 {
		t.Errorf("R = %d, want 0-255", r)
	}
	if g < 0 || g > 255 {
		t.Errorf("G = %d, want 0-255", g)
	}
	if b < 0 || b > 255 {
		t.Errorf("B = %d, want 0-255", b)
	}
}

func TestColorConversionInsufficientComponents(t *testing.T) {
	// Test that functions handle insufficient components gracefully
	precision := 8

	tests := []struct {
		name string
		conv colorConversion
		need int
	}{
		{"PhotoYCC", convertPhotoYCCToRGB, 3},
		{"YCCK", convertYCCKToRGB, 4},
		{"CIEJab", convertCIEJabToRGB, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_empty", func(t *testing.T) {
			componentData := [][]int32{}
			tt.conv(componentData, precision) // Should not panic
		})

		t.Run(tt.name+"_insufficient", func(t *testing.T) {
			// Create data with one less component than needed
			componentData := make([][]int32, tt.need-1)
			for i := range componentData {
				componentData[i] = []int32{128}
			}
			tt.conv(componentData, precision) // Should not panic
		})
	}
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
