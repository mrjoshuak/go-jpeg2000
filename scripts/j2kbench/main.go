// Command j2kbench times this library encoding and decoding one image, so its
// speed can be quoted against OpenJPEG and OpenJPH rather than against its own
// history.
//
//	j2kbench <in.pgm> <reps> [ht|part1]
//
// It prints two lines, milliseconds, best of reps:
//
//	encode <ms>
//	decode <ms>
//
// Best-of rather than mean, and the same rule for every implementation
// compared: the interesting quantity is how fast the code goes when the machine
// lets it, and a mean on a laptop mostly measures what else is running.
//
// The image comes from a file rather than being generated, so every
// implementation in the comparison sees the same samples. Content matters here
// — the entropy coder's cost depends on how many magnitude bits the
// coefficients occupy — so a comparison on different pixels would measure the
// fixtures.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"
	"time"

	jp2 "github.com/mrjoshuak/go-jpeg2000"
)

func readPGM(path string) (*image.Gray, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	i := 0
	var fields [][]byte
	for len(fields) < 4 {
		for i < len(d) && (d[i] == ' ' || d[i] == '\n' || d[i] == '\t' || d[i] == '\r') {
			i++
		}
		if i < len(d) && d[i] == '#' {
			for i < len(d) && d[i] != '\n' {
				i++
			}
			continue
		}
		j := i
		for j < len(d) && !(d[j] == ' ' || d[j] == '\n' || d[j] == '\t' || d[j] == '\r') {
			j++
		}
		fields = append(fields, d[i:j])
		i = j
	}
	i++
	w, _ := strconv.Atoi(string(fields[1]))
	h, _ := strconv.Atoi(string(fields[2]))
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := i + y*w + x
			if off < len(d) {
				img.Set(x, y, color.Gray{Y: d[off]})
			}
		}
	}
	return img, nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: j2kbench <in.pgm> <reps> [ht|part1]")
		os.Exit(2)
	}
	reps, _ := strconv.Atoi(os.Args[2])
	mode := "ht"
	if len(os.Args) > 3 {
		mode = os.Args[3]
	}

	img, err := readPGM(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	opts := &jp2.Options{
		HighThroughput: mode == "ht",
		Lossless:       true,
		Format:         jp2.FormatJ2K,
		NumResolutions: 6,
	}

	bestEnc, bestDec := 1e30, 1e30
	var cs []byte
	for r := 0; r < reps; r++ {
		var buf bytes.Buffer
		t0 := time.Now()
		if err := jp2.Encode(&buf, img, opts); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		if ms := float64(time.Since(t0).Nanoseconds()) / 1e6; ms < bestEnc {
			bestEnc = ms
		}
		cs = buf.Bytes()

		t1 := time.Now()
		if _, err := jp2.Decode(bytes.NewReader(cs)); err != nil {
			fmt.Fprintln(os.Stderr, "decode:", err)
			os.Exit(1)
		}
		if ms := float64(time.Since(t1).Nanoseconds()) / 1e6; ms < bestDec {
			bestDec = ms
		}
	}

	fmt.Printf("encode %.3f\n", bestEnc)
	fmt.Printf("decode %.3f\n", bestDec)
	fmt.Fprintf(os.Stderr, "bytes %d\n", len(cs))
}
