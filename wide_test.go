package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// hostileFloats returns n binary32 samples that span the encoding rather than a
// convenient corner of it.
//
// The float path used to be tested only with small positive integers, which
// occupy a handful of magnitude bits. Nothing about them ever drove a wavelet
// coefficient past 32 bits, so the overflow that made every float codestream
// unreadable to any other implementation never showed up.
func hostileFloats(n, seed int) []float32 {
	special := []uint32{
		0x00000000, 0x80000000, // +0, -0
		0x7f800000, 0xff800000, // +Inf, -Inf
		0x7fc00000, 0xffc00000, // quiet NaNs
		0x7fffffff, 0xffffffff, // NaNs with every payload bit set
		0x00000001, 0x80000001, // smallest denormals
		0x007fffff, 0x807fffff, // largest denormals
		0x7f7fffff, 0xff7fffff, // +/- FLT_MAX
		0x3f800000, 0xbf800000, // +/- 1
	}
	out := make([]float32, n)
	state := uint32(seed)*1664525 + 1013904223
	for i := range out {
		if i < len(special) {
			out[i] = math.Float32frombits(special[i])
			continue
		}
		state = state*1664525 + 1013904223
		out[i] = math.Float32frombits(state ^ (state >> 15))
	}
	return out
}

func floatFixtureImage(w, h, comps int) *FloatImage {
	img := &FloatImage{Width: w, Height: h, BitDepth: 32, Signed: true,
		Components: make([][]float32, comps)}
	for c := 0; c < comps; c++ {
		img.Components[c] = hostileFloats(w*h, c+1)
	}
	return img
}

func sameFloatBits(a, b []float32) (int, int) {
	diff, first := 0, -1
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			diff++
			if first < 0 {
				first = i
			}
		}
	}
	return diff, first
}

// TestFloatRoundTripHostileContent covers EncodeFloat and DecodeFloat over
// genuine binary32 content at every resolution count, tiled and untiled, with
// one component and with three.
func TestFloatRoundTripHostileContent(t *testing.T) {
	for _, comps := range []int{1, 3} {
		for _, nres := range []int{1, 2, 3, 6} {
			for _, tile := range []int{0, 8, 13} {
				img := floatFixtureImage(32, 32, comps)
				opts := &Options{
					HighThroughput: true, Lossless: true,
					Format: FormatJ2K, NumResolutions: nres,
				}
				if tile > 0 {
					opts.TileSize.X, opts.TileSize.Y = tile, tile
				}
				var buf bytes.Buffer
				if err := EncodeFloat(&buf, img, opts); err != nil {
					t.Fatalf("comps=%d nres=%d tile=%d: EncodeFloat: %v", comps, nres, tile, err)
				}
				got, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
				if err != nil {
					t.Fatalf("comps=%d nres=%d tile=%d: DecodeFloat: %v", comps, nres, tile, err)
				}
				if len(got.Components) != comps {
					t.Fatalf("comps=%d nres=%d tile=%d: decoded %d components",
						comps, nres, tile, len(got.Components))
				}
				for c := 0; c < comps; c++ {
					if n, first := sameFloatBits(img.Components[c], got.Components[c]); n != 0 {
						t.Fatalf("comps=%d nres=%d tile=%d: component %d has %d differing samples, first at %d: %08x vs %08x",
							comps, nres, tile, c, n, first,
							math.Float32bits(img.Components[c][first]),
							math.Float32bits(got.Components[c][first]))
					}
				}
			}
		}
	}
}

// parseHeader reads the marker segments of a J2K codestream this package wrote.
func parseHeader(t *testing.T, cs []byte) *codestream.Header {
	t.Helper()
	h, err := codestream.NewParser(bytes.NewReader(cs)).ReadHeader()
	if err != nil {
		t.Fatalf("parsing our own codestream: %v", err)
	}
	return h
}

