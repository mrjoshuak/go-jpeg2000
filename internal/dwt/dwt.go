// Package dwt implements the Discrete Wavelet Transform for JPEG 2000.
//
// JPEG 2000 uses two wavelet filters:
// - 5-3 reversible (lossless): integer arithmetic
// - 9-7 irreversible (lossy): floating-point arithmetic
//
// Both use lifting-based implementations for efficiency.
package dwt

import (
	"math"
	"sync"
)

// Buffer pools for temporary storage to reduce allocations
var (
	intBufPool = sync.Pool{
		New: func() interface{} {
			buf := make([]int32, 4096)
			return &buf
		},
	}
	floatBufPool = sync.Pool{
		New: func() interface{} {
			buf := make([]float64, 4096)
			return &buf
		},
	}
)

// getIntBuf returns a buffer of at least size n from the pool.
func getIntBuf(n int) []int32 {
	bp := intBufPool.Get().(*[]int32)
	buf := *bp
	if cap(buf) < n {
		buf = make([]int32, n)
		*bp = buf
	}
	return buf[:n]
}

// putIntBuf returns a buffer to the pool.
func putIntBuf(buf []int32) {
	bp := &buf
	intBufPool.Put(bp)
}

// getFloatBuf returns a buffer of at least size n from the pool.
func getFloatBuf(n int) []float64 {
	bp := floatBufPool.Get().(*[]float64)
	buf := *bp
	if cap(buf) < n {
		buf = make([]float64, n)
		*bp = buf
	}
	return buf[:n]
}

// Transform type constants.
const (
	// Reversible53 is the 5-3 reversible wavelet transform (lossless).
	Reversible53 = iota
	// Irreversible97 is the 9-7 irreversible wavelet transform (lossy).
	Irreversible97
)

// Forward53 performs the forward 5-3 reversible wavelet transform.
// The input slice is modified in-place.
// length is the number of samples to transform.
// After transformation:
// - Even indices contain low-pass (L) coefficients
// - Odd indices contain high-pass (H) coefficients
func Forward53(data []int32, length int) {
	if length < 2 {
		return
	}

	// Apply lifting steps with loop unrolling
	// Step 1: Update odd samples (high-pass)
	// H[n] = X[2n+1] - floor((X[2n] + X[2n+2]) / 2)
	i := 1
	// Unroll by 4 (processes indices 1, 3, 5, 7)
	for ; i+6 < length-1; i += 8 {
		data[i] -= (data[i-1] + data[i+1]) >> 1
		data[i+2] -= (data[i+1] + data[i+3]) >> 1
		data[i+4] -= (data[i+3] + data[i+5]) >> 1
		data[i+6] -= (data[i+5] + data[i+7]) >> 1
	}
	for ; i < length-1; i += 2 {
		data[i] -= (data[i-1] + data[i+1]) >> 1
	}
	// Handle last odd sample (symmetric extension)
	if length&1 == 0 {
		data[length-1] -= data[length-2]
	}

	// Step 2: Update even samples (low-pass)
	// L[n] = X[2n] + floor((H[n-1] + H[n] + 2) / 4)
	data[0] += (data[1] + data[1] + 2) >> 2
	i = 2
	// Unroll by 4 (processes indices 2, 4, 6, 8)
	for ; i+6 < length-1; i += 8 {
		data[i] += (data[i-1] + data[i+1] + 2) >> 2
		data[i+2] += (data[i+1] + data[i+3] + 2) >> 2
		data[i+4] += (data[i+3] + data[i+5] + 2) >> 2
		data[i+6] += (data[i+5] + data[i+7] + 2) >> 2
	}
	for ; i < length-1; i += 2 {
		data[i] += (data[i-1] + data[i+1] + 2) >> 2
	}
	// Handle last even sample
	if length&1 != 0 {
		data[length-1] += (data[length-2] + data[length-2] + 2) >> 2
	}

	// Rearrange coefficients: L L L... H H H...
	deinterleave(data, length)
}

// Inverse53 performs the inverse 5-3 reversible wavelet transform.
// Reconstructs the original signal from wavelet coefficients.
func Inverse53(data []int32, length int) {
	if length < 2 {
		return
	}

	// Rearrange from L L L... H H H... to interleaved
	interleave(data, length)

	// Reverse lifting steps
	// Step 1: Undo low-pass update
	data[0] -= (data[1] + data[1] + 2) >> 2
	for i := 2; i < length-1; i += 2 {
		data[i] -= (data[i-1] + data[i+1] + 2) >> 2
	}
	if length&1 != 0 {
		data[length-1] -= (data[length-2] + data[length-2] + 2) >> 2
	}

	// Step 2: Undo high-pass update
	for i := 1; i < length-1; i += 2 {
		data[i] += (data[i-1] + data[i+1]) >> 1
	}
	if length&1 == 0 {
		data[length-1] += data[length-2]
	}
}

