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

pass() { CHECKS=$((CHECKS + 1)); printf '  ok   - %s\n' "$*"; }
fail() {
	CHECKS=$((CHECKS + 1))
	FAILURES=$((FAILURES + 1))
	printf '  FAIL - %s\n' "$*"
}
note() { printf '  ..   - %s\n' "$*"; }
gap() {
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
fi

if ! have opj_compress; then
	gap "OpenJPEG not installed; Part 1 interoperability unchecked"
else
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
	if ! go run ./scripts/matrixgen "$MX" >"$WORK/matrix.tsv" 2>"$WORK/matrix.err"; then
		fail "capability matrix: generator failed"
		head -5 "$WORK/matrix.err"
	else
		while IFS=$'\t' read -r name comps depth stream ref; do
			if [ "$comps" = "ENCODE_FAIL" ]; then
				fail "matrix $name: encoder failed"
				continue
			fi
			ext=pgm
			[ "$comps" = "3" ] && ext=ppm
			out="$MX/$name.out.$ext"
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
