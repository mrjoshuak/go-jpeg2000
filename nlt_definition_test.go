package jpeg2000

import "testing"

// TestNLTType3MatchesTheDefinition pins nltType3 to fixed vectors instead of to
// itself.
//
// nltType3 is one function used by both the encoder and the decoder, and it is
// an involution: applying it twice returns the input whatever mask it uses. So
// every round trip through this library passes with a *wrong* mask too — the
// two directions are wrong identically, and the values on the wire simply mean
// something else to every other reader. That is the exact defect shape that hid
// the ACES matrices, the floatvector count prefix and the chroma encoding in
// the sibling repository, and no round-trip test can see it by construction.
//
// The vectors below are literals: v maps to v XOR (2^(p-1) - 1) when v is
// negative and to itself otherwise. That behaviour is what OpenJPEG and OpenJPH
// read and write byte-for-byte — the gate's float and half rows against both
// oracles are the external anchor — so pinning it here turns "the oracles agree
// with us today" into a check that fails the moment the mask moves.
func TestNLTType3MatchesTheDefinition(t *testing.T) {
	cases := []struct {
		precision int
		in, out   int32
	}{
		// Non-negative values pass through at every precision.
		{16, 0, 0},
		{16, 5, 5},
		{16, 32767, 32767},
		{32, 123456789, 123456789},

		// p=16: mask is 0x7FFF.
		{16, -1, -32768}, // -1 ^ 0x7FFF
		{16, -32768, -1}, // the other end of the involution
		{16, -2, -32767}, // -2 ^ 0x7FFF
		{16, -32767, -2},

		// p=32: mask is 0x7FFFFFFF.
		{32, -1, -2147483648},
		{32, -2147483648, -1},
		{32, -2, -2147483647},
	}

	for _, c := range cases {
		data := []int32{c.in}
		nltType3(data, c.precision)
		if data[0] != c.out {
			t.Errorf("nltType3(%d, p=%d) = %d, want %d; the mask is 2^(p-1)-1 and "+
				"both codec directions share this function, so a round trip cannot "+
				"catch a wrong one", c.in, c.precision, data[0], c.out)
		}
	}

	// The involution property, separately from the vectors: applying the
	// transform twice must return the input. This is what a wrong mask still
	// satisfies — it is here as the control showing why the vectors above are
	// the load-bearing half.
	vals := []int32{0, 1, -1, 42, -42, 32767, -32768}
	data := append([]int32(nil), vals...)
	nltType3(data, 16)
	nltType3(data, 16)
	for i, v := range vals {
		if data[i] != v {
			t.Errorf("nltType3 applied twice changed %d to %d; it must be an involution",
				v, data[i])
		}
	}
}
