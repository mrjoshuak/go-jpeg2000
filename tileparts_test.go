package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

// wireTilePart is one tile-part as read straight off the bytes: where its SOT
// marker starts, its Isot, Psot, TPsot and TNsot. Parsed here rather than
// through the library, so the test does not grade the parser it exercises.
type wireTilePart struct {
	start, length int
	tile          int
	part, count   int
}

// wireTLM is one TLM marker's Stlm and its (Ttlm, Ptlm) pairs.
type wireTLM struct {
	stlm    byte
	tiles   []int
	lengths []int
}

// walkCodestream walks a raw J2K codestream: main-header marker segments up to
// the first SOT, then tile-parts by Psot, which must land exactly on EOC.
func walkCodestream(t *testing.T, cs []byte) (parts []wireTilePart, tlms []wireTLM) {
	t.Helper()
	if len(cs) < 4 || binary.BigEndian.Uint16(cs) != 0xFF4F {
		t.Fatalf("no SOC")
	}
	p := 2
	for {
		if p+4 > len(cs) {
			t.Fatalf("main header runs off the end")
		}
		m := binary.BigEndian.Uint16(cs[p:])
		if m == 0xFF90 {
			break
		}
		n := int(binary.BigEndian.Uint16(cs[p+2:]))
		if m == 0xFF55 { // TLM
			body := cs[p+4 : p+2+n]
			stlm := body[1]
			st := int(stlm>>4) & 3
			sp := 2
			if stlm&0x40 != 0 {
				sp = 4
			}
			var tl wireTLM
			tl.stlm = stlm
			for q := 2; q+st+sp <= len(body); q += st + sp {
				tile := 0
				switch st {
				case 1:
					tile = int(body[q])
				case 2:
					tile = int(binary.BigEndian.Uint16(body[q:]))
				}
				var l int
				if sp == 4 {
					l = int(binary.BigEndian.Uint32(body[q+st:]))
				} else {
					l = int(binary.BigEndian.Uint16(body[q+st:]))
				}
				tl.tiles = append(tl.tiles, tile)
				tl.lengths = append(tl.lengths, l)
			}
			if (len(body)-2)%(st+sp) != 0 {
				t.Errorf("TLM body of %d bytes is not a whole number of %d-byte entries", len(body)-2, st+sp)
			}
			tlms = append(tlms, tl)
		}
		p += 2 + n
	}
	for {
		m := binary.BigEndian.Uint16(cs[p:])
		if m == 0xFFD9 {
			if p+2 != len(cs) {
				t.Errorf("%d bytes after EOC", len(cs)-p-2)
			}
			return parts, tlms
		}
		if m != 0xFF90 {
			t.Fatalf("expected SOT at %d, found %#04x: Psot does not partition the codestream", p, m)
		}
		tp := wireTilePart{
			start:  p,
			tile:   int(binary.BigEndian.Uint16(cs[p+4:])),
			length: int(binary.BigEndian.Uint32(cs[p+6:])),
			part:   int(cs[p+10]),
			count:  int(cs[p+11]),
		}
		if tp.length < 14 {
			t.Fatalf("Psot %d at %d", tp.length, p)
		}
		parts = append(parts, tp)
		p += tp.length
	}
}

// tilePartTestImage is a 16-bit greyscale image with structure. Its samples
// stay below 2^15, which keeps this test independent of how the image.Image
// conversion treats the upper half of the 16-bit range.
func tilePartTestImage(w, h int) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	s := uint32(99)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			v := 2000 + 6000*((x/7+y/5)%5) + int(s%512)
			img.SetGray16(x, y, color.Gray16{Y: uint16(v)})
		}
	}
	return img
}

