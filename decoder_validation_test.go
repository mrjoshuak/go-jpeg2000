package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// csBuilder assembles a raw J2K codestream field by field so that a test can
// put one header value out of range and leave everything else valid.
type csBuilder struct {
	Xsiz, Ysiz   uint32
	XOsiz, YOsiz uint32
	XTsiz, YTsiz uint32
	XTOsiz       uint32
	YTOsiz       uint32
	Csiz         uint16
	Ssiz         []byte // one triple (Ssiz, XRsiz, YRsiz) per component

	Scod       uint8
	ProgOrder  uint8
	NumLayers  uint16
	MCT        uint8
	NumDecomp  uint8
	CBWidthExp uint8
	CBHeightXp uint8
	CBStyle    uint8
	Wavelet    uint8

	Sqcd uint8
}

func defaultBuilder() *csBuilder {
	return &csBuilder{
		Xsiz: 32, Ysiz: 32,
		XTsiz: 32, YTsiz: 32,
		Csiz: 1,
		Ssiz: []byte{0x07, 1, 1},

		ProgOrder:  0,
		NumLayers:  1,
		NumDecomp:  2,
		CBWidthExp: 4, CBHeightXp: 4,
		Wavelet: 1,
		Sqcd:    0x40,
	}
}

func (b *csBuilder) bytes() []byte {
	var buf bytes.Buffer
	be := binary.BigEndian
	put16 := func(v uint16) { _ = binary.Write(&buf, be, v) }
	put32 := func(v uint32) { _ = binary.Write(&buf, be, v) }

	put16(0xFF4F) // SOC

	// SIZ
	put16(0xFF51)
	put16(uint16(38 + len(b.Ssiz)))
	put16(0) // Rsiz
	put32(b.Xsiz)
	put32(b.Ysiz)
	put32(b.XOsiz)
	put32(b.YOsiz)
	put32(b.XTsiz)
	put32(b.YTsiz)
	put32(b.XTOsiz)
	put32(b.YTOsiz)
	put16(b.Csiz)
	buf.Write(b.Ssiz)

	// COD
	put16(0xFF52)
	put16(12)
	buf.WriteByte(b.Scod)
	buf.WriteByte(b.ProgOrder)
	put16(b.NumLayers)
	buf.WriteByte(b.MCT)
	buf.WriteByte(b.NumDecomp)
	buf.WriteByte(b.CBWidthExp)
	buf.WriteByte(b.CBHeightXp)
	buf.WriteByte(b.CBStyle)
	buf.WriteByte(b.Wavelet)

	// QCD
	put16(0xFF5C)
	put16(5)
	buf.WriteByte(b.Sqcd)
	put16(0x4000)

	// SOT / SOD / EOC with no tile data
	put16(0xFF90)
	put16(10)
	put16(0) // Isot
	put32(0) // Psot: to end of codestream
	buf.WriteByte(0)
	buf.WriteByte(1)
	put16(0xFF93) // SOD
	put16(0xFFD9) // EOC

	return buf.Bytes()
}

