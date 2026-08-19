package jpeg2000

import (
	"encoding/binary"
	"testing"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// TestIpltRoundTrip pins the variable-length form against values chosen to sit
// on and around every seven-bit boundary, which is where a shift-by-seven
// encoder goes wrong without saying so.
func TestIpltRoundTrip(t *testing.T) {
	values := []int{0, 1, 126, 127, 128, 129, 255, 16383, 16384, 16385,
		1 << 20, 1<<21 - 1, 1 << 21, 1<<28 - 1, 1 << 28}

	// Encode them all into one PLT segment and read them back.
	seg := generatePLT(values)
	if len(seg) == 0 {
		t.Fatal("generatePLT returned nothing for a non-empty list")
	}
	if m := binary.BigEndian.Uint16(seg[0:2]); m != uint16(codestream.PLT) {
		t.Fatalf("segment starts with marker %#04x, want PLT %#04x", m, uint16(codestream.PLT))
	}
	l := int(binary.BigEndian.Uint16(seg[2:4]))
	if l+2 != len(seg) {
		t.Fatalf("Lplt says %d bytes after the marker, segment carries %d", l, len(seg)-2)
	}

	zplt, got, err := parsePLT(seg[4:])
	if err != nil {
		t.Fatalf("parsePLT: %v", err)
	}
	if zplt != 0 {
		t.Errorf("Zplt = %d, want 0 for the first segment", zplt)
	}
	if len(got) != len(values) {
		t.Fatalf("read back %d lengths, wrote %d", len(got), len(values))
	}
	for i := range values {
		if got[i] != values[i] {
			t.Errorf("length %d: wrote %d, read %d", i, values[i], got[i])
		}
	}
}

// TestIpltByteCounts states the encoding's own arithmetic: ipltLen must agree
// with what pltIplt actually writes, since generatePLT uses the former to
// decide when a segment is full and the latter to fill it. If they disagree a
// segment overruns its declared length, which no round trip through this
// library would notice.
func TestIpltByteCounts(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 16383, 16384, 1<<21 - 1, 1 << 21, 1 << 28} {
		if got, want := len(pltIplt(n, nil)), ipltLen(n); got != want {
			t.Errorf("length %d encodes to %d bytes, ipltLen says %d", n, got, want)
		}
	}
}

// TestPLTSegmentsSplit checks that a list too long for one marker segment
// becomes several with increasing Zplt, and that no packet length is split
// across the boundary.
func TestPLTSegmentsSplit(t *testing.T) {
	// Each length here needs 3 Iplt bytes, so ~21845 of them fill a segment.
	n := 30000
	values := make([]int, n)
	for i := range values {
		values[i] = 1 << 15
	}
	seg := generatePLT(values)

	var all []int
	pos, wantZ := 0, 0
	for pos < len(seg) {
		if binary.BigEndian.Uint16(seg[pos:pos+2]) != uint16(codestream.PLT) {
			t.Fatalf("expected a PLT marker at %d", pos)
		}
		l := int(binary.BigEndian.Uint16(seg[pos+2 : pos+4]))
		if l+2 > 65537 {
			t.Fatalf("segment at %d declares %d bytes, over the 16-bit maximum", pos, l)
		}
		z, part, err := parsePLT(seg[pos+4 : pos+2+l])
		if err != nil {
			t.Fatalf("parsePLT at %d: %v", pos, err)
		}
		if z != wantZ {
			t.Errorf("segment at %d has Zplt %d, want %d", pos, z, wantZ)
		}
		wantZ++
		all = append(all, part...)
		pos += 2 + l
	}
	if wantZ < 2 {
		t.Fatalf("%d lengths fit in %d segment(s); the split was not exercised", n, wantZ)
	}
	if len(all) != n {
		t.Fatalf("read %d lengths across %d segments, wrote %d", len(all), wantZ, n)
	}
	for i, v := range all {
		if v != values[i] {
			t.Fatalf("length %d differs across the segment boundary: got %d, want %d", i, v, values[i])
		}
	}
}

// TestParsePLTRejectsTruncated confirms a segment ending mid-number is an
// error rather than a silently short list.
func TestParsePLTRejectsTruncated(t *testing.T) {
	// 0x81 sets the continuation bit, so the list never terminates.
	if _, _, err := parsePLT([]byte{0, 0x81}); err == nil {
		t.Error("a PLT segment ending inside a packet length was accepted")
	}
	if _, _, err := parsePLT(nil); err == nil {
		t.Error("an empty PLT segment was accepted")
	}
}

// TestGenerateTLMShape pins the TLM segment's declared length and Stlm against
// what it actually carries.
func TestGenerateTLMShape(t *testing.T) {
	idx := []int{0, 1, 2}
	lens := []uint32{100, 200, 300}
	seg := generateTLM(idx, lens)
	if len(seg) == 0 {
		t.Fatal("generateTLM returned nothing")
	}
	if m := binary.BigEndian.Uint16(seg[0:2]); m != uint16(codestream.TLM) {
		t.Fatalf("segment starts with %#04x, want TLM %#04x", m, uint16(codestream.TLM))
	}
	l := int(binary.BigEndian.Uint16(seg[2:4]))
	if l+2 != len(seg) {
		t.Fatalf("Ltlm says %d bytes after the marker, segment carries %d", l, len(seg)-2)
	}
	// ST is bits 4-5 and SP is bit 6. 0x60 would put 2 in ST, promising a
	// two-byte tile index that is not written; OpenJPEG reports that as "TLM
	// marker not of expected size" and silently ignores the marker.
	if seg[5] != 0x50 {
		t.Fatalf("Stlm = %#02x, want 0x50 (ST=1 in bits 4-5, SP=1 in bit 6)", seg[5])
	}
	if st := (seg[5] >> 4) & 0x3; st != 1 {
		t.Errorf("Stlm encodes ST=%d, but each entry writes a one-byte tile index", st)
	}
	if sp := (seg[5] >> 6) & 0x1; sp != 1 {
		t.Errorf("Stlm encodes SP=%d, but each entry writes a four-byte length", sp)
	}
	for i := range idx {
		off := 6 + i*5
		if int(seg[off]) != idx[i] {
			t.Errorf("entry %d has tile index %d, want %d", i, seg[off], idx[i])
		}
		if got := binary.BigEndian.Uint32(seg[off+1 : off+5]); got != lens[i] {
			t.Errorf("entry %d has length %d, want %d", i, got, lens[i])
		}
	}
}
