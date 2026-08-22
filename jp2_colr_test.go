package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// TestJP2ColourspaceEnumerationIsCorrect reads the colr box out of the bytes
// this library writes and checks the enumeration against the standard's values.
//
// ISO/IEC 15444-1 I.5.3.3: EnumCS is 16 for sRGB, 17 for greyscale, 18 for
// sYCC. That value describes the codestream to every reader except this one —
// our decoder takes the component count from the codestream's SIZ marker, so a
// swapped enumeration round-trips through this library perfectly while telling
// another implementation that a three-component image is greyscale.
//
// The JP2 boxes went unchecked by anything external for a long time while the
// codestreams inside them were verified against two oracles across the whole
// capability matrix. A container both halves of one library agree on is exactly
// what an external oracle is for, and this test is the cheap half of that: it
// reads the bytes rather than trusting a round trip.
func TestJP2ColourspaceEnumerationIsCorrect(t *testing.T) {
	const size = 16

	gray := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			gray.Set(x, y, color.Gray{Y: uint8(20 + (x*13+y*3)%200)})
		}
	}
	rgb := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			rgb.Set(x, y, color.NRGBA{
				R: uint8(20 + (x*13)%200), G: uint8(20 + (y*7)%200),
				B: uint8(20 + (x*y)%200), A: 255,
			})
		}
	}

	// The values the standard assigns, written out rather than referenced, so
	// this test still means something if the constants are renamed.
	const enumSRGB = 16
	const enumGrey = 17

	for _, c := range []struct {
		name string
		img  image.Image
		want uint32
	}{
		{"greyscale", gray, enumGrey},
		{"colour", rgb, enumSRGB},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, c.img, &Options{
				HighThroughput: true, Lossless: true,
				Format: FormatJP2, NumResolutions: 2,
			}); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, ok := colrEnumeration(buf.Bytes())
			if !ok {
				t.Fatal("no colr box found in the JP2 this library wrote")
			}
			if got != c.want {
				t.Errorf("colr box declares EnumCS %d for a %s image, want %d "+
					"(16 sRGB, 17 greyscale, ISO/IEC 15444-1 I.5.3.3); our own "+
					"decoder reads the component count from SIZ, so this is "+
					"invisible to a round trip through this library",
					got, c.name, c.want)
			}
		})
	}
}

// colrEnumeration finds the colr box and returns its EnumCS.
//
// It walks the box structure rather than searching for the four bytes "colr",
// which can occur inside a codestream by chance.
func colrEnumeration(d []byte) (uint32, bool) {
	var walk func(b []byte) (uint32, bool)
	walk = func(b []byte) (uint32, bool) {
		for len(b) >= 8 {
			size := int(binary.BigEndian.Uint32(b[0:4]))
			typ := string(b[4:8])
			if size == 0 {
				size = len(b)
			}
			if size < 8 || size > len(b) {
				return 0, false
			}
			body := b[8:size]
			switch typ {
			case "colr":
				// Method(1) Precedence(1) Approximation(1) then EnumCS(4)
				// when the method is enumerated.
				if len(body) >= 7 && body[0] == 1 {
					return binary.BigEndian.Uint32(body[3:7]), true
				}
			case "jp2h":
				if v, ok := walk(body); ok {
					return v, true
				}
			}
			b = b[size:]
		}
		return 0, false
	}
	return walk(d)
}
