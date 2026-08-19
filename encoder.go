package jpeg2000

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"runtime"
	"sync"

	"github.com/mrjoshuak/go-jpeg2000/internal/box"
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
	"github.com/mrjoshuak/go-jpeg2000/internal/entropy"
	"github.com/mrjoshuak/go-jpeg2000/internal/mct"
)

// encoder handles JPEG 2000 encoding.
type encoder struct {
	w       io.Writer
	img     image.Image
	options *Options

	// Tile-part index and length, in the order the tile-parts were written.
	// TLM lists these, and it sits in the main header, so it can only be built
	// after every tile-part exists.
	tilePartIdx []int
	tilePartLen []uint32

	// Image parameters
	width              int
	height             int
	numComponents      int
	componentPrecision []int
	componentSigned    []bool

	// Component data
	componentData [][]int32

	// Float encoding state. isFloat marks sample data that is carried through
	// the integer pipeline as a reinterpreted floating-point bit pattern and
	// therefore needs the NLT Type 3 point transform; it covers both binary32
	// (floatImg) and binary16 (halfImg) samples.
	isFloat  bool
	floatImg *FloatImage
	halfImg  *HalfImage

	// Wide sample state. A binary32 component fills the whole int32 range
	// after the NLT Type 3 point transform, so its wavelet coefficients need
	// more magnitude bits than a 32-bit sign-magnitude word holds. See wide.go.
	wide      bool
	wideData  [][]int64 // one transformed plane per component, single-tile
	wideTiles [][][]int64
	wideMb    []int // per subband, the magnitude bit-planes it needs
	wideGuard int

	// Quantization state for the irreversible path, set by
	// transformIrreversible and read by generateQCD and bandMb. Those two must
	// agree: the QCD exponent a decoder reads is what fixes Mb, and the
	// zero-bit-plane counts in the packet headers are written relative to it.
	qcdGuardBits int
	qcdSteps     []codestream.StepSize
}

// htCoder reports whether this codestream's code-blocks are HT-coded.
//
// The option asks for it, and a wide component requires it whether it was asked
// for or not: a binary32 component's coefficients do not fit the int32 word the
// Part 1 MQ coder in this package works on, so encodeCodeBlock uses the HT coder
// for them regardless. Every piece of signalling that describes the block coder
// has to follow the coder actually used — the CAP marker, the COD code-block
// style, and the packet-header parameters — or the codestream declares Part 1
// over HT-coded bytes, which no decoder can read.
func (e *encoder) htCoder() bool { return e.options.HighThroughput || e.wide }

// numResolutions returns the number of resolution levels to encode,
// defaulting to six when the option is unset. Every part of the encoder must
// agree on this value: the COD marker records it, and the DWT, the subband
// layout and the code-block partition are all derived from it.
func (e *encoder) numResolutions() int {
	n := e.options.NumResolutions
	if n <= 0 {
		n = 6
	}
	// Clamp to what the image can actually carry: each extra resolution halves
	// the LL band, so a level whose band would be empty cannot be coded. A
	// 16x16 image supports at most 5. This clamp applies to the HT path only,
	// which is what OpenJPH's own limit follows; the Part 1 path codes the
	// degenerate levels as the packets the layout says are present, and
	// clamping there would change the packet count existing callers observe.
	if !e.options.HighThroughput {
		return n
	}
	// The decomposition applies to each tile independently, so it is the tile
	// that has to carry the levels, not the image.
	tw, th := e.tileExtent()
	maxRes := 1
	for d, hh := tw, th; d > 1 && hh > 1; d, hh = (d+1)/2, (hh+1)/2 {
		maxRes++
	}
	if n > maxRes {
		n = maxRes
	}
	if n < 1 {
		n = 1
	}
	return n
}

// codeBlockExponents returns the log2 code-block width and height. The COD
// marker carries these values and the decoder partitions the subbands with
// them, so the encoder must partition with exactly the same numbers.
//
// The values are held to the limits ISO/IEC 15444-1 Table A.18 places on
// them — each exponent in [2, 10] and their sum at most 12, i.e. at most
// 4096 samples per code-block — because a decoder is entitled to reject
// anything outside that range. A requested 128x128 block therefore becomes
// 64x64.
func (e *encoder) codeBlockExponents() (int, int) {
	xcb, ycb := e.options.CodeBlockSize.X, e.options.CodeBlockSize.Y

	if e.htCoder() {
		xcb = log2BlockSize(e.options.HTBlockWidth)
		ycb = log2BlockSize(e.options.HTBlockHeight)
	}

	if xcb <= 0 {
		xcb = 6 // default: 2^6 = 64
	}
	if ycb <= 0 {
		ycb = 6
	}

	xcb = clampInt(xcb, 2, 10)
	ycb = clampInt(ycb, 2, 10)
	for xcb+ycb > 12 {
		if xcb >= ycb {
			xcb--
		} else {
			ycb--
		}
	}
	return xcb, ycb
}

