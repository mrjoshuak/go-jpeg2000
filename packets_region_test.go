package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// encodeRegionFixture writes a lossless codestream of size x size with the
// given precinct exponent, and returns the bytes.
func encodeRegionFixture(t *testing.T, size, precExp, nres int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(20 + (x*13+y*3)%200)})
		}
	}
	var ps []PrecinctSize
	if precExp > 0 {
		for i := 0; i < nres; i++ {
			ps = append(ps, PrecinctSize{WidthExp: uint8(precExp), HeightExp: uint8(precExp)})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres, PrecinctSizes: ps,
	}); err != nil {
		t.Fatalf("Encode(precExp=%d): %v", precExp, err)
	}
	return buf.Bytes()
}

// TestPacketsForRegionIsASubset is the property a precinct partition exists to
// provide: a viewport resolves to a small set of byte ranges rather than to the
// whole file.
//
// It is stated as a comparison against the same image written without a
// partition, because the absolute numbers are not the point — the point is that
// without precincts a region query cannot narrow anything, since one packet per
// resolution covers everything. A partition that this library wrote but
// mislocated would still return "everything" and pass a test that only asked
// whether the region was covered.
func TestPacketsForRegionIsASubset(t *testing.T) {
	const size, nres = 256, 4

	whole := encodeRegionFixture(t, size, 0, nres) // maximal precinct
	parts := encodeRegionFixture(t, size, 5, nres) // 32x32 precincts

	idxWhole, err := BuildPacketIndex(whole)
	if err != nil {
		t.Fatalf("BuildPacketIndex(no precincts): %v", err)
	}
	idxParts, err := BuildPacketIndex(parts)
	if err != nil {
		t.Fatalf("BuildPacketIndex(precincts): %v", err)
	}

	// A 64x64 viewport in the top-left corner: a sixteenth of the image.
	const vx0, vy0, vx1, vy1 = 0, 0, 64, 64

	bytesFor := func(idx *PacketIndex, addrs []PacketAddress) int {
		total := 0
		for _, a := range addrs {
			if r, ok := idx.Range(a); ok {
				total += r.Length
			}
		}
		return total
	}

	allWhole := idxWhole.AllAddresses()
	regWhole := idxWhole.PacketsForRegion(vx0, vy0, vx1, vy1, -1)
	allParts := idxParts.AllAddresses()
	regParts := idxParts.PacketsForRegion(vx0, vy0, vx1, vy1, -1)

	if len(regWhole) != len(allWhole) {
		t.Errorf("without precincts a region query returned %d of %d packets; "+
			"one packet per resolution covers the whole image, so it must return all of them",
			len(regWhole), len(allWhole))
	}
	if len(regParts) >= len(allParts) {
		t.Fatalf("with 32x32 precincts a 64x64 viewport selected %d of %d packets; "+
			"it must select fewer, or the partition is not being located",
			len(regParts), len(allParts))
	}

	regionBytes := bytesFor(idxParts, regParts)
	allBytes := bytesFor(idxParts, allParts)
	if regionBytes >= allBytes {
		t.Errorf("region resolves to %d bytes of %d; a viewport must cost less than the file",
			regionBytes, allBytes)
	}
	t.Logf("viewport %dx%d of %dx%d: %d/%d packets, %d/%d bytes",
		vx1-vx0, vy1-vy0, size, size, len(regParts), len(allParts), regionBytes, allBytes)
}

// TestPacketsForRegionCoversTheRegion checks the other direction: every packet
// the query drops must genuinely lie outside the viewport. A query that is
// merely small is worthless if it omits data the region needs.
func TestPacketsForRegionCoversTheRegion(t *testing.T) {
	const size, nres = 256, 4
	cs := encodeRegionFixture(t, size, 5, nres)
	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	for _, v := range []struct{ x0, y0, x1, y1 int }{
		{0, 0, 64, 64},
		{96, 96, 160, 160},
		{192, 0, 256, 64},
		{0, 0, 256, 256},
		{127, 127, 129, 129}, // straddles a precinct boundary
	} {
		selected := map[PacketAddress]bool{}
		for _, a := range idx.PacketsForRegion(v.x0, v.y0, v.x1, v.y1, -1) {
			selected[a] = true
		}
		for _, e := range idx.entries {
			if len(e.data) == 0 || e.x1 <= e.x0 || e.y1 <= e.y0 {
				continue
			}
			overlaps := e.x0 < v.x1 && e.x1 > v.x0 && e.y0 < v.y1 && e.y1 > v.y0
			if overlaps && !selected[e.addr] {
				t.Errorf("viewport (%d,%d)-(%d,%d): packet r%d p%d covering (%d,%d)-(%d,%d) overlaps but was dropped",
					v.x0, v.y0, v.x1, v.y1, e.addr.Resolution, e.addr.Precinct, e.x0, e.y0, e.x1, e.y1)
			}
			if !overlaps && selected[e.addr] {
				t.Errorf("viewport (%d,%d)-(%d,%d): packet r%d p%d covering (%d,%d)-(%d,%d) does not overlap but was selected",
					v.x0, v.y0, v.x1, v.y1, e.addr.Resolution, e.addr.Precinct, e.x0, e.y0, e.x1, e.y1)
			}
		}
	}
}

// TestPacketRangesAreTheRealBytes pins Range against the codestream itself: the
// offset and length must name exactly the bytes GetPacket returns. An index
// whose ranges are plausible but off by a header is worse than no index, since
// a ranged read would silently fetch the wrong packet.
func TestPacketRangesAreTheRealBytes(t *testing.T) {
	cs := encodeRegionFixture(t, 128, 5, 3)
	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}
	checked := 0
	for _, a := range idx.AllAddresses() {
		r, ok := idx.Range(a)
		if !ok {
			continue
		}
		want, err := idx.GetPacket(a)
		if err != nil {
			t.Fatalf("GetPacket(%v): %v", a, err)
		}
		if r.Offset < 0 || r.Offset+r.Length > len(cs) {
			t.Fatalf("packet %v range %d+%d falls outside the %d-byte codestream",
				a, r.Offset, r.Length, len(cs))
		}
		if got := cs[r.Offset : r.Offset+r.Length]; !bytes.Equal(got, want) {
			t.Fatalf("packet %v: the bytes at offset %d differ from the packet's own data",
				a, r.Offset)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no packet had a range; the index recorded nothing to check")
	}
	t.Logf("%d packet ranges match the codestream byte for byte", checked)
}
