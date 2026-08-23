#!/usr/bin/env python3
"""Write a deterministic greyscale PGM.

    mkpgm.py <out.pgm> <width> <height> [seed]

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
    if len(sys.argv) not in (4, 5):
        print("usage: mkpgm.py <out.pgm> <width> <height> [seed]", file=sys.stderr)
        return 2
    path = sys.argv[1]
    w = int(sys.argv[2])
    h = int(sys.argv[3])
    seed = int(sys.argv[4]) if len(sys.argv) == 5 else 7

    # A small xorshift so the pattern is reproducible across Python versions,
    # which random.Random does not guarantee.
    state = (seed * 2654435761) & 0xFFFFFFFF or 1
    out = bytearray()
    for _ in range(w * h):
        state ^= (state << 13) & 0xFFFFFFFF
        state ^= state >> 17
        state ^= (state << 5) & 0xFFFFFFFF
        state &= 0xFFFFFFFF
        out.append(state & 0xFF)

    with open(path, "wb") as fh:
        fh.write(b"P5\n%d %d\n255\n" % (w, h))
        fh.write(bytes(out))
    return 0


sys.exit(main())