// 9-7 filter coefficients (from ITU-T Rec. T.800)
const (
	alpha97 = -1.586134342059924  // Step 1
	beta97  = -0.052980118572961  // Step 2
	gamma97 = 0.882911075530934   // Step 3
	delta97 = 0.443506852043971   // Step 4
	k97     = 1.230174104914001   // Scaling factor
	k97Inv  = 0.812893066115961   // 1/k
)

// Forward97 performs the forward 9-7 irreversible wavelet transform.
// Uses floating-point arithmetic for lossy compression.
func Forward97(data []float64, length int) {
	if length < 2 {
		return
	}

	// Step 1: Predict (alpha)
	for i := 1; i < length-1; i += 2 {
		data[i] += alpha97 * (data[i-1] + data[i+1])
	}
	if length&1 == 0 {
		data[length-1] += 2 * alpha97 * data[length-2]
	}

	// Step 2: Update (beta)
	data[0] += 2 * beta97 * data[1]
	for i := 2; i < length-1; i += 2 {
		data[i] += beta97 * (data[i-1] + data[i+1])
	}
	if length&1 != 0 {
		data[length-1] += 2 * beta97 * data[length-2]
	}

	// Step 3: Predict (gamma)
	for i := 1; i < length-1; i += 2 {
		data[i] += gamma97 * (data[i-1] + data[i+1])
	}
	if length&1 == 0 {
		data[length-1] += 2 * gamma97 * data[length-2]
	}

	// Step 4: Update (delta)
	data[0] += 2 * delta97 * data[1]
	for i := 2; i < length-1; i += 2 {
		data[i] += delta97 * (data[i-1] + data[i+1])
	}
	if length&1 != 0 {
		data[length-1] += 2 * delta97 * data[length-2]
	}

	// Step 5: Scale
	for i := 0; i < length; i += 2 {
		data[i] *= k97Inv
	}
	for i := 1; i < length; i += 2 {
		data[i] *= k97
	}

	// Rearrange coefficients
	deinterleaveFloat(data, length)
}

// Inverse97 performs the inverse 9-7 irreversible wavelet transform.
func Inverse97(data []float64, length int) {
	if length < 2 {
		return
	}

	// Rearrange from separated to interleaved
	interleaveFloat(data, length)

	// Undo scaling
	for i := 0; i < length; i += 2 {
		data[i] *= k97
	}
	for i := 1; i < length; i += 2 {
		data[i] *= k97Inv
	}

	// Undo Step 4: Update (delta)
	data[0] -= 2 * delta97 * data[1]
	for i := 2; i < length-1; i += 2 {
		data[i] -= delta97 * (data[i-1] + data[i+1])
	}
	if length&1 != 0 {
		data[length-1] -= 2 * delta97 * data[length-2]
	}

	// Undo Step 3: Predict (gamma)
	for i := 1; i < length-1; i += 2 {
		data[i] -= gamma97 * (data[i-1] + data[i+1])
	}
	if length&1 == 0 {
		data[length-1] -= 2 * gamma97 * data[length-2]
	}

	// Undo Step 2: Update (beta)
	data[0] -= 2 * beta97 * data[1]
	for i := 2; i < length-1; i += 2 {
		data[i] -= beta97 * (data[i-1] + data[i+1])
	}
	if length&1 != 0 {
		data[length-1] -= 2 * beta97 * data[length-2]
	}

	// Undo Step 1: Predict (alpha)
	for i := 1; i < length-1; i += 2 {
		data[i] -= alpha97 * (data[i-1] + data[i+1])
	}
	if length&1 == 0 {
		data[length-1] -= 2 * alpha97 * data[length-2]
	}
}

// deinterleave rearranges data from interleaved to separated (L...H...).
func deinterleave(data []int32, length int) {
	if length < 2 {
		return
	}

	temp := getIntBuf(length)
	halfLen := (length + 1) / 2

	// Copy even samples (low-pass) to first half
	for i, j := 0, 0; i < length; i, j = i+2, j+1 {
		temp[j] = data[i]
	}
	// Copy odd samples (high-pass) to second half
	for i, j := 1, halfLen; i < length; i, j = i+2, j+1 {
		temp[j] = data[i]
	}

	copy(data[:length], temp[:length])
	putIntBuf(temp)
}

