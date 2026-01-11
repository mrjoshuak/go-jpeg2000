// Package entropy implements entropy coding for JPEG 2000.
//
// This includes:
// - MQ coder (arithmetic entropy coder)
// - Context modeling for EBCOT
// - Raw (bypass) mode
package entropy

// mqState represents a state in the MQ coder state machine.
// This follows OpenJPEG's implementation with 94 states (47 * 2),
// where even indices have MPS=0 and odd indices have MPS=1.
type mqState struct {
	Qe   uint32 // Probability estimate (fixed-point, qeval)
	MPS  uint8  // Most probable symbol for this state (0 or 1)
	NMPS uint8  // Next state index if MPS occurs
	NLPS uint8  // Next state index if LPS occurs
}

// MQ coder state table from OpenJPEG (94 states = 47 * 2)
// Even indices have MPS=0, odd indices have MPS=1
var mqStates = []mqState{
	{0x5601, 0, 2, 3},   // 0
	{0x5601, 1, 3, 2},   // 1
	{0x3401, 0, 4, 12},  // 2
	{0x3401, 1, 5, 13},  // 3
	{0x1801, 0, 6, 18},  // 4
	{0x1801, 1, 7, 19},  // 5
	{0x0AC1, 0, 8, 24},  // 6
	{0x0AC1, 1, 9, 25},  // 7
	{0x0521, 0, 10, 58}, // 8
	{0x0521, 1, 11, 59}, // 9
	{0x0221, 0, 76, 66}, // 10
	{0x0221, 1, 77, 67}, // 11
	{0x5601, 0, 14, 13}, // 12
	{0x5601, 1, 15, 12}, // 13
	{0x5401, 0, 16, 28}, // 14
	{0x5401, 1, 17, 29}, // 15
	{0x4801, 0, 18, 28}, // 16
	{0x4801, 1, 19, 29}, // 17
	{0x3801, 0, 20, 28}, // 18
	{0x3801, 1, 21, 29}, // 19
	{0x3001, 0, 22, 34}, // 20
	{0x3001, 1, 23, 35}, // 21
	{0x2401, 0, 24, 36}, // 22
	{0x2401, 1, 25, 37}, // 23
	{0x1C01, 0, 26, 40}, // 24
	{0x1C01, 1, 27, 41}, // 25
	{0x1601, 0, 58, 42}, // 26
	{0x1601, 1, 59, 43}, // 27
	{0x5601, 0, 30, 29}, // 28
	{0x5601, 1, 31, 28}, // 29
	{0x5401, 0, 32, 28}, // 30
	{0x5401, 1, 33, 29}, // 31
	{0x5101, 0, 34, 30}, // 32
	{0x5101, 1, 35, 31}, // 33
	{0x4801, 0, 36, 32}, // 34
	{0x4801, 1, 37, 33}, // 35
	{0x3801, 0, 38, 34}, // 36
	{0x3801, 1, 39, 35}, // 37
	{0x3401, 0, 40, 36}, // 38
	{0x3401, 1, 41, 37}, // 39
	{0x3001, 0, 42, 38}, // 40
	{0x3001, 1, 43, 39}, // 41
	{0x2801, 0, 44, 38}, // 42
	{0x2801, 1, 45, 39}, // 43
	{0x2401, 0, 46, 40}, // 44
	{0x2401, 1, 47, 41}, // 45
	{0x2201, 0, 48, 42}, // 46
	{0x2201, 1, 49, 43}, // 47
	{0x1C01, 0, 50, 44}, // 48
	{0x1C01, 1, 51, 45}, // 49
	{0x1801, 0, 52, 46}, // 50
	{0x1801, 1, 53, 47}, // 51
	{0x1601, 0, 54, 48}, // 52
	{0x1601, 1, 55, 49}, // 53
	{0x1401, 0, 56, 50}, // 54
	{0x1401, 1, 57, 51}, // 55
	{0x1201, 0, 58, 52}, // 56
	{0x1201, 1, 59, 53}, // 57
	{0x1101, 0, 60, 54}, // 58
	{0x1101, 1, 61, 55}, // 59
	{0x0AC1, 0, 62, 56}, // 60
	{0x0AC1, 1, 63, 57}, // 61
	{0x09C1, 0, 64, 58}, // 62
	{0x09C1, 1, 65, 59}, // 63
	{0x08A1, 0, 66, 60}, // 64
	{0x08A1, 1, 67, 61}, // 65
	{0x0521, 0, 68, 62}, // 66
	{0x0521, 1, 69, 63}, // 67
	{0x0441, 0, 70, 64}, // 68
	{0x0441, 1, 71, 65}, // 69
	{0x02A1, 0, 72, 66}, // 70
	{0x02A1, 1, 73, 67}, // 71
	{0x0221, 0, 74, 68}, // 72
	{0x0221, 1, 75, 69}, // 73
	{0x0141, 0, 76, 70}, // 74
	{0x0141, 1, 77, 71}, // 75
	{0x0111, 0, 78, 72}, // 76
	{0x0111, 1, 79, 73}, // 77
	{0x0085, 0, 80, 74}, // 78
	{0x0085, 1, 81, 75}, // 79
	{0x0049, 0, 82, 76}, // 80
	{0x0049, 1, 83, 77}, // 81
	{0x0025, 0, 84, 78}, // 82
	{0x0025, 1, 85, 79}, // 83
	{0x0015, 0, 86, 80}, // 84
	{0x0015, 1, 87, 81}, // 85
	{0x0009, 0, 88, 82}, // 86
	{0x0009, 1, 89, 83}, // 87
	{0x0005, 0, 90, 84}, // 88
	{0x0005, 1, 91, 85}, // 89
	{0x0001, 0, 90, 86}, // 90
	{0x0001, 1, 91, 87}, // 91
	{0x5601, 0, 92, 92}, // 92 - Uniform context (MPS=0)
	{0x5601, 1, 93, 93}, // 93 - Uniform context (MPS=1)
}

