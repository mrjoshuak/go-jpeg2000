package entropy

import (
	"encoding/hex"
	"testing"
)

// The code-block below was cut out of a codestream written by OpenJPEG's
// opj_compress, so it pins the Part 1 (MQ/EBCOT) block decoder to an external
// reference rather than to this library's own encoder.
//
// Reproduce the fixture with:
//
//	# tiny.pgm: 8x8 P5, sample(x,y) = 20 + ((x*13 + y*3) % 200)
//	opj_compress -i tiny.pgm -o mq1.j2k -n 1 -r 1
//
// The resulting codestream declares one 8x8 tile, one component, no wavelet
// decomposition (-n 1, so the only subband is LL and no DWT is involved) and
// the reversible 5/3 path, with QCD Sqcd=0x40 (2 guard bits, no quantization)
// and SPqcd=0x40 (exponent 8). That makes Mb = guard + exponent - 1 = 9. The
// packet header codes two missing MSBs, so the block carries
// numbps = Mb - 2 = 7 magnitude bit-planes in 3*7-2 = 19 coding passes, and the
// 53 bytes below are the packet body.
//
// Because there is no wavelet and no quantization, the coefficients are exactly
// the DC-level-shifted samples, sample - 128.
const openjpegMQBlockHex = "11503a92230d1f98e6f06ca022dc7d7b6cb8b4f49595d5848313c33a916207506ff6cefd12b4046af57abd45b2bdc44e227113887f"

const openjpegMQBlockNumBPS = 7

// tinyPGMCoefficients returns the expected coefficients for the fixture block.
func tinyPGMCoefficients() []int32 {
	out := make([]int32, 64)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			sample := 20 + ((x*13 + y*3) % 200)
			out[y*8+x] = int32(sample - 128)
		}
	}
	return out
}

func TestT1DecodeOpenJPEGBlock(t *testing.T) {
	data, err := hex.DecodeString(openjpegMQBlockHex)
	if err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	if len(data) != 53 {
		t.Fatalf("fixture is %d bytes, want 53", len(data))
	}

	t1 := NewT1(8, 8)
	got := t1.Decode(data, openjpegMQBlockNumBPS, BandLL)
	want := tinyPGMCoefficients()

	if len(got) != len(want) {
		t.Fatalf("decoded %d coefficients, want %d", len(got), len(want))
	}
	bad := 0
	for i := range want {
		if got[i] != want[i] {
			if bad < 12 {
				t.Errorf("coefficient %d (x=%d y=%d): got %d want %d",
					i, i%8, i/8, got[i], want[i])
			}
			bad++
		}
	}
	if bad != 0 {
		t.Fatalf("%d/%d coefficients differ from the OpenJPEG-encoded block", bad, len(want))
	}
}