// log2BlockSize converts a code-block edge length to its log2 exponent,
// returning 0 for a size that is not a power of two in the usable range.
func log2BlockSize(size int) int {
	for exp := 2; exp <= 10; exp++ {
		if size == 1<<exp {
			return exp
		}
	}
	return 0
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// maxPrecision returns the maximum precision across all components.
func (e *encoder) maxPrecision() int {
	m := 0
	for _, p := range e.componentPrecision {
		if p > m {
			m = p
		}
	}
	return m
}

// newEncoder creates a new encoder.
func newEncoder(w io.Writer, img image.Image, options *Options) *encoder {
	bounds := img.Bounds()
	return &encoder{
		w:       w,
		img:     img,
		options: options,
		width:   bounds.Dx(),
		height:  bounds.Dy(),
	}
}

// encode encodes the image.
func (e *encoder) encode() error {
	// Extract image data
	if err := e.extractImageData(); err != nil {
		return fmt.Errorf("extracting image data: %w", err)
	}

	// Apply preprocessing
	if err := e.preprocess(); err != nil {
		return fmt.Errorf("preprocessing: %w", err)
	}

	// Generate codestream
	codestream, err := e.generateCodestream()
	if err != nil {
		return fmt.Errorf("generating codestream: %w", err)
	}

	// Write output based on format
	switch e.options.Format {
	case FormatJP2:
		return e.writeJP2(codestream)
	case FormatJ2K:
		_, err := e.w.Write(codestream)
		return err
	default:
		return fmt.Errorf("unsupported format: %s", e.options.Format)
	}
}

// extractImageData extracts pixel data from the source image.
func (e *encoder) extractImageData() error {
	bounds := e.img.Bounds()

	// Determine image properties based on type
	switch img := e.img.(type) {
	case *image.Gray:
		e.numComponents = 1
		e.componentPrecision = []int{8}
		e.componentSigned = []bool{false}
		e.componentData = make([][]int32, 1)
		e.componentData[0] = make([]int32, e.width*e.height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				e.componentData[0][idx] = int32(img.GrayAt(x, y).Y)
			}
		}

	case *image.Gray16:
		e.numComponents = 1
		e.componentPrecision = []int{16}
		e.componentSigned = []bool{false}
		e.componentData = make([][]int32, 1)
		e.componentData[0] = make([]int32, e.width*e.height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				e.componentData[0][idx] = int32(img.Gray16At(x, y).Y)
			}
		}

	case *image.RGBA:
		e.numComponents = 4
		e.componentPrecision = []int{8, 8, 8, 8}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBAAt(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.RGBA64:
		e.numComponents = 4
		e.componentPrecision = []int{16, 16, 16, 16}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.RGBA64At(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.NRGBA:
		e.numComponents = 4
		e.componentPrecision = []int{8, 8, 8, 8}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.NRGBAAt(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	case *image.NRGBA64:
		e.numComponents = 4
		e.componentPrecision = []int{16, 16, 16, 16}
		e.componentSigned = []bool{false, false, false, false}
		e.componentData = make([][]int32, 4)
		for c := 0; c < 4; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				c := img.NRGBA64At(x, y)
				e.componentData[0][idx] = int32(c.R)
				e.componentData[1][idx] = int32(c.G)
				e.componentData[2][idx] = int32(c.B)
				e.componentData[3][idx] = int32(c.A)
			}
		}

	default:
		// Generic fallback - check if color model supports alpha by testing
		// whether it can represent a fully transparent pixel.
		_, _, _, testA := e.img.ColorModel().Convert(color.Transparent).RGBA()
		hasAlpha := testA == 0

		if hasAlpha {
			e.numComponents = 4
			e.componentPrecision = []int{8, 8, 8, 8}
			e.componentSigned = []bool{false, false, false, false}
		} else {
			e.numComponents = 3
			e.componentPrecision = []int{8, 8, 8}
			e.componentSigned = []bool{false, false, false}
		}
		e.componentData = make([][]int32, e.numComponents)
		for c := 0; c < e.numComponents; c++ {
			e.componentData[c] = make([]int32, e.width*e.height)
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				idx := (y-bounds.Min.Y)*e.width + (x - bounds.Min.X)
				r, g, b, a := e.img.At(x, y).RGBA()
				e.componentData[0][idx] = int32(r >> 8)
				e.componentData[1][idx] = int32(g >> 8)
				e.componentData[2][idx] = int32(b >> 8)
				if hasAlpha {
					e.componentData[3][idx] = int32(a >> 8)
				}
			}
		}
	}

	// Apply precision override if specified
	if e.options.Precision > 0 && e.options.Precision <= 16 {
		targetPrecision := e.options.Precision
		dstMax := int32((1 << targetPrecision) - 1)
		for c := 0; c < e.numComponents; c++ {
			if e.componentPrecision[c] == targetPrecision {
				continue
			}
			srcMax := int32((1 << e.componentPrecision[c]) - 1)
			for i := range e.componentData[c] {
				e.componentData[c][i] = e.componentData[c][i] * dstMax / srcMax
			}
			e.componentPrecision[c] = targetPrecision
		}
	}

	return nil
}

// extractFloatData extracts pixel data from a FloatImage, reinterpreting
// IEEE 754 float32 bits as int32 values for the integer wavelet pipeline.
func (e *encoder) extractFloatData() error {
	if e.floatImg == nil || len(e.floatImg.Components) == 0 {
		return fmt.Errorf("float image has no components")
	}
	if e.floatImg.Width <= 0 || e.floatImg.Height <= 0 {
		return fmt.Errorf("invalid float image size %dx%d", e.floatImg.Width, e.floatImg.Height)
	}
	want := e.floatImg.Width * e.floatImg.Height
	for c, comp := range e.floatImg.Components {
		if len(comp) != want {
			return fmt.Errorf("float component %d has %d samples, want %d", c, len(comp), want)
		}
	}
	e.isFloat = true
	// A binary32 sample fills the whole int32 range once the NLT Type 3 point
	// transform has run, so its coefficients need a 64-bit word. See wide.go.
	e.wide = true
	e.numComponents = len(e.floatImg.Components)
	e.componentPrecision = make([]int, e.numComponents)
	e.componentSigned = make([]bool, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentPrecision[c] = 32
		e.componentSigned[c] = true
	}
	e.width = e.floatImg.Width
	e.height = e.floatImg.Height

	e.componentData = make([][]int32, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentData[c] = make([]int32, e.width*e.height)
		for i, f := range e.floatImg.Components[c] {
			e.componentData[c][i] = int32(math.Float32bits(f))
		}
	}

	return nil
}

// encodeFloat encodes a FloatImage.
func (e *encoder) encodeFloat() error {
	if err := e.extractFloatData(); err != nil {
		return fmt.Errorf("extracting float data: %w", err)
	}

	if err := e.preprocess(); err != nil {
		return fmt.Errorf("preprocessing: %w", err)
	}

	cs, err := e.generateCodestream()
	if err != nil {
		return fmt.Errorf("generating codestream: %w", err)
	}

	switch e.options.Format {
	case FormatJP2:
		return e.writeJP2(cs)
	case FormatJ2K:
		_, err := e.w.Write(cs)
		return err
	default:
		return fmt.Errorf("unsupported format: %s", e.options.Format)
	}
}

// extractHalfData extracts pixel data from a HalfImage, reinterpreting the
// IEEE 754 binary16 bit patterns as signed 16-bit values (sign-extended into
// int32) for the integer wavelet pipeline.
func (e *encoder) extractHalfData() error {
	if e.halfImg == nil || len(e.halfImg.Components) == 0 {
		return fmt.Errorf("half image has no components")
	}
	if e.halfImg.Width <= 0 || e.halfImg.Height <= 0 {
		return fmt.Errorf("invalid half image size %dx%d", e.halfImg.Width, e.halfImg.Height)
	}
	want := e.halfImg.Width * e.halfImg.Height
	for c, comp := range e.halfImg.Components {
		if len(comp) != want {
			return fmt.Errorf("half component %d has %d samples, want %d", c, len(comp), want)
		}
	}

	e.isFloat = true
	e.numComponents = len(e.halfImg.Components)
	e.componentPrecision = make([]int, e.numComponents)
	e.componentSigned = make([]bool, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentPrecision[c] = 16
		e.componentSigned[c] = true
	}
	e.width = e.halfImg.Width
	e.height = e.halfImg.Height

	e.componentData = make([][]int32, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		e.componentData[c] = make([]int32, want)
		for i, h := range e.halfImg.Components[c] {
			// Sign-extend: the half sign bit becomes the int32 sign bit, which
			// is what the NLT Type 3 transform keys off.
			e.componentData[c][i] = int32(int16(h))
		}
	}

	return nil
}