// interleave rearranges data from separated (L...H...) to interleaved.
func interleave(data []int32, length int) {
	if length < 2 {
		return
	}

	temp := getIntBuf(length)
	copy(temp[:length], data[:length])

	halfLen := (length + 1) / 2

	// Copy low-pass samples to even positions
	for i, j := 0, 0; j < halfLen; i, j = i+2, j+1 {
		data[i] = temp[j]
	}
	// Copy high-pass samples to odd positions
	for i, j := 1, halfLen; j < length; i, j = i+2, j+1 {
		data[i] = temp[j]
	}
	putIntBuf(temp)
}

// deinterleaveFloat rearranges float64 data from interleaved to separated.
func deinterleaveFloat(data []float64, length int) {
	if length < 2 {
		return
	}

	temp := getFloatBuf(length)
	halfLen := (length + 1) / 2

	for i, j := 0, 0; i < length; i, j = i+2, j+1 {
		temp[j] = data[i]
	}
	for i, j := 1, halfLen; i < length; i, j = i+2, j+1 {
		temp[j] = data[i]
	}

	copy(data[:length], temp[:length])
	putFloatBuf(temp)
}

// interleaveFloat rearranges float64 data from separated to interleaved.
func interleaveFloat(data []float64, length int) {
	if length < 2 {
		return
	}

	temp := getFloatBuf(length)
	copy(temp[:length], data[:length])

	halfLen := (length + 1) / 2

	for i, j := 0, 0; j < halfLen; i, j = i+2, j+1 {
		data[i] = temp[j]
	}
	for i, j := 1, halfLen; j < length; i, j = i+2, j+1 {
		data[i] = temp[j]
	}
	putFloatBuf(temp)
}

// putFloatBuf returns a buffer to the pool.
func putFloatBuf(buf []float64) {
	bp := &buf
	floatBufPool.Put(bp)
}

// Forward2D53 performs a 2D forward 5-3 wavelet transform.
// data is a row-major 2D array with the given dimensions.
func Forward2D53(data []int32, width, height int) {
	// Transform rows - unroll by 4 for better pipelining
	y := 0
	for ; y+4 <= height; y += 4 {
		Forward53(data[y*width:(y+1)*width], width)
		Forward53(data[(y+1)*width:(y+2)*width], width)
		Forward53(data[(y+2)*width:(y+3)*width], width)
		Forward53(data[(y+3)*width:(y+4)*width], width)
	}
	for ; y < height; y++ {
		Forward53(data[y*width:(y+1)*width], width)
	}

	// Transform columns using pooled buffer
	// Process 4 columns at a time for better cache utilization
	col := getIntBuf(height * 4)
	x := 0
	for ; x+4 <= width; x += 4 {
		// Extract 4 columns
		for yy := 0; yy < height; yy++ {
			rowStart := yy * width
			col[yy] = data[rowStart+x]
			col[height+yy] = data[rowStart+x+1]
			col[2*height+yy] = data[rowStart+x+2]
			col[3*height+yy] = data[rowStart+x+3]
		}
		// Transform all 4
		Forward53(col[:height], height)
		Forward53(col[height:2*height], height)
		Forward53(col[2*height:3*height], height)
		Forward53(col[3*height:4*height], height)
		// Write back
		for yy := 0; yy < height; yy++ {
			rowStart := yy * width
			data[rowStart+x] = col[yy]
			data[rowStart+x+1] = col[height+yy]
			data[rowStart+x+2] = col[2*height+yy]
			data[rowStart+x+3] = col[3*height+yy]
		}
	}
	// Handle remaining columns
	for ; x < width; x++ {
		for yy := 0; yy < height; yy++ {
			col[yy] = data[yy*width+x]
		}
		Forward53(col[:height], height)
		for yy := 0; yy < height; yy++ {
			data[yy*width+x] = col[yy]
		}
	}
	putIntBuf(col)
}

// Inverse2D53 performs a 2D inverse 5-3 wavelet transform.
func Inverse2D53(data []int32, width, height int) {
	// Transform columns first (reverse order of forward)
	col := getIntBuf(height)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			col[y] = data[y*width+x]
		}
		Inverse53(col, height)
		for y := 0; y < height; y++ {
			data[y*width+x] = col[y]
		}
	}
	putIntBuf(col)

	// Transform rows
	for y := 0; y < height; y++ {
		row := data[y*width : (y+1)*width]
		Inverse53(row, width)
	}
}

