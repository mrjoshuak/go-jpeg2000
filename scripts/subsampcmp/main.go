// Command subsampcmp decodes a codestream whose components are subsampled and
// compares every component against the plane the fixture was built from,
// reporting the count per component.
//
// It exists because the obvious oracle does not work here. opj_decompress
// writes a subsampled image through its own upsampling and layout conventions,
// and comparing against that output made a correct decoder look wrong in 4075
// of 4096 samples — which is a measurement of the convention, not of the codec.
// The fixture's own planes are unambiguous: a component sample covers XRsiz by
// YRsiz samples of the reference grid (A.5.1), so replicating it across that
// footprint is what any decoder placing the component on the reference grid
// must produce.
//
//	subsampcmp <codestream> <planes.raw> <dx> <dy>
//
// planes.raw holds component 0 at full resolution followed by components 1 and
// 2 subsampled by (dx, dy). The exit status is non-zero when anything differs,
// and the counts are printed either way so a partial failure is legible.
package main

import (
	"fmt"
	"os"
	"strconv"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: subsampcmp <codestream> <planes.raw> <dx> <dy>")
		os.Exit(2)
	}
	dx, err := strconv.Atoi(os.Args[3])
	if err != nil || dx < 1 {
		fmt.Println("bad dx:", os.Args[3])
		os.Exit(2)
	}
	dy, err := strconv.Atoi(os.Args[4])
	if err != nil || dy < 1 {
		fmt.Println("bad dy:", os.Args[4])
		os.Exit(2)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()

	img, err := jp2.Decode(f)
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Println("planes:", err)
		os.Exit(1)
	}

	cw, ch := (w+dx-1)/dx, (h+dy-1)/dy
	off := [3]int{0, w * h, w*h + cw*ch}
	pw := [3]int{w, cw, cw}
	sx := [3]int{1, dx, dx}
	sy := [3]int{1, dy, dy}

	if need := w*h + 2*cw*ch; len(raw) < need {
		fmt.Printf("planes file is %d bytes, need %d for %dx%d at %dx%d subsampling\n",
			len(raw), need, w, h, dx, dy)
		os.Exit(1)
	}

	diff := [3]int{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			got := [3]int{int(byte(r >> 8)), int(byte(g >> 8)), int(byte(bl >> 8))}
			for p := 0; p < 3; p++ {
				if got[p] != int(raw[off[p]+(y/sy[p])*pw[p]+x/sx[p]]) {
					diff[p]++
				}
			}
		}
	}

	fmt.Printf("%d %d %d of %d\n", diff[0], diff[1], diff[2], w*h)
	if diff[0]+diff[1]+diff[2] != 0 {
		os.Exit(1)
	}
}