// encodeHalf encodes a HalfImage.
func (e *encoder) encodeHalf() error {
	if err := e.extractHalfData(); err != nil {
		return fmt.Errorf("extracting half data: %w", err)
	}

	if err := e.preprocess(); err != nil {
		return fmt.Errorf("preprocessing: %w", err)
	}

	cs, err := e.generateCodestream()
	if err != nil {
		return fmt.Errorf("generating codestream: %w", err)
	}

	switch e.options.Format {
	case FormatJP2:
		return e.writeJP2(cs)
	case FormatJ2K:
		_, err := e.w.Write(cs)
		return err
	default:
		return fmt.Errorf("unsupported format: %s", e.options.Format)
	}
}

// preprocess applies preprocessing transforms.
func (e *encoder) preprocess() error {
	// For float data, apply NLT Type 3 before anything else
	if e.isFloat {
		for c := 0; c < e.numComponents; c++ {
			nltType3(e.componentData[c], e.componentPrecision[c])
		}
	}

	// Apply DC level shift per component (skip signed components)
	for c := 0; c < e.numComponents; c++ {
		if !e.componentSigned[c] {
			mct.DCLevelShiftForward(e.componentData[c], e.componentPrecision[c])
		}
	}

	// A component whose coefficients do not fit int32 takes the 64-bit
	// transform chain instead of everything below.
	if e.wide {
		return e.preprocessWide()
	}

	// Apply MCT if we have 3+ components
	if e.numComponents >= 3 {
		if e.options.Lossless {
			if e.maxPrecision() > 16 {
				mct.ForwardRCT32(e.componentData[0], e.componentData[1], e.componentData[2])
			} else {
				mct.ForwardRCT(e.componentData[0], e.componentData[1], e.componentData[2])
			}
		} else {
			// Convert to float for ICT
			compFloat := make([][]float64, 3)
			for c := 0; c < 3; c++ {
				compFloat[c] = make([]float64, len(e.componentData[c]))
				for i, v := range e.componentData[c] {
					compFloat[c][i] = float64(v)
				}
			}
			mct.ForwardICT(compFloat[0], compFloat[1], compFloat[2])
			for c := 0; c < 3; c++ {
				for i, v := range compFloat[c] {
					if v >= 0 {
						e.componentData[c][i] = int32(v + 0.5)
					} else {
						e.componentData[c][i] = int32(v - 0.5)
					}
				}
			}
		}
	}

	// Apply DWT. The level count must match the COD marker exactly: one
	// fewer than the resolution count, including zero levels when only the
	// base resolution is coded.
	numLevels := e.numResolutions() - 1

	// A tiled image is transformed one tile at a time, at each tile's own
	// absolute origin: the wavelet does not run across a tile boundary, and
	// which coefficients are lowpass depends on where the tile starts.
	// transformTile dispatches on Lossless itself.
	if e.numTiles() > 1 {
		return nil
	}

	// Untiled irreversible: transformIrreversible also applies the explicit
	// quantisation the 9/7 path signals in QCD.
	if !e.options.Lossless {
		e.transformIrreversible(numLevels)
		return nil
	}

	for c := 0; c < e.numComponents; c++ {
		if e.maxPrecision() > 16 {
			dwt.DecomposeMultiLevel53_32bit(e.componentData[c], e.width, e.height, numLevels)
		} else {
			dwt.DecomposeMultiLevel53(e.componentData[c], e.width, e.height, numLevels)
		}
	}

	return nil
}

// maxQuantIndexBits caps the magnitude of a quantization index. The HT block
// coder doubles the magnitude before coding it (val = t + t in the reference
// encoder), so an index must leave room for that doubling and for the sign in a
// 32-bit word.
const maxQuantIndexBits = 28

// maxBandMb caps Mb, the bit-plane count a subband may declare.
//
// A code-block signals numbps = 2, so its zero bit-plane count is Mb - 1, and
// the HT block decoder derives the position it places magnitudes at as
// p = 30 - (zero bit-planes). ISO/IEC 15444-15 leaves no room below that:
// OpenJPH takes its "p < 0" error path at 31 zero planes
// (ojph_block_decoder32.cpp:768) and OpenJPEG computes the same p. Mb of 31 or
// more therefore produces a file that this library reads back correctly and no
// other implementation does — measured, at eight resolution levels, as a decode
// in which 65151 of 65536 samples differ.
const maxBandMb = 30

// transformIrreversible applies the 9/7 wavelet transform and the Annex E
// quantizer, and records the QCD parameters the codestream must then carry.
//
// The step sizes cannot be chosen independently of the data: the QCD exponent
// fixes Mb, the number of bit-planes a subband is allowed to occupy, and the
// packet headers and the block coder both derive the coded magnitude range from
// it. So the coefficients are measured first, then the step sizes are widened
// if the indices would not fit, and only then are the guard bits set so that
// Mb covers what the block coder will actually emit.
func (e *encoder) transformIrreversible(numLevels int) {
	numRes := numLevels + 1
	prec := e.maxPrecision()

	coefs := make([][]float64, e.numComponents)
	for c := 0; c < e.numComponents; c++ {
		coefs[c] = make([]float64, len(e.componentData[c]))
		for i, v := range e.componentData[c] {
			coefs[c][i] = float64(v)
		}
		dwt.DecomposeMultiLevel97(coefs[c], e.width, e.height, numLevels)
	}

	// Largest coefficient magnitude in each subband, over every component.
	numBands := 3*numLevels + 1
	bandMax := make([]float64, numBands)
	codestream.ForEachSubband(e.width, e.height, numRes, func(sb codestream.SubbandRect) {
		m := 0.0
		for _, data := range coefs {
			for y := 0; y < sb.H; y++ {
				row := (sb.Y0 + y) * e.width
				for x := 0; x < sb.W; x++ {
					if v := math.Abs(data[row+sb.X0+x]); v > m {
						m = v
					}
				}
			}
		}
		if sb.Index < len(bandMax) {
			bandMax[sb.Index] = m
		}
	})

	steps := packStepSizes(idealStepSizes(numRes, e.options.Quality, prec), numRes, prec)

	// Widen every step size by the same power of two until the indices fit and
	// no subband declares more bit-planes than the block coder can place.
	// Halving the exponent field doubles Δ_b, which drops one bit from every
	// index and one from every Mb, and leaves Mb − (index bits) unchanged.
	guard := guardBitsFor(bandMax, steps, numRes, prec)
	for indexBits(bandMax, steps, numRes, prec) > maxQuantIndexBits ||
		guard+maxStepExponent(steps)-1 > maxBandMb {
		widened := false
		for i := range steps {
			if steps[i].Exponent > 1 {
				steps[i].Exponent--
				widened = true
			}
		}
		if !widened {
			break
		}
		guard = guardBitsFor(bandMax, steps, numRes, prec)
	}

	e.qcdGuardBits = guard
	e.qcdSteps = steps

	// Quantize in place, one step size per subband.
	codestream.ForEachSubband(e.width, e.height, numRes, func(sb codestream.SubbandRect) {
		if sb.Index >= len(steps) {
			return
		}
		delta := steps[sb.Index].Delta(prec + codestream.BandGainLog2(sb.Res, sb.Detail))
		for c := 0; c < e.numComponents; c++ {
			for y := 0; y < sb.H; y++ {
				row := (sb.Y0 + y) * e.width
				for x := 0; x < sb.W; x++ {
					i := row + sb.X0 + x
					e.componentData[c][i] = quantizeIndex(coefs[c][i], delta)
				}
			}
		}
	})
}