// TestFloatSignalsWideMagnitudeBudget checks the numbers the codestream
// declares, not just that it round-trips.
//
// The guard bits and exponents below are the ones ojph_compress writes for the
// same geometry, read out of its QCD markers. They are what makes the file
// readable by anything else: a binary32 component needs 33 magnitude bit-planes
// after one reversible 5/3 level and 34 after two, and 35 once the RCT has
// widened the chrominance differences. This encoder used to signal 2 guard bits
// against an exponent clamped to 31, i.e. Mb = 32 for every subband, which is
// one to three bit-planes short of what it then emitted.
func TestFloatSignalsWideMagnitudeBudget(t *testing.T) {
	cases := []struct {
		comps, nres int
		guard       int
		exps        []int
	}{
		// One component, two decomposition levels: LL and the finest level's
		// detail bands take 33 bit-planes, the coarser level's take 34.
		{1, 3, 4, []int{30, 31, 31, 31, 30, 30, 30}},
		// One decomposition level: every band takes 33.
		{1, 2, 3, []int{31, 31, 31, 31}},
		// Three components: the RCT adds one bit-plane throughout.
		{3, 3, 5, []int{30, 31, 31, 31, 30, 30, 30}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := EncodeFloat(&buf, floatFixtureImage(32, 32, c.comps), &Options{
			HighThroughput: true, Lossless: true,
			Format: FormatJ2K, NumResolutions: c.nres,
		}); err != nil {
			t.Fatalf("comps=%d nres=%d: EncodeFloat: %v", c.comps, c.nres, err)
		}
		h := parseHeader(t, buf.Bytes())
		if got := h.Quantization.GuardBits(); got != c.guard {
			t.Errorf("comps=%d nres=%d: %d guard bits, want %d", c.comps, c.nres, got, c.guard)
		}
		if len(h.Quantization.StepSizes) != len(c.exps) {
			t.Fatalf("comps=%d nres=%d: %d step sizes, want %d",
				c.comps, c.nres, len(h.Quantization.StepSizes), len(c.exps))
		}
		for i, want := range c.exps {
			if got := int(h.Quantization.StepSizes[i].Exponent); got != want {
				t.Errorf("comps=%d nres=%d: subband %d exponent %d, want %d",
					c.comps, c.nres, i, got, want)
			}
		}
		if !h.WideSamples() {
			t.Errorf("comps=%d nres=%d: the header does not report a wide magnitude budget", c.comps, c.nres)
		}
		if mb := h.MaxBandMb(); mb <= codestream.MaxBitPlanes {
			t.Errorf("comps=%d nres=%d: largest Mb is %d, which fits a 32-bit word",
				c.comps, c.nres, mb)
		}
	}
}

// TestFloatDeclaresMagnitudeBudgetInCAP checks Ccap^15's B_p field, which tells
// a decoder how wide a coefficient word it needs before it reads a single
// packet. It was a constant declaring ten bit-planes whatever the file held.
func TestFloatDeclaresMagnitudeBudgetInCAP(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, floatFixtureImage(32, 32, 1), &Options{
		HighThroughput: true, Lossless: true,
		Format: FormatJ2K, NumResolutions: 3,
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	h := parseHeader(t, buf.Bytes())
	if h.Capabilities == nil || len(h.Capabilities.CCAPi) == 0 {
		t.Fatal("no Ccap field in the CAP marker")
	}
	// ojph_compress writes 0x0015 for a binary32 codestream, which is B_p = 21,
	// covering 32 to 35 magnitude bit-planes.
	if got := h.Capabilities.CCAPi[0]; got != 0x0015 {
		t.Errorf("Ccap = %#04x, want %#04x (B_p = 21)", got, 0x0015)
	}
}

// TestCcapMagB pins the B_p mapping to the values ojph_compress writes.
func TestCcapMagB(t *testing.T) {
	cases := []struct {
		magb int
		want uint16
	}{
		{7, 0},   // 8-bit greyscale, no decomposition
		{8, 0},   //
		{10, 2},  // 8-bit greyscale, two decompositions
		{15, 7},  // 16-bit, no decomposition
		{18, 10}, // 16-bit, two decompositions
		{33, 21}, // binary32
		{34, 21},
		{35, 21}, // binary32 under the RCT
		{48, 31},
	}
	for _, c := range cases {
		if got := ccapMagB(c.magb); got != c.want {
			t.Errorf("ccapMagB(%d) = %d, want %d", c.magb, got, c.want)
		}
	}
}

// TestNLTAllComponentsAccepted covers the Cnlt form OpenJPH writes.
//
// ISO/IEC 15444-2 A.3.10 lets one NLT marker segment apply to every component
// by setting Cnlt to 0xFFFF, and that is what the reference emits for a float
// codestream. This parser read Cnlt as a plain component index and rejected
// 65535 as out of range, so no OpenJPH float file could be opened at all.
func TestNLTAllComponentsAccepted(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeFloat(&buf, floatFixtureImage(16, 16, 1), &Options{
		HighThroughput: true, Lossless: true,
		Format: FormatJ2K, NumResolutions: 3,
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	cs := buf.Bytes()

	// Rewrite this file's per-component Cnlt as the all-components form. Only
	// that field changes, so a decode that now fails can only be failing on it.
	patched := append([]byte(nil), cs...)
	found := false
	for i := 0; i+8 <= len(patched); i++ {
		if binary.BigEndian.Uint16(patched[i:]) == uint16(codestream.NLT) &&
			binary.BigEndian.Uint16(patched[i+2:]) == 6 {
			binary.BigEndian.PutUint16(patched[i+4:], codestream.NLTAllComponents)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no NLT marker segment in our own float codestream")
	}

	want, err := DecodeFloat(bytes.NewReader(cs))
	if err != nil {
		t.Fatalf("DecodeFloat on the unpatched stream: %v", err)
	}
	got, err := DecodeFloat(bytes.NewReader(patched))
	if err != nil {
		t.Fatalf("DecodeFloat with Cnlt = 0xFFFF: %v", err)
	}
	if n, first := sameFloatBits(want.Components[0], got.Components[0]); n != 0 {
		t.Fatalf("Cnlt = 0xFFFF decoded %d samples differently, first at %d", n, first)
	}
}

// TestNominalWideBitsMatchesReference pins the per-subband magnitude budget to
// the values ojph_compress derives, which were read back out of the QCD markers
// it writes at 0 to 5 decomposition levels.
func TestNominalWideBitsMatchesReference(t *testing.T) {
	cases := []struct {
		nres, comps int
		want        []int
	}{
		{1, 1, []int{31}},
		{2, 1, []int{33, 33, 33, 33}},
		{3, 1, []int{33, 34, 34, 34, 33, 33, 33}},
		{4, 1, []int{33, 34, 34, 34, 34, 34, 34, 33, 33, 33}},
		{3, 3, []int{34, 35, 35, 35, 34, 34, 34}},
	}
	for _, c := range cases {
		e := &encoder{
			options:            &Options{HighThroughput: true, Lossless: true, NumResolutions: c.nres},
			numComponents:      c.comps,
			width:              256,
			height:             256,
			componentPrecision: make([]int, c.comps),
		}
		for i := range e.componentPrecision {
			e.componentPrecision[i] = 32
		}
		got := e.nominalWideBits()
		if len(got) != len(c.want) {
			t.Fatalf("nres=%d comps=%d: %d subbands, want %d", c.nres, c.comps, len(got), len(c.want))
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("nres=%d comps=%d: subband %d needs %d bit-planes, want %d",
					c.nres, c.comps, i, got[i], c.want[i])
			}
		}
	}
}

// TestFloatBudgetCoversExtremeSample checks the case the reference's own
// nominal rule does not cover.
//
// The float bit pattern 0xFFFFFFFF becomes -2^31 under the NLT Type 3 point
// transform, which is 32 magnitude bits where a signed 32-bit sample nominally
// has 31. ojph_compress signals 31 for it at zero decomposition levels and then
// cannot read its own file back. This encoder measures the transformed
// coefficients and raises the budget, so the sample survives.
func TestFloatBudgetCoversExtremeSample(t *testing.T) {
	comp := make([]float32, 16*16)
	for i := range comp {
		comp[i] = math.Float32frombits(0xFFFFFFFF)
	}
	img := &FloatImage{Width: 16, Height: 16, BitDepth: 32, Signed: true,
		Components: [][]float32{comp}}

	var buf bytes.Buffer
	if err := EncodeFloat(&buf, img, &Options{
		HighThroughput: true, Lossless: true,
		Format: FormatJ2K, NumResolutions: 1,
	}); err != nil {
		t.Fatalf("EncodeFloat: %v", err)
	}
	h := parseHeader(t, buf.Bytes())
	if mb := h.MaxBandMb(); mb < 32 {
		t.Errorf("signalled Mb is %d, too small for a 32-bit magnitude", mb)
	}
	got, err := DecodeFloat(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeFloat: %v", err)
	}
	if n, first := sameFloatBits(comp, got.Components[0]); n != 0 {
		t.Fatalf("%d samples differ, first at %d", n, first)
	}
}
