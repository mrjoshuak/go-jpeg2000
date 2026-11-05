#!/usr/bin/env python3
"""Write a deterministic greyscale PGM.

    mkpgm.py <out.pgm> <width> <height> [seed] [maxval]

maxval is 255 (the default) or 65535. A 16-bit fixture is what reaches the
upper half of the sample range, where this library's decoder once returned
every value above 32768 off by one while every 8-bit check stayed green.

It exists so the gate's fixtures do not depend on oiiotool. That dependency was
real and was not noticed locally: this machine has oiiotool, the sibling
repository's CI builds OpenImageIO from source, and this repository's CI has
neither — so four checks that passed here failed there with "could not build
the fixture". A gate whose fixtures need a tool the gate does not require is a
gate that only runs where it was written.

The content is a hash-based pattern rather than a smooth ramp: incompressible
content is what a codec benchmark should be timed on, and a decoder that emits
zeros or a constant is immediately distinguishable from one that works.
"""
import sys


def main():
    if len(sys.argv) not in (4, 5, 6):
        print("usage: mkpgm.py <out.pgm> <width> <height> [seed] [maxval]", file=sys.stderr)
        return 2
    path = sys.argv[1]
    w = int(sys.argv[2])
    h = int(sys.argv[3])
    seed = int(sys.argv[4]) if len(sys.argv) >= 5 else 7
    maxval = int(sys.argv[5]) if len(sys.argv) == 6 else 255
    if maxval not in (255, 65535):
        print("maxval must be 255 or 65535", file=sys.stderr)
        return 2

    # A small xorshift so the pattern is reproducible across Python versions,
    # which random.Random does not guarantee.
    state = (seed * 2654435761) & 0xFFFFFFFF or 1
    out = bytearray()
    for _ in range(w * h):
        state ^= (state << 13) & 0xFFFFFFFF
        state ^= state >> 17
        state ^= (state << 5) & 0xFFFFFFFF
        state &= 0xFFFFFFFF
        if maxval == 255:
            out.append(state & 0xFF)
        else:
            out += (state & 0xFFFF).to_bytes(2, "big")

    with open(path, "wb") as fh:
        fh.write(b"P5\n%d %d\n%d\n" % (w, h, maxval))
        fh.write(bytes(out))
    return 0


sys.exit(main())