// guardBitsFor returns the guard-bit count Mb = G + ε_b − 1 needs so that it
// covers every subband. A conforming decoder rejects a code-block whose
// per-quad exponent U_q exceeds Mb, and the HT coder emits U_q equal to the bit
// count of twice the index magnitude, one more than the magnitude itself.
func guardBitsFor(bandMax []float64, steps []codestream.StepSize, numRes, prec int) int {
	guard := 2
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			idx := codestream.BandIndex(res, detail)
			if idx >= len(steps) || idx >= len(bandMax) {
				continue
			}
			rb := prec + codestream.BandGainLog2(res, detail)
			bits := magnitudeBits(bandMax[idx] / steps[idx].Delta(rb))
			if g := bits + 2 - int(steps[idx].Exponent); g > guard {
				guard = g
			}
		}
	}
	if guard > 7 {
		guard = 7
	}
	return guard
}

// maxStepExponent returns the largest exponent in a step-size list.
func maxStepExponent(steps []codestream.StepSize) int {
	m := 0
	for _, s := range steps {
		if int(s.Exponent) > m {
			m = int(s.Exponent)
		}
	}
	return m
}

// indexBits returns the largest number of magnitude bits any quantization
// index will need under the given step sizes.
func indexBits(bandMax []float64, steps []codestream.StepSize, numRes, prec int) int {
	worst := 0
	for res := 0; res < numRes; res++ {
		details := 1
		if res > 0 {
			details = 3
		}
		for detail := 0; detail < details; detail++ {
			idx := codestream.BandIndex(res, detail)
			if idx >= len(steps) || idx >= len(bandMax) {
				continue
			}
			rb := prec + codestream.BandGainLog2(res, detail)
			if b := magnitudeBits(bandMax[idx] / steps[idx].Delta(rb)); b > worst {
				worst = b
			}
		}
	}
	return worst
}

// magnitudeBits returns the number of bits needed for the integer part of v.
func magnitudeBits(v float64) int {
	if !(v >= 1) {
		return 0
	}
	if math.IsInf(v, 0) {
		return 63
	}
	return int(math.Floor(math.Log2(v))) + 1
}

// generateCodestream generates the JPEG 2000 codestream.
func (e *encoder) generateCodestream() ([]byte, error) {
	var buf []byte

	// SOC marker
	buf = append(buf, 0xFF, 0x4F)

	// SIZ marker
	siz := e.generateSIZ()
	buf = append(buf, siz...)

	// CAP marker (required for HTJ2K mode)
	if e.htCoder() {
		cap := e.generateCAP()
		buf = append(buf, cap...)
	}

	// COD marker
	cod := e.generateCOD()
	buf = append(buf, cod...)

	// QCD marker
	qcd := e.generateQCD()
	buf = append(buf, qcd...)

	// NLT markers (for float encoding)
	if e.isFloat {
		nlt := e.generateNLT()
		buf = append(buf, nlt...)
	}

	// Comment marker (optional)
	if e.options.Comment != "" {
		com := e.generateCOM()
		buf = append(buf, com...)
	}

	// Generate tile data.
	//
	// TLM lists every tile-part's length and lives in the main header, so the
	// tiles have to exist before the header is complete. They are assembled
	// here and the marker is spliced in above them rather than appended, which
	// is the whole point: a reader gets the tile-part map without seeking to
	// the end of the file.
	tileData, err := e.generateTiles()
	if err != nil {
		return nil, err
	}
	if e.options.WritePacketLengths {
		if tlm := generateTLM(e.tilePartIdx, e.tilePartLen); tlm != nil {
			buf = append(buf, tlm...)
		}
	}
	buf = append(buf, tileData...)

	// EOC marker
	buf = append(buf, 0xFF, 0xD9)

	return buf, nil
}

// generateSIZ generates the SIZ marker segment.
func (e *encoder) generateSIZ() []byte {
	numComp := e.numComponents

	// Length = 38 + 3*numComponents
	length := 38 + 3*numComp

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.SIZ))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	// Rsiz (profile). In HTJ2K mode bit 14 must also be set, to declare that
	// extended capabilities are signalled in the CAP marker. Without it a
	// conforming decoder reads the stream as baseline Part 1 and rejects the
	// HT-coded block data; OpenJPH reports the file as "not a JPH file".
	rsiz := uint16(e.options.Profile)
	if e.htCoder() {
		rsiz |= 0x4000
	}
	binary.BigEndian.PutUint16(buf[4:6], rsiz)

	// Image dimensions
	binary.BigEndian.PutUint32(buf[6:10], uint32(e.width))
	binary.BigEndian.PutUint32(buf[10:14], uint32(e.height))

	// Image offset (0, 0)
	binary.BigEndian.PutUint32(buf[14:18], 0)
	binary.BigEndian.PutUint32(buf[18:22], 0)

	// Tile size
	tileWidth, tileHeight := e.tileDims()
	binary.BigEndian.PutUint32(buf[22:26], uint32(tileWidth))
	binary.BigEndian.PutUint32(buf[26:30], uint32(tileHeight))

	// Tile offset
	binary.BigEndian.PutUint32(buf[30:34], 0)
	binary.BigEndian.PutUint32(buf[34:38], 0)

	// Number of components
	binary.BigEndian.PutUint16(buf[38:40], uint16(numComp))

	// Component info
	for c := 0; c < numComp; c++ {
		offset := 40 + c*3
		// Ssiz: bit depth (precision - 1, with sign bit)
		ssiz := uint8(e.componentPrecision[c] - 1)
		if e.componentSigned[c] {
			ssiz |= 0x80
		}
		buf[offset] = ssiz
		// XRsiz, YRsiz: subsampling
		buf[offset+1] = 1
		buf[offset+2] = 1
	}

	return buf
}

