#!/usr/bin/env python3
"""Compare a reference decode of a JP2 against the fixture it was built from.

    jp2cmp.py <decoded.pgm|ppm> <components> <depth>

Prints "exact" and exits 0, or a description and exits 1.

The expected samples are recomputed here from the same ramp the generator uses
rather than read from a file the generator wrote, so a generator that wrote its
fixture and its image from one wrong expression cannot agree with itself.
"""
import sys


def ramp(x, y, c):
    return 20 + ((x * 13 + y * 3 + c * 29) % 200)


def read_pnm(path):
    d = open(path, "rb").read()
    i = 0
    fields = []
    while len(fields) < 4:
        while d[i:i + 1].isspace():
            i += 1
        if d[i:i + 1] == b"#":          # OpenJPEG stamps its version in a comment
            while d[i:i + 1] != b"\n":
                i += 1
            continue
        j = i
        while not d[j:j + 1].isspace():
            j += 1
        fields.append(d[i:j])
        i = j
    return fields, d[i + 1:]


def main():
    if len(sys.argv) != 4:
        print("usage: jp2cmp.py <decoded> <components> <depth>")
        return 1
    path = sys.argv[1]
    comps = int(sys.argv[2])
    depth = int(sys.argv[3])
    size = 32

    fields, raster = read_pnm(path)
    w, h = int(fields[1]), int(fields[2])
    if (w, h) != (size, size):
        print(f"reference decoded {w}x{h}, the fixture is {size}x{size}")
        return 1

    want = bytearray()
    for y in range(size):
        for x in range(size):
            for c in range(comps):
                v = ramp(x, y, c)
                if depth >= 16:
                    want += (v * 257).to_bytes(2, "big")
                else:
                    want += bytes([v])

    if len(raster) != len(want):
        print(f"raster is {len(raster)} bytes, expected {len(want)}")
        return 1
    n = sum(1 for a, b in zip(raster, want) if a != b)
    if n:
        print(f"{n}/{len(want)} samples differ")
        return 1
    print("exact")
    return 0


sys.exit(main())
