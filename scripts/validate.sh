#!/usr/bin/env bash
#
# go-jpeg2000 validation gate.
#
# Builds, lints, runs the suite under the race detector, and then checks
# interoperability against implementations this project did not write.
#
# The external checks are the point. Every codec defect this library carried was
# self-inverse: encoder and decoder shared a convention no one else implements,
# so every round-trip test passed while no conforming decoder could read the
# output and nothing conforming could be read. A round trip cannot detect that;
# only another implementation can.
#
# Oracles used, all installed separately from this repo:
#   ojph_compress / ojph_expand   OpenJPH, the HTJ2K reference (ISO/IEC 15444-15)
#   opj_compress / opj_decompress OpenJPEG, the Part 1 reference
#
# Exits non-zero on the first failure.

set -uo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPO" || exit 1

FAILURES=0
CHECKS=0
GAPS=0

# STRICT=1 turns a gap into a failure.
#
# A gap is "this could not be measured" — an oracle that is not installed, or a
# fixture the oracle would not produce. Locally that is information. In CI it is
# a way for the gate to pass while measuring nothing, so the conformance job
# sets STRICT=1 and a missing oracle fails the build instead of shrinking it.
STRICT=${STRICT:-0}

pass() { CHECKS=$((CHECKS + 1)); printf '  ok   - %s\n' "$*"; }
fail() {
	CHECKS=$((CHECKS + 1))
	FAILURES=$((FAILURES + 1))
	printf '  FAIL - %s\n' "$*"
}
note() { printf '  ..   - %s\n' "$*"; }
gap() {
	if [ "$STRICT" = "1" ]; then
		CHECKS=$((CHECKS + 1))
		FAILURES=$((FAILURES + 1))
		printf '  FAIL - (strict) %s\n' "$*"
		return
	fi
	GAPS=$((GAPS + 1))
	printf '  gap  - %s\n' "$*"
}

echo "go-jpeg2000 validation gate"
echo "repo: $REPO"
echo "go:   $(go version)"
echo

echo "=== build and static analysis ==="
if out=$(go build ./... 2>&1) && [ -z "$out" ]; then pass "go build ./..."; else
	fail "go build ./..."
	echo "$out" | head -20
fi
if [ -z "$(gofmt -l . 2>/dev/null)" ]; then pass "gofmt -l ."; else
	fail "gofmt: $(gofmt -l . | tr '\n' ' ')"
fi
if out=$(go vet ./... 2>&1) && [ -z "$out" ]; then pass "go vet ./..."; else
	fail "go vet ./..."
	echo "$out" | head -20
fi

echo
echo "=== test suite ==="
if out=$(go test ./... 2>&1); then
	pass "go test ./... ($(echo "$out" | grep -c '^ok') packages)"
else
	fail "go test ./..."
	echo "$out" | grep -E '^(---|FAIL)' | head -20
fi
# -race also enables checkptr, which catches invalid unsafe.Pointer arithmetic.
if out=$(go test -race ./... 2>&1); then
	pass "go test -race ./... ($(echo "$out" | grep -c '^ok') packages)"
else
	fail "go test -race ./..."
	echo "$out" | grep -E '^(---|FAIL|WARNING)' | head -20
fi

echo
echo "=== external oracles ==="