// TestDecodeRejectsOutOfRangeHeaderFields walks every header field the decoder
// uses to size an allocation, index a slice or divide, puts it out of range on
// its own, and requires a diagnostic error rather than a panic, a hang or a
// silently wrong decode.
func TestDecodeRejectsOutOfRangeHeaderFields(t *testing.T) {
	// The unmodified builder must decode, otherwise the table below proves
	// nothing.
	if _, err := Decode(bytes.NewReader(defaultBuilder().bytes())); err != nil {
		t.Fatalf("baseline codestream does not decode: %v", err)
	}

	tests := []struct {
		name  string
		mut   func(*csBuilder)
		field string // substring the error must name
	}{
		{"zero width", func(b *csBuilder) { b.Xsiz = 0 }, "dimensions"},
		{"zero height", func(b *csBuilder) { b.Ysiz = 0 }, "dimensions"},
		{"image X offset past width", func(b *csBuilder) { b.XOsiz = 32 }, "X offset"},
		{"image Y offset past height", func(b *csBuilder) { b.YOsiz = 40 }, "Y offset"},
		{"zero tile width", func(b *csBuilder) { b.XTsiz = 0 }, "tile dimensions"},
		{"zero tile height", func(b *csBuilder) { b.YTsiz = 0 }, "tile dimensions"},
		{"tile X origin past image origin", func(b *csBuilder) { b.XTOsiz = 4 }, "tile X offset"},
		{"tile Y origin past image origin", func(b *csBuilder) { b.YTOsiz = 4 }, "tile Y offset"},
		{"image far larger than input", func(b *csBuilder) { b.Xsiz, b.Ysiz, b.XTsiz, b.YTsiz = 60000, 60000, 60000, 60000 }, "justify"},
		{"tile grid larger than input", func(b *csBuilder) { b.Xsiz, b.Ysiz, b.XTsiz, b.YTsiz = 4096, 4096, 1, 1 }, "tile"},
		{"zero components", func(b *csBuilder) { b.Csiz, b.Ssiz = 0, nil }, "components"},
		{"zero X subsampling", func(b *csBuilder) { b.Ssiz = []byte{0x07, 0, 1} }, "subsampling"},
		{"zero Y subsampling", func(b *csBuilder) { b.Ssiz = []byte{0x07, 1, 0} }, "subsampling"},
		{"precision above 38", func(b *csBuilder) { b.Ssiz = []byte{0x7F, 1, 1} }, "precision"},
		{"subsampling empties component", func(b *csBuilder) {
			b.Xsiz, b.XOsiz = 32, 30
			b.XTOsiz = 0
			b.Ssiz = []byte{0x07, 255, 1}
		}, "sample grid"},
		{"decomposition levels above 32", func(b *csBuilder) { b.NumDecomp = 200 }, "decomposition"},
		{"code-block width exponent above 8", func(b *csBuilder) { b.CBWidthExp = 60 }, "code-block width exponent"},
		{"code-block height exponent above 8", func(b *csBuilder) { b.CBHeightXp = 60 }, "code-block height exponent"},
		{"code-block area above 4096", func(b *csBuilder) { b.CBWidthExp, b.CBHeightXp = 8, 8 }, "sample area"},
		{"undefined progression order", func(b *csBuilder) { b.ProgOrder = 9 }, "progression order"},
		{"undefined quantization style", func(b *csBuilder) { b.Sqcd = 0x40 | 0x1F }, "quantization style"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := defaultBuilder()
			tt.mut(b)
			data := b.bytes()

			for _, entry := range []struct {
				name string
				fn   func([]byte) error
			}{
				{"Decode", func(d []byte) error { _, err := Decode(bytes.NewReader(d)); return err }},
				{"DecodeHalf", func(d []byte) error { _, err := DecodeHalf(bytes.NewReader(d)); return err }},
				{"DecodeFloat", func(d []byte) error { _, err := DecodeFloat(bytes.NewReader(d)); return err }},
			} {
				err := entry.fn(data)
				if err == nil {
					t.Fatalf("%s accepted a codestream with %s", entry.name, tt.name)
				}
			}

			// Only Decode's message is checked for the field name: the half
			// and float paths reject this stream earlier, for not carrying
			// 16-bit NLT samples.
			_, err := Decode(bytes.NewReader(data))
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error does not name %q: %v", tt.field, err)
			}
		})
	}
}

// TestDecodeRejectsNegativeMarkerLength covers the marker-segment lengths that
// are subtracted from before use. A COM segment shorter than its own fixed
// fields used to reach make with a negative count.
func TestDecodeRejectsNegativeMarkerLength(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker uint16
		length uint16
		body   []byte
	}{
		{"COM shorter than its header", 0xFF64, 2, nil},
		{"COM length 3", 0xFF64, 3, []byte{0}},
		{"COD shorter than SPcod", 0xFF52, 4, []byte{0, 0}},
		{"COC shorter than SPcoc", 0xFF53, 4, []byte{0, 0}},
		{"QCC shorter than Sqcc", 0xFF5D, 3, []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := defaultBuilder()
			cs := b.bytes()

			// Splice the short marker segment in just before the SOT.
			sot := bytes.Index(cs, []byte{0xFF, 0x90})
			if sot < 0 {
				t.Fatal("no SOT in the baseline codestream")
			}
			var seg bytes.Buffer
			_ = binary.Write(&seg, binary.BigEndian, tc.marker)
			_ = binary.Write(&seg, binary.BigEndian, tc.length)
			seg.Write(tc.body)

			data := append(append(append([]byte(nil), cs[:sot]...), seg.Bytes()...), cs[sot:]...)

			if _, err := Decode(bytes.NewReader(data)); err == nil {
				t.Fatalf("Decode accepted %s", tc.name)
			}
		})
	}
}
