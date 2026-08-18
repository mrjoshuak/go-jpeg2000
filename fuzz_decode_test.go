package jpeg2000

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// halfSeedCodestream builds the reference half-float codestream used as the
// fuzz seed and as the base for the corruption sweeps below. It is the
// smallest thing this package can produce that exercises the whole decode
// path: SIZ, COD, QCD, CAP, NLT, a tile-part header and code-block data.
func halfSeedCodestream(t testing.TB) []byte {
	t.Helper()
	const w, h = 16, 12
	src := make([]uint16, w*h)
	for i := range src {
		src[i] = uint16(i * 7)
	}
	var buf bytes.Buffer
	if err := EncodeHalf(&buf, &HalfImage{Width: w, Height: h, Components: [][]uint16{src}}, nil); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	return buf.Bytes()
}

// decodeEveryEntryPoint runs every public decode entry point over the same
// bytes. None of them may panic, and none may block: the callers of this
// package hand it files that arrived over a network.
func decodeEveryEntryPoint(data []byte) {
	_, _ = Decode(bytes.NewReader(data))
	_, _ = DecodeConfig(bytes.NewReader(data), nil)
	_, _ = DecodeConfig(bytes.NewReader(data), &Config{ReduceResolution: 2, QualityLayers: 2})
	_, _ = DecodeHalf(bytes.NewReader(data))
	_, _ = DecodeHalfConfig(bytes.NewReader(data), nil)
	_, _ = DecodeFloat(bytes.NewReader(data))
	_, _ = DecodeFloatConfig(bytes.NewReader(data), &Config{ReduceResolution: 1})
	_, _ = DecodeMetadata(bytes.NewReader(data))
	_, _ = ExtractPackets(data)
	_, _ = BuildPacketIndex(data)
	_, _ = NewProgressiveDecoderFromCodestream(data)
}

// FuzzDecodeHalf drives every public decode entry point with mutations of a
// real half-float codestream. The seed corpus in testdata/fuzz keeps the
// specific malformed inputs that used to panic or hang under test on every
// `go test` run, not only when fuzzing is enabled.
//
// Run with: go test -run=Fuzz -fuzz=FuzzDecodeHalf -fuzztime=60s
func FuzzDecodeHalf(f *testing.F) {
	valid := halfSeedCodestream(f)
	f.Add(valid)

	// Single-byte corruptions of the SIZ and COD fields that size
	// allocations: image size, image origin, tile origin, decomposition
	// count and the two code-block exponents.
	for _, off := range []int{93, 97, 98, 101, 105, 117, 121, 139, 140, 141} {
		if off >= len(valid) {
			continue
		}
		bad := append([]byte(nil), valid...)
		bad[off] ^= 0xFF
		f.Add(bad)
	}

	// Truncations at each structural boundary.
	for _, n := range []int{0, 1, 2, 12, 85, 87, 91, 127, 130, 144, 180, 200} {
		if n <= len(valid) {
			f.Add(append([]byte(nil), valid[:n]...))
		}
	}

	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		decodeEveryEntryPoint(data)
	})
}

// halfSeedRawCodestream builds the same image as a bare J2K codestream, with
// no JP2 box wrapper. Fuzzing this form spends the whole budget inside the
// codestream parser and the tile decoder instead of on the box layer, which
// rejects almost every mutation in its first twelve bytes.
func halfSeedRawCodestream(t testing.TB) []byte {
	t.Helper()
	const w, h = 16, 12
	src := make([]uint16, w*h)
	for i := range src {
		src[i] = uint16(i*11 + 3)
	}
	opts := DefaultOptions()
	opts.Format = FormatJ2K
	var buf bytes.Buffer
	if err := EncodeHalf(&buf, &HalfImage{Width: w, Height: h, Components: [][]uint16{src}}, opts); err != nil {
		t.Fatalf("EncodeHalf: %v", err)
	}
	return buf.Bytes()
}

// FuzzDecodeCodestream fuzzes the raw J2K path. The SOC marker is re-attached
// to every input so that no execution is wasted on the format sniff: each one
// reaches readSIZ and, if the header survives, the tile decoder.
//
// Run with: go test -run=Fuzz -fuzz=FuzzDecodeCodestream -fuzztime=60s
func FuzzDecodeCodestream(f *testing.F) {
	raw := halfSeedRawCodestream(f)
	if len(raw) < 2 {
		f.Fatal("raw codestream is too short to seed")
	}
	body := raw[2:] // strip SOC; the fuzz function puts it back

	f.Add(body)
	for _, off := range []int{6, 10, 11, 14, 15, 30, 31, 32, 38, 46, 52, 53, 54, 55, 56} {
		if off >= len(body) {
			continue
		}
		bad := append([]byte(nil), body...)
		bad[off] ^= 0xFF
		f.Add(bad)
	}
	for n := 0; n < len(body) && n < 64; n += 4 {
		f.Add(append([]byte(nil), body[:n]...))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cs := make([]byte, 0, len(data)+2)
		cs = append(cs, 0xFF, 0x4F)
		cs = append(cs, data...)
		decodeEveryEntryPoint(cs)
	})
}