// Flat arrays for faster access (cache-friendly)
// These are indexed directly by state number without struct field access.
var (
	mqQe   [94]uint32 // Probability estimates
	mqNMPS [94]uint8  // Next state for MPS
	mqNLPS [94]uint8  // Next state for LPS
)

func init() {
	for i, s := range mqStates {
		mqQe[i] = s.Qe
		mqNMPS[i] = s.NMPS
		mqNLPS[i] = s.NLPS
	}
}

// Context indices for EBCOT coding passes.
const (
	// Zero coding contexts (9 contexts based on neighbors)
	CtxZC0 = iota // LL band
	CtxZC1
	CtxZC2
	CtxZC3
	CtxZC4
	CtxZC5
	CtxZC6
	CtxZC7
	CtxZC8

	// Sign coding contexts (5 contexts)
	CtxSC0
	CtxSC1
	CtxSC2
	CtxSC3
	CtxSC4

	// Magnitude refinement contexts (3 contexts)
	CtxMag0
	CtxMag1
	CtxMag2

	// Run-length context
	CtxRL

	// Uniform context
	CtxUni

	NumContexts // Total number of contexts
)

// MQEncoder implements the MQ arithmetic encoder.
type MQEncoder struct {
	// Interval size (A register)
	A uint32
	// Code register (C register)
	C uint32
	// Bit counter
	CT uint32
	// Output buffer
	buf []byte
	// Buffer position (index of last written byte)
	bp int
	// Context states - each context holds an index into mqStates
	contexts [NumContexts]uint8
}

// NewMQEncoder creates a new MQ encoder.
func NewMQEncoder() *MQEncoder {
	e := &MQEncoder{
		A:   0x8000,
		C:   0,
		CT:  12,
		buf: make([]byte, 1, 8192), // Pre-allocate 8KB for output
		bp:  0,
	}
	e.buf[0] = 0 // Initial byte (bp[-1] in OpenJPEG terms)
	// Initialize all contexts to state 0 (MPS=0)
	for i := range e.contexts {
		e.contexts[i] = 0
	}
	// Set uniform context to state 92 (MPS=0)
	e.contexts[CtxUni] = 92
	return e
}

// Reset resets the encoder state.
func (e *MQEncoder) Reset() {
	e.A = 0x8000
	e.C = 0
	e.CT = 12
	// Reuse buffer capacity, just reset length
	if cap(e.buf) > 0 {
		e.buf = e.buf[:1]
	} else {
		e.buf = make([]byte, 1, 8192)
	}
	e.buf[0] = 0
	e.bp = 0
	for i := range e.contexts {
		e.contexts[i] = 0
	}
	e.contexts[CtxUni] = 92
}

// Encode encodes a binary decision (0 or 1) for the given context.
// Optimized: uses flat arrays and inlines MPS/LPS handling.
func (e *MQEncoder) Encode(ctx int, decision int) {
	stateIdx := e.contexts[ctx]
	qe := mqQe[stateIdx]
	// MPS is determined by state index: even = 0, odd = 1
	mps := stateIdx & 1

	e.A -= qe

	if uint8(decision) == mps {
		// MPS path (most probable symbol)
		if (e.A & 0x8000) == 0 {
			if e.A < qe {
				e.A = qe
			} else {
				e.C += qe
			}
			e.contexts[ctx] = mqNMPS[stateIdx]
			e.renormEnc()
		} else {
			e.C += qe
		}
	} else {
		// LPS path (least probable symbol)
		if e.A < qe {
			e.C += qe
		} else {
			e.A = qe
		}
		e.contexts[ctx] = mqNLPS[stateIdx]
		e.renormEnc()
	}
}

