// +build ignore

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
)

func main() {
	sizes := []int{64, 128, 256, 512}
	iterations := 10

	fmt.Println("=== JPEG2000 Benchmark Comparison ===")
	fmt.Println("Go Implementation vs OpenJPEG Reference")
	fmt.Println()

	tmpDir, err := os.MkdirTemp("", "jp2bench")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	fmt.Printf("%-10s | %-20s | %-20s | %-10s\n", "Size", "Go Encode", "OpenJPEG Encode", "Ratio")
	fmt.Println("-----------+----------------------+----------------------+-----------")

	for _, size := range sizes {
		// Create test image
		img := createTestImage(size)

		// Save as PNG for OpenJPEG
		pngPath := filepath.Join(tmpDir, fmt.Sprintf("test_%d.png", size))
		jp2PathOPJ := filepath.Join(tmpDir, fmt.Sprintf("test_%d_opj.jp2", size))

		pngFile, _ := os.Create(pngPath)
		png.Encode(pngFile, img)
		pngFile.Close()

		// Benchmark Go encoding
		goEncodeTime := benchmarkGoEncode(img, iterations)

		// Benchmark OpenJPEG encoding
		opjEncodeTime := benchmarkOpenJPEGEncode(pngPath, jp2PathOPJ, iterations)

		ratio := float64(goEncodeTime) / float64(opjEncodeTime)
		fmt.Printf("%-10s | %-20s | %-20s | %-10.2fx\n",
			fmt.Sprintf("%dx%d", size, size),
			goEncodeTime.Round(time.Microsecond),
			opjEncodeTime.Round(time.Microsecond),
			ratio)
	}

	fmt.Println()
	fmt.Printf("%-10s | %-20s | %-20s | %-10s\n", "Size", "Go Decode", "OpenJPEG Decode", "Ratio")
	fmt.Println("-----------+----------------------+----------------------+-----------")

	for _, size := range sizes {
		// Create test image and encode with Go
		img := createTestImage(size)
		jp2PathGo := filepath.Join(tmpDir, fmt.Sprintf("test_%d_go.jp2", size))

		var buf bytes.Buffer
		opts := jpeg2000.DefaultOptions()
		opts.Lossless = true
		jpeg2000.Encode(&buf, img, opts)

		// Save Go-encoded file for OpenJPEG to decode
		os.WriteFile(jp2PathGo, buf.Bytes(), 0644)

		// Benchmark Go decoding
		goDecodeTime := benchmarkGoDecode(buf.Bytes(), iterations)

		// Benchmark OpenJPEG decoding
		outPng := filepath.Join(tmpDir, fmt.Sprintf("out_%d.png", size))
		opjDecodeTime := benchmarkOpenJPEGDecode(jp2PathGo, outPng, iterations)

		ratio := float64(goDecodeTime) / float64(opjDecodeTime)
		fmt.Printf("%-10s | %-20s | %-20s | %-10.2fx\n",
			fmt.Sprintf("%dx%d", size, size),
			goDecodeTime.Round(time.Microsecond),
			opjDecodeTime.Round(time.Microsecond),
			ratio)
	}

	// Additional detailed benchmarks
	fmt.Println()
	fmt.Println("=== Detailed Component Benchmarks (Go) ===")
	runDetailedBenchmarks()
}

func createTestImage(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 255) / size),
				G: uint8((y * 255) / size),
				B: uint8(((x + y) * 127) / size),
				A: 255,
			})
		}
	}
	return img
}

func benchmarkGoEncode(img image.Image, iterations int) time.Duration {
	opts := jpeg2000.DefaultOptions()
	opts.Lossless = true

	// Warmup
	var buf bytes.Buffer
	jpeg2000.Encode(&buf, img, opts)

	start := time.Now()
	for i := 0; i < iterations; i++ {
		buf.Reset()
		jpeg2000.Encode(&buf, img, opts)
	}
	return time.Since(start) / time.Duration(iterations)
}

func benchmarkGoDecode(data []byte, iterations int) time.Duration {
	// Warmup
	jpeg2000.Decode(bytes.NewReader(data))

	start := time.Now()
	for i := 0; i < iterations; i++ {
		jpeg2000.Decode(bytes.NewReader(data))
	}
	return time.Since(start) / time.Duration(iterations)
}

func benchmarkOpenJPEGEncode(pngPath, jp2Path string, iterations int) time.Duration {
	// Warmup - use ALL_CPUS for fair parallel comparison
	exec.Command("opj_compress", "-i", pngPath, "-o", jp2Path, "-r", "1", "-threads", "ALL_CPUS").Run()

	start := time.Now()
	for i := 0; i < iterations; i++ {
		cmd := exec.Command("opj_compress", "-i", pngPath, "-o", jp2Path, "-r", "1", "-threads", "ALL_CPUS", "-quiet")
		cmd.Run()
	}
	return time.Since(start) / time.Duration(iterations)
}

func benchmarkOpenJPEGDecode(jp2Path, outPath string, iterations int) time.Duration {
	// Warmup - use ALL_CPUS for fair parallel comparison
	exec.Command("opj_decompress", "-i", jp2Path, "-o", outPath, "-threads", "ALL_CPUS").Run()

	start := time.Now()
	for i := 0; i < iterations; i++ {
		cmd := exec.Command("opj_decompress", "-i", jp2Path, "-o", outPath, "-threads", "ALL_CPUS", "-quiet")
		cmd.Run()
	}
	return time.Since(start) / time.Duration(iterations)
}

func runDetailedBenchmarks() {
	fmt.Println()
	cmd := exec.Command("go", "test", "-bench=.", "-benchtime=1s", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
