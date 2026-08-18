package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
)

func tileRamp(x, y int) int { return 20 + ((x*13 + y*3) % 200) }

func tileTestImage(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.Gray{Y: uint8(tileRamp(x, y))})
		}
	}
	return img
}

// TestTiledCodestreamHasOneTilePartPerTile is the structural half of the tiling
// defect: SIZ used to declare a tile grid that the codestream did not carry.
// One tile-part was written, holding packets for the whole image, so every
// decoder that believed SIZ ran off the end of tile 0's geometry.
func TestTiledCodestreamHasOneTilePartPerTile(t *testing.T) {
	var buf bytes.Buffer
	err := Encode(&buf, tileTestImage(64, 64), &Options{
		HighThroughput: true, Lossless: true, Format: FormatJ2K,
		NumResolutions: 3, TileSize: image.Point{X: 20, Y: 20},
	})
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	cs := buf.Bytes()

	// Walk the tile-parts by their declared lengths rather than by searching
	// for markers: a tile-part that lies about Psot would still be found by a
	// search, and the point is that the lengths partition the codestream.
	pos := 0
	for pos+1 < len(cs) && !(cs[pos] == 0xFF && cs[pos+1] == 0x90) {
		pos++
	}
	var isots []int
	for pos+1 < len(cs) && cs[pos] == 0xFF && cs[pos+1] == 0x90 {
		if pos+14 > len(cs) {
			t.Fatalf("tile-part header at %d runs past the end of the codestream", pos)
		}
		if lsot := binary.BigEndian.Uint16(cs[pos+2 : pos+4]); lsot != 10 {
			t.Fatalf("tile-part at %d has Lsot %d, want 10", pos, lsot)
		}
		isot := int(binary.BigEndian.Uint16(cs[pos+4 : pos+6]))
		psot := int(binary.BigEndian.Uint32(cs[pos+6 : pos+10]))
		if cs[pos+12] != 0xFF || cs[pos+13] != 0x93 {
			t.Fatalf("tile-part %d is not followed by SOD", isot)
		}
		if psot < 14 || pos+psot > len(cs) {
			t.Fatalf("tile-part %d has Psot %d, which does not fit at offset %d of %d",
				isot, psot, pos, len(cs))
		}
		isots = append(isots, isot)
		pos += psot
	}

	// 64 in 20-pixel tiles is a 4x4 grid: three full tiles and a 4-pixel
	// remainder in each direction.
	want := 16
	if len(isots) != want {
		t.Fatalf("codestream carries %d tile-parts, want %d (Isot values %v)", len(isots), want, isots)
	}
	for i, isot := range isots {
		if isot != i {
			t.Errorf("tile-part %d has Isot %d, want %d", i, isot, i)
		}
	}
	if pos+2 != len(cs) || cs[pos] != 0xFF || cs[pos+1] != 0xD9 {
		t.Errorf("the tile-part lengths do not run up to EOC: ended at %d of %d", pos, len(cs))
	}
}

// TestTiledRoundTripExact covers the geometry a tile grid produces, with the
// emphasis on tile sizes that do not divide the image: those put whole tiles at
// origins that are odd once halved, and the subband split of a tile depends on
// that parity rather than on the tile's size.
func TestTiledRoundTripExact(t *testing.T) {
	shapes := []struct{ w, h, tw, th int }{
		{32, 32, 16, 16}, // divides evenly
		{32, 32, 8, 8},
		{32, 32, 12, 12}, // does not divide; odd origins at two levels
		{32, 32, 20, 20},
		{32, 32, 13, 13},
		{32, 32, 31, 31}, // a one-pixel-wide last column of tiles
		{40, 24, 16, 8},  // non-square tiles
		{37, 29, 13, 11}, // nothing divides anything
		{64, 64, 64, 64}, // one tile, taken through the same path as SIZ sees it
		{40, 24, 64, 64}, // tile larger than the image: a 1x1 grid
	}
	for _, s := range shapes {
		for _, nres := range []int{1, 2, 3} {
			for _, ht := range []bool{true, false} {
				name := fmt.Sprintf("%dx%d_tile%dx%d_res%d_ht%v", s.w, s.h, s.tw, s.th, nres, ht)
				t.Run(name, func(t *testing.T) {
					src := tileTestImage(s.w, s.h)
					var buf bytes.Buffer
					err := Encode(&buf, src, &Options{
						HighThroughput: ht, Lossless: true, Format: FormatJ2K,
						NumResolutions: nres, TileSize: image.Point{X: s.tw, Y: s.th},
					})
					if err != nil {
						t.Fatalf("Encode() error: %v", err)
					}
					img, err := Decode(bytes.NewReader(buf.Bytes()))
					if err != nil {
						t.Fatalf("Decode() error: %v", err)
					}
					got, ok := img.(*image.Gray)
					if !ok {
						t.Fatalf("decoded %T, want *image.Gray", img)
					}
					if b := got.Bounds(); b.Dx() != s.w || b.Dy() != s.h {
						t.Fatalf("decoded %dx%d, want %dx%d", b.Dx(), b.Dy(), s.w, s.h)
					}
					diff := 0
					for y := 0; y < s.h; y++ {
						for x := 0; x < s.w; x++ {
							if int(got.GrayAt(x, y).Y) != tileRamp(x, y) {
								diff++
							}
						}
					}
					if diff != 0 {
						t.Errorf("%d of %d samples differ after a tiled round trip", diff, s.w*s.h)
					}
				})
			}
		}
	}
}

