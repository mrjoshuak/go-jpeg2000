package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestFloatImageBitDepthIsOutputOnly pins what BitDepth and Signed actually do,
// which is nothing on the way in.
//
// The float path reinterprets binary32 bit patterns as signed 32-bit samples,
// so the codestream's Ssiz must say 32-bit signed for the samples to mean
// anything to another reader. A caller setting BitDepth to 12 therefore cannot
// be given a 12-bit codestream — and was given a 32-bit one with no indication,
// the same shape as Config.DecodeArea before v1.5.2: a field declared,
// documented as "original bit depth", and read by nothing.
//
// Encoding still accepts every value, because callers pass 16 and 32 today and
// their files are correct; withdrawing that would break working code to make a
// point. What this test does is make the asymmetry a fact the suite asserts
// rather than a surprise the next reader discovers. If EncodeFloat ever starts
// honouring these fields, this test fails and says so.
func TestFloatImageBitDepthIsOutputOnly(t *testing.T) {
	mk := func(depth int, signed bool) []byte {
		img := &FloatImage{
			Width: 32, Height: 32, BitDepth: depth, Signed: signed,
			Components: [][]float32{make([]float32, 32*32)},
		}
		for i := range img.Components[0] {
			img.Components[0][i] = float32(i%97) - 48
		}
		var buf bytes.Buffer
		if err := EncodeFloat(&buf, img, &Options{
			HighThroughput: true, Lossless: true,
			Format: FormatJ2K, NumResolutions: 3,
		}); err != nil {
			t.Fatalf("EncodeFloat(depth=%d signed=%v): %v", depth, signed, err)
		}
		return buf.Bytes()
	}

	// Ssiz lives in the SIZ marker segment: marker(2) Lsiz(2) Rsiz(2) Xsiz(4)
	// Ysiz(4) XOsiz(4) YOsiz(4) XTsiz(4) YTsiz(4) XTOsiz(4) YTOsiz(4) Csiz(2)
	// then one Ssiz byte per component.
	ssiz := func(cs []byte) byte {
		i := bytes.Index(cs, []byte{0xFF, 0x51})
		if i < 0 {
			t.Fatal("no SIZ marker in the codestream")
		}
		csiz := binary.BigEndian.Uint16(cs[i+38 : i+40])
		if csiz != 1 {
			t.Fatalf("fixture has %d components, want 1", csiz)
		}
		return cs[i+40]
	}

	const want = byte(0x9F) // 32 bits (0x1F+1), signed (high bit)
	for _, c := range []struct {
		depth  int
		signed bool
	}{{0, false}, {12, true}, {16, true}, {32, true}, {32, false}} {
		got := ssiz(mk(c.depth, c.signed))
		if got != want {
			t.Errorf("BitDepth=%d Signed=%v produced Ssiz 0x%02X (%d-bit, signed=%v); "+
				"the float path can only write 0x%02X, so these fields are output-only",
				c.depth, c.signed, got, (got&0x7F)+1, got&0x80 != 0, want)
		}
	}
}
