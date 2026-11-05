package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
)

// xorshift32 is a small deterministic generator for test content.
type xorshift32 uint32

func (s *xorshift32) next() uint32 {
	x := uint32(*s)
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	*s = xorshift32(x)
	return x
}

// ctLikeFrame returns a synthetic CT slice in Hounsfield units: air around
// -1000, a body ellipse of soft tissue between -100 and +80, a ring of bone up
// to +2000 and a few dense spots, all with noise. Real CT is mostly smooth
// regions with structure, which is the content the wavelet and the block coder
// are tuned for; uniform noise alone would leave most of the coder unexercised.
func ctLikeFrame(w, h int, seed uint32) []int16 {
	rng := xorshift32(seed*2654435761 | 1)
	noise := func(amp int) int { return int(rng.next()%uint32(2*amp+1)) - amp }
	out := make([]int16, w*h)
	cx, cy := float64(w)/2, float64(h)/2
	ax, ay := 0.42*float64(w), 0.36*float64(h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := (float64(x)-cx)/ax, (float64(y)-cy)/ay
			r := math.Sqrt(dx*dx + dy*dy)
			var v int
			switch {
			case r > 1:
				v = -1000 + noise(20)
			case r > 0.93:
				// Cortical bone ring.
				v = 1200 + int(800*(1-math.Abs(r-0.965)/0.035)) + noise(40)
			default:
				// Soft tissue with a slow gradient and two organs.
				v = -100 + int(180*(0.5+0.5*math.Sin(float64(x)/23)*math.Cos(float64(y)/31))) + noise(15)
				if (dx-0.3)*(dx-0.3)+dy*dy < 0.04 {
					v = 40 + noise(10)
				}
				if (dx+0.35)*(dx+0.35)+(dy-0.1)*(dy-0.1) < 0.02 {
					v = -60 + noise(10)
				}
			}
			if v > 2000 {
				v = 2000
			}
			out[y*w+x] = int16(v)
		}
	}
	// Sparse dense spots: contrast, calcifications, metal.
	for i := 0; i < w*h/2000; i++ {
		x, y := int(rng.next()%uint32(w)), int(rng.next()%uint32(h))
		out[y*w+x] = int16(1500 + rng.next()%1500)
	}
	return out
}

// sizSsiz returns the Ssiz byte of component c from a raw J2K codestream: SOC
// (2 bytes), then the SIZ marker, whose component records start 40 bytes in.
func sizSsiz(t *testing.T, cs []byte, c int) byte {
	t.Helper()
	if len(cs) < 45+3*c || binary.BigEndian.Uint16(cs[2:4]) != 0xFF51 {
		t.Fatalf("no SIZ marker at the start of the codestream")
	}
	return cs[2+40+3*c]
}

