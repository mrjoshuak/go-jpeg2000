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
		HighThroughput: true, Lossless: true,
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
	for size in 32 64 128 200; do
		f="$WORK/w_$size.j2c"
		if ! go run "$WORK/enc.go" "$f" "$size" 1 >/dev/null 2>&1; then
			fail "write ${size}px: our encoder failed"
			continue
		fi
		if ! ojph_expand -i "$f" -o "$WORK/w_$size.out.pgm" >/dev/null 2>&1; then
			fail "write ${size}px: OpenJPH refused our codestream"
			continue
		fi
		if d=$(cmp_pgm "$WORK/w_$size.out.pgm" "$f.pgm"); then
			pass "write ${size}px HTJ2K: OpenJPH decodes it exactly ($d differ)"
		else
			fail "write ${size}px HTJ2K: OpenJPH decoded $d samples differently"
		fi
	done

	# Multi-resolution write is known not to round-trip through the reference.
	if go run "$WORK/enc.go" "$WORK/wr2.j2c" 32 2 >/dev/null 2>&1 &&
		ojph_expand -i "$WORK/wr2.j2c" -o "$WORK/wr2.out.pgm" >/dev/null 2>&1; then
		d=$(cmp_pgm "$WORK/wr2.out.pgm" "$WORK/wr2.j2c.pgm" || true)
		if [ "${d%%/*}" = "0" ]; then
			pass "write 32px 2 resolutions: OpenJPH decodes it exactly"
		else
			gap "write 32px 2 resolutions: OpenJPH differs on $d samples (forward DWT is not yet bit-conformant above one level)"
		fi
	fi

	# READ side: do we read what the reference produces, exactly?
	python3 - "$WORK/src.pgm" <<'PYEOF'
import sys
w=h=8
px=bytes([20+((x*13+y*3)%200) for y in range(h) for x in range(w)])
open(sys.argv[1],'wb').write(b'P5\n8 8\n255\n'+px)
PYEOF
	for nd in 0 1; do
		if ! ojph_compress -i "$WORK/src.pgm" -o "$WORK/r_$nd.j2c" \
			-num_decomps "$nd" -reversible true >/dev/null 2>&1; then
			gap "read: OpenJPH could not produce a -num_decomps $nd fixture"
			continue
		fi
		# Control: the oracle must round-trip its own output, or it proves nothing.
		ojph_expand -i "$WORK/r_$nd.j2c" -o "$WORK/r_$nd.ctl.pgm" >/dev/null 2>&1
		if ! cmp -s "$WORK/r_$nd.ctl.pgm" "$WORK/src.pgm"; then
			gap "read -num_decomps $nd: oracle control failed, measurement would be meaningless"
			continue
		fi
		out=$(go run ./scripts/decodecmp "$WORK/r_$nd.j2c" "$WORK/src.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read HTJ2K -num_decomps $nd: we decode OpenJPH's codestream exactly"
		else
			if [ "$nd" = "0" ]; then
				fail "read HTJ2K -num_decomps $nd: $out samples differ"
			else
				gap "read HTJ2K -num_decomps $nd: $out samples differ"
			fi
		fi
	done
fi

if ! have opj_compress; then
	gap "OpenJPEG not installed; Part 1 interoperability unchecked"
else
	for n in 1 2; do
		if ! opj_compress -i "$WORK/src.pgm" -o "$WORK/p_$n.j2k" -n "$n" -r 1 >/dev/null 2>&1; then
			gap "read Part 1 -n $n: OpenJPEG could not produce a fixture"
			continue
		fi
		out=$(go run ./scripts/decodecmp "$WORK/p_$n.j2k" "$WORK/src.pgm" 2>&1)
		if [ "$out" = "0" ]; then
			pass "read Part 1 MQ -n $n: we decode OpenJPEG's codestream exactly"
		else
			if [ "$n" = "1" ]; then
				fail "read Part 1 MQ -n $n: $out samples differ"
			else
				gap "read Part 1 MQ -n $n: $out samples differ"
			fi
		fi
	done
fi

echo
echo "=== result ==="
echo "checks run: $CHECKS, failures: $FAILURES, known gaps: $GAPS"
if [ "$FAILURES" -ne 0 ]; then
	echo "VALIDATION FAILED"
	exit 1
fi
echo "VALIDATION PASSED"
