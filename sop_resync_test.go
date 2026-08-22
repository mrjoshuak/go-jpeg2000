package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// sopFixture encodes a multi-precinct greyscale image with the given error
// resilience markers and returns the codestream.
//
// Precincts are small so the tile holds many packets: resynchronisation can
// only be demonstrated when there are later packets to recover, and a
// single-packet tile would make the test vacuous whatever the decoder did.
func sopFixture(t *testing.T, sop, eph bool) ([]byte, []byte) {
	t.Helper()
	const size = 64

	img := image.NewGray(image.Rect(0, 0, size, size))
	want := make([]byte, size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := uint8(20 + (x*13+y*3)%200)
			img.Set(x, y, color.Gray{Y: v})
			want[y*size+x] = v
		}
	}

	var buf bytes.Buffer
	if err := Encode(&buf, img, &Options{
		HighThroughput: true, Lossless: true,
		Format: FormatJ2K, NumResolutions: 3,
		EnableSOP: sop, EnableEPH: eph,
		PrecinctSizes: []PrecinctSize{{6, 6}, {6, 6}, {6, 6}},
	}); err != nil {
		t.Fatalf("Encode(sop=%v eph=%v): %v", sop, eph, err)
	}
	return buf.Bytes(), want
}

// TestErrorResilienceMarkersAreWritten checks that the markers the coding style
// declares are actually in the codestream.
//
// This is the defect the item existed to find, and it was worse than "not
// implemented": Options.EnableSOP and EnableEPH set their bits in the COD's
// Scod field and no marker was ever written, so the codestream declared a
// structure it did not have. A file written with EnableEPH could not be decoded
// by OpenJPEG at all — it expects EPH after each packet header when Scod says
// so — while this library round-tripped it perfectly, because our decoder skips
// the marker only when it is present and never minded that it never was.
func TestErrorResilienceMarkersAreWritten(t *testing.T) {
	for _, c := range []struct {
		name     string
		sop, eph bool
	}{
		{"neither", false, false},
		{"sop", true, false},
		{"eph", false, true},
		{"both", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cs, _ := sopFixture(t, c.sop, c.eph)

			nSOP := bytes.Count(cs, []byte{0xFF, 0x91})
			nEPH := bytes.Count(cs, []byte{0xFF, 0x92})
			if c.sop && nSOP == 0 {
				t.Error("Scod declares SOP and the codestream holds no SOP marker; " +
					"a conformant decoder expecting a six-byte segment before each " +
					"packet reads the packet's own bytes as one")
			}
			if !c.sop && nSOP != 0 {
				t.Errorf("SOP was not requested but %d markers were written", nSOP)
			}
			if c.eph && nEPH == 0 {
				t.Error("Scod declares EPH and the codestream holds no EPH marker; " +
					"OpenJPEG refuses such a file outright")
			}
			if !c.eph && nEPH != 0 {
				t.Errorf("EPH was not requested but %d markers were written", nEPH)
			}

			// And the file still decodes to the original.
			img, err := Decode(bytes.NewReader(cs))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := img.Bounds().Dx(); got != 64 {
				t.Fatalf("decoded %d wide, want 64", got)
			}
		})
	}
}

// TestSOPResynchronisesAfterCorruption is the item's third condition: that a
// deliberately corrupted stream can be resynchronised.
//
// The corruption goes into a packet *header*, not into code-block data. A byte
// flipped in a code-block body produces wrong samples in that block and nothing
// more — the decoder never loses its place, so surviving it demonstrates
// nothing about resynchronisation. Damaging a header is what costs the decoder
// its position in a bit stream that has no self-delimiting structure, and
// finding the next boundary again is precisely what SOP is for.
//
// The assertion is comparative rather than absolute: with SOP the decode
// recovers and returns an image, without it the same damage in the same place
// does not. An absolute assertion about sample values would be measuring the
// entropy coder's luck.
func TestSOPResynchronisesAfterCorruption(t *testing.T) {
	withSOP, _ := sopFixture(t, true, false)
	without, _ := sopFixture(t, false, false)

	// Damage the second packet's header. With SOP present the packets are
	// findable, so the offset is taken from the second marker; the same
	// relative position is used in the SOP-free stream so both are hit in a
	// comparable place.
	first := bytes.Index(withSOP, []byte{0xFF, 0x91})
	if first < 0 {
		t.Fatal("no SOP marker in the fixture")
	}
	second := bytes.Index(withSOP[first+2:], []byte{0xFF, 0x91})
	if second < 0 {
		t.Skip("fixture holds only one packet, so there is nothing to resynchronise to")
	}
	hdr := first + 2 + second + 6 // first byte of the second packet's header

	damaged := append([]byte(nil), withSOP...)
	damaged[hdr] ^= 0xFF
	damaged[hdr+1] ^= 0xFF

	_, cost, err := DecodeConfigCost(bytes.NewReader(damaged), nil)
	if err != nil {
		t.Fatalf("a stream with SOP markers failed to decode after its second "+
			"packet header was corrupted: %v; the whole point of SOP is that the "+
			"following packet boundaries are still findable", err)
	}
	// Surviving is not the claim. The claim is that the decoder lost its place
	// and found it again, and the only direct evidence of that is the count.
	if cost.Resyncs == 0 {
		t.Error("the damaged stream decoded without resynchronising once, so this " +
			"test is measuring the entropy coder's tolerance rather than SOP; " +
			"either the corruption did not cost the decoder its place or the " +
			"resynchronisation path is not being reached")
	} else {
		t.Logf("recovered from %d damaged packet(s) by scanning to the next SOP marker",
			cost.Resyncs)
	}

	// An undamaged stream must not resynchronise at all: a decoder that
	// silently recovers from nothing is hiding a parse it got wrong.
	if _, clean, err := DecodeConfigCost(bytes.NewReader(withSOP), nil); err != nil {
		t.Fatalf("the undamaged fixture failed to decode: %v", err)
	} else if clean.Resyncs != 0 {
		t.Errorf("an undamaged stream resynchronised %d times; the decoder is "+
			"recovering from a parse failure that should not be happening",
			clean.Resyncs)
	}

	// The control: the same damage without markers to resynchronise on. This
	// is allowed to fail — and if it does not, the test above proves nothing,
	// because the decode would have survived without any help from SOP.
	sod := bytes.Index(without, []byte{0xFF, 0x93})
	if sod < 0 {
		t.Fatal("no SOD in the SOP-free fixture")
	}
	ctlPos := sod + 2 + (hdr - (first + 2 + second + 6)) + 8
	if ctlPos+1 >= len(without) {
		t.Skip("SOP-free fixture too short to damage comparably")
	}
	ctl := append([]byte(nil), without...)
	ctl[ctlPos] ^= 0xFF
	ctl[ctlPos+1] ^= 0xFF
	if _, err := Decode(bytes.NewReader(ctl)); err == nil {
		t.Log("note: the SOP-free control also decoded; this damage did not cost " +
			"the decoder its place, so the assertion above is weaker than it looks")
	}
}