// Forward2D97 performs a 2D forward 9-7 wavelet transform.
func Forward2D97(data []float64, width, height int) {
	// Transform rows
	for y := 0; y < height; y++ {
		row := data[y*width : (y+1)*width]
		Forward97(row, width)
	}

	// Transform columns using pooled buffer
	col := getFloatBuf(height)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			col[y] = data[y*width+x]
		}
		Forward97(col, height)
		for y := 0; y < height; y++ {
			data[y*width+x] = col[y]
		}
	}
	putFloatBuf(col)
}

// Inverse2D97 performs a 2D inverse 9-7 wavelet transform.
func Inverse2D97(data []float64, width, height int) {
	// Transform columns first
	col := getFloatBuf(height)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			col[y] = data[y*width+x]
		}
		Inverse97(col, height)
		for y := 0; y < height; y++ {
			data[y*width+x] = col[y]
		}
	}
	putFloatBuf(col)

	// Transform rows
	for y := 0; y < height; y++ {
		row := data[y*width : (y+1)*width]
		Inverse97(row, width)
	}
}

// SubbandBounds calculates the bounds for each subband at a resolution level.
// Returns the x0, y0, x1, y1 for LL, HL, LH, HH subbands.
type SubbandBounds struct {
	X0, Y0, X1, Y1 int
}

// CalculateSubbands calculates subband bounds for a given resolution level.
// level 0 is the finest resolution.
func CalculateSubbands(width, height, level int) (ll, hl, lh, hh SubbandBounds) {
	// At each level, dimensions are halved
	w := width >> level
	h := height >> level

	halfW := (w + 1) / 2
	halfH := (h + 1) / 2

	ll = SubbandBounds{0, 0, halfW, halfH}
	hl = SubbandBounds{halfW, 0, w, halfH}
	lh = SubbandBounds{0, halfH, halfW, h}
	hh = SubbandBounds{halfW, halfH, w, h}

	return
}

// Quantize quantizes wavelet coefficients with the given step size.
func Quantize(data []float64, stepSize float64) []int32 {
	result := make([]int32, len(data))
	invStep := 1.0 / stepSize
	for i, v := range data {
		if v >= 0 {
			result[i] = int32(math.Floor(v*invStep + 0.5))
		} else {
			result[i] = int32(math.Ceil(v*invStep - 0.5))
		}
	}
	return result
}

// Dequantize reconstructs floating-point values from quantized coefficients.
func Dequantize(data []int32, stepSize float64) []float64 {
	result := make([]float64, len(data))
	for i, v := range data {
		result[i] = float64(v) * stepSize
	}
	return result
}

// DecomposeMultiLevel performs multi-level 2D wavelet decomposition.
// Returns coefficient data organized by resolution level.
func DecomposeMultiLevel53(data []int32, width, height, levels int) {
	w, h := width, height
	for level := 0; level < levels; level++ {
		Forward2D53(data, w, h)
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
}

// ReconstructMultiLevel performs multi-level 2D wavelet reconstruction.
func ReconstructMultiLevel53(data []int32, width, height, levels int) {
	// Calculate dimensions at coarsest level
	dims := make([]struct{ w, h int }, levels)
	w, h := width, height
	for level := 0; level < levels; level++ {
		dims[level] = struct{ w, h int }{w, h}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}

	// Reconstruct from coarsest to finest
	for level := levels - 1; level >= 0; level-- {
		Inverse2D53(data, dims[level].w, dims[level].h)
	}
}

// DecomposeMultiLevel97 performs multi-level 2D 9-7 wavelet decomposition.
func DecomposeMultiLevel97(data []float64, width, height, levels int) {
	w, h := width, height
	for level := 0; level < levels; level++ {
		Forward2D97(data, w, h)
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
}

// ReconstructMultiLevel97 performs multi-level 2D 9-7 wavelet reconstruction.
func ReconstructMultiLevel97(data []float64, width, height, levels int) {
	dims := make([]struct{ w, h int }, levels)
	w, h := width, height
	for level := 0; level < levels; level++ {
		dims[level] = struct{ w, h int }{w, h}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}

	for level := levels - 1; level >= 0; level-- {
		Inverse2D97(data, dims[level].w, dims[level].h)
	}
}