func TestTilePartsPerResolution(t *testing.T) {
	const w, h = 96, 80
	src := tilePartTestImage(w, h)

	type tc struct {
		name  string
		opts  func(*Options)
		tiles int
	}
	cases := []tc{
		{"ht-rpcl-plt", func(o *Options) {
			o.HighThroughput = true
			o.ProgressionOrder = RPCL
			o.WritePacketLengths = true
		}, 1},
		{"ht-rpcl-precincts", func(o *Options) {
			o.HighThroughput = true
			o.ProgressionOrder = RPCL
			o.WritePacketLengths = true
			o.PrecinctSizes = []PrecinctSize{{5, 5}}
		}, 1},
		{"ht-rpcl-no-markers", func(o *Options) {
			o.HighThroughput = true
			o.ProgressionOrder = RPCL
		}, 1},
		// SOP without PLT: the PLT this encoder writes leaves the six SOP
		// bytes out of each packet's length, so with both the index is off
		// by six bytes a packet whether or not the tile is split. That is a
		// defect of its own, not of tile-parts.
		{"part1-rlcp-sop-eph", func(o *Options) {
			o.ProgressionOrder = RLCP
			o.EnableSOP, o.EnableEPH = true, true
		}, 1},
		{"part1-rlcp-eph-plt", func(o *Options) {
			o.ProgressionOrder = RLCP
			o.WritePacketLengths = true
			o.EnableEPH = true
		}, 1},
		{"part1-lrcp-one-layer", func(o *Options) {
			o.ProgressionOrder = LRCP
			o.WritePacketLengths = true
		}, 1},
		{"part1-rlcp-layers", func(o *Options) {
			o.ProgressionOrder = RLCP
			o.NumLayers = 3
			o.WritePacketLengths = true
		}, 1},
		{"ht-rpcl-tiled", func(o *Options) {
			o.HighThroughput = true
			o.ProgressionOrder = RPCL
			o.WritePacketLengths = true
			o.TileSize = image.Point{48, 48}
		}, 4},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.Format = FormatJ2K
			opts.Lossless = true
			opts.NumResolutions = 4
			c.opts(opts)

			// The same image and options with a single tile-part per tile.
			var whole bytes.Buffer
			if err := Encode(&whole, src, opts); err != nil {
				t.Fatalf("Encode without tile-parts: %v", err)
			}
			opts.TilePartsPerResolution = true
			var split bytes.Buffer
			if err := Encode(&split, src, opts); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			cs := split.Bytes()

			// Wire: every tile is numRes tile-parts, in order, each saying
			// how many there are, and Psot walks the codestream exactly.
			parts, tlms := walkCodestream(t, cs)
			const numRes = 4
			if len(parts) != numRes*c.tiles {
				t.Fatalf("%d tile-parts, want %d (one per resolution per tile)", len(parts), numRes*c.tiles)
			}
			for i, tp := range parts {
				if tp.tile != i/numRes || tp.part != i%numRes || tp.count != numRes {
					t.Errorf("tile-part %d: Isot %d TPsot %d TNsot %d, want %d %d %d",
						i, tp.tile, tp.part, tp.count, i/numRes, i%numRes, numRes)
				}
			}

			// TLM lists every tile-part, in order, with its Psot. Stlm 0x50
			// is ST=1 (one-byte Ttlm) and SP=1 (four-byte Ptlm); the size of
			// each entry has to follow from it, which is the defect opj_dump
			// once caught.
			if opts.WritePacketLengths {
				if len(tlms) != 1 {
					t.Fatalf("%d TLM markers, want 1", len(tlms))
				}
				tl := tlms[0]
				if tl.stlm != 0x50 {
					t.Errorf("Stlm = %#02x, want 0x50", tl.stlm)
				}
				if len(tl.lengths) != len(parts) {
					t.Fatalf("TLM lists %d tile-parts, the codestream has %d", len(tl.lengths), len(parts))
				}
				for i, tp := range parts {
					if tl.tiles[i] != tp.tile || tl.lengths[i] != tp.length {
						t.Errorf("TLM entry %d: tile %d length %d, SOT says tile %d length %d",
							i, tl.tiles[i], tl.lengths[i], tp.tile, tp.length)
					}
				}
			} else if len(tlms) != 0 {
				t.Errorf("TLM written without WritePacketLengths")
			}

			// The full decode is exact.
			got, err := Decode(bytes.NewReader(cs))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !bytes.Equal(got.(*image.Gray16).Pix, src.Pix) {
				t.Errorf("decoded samples differ from the source")
			}

			// The packets are the same packets, in the same order, as the
			// single-tile-part codestream's: splitting moves the headers
			// between them and nothing else.
			wantIdx, err := BuildPacketIndex(whole.Bytes())
			if err != nil {
				t.Fatalf("BuildPacketIndex without tile-parts: %v", err)
			}
			gotIdx, err := BuildPacketIndex(cs)
			if err != nil {
				t.Fatalf("BuildPacketIndex: %v", err)
			}
			wantAddrs, gotAddrs := wantIdx.AllAddresses(), gotIdx.AllAddresses()
			if len(gotAddrs) != len(wantAddrs) {
				t.Fatalf("index has %d packets, want %d", len(gotAddrs), len(wantAddrs))
			}
			for i, a := range wantAddrs {
				if gotAddrs[i] != a {
					t.Fatalf("packet %d: address %+v, want %+v", i, gotAddrs[i], a)
				}
				wp, _ := wantIdx.GetPacket(a)
				gp, _ := gotIdx.GetPacket(a)
				if !bytes.Equal(gp, wp) {
					t.Errorf("packet %+v: %d bytes, want %d identical bytes", a, len(gp), len(wp))
				}
				// And each packet lies inside the tile-part of its own
				// resolution.
				r, ok := gotIdx.Range(a)
				if !ok {
					continue
				}
				tp := parts[int(a.Tile)*numRes+int(a.Resolution)]
				if r.Offset < tp.start || r.Offset+r.Length > tp.start+tp.length {
					t.Errorf("packet %+v at [%d,%d) is outside its resolution's tile-part [%d,%d)",
						a, r.Offset, r.Offset+r.Length, tp.start, tp.start+tp.length)
				}
			}
			_, _, fromPLT := gotIdx.IndexCost()
			if fromPLT != opts.WritePacketLengths {
				t.Errorf("index built from PLT = %v, want %v", fromPLT, opts.WritePacketLengths)
			}
		})
	}
}