func TestEncodeInt16RoundTrip(t *testing.T) {
	const w, h = 96, 80

	extremes := make([]int16, w*h)
	probes := []int16{math.MinInt16, math.MaxInt16, 0, -1, 1, math.MinInt16 + 1, math.MaxInt16 - 1, -1024, 3071}
	for i := range extremes {
		extremes[i] = probes[i%len(probes)]
	}
	alternating := make([]int16, w*h)
	for i := range alternating {
		if (i/w+i%w)%2 == 0 {
			alternating[i] = math.MinInt16
		} else {
			alternating[i] = math.MaxInt16
		}
	}
	noise := make([]int16, w*h)
	rng := xorshift32(12345)
	for i := range noise {
		noise[i] = int16(rng.next())
	}
	constant := func(v int16) []int16 {
		out := make([]int16, w*h)
		for i := range out {
			out[i] = v
		}
		return out
	}

	planes := []struct {
		name string
		data [][]int16
	}{
		{"ct", [][]int16{ctLikeFrame(w, h, 1)}},
		{"extremes", [][]int16{extremes}},
		{"checkerboard-min-max", [][]int16{alternating}},
		{"full-range-noise", [][]int16{noise}},
		{"constant-min", [][]int16{constant(math.MinInt16)}},
		{"constant-max", [][]int16{constant(math.MaxInt16)}},
		{"three-components", [][]int16{ctLikeFrame(w, h, 2), noise, extremes}},
	}
	type config struct {
		name string
		opts func(*Options)
	}
	configs := []config{
		{"part1", func(o *Options) {}},
		{"ht", func(o *Options) { o.HighThroughput = true }},
		{"ht-rpcl-dicom", func(o *Options) {
			// The DICOM HTJ2K Lossless RPCL parameters.
			o.HighThroughput = true
			o.ProgressionOrder = RPCL
			o.NumResolutions = 5
			o.HTBlockWidth, o.HTBlockHeight = 64, 64
			o.WritePacketLengths = true
		}},
		{"part1-tiled", func(o *Options) { o.TileSize = image.Point{40, 40} }},
		{"ht-one-resolution", func(o *Options) { o.HighThroughput = true; o.NumResolutions = 1 }},
	}

	for _, p := range planes {
		for _, cfg := range configs {
			t.Run(p.name+"/"+cfg.name, func(t *testing.T) {
				src := &Int16Image{Width: w, Height: h, Components: p.data}
				opts := DefaultOptions()
				opts.Format = FormatJ2K
				cfg.opts(opts)

				var buf bytes.Buffer
				if err := EncodeInt16(&buf, src, opts); err != nil {
					t.Fatalf("EncodeInt16: %v", err)
				}
				for c := range p.data {
					if got := sizSsiz(t, buf.Bytes(), c); got != 0x8F {
						t.Errorf("component %d: Ssiz = %#02x, want 0x8F (16-bit signed)", c, got)
					}
				}

				got, err := DecodeInt16(bytes.NewReader(buf.Bytes()))
				if err != nil {
					t.Fatalf("DecodeInt16: %v", err)
				}
				if got.Width != w || got.Height != h || len(got.Components) != len(p.data) {
					t.Fatalf("decoded %dx%d with %d components, want %dx%d with %d",
						got.Width, got.Height, len(got.Components), w, h, len(p.data))
				}
				for c := range p.data {
					bad := 0
					for i, want := range p.data[c] {
						if got.Components[c][i] != want {
							if bad < 3 {
								t.Errorf("component %d sample %d: got %d, want %d", c, i, got.Components[c][i], want)
							}
							bad++
						}
					}
					if bad > 0 {
						t.Errorf("component %d: %d of %d samples wrong", c, bad, len(p.data[c]))
					}
				}
			})
		}
	}
}

// TestEncodeInt16IsLossless checks that EncodeInt16 codes losslessly even when
// the options ask for lossy, and does not write that choice back into the
// caller's options.
func TestEncodeInt16IsLossless(t *testing.T) {
	src := &Int16Image{Width: 32, Height: 32, Components: [][]int16{ctLikeFrame(32, 32, 3)}}
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = false
	opts.Quality = 10
	var buf bytes.Buffer
	if err := EncodeInt16(&buf, src, opts); err != nil {
		t.Fatalf("EncodeInt16: %v", err)
	}
	if opts.Lossless {
		t.Errorf("EncodeInt16 modified the caller's Options")
	}
	got, err := DecodeInt16(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeInt16: %v", err)
	}
	for i, want := range src.Components[0] {
		if got.Components[0][i] != want {
			t.Fatalf("sample %d: got %d, want %d", i, got.Components[0][i], want)
		}
	}
}

// TestEncodeInt16JP2 checks that the JP2 wrapper agrees with SIZ about the
// sign: the ihdr box's BPC uses Ssiz's encoding, so 16-bit signed is 0x8F.
func TestEncodeInt16JP2(t *testing.T) {
	src := &Int16Image{Width: 24, Height: 16, Components: [][]int16{ctLikeFrame(24, 16, 4)}}
	opts := DefaultOptions()
	opts.Format = FormatJP2
	var buf bytes.Buffer
	if err := EncodeInt16(&buf, src, opts); err != nil {
		t.Fatalf("EncodeInt16: %v", err)
	}
	b := buf.Bytes()
	i := bytes.Index(b, []byte("ihdr"))
	if i < 0 || i+4+11 > len(b) {
		t.Fatalf("no ihdr box")
	}
	// ihdr contents: HEIGHT(4) WIDTH(4) NC(2) BPC(1) ...
	if bpc := b[i+4+10]; bpc != 0x8F {
		t.Errorf("ihdr BPC = %#02x, want 0x8F", bpc)
	}
	got, err := DecodeInt16(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("DecodeInt16: %v", err)
	}
	for i, want := range src.Components[0] {
		if got.Components[0][i] != want {
			t.Fatalf("sample %d: got %d, want %d", i, got.Components[0][i], want)
		}
	}
}

