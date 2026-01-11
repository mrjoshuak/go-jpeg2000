package entropy

import (
	"math/rand"
	"testing"
)

// Benchmark simulating full 512x512 image encoding (64 blocks)
func BenchmarkT1_Full512x512(b *testing.B) {
	numBlocks := 64

	// Create random-ish data for more realistic test
	rng := rand.New(rand.NewSource(42))
	data := make([]int32, 64*64)
	for i := range data {
		data[i] = int32(rng.Intn(256))
	}

	t1 := NewT1(64, 64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < numBlocks; block++ {
			clearFlagsFast(t1.flags)
			t1.SetData(data)
			t1.Encode(0)
		}
	}
}

// Full 512x512 with EncodeFast5 (best optimized version)
func BenchmarkT1_Full512x512_Encode(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	data := make([]int32, 512*512)
	for i := range data {
		data[i] = int32(rng.Intn(256))
	}

	t1 := NewT1(512, 512)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		t1.SetData(data)
		_ = t1.Encode(0)
	}
}
