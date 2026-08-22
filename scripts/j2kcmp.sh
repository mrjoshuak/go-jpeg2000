#!/usr/bin/env bash
# j2kcmp.sh times this library against OpenJPEG and OpenJPH on one image.
#
#   j2kcmp.sh <workdir> <in.pgm> <reps>
#
# Prints a row per implementation: name, encode ms, decode ms. Best of reps for
# every one, which is the same rule this library's own timer uses.
#
# The reference tools are command-line programs, so their timings include
# process start and file I/O that an in-process Go call does not pay. That is
# stated rather than corrected for: subtracting an estimate would be inventing a
# number. What the comparison is good for is orders of magnitude and direction,
# not a percentage.
set -u

WORK="${1:?usage: j2kcmp.sh <workdir> <in.pgm> <reps>}"
SRC="${2:?need an input pgm}"
REPS="${3:-3}"
mkdir -p "$WORK"

# best <label> <command...> — runs the command REPS times, prints the best
# wall-clock in milliseconds.
best() {
	local b=999999
	local i t0 t1 ms
	for ((i = 0; i < REPS; i++)); do
		t0=$(python3 -c 'import time;print(int(time.time()*1000))')
		"$@" >/dev/null 2>&1
		t1=$(python3 -c 'import time;print(int(time.time()*1000))')
		ms=$((t1 - t0))
		[ "$ms" -lt "$b" ] && b=$ms
	done
	echo "$b"
}

if command -v opj_compress >/dev/null 2>&1; then
	e=$(best opj_compress -i "$SRC" -o "$WORK/opj.j2k")
	opj_compress -i "$SRC" -o "$WORK/opj.j2k" >/dev/null 2>&1
	d=$(best opj_decompress -i "$WORK/opj.j2k" -o "$WORK/opj.pgm")
	echo -e "openjpeg\t$e\t$d"
fi

if command -v ojph_compress >/dev/null 2>&1; then
	e=$(best ojph_compress -i "$SRC" -o "$WORK/ojph.j2c" -num_decomps 5 -reversible true)
	ojph_compress -i "$SRC" -o "$WORK/ojph.j2c" -num_decomps 5 -reversible true >/dev/null 2>&1
	d=$(best ojph_expand -i "$WORK/ojph.j2c" -o "$WORK/ojph.pgm")
	echo -e "openjph\t$e\t$d"
fi
