package jpeg2000

import (
	"bytes"
	"image"
	"sync"
	"testing"
)

// TestDecodeHTConcurrentStable decodes HTJ2K on several goroutines at once. Run with -race.
func TestDecodeHTConcurrentStable(t *testing.T) {
	const w, h, workers, rounds = 256, 256, 8, 12
	o := DefaultOptions()
	o.Format = FormatJ2K
	o.Lossless = true
	o.HighThroughput = true
	o.NumResolutions = 4
	o.ProgressionOrder = RPCL

	type frame struct {
		pix  []byte
		code []byte
	}
	frames := make([]frame, workers)
	for i := range frames {
		img := image.NewGray(image.Rect(0, 0, w, h))
		seed := uint32(i + 1)
		for j := range img.Pix {
			seed = seed*1664525 + 1013904223
			img.Pix[j] = byte(seed>>24)/2 + byte(i*13)
		}
		var buf bytes.Buffer
		if err := Encode(&buf, img, o); err != nil {
			t.Fatal(err)
		}
		frames[i] = frame{pix: img.Pix, code: buf.Bytes()}
	}

	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for i := range frames {
		wg.Add(1)
		go func(f frame) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				img, err := Decode(bytes.NewReader(f.code))
				if err != nil {
					errs <- err.Error()
					return
				}
				g, ok := img.(*image.Gray)
				if !ok {
					errs <- "decoded image is not *image.Gray"
					return
				}
				if !bytes.Equal(g.Pix, f.pix) {
					errs <- "decoded samples differ from the input"
					return
				}
			}
		}(frames[i])
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