// TestTiledRoundTrip16Bit checks the same for 16-bit samples, which take a
// different DC level shift and a different output image type.
//
// The samples stay under 15 bits because the block coder loses the least
// significant bit of a full-range 16-bit sample — measured at 445 of 1073
// samples off by one for values up to 56540, identically with and without
// tiles, so it is a precision limit of the coder and not of the tile grid.
func TestTiledRoundTrip16Bit(t *testing.T) {
	const w, h = 37, 29
	src := image.NewGray16(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.Gray16{Y: uint16(tileRamp(x, y) * 100)})
		}
	}
	var buf bytes.Buffer
	err := Encode(&buf, src, &Options{
		HighThroughput: true, Lossless: true, Format: FormatJ2K,
		NumResolutions: 3, TileSize: image.Point{X: 13, Y: 11},
	})
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	img, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	got, ok := img.(*image.Gray16)
	if !ok {
		t.Fatalf("decoded %T, want *image.Gray16", img)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if want := uint16(tileRamp(x, y) * 100); got.Gray16At(x, y).Y != want {
				t.Fatalf("sample (%d,%d) = %d, want %d", x, y, got.Gray16At(x, y).Y, want)
			}
		}
	}
}

// TestTileGeometryMatchesOriginZero pins the general geometry to the
// origin-zero rules the single-tile path is validated against: at the image
// origin the two must agree, or the tiling work would have changed the output
// of every untiled encode.
func TestTileGeometryMatchesOriginZero(t *testing.T) {
	for _, dim := range [][2]int{{32, 32}, {17, 9}, {65, 65}, {13, 40}, {1, 1}} {
		w, h := dim[0], dim[1]
		for numRes := 1; numRes <= 4; numRes++ {
			for r := 0; r < numRes; r++ {
				for b := 0; b < 3; b++ {
					if r == 0 && b > 0 {
						continue
					}
					sb := tileBandGeom(0, 0, w, h, numRes, r, b)
					wantW, wantH := bandDims(w, h, numRes, r, b)
					if sb.width() != wantW || sb.height() != wantH {
						t.Errorf("%dx%d numRes=%d r=%d b=%d: size %dx%d, want %dx%d",
							w, h, numRes, r, b, sb.width(), sb.height(), wantW, wantH)
					}
					bandType := bandTypeOf(r, b)
					wantX, wantY := computeSubbandOffset(w, h, numRes, r, bandType)
					if sb.ox != wantX || sb.oy != wantY {
						t.Errorf("%dx%d numRes=%d r=%d b=%d: offset (%d,%d), want (%d,%d)",
							w, h, numRes, r, b, sb.ox, sb.oy, wantX, wantY)
					}
					// A tile at the origin starts every band at zero, which is
					// why the size-only rule can survive there.
					if sb.x0 != 0 || sb.y0 != 0 {
						t.Errorf("%dx%d numRes=%d r=%d b=%d: band starts at (%d,%d), want the origin",
							w, h, numRes, r, b, sb.x0, sb.y0)
					}
				}
			}
		}
	}
}

