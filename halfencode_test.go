package jpeg2000

import (
	"bytes"
	"math"
	"testing"
)

// float32ToHalf converts a float32 to the nearest IEEE 754 binary16 pattern
// (round-to-nearest-even). Used only to build test inputs.
func float32ToHalf(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16((b >> 16) & 0x8000)
	exp := int32((b>>23)&0xFF) - 127 + 15
	mant := b & 0x7FFFFF

	switch {
	case (b>>23)&0xFF == 0xFF: // Inf/NaN
		if mant != 0 {
			m := uint16(mant >> 13)
			if m == 0 {
				m = 1
			}
			return sign | 0x7C00 | m
		}
		return sign | 0x7C00
	case exp >= 0x1F:
		return sign | 0x7C00 // overflow to Inf
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint32(14 - exp)
		half := uint16(mant >> shift)
		if mant>>(shift-1)&1 == 1 {
			half++
		}
		return sign | half
	default:
		half := sign | uint16(exp)<<10 | uint16(mant>>13)
		if mant&0x1000 != 0 && (mant&0x0FFF != 0 || (mant>>13)&1 == 1) {
			half++
		}
		return half
	}
}

// TestHalfToFloat32Exact checks the widening conversion against the definition
// of binary16: for every one of the 65536 patterns, the float32 result must
// have the same sign, the same class, and (for finite values) exactly the
// value m * 2^e that the pattern denotes.
func TestHalfToFloat32Exact(t *testing.T) {
	for i := 0; i < 1<<16; i++ {
		h := uint16(i)
		got := halfToFloat32(h)

		sign := h>>15 == 1
		exp := int(h>>10) & 0x1F
		mant := int(h & 0x3FF)

		if exp == 0x1F {
			if mant == 0 {
				if !math.IsInf(float64(got), map[bool]int{true: -1, false: 1}[sign]) {
					t.Fatalf("half 0x%04X: got %v, want infinity", h, got)
				}
			} else if !math.IsNaN(float64(got)) {
				t.Fatalf("half 0x%04X: got %v, want NaN", h, got)
			}
			continue
		}

		// Exact rational value of the pattern.
		var want float64
		if exp == 0 {
			want = float64(mant) * math.Pow(2, -24)
		} else {
			want = (1 + float64(mant)/1024) * math.Pow(2, float64(exp-15))
		}
		if sign {
			want = -want
		}
		if float64(got) != want {
			t.Fatalf("half 0x%04X: got %v, want %v", h, got, want)
		}
		if want == 0 && math.Signbit(float64(got)) != sign {
			t.Fatalf("half 0x%04X: sign of zero not preserved", h)
		}
	}
}

// TestEncodeHalfAllPatterns pushes every one of the 65536 binary16 bit
// patterns through EncodeHalf/DecodeHalf and requires bit-exact recovery.
// The input is defined by the pattern index, not by an encoder, so this does
// not depend on the codec agreeing with itself about what the data means.
func TestEncodeHalfAllPatterns(t *testing.T) {
	const width, height = 256, 256
	comp := make([]uint16, width*height)
	for i := range comp {
		comp[i] = uint16(i)
	}
	img := &HalfImage{Width: width, Height: height, Components: [][]uint16{comp}}

	var buf bytes.Buffer
	if err := EncodeHalf(&buf, img, &Options{Format: FormatJ2K, Lossless: true, NumResolutions: 6}); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}

	out, err := DecodeHalf(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeHalf: %v", err)
	}
	if out.Width != width || out.Height != height || out.ComponentCount() != 1 {
		t.Fatalf("geometry: got %dx%d/%d comps", out.Width, out.Height, out.ComponentCount())
	}
	for i, want := range comp {
		if out.Components[0][i] != want {
			t.Fatalf("sample %d: got 0x%04X, want 0x%04X", i, out.Components[0][i], want)
		}
	}
}

// TestEncodeHalfRGB exercises the 3-component path, where the reversible
// colour transform runs.
func TestEncodeHalfRGB(t *testing.T) {
	const width, height = 61, 37 // odd sizes: exercises the DWT boundary cases
	comps := make([][]uint16, 3)
	for c := range comps {
		comps[c] = make([]uint16, width*height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				v := float32(x)/float32(width) - float32(c)*0.25
				if c == 2 {
					v = -v * 1000
				}
				comps[c][y*width+x] = float32ToHalf(v)
			}
		}
	}
	img := &HalfImage{Width: width, Height: height, Components: comps}

	var buf bytes.Buffer
	if err := EncodeHalf(&buf, img, &Options{Format: FormatJ2K, Lossless: true, NumResolutions: 6}); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	out, err := DecodeHalf(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeHalf: %v", err)
	}
	for c := range comps {
		for i, want := range comps[c] {
			if out.Components[c][i] != want {
				t.Fatalf("comp %d sample %d: got 0x%04X, want 0x%04X", c, i, out.Components[c][i], want)
			}
		}
	}
}

// TestDecodeHalfRejectsFloatCodestream checks that a 32-bit float codestream
// is rejected rather than reinterpreted as half data.
func TestDecodeHalfRejectsFloatCodestream(t *testing.T) {
	img := &FloatImage{
		Width: 8, Height: 8,
		Components: [][]float32{make([]float32, 64)},
		BitDepth:   32,
		Signed:     true,
	}
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, &Options{Format: FormatJ2K}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	if _, err := DecodeHalf(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("DecodeHalf accepted a 32-bit float codestream")
	}
}

// TestDecodeFloatOnHalfCodestream checks that the FloatImage decoder widens
// half samples instead of reinterpreting their bits as binary32.
func TestDecodeFloatOnHalfCodestream(t *testing.T) {
	const width, height = 8, 8
	comp := make([]uint16, width*height)
	want := make([]float32, width*height)
	for i := range comp {
		v := float32(i) * 0.5
		comp[i] = float32ToHalf(v)
		want[i] = v
	}
	var buf bytes.Buffer
	if err := EncodeHalf(&buf, &HalfImage{Width: width, Height: height, Components: [][]uint16{comp}}, nil); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	out, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat: %v", err)
	}
	for i := range want {
		if out.Components[0][i] != want[i] {
			t.Fatalf("sample %d: got %v, want %v", i, out.Components[0][i], want[i])
		}
	}
}

// TestEncodeHalfRejectsMalformedImage checks the input validation.
func TestEncodeHalfRejectsMalformedImage(t *testing.T) {
	cases := []struct {
		name string
		img  *HalfImage
	}{
		{"nil", nil},
		{"no components", &HalfImage{Width: 4, Height: 4}},
		{"zero size", &HalfImage{Width: 0, Height: 4, Components: [][]uint16{{}}}},
		{"short component", &HalfImage{Width: 4, Height: 4, Components: [][]uint16{make([]uint16, 8)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := EncodeHalf(&buf, tc.img, nil); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