// generateCOD generates the COD marker segment.
// precinctExpsFor returns the precinct exponents this encoder writes at one
// resolution, and whether an explicit partition is in force at all. A list
// shorter than the resolution count repeats its last entry, which is what makes
// a single {7,7} mean "128x128 everywhere".
func (e *encoder) precinctExpsFor(res int) (int, int, bool) {
	ps := e.options.PrecinctSizes
	if len(ps) == 0 {
		return 15, 15, false
	}
	p := ps[len(ps)-1]
	if res < len(ps) {
		p = ps[res]
	}
	return int(p.WidthExp), int(p.HeightExp), true
}

func (e *encoder) generateCOD() []byte {
	numRes := e.numResolutions()

	// Base length is 12; an explicit precinct partition adds one byte per
	// resolution, which is the only variable-length part of SPcod.
	length := 12
	_, _, explicit := e.precinctExpsFor(0)
	if explicit {
		length += numRes
	}

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.COD))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	// Scod: coding style
	scod := uint8(0)
	if e.options.EnableSOP {
		scod |= codestream.CodingStyleSOP
	}
	if e.options.EnableEPH {
		scod |= codestream.CodingStyleEPH
	}
	if explicit {
		scod |= codestream.CodingStylePrecincts
	}
	buf[4] = scod

	// SGcod
	buf[5] = uint8(e.options.ProgressionOrder) // Progression order
	numLayers := e.options.NumLayers
	if numLayers <= 0 {
		numLayers = 1
	}
	binary.BigEndian.PutUint16(buf[6:8], uint16(numLayers))

	// MCT applies to the first three components and requires that they exist
	// and share one geometry. Signalling it on a 1- or 2-component image
	// produces a stream a conforming decoder rejects: OpenJPH reports the
	// second and third components as (0,0)-(0,0) and refuses the tile. This
	// was previously set unconditionally, contradicting its own comment.
	if e.numComponents >= 3 {
		buf[8] = 1
	}

	// SPcod
	buf[9] = uint8(numRes - 1) // Number of decomposition levels

	// Code-block size, from the single source of truth the tile encoder also
	// uses.
	cbWidth, cbHeight := e.codeBlockExponents()

	buf[10] = uint8(cbWidth - 2)  // Code-block width exponent
	buf[11] = uint8(cbHeight - 2) // Code-block height exponent

	// Code-block style flags
	cbStyle := uint8(0)
	if e.htCoder() {
		cbStyle |= codestream.CodeBlockHT // Set HTJ2K flag (0x40)
	}
	buf[12] = cbStyle

	if e.options.Lossless {
		buf[13] = 1 // 5-3 reversible wavelet
	} else {
		buf[13] = 0 // 9-7 irreversible wavelet
	}

	// Cprecincts: one byte per resolution, PPx in the low nibble and PPy in
	// the high one, lowest resolution first.
	if explicit {
		for r := 0; r < numRes; r++ {
			ppx, ppy, _ := e.precinctExpsFor(r)
			buf[14+r] = uint8(ppx&0x0F) | uint8((ppy&0x0F)<<4)
		}
	}

	return buf
}

// generateQCD generates the QCD marker segment.
func (e *encoder) generateQCD() []byte {
	numRes := e.numResolutions()

	// Calculate number of subbands
	numBands := 3*(numRes-1) + 1

	var buf []byte
	if e.options.Lossless {
		// No quantization
		length := 3 + numBands
		buf = make([]byte, 2+length)
		binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.QCD))
		binary.BigEndian.PutUint16(buf[2:4], uint16(length))

		if e.wide {
			// The magnitude budget was measured from the transformed
			// coefficients; see setWideMagnitudeBudget.
			buf[4] = codestream.QuantizationNone | uint8(e.wideGuard)<<5
			for i := 0; i < numBands; i++ {
				exp := 31
				if i < len(e.wideMb) {
					exp = e.wideMb[i] - e.wideGuard + 1
				}
				buf[5+i] = uint8(clampInt(exp, 0, 31)) << 3
			}
			return buf
		}

		// Sqcd: no quantization, guard bits
		maxPrec := e.maxPrecision()
		// Two guard bits. Mb = guardBits + exponent - 1 bounds U_q, the
		// per-quad exponent the HT block coder emits in the doubled domain,
		// and a conforming decoder rejects any block with
		// U_q > Mb + 2 - numbps. With no guard bits Mb is one or two short of
		// the U_q the detail bands actually produce, which is why OpenJPH and
		// OpenJPEG refused those code-blocks.
		guardBits := uint8(2)
		if maxPrec > 16 {
			guardBits = 2 // need more guard bits for 32-bit
		}
		buf[4] = codestream.QuantizationNone | (guardBits << 5)

		// SPqcd: one exponent per subband
		for i := 0; i < numBands; i++ {
			// Default exponent based on subband level
			exp := maxPrec + i/3
			if exp > 31 {
				exp = 31 // clamp to 5-bit range
			}
			buf[5+i] = uint8(exp) << 3
		}
	} else {
		// Scalar expounded quantization: an exponent/mantissa pair per
		// subband, in the order LL, then HL, LH, HH of each resolution level
		// (ISO/IEC 15444-1 A.6.4, Table A.28 style 2).
		//
		// This used to declare style 1, scalar derived, and then write a
		// single sixteen-bit field that was neither an exponent nor a
		// mantissa but (100 - quality) * 256. OpenJPH refuses the file
		// outright — "Scalar derived quantization is not supported yet in QCD
		// marker", ojph_params.cpp:1926 — and a decoder that did accept it
		// would derive step sizes unrelated to the ones the encoder used,
		// because nothing on the encoding side ever read that field back.
		guard, steps := e.quantizationParameters()

		length := 3 + 2*numBands
		buf = make([]byte, 2+length)
		binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.QCD))
		binary.BigEndian.PutUint16(buf[2:4], uint16(length))
		buf[4] = codestream.QuantizationScalarExpounded | (uint8(guard) << 5)
		for i := 0; i < numBands; i++ {
			var s codestream.StepSize
			if i < len(steps) {
				s = steps[i]
			}
			binary.BigEndian.PutUint16(buf[5+2*i:7+2*i],
				uint16(s.Exponent)<<11|(s.Mantissa&0x07FF))
		}
	}

	return buf
}

// quantizationParameters returns the guard-bit count and per-subband step sizes
// the irreversible path settled on. transformIrreversible always runs before
// any marker is written; the fallback keeps a caller that reaches here without
// having transformed anything from writing a zero-length step-size list.
func (e *encoder) quantizationParameters() (int, []codestream.StepSize) {
	if len(e.qcdSteps) > 0 {
		return e.qcdGuardBits, e.qcdSteps
	}
	numRes := e.numResolutions()
	prec := e.maxPrecision()
	return 2, packStepSizes(idealStepSizes(numRes, e.options.Quality, prec), numRes, prec)
}