// TestDecodeCorruptedRawCodestreamNeverPanics runs the flip and truncate sweep
// over the bare J2K form, where every byte the sweep touches is a codestream
// field rather than a JP2 box header.
func TestDecodeCorruptedRawCodestreamNeverPanics(t *testing.T) {
	raw := halfSeedRawCodestream(t)

	run := func(name string, data []byte) {
		defer func() {
			if e := recover(); e != nil {
				t.Fatalf("%s: panic: %v\n%s", name, e, debugStack())
			}
		}()
		decodeEveryEntryPoint(data)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for i := 0; i < len(raw); i++ {
		flipped := append([]byte(nil), raw...)
		flipped[i] ^= 0xFF
		run(fmt.Sprintf("flip %d", i), flipped)
		run(fmt.Sprintf("truncate %d", i), raw[:i])
		if time.Now().After(deadline) {
			t.Fatalf("sweep did not finish within its time budget (stalled at offset %d)", i)
		}
	}
}

// TestDecodeCorruptedHalfNeverPanics replays the full single-byte flip and
// truncation sweep over the reference codestream through every decode entry
// point. Each case must return an error rather than panicking, and the sweep
// as a whole must finish promptly: a decoder that loops on a corrupt loop
// bound shows up here as a timeout rather than as a wrong answer.
func TestDecodeCorruptedHalfNeverPanics(t *testing.T) {
	valid := halfSeedCodestream(t)

	run := func(name string, data []byte) {
		defer func() {
			if e := recover(); e != nil {
				t.Fatalf("%s: panic: %v\n%s", name, e, debugStack())
			}
		}()
		decodeEveryEntryPoint(data)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for i := 0; i < len(valid); i++ {
		flipped := append([]byte(nil), valid...)
		flipped[i] ^= 0xFF
		run(fmt.Sprintf("flip %d", i), flipped)

		run(fmt.Sprintf("truncate %d", i), valid[:i])

		if time.Now().After(deadline) {
			t.Fatalf("sweep did not finish within its time budget (stalled at offset %d)", i)
		}
	}
}

// TestDecodeCorruptedHalfBoundedAllocation asserts the decoder never turns a
// few hundred bytes into a large allocation. A header field claiming a
// 60000x60000 image is eight bytes long; honouring it would be a remote
// out-of-memory for any caller that decodes untrusted files.
func TestDecodeCorruptedHalfBoundedAllocation(t *testing.T) {
	valid := halfSeedCodestream(t)

	// Amplification is bounded by maxDecodedSamples, an absolute cap, not by a
	// ratio to the input length -- see the comment on that constant for why a
	// ratio cannot separate a hostile claim from a legitimately tiny encoding
	// of a large flat image. So a small input can still reach tens of
	// megabytes; what it can no longer do is exhaust memory. Before these
	// guards this same sweep reached 205 GB on a single input.
	//
	// 64 MiB is chosen to sit above the measured worst case (about 17 MB at
	// flip 95) with room for the allocator to move, and far below anything that
	// threatens a host.
	const budget = (64 << 20) * allocBudgetScale

	var worst uint64
	worstOff := -1
	for i := 0; i < len(valid); i++ {
		flipped := append([]byte(nil), valid...)
		flipped[i] ^= 0xFF

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		decodeEveryEntryPoint(flipped)
		runtime.ReadMemStats(&after)

		grew := after.TotalAlloc - before.TotalAlloc
		if grew > worst {
			worst, worstOff = grew, i
		}
		if grew > budget {
			t.Fatalf("flip %d: decoding a %d-byte input allocated %d bytes, above the %d byte budget",
				i, len(flipped), grew, uint64(budget))
		}
	}
	t.Logf("worst case: flip %d allocated %d bytes from %d input bytes", worstOff, worst, len(valid))
}

// TestDecodeRejectsOversizedImage checks the specific guard the SIZ marker
// needs: an image area no plausible codestream of this length could carry must
// be refused, with an error that names the field.
func TestDecodeRejectsOversizedImage(t *testing.T) {
	valid := halfSeedCodestream(t)

	// Locate the SIZ marker inside the JP2 container and overwrite Xsiz/Ysiz
	// with 60000x60000.
	sizPos := bytes.Index(valid, []byte{0xFF, 0x4F, 0xFF, 0x51})
	if sizPos < 0 {
		t.Fatal("no SOC/SIZ marker in the reference codestream")
	}
	huge := append([]byte(nil), valid...)
	// SOC(2) + SIZ(2) + Lsiz(2) + Rsiz(2) puts Xsiz eight bytes past SOC.
	xsiz := sizPos + 8
	put32 := func(off int, v uint32) {
		huge[off] = byte(v >> 24)
		huge[off+1] = byte(v >> 16)
		huge[off+2] = byte(v >> 8)
		huge[off+3] = byte(v)
	}
	put32(xsiz, 60000)
	put32(xsiz+4, 60000)

	_, err := DecodeHalf(bytes.NewReader(huge))
	if err == nil {
		t.Fatal("DecodeHalf accepted a 60000x60000 image in a 372-byte file")
	}
	if !strings.Contains(err.Error(), "justify") && !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error does not explain the size limit: %v", err)
	}

	if _, err := Decode(bytes.NewReader(huge)); err == nil {
		t.Fatal("Decode accepted a 60000x60000 image in a 372-byte file")
	}
}

// TestSeedCorpusFilesPresent guards the fuzz seed corpora against being
// dropped from the repository, since the corpora are what keep the historical
// crashers under test on an ordinary `go test` run.
func TestSeedCorpusFilesPresent(t *testing.T) {
	for _, target := range []string{"FuzzDecodeHalf", "FuzzDecodeCodestream"} {
		dir := filepath.Join("testdata", "fuzz", target)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s seed corpus: %v", target, err)
		}
		if len(entries) < 16 {
			t.Errorf("%s seed corpus has %d files, expected at least 16", target, len(entries))
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s/%s: %v", target, e.Name(), err)
			}
			if !bytes.HasPrefix(data, []byte("go test fuzz v1")) {
				t.Errorf("%s/%s is not a Go fuzz corpus file", target, e.Name())
			}
		}
	}
}

func debugStack() []byte {
	buf := make([]byte, 8192)
	return buf[:runtime.Stack(buf, false)]
}
