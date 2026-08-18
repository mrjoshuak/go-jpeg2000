package entropy

import (
	"encoding/hex"
	"testing"
)

// These four code-blocks come out of a two-resolution codestream written by
// OpenJPEG, so between them they exercise all four zero-coding context tables
// (Table D.1) against an external reference. TestT1DecodeOpenJPEGBlock only
// covers LL, and the LL column of the table is the one band that does not
// exchange the horizontal and vertical neighbour counts, so without these the
// HL/LH/HH columns are unpinned.
//
// Reproduce with a 16x16 P5 whose samples come from
// random.seed-free generator in /tmp (any content works; the fixture records
// what OpenJPEG produced for one specific image):
//
//	opj_compress -i s16.pgm -o s16_2.j2k -n 2 -r 1
//
// The expected coefficients are the reversible 5/3 analysis of the DC-shifted
// samples at one decomposition level, computed independently of this library.
var openJPEGBandFixtures = []struct {
	name     string
	bandType int
	numBPS   int
	dataHex  string
	want     []int32
}{
	{
		name: "LL", bandType: BandLL, numBPS: 7,
		dataHex: "1ce68812084c9fe2fefde399a0a60b4eb76d060d261d2808ff13f007ec3866175b662c338a70365f9c2d0f254eb795b12e2b4f4c0e601fe480281f",
		want: []int32{
			-11, 56, 71, 111, 94, 91, 47, 16,
			24, 28, 94, 73, 98, 69, 68, 23,
			21, 43, 68, 88, 93, 77, 46, 19,
			-8, 18, 59, 68, 70, 53, 29, 16,
			-10, -10, 53, 31, 58, 54, 25, -3,
			-1, 26, 19, 23, 34, 32, 2, 16,
			-7, 8, -5, -5, -5, 4, 3, 3,
			12, -25, -17, -22, -42, -18, -15, 0,
		},
	},
	{
		name: "HL", bandType: BandHL, numBPS: 7,
		dataHex: "364722db6ada69e5b00232e80e8c45644a2771f9fb7da3823e41de6d10f0fafd1ea215720a645a6848f45248f8221faf40f938fcdf158a98",
		want: []int32{
			15, -9, 8, 20, -20, -38, -23, -65,
			21, -4, 12, 8, -23, 4, 8, -12,
			1, 15, 1, 2, 16, 26, -16, -60,
			6, 1, 22, -10, -8, 5, -3, 0,
			-8, -4, 17, 1, -5, 2, 33, -8,
			5, 17, -2, -23, -9, 6, 35, 0,
			-33, 3, 40, 33, 11, 11, 1, 16,
			20, -7, -18, -10, -3, 14, 20, -11,
		},
	},
	{
		name: "LH", bandType: BandLH, numBPS: 6,
		dataHex: "1956cc53562c6e964fb1b17b5848eab24c3b5dfca2765900cf4a615fe1b8d374f1c46bf8e3653d5f42ad468e4939f9902d49e0ff1649",
		want: []int32{
			-14, 18, 2, 13, -21, 16, -7, 2,
			-8, 14, 5, -2, -26, -8, -14, 5,
			-15, 37, -7, 1, 20, -10, 32, 0,
			6, 5, 18, -6, 31, 0, 17, -9,
			4, -11, 0, 16, 14, 12, 1, 6,
			-20, -25, 30, -15, 17, -2, 10, -5,
			-27, -9, 6, 7, 3, 25, -15, -14,
			-35, 6, 0, -23, -24, 20, -22, -9,
		},
	},
	{
		name: "HH", bandType: BandHH, numBPS: 6,
		dataHex: "19ce711e204bd49860a5fb0d6e4c54ea49193c3bfdc8a9790af7d0cff46e1a1457831b164ea7f365e9b289043955c57ab2ab7d31c15cea5b3f",
		want: []int32{
			4, 20, -11, 23, 23, -11, -15, -46,
			-26, 24, 6, 27, 8, 11, -14, -6,
			2, 24, 29, 24, 21, 15, -25, 6,
			-5, 32, 16, 13, 22, 23, -5, -34,
			14, 8, -6, -5, 11, 13, -18, -30,
			-9, -2, 2, -35, 10, -3, 10, 11,
			-7, -14, 21, 4, -25, -25, 50, 55,
			4, -9, 1, 5, -13, -13, 10, 30,
		},
	},
}

func TestT1DecodeOpenJPEGBands(t *testing.T) {
	for _, f := range openJPEGBandFixtures {
		t.Run(f.name, func(t *testing.T) {
			data, err := hex.DecodeString(f.dataHex)
			if err != nil {
				t.Fatalf("bad fixture: %v", err)
			}
			t1 := NewT1(8, 8)
			got := t1.Decode(data, f.numBPS, f.bandType)
			if len(got) != 64 {
				t.Fatalf("decoded %d coefficients, want 64", len(got))
			}
			bad := 0
			for i := range f.want {
				if got[i] != f.want[i] {
					if bad < 8 {
						t.Errorf("coefficient %d (x=%d y=%d): got %d want %d",
							i, i%8, i/8, got[i], f.want[i])
					}
					bad++
				}
			}
			if bad != 0 {
				t.Fatalf("%d/64 coefficients differ from the OpenJPEG-encoded %s block", bad, f.name)
			}
		})
	}
}