// generateCOM generates the COM marker segment.
func (e *encoder) generateCOM() []byte {
	comment := []byte(e.options.Comment)
	length := 4 + len(comment)

	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.COM))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))
	binary.BigEndian.PutUint16(buf[4:6], codestream.CommentLatin1)
	copy(buf[6:], comment)

	return buf
}

// generateCAP generates the CAP (extended capabilities) marker segment.
// This marker is required for HTJ2K mode to signal the use of the
// High-Throughput block coder.
func (e *encoder) generateCAP() []byte {
	// CAP marker format:
	// - Marker (2 bytes): 0xFF50
	// - Length (2 bytes): 8 (length field + Pcap + one Ccap)
	// - Pcap (4 bytes): capabilities flags
	// - Ccap (2 bytes): parameters for the capability Pcap signalled
	// Total: 10 bytes
	//
	// Part 15 requires one 16-bit Ccap field per bit set in Pcap. Emitting
	// Pcap alone produces a marker a conforming decoder rejects.
	length := 8 // Length includes itself, Pcap and Ccap

	buf := make([]byte, 10)
	binary.BigEndian.PutUint16(buf[0:2], uint16(codestream.CAP))
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))
	binary.BigEndian.PutUint32(buf[4:8], codestream.CapPcapHTJ2K)
	// Ccap^15 declares the largest magnitude budget any subband of this
	// codestream carries. It was a constant, which said "10 bit-planes" for
	// every file this library wrote, including binary32 ones needing 35.
	binary.BigEndian.PutUint16(buf[8:10], codestream.CapCcapHTDefault|ccapMagB(e.maxBandMbSignalled()))

	return buf
}

// generateNLT generates NLT marker segments for float encoding.
// One NLT marker is written per component.
func (e *encoder) generateNLT() []byte {
	var buf []byte
	for c := 0; c < e.numComponents; c++ {
		// ISO/IEC 15444-2 A.3.10 / 15444-15 Annex A:
		//   NLT   marker 0xFF76
		//   Lnlt  2 bytes, counting itself: 2 + 2 + 1 + 1 = 6
		//   Cnlt  2 bytes, the component index (0xFFFF means all components)
		//   BDnlt 1 byte, bit 7 = signed, bits 0-6 = depth-1
		//   Tnlt  1 byte, 3 = binary complement (the float/half transform)
		// Cnlt is sixteen bits wide unconditionally; writing it as one byte
		// makes Lnlt 5 and OpenJPH rejects the segment outright
		// ("Unsupported NLT type", ojph_params.cpp:2256, which requires
		// length == 6). This repository's own parser requires 6 as well.
		marker := make([]byte, 8)
		binary.BigEndian.PutUint16(marker[0:2], uint16(codestream.NLT))
		binary.BigEndian.PutUint16(marker[2:4], 6)         // Lnlt
		binary.BigEndian.PutUint16(marker[4:6], uint16(c)) // Cnlt
		bdnlt := uint8(e.componentPrecision[c] - 1)
		if e.componentSigned[c] {
			bdnlt |= 0x80
		}
		marker[6] = bdnlt
		marker[7] = 3 // Tnlt: type 3, binary complement
		buf = append(buf, marker...)
	}
	return buf
}

// generateTiles generates the tile-part segments, one per tile.
//
// A tile grid larger than 1x1 used to be signalled in SIZ and then ignored:
// the encoder emitted a single tile-part holding packets for the whole image,
// which every conforming decoder reads as tile 0 of a grid it was told is
// smaller, so it runs out of geometry inside the first code-block. See
// encodeTileGrid.
func (e *encoder) generateTiles() ([]byte, error) {
	if e.numTiles() > 1 {
		return e.encodeTileGrid()
	}
	return e.encodeTile(0)
}

// codeBlockJob represents a code-block encoding job for parallel processing.
type codeBlockJob struct {
	index int // Order in output
	// Exactly one of data and data64 is set. data64 carries a code-block whose
	// coefficients need more than 32 bits; see wide.go.
	data     []int32
	data64   []int64
	width    int
	height   int
	bandType int

	// Packet grouping. A T2 packet covers one precinct of one (component,
	// resolution), so each code-block must know which precinct it belongs to
	// as well as where it sits inside it. Without an explicit partition there
	// is one precinct per resolution and prec is always 0.
	comp     int
	res      int
	bandIdx  int
	prec     int
	cbx, cby int
}

// codeBlockResult holds the encoded result.
type codeBlockResult struct {
	index       int
	encoded     []byte
	numBPS      int
	truncPoints []int // byte position after each complete bit-plane
}

// computeNumBPS computes the number of bit-planes from absolute values.
func computeNumBPS(data []int32) int {
	maxVal := int32(0)
	for _, v := range data {
		abs := v
		if abs < 0 {
			if abs == math.MinInt32 {
				abs = math.MaxInt32
			} else {
				abs = -abs
			}
		}
		if abs > maxVal {
			maxVal = abs
		}
	}
	if maxVal == 0 {
		return 0
	}
	numBPS := 0
	for maxVal > 0 {
		numBPS++
		maxVal >>= 1
	}
	return numBPS
}

// encodeTile encodes the whole image as a single tile-part.
func (e *encoder) encodeTile(tileIdx int) ([]byte, error) {
	jobs, layout := e.collectJobs(e.componentData, e.wideData, 0, 0, e.width, e.height)
	encoded, numBPS, passes := e.encodeJobs(jobs)
	data, pktLens := e.assembleTileData(layout, jobs, encoded, numBPS, passes)
	return e.createTileHeader(tileIdx, data, pktLens), nil
}

// bandLayout is the code-block grid of one subband.
type bandLayout struct {
	cbX, cbY int
}

// precLayout is the code-block partition of one precinct: one bandLayout per
// band of the resolution the precinct belongs to.
type precLayout struct {
	bands []bandLayout
}

// resLayout is the code-block partition of one resolution of one
// tile-component. A resolution with no samples has no precinct and therefore
// contributes no packet at all (ISO/IEC 15444-1 B.6), which is what present
// records; that is not the same as a resolution whose bands happen to be
// empty, and a decoder that reads a packet for it desynchronises.
type resLayout struct {
	present   bool
	bands     []bandLayout
	precincts []precLayout
}

// tileLayout holds the resolution layouts of every component of one tile.
type tileLayout struct {
	numRes int
	res    []resLayout
	// The tile's own coordinates. The positional progression orders walk image
	// coordinates rather than precinct indices, so the packet writer needs
	// them as much as the reader does.
	x0, y0, x1, y1 int
}