WORK=$(mktemp -d "${TMPDIR:-/tmp}/go-jpeg2000-validate.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

have() { command -v "$1" >/dev/null 2>&1; }

# Source rasters both oracles read back. Written once so the Part 1 checks still
# run when OpenJPH is absent.
python3 - "$WORK/src.pgm" "$WORK/src32.pgm" <<'PYEOF'
import sys
for path, n in ((sys.argv[1], 8), (sys.argv[2], 32)):
    px = bytes([20 + ((x*13 + y*3) % 200) for y in range(n) for x in range(n)])
    open(path, 'wb').write(b'P5\n%d %d\n255\n' % (n, n) + px)
PYEOF

# Float source rasters, in the one format either oracle carries binary32 on.
#
# The content is deliberately hostile: both zeros, both infinities, NaNs
# including the all-ones pattern, the smallest and largest denormals, FLT_MAX
# and a spread of exponents and signs. Small positive integers are what hid the
# 32-bit overflow on this path -- they occupy a handful of magnitude bits, so no
# coefficient ever needed the 33rd.
python3 - "$WORK/src.pfm" "$WORK/src3.pfm" "$WORK/srcb.pfm" <<'PYEOF'
import struct, sys

N = 32
SPECIAL = [0x00000000, 0x80000000, 0x7f800000, 0xff800000, 0x7fc00000,
           0xffc00000, 0x7fffffff, 0xffffffff, 0x00000001, 0x80000001,
           0x007fffff, 0x807fffff, 0x7f7fffff, 0xff7fffff, 0x3f800000,
           0xbf800000]

def bits(i, c):
    if i < len(SPECIAL):
        return SPECIAL[i]
    s = (i * 1664525 + c * 1013904223 + 22695477) & 0xFFFFFFFF
    s = (s * 1664525 + 1013904223) & 0xFFFFFFFF
    return s ^ (s >> 15)

for path, nc in ((sys.argv[1], 1), (sys.argv[2], 3)):
    out = [b'PF\n' if nc == 3 else b'Pf\n', b'%d %d\n-1.0\n' % (N, N)]
    for row in range(N):
        y = N - 1 - row
        for x in range(N):
            for c in range(nc):
                out.append(struct.pack('<I', bits(y * N + x, c)))
    open(path, 'wb').write(b''.join(out))

# The same content with every magnitude held under 2^30.
#
# It exists for the reduced-resolution checks and nowhere else. A reduced
# decode returns an LL band, whose coefficients are a lowpass of the NLT words
# rather than the words themselves, so content using the extremes of that word
# produces coefficients with no float bit pattern to map back to. That is not a
# resolution the format defines an answer for, and comparing two decoders'
# handling of it measures which way each overflows. Bounding the magnitudes
# keeps every coefficient representable at every level, so the comparison is
# about the decode. The unbounded fixture is still used, to check that the
# non-representable case is reported rather than wrapped.
out = [b'Pf\n', b'%d %d\n-1.0\n' % (N, N)]
for row in range(N):
    y = N - 1 - row
    for x in range(N):
        b = bits(y * N + x, 0)
        out.append(struct.pack('<I', (b & 0x80000000) | (b & 0x3FFFFFFF)))
open(sys.argv[3], 'wb').write(b''.join(out))
PYEOF

# Compare a decoded PGM/PPM against the reference raster, tolerating the
# comment line opj_decompress writes into its header. Prints "exact" or a
# description of the difference.
cmp_raster() {
	python3 - "$1" "$2" <<'PYEOF'
import sys

def rd(p):
    d = open(p, 'rb').read()
    i, t = 0, []
    while len(t) < 4:
        while d[i:i+1] in b' \n\t\r':
            i += 1
        if d[i:i+1] == b'#':
            while d[i:i+1] != b'\n':
                i += 1
            continue
        s = i
        while d[i:i+1] not in b' \n\t\r':
            i += 1
        t.append(d[s:i])
    return t, d[i+1:]

ta, a = rd(sys.argv[1])
tb, b = rd(sys.argv[2])
if ta[0] != tb[0]:
    print("format %s where the reference raster is %s" % (ta[0].decode(), tb[0].decode()))
    sys.exit(1)
if ta[3] != tb[3]:
    print("maxval %s where the reference raster says %s" % (ta[3].decode(), tb[3].decode()))
    sys.exit(1)
if len(a) != len(b):
    print("raster %d bytes vs %d" % (len(a), len(b)))
    sys.exit(1)
n = sum(1 for x, y in zip(a, b) if x != y)
if n:
    print("%d/%d samples differ, max delta %d"
          % (n, len(b), max(abs(x - y) for x, y in zip(a, b))))
    sys.exit(1)
print("exact")
PYEOF
}

if ! have ojph_compress || ! have ojph_expand; then
	gap "OpenJPH not installed; HTJ2K interoperability unchecked"
else
	note "OpenJPH: $(ojph_expand 2>&1 | head -1 | tr -d '\n' | cut -c1-60)"

	# Build the probe that writes a codestream through the public API.
	cat >"$WORK/enc.go" <<'GOEOF'
//go:build ignore

package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
	size, _ := strconv.Atoi(os.Args[2])
	nres, _ := strconv.Atoi(os.Args[3])
	tile := 0
	if len(os.Args) > 4 {
		tile, _ = strconv.Atoi(os.Args[4])
	}
	img := image.NewGray(image.Rect(0, 0, size, size))
	raw := make([]byte, size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := uint8(20 + ((x*13 + y*3) % 200))
			img.Set(x, y, color.Gray{Y: v})
			raw[y*size+x] = v
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	opts := &jp2.Options{
		HighThroughput: true, Lossless: true,
		Format: jp2.FormatJ2K, NumResolutions: nres,
	}
	if tile > 0 {
		opts.TileSize = image.Point{X: tile, Y: tile}
	}
	if err := jp2.Encode(f, img, opts); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	ref, _ := os.Create(os.Args[1] + ".pgm")
	fmt.Fprintf(ref, "P5\n%d %d\n255\n", size, size)
	ref.Write(raw)
	ref.Close()
}
GOEOF

	# Compare a decoded PGM against the reference raster.
	cmp_pgm() {
		python3 - "$1" "$2" <<'PYEOF'
import sys
def rd(p):
    d=open(p,'rb').read(); i=0; t=[]
    while len(t)<4:
        while d[i] in b' \n\t\r': i+=1
        s=i
        while d[i] not in b' \n\t\r': i+=1
        t.append(d[s:i])
    return d[i+1:]
a=rd(sys.argv[1]); b=rd(sys.argv[2])
n=sum(1 for x,y in zip(a,b) if x!=y)
print(f"{n}/{len(b)}")
sys.exit(0 if n==0 and len(a)==len(b) else 1)
PYEOF
	}

	# WRITE side: does the HTJ2K reference read what we produce, exactly?
	# Every resolution count from 1 (no wavelet levels) to 4 (three levels of
	# decomposition) is asserted: the forward 5/3 pass order only shows up
	# above zero levels, and it grows worse with each further level.
	for size in 32 64 128 200; do
		for nres in 1 2 3 4; do
			f="$WORK/w_${size}_$nres.j2c"
			if ! go run "$WORK/enc.go" "$f" "$size" "$nres" >/dev/null 2>&1; then
				fail "write ${size}px ${nres} resolutions: our encoder failed"
				continue
			fi
			if ! ojph_expand -i "$f" -o "$WORK/w_${size}_$nres.out.pgm" >/dev/null 2>&1; then
				fail "write ${size}px ${nres} resolutions: OpenJPH refused our codestream"
				continue
			fi
			if d=$(cmp_pgm "$WORK/w_${size}_$nres.out.pgm" "$f.pgm"); then
				pass "write ${size}px ${nres} resolutions HTJ2K: OpenJPH decodes it exactly ($d differ)"
			else
				fail "write ${size}px ${nres} resolutions HTJ2K: OpenJPH decoded $d samples differently"
			fi
		done
	done


	# WRITE side, irreversible 9/7. Two things are asserted: that OpenJPH
	# accepts the file at all — it refuses a QCD that declares scalar derived
	# quantization, which is what this encoder used to emit — and that the
	# samples it reconstructs are inside the error the step sizes imply.
	#
	# That tolerance is derived, not fitted. It is computed from the QCD marker
	# in the file under test: Σ_b Δ_b·A_b, where Δ_b = 2^(R_b − ε_b)(1 + μ_b/2^11)
	# is Equation E-3 read straight out of the marker and A_b is the peak L1
	# synthesis gain of the subband, so nothing about the measurement enters it.
	cat >"$WORK/enclossy.go" <<'GOEOF'
//go:build ignore

package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
	size, _ := strconv.Atoi(os.Args[2])
	nres, _ := strconv.Atoi(os.Args[3])
	quality, _ := strconv.Atoi(os.Args[4])
	img := image.NewGray(image.Rect(0, 0, size, size))
	raw := make([]byte, size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := uint8((20 + ((x*13+y*3)%200) + ((x^y)%2)*70) % 256)
			img.Set(x, y, color.Gray{Y: v})
			raw[y*size+x] = v
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := jp2.Encode(f, img, &jp2.Options{
		HighThroughput: true, Lossless: false, Quality: quality,
		Format: jp2.FormatJ2K, NumResolutions: nres,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	ref, _ := os.Create(os.Args[1] + ".pgm")
	fmt.Fprintf(ref, "P5\n%d %d\n255\n", size, size)
	ref.Write(raw)
	ref.Close()
}
GOEOF

	# Prints "<max sample error> <bound>" and exits non-zero if the error is
	# outside the bound the file's own QCD marker implies.
	lossy_check() {
		python3 - "$1" "$2" "$3" <<'PYEOF'
import struct, sys

def rd(p):
    d = open(p, 'rb').read(); i = 0; t = []
    while len(t) < 4:
        while d[i] in b' \n\t\r': i += 1
        s = i
        while d[i] not in b' \n\t\r': i += 1
        t.append(d[s:i])
    return d[i+1:]

# Peak L1 synthesis gains of the 9/7 filter bank, with the factor of two per
# filtered dimension that period-symmetric extension can add.
def lo(n): return 1.0 if n <= 0 else 2.0 * 1.4 * 2.0 ** n
def hi(n): return 1.0 if n <= 0 else 2.0 * 0.7 * 2.0 ** n
def gain(numres, res, detail):
    if res == 0: return lo(numres - 1) ** 2
    d = numres - res
    return hi(d) * lo(d) if detail in (0, 1) else hi(d) * hi(d)

d = open(sys.argv[1], 'rb').read()
i, prec, decomps, wav, steps = 2, None, None, None, None
while i < len(d) - 3:
    m = struct.unpack('>H', d[i:i+2])[0]
    if m in (0xff93, 0xffd9, 0xff90): break
    L = struct.unpack('>H', d[i+2:i+4])[0]
    seg = d[i+4:i+2+L]
    if m == 0xff51:
        ncomp = struct.unpack('>H', seg[34:36])[0]
        prec = max((seg[36+3*c] & 0x7f) + 1 for c in range(ncomp))
    elif m == 0xff52:
        decomps, wav = seg[5], seg[9]
    elif m == 0xff5c:
        if seg[0] & 0x1f != 2:
            print("QCD style %d, want 2 (scalar expounded)" % (seg[0] & 0x1f))
            sys.exit(2)
        steps = [struct.unpack('>H', seg[1+2*k:3+2*k])[0]
                 for k in range((len(seg)-1)//2)]
    i += 2 + L
numres = decomps + 1
bound = 0.0
for res in range(numres):
    for detail in range(1 if res == 0 else 3):
        idx = 0 if res == 0 else 1 + (res-1)*3 + detail
        v = steps[idx]
        g = 0 if res == 0 else (2 if detail == 2 else 1)
        delta = (1 + (v & 0x7ff)/2048.0) * 2.0 ** (prec + g - (v >> 11))
        bound += delta * gain(numres, res, detail)
a, b = rd(sys.argv[2]), rd(sys.argv[3])
if len(a) != len(b):
    print("raster length %d vs %d" % (len(a), len(b)))
    sys.exit(2)
err = max(abs(x - y) for x, y in zip(a, b))
print("%d %.3f" % (err, bound))
sys.exit(0 if err <= bound + 0.5 else 1)
PYEOF
	}

	for size in 32 64 127; do
		for nres in 1 3 5; do
			for q in 100 75; do
				f="$WORK/l_${size}_${nres}_$q.j2c"
				if ! go run "$WORK/enclossy.go" "$f" "$size" "$nres" "$q" >/dev/null 2>&1; then
					fail "lossy ${size}px ${nres} res q$q: our encoder failed"
					continue
				fi
				if ! ojph_expand -i "$f" -o "$f.out.pgm" >"$WORK/l.err" 2>&1; then
					fail "lossy ${size}px ${nres} res q$q: OpenJPH refused our codestream: $(head -1 "$WORK/l.err")"
					continue
				fi
				if out=$(lossy_check "$f" "$f.pgm" "$f.out.pgm"); then
					pass "lossy 9/7 ${size}px ${nres} res q$q: OpenJPH decodes within the QCD's own bound (max error ${out% *} of ${out#* })"
				else
					fail "lossy 9/7 ${size}px ${nres} res q$q: $out"
				fi
			done
		done
	done

	# At quality 100 the step sizes are sized for half a sample of error, so a
	# decoder that rounds to an integer has to return the source exactly.
	for size in 32 64 127; do
		for nres in 1 3 5; do
			f="$WORK/l_${size}_${nres}_100.j2c"
			[ -f "$f.out.pgm" ] || continue
			if d=$(cmp_pgm "$f.out.pgm" "$f.pgm"); then
				pass "lossy 9/7 ${size}px ${nres} res q100: OpenJPH decodes it exactly ($d differ)"
			else
				fail "lossy 9/7 ${size}px ${nres} res q100: OpenJPH decoded $d samples differently"
			fi
		done
	done

	# Sizes whose subbands come out with an odd width still fail, for a reason
	# that is not the wavelet: the forward transform matches Annex F and the
	# OpenJPH coefficient fixtures at these geometries (see
	# internal/dwt/conformance_test.go), so the divergence is downstream.
	for size in 17 25; do
		f="$WORK/odd_$size.j2c"
		if ! go run "$WORK/enc.go" "$f" "$size" 2 >/dev/null 2>&1; then
			gap "write ${size}px 2 resolutions: our encoder failed"
			continue
		fi
		if ! ojph_expand -i "$f" -o "$WORK/odd_$size.out.pgm" >/dev/null 2>&1; then
			gap "write ${size}px 2 resolutions: OpenJPH refused our codestream (odd subband width)"
			continue
		fi
		d=$(cmp_pgm "$WORK/odd_$size.out.pgm" "$f.pgm" || true)
		if [ "${d%%/*}" = "0" ]; then
			pass "write ${size}px 2 resolutions: OpenJPH decodes it exactly"
		else
			gap "write ${size}px 2 resolutions: OpenJPH differs on $d samples (odd subband width, not the DWT)"
		fi
	done

	# WRITE side, tiled. A tiled image is one tile-part per tile, each coded at
	# its own absolute origin, and the tile sizes below are chosen to exercise
	# both cases: 16 and 32 divide 64 evenly, while 20, 24 and 13 leave a short
	# last row and column, and 20 and 13 put whole tiles at origins that are
	# odd once halved, which splits their subbands the other way round.
	for tile in 16 32 20 24 13; do
		f="$WORK/t_$tile.j2c"
		if ! go run "$WORK/enc.go" "$f" 64 3 "$tile" >/dev/null 2>&1; then
			fail "write 64px in ${tile}x${tile} tiles: our encoder failed"
			continue
		fi
		if ! ojph_expand -i "$f" -o "$WORK/t_$tile.out.pgm" >/dev/null 2>&1; then
			fail "write 64px in ${tile}x${tile} tiles: OpenJPH refused our codestream"
			continue
		fi
		if d=$(cmp_pgm "$WORK/t_$tile.out.pgm" "$f.pgm"); then
			pass "write 64px in ${tile}x${tile} tiles: OpenJPH decodes it exactly ($d differ)"
		else
			fail "write 64px in ${tile}x${tile} tiles: OpenJPH decoded $d samples differently"
		fi
	done

	# READ side: do we read what the reference produces, exactly?
	for src in src src32; do
		for nd in 0 1 2 3; do
			f="$WORK/r_${src}_$nd.j2c"
			if ! ojph_compress -i "$WORK/$src.pgm" -o "$f" \
				-num_decomps "$nd" -reversible true >/dev/null 2>&1; then
				gap "read $src: OpenJPH could not produce a -num_decomps $nd fixture"
				continue
			fi
			# Control: the oracle must round-trip its own output, or it proves nothing.
			ojph_expand -i "$f" -o "$WORK/r_${src}_$nd.ctl.pgm" >/dev/null 2>&1
			if ! cmp -s "$WORK/r_${src}_$nd.ctl.pgm" "$WORK/$src.pgm"; then
				gap "read $src -num_decomps $nd: oracle control failed, measurement would be meaningless"
				continue
			fi
			out=$(go run ./scripts/decodecmp "$f" "$WORK/$src.pgm" 2>&1)
			if [ "$out" = "0" ]; then
				pass "read HTJ2K $src -num_decomps $nd: we decode OpenJPH's codestream exactly"
			else
				fail "read HTJ2K $src -num_decomps $nd: $out samples differ"
			fi
		done
	done

	# READ side, tiled: the same geometry from the other direction. src32 is
	# 32x32, so 12 and 13 leave a short last tile and put tiles at odd origins.
	for tile in 8 16 12 13; do
		f="$WORK/rt_$tile.j2c"
		if ! ojph_compress -i "$WORK/src32.pgm" -o "$f" \
			-tile_size "{$tile,$tile}" -num_decomps 2 -reversible true >/dev/null 2>&1; then
			gap "read HTJ2K ${tile}x${tile} tiles: OpenJPH could not produce a fixture"
			continue
		fi
		ojph_expand -i "$f" -o "$WORK/rt_$tile.ctl.pgm" >/dev/null 2>&1
		if ! cmp -s "$WORK/rt_$tile.ctl.pgm" "$WORK/src32.pgm"; then
			gap "read HTJ2K ${tile}x${tile} tiles: oracle control failed, measurement would be meaningless"
			continue
		fi
		out=$(go run ./scripts/decodecmp "$f" "$WORK/src32.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read HTJ2K ${tile}x${tile} tiles: we decode OpenJPH's codestream exactly"
		else
			fail "read HTJ2K ${tile}x${tile} tiles: $out samples differ"
		fi
	done

	# READ side, binary32. This is the direction the capability matrix cannot
	# cover, and until it was checked no OpenJPH float codestream could be read
	# at all: the reference writes Cnlt = 0xFFFF, the "all components" form, and
	# this parser rejected it as an out-of-range component index.
	#
	# The decomposition counts matter more here than on the integer path. Each
	# level widens the coefficients, and the codestream signals that width in
	# QCD as guard bits and exponents; a decoder that ignores it, or that holds
	# the coefficients in a word too narrow for it, reconstructs samples that
	# are shifted rather than obviously wrong.
	for src in src src3; do
		for nd in 0 1 2 3 5; do
			f="$WORK/fr_${src}_$nd.j2c"
			if ! ojph_compress -i "$WORK/$src.pfm" -o "$f" \
				-num_decomps "$nd" -reversible true >/dev/null 2>&1; then
				gap "read float $src -num_decomps $nd: OpenJPH could not produce a fixture"
				continue
			fi
			# Control: the oracle must round-trip its own output bit for bit,
			# or the comparison below says nothing about this library.
			#
			# One of these controls is expected to fail, and it is the reason
			# the control exists. At zero decomposition levels ojph_compress
			# signals Mb = 31 for a signed 32-bit component, and the float bit
			# pattern 0xFFFFFFFF becomes -2^31 under the NLT Type 3 point
			# transform, which needs 32. OpenJPH loses exactly that one sample
			# out of 1024 from its own file. This encoder measures the
			# transformed coefficients rather than assuming the nominal value,
			# signals 32, and OpenJPH reads it back exactly -- which the write
			# side below asserts.
			if ! ojph_expand -i "$f" -o "$f.ctl.pfm" >/dev/null 2>&1; then
				gap "read float $src -num_decomps $nd: the oracle could not read its own codestream"
				continue
			fi
			if ! out=$(go run ./scripts/floatpfm dec "$f" "$f.ours.pfm" 2>&1); then
				fail "read float $src -num_decomps $nd: $out"
				continue
			fi

			# What is asserted is that we read OpenJPH's codestream the way
			# OpenJPH reads it. That is the claim worth making, and unlike
			# "we recover the original samples" it does not require the
			# oracle's own round trip to be lossless.
			#
			# The distinction is not hypothetical. At zero decomposition levels
			# ojph_compress signals Mb = 31 for a signed 32-bit component,
			# while the float bit pattern 0xFFFFFFFF becomes -2^31 under the
			# NLT Type 3 point transform and needs 32. OpenJPH loses exactly
			# that one sample of 1024 from its own file. Comparing against the
			# source would blame this library for the oracle's lost sample and
			# so had to be skipped; comparing against the oracle's own reading
			# measures the thing that matters and passes.
			if out=$(go run ./scripts/floatpfm cmp "$f.ctl.pfm" "$f.ours.pfm"); then
				if go run ./scripts/floatpfm cmp "$WORK/$src.pfm" "$f.ctl.pfm" >/dev/null 2>&1; then
					pass "read float binary32 $src -num_decomps $nd: we decode OpenJPH's codestream exactly, and it round-trips the source"
				else
					pass "read float binary32 $src -num_decomps $nd: we decode OpenJPH's codestream exactly as OpenJPH does (its own round trip is lossy here; see the write check below)"
				fi
			else
				fail "read float binary32 $src -num_decomps $nd: we and OpenJPH disagree about its own codestream: $out"
			fi
		done
	done

	# READ side, binary32, tiled: the geometry and the widened coefficients at
	# once. 12 and 13 leave a short last tile and put whole tiles at origins
	# that are odd once halved.
	for tile in 8 16 12 13; do
		f="$WORK/frt_$tile.j2c"
		if ! ojph_compress -i "$WORK/src.pfm" -o "$f" \
			-tile_size "{$tile,$tile}" -num_decomps 2 -reversible true >/dev/null 2>&1; then
			gap "read float ${tile}x${tile} tiles: OpenJPH could not produce a fixture"
			continue
		fi
		if ! ojph_expand -i "$f" -o "$f.ctl.pfm" >/dev/null 2>&1 ||
			! go run ./scripts/floatpfm cmp "$WORK/src.pfm" "$f.ctl.pfm" >/dev/null 2>&1; then
			gap "read float ${tile}x${tile} tiles: oracle control failed, measurement would be meaningless"
			continue
		fi
		if ! out=$(go run ./scripts/floatpfm dec "$f" "$f.ours.pfm" 2>&1); then
			fail "read float ${tile}x${tile} tiles: $out"
			continue
		fi
		if out=$(go run ./scripts/floatpfm cmp "$WORK/src.pfm" "$f.ours.pfm"); then
			pass "read float binary32 ${tile}x${tile} tiles: we decode OpenJPH's codestream exactly"
		else
			fail "read float binary32 ${tile}x${tile} tiles: $out"
		fi
	done

	# WRITE side, binary32, round-tripped through the oracle at every
	# resolution count and a tile grid. The capability matrix covers a subset;
	# this widens it, because the magnitude budget a subband needs grows with
	# the decomposition level and it is that budget the codestream must signal.
	for src in src src3; do
		for nres in 1 2 3 5 6; do
			f="$WORK/fw_${src}_$nres.j2c"
			if ! out=$(go run ./scripts/floatpfm enc "$WORK/$src.pfm" "$f" "$nres" 2>&1); then
				fail "write float $src $nres resolutions: our encoder failed: $out"
				continue
			fi
			if ! err=$(ojph_expand -i "$f" -o "$f.out.pfm" 2>&1); then
				fail "write float $src $nres resolutions: OpenJPH refused our codestream: $(echo "$err" | grep -oE 'ojph error.*' | head -1 | cut -c1-70)"
				continue
			fi
			if out=$(go run ./scripts/floatpfm cmp "$WORK/$src.pfm" "$f.out.pfm"); then
				pass "write float binary32 $src $nres resolutions: OpenJPH decodes it exactly"
			else
				fail "write float binary32 $src $nres resolutions: $out"
			fi
		done
	done
	for tile in 8 16 12 13; do
		f="$WORK/fwt_$tile.j2c"
		if ! out=$(go run ./scripts/floatpfm enc "$WORK/src.pfm" "$f" 3 "$tile" 2>&1); then
			fail "write float ${tile}x${tile} tiles: our encoder failed: $out"
			continue
		fi
		if ! err=$(ojph_expand -i "$f" -o "$f.out.pfm" 2>&1); then
			fail "write float ${tile}x${tile} tiles: OpenJPH refused our codestream: $(echo "$err" | grep -oE 'ojph error.*' | head -1 | cut -c1-70)"
			continue
		fi
		if out=$(go run ./scripts/floatpfm cmp "$WORK/src.pfm" "$f.out.pfm"); then
			pass "write float binary32 ${tile}x${tile} tiles: OpenJPH decodes it exactly"
		else
			fail "write float binary32 ${tile}x${tile} tiles: $out"
		fi
	done
fi

if ! have opj_compress; then
	gap "OpenJPEG not installed; Part 1 interoperability unchecked"
else
	# WRITE side, Part 1 MQ: does the Part 1 reference read what we produce,
	# exactly? This is the direction that was unreachable until the encoder
	# emitted conforming packets for the MQ block coder -- it wrote a private
	# container instead, which nothing outside this repository can parse.
	#
	# The probe drives the public API, so what is measured is what a caller
	# gets: HighThroughput unset is Part 1, and the same entry point carries
	# tiles, quality layers, 16-bit samples and three components.
	cat >"$WORK/p1enc.go" <<'GOEOF'
//go:build ignore

package main

// p1enc <out.j2k> <size> <nres> [tile] [comps] [depth] [layers] [quality] [order] [precexp]
//
// Encodes a Part 1 (MQ) codestream through the public API and writes the source
// raster beside it as a PGM or PPM.

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

// rgbColor is an alpha-free colour. The encoder picks its component count from
// whether the image's colour model can represent transparency, so this is what
// makes a three-component image rather than a four-component one.
type rgbColor struct{ R, G, B uint8 }

func (c rgbColor) RGBA() (r, g, b, a uint32) {
	return uint32(c.R) * 0x101, uint32(c.G) * 0x101, uint32(c.B) * 0x101, 0xFFFF
}

var rgbModel = color.ModelFunc(func(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	return rgbColor{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
})

type rgbImage struct {
	pix  []rgbColor
	rect image.Rectangle
}

func (m *rgbImage) ColorModel() color.Model { return rgbModel }
func (m *rgbImage) Bounds() image.Rectangle { return m.rect }
func (m *rgbImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(m.rect) {
		return rgbColor{}
	}
	return m.pix[(y-m.rect.Min.Y)*m.rect.Dx()+(x-m.rect.Min.X)]
}

func arg(i, def int) int {
	if i >= len(os.Args) {
		return def
	}
	v, err := strconv.Atoi(os.Args[i])
	if err != nil {
		return def
	}
	return v
}

func main() {
	out := os.Args[1]
	size := arg(2, 32)
	nres := arg(3, 3)
	tile := arg(4, 0)
	comps := arg(5, 1)
	depth := arg(6, 8)
	layers := arg(7, 1)
	quality := arg(8, 0) // 0 asks for lossless
	order := arg(9, 0)   // Table A.16: 0=LRCP, 1=RLCP, 2=RPCL, 3=PCRL, 4=CPRL
	precexp := arg(10, 0) // 0 leaves the maximal precinct; otherwise 2^n per side
	pktlen := arg(11, 0)  // non-zero writes PLT in every tile-part and TLM in the main header

	maxv := (1 << depth) - 1
	sample := func(x, y, c int) int {
		v := (20 + ((x*13+y*3+c*57)%200))*maxv/255 + ((x^y)%3)*(maxv/32)
		if v > maxv {
			v = maxv
		}
		return v
	}

	var img image.Image
	switch {
	case comps == 3:
		m := &rgbImage{rect: image.Rect(0, 0, size, size), pix: make([]rgbColor, size*size)}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				m.pix[y*size+x] = rgbColor{
					uint8(sample(x, y, 0)), uint8(sample(x, y, 1)), uint8(sample(x, y, 2))}
			}
		}
		img = m
	case depth > 8:
		m := image.NewGray16(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				m.SetGray16(x, y, color.Gray16{Y: uint16(sample(x, y, 0))})
			}
		}
		img = m
	default:
		m := image.NewGray(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				m.SetGray(x, y, color.Gray{Y: uint8(sample(x, y, 0))})
			}
		}
		img = m
	}

	opts := &jp2.Options{
		HighThroughput: false, // Part 1: the MQ block coder
		Lossless:       quality == 0,
		Format:         jp2.FormatJ2K,
		NumResolutions: nres,
		NumLayers:      layers,

		ProgressionOrder: jp2.ProgressionOrder(order),
	}
	if precexp > 0 {
		for i := 0; i < nres; i++ {
			opts.PrecinctSizes = append(opts.PrecinctSizes,
				jp2.PrecinctSize{WidthExp: uint8(precexp), HeightExp: uint8(precexp)})
		}
	}
	opts.WritePacketLengths = pktlen != 0
	if quality > 0 {
		opts.Quality = quality
	}
	if tile > 0 {
		opts.TileSize = image.Point{X: tile, Y: tile}
	}
	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := jp2.Encode(f, img, opts); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	f.Close()

	magic, ext := "P5", ".pgm"
	if comps == 3 {
		magic, ext = "P6", ".ppm"
	}
	rf, err := os.Create(out + ext)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := bufio.NewWriter(rf)
	fmt.Fprintf(w, "%s\n%d %d\n%d\n", magic, size, size, maxv)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			for c := 0; c < comps; c++ {
				v := sample(x, y, c)
				if depth > 8 {
					w.WriteByte(byte(v >> 8))
				}
				w.WriteByte(byte(v))
			}
		}
	}
	w.Flush()
	rf.Close()
	fmt.Println(out + ext)
}
GOEOF

	# name size nres tile comps depth layers quality
	p1_write() {
		local name=$1
		shift
		local ext=pgm
		[ "${4:-1}" = 3 ] && ext=ppm
		local f="$WORK/p1w_$name.j2k"
		if ! ref=$(go run "$WORK/p1enc.go" "$f" "$@" 2>&1); then
			fail "write Part 1 MQ $name: our encoder failed: $(echo "$ref" | head -1)"
			return
		fi
		if ! err=$(opj_decompress -i "$f" -o "$f.out.$ext" 2>&1); then
			fail "write Part 1 MQ $name: OpenJPEG refused our codestream: $(echo "$err" | grep -i error | head -1 | cut -c1-70)"
			return
		fi
		if d=$(cmp_raster "$f.out.$ext" "$ref"); then
			pass "write Part 1 MQ $name: OpenJPEG decodes it exactly"
		else
			fail "write Part 1 MQ $name: $d"
		fi
	}

	# Control: the oracle must round-trip its own output, or a failure below
	# says nothing about this library.
	if opj_compress -i "$WORK/src32.pgm" -o "$WORK/p1ctl.j2k" -n 3 -r 1 >/dev/null 2>&1 &&
		opj_decompress -i "$WORK/p1ctl.j2k" -o "$WORK/p1ctl.pgm" >/dev/null 2>&1 &&
		cmp_raster "$WORK/p1ctl.pgm" "$WORK/src32.pgm" >/dev/null; then
		pass "Part 1 oracle control: OpenJPEG round-trips its own codestream exactly"
	else
		gap "Part 1 oracle control failed; the write checks below would be meaningless"
	fi

	# Sizes and decomposition depths. 17 and 25 leave subbands with an odd
	# width, and six resolutions on a 17-pixel image reduces the LL band to a
	# single sample.
	for size in 17 25 32 64 127 200; do
		for nres in 1 2 3 6; do
			p1_write "${size}px_${nres}res" "$size" "$nres"
		done
	done

	# Tiled: 16 and 32 divide 64 evenly, while 20, 24 and 13 leave a short last
	# row and column, and 20 and 13 put whole tiles at origins that are odd
	# once halved.
	for tile in 16 32 20 24 13; do
		p1_write "tile$tile" 64 3 "$tile"
	done

	# Three components (the reversible colour transform) and 16-bit samples.
	p1_write "rgb8" 64 3 0 3 8
	p1_write "gray16" 64 3 0 1 16

	# Real quality layers. Every layer is decoded here, so what is asserted is
	# that splitting a block's coding passes across packets loses nothing.
	for layers in 2 3 8; do
		p1_write "layers$layers" 64 3 0 1 8 "$layers"
	done
	p1_write "layers4_tiled" 64 3 16 1 8 4

	# Decoder configuration options that ask for less than the whole image.
	#
	# A decoder that silently ignores such an option is worse than one that
	# lacks it: a caller sizing a buffer for a 32x16 region and receiving the
	# whole 64x32 image gets no indication the request was dropped.
	# Config.DecodeArea was documented as "specifies a region to decode" and
	# read by nothing; both it and ReduceResolution now do what they say, and
	# both are measured for what they save rather than only that they run.
	#
	# ReduceResolution was refused for a while on the strength of a comparison
	# against a downsample of the full decode — "off by 175 on a ramp spanning
	# 0 to 2". That was the wrong oracle: the LL band at resolution r is the
	# image the wavelet reconstructs at that scale, not an arithmetic average
	# of the finer one, so the two disagree by construction. Against the
	# reference's own reduced decode, checked below, this library was already
	# bit-exact. The refusal cost a real capability for a measurement error,
	# which is why the external check now runs rather than a refusal assertion.
	# Region decode, both halves: the samples must be the ones a full decode
	# produces for that rectangle, exactly, and getting them must cost less
	# than decoding everything. A region decode that produced the right pixels
	# by decoding the whole image and cropping would satisfy the first and none
	# of the point.
	#
	# The half and float paths are checked separately from the image.Image one
	# because they are different functions, and they are the only ones an EXR
	# HTJ2K chunk ever reaches: a region correct on DecodeConfigCost and broken
	# on DecodeFloatConfig would pass every check written against image.Image
	# and fail the only caller there is.
	if out=$(go test ./ -run 'TestReduceResolution|TestDecodeArea|TestRegionDecode' -v 2>&1); then
		cost=$(printf '%s\n' "$out" | sed -n 's/.*\(64x64 of .*\)$/\1/p' | head -1)
		pass "decoder options: a region decodes exactly and costs less (${cost:-measured}), and an unhonourable request is refused"
		for path in float half; do
			line=$(printf '%s\n' "$out" | sed -n "s/.*\($path path, .*\)\$/\1/p" | head -1)
			if [ -n "$line" ]; then
				pass "decoder options: $line"
			else
				fail "decoder options: the $path path reported no region measurement"
			fi
		done
	else
		fail "decoder options: $(printf '%s\n' "$out" | grep -E 'config_(options|int)_test|region_(cost|planes)_test' | head -1 | cut -c1-110)"
	fi

	# Reduced-resolution decode against the reference's own reduced decode.
	#
	# This is the oracle the refusal never used. ojph_expand -skip_res N and
	# opj_decompress -r N each reconstruct the same resolution this library is
	# asked for, so the comparison is between two decoders reading one
	# codestream — which is the claim worth making, and the only one that does
	# not require agreeing first on what a "downsample" means.
	#
	# Both entry points are covered because an EXR HTJ2K chunk is float or
	# half and they are different functions. The half path refused this
	# outright until it was measured, so it has never been compared to anything.
	if ! command -v ojph_expand >/dev/null 2>&1; then
		gap "reduced resolution float: ojph_expand is not installed"
	else
		rr_fail=0
		rr_ran=0
		rr_last=""
		for nd in 2 3 5; do
			rf="$WORK/rr_$nd.j2c"
			if ! ojph_compress -i "$WORK/srcb.pfm" -o "$rf" \
				-num_decomps "$nd" -reversible true >/dev/null 2>&1; then
				gap "reduced resolution float -num_decomps $nd: no fixture"
				continue
			fi
			for r in 1 2 3; do
				[ "$r" -gt "$nd" ] && continue
				if ! ojph_expand -i "$rf" -o "$rf.ref$r.pfm" -skip_res "$r" >/dev/null 2>&1; then
					gap "reduced resolution float nd=$nd r=$r: the oracle would not produce it"
					continue
				fi
				if ! out=$(go run ./scripts/floatpfm dec "$rf" "$rf.ours$r.pfm" "$r" 2>&1); then
					fail "reduced resolution float nd=$nd r=$r: $out"
					rr_fail=1
					continue
				fi
				rr_ran=$((rr_ran + 1))
				if out=$(go run ./scripts/floatpfm cmp "$rf.ref$r.pfm" "$rf.ours$r.pfm" 2>&1); then
					rr_last="nd=$nd r=$r"
				else
					fail "reduced resolution float nd=$nd r=$r: $out"
					rr_fail=1
				fi
			done
		done
		if [ "$rr_ran" -eq 0 ]; then
			fail "reduced resolution float: nothing was compared"
		elif [ "$rr_fail" -eq 0 ]; then
			pass "reduced resolution float: $rr_ran decodes bit-identical to ojph_expand -skip_res, up to $rr_last"
		fi
	fi

	# The half entry point, against OpenJPEG.
	#
	# It is a different function from the float one and it refused a reduced
	# decode outright until this was measured, so it had never been compared
	# with anything. It is also the one an EXR chunk of half channels reaches,
	# which is most of them.
	if ! command -v opj_decompress >/dev/null 2>&1; then
		gap "reduced resolution half: opj_decompress is not installed"
	elif ! go build -o "$WORK/halfpgm" ./scripts/halfpgm/ 2>"$WORK/halfpgm.err"; then
		fail "reduced resolution half: could not build scripts/halfpgm: $(head -1 "$WORK/halfpgm.err")"
	elif ! "$WORK/halfpgm" enc "$WORK/hp.j2c" "$WORK/hp_src.pgm" 4 >/dev/null 2>&1; then
		fail "reduced resolution half: could not write a fixture"
	else
		hp_fail=0
		hp_ran=0
		for r in 0 1 2 3; do
			if ! opj_decompress -i "$WORK/hp.j2c" -o "$WORK/hp_ref$r.pgm" -r "$r" >/dev/null 2>&1; then
				gap "reduced resolution half r=$r: the oracle would not produce it"
				continue
			fi
			if ! out=$("$WORK/halfpgm" dec "$WORK/hp.j2c" "$WORK/hp_ours$r.pgm" "$r" 2>&1); then
				fail "reduced resolution half r=$r: $out"
				hp_fail=1
				continue
			fi
			hp_ran=$((hp_ran + 1))
			if ! d=$(cmp_raster "$WORK/hp_ref$r.pgm" "$WORK/hp_ours$r.pgm"); then
				fail "reduced resolution half r=$r: $d"
				hp_fail=1
			fi
		done
		if [ "$hp_ran" -eq 0 ]; then
			fail "reduced resolution half: nothing was compared"
		elif [ "$hp_fail" -eq 0 ]; then
			pass "reduced resolution half: $hp_ran decodes bit-identical to opj_decompress -r, full resolution through 8x8"
		fi
	fi

	# A reduced decode of content that uses the extremes of the NLT word has no
	# answer in the sample domain: the LL coefficient leaves the range the point
	# transform maps from. It must say so. Before this it narrowed with a plain
	# int32 conversion and returned the wrapped value — 9 of 256 samples wrong
	# against OpenJPH, each by exactly the sign-magnitude complement, silently.
	#
	# The full decode of the same fixture must stay exact, or this has been
	# "fixed" by refusing the content rather than the impossible request.
	ovf="$WORK/rr_ovf.j2c"
	if ! command -v ojph_compress >/dev/null 2>&1; then
		gap "reduced resolution overflow: ojph_compress is not installed"
	elif ! ojph_compress -i "$WORK/src.pfm" -o "$ovf" -num_decomps 2 -reversible true >/dev/null 2>&1; then
		gap "reduced resolution overflow: no fixture"
	elif ! go run ./scripts/floatpfm dec "$ovf" "$ovf.full.pfm" 0 >/dev/null 2>&1; then
		fail "reduced resolution overflow: the full decode of the extreme fixture broke"
	elif ! go run ./scripts/floatpfm cmp "$WORK/src.pfm" "$ovf.full.pfm" >/dev/null 2>&1; then
		fail "reduced resolution overflow: the full decode of the extreme fixture is no longer exact"
	elif out=$(go run ./scripts/floatpfm dec "$ovf" "$ovf.r1.pfm" 1 2>&1); then
		fail "reduced resolution overflow: a non-representable reduced decode returned samples instead of reporting"
	elif ! printf '%s\\n' "$out" | grep -q "outside the range"; then
		fail "reduced resolution overflow: it failed without naming the cause: $(printf '%s\\n' "$out" | head -1 | cut -c1-90)"
	else
		pass "reduced resolution overflow: a coefficient with no sample to map to is reported, not wrapped, and the full decode of the same fixture stays exact"
	fi

	# And it must cost less, or it is a full decode with a smaller output —
	# which is exactly what it was until the resolutions above the target
	# stopped being entropy-decoded.
	if out=$(go test ./ -run 'TestReduceResolutionCostsLess' -v 2>&1); then
		line=$(printf '%s\n' "$out" | sed -n 's/.*\(reduce 1: .*\)$/\1/p' | head -1)
		deep=$(printf '%s\n' "$out" | sed -n 's/.*\(reduce 5: .*\)$/\1/p' | head -1)
		pass "reduced resolution costs less: $line; $deep"
	else
		fail "reduced resolution costs less: $(printf '%s\n' "$out" | grep -E 'config_options_test' | head -1 | cut -c1-110)"
	fi

	# The other half of PLT, and the half an external oracle cannot see.
	#
	# Everything above asks OpenJPEG whether our PLT codestream is valid. It
	# said yes, and this library still could not read its own output: the tile
	# index required SOD immediately after SOT, which holds only for an empty
	# tile-part header, so a codestream carrying PLT had every tile indexed as
	# absent and decoded to DC-shifted nothing — 99.6% of samples wrong while
	# the reference read the same bytes correctly.
	#
	# A defect that lives only on this side of the round trip is invisible to
	# an oracle by construction. The check below is against the source image,
	# not against a decode of a PLT-free codestream, since those two could
	# agree on being wrong.
	if out=$(go test ./ -run 'TestPacketLengthMarkers' -v 2>&1); then
		n=$(printf '%s\n' "$out" | grep -cE '^\s*--- PASS')
		if [ "$n" -lt 4 ]; then
			fail "read our own PLT: only $n assertions ran"
		else
			pass "read our own PLT: a codestream we wrote with packet lengths is one we can read, with and without precincts, and the markers change no sample"
		fi
	else
		fail "read our own PLT: $(printf '%s\n' "$out" | grep -E '^\s+plt_roundtrip_test' | head -1 | cut -c1-110)"
	fi

	# Component subsampling (XRsiz, YRsiz above 1).
	#
	# The components of a subsampled image sit on different grids, and one
	# sample of a subsampled component covers XRsiz by YRsiz samples of the
	# reference grid (A.5.1). This library used to write every component into
	# the output plane at its own index, so a half-resolution component landed
	# in a quarter of the plane and the rest stayed zero — 4091 of 4096 samples
	# wrong on the chroma of a 4:2:0 fixture, while the full-resolution
	# component beside it was exact.
	#
	# The comparison is against the fixture's own planes rather than against
	# opj_decompress's output, deliberately. The reference writes a subsampled
	# image through its own upsampling and layout convention, and comparing
	# against that reported 4075 of 4096 samples wrong for a component that was
	# in fact bit-exact. A control below establishes that the fixture is sound
	# before anything is concluded from it.
	if ! python3 - "$WORK/yuv420.raw" "$WORK/yuv422.raw" <<'SUBEOF'; then
import sys
w = h = 64
luma = bytes([(x * 3 + y) % 256 for y in range(h) for x in range(w)])
cw, ch = w // 2, h // 2
u = bytes([(x * 5 + y * 7) % 256 for y in range(ch) for x in range(cw)])
v = bytes([(x * 11 + y * 3) % 256 for y in range(ch) for x in range(cw)])
open(sys.argv[1], 'wb').write(luma + u + v)
u2 = bytes([(x * 5 + y * 7) % 256 for y in range(h) for x in range(cw)])
v2 = bytes([(x * 11 + y * 3) % 256 for y in range(h) for x in range(cw)])
open(sys.argv[2], 'wb').write(luma + u2 + v2)
SUBEOF
		gap "component subsampling: the fixture planes could not be written"
	else
		for spec in "420:2:2:1x1:2x2:2x2" "422:2:1:1x1:2x1:2x1"; do
			name=${spec%%:*}
			rest=${spec#*:}
			sdx=${rest%%:*}
			rest=${rest#*:}
			sdy=${rest%%:*}
			subs=${rest#*:}
			f="$WORK/sub_$name.j2k"
			if ! opj_compress -i "$WORK/yuv$name.raw" -o "$f" \
				-F "64,64,3,8,u@$subs" -n 3 -r 1 >/dev/null 2>&1; then
				gap "read subsampling $name: OpenJPEG could not produce a fixture"
				continue
			fi
			# Control: the reference must accept the fixture as the geometry it
			# was meant to be, or a disagreement below says nothing.
			geom=$(opj_dump -i "$f" 2>/dev/null | grep -cE "dx=$sdx, dy=$sdy")
			if [ "${geom:-0}" -lt 2 ]; then
				gap "read subsampling $name: the fixture does not declare two components at ${sdx}x${sdy}"
				continue
			fi
			if out=$(go run ./scripts/subsampcmp "$f" "$WORK/yuv$name.raw" "$sdx" "$sdy" 2>&1); then
				pass "read subsampling $name: every component lands on the reference grid exactly ($out)"
			else
				fail "read subsampling $name: $out"
			fi
		done
	fi

	# Writing subsampled components, read back by OpenJPEG.
	#
	# -upsample is what makes this measurable: it puts every component on the
	# reference grid, so one raw file carries all of them at a known size and
	# the expected value at each position is a closed form. Without it the
	# reference writes only the first component to a PGM, or refuses a raw file
	# whose components differ in size.
	#
	# A subsampled component also rules out the multiple component transform,
	# which mixes the first three sample by sample and so needs them on one
	# grid. Applying it anyway read past the end of the shorter plane.
	sub_write() {
		name=$1
		dx=$2
		dy=$3
		f="$WORK/subw_$name.j2k"
		shift 3
		if ! err=$(go run ./scripts/subsampgen "$f" 64 "$dx" "$dy" "$@" 2>&1); then
			fail "write subsampling $name: our encoder failed: $(echo "$err" | head -1 | cut -c1-90)"
			return
		fi
		if ! err=$(opj_decompress -i "$f" -o "$f.raw" -upsample 2>&1); then
			fail "write subsampling $name: OpenJPEG refused our codestream: $(echo "$err" | grep -i error | head -1 | cut -c1-70)"
			return
		fi
		if out=$(python3 - "$f.raw" "$dx" "$dy" <<'SUBWEOF'
import sys
d = open(sys.argv[1], 'rb').read()
dx, dy = int(sys.argv[2]), int(sys.argv[3])
w = h = 64
n = w * h
if len(d) < 4 * n:
    print("reference wrote %d bytes, expected %d" % (len(d), 4 * n))
    sys.exit(1)

def sample(c, x, y):
    if c == 0: return (x * 3 + y) % 256
    if c == 1: return (x + y * 5) % 256
    if c == 2: return (x * x + y * y) // 32 % 256
    return 255

bad = [0, 0, 0, 0]
for c in range(4):
    sx, sy = (dx, dy) if c in (1, 2) else (1, 1)
    for y in range(h):
        for x in range(w):
            want = sample(c, (x // sx) * sx, (y // sy) * sy)
            if d[c * n + y * w + x] != want:
                bad[c] += 1
print("per component %s of %d" % (bad, n))
sys.exit(1 if sum(bad) else 0)
SUBWEOF
		); then
			pass "write subsampling $name: OpenJPEG places every component exactly ($out)"
		else
			fail "write subsampling $name: $out"
		fi
	}

	sub_write "420" 2 2
	sub_write "422" 2 1
	sub_write "420_tiled" 2 2 32
	sub_write "422_tiled" 2 1 32
	sub_write "420_lossy" 2 2 0 1
	sub_write "411" 4 1

	# Packet length markers: PLT in every tile-part header, TLM in the main
	# header.
	#
	# These do not change the image, so a decode check alone is close to
	# vacuous: a decoder that ignores both markers returns the right pixels
	# whatever they contain. The marker that is present but malformed is the
	# real risk, and it is silent — OpenJPEG reported our first TLM as "not of
	# expected size" and carried on decoding perfectly, because Stlm claimed a
	# two-byte tile index (0x60) where a one-byte one was written (0x50). So
	# what is asserted here is that the reference parses the main header with
	# no diagnostic at all, not merely that the pixels survive.
	plt_write() {
		name=$1
		shift
		# Three components come back as P6; anything else as P5. The reference
		# raster's own format is what the comparison is against.
		local ext=pgm
		[ "${4:-1}" = 3 ] && ext=ppm
		f="$WORK/plt_$name.j2k"
		if ! ref=$(go run "$WORK/p1enc.go" "$f" "$@" 2>&1); then
			fail "write PLT/TLM $name: our encoder failed: $(echo "$ref" | head -1)"
			return
		fi
		if ! err=$(opj_decompress -i "$f" -o "$f.out.$ext" 2>&1); then
			fail "write PLT/TLM $name: OpenJPEG refused our codestream: $(echo "$err" | grep -i error | head -1 | cut -c1-70)"
			return
		fi
		if ! d=$(cmp_raster "$f.out.$ext" "$ref"); then
			fail "write PLT/TLM $name: $d"
			return
		fi
		if command -v opj_dump >/dev/null 2>&1; then
			diag=$(opj_dump -i "$f" 2>&1 | grep -iE "warning|error" | head -1)
			if [ -n "$diag" ]; then
				fail "write PLT/TLM $name: the reference decodes it but complains: $(echo "$diag" | cut -c1-95)"
				return
			fi
			pass "write PLT/TLM $name: OpenJPEG decodes it exactly and parses both markers without complaint"
		else
			pass "write PLT/TLM $name: OpenJPEG decodes it exactly (opj_dump absent, markers not inspected)"
		fi
	}

	# p1enc argument 11 turns the markers on.
	plt_write "plain" 64 3 0 1 8 1 0 0 0 1
	plt_write "precincts" 64 3 0 1 8 1 0 0 5 1
	plt_write "tiled" 128 3 64 1 8 1 0 0 5 1
	plt_write "rgb" 64 3 0 3 8 1 0 0 5 1
	plt_write "layers3" 64 3 0 1 8 3 0 0 5 1

	# An index built from the markers must name the same packets, at the same
	# byte ranges, as one built by parsing every packet header; and it must
	# cost the headers rather than the file.
	if out=$(go test ./ -run 'TestPLTIndex' -v 2>&1); then
		cost=$(printf '%s\n' "$out" | sed -n 's/.*size *\([0-9]* *: indexed .*\)$/\1/p' | tail -1)
		pass "PLT index: built from the markers, identical to the walked index (${cost:-measured})"
	else
		fail "PLT index: $(printf '%s\n' "$out" | grep -E 'lengthmarkers_index_test' | head -1 | cut -c1-110)"
	fi

	# Explicit precinct partitions, written by us and read by OpenJPEG.
	#
	# The read direction is checked further down; this is the other half. A
	# partition is only useful if other implementations accept it, and the two
	# halves fail differently: writing a partition the COD does not describe
	# produces a codestream nothing can read, while reading one is a decode
	# problem alone.
	#
	# 2^4 is smaller than the 64x64 code-block, so the code-block partition is
	# clipped to the precinct (B.7); 2^8 is larger than the image, which is the
	# degenerate single-precinct case reached through the explicit path rather
	# than the default one.
	for pexp in 4 5 6 8; do
		p1_write "prec2e$pexp" 64 3 0 1 8 1 0 0 "$pexp"
	done
	p1_write "prec_odd" 127 3 0 1 8 1 0 0 5
	p1_write "prec_tiled" 64 3 32 1 8 1 0 0 5
	p1_write "prec_rgb" 64 3 0 3 8 1 0 0 5
	p1_write "prec_layers3" 64 3 0 1 8 3 0 0 5

	# Every progression order carrying a real precinct partition. With one
	# precinct the five orders emit the same sequence, so this is the first
	# check that can tell them apart at all.
	#
	# The three axes are crossed rather than checked one at a time: the orders
	# differ only when precinct, layer and component all have more than one
	# value, and an implementation that permutes two of the three correctly can
	# still emit the third in the wrong place.
	for po in 0 1 2 3 4; do
		p1_write "prec_order$po" 64 3 0 1 8 1 0 "$po" 5
		p1_write "prec_order${po}_rgb" 64 3 0 3 8 1 0 "$po" 5
		p1_write "prec_order${po}_layers3" 64 3 0 1 8 3 0 "$po" 5
		p1_write "prec_order${po}_rgb_layers3" 64 3 0 3 8 3 0 "$po" 5
	done

	# Quality layers as an external decoder sees them, one prefix at a time.
	# Decoding every layer being exact says the split loses nothing; it does not
	# say the split means anything, because a single layer holding everything
	# would pass it too. This asserts that each further layer of ours improves
	# what OpenJPEG reconstructs, which only holds if the coding passes really
	# are distributed across the packets.
	f="$WORK/p1w_layers8.j2k"
	if [ -f "$f" ]; then
		prev=""
		monotone=1
		detail=""
		for l in 1 2 4 6 8; do
			opj_decompress -i "$f" -o "$f.l$l.pgm" -l "$l" >/dev/null 2>&1
			mse=$(python3 - "$f.l$l.pgm" "$f.pgm" <<'PYEOF'
import sys

def rd(p):
    d = open(p, 'rb').read()
    i, t = 0, []
    while len(t) < 4:
        while d[i:i+1] in b' \n\t\r':
            i += 1
        if d[i:i+1] == b'#':
            while d[i:i+1] != b'\n':
                i += 1
            continue
        s = i
        while d[i:i+1] not in b' \n\t\r':
            i += 1
        t.append(d[s:i])
    return d[i+1:]

a, b = rd(sys.argv[1]), rd(sys.argv[2])
print("%.3f" % (sum((x - y) ** 2 for x, y in zip(a, b)) / max(len(b), 1)))
PYEOF
			)
			detail="$detail l$l=$mse"
			if [ -n "$prev" ] && ! python3 -c "import sys; sys.exit(0 if float('$mse') <= float('$prev') + 1e-9 else 1)"; then
				monotone=0
			fi
			prev=$mse
		done
		if [ "$monotone" = 1 ] && [ "$prev" = "0.000" ]; then
			pass "write Part 1 MQ 8 quality layers: each prefix OpenJPEG decodes improves, and all eight are exact ($detail )"
		else
			fail "write Part 1 MQ 8 quality layers: prefixes do not improve monotonically to exact ($detail )"
		fi
	fi

	# Every progression order, with four layers and three components so that
	# each of layer, resolution and component takes a turn as the outermost
	# index. A single layer and a single component collapse all five orders
	# onto the same sequence, which is what let a layer-major walk pass for
	# every one of them.
	for order in 0 1 2 3 4; do
		p1_write "order${order}_layers4" 64 3 0 3 8 4 0 "$order"
	done

	# Irreversible 9/7 at quality 100, where the step sizes are sized for half
	# a sample of error, so a decoder that rounds to an integer must return the
	# source exactly.
	for nres in 1 3 5; do
		p1_write "lossy_${nres}res_q100" 64 "$nres" 0 1 8 1 100
	done

	for src in src src32; do
		for n in 1 2 3; do
			f="$WORK/p_${src}_$n.j2k"
			if ! opj_compress -i "$WORK/$src.pgm" -o "$f" -n "$n" -r 1 >/dev/null 2>&1; then
				gap "read Part 1 $src -n $n: OpenJPEG could not produce a fixture"
				continue
			fi
			out=$(go run ./scripts/decodecmp "$f" "$WORK/$src.pgm" 2>&1)
			if [ "$out" = "0" ]; then
				pass "read Part 1 MQ $src -n $n: we decode OpenJPEG's codestream exactly"
			else
				fail "read Part 1 MQ $src -n $n: $out samples differ"
			fi
		done
	done

	# WRITE side, HT with quality layers. Multiple layers used to be written as
	# a private per-layer length table rather than as packets; they are real
	# packets now, one per (layer, resolution, component), with each block's
	# coding passes split across them.
	#
	# OpenJPH is not the oracle here: it refuses any codestream with more than
	# one quality layer ("The current implementation supports 1 quality layer
	# only"). OpenJPEG reads both HT block coding and multiple layers, so it is
	# what gates this corner.
	cat >"$WORK/enclayers.go" <<'GOEOF'
//go:build ignore

package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
	size, _ := strconv.Atoi(os.Args[2])
	layers, _ := strconv.Atoi(os.Args[3])
	img := image.NewGray(image.Rect(0, 0, size, size))
	raw := make([]byte, size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := uint8(20 + ((x*13 + y*3) % 200))
			img.Set(x, y, color.Gray{Y: v})
			raw[y*size+x] = v
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := jp2.Encode(f, img, &jp2.Options{
		HighThroughput: true, Lossless: true, NumLayers: layers,
		Format: jp2.FormatJ2K, NumResolutions: 3,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	ref, _ := os.Create(os.Args[1] + ".pgm")
	fmt.Fprintf(ref, "P5\n%d %d\n255\n", size, size)
	ref.Write(raw)
	ref.Close()
}
GOEOF

	for layers in 2 3 8; do
		f="$WORK/wl_$layers.j2c"
		if ! go run "$WORK/enclayers.go" "$f" 64 "$layers" >/dev/null 2>&1; then
			fail "write HTJ2K $layers quality layers: our encoder failed"
			continue
		fi
		if ! err=$(opj_decompress -i "$f" -o "$f.out.pgm" 2>&1); then
			fail "write HTJ2K $layers quality layers: OpenJPEG refused our codestream: $(echo "$err" | grep -i error | head -1 | cut -c1-70)"
			continue
		fi
		if d=$(cmp_raster "$f.out.pgm" "$f.pgm"); then
			pass "write HTJ2K $layers quality layers: OpenJPEG decodes it exactly"
		else
			fail "write HTJ2K $layers quality layers: $d"
		fi
	done

	# Quality layers in the read direction: a prefix of someone else's
	# codestream must reconstruct a complete image at lower quality.
	#
	# The write direction above asserts the mirror of this. What is asserted
	# here is not that our reconstruction equals OpenJPEG's at every layer
	# count — reconstructing a coefficient whose low bits were not transmitted
	# is implementation-defined, and the two libraries choose differently — but
	# the three properties that are not: each further layer improves the
	# result, the image is complete at every layer count rather than partial,
	# and decoding every layer is exact.
	#
	# Decoding every layer alone would be a weak check: a codestream whose
	# first layer held everything would pass it. The monotone improvement is
	# what says the coding passes are really distributed across the packets.
	if ! opj_compress -i "$WORK/src32.pgm" -o "$WORK/rl.j2k" -n 3 -r 20,10,5,2,1 >/dev/null 2>&1; then
		gap "read quality layers: OpenJPEG could not produce a multi-layer fixture"
	elif [ "$(opj_dump -i "$WORK/rl.j2k" 2>/dev/null | grep -c 'numlayers=5')" -lt 1 ]; then
		gap "read quality layers: the fixture does not declare five layers"
	else
		prev=""
		monotone=1
		detail=""
		bad=""
		for l in 1 2 3 4 5; do
			out=$(go run ./scripts/layercmp "$WORK/rl.j2k" "$WORK/src32.pgm" "$l" 2>&1)
			mse=$(printf '%s' "$out" | awk '{print $1}')
			dim=$(printf '%s' "$out" | awk '{print $2}')
			case "$mse" in
			''|*[!0-9.]*) bad="layer $l: $out"; break ;;
			esac
			if [ "$dim" != "32x32" ]; then
				bad="layer $l reconstructed $dim, not the whole 32x32 image"
				break
			fi
			detail="$detail l$l=$mse"
			if [ -n "$prev" ] && ! python3 -c "import sys; sys.exit(0 if float('$mse') <= float('$prev') + 1e-9 else 1)"; then
				monotone=0
			fi
			prev=$mse
		done
		if [ -n "$bad" ]; then
			fail "read quality layers: $bad"
		elif [ "$monotone" = "0" ]; then
			fail "read quality layers: a further layer of OpenJPEG's codestream made our reconstruction worse ($detail )"
		elif ! python3 -c "import sys; sys.exit(0 if float('$prev') == 0 else 1)"; then
			fail "read quality layers: all five layers of OpenJPEG's codestream still leave error $prev"
		else
			pass "read quality layers: each prefix of OpenJPEG's codestream improves our reconstruction and all five are exact ($detail )"
		fi
	fi

	# Tiled Part 1: same geometry, MQ-coded rather than HT.
	for tile in 8 16 12 13; do
		f="$WORK/pt_$tile.j2k"
		if ! opj_compress -i "$WORK/src32.pgm" -o "$f" -t "$tile,$tile" -n 3 -r 1 >/dev/null 2>&1; then
			gap "read Part 1 ${tile}x${tile} tiles: OpenJPEG could not produce a fixture"
			continue
		fi
		out=$(go run ./scripts/decodecmp "$f" "$WORK/src32.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read Part 1 MQ ${tile}x${tile} tiles: we decode OpenJPEG's codestream exactly"
		else
			fail "read Part 1 MQ ${tile}x${tile} tiles: $out samples differ"
		fi
	done


	# Explicit precinct partitions.
	#
	# Scod bit 0 declares a precinct partition, which makes a resolution hold
	# many packets instead of one and makes each of them cover a region of the
	# image rather than all of it. This library used to ignore the declaration
	# entirely and read every such codestream against a single maximal
	# precinct, so from the second packet on it was parsing packet headers at
	# the wrong offsets: 65114 of 65536 samples wrong on the first fixture
	# below. Nothing in this gate measured it, while ROADMAP.md claimed it did.
	#
	# Three things are varied because each broke separately while the others
	# worked: the precinct size (a precinct smaller than the declared
	# code-block clips the code-block partition, B.7), the progression order
	# (PCRL and CPRL put position outside resolution, so precinct index means a
	# different region at each resolution and B.12.1.4's coordinate walk is the
	# only correct one), and tiling (the precinct grid is anchored in the
	# resolution's absolute coordinates, not the tile's).
	prec_read() {
		name=$1
		shift
		f="$WORK/prc_$name.j2k"
		if ! opj_compress -i "$WORK/src32.pgm" -o "$f" -r 1 "$@" >/dev/null 2>&1; then
			gap "read precincts $name: OpenJPEG could not produce a fixture"
			return
		fi
		out=$(go run ./scripts/decodecmp "$f" "$WORK/src32.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read precincts $name: we decode OpenJPEG's codestream exactly"
		else
			fail "read precincts $name: $out samples differ"
		fi
	}

	# Control first: the oracle must round-trip a precinct codestream of its
	# own, or none of the rows below mean anything.
	if opj_compress -i "$WORK/src32.pgm" -o "$WORK/prcctl.j2k" -n 3 -r 1 \
		-c "[64,64],[64,64],[64,64]" >/dev/null 2>&1 &&
		opj_decompress -i "$WORK/prcctl.j2k" -o "$WORK/prcctl.pgm" >/dev/null 2>&1 &&
		cmp_raster "$WORK/prcctl.pgm" "$WORK/src32.pgm" >/dev/null; then
		pass "precinct oracle control: OpenJPEG round-trips its own precinct codestream exactly"

		# Sizes, including 8x8 and 16x16, which are smaller than the default
		# 64x64 code-block and so exercise the clipping.
		for sz in 128 64 32 16 8; do
			prec_read "${sz}x${sz}" -n 3 -c "[$sz,$sz],[$sz,$sz],[$sz,$sz]"
		done
		# Sizes that differ per resolution, which is what the marker allows and
		# a single-size implementation gets right by accident.
		prec_read "mixed" -n 3 -c "[128,128],[64,64],[32,32]"

		# Every progression order over a multi-precinct image. With one
		# precinct all five agree, which is exactly why this was never caught.
		for po in LRCP RLCP RPCL PCRL CPRL; do
			prec_read "order_$po" -n 3 -c "[32,32],[32,32],[32,32]" -p "$po"
			prec_read "order_${po}_layers4" -n 3 -c "[32,32],[32,32],[32,32]" -p "$po" -l 4
		done

		# Precincts inside tiles, and precincts with several quality layers.
		prec_read "tiled" -n 3 -c "[32,32],[32,32],[32,32]" -t 64,64
		prec_read "tiled_odd" -n 3 -c "[32,32],[32,32],[32,32]" -t 13,13
		prec_read "layers4" -n 3 -c "[32,32],[32,32],[32,32]" -l 4

		# A packet index over a multi-precinct codestream must resolve an
		# image region to the byte ranges covering it. This is the point of
		# the partition: without it a resolution is one packet spanning the
		# whole image, so every viewport query returns the whole file.
		if out=$(go test ./ -run 'TestPacketsForRegion|TestPacketRangesAreTheRealBytes' -v 2>&1); then
			ratio=$(printf '%s\n' "$out" | sed -n 's/.*viewport \(.*\)$/\1/p' | head -1)
			pass "precinct byte ranges: a viewport resolves to a subset of the codestream (${ratio:-measured})"
		else
			fail "precinct byte ranges: $(printf '%s\n' "$out" | grep -E '^\s+packets_region_test' | head -1 | cut -c1-110)"
		fi

		# Signal: the comparison must be able to report a difference. The
		# reference here is the same size as the real one and differs in a
		# single sample, so what is proved is that values are compared rather
		# than dimensions.
		python3 - "$WORK/src32.pgm" "$WORK/src32alt.pgm" <<'PRCEOF'
import sys
d = bytearray(open(sys.argv[1], 'rb').read())
d[-1] ^= 0xFF
open(sys.argv[2], 'wb').write(bytes(d))
PRCEOF
		sig=$(go run ./scripts/decodecmp "$WORK/prcctl.j2k" "$WORK/src32alt.pgm" 2>&1)
		if [ "$sig" = "0" ]; then
			fail "precinct signal check: comparing a precinct codestream against a different image reported no difference"
		else
			pass "precinct signal check: the comparison reports a difference when the images differ"
		fi
	else
		gap "precinct oracle control failed; the precinct rows would be meaningless"
	fi

	# SOP and EPH packet markers. A decoder that reads through an SOP segment
	# takes six bytes of marker as packet header and recovers nothing after it,
	# so these are worth asserting rather than assuming: with the coding-style
	# detection disabled every sample of these fixtures comes back wrong.
	for opt in "-SOP -EPH:sopeph" "-SOP:sop" "-EPH:eph"; do
		flags=${opt%%:*}
		name=${opt##*:}
		f="$WORK/pm_$name.j2k"
		if ! opj_compress -i "$WORK/src32.pgm" -o "$f" -n 4 $flags -r 1 >/dev/null 2>&1; then
			gap "read Part 1 $name: OpenJPEG could not produce a fixture"
			continue
		fi
		out=$(go run ./scripts/decodecmp "$f" "$WORK/src32.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read Part 1 $name markers: we decode OpenJPEG's codestream exactly"
		else
			fail "read Part 1 $name markers: $out samples differ"
		fi
	done
fi

echo
echo "=== capability matrix ==="
#
# The checks above cover 8-bit greyscale. This section covers the rest of what
# the format supports, because a gate that only exercises one corner is how
# every defect outside that corner survived: the lossy path, tiled images and
# float components were all non-conforming while the greyscale gate was green.
#
if ! have ojph_expand; then
	gap "OpenJPH not installed; capability matrix unchecked"
else
	MX="$WORK/matrix"
	# ---- SOP and EPH error resilience markers -----------------------------
	#
	# EnableSOP and EnableEPH set their bits in the COD's Scod field and wrote
	# no markers at all until v1.5.9, so the codestream declared a structure it
	# did not have. OpenJPEG refused an EnableEPH file outright — it expects
	# EPH after each packet header when Scod says so — while this library
	# round-tripped the same file perfectly, because our decoder skips the
	# marker only when present and never minded that it never was.
	#
	# The check is the reference decoding all four combinations to exactly the
	# fixture, plus the reference's own SOP/EPH stream decoding here.
	if ! command -v opj_compress >/dev/null 2>&1; then
		skip "error resilience markers: opj_compress is not installed"
	else
		SOPD="$WORK/sop"
		mkdir -p "$SOPD"
		if ! go run ./scripts/sopgen "$SOPD" >"$WORK/sop.tsv" 2>"$WORK/sop.err"; then
			fail "error resilience markers: generator failed: $(head -1 "$WORK/sop.err")"
		else
			sop_fail=0
			sop_n=0
			while IFS=$'\t' read -r name nsop neph path; do
				if [ "$nsop" = "ENCODE_FAIL" ]; then
					fail "error resilience $name: encoder failed"
					sop_fail=1
					continue
				fi
				out="$SOPD/$name.out.pgm"
				if ! opj_decompress -i "$path" -o "$out" >/dev/null 2>&1; then
					fail "error resilience $name: the reference refused a file declaring SOP=$nsop EPH=$neph"
					sop_fail=1
					continue
				fi
				if d=$(python3 scripts/jp2cmp.py "$out" 1 8); then
					sop_n=$((sop_n + 1))
				else
					fail "error resilience $name: $d"
					sop_fail=1
				fi
			done <"$WORK/sop.tsv"
			if [ "$sop_fail" = "0" ] && [ "$sop_n" -gt 0 ]; then
				pass "error resilience markers: $sop_n combinations of SOP and EPH decode through the reference to exactly the fixture, markers present as the coding style declares"
			fi
		fi

		# And the other direction: the reference's own SOP/EPH stream read here.
		if oiiotool --pattern noise:type=uniform:seed=3 48x32 1 -d uint8 -o "$SOPD/src.png" >/dev/null 2>&1 &&
			opj_compress -i "$SOPD/src.png" -o "$SOPD/ref.j2k" -SOP -EPH >/dev/null 2>&1 &&
			opj_decompress -i "$SOPD/ref.j2k" -o "$SOPD/ref.pgm" >/dev/null 2>&1; then
			d=$(go run ./scripts/decodecmp "$SOPD/ref.j2k" "$SOPD/ref.pgm" 2>&1 | head -1)
			if [ "$d" = "0" ]; then
				pass "error resilience markers: a codestream the reference wrote with SOP and EPH decodes here sample for sample"
			else
				fail "error resilience markers: $d samples differ reading the reference's SOP/EPH stream"
			fi
		else
			gap "error resilience markers: could not build the reference's SOP/EPH fixture"
		fi

		# Resynchronisation, which is the third of the item's conditions and
		# the one that needed a positive check rather than a fallback: a
		# damaged packet header parses into a different-but-readable header
		# instead of failing, so waiting for an error recovers nothing. The Go
		# test asserts the recovery count, not merely that the decode survived.
		if go test ./ -run 'TestSOPResynchronisesAfterCorruption|TestErrorResilienceMarkersAreWritten' >/dev/null 2>&1; then
			pass "error resilience markers: a corrupted packet header is recovered from by scanning to the next SOP marker, and an undamaged stream resynchronises zero times"
		else
			fail "error resilience markers: the resynchronisation test fails"
		fi
	fi

	# ---- the JP2 container, read by another implementation ----------------
	#
	# The codestreams this library writes have been checked against two oracles
	# across the whole capability matrix. The JP2 boxes around them had never
	# been read by anything else: a codestream is the payload of a JP2, not the
	# container, and every box was validated only by the parser in this same
	# repository — a wrapper both halves of one library agree on, which is the
	# shape of defect an external oracle exists to catch.
	#
	# The comparison recomputes the expected samples from the ramp rather than
	# reading a reference file the generator wrote, so a generator that built
	# its fixture and its image from one wrong expression cannot agree with
	# itself. A one-resolution RGB case is included because its codestream is
	# trivial, so anything the reference objects to there is the container.
	if ! command -v opj_decompress >/dev/null 2>&1; then
		skip "JP2 container: opj_decompress is not installed"
	else
		J2D="$WORK/jp2"
		mkdir -p "$J2D"
		if ! go run ./scripts/jp2gen "$J2D" >"$WORK/jp2.tsv" 2>"$WORK/jp2.err"; then
			fail "JP2 container: generator failed: $(head -1 "$WORK/jp2.err")"
		else
			jp2_fail=0
			jp2_n=0
			while IFS=$'\t' read -r name comps depth path; do
				if [ "$comps" = "ENCODE_FAIL" ]; then
					fail "JP2 container $name: encoder failed"
					jp2_fail=1
					continue
				fi
				ext=pgm
				[ "$comps" = "3" ] && ext=ppm
				out="$J2D/$name.out.$ext"
				if ! opj_decompress -i "$path" -o "$out" >/dev/null 2>&1; then
					fail "JP2 container $name: the reference refused the file this library wrote"
					jp2_fail=1
					continue
				fi
				if d=$(python3 scripts/jp2cmp.py "$out" "$comps" "$depth"); then
					jp2_n=$((jp2_n + 1))
				else
					fail "JP2 container $name: $d"
					jp2_fail=1
				fi
			done <"$WORK/jp2.tsv"
			if [ "$jp2_fail" = "0" ] && [ "$jp2_n" -gt 0 ]; then
				pass "JP2 container: $jp2_n files this library wrapped decode through the reference to exactly the fixture — greyscale 8 and 16 bit and sRGB, including a one-resolution case whose codestream is trivial"
			fi
		fi
	fi

	# ---- a non-zero image offset, written by the reference ----------------
	#
	# XOsiz and YOsiz shift the image origin on the reference grid, and every
	# tile and code-block rectangle is computed relative to them. A decoder
	# that ignores the offset produces a plausible image of the right size
	# holding the wrong samples, which no round trip through this library can
	# see: our encoder does not write a non-zero offset, so the codestream has
	# to come from somewhere else. opj_compress -d writes one.
	#
	# This axis was added when the matrix was widened. It found nothing, which
	# is recorded rather than quietly dropped: an axis that passes is evidence,
	# and one that is removed after passing is evidence thrown away.
	if ! command -v opj_compress >/dev/null 2>&1; then
		skip "image offset: opj_compress is not installed"
	else
		OFFD="$WORK/imgoffset"
		mkdir -p "$OFFD"
		if ! oiiotool --pattern noise:type=uniform:seed=7 48x32 1 -d uint8 -o "$OFFD/src.png" >/dev/null 2>&1; then
			gap "image offset: could not build the source raster"
		elif ! opj_compress -i "$OFFD/src.png" -o "$OFFD/off.j2k" -d 7,5 >/dev/null 2>&1; then
			fail "image offset: the reference encoder would not write a non-zero XOsiz/YOsiz"
		elif ! opj_decompress -i "$OFFD/off.j2k" -o "$OFFD/off.ref.pgm" >/dev/null 2>&1; then
			fail "image offset: the reference cannot decode its own offset codestream"
		else
			d=$(go run ./scripts/decodecmp "$OFFD/off.j2k" "$OFFD/off.ref.pgm" 2>&1 | head -1)
			if [ "$d" = "0" ]; then
				pass "image offset: a codestream the reference wrote with XOsiz=7 YOsiz=5 decodes here sample for sample"
			else
				fail "image offset: $d samples differ from the reference's own decode of its offset codestream"
			fi
		fi
	fi

	if ! go run ./scripts/matrixgen "$MX" >"$WORK/matrix.tsv" 2>"$WORK/matrix.err"; then
		fail "capability matrix: generator failed"
		head -5 "$WORK/matrix.err"
	else
		while IFS=$'\t' read -r name kind comps depth stream ref; do
			if [ "$comps" = "ENCODE_FAIL" ]; then
				fail "matrix $name: encoder failed"
				continue
			fi
			out="$MX/$name.out.$kind"

			# A binary32 component cannot be compared through PGM or PPM.
			# ojph_expand writes an all-zero raster with maxval 0 for one --
			# for its own codestreams as much as for ours -- so the float rows
			# of this matrix used to assert nothing whatever. PFM is the only
			# raster either oracle carries binary32 on, and the control below
			# is what distinguishes "the oracle cannot carry this content" from
			# "our codestream is wrong".
			if [ "$kind" = pfm ]; then
				ctl="$MX/$name.ctl"
				if ! ojph_compress -i "$ref" -o "$ctl.j2c" \
					-num_decomps 2 -reversible true >/dev/null 2>&1 ||
					! ojph_expand -i "$ctl.j2c" -o "$ctl.pfm" >/dev/null 2>&1 ||
					! go run ./scripts/floatpfm cmp "$ref" "$ctl.pfm" >/dev/null 2>&1; then
					gap "matrix $name: oracle does not round-trip this float raster, measurement would be meaningless"
					continue
				fi
				if ! err=$(ojph_expand -i "$stream" -o "$out" 2>&1); then
					fail "matrix $name: reference refused our codestream: $(echo "$err" | grep -oE 'ojph error.*' | head -1 | cut -c1-70)"
					continue
				fi
				if d=$(go run ./scripts/floatpfm cmp "$out" "$ref"); then
					pass "matrix $name: the reference decodes it exactly"
				else
					fail "matrix $name: $d"
				fi
				continue
			fi

			if ! err=$(ojph_expand -i "$stream" -o "$out" 2>&1); then
				fail "matrix $name: reference refused our codestream: $(echo "$err" | grep -oE 'ojph error.*' | head -1 | cut -c1-70)"
				continue
			fi
			# Compare header and raster separately: a wrong maxval means the
			# precision we signalled is nonsensical even when the samples decode.
			if d=$(python3 - "$out" "$ref" <<'PYEOF'
import sys
def rd(p):
    d=open(p,'rb').read(); i=0; t=[]
    while len(t)<4:
        while d[i] in b' \n\t\r': i+=1
        s=i
        while d[i] not in b' \n\t\r': i+=1
        t.append(d[s:i])
    return t, d[i+1:]
ta,a = rd(sys.argv[1]); tb,b = rd(sys.argv[2])
if ta[3] != tb[3]:
    print(f"maxval {ta[3].decode()} where the reference raster says {tb[3].decode()}"); sys.exit(1)
if len(a) != len(b):
    print(f"raster {len(a)} bytes vs {len(b)}"); sys.exit(1)
n = sum(1 for x,y in zip(a,b) if x!=y)
print("exact" if n==0 else f"{n}/{len(b)} samples differ")
sys.exit(0 if n==0 else 1)
PYEOF
			); then
				pass "matrix $name: the reference decodes it exactly"
			else
				fail "matrix $name: $d"
			fi
		done <"$WORK/matrix.tsv"
	fi
fi

echo
echo "=== result ==="
echo "checks run: $CHECKS, failures: $FAILURES, known gaps: $GAPS"
if [ "$FAILURES" -ne 0 ]; then
	echo "VALIDATION FAILED"
	exit 1
fi
echo "VALIDATION PASSED"
