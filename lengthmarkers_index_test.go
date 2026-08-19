package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// encodePLTFixture writes a lossless codestream with or without packet length
// markers, otherwise identical.
func encodePLTFixture(t *testing.T, size, precExp, nres int, plt bool) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(20 + (x*13+y*3)%200)})
		}
	}
	var ps []PrecinctSize
	for i := 0; i < nres && precExp > 0; i++ {
		ps = append(ps, PrecinctSize{WidthExp: uint8(precExp), HeightExp: uint8(precExp)})
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		Lossless: true, Format: FormatJ2K, NumResolutions: nres,
		PrecinctSizes: ps, WritePacketLengths: plt,
	}); err != nil {
		t.Fatalf("Encode(plt=%v): %v", plt, err)
	}
	return buf.Bytes()
}

// TestPLTIndexMatchesTheWalk is the correctness half: an index built from the
// PLT markers must name exactly the same packets, at exactly the same byte
// ranges, as one built by parsing every packet header.
//
// The two paths share no arithmetic — one sums declared lengths, the other
// parses tag trees and code-block contributions — so agreement between them is
// real evidence rather than a round trip. A PLT list that is plausible but
// shifted by one packet would still decode, because the decoder does not use
// PLT; only this comparison sees it.
func TestPLTIndexMatchesTheWalk(t *testing.T) {
	for _, tc := range []struct {
		name             string
		size, prec, nres int
	}{
		{"plain", 128, 0, 3},
		{"precincts", 256, 5, 4},
		{"small_precincts", 128, 4, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPLT := encodePLTFixture(t, tc.size, tc.prec, tc.nres, true)
			noPLT := encodePLTFixture(t, tc.size, tc.prec, tc.nres, false)

			idxP, err := BuildPacketIndex(withPLT)
			if err != nil {
				t.Fatalf("BuildPacketIndex(with PLT): %v", err)
			}
			idxW, err := BuildPacketIndex(noPLT)
			if err != nil {
				t.Fatalf("BuildPacketIndex(walked): %v", err)
			}

			if _, _, fromPLT := idxP.IndexCost(); !fromPLT {
				t.Fatal("the index of a codestream carrying PLT was not built from it")
			}
			if _, _, fromPLT := idxW.IndexCost(); fromPLT {
				t.Fatal("an index of a codestream without PLT claims it came from PLT")
			}

			addrP, addrW := idxP.AllAddresses(), idxW.AllAddresses()
			if len(addrP) != len(addrW) {
				t.Fatalf("PLT index has %d packets, the walk found %d", len(addrP), len(addrW))
			}

			// The two codestreams differ only by the markers, so the packet
			// bytes must be identical even though the offsets are not.
			for i := range addrP {
				if addrP[i] != addrW[i] {
					t.Fatalf("packet %d: PLT index says %v, the walk says %v", i, addrP[i], addrW[i])
				}
				pp, err := idxP.GetPacket(addrP[i])
				if err != nil {
					t.Fatalf("GetPacket(%v) from PLT index: %v", addrP[i], err)
				}
				pw, err := idxW.GetPacket(addrW[i])
				if err != nil {
					t.Fatalf("GetPacket(%v) from walked index: %v", addrW[i], err)
				}
				if !bytes.Equal(pp, pw) {
					t.Fatalf("packet %d (%v) differs: PLT gave %d bytes, the walk gave %d",
						i, addrP[i], len(pp), len(pw))
				}
			}

			// And the ranges must name those same bytes in the file.
			for _, a := range addrP {
				r, ok := idxP.Range(a)
				if !ok {
					continue
				}
				want, _ := idxP.GetPacket(a)
				if !bytes.Equal(withPLT[r.Offset:r.Offset+r.Length], want) {
					t.Fatalf("packet %v: bytes at offset %d are not the packet's own", a, r.Offset)
				}
			}
		})
	}
}

// TestPLTIndexCostIsBoundedByTheHeaders is the reason the markers are worth
// writing: with PLT the bytes needed to index a file are the headers, not the
// file.
//
// It is stated as a ratio against the codestream rather than an absolute
// figure, and compared against the same image indexed by walking, because the
// absolute number is uninteresting and would need updating whenever the fixture
// changed. What must hold is that one grows with the image and the other does
// not.
func TestPLTIndexCostIsBoundedByTheHeaders(t *testing.T) {
	const prec, nres = 5, 4

	var small, large int
	for _, size := range []int{128, 512} {
		cs := encodePLTFixture(t, size, prec, nres, true)
		idx, err := BuildPacketIndex(cs)
		if err != nil {
			t.Fatalf("BuildPacketIndex(%d): %v", size, err)
		}
		read, total, fromPLT := idx.IndexCost()
		if !fromPLT {
			t.Fatalf("size %d: index was not built from PLT", size)
		}
		if read >= total {
			t.Errorf("size %d: indexing read %d of %d bytes; PLT must cost less than the whole codestream",
				size, read, total)
		}
		pct := 100 * float64(read) / float64(total)
		t.Logf("size %4d: indexed %d of %d bytes (%.1f%%), %d packets",
			size, read, total, pct, idx.Len())
		if size == 128 {
			small = read
		} else {
			large = read
		}
	}

	// A 512x512 image is sixteen times the pixels of a 128x128 one. The
	// header cost carries one length per packet, so it grows with the packet
	// count rather than with the data — it must not grow like the codestream.
	if large > small*16 {
		t.Errorf("index cost grew from %d to %d bytes across a 16x area increase; "+
			"that is proportional to the image, which is what PLT exists to avoid",
			small, large)
	}
}