func newTileLayout(numComp, numRes int) *tileLayout {
	return &tileLayout{numRes: numRes, res: make([]resLayout, numComp*numRes)}
}

func (l *tileLayout) at(c, r int) *resLayout { return &l.res[c*l.numRes+r] }

// collectJobs builds the code-block encoding jobs for one tile, along with the
// code-block partition its packet headers have to describe.
//
// comps holds one wavelet-transformed array per component, each the size of a
// tile spanning [x0, x1) x [y0, y1) in image coordinates. Every subband size
// and offset is derived from those absolute coordinates, because that is what
// ISO/IEC 15444-1 B.5 derives them from; passing the whole image reproduces
// the single-tile case exactly. tileBands is the single description of that
// geometry, and the decoder walks the same one.
//
// Exactly one of comps and comps64 is non-nil: comps64 carries a component
// whose coefficients need more than 32 bits, which is the binary32 case. The
// geometry below is shared deliberately — the code-block partition and the
// packet layout must not be able to differ between the two widths.
func (e *encoder) collectJobs(comps [][]int32, comps64 [][]int64, x0, y0, x1, y1 int) ([]codeBlockJob, *tileLayout) {
	numRes := e.numResolutions()
	numComps := len(comps)
	if comps64 != nil {
		numComps = len(comps64)
	}

	// Code-block size, from the same source of truth generateCOD writes into
	// the codestream. If these disagree the decoder partitions the subbands
	// differently and reconstructs garbage.
	cbWidthExp, cbHeightExp := e.codeBlockExponents()
	cbWidth := 1 << cbWidthExp
	cbHeight := 1 << cbHeightExp
	stride := x1 - x0

	layout := newTileLayout(numComps, numRes)
	layout.x0, layout.y0, layout.x1, layout.y1 = x0, y0, x1, y1
	var jobs []codeBlockJob

	// The partition comes from precinctsFor, the same function the decoder
	// reads packet headers against. One definition, walked from both ends:
	// a writer and a reader with their own copies of this arithmetic is how
	// the code-block partition drifted before, and the drift is invisible to a
	// round trip because both ends move together.
	for c := 0; c < numComps; c++ {
		for res := 0; res < numRes; res++ {
			ppx, ppy, _ := e.precinctExpsFor(res)
			precincts := precinctsFor(x0, y0, x1, y1, numRes, res, cbWidth, cbHeight, ppx, ppy)
			if precincts == nil {
				continue
			}
			rl := layout.at(c, res)
			rl.present = true
			rl.precincts = make([]precLayout, len(precincts))

			for p, pbands := range precincts {
				rl.precincts[p].bands = make([]bandLayout, len(pbands))
				for b, bg := range pbands {
					rl.precincts[p].bands[b] = bandLayout{cbX: bg.cbX, cbY: bg.cbY}

					for cby := 0; cby < bg.cbY; cby++ {
						by0 := max(bg.sb.y0, (bg.firstY+cby)*bg.cbH)
						by1 := min(bg.sb.y1, (bg.firstY+cby+1)*bg.cbH)
						for cbx := 0; cbx < bg.cbX; cbx++ {
							bx0 := max(bg.sb.x0, (bg.firstX+cbx)*bg.cbW)
							bx1 := min(bg.sb.x1, (bg.firstX+cbx+1)*bg.cbW)
							w, h := bx1-bx0, by1-by0
							if w <= 0 || h <= 0 {
								continue
							}
							ox := bg.sb.ox + bx0 - bg.sb.x0
							oy := bg.sb.oy + by0 - bg.sb.y0

							var narrow []int32
							var wide []int64
							if comps64 != nil {
								wide = extractCodeBlock(comps64[c], stride, ox, oy, w, h)
							} else {
								narrow = extractCodeBlock(comps[c], stride, ox, oy, w, h)
							}
							jobs = append(jobs, codeBlockJob{
								index:    len(jobs),
								data:     narrow,
								data64:   wide,
								width:    w,
								height:   h,
								bandType: bg.bandType,
								comp:     c,
								res:      res,
								bandIdx:  b,
								prec:     p,
								cbx:      cbx,
								cby:      cby,
							})
						}
					}
				}
			}
			// Kept for callers that still read rl.bands: the whole-resolution
			// view is the first precinct's when there is only one.
			if len(rl.precincts) > 0 {
				rl.bands = rl.precincts[0].bands
			}
		}
	}
	return jobs, layout
}

// encodeJobs runs the block coder over every job, in parallel when there is
// enough work for it, and returns the encoded segments in job order along with
// the magnitude bit count and coding-pass count of each.
func (e *encoder) encodeJobs(jobs []codeBlockJob) ([][]byte, []int, [][]int) {
	encoded := make([][]byte, len(jobs))
	numBPS := make([]int, len(jobs))
	truncPoints := make([][]int, len(jobs))

	// Sequential encoding for small job counts or single-threaded mode.
	// Set GOMAXPROCS=1 to force single-threaded encoding.
	if len(jobs) <= 4 || runtime.GOMAXPROCS(0) == 1 {
		for i, job := range jobs {
			numBPS[i] = jobNumBPS(job)
			encoded[i], truncPoints[i] = e.encodeCodeBlock(job)
		}
		return encoded, numBPS, truncPoints
	}

	// Parallel encoding - use all available cores
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	// Pre-fill job channel before starting workers to reduce contention
	jobChan := make(chan codeBlockJob, len(jobs))
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	resultChan := make(chan codeBlockResult, len(jobs))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				numBPS := jobNumBPS(job)
				// encodeCodeBlock selects the HT or MQ coder to match what the
				// COD/CAP markers declare, and returns copies that stay valid
				// after the coder goes back to its pool.
				encodedCopy, tpCopy := e.encodeCodeBlock(job)
				resultChan <- codeBlockResult{
					index:       job.index,
					encoded:     encodedCopy,
					numBPS:      numBPS,
					truncPoints: tpCopy,
				}
			}
		}()
	}

	// Wait for completion
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results in order
	for result := range resultChan {
		encoded[result.index] = result.encoded
		numBPS[result.index] = result.numBPS
		truncPoints[result.index] = result.truncPoints
	}

	return encoded, numBPS, truncPoints
}

// assembleTileData turns the encoded code-blocks of one tile into the bytes
// that follow its SOD marker.
func (e *encoder) assembleTileData(layout *tileLayout, jobs []codeBlockJob, encoded [][]byte, numBPS []int, truncPoints [][]int) ([]byte, []int) {
	// Conforming T2 packets, verified end to end against both references:
	// OpenJPH decodes the HT output and OpenJPEG the Part 1 MQ output to the
	// exact source samples. Quality layers are real packets too — one per
	// (layer, resolution, component) — rather than the private per-layer
	// length table this encoder used to write.
	return e.buildStandardTileData(layout, jobs, encoded, numBPS, truncPoints)
}