// renormEnc performs encoder interval renormalization.
func (e *MQEncoder) renormEnc() {
	for (e.A & 0x8000) == 0 {
		e.A <<= 1
		e.C <<= 1
		e.CT--
		if e.CT == 0 {
			e.byteOut()
		}
	}
}

// byteOut outputs a byte with bit stuffing.
func (e *MQEncoder) byteOut() {
	if e.buf[e.bp] == 0xFF {
		e.bp++
		if e.bp >= len(e.buf) {
			e.buf = append(e.buf, 0)
		}
		e.buf[e.bp] = byte(e.C >> 20)
		e.C &= 0xFFFFF
		e.CT = 7
	} else {
		if (e.C & 0x8000000) == 0 {
			e.bp++
			if e.bp >= len(e.buf) {
				e.buf = append(e.buf, 0)
			}
			e.buf[e.bp] = byte(e.C >> 19)
			e.C &= 0x7FFFF
			e.CT = 8
		} else {
			e.buf[e.bp]++
			if e.buf[e.bp] == 0xFF {
				e.C &= 0x7FFFFFF
				e.bp++
				if e.bp >= len(e.buf) {
					e.buf = append(e.buf, 0)
				}
				e.buf[e.bp] = byte(e.C >> 20)
				e.C &= 0xFFFFF
				e.CT = 7
			} else {
				e.bp++
				if e.bp >= len(e.buf) {
					e.buf = append(e.buf, 0)
				}
				e.buf[e.bp] = byte(e.C >> 19)
				e.C &= 0x7FFFF
				e.CT = 8
			}
		}
	}
}

// Flush finalizes the encoding and returns the compressed data.
func (e *MQEncoder) Flush() []byte {
	// C.2.9 Termination of coding (FLUSH)
	e.setbits()
	e.C <<= e.CT
	e.byteOut()
	e.C <<= e.CT
	e.byteOut()

	// Don't include trailing 0xFF
	endPos := e.bp + 1
	if endPos > 0 && e.buf[endPos-1] == 0xFF {
		endPos--
	}

	// Skip the initial dummy byte
	if endPos > 1 {
		return e.buf[1:endPos]
	}
	return nil
}

// setbits sets remaining bits for flushing.
func (e *MQEncoder) setbits() {
	tempC := e.C + e.A
	e.C |= 0xFFFF
	if e.C >= tempC {
		e.C -= 0x8000
	}
}

// Bytes returns the current encoded data (without flushing).
func (e *MQEncoder) Bytes() []byte {
	if e.bp > 0 {
		return e.buf[1 : e.bp+1]
	}
	return nil
}

// MQDecoder implements the MQ arithmetic decoder.
type MQDecoder struct {
	// Code register
	C uint32
	// Interval size
	A uint32
	// Bit counter
	CT uint32
	// Input buffer position
	bp int
	// Input data
	data []byte
	// Context states - each context holds an index into mqStates
	contexts [NumContexts]uint8
	// End of byte stream counter
	endCounter int
}

// NewMQDecoder creates a new MQ decoder.
func NewMQDecoder(data []byte) *MQDecoder {
	d := &MQDecoder{
		A:    0x8000,
		C:    0,
		CT:   0,
		data: data,
		bp:   -1,
	}
	// Initialize all contexts to state 0 (MPS=0)
	for i := range d.contexts {
		d.contexts[i] = 0
	}
	// Set uniform context to state 92 (MPS=0)
	d.contexts[CtxUni] = 92

	// Initialize C register (INITDEC procedure)
	// C.3.5 Initialization of the decoder
	if len(data) == 0 {
		d.C = 0xFF << 16
	} else {
		d.bp = 0
		d.C = uint32(data[0]) << 16
	}
	d.byteIn()
	d.C <<= 7
	d.CT -= 7
	d.A = 0x8000

	return d
}