// TestTilePartsPrefixIsLowResolution checks the property tile-parts per
// resolution exist for: the codestream up to the end of tile-part k, closed
// with EOC, decodes at resolution k to the same image as the whole codestream
// does. The breakpoints are found from TLM alone, as a server cutting a
// thumbnail would find them.
func TestTilePartsPrefixIsLowResolution(t *testing.T) {
	const w, h, numRes = 736, 736, 5
	src := tilePartTestImage(w, h)
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true
	opts.HighThroughput = true
	opts.ProgressionOrder = RPCL
	opts.NumResolutions = numRes
	opts.HTBlockWidth, opts.HTBlockHeight = 64, 64
	opts.WritePacketLengths = true
	opts.TilePartsPerResolution = true
	var buf bytes.Buffer
	if err := Encode(&buf, src, opts); err != nil {
		t.Fatal(err)
	}
	cs := buf.Bytes()
	parts, tlms := walkCodestream(t, cs)
	if len(tlms) != 1 || len(tlms[0].lengths) != numRes {
		t.Fatalf("TLM does not list %d tile-parts", numRes)
	}
	end := parts[0].start // the main header ends where the first SOT starts
	for k := 0; k < numRes; k++ {
		end += tlms[0].lengths[k]
		prefix := append(append([]byte{}, cs[:end]...), 0xFF, 0xD9)
		reduce := numRes - 1 - k
		want, err := DecodeConfig(bytes.NewReader(cs), &Config{ReduceResolution: reduce})
		if err != nil {
			t.Fatalf("resolution %d, whole codestream: %v", k, err)
		}
		got, err := DecodeConfig(bytes.NewReader(prefix), &Config{ReduceResolution: reduce})
		if err != nil {
			t.Fatalf("resolution %d, %d-byte prefix: %v", k, len(prefix), err)
		}
		wg, gg := want.(*image.Gray16), got.(*image.Gray16)
		if wg.Bounds() != gg.Bounds() || !bytes.Equal(wg.Pix, gg.Pix) {
			t.Errorf("resolution %d (%dx%d): the %d-byte prefix decodes differently from the whole %d bytes",
				k, gg.Bounds().Dx(), gg.Bounds().Dy(), len(prefix), len(cs))
		} else {
			t.Logf("resolution %d: %dx%d from the first %d of %d bytes", k, gg.Bounds().Dx(), gg.Bounds().Dy(), end, len(cs))
		}
	}
}

func TestTilePartsPerResolutionRefusesInterleavedOrders(t *testing.T) {
	src := tilePartTestImage(32, 32)
	for name, set := range map[string]func(*Options){
		"PCRL":          func(o *Options) { o.ProgressionOrder = PCRL },
		"CPRL":          func(o *Options) { o.ProgressionOrder = CPRL },
		"LRCP-2-layers": func(o *Options) { o.ProgressionOrder = LRCP; o.NumLayers = 2 },
		"PCRL-HT":       func(o *Options) { o.ProgressionOrder = PCRL; o.HighThroughput = true },
	} {
		opts := DefaultOptions()
		opts.Format = FormatJ2K
		opts.Lossless = true
		opts.TilePartsPerResolution = true
		set(opts)
		if err := Encode(&bytes.Buffer{}, src, opts); err == nil {
			t.Errorf("%s: Encode accepted TilePartsPerResolution", name)
		}
	}
}
