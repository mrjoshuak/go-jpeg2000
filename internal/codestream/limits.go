package codestream

// Structural limits imposed by ISO/IEC 15444-1 on the values a codestream
// header may carry. Every one of these is a value the decoder reads straight
// out of the file and then uses to size an allocation, index a slice, or
// divide, so each is checked before use rather than trusted.
const (
	// MaxComponents is the largest Csiz the standard permits (Table A.9).
	MaxComponents = 16384

	// MaxPrecision is the largest component bit depth (Ssiz+1) the standard
	// permits (Table A.11).
	MaxPrecision = 38

	// MaxDecompositionLevels is the largest SPcod/SPcoc number of wavelet
	// decomposition levels (Table A.20). Anything larger makes the resolution
	// scale factor 1<<n overflow.
	MaxDecompositionLevels = 32

	// MaxCodeBlockExp is the largest xcb-2 / ycb-2 exponent (Table A.18),
	// giving code-block dimensions of at most 1024.
	MaxCodeBlockExp = 8

	// MaxCodeBlockArea is the cap the standard puts on the code-block area
	// (Table A.18).
	MaxCodeBlockArea = 4096

	// MaxCodeBlockExpSum is the cap on (xcb-2)+(ycb-2) that expresses the
	// 4096-sample area limit: 2^(xcb+ycb) <= 4096 with xcb,ycb >= 2.
	MaxCodeBlockExpSum = 8

	// MaxPrecinctExp is the largest PPx/PPy exponent (Table A.21). The parser
	// masks to four bits, so this is a completeness check.
	MaxPrecinctExp = 15

	// MaxTiles is the largest number of tiles an image may be divided into.
	// Isot in the SOT marker is 16 bits, so a tile index above 65534 could
	// never be signalled.
	MaxTiles = 65535

	// MaxDimension bounds Xsiz/Ysiz so that every derived coordinate stays
	// inside a signed 32-bit integer on every platform Go supports.
	MaxDimension = 1<<31 - 1

	// MaxProgressionOrder is the largest defined SGcod progression order
	// (Table A.16): 0=LRCP, 1=RLCP, 2=RPCL, 3=PCRL, 4=CPRL.
	MaxProgressionOrder = 4

	// MaxQuantizationStyle is the largest defined Sqcd/Sqcc style (Table A.28):
	// 0=none, 1=scalar derived, 2=scalar expounded.
	MaxQuantizationStyle = 2

	// MaxBitPlanes bounds the number of magnitude bit-planes a code-block may
	// declare. Coefficients are held in int32, so a bit-plane index of 31 or
	// more cannot contribute anything and only costs decode time.
	MaxBitPlanes = 31
)

// ceilDivU32 computes ceil(a/b) in 64-bit arithmetic so that neither the
// subtraction nor the addition of b-1 can wrap.
func ceilDivU32(a, b uint32) uint64 {
	if b == 0 {
		return 0
	}
	return (uint64(a) + uint64(b) - 1) / uint64(b)
}