// TestTileBandCoordinates states Equation B-15 on a tile whose origin is odd,
// where the coordinates it gives and the size-only rule disagree outright.
//
// The tile spans [13, 26) with one decomposition level.
func TestTileBandCoordinates(t *testing.T) {
	const numRes = 2
	x0, x1 := 13, 26
	y0, y1 := 0, 13

	// The LL band holds the even coordinates of [13, 26): 14, 16, ... 24, so
	// it spans [7, 13) and is 6 wide. The size-only rule would make it
	// ceil(13/2) = 7 wide, because it cannot see that the interval starts odd.
	ll := tileBandGeom(x0, y0, x1, y1, numRes, 0, 0)
	if ll.x0 != 7 || ll.x1 != 13 || ll.width() != 6 {
		t.Errorf("LL band spans [%d,%d), want [7,13)", ll.x0, ll.x1)
	}
	if sizeOnly, _ := bandDims(x1-x0, y1-y0, numRes, 0, 0); sizeOnly != 7 {
		t.Errorf("the size-only rule gives %d, expected the 7 this test is contrasting with", sizeOnly)
	}

	// HL at resolution 1 holds the odd coordinates, tbx0 = ceil((13-1)/2) = 6
	// through tbx1 = ceil((26-1)/2) = 13, so it is 7 wide and sits to the
	// right of the 6-wide lowpass half in the tile array.
	hl := tileBandGeom(x0, y0, x1, y1, numRes, 1, bandHL)
	if hl.x0 != 6 || hl.x1 != 13 || hl.width() != 7 {
		t.Errorf("HL band spans [%d,%d), want [6,13)", hl.x0, hl.x1)
	}
	if hl.ox != 6 {
		t.Errorf("HL band sits at column %d of the tile array, want 6", hl.ox)
	}

	// The two halves must still account for every sample of the resolution.
	if rw, _ := tileResDims(x0, y0, x1, y1, numRes, 1); ll.width()+hl.width() != rw {
		t.Errorf("bands cover %d samples, resolution 1 is %d wide", ll.width()+hl.width(), rw)
	}
}

// TestCodeBlockRangeAnchoredAtZero states B.7: the code-block partition is
// anchored at coordinate zero, so a band that starts inside a block gets a
// short first one and may need one block more than its size suggests.
func TestCodeBlockRangeAnchoredAtZero(t *testing.T) {
	cases := []struct {
		b0, b1, cb          int
		wantFirst, wantSpan int
	}{
		{0, 64, 64, 0, 1},
		{0, 65, 64, 0, 2},
		{60, 65, 64, 0, 2}, // straddles the boundary at 64
		{64, 96, 64, 1, 1}, // starts on a boundary
		{12, 32, 64, 0, 1}, // wholly inside the first block
		{130, 131, 64, 2, 1},
		{10, 10, 64, 0, 0}, // empty band
	}
	for _, c := range cases {
		first, span := codeBlockRange(c.b0, c.b1, c.cb)
		if first != c.wantFirst || span != c.wantSpan {
			t.Errorf("codeBlockRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
				c.b0, c.b1, c.cb, first, span, c.wantFirst, c.wantSpan)
		}
	}
}

// TestEmptyResolutionCarriesNoBand covers the degenerate tile a one-pixel-wide
// column produces: its coarse resolutions hold no samples, and a resolution
// with no samples has no precinct and so no packet at all (B.6).
func TestEmptyResolutionCarriesNoBand(t *testing.T) {
	// A tile spanning [31,32) with two decomposition levels: resolution 0
	// spans ceil(31/4)=8 .. ceil(32/4)=8, which is empty.
	bands := tileBands(31, 0, 32, 32, 3, 64, 64)
	for _, b := range bands {
		if b.res == 0 {
			t.Fatalf("resolution 0 is empty but produced a band %+v", b)
		}
	}
	if len(bands) == 0 {
		t.Fatal("the tile produced no bands at all")
	}
}

func bandTypeOf(r, b int) int {
	if r == 0 {
		return entropy.BandLL
	}
	switch b {
	case bandHL:
		return entropy.BandHL
	case bandLH:
		return entropy.BandLH
	default:
		return entropy.BandHH
	}
}