// extractCodeBlock copies one code-block out of a tile-component's
// coefficient array, whose rows are stride samples apart.
func extractCodeBlock[T int32 | int64](data []T, stride, x, y, w, h int) []T {
	if w <= 0 || h <= 0 {
		return nil
	}
	out := make([]T, w*h)
	for row := 0; row < h; row++ {
		src := (y+row)*stride + x
		if src < 0 || src+w > len(data) {
			continue
		}
		copy(out[row*w:(row+1)*w], data[src:src+w])
	}
	return out
}

// createTileHeader creates the tile-part header, optionally carrying the PLT
// segments that list this tile-part's packet lengths.
//
// PLT sits between the SOT segment and SOD, and its bytes count toward Psot,
// so the length has to be computed after the segments exist rather than from
// the tile data alone.
func (e *encoder) createTileHeader(tileIdx int, tileData []byte, pktLens []int) []byte {
	var plt []byte
	if e.options.WritePacketLengths {
		plt = generatePLT(pktLens)
	}

	sotLength := 10
	tilePartLength := uint32(14 + len(plt) + len(tileData))

	header := make([]byte, 12, 14+len(plt))
	binary.BigEndian.PutUint16(header[0:2], uint16(codestream.SOT))
	binary.BigEndian.PutUint16(header[2:4], uint16(sotLength))
	binary.BigEndian.PutUint16(header[4:6], uint16(tileIdx))
	binary.BigEndian.PutUint32(header[6:10], tilePartLength)
	header[10] = 0 // Tile-part index
	header[11] = 1 // Number of tile-parts

	header = append(header, plt...)
	header = binary.BigEndian.AppendUint16(header, uint16(codestream.SOD))

	// Every tile-part's length, in order, is what TLM records.
	e.tilePartIdx = append(e.tilePartIdx, tileIdx)
	e.tilePartLen = append(e.tilePartLen, tilePartLength)

	return append(header, tileData...)
}

// writeJP2 writes a JP2 file.
func (e *encoder) writeJP2(codestream []byte) error {
	boxWriter := box.NewWriter(e.w)

	// Write signature
	if err := boxWriter.WriteSignature(); err != nil {
		return err
	}

	// Write file type box
	ftypBox := box.CreateFileTypeBox()
	if err := boxWriter.WriteBox(ftypBox); err != nil {
		return err
	}

	// Determine colorspace from options or default based on components
	var colorspace uint32
	switch e.options.ColorSpace {
	case ColorSpaceBilevel:
		colorspace = box.CSBilevel1
	case ColorSpaceGray:
		colorspace = box.CSGray
	case ColorSpaceSRGB:
		colorspace = box.CSSRGB
	case ColorSpaceSYCC:
		colorspace = box.CSYCbCr1
	case ColorSpaceYCbCr2:
		colorspace = box.CSYCbCr2
	case ColorSpaceYCbCr3:
		colorspace = box.CSYCbCr3
	case ColorSpacePhotoYCC:
		colorspace = box.CSPhotoYCC
	case ColorSpaceCMY:
		colorspace = box.CSCMY
	case ColorSpaceCMYK:
		colorspace = box.CSCMYK
	case ColorSpaceYCCK:
		colorspace = box.CSYCCK
	case ColorSpaceCIELab:
		colorspace = box.CSCIELab
	case ColorSpaceCIEJab:
		colorspace = box.CSCIEJab
	case ColorSpaceESRGB:
		colorspace = box.CSeSRGB
	case ColorSpaceROMMRGB:
		colorspace = box.CSROMMRGB
	case ColorSpaceYPbPr60:
		colorspace = box.CSYPbPr1125
	case ColorSpaceYPbPr50:
		colorspace = box.CSYPbPr1250
	case ColorSpaceEYCC:
		colorspace = box.CSeSYCC
	default:
		// Default based on number of components
		if e.numComponents == 1 {
			colorspace = box.CSGray
		} else {
			// 3 or 4 components default to sRGB (4th component is alpha)
			colorspace = box.CSSRGB
		}
	}

	// Write JP2 header
	jp2hBox := box.CreateJP2Header(
		uint32(e.width),
		uint32(e.height),
		uint16(e.numComponents),
		uint8(e.maxPrecision()-1),
		colorspace,
	)
	if err := boxWriter.WriteBox(jp2hBox); err != nil {
		return err
	}

	// Write codestream
	jp2cBox := box.CreateCodestreamBox(codestream)
	if err := boxWriter.WriteBox(jp2cBox); err != nil {
		return err
	}

	return nil
}

// encodeCodeBlock encodes one code-block with the block coder that this
// codestream actually declares.
//
// This branch is the whole point: when HighThroughput is set the COD marker
// advertises the HT code-block style, the CAP marker declares Part 15 and Rsiz
// sets bit 14. Emitting Part 1 MQ-coded data under that signalling produces a
// file that only this library can read — every conforming decoder trusts the
// declaration, runs the HT block decoder over MQ bytes, recovers no
// coefficients and yields a flat mid-grey image. Round-trip tests cannot see
// it, because the same wrong choice is made on the way back in.
//
// The returned slice is always a fresh copy, so it stays valid after the coder
// is returned to its pool.
func (e *encoder) encodeCodeBlock(job codeBlockJob) ([]byte, []int) {
	data, width, height, bandType := job.data, job.width, job.height, job.bandType
	if job.data64 != nil {
		// A wide code-block is HT-only: the Part 1 MQ coder in this package
		// works on int32 coefficients, and a binary32 component's do not fit.
		out := entropy.EncodeCleanup64(job.data64, width, height)
		return out, []int{len(out)}
	}
	if e.htCoder() {
		ht := entropy.GetHTEncoder(width, height)
		ht.SetData(data)
		encoded := ht.Encode(bandType)
		out := make([]byte, len(encoded))
		copy(out, encoded)
		entropy.PutHTEncoder(ht)
		// The HT cleanup pass is a single coding pass, so there is one
		// truncation point and it sits at the end of the segment.
		return out, []int{len(out)}
	}

	t1 := entropy.GetT1(width, height)
	t1.SetData(data)
	encoded := t1.Encode(bandType)
	// Copy before pooling: Encode returns a view of the T1's internal mqBuf,
	// and the truncation points live on the T1 itself.
	out := make([]byte, len(encoded))
	copy(out, encoded)
	tp := t1.TruncationPoints()
	tpCopy := make([]int, len(tp))
	copy(tpCopy, tp)
	entropy.PutT1(t1)
	return out, tpCopy
}