// TestDecodeInt16Refuses checks that data which is not signed integers of at
// most 16 bits is refused rather than reinterpreted.
func TestDecodeInt16Refuses(t *testing.T) {
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.Lossless = true

	gray := image.NewGray16(image.Rect(0, 0, 8, 8))
	gray.SetGray16(1, 1, color.Gray16{Y: 40000})
	var unsigned bytes.Buffer
	if err := Encode(&unsigned, gray, opts); err != nil {
		t.Fatal(err)
	}
	var half bytes.Buffer
	if err := EncodeHalf(&half, &HalfImage{Width: 8, Height: 8, Components: [][]uint16{make([]uint16, 64)}}, opts); err != nil {
		t.Fatal(err)
	}
	var float bytes.Buffer
	if err := EncodeFloat(&float, &FloatImage{Width: 8, Height: 8, Components: [][]float32{make([]float32, 64)}}, opts); err != nil {
		t.Fatal(err)
	}

	// A signed codestream whose SIZ claims 20-bit samples: the refusal is
	// made from the header, before anything is decoded.
	var signed bytes.Buffer
	if err := EncodeInt16(&signed, &Int16Image{Width: 8, Height: 8, Components: [][]int16{make([]int16, 64)}}, opts); err != nil {
		t.Fatal(err)
	}
	wide := append([]byte{}, signed.Bytes()...)
	if sizSsiz(t, wide, 0) != 0x8F {
		t.Fatal("fixture is not 16-bit signed")
	}
	wide[2+40] = 0x80 | (20 - 1)

	for name, cs := range map[string][]byte{
		"unsigned":      unsigned.Bytes(),
		"half":          half.Bytes(),
		"float":         float.Bytes(),
		"20-bit signed": wide,
		"not JPEG 2000": []byte("definitely not a codestream"),
		"truncated SIZ": signed.Bytes()[:20],
		"empty":         nil,
	} {
		if _, err := DecodeInt16(bytes.NewReader(cs)); err == nil {
			t.Errorf("%s: DecodeInt16 accepted it", name)
		}
	}
}

// TestEncodeInt16RejectsBadInput checks the size validation.
func TestEncodeInt16RejectsBadInput(t *testing.T) {
	for name, img := range map[string]*Int16Image{
		"nil components": {Width: 4, Height: 4},
		"zero size":      {Width: 0, Height: 4, Components: [][]int16{{}}},
		"short plane":    {Width: 4, Height: 4, Components: [][]int16{make([]int16, 15)}},
		"ragged":         {Width: 4, Height: 4, Components: [][]int16{make([]int16, 16), make([]int16, 12)}},
	} {
		if err := EncodeInt16(&bytes.Buffer{}, img, nil); err == nil {
			t.Errorf("%s: EncodeInt16 accepted it", name)
		}
	}

	// A format this encoder does not write is refused, not written as
	// something else.
	opts := DefaultOptions()
	opts.Format = FormatJPX
	img := &Int16Image{Width: 4, Height: 4, Components: [][]int16{make([]int16, 16)}}
	if err := EncodeInt16(&bytes.Buffer{}, img, opts); err == nil {
		t.Errorf("FormatJPX: EncodeInt16 accepted it")
	}
}

// TestDecodeInt16ReducedResolution checks the reduced decode of a signed
// image keeps the sign: the LL band of a frame that is mostly air is mostly
// negative, and a reconstruction that lost the sign would be positive.
func TestDecodeInt16ReducedResolution(t *testing.T) {
	const w, h = 128, 96
	src := &Int16Image{Width: w, Height: h, Components: [][]int16{ctLikeFrame(w, h, 5)}}
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	opts.HighThroughput = true
	opts.NumResolutions = 4
	var buf bytes.Buffer
	if err := EncodeInt16(&buf, src, opts); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInt16Config(bytes.NewReader(buf.Bytes()), &Config{ReduceResolution: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != w/4 || got.Height != h/4 {
		t.Fatalf("decoded %dx%d, want %dx%d", got.Width, got.Height, w/4, h/4)
	}
	// The corner is air in the source, around -1000.
	if v := got.Components[0][0]; v > -900 || v < -1100 {
		t.Errorf("corner sample at 1/4 resolution = %d, want about -1000", v)
	}
}

func TestInt16ImageAt(t *testing.T) {
	m := &Int16Image{Width: 2, Height: 2, Components: [][]int16{{-1, 2, -3, 4}, {5, 6}}}
	if got := m.At(0, 1); got[0] != -3 || got[1] != 0 {
		t.Errorf("At(0,1) = %v, want [-3 0]", got)
	}
	if m.At(2, 0) != nil || m.At(0, -1) != nil {
		t.Errorf("At outside the image should be nil")
	}
	if m.Bounds() != image.Rect(0, 0, 2, 2) || m.ComponentCount() != 2 {
		t.Errorf("Bounds/ComponentCount wrong")
	}
}