// byteIn reads a byte with bit stuffing handling.
func (d *MQDecoder) byteIn() {
	if d.bp < 0 {
		d.bp = 0
	}

	// Check if we're past the end
	if d.bp >= len(d.data) {
		d.C += 0xFF00
		d.CT = 8
		d.endCounter++
		return
	}

	// Get next byte
	var nextByte byte
	if d.bp+1 < len(d.data) {
		nextByte = d.data[d.bp+1]
	} else {
		nextByte = 0xFF
	}

	if d.data[d.bp] == 0xFF {
		if nextByte > 0x8F {
			// Marker - don't advance
			d.C += 0xFF00
			d.CT = 8
			d.endCounter++
		} else {
			d.bp++
			d.C += uint32(nextByte) << 9
			d.CT = 7
		}
	} else {
		d.bp++
		d.C += uint32(nextByte) << 8
		d.CT = 8
	}
}

// Decode decodes a binary decision for the given context.
// Optimized: uses flat arrays and inlines exchange handling.
func (d *MQDecoder) Decode(ctx int) int {
	stateIdx := d.contexts[ctx]
	qe := mqQe[stateIdx]
	mps := int(stateIdx & 1)

	d.A -= qe

	if (d.C >> 16) < qe {
		// Upper (LPS) sub-interval
		var decision int
		if d.A < qe {
			// Conditional exchange: actually MPS
			d.A = qe
			decision = mps
			d.contexts[ctx] = mqNMPS[stateIdx]
		} else {
			// LPS
			d.A = qe
			decision = 1 - mps
			d.contexts[ctx] = mqNLPS[stateIdx]
		}
		d.renormDec()
		return decision
	}

	// Lower (MPS) sub-interval
	d.C -= qe << 16
	if (d.A & 0x8000) == 0 {
		var decision int
		if d.A < qe {
			// Conditional exchange: actually LPS
			decision = 1 - mps
			d.contexts[ctx] = mqNLPS[stateIdx]
		} else {
			// MPS
			decision = mps
			d.contexts[ctx] = mqNMPS[stateIdx]
		}
		d.renormDec()
		return decision
	}
	return mps
}

// renormDec performs decoder interval renormalization.
func (d *MQDecoder) renormDec() {
	for (d.A & 0x8000) == 0 {
		if d.CT == 0 {
			d.byteIn()
		}
		d.A <<= 1
		d.C <<= 1
		d.CT--
	}
}

// ResetContext resets a specific context to its initial state.
func (d *MQDecoder) ResetContext(ctx int) {
	if ctx == CtxUni {
		d.contexts[ctx] = 92
	} else {
		d.contexts[ctx] = 0
	}
}

// ResetAllContexts resets all contexts to their initial states.
func (d *MQDecoder) ResetAllContexts() {
	for i := range d.contexts {
		d.contexts[i] = 0
	}
	d.contexts[CtxUni] = 92
}

// RawDecoder implements raw (bypass) mode decoding.
type RawDecoder struct {
	data []byte
	pos  int
	c    byte
	ct   int
}

// NewRawDecoder creates a new raw decoder.
func NewRawDecoder(data []byte) *RawDecoder {
	return &RawDecoder{
		data: data,
		pos:  0,
		c:    0,
		ct:   0,
	}
}

// DecodeBit decodes a single bit in raw mode.
func (r *RawDecoder) DecodeBit() int {
	if r.ct == 0 {
		if r.c == 0xFF {
			if r.pos < len(r.data) && r.data[r.pos] > 0x8F {
				r.c = 0xFF
				r.ct = 8
			} else if r.pos < len(r.data) {
				r.c = r.data[r.pos]
				r.pos++
				r.ct = 7
			} else {
				r.c = 0xFF
				r.ct = 8
			}
		} else {
			if r.pos < len(r.data) {
				r.c = r.data[r.pos]
				r.pos++
				r.ct = 8
			} else {
				r.c = 0xFF
				r.ct = 8
			}
		}
	}
	r.ct--
	return int((r.c >> r.ct) & 1)
}

// RawEncoder implements raw (bypass) mode encoding.
type RawEncoder struct {
	buf []byte
	c   uint32
	ct  int
}

// NewRawEncoder creates a new raw encoder.
func NewRawEncoder() *RawEncoder {
	return &RawEncoder{
		buf: make([]byte, 0, 64),
		c:   0,
		ct:  8,
	}
}

// EncodeBit encodes a single bit in raw mode.
func (r *RawEncoder) EncodeBit(bit int) {
	r.ct--
	r.c = r.c + (uint32(bit&1) << r.ct)
	if r.ct == 0 {
		r.buf = append(r.buf, byte(r.c))
		r.ct = 8
		if byte(r.c) == 0xFF {
			r.ct = 7
		}
		r.c = 0
	}
}

// Flush flushes remaining bits and returns the data.
func (r *RawEncoder) Flush() []byte {
	if r.ct < 8 {
		r.buf = append(r.buf, byte(r.c))
	}
	return r.buf
}
