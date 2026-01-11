// Package bio provides bit-level I/O for JPEG 2000 codestreams.
package bio

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// errWriter is an io.Writer that always returns an error after n writes.
type errWriter struct {
	n   int
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.n <= 0 {
		return 0, e.err
	}
	e.n--
	return len(p), nil
}

// limitedReader limits how many bytes can be read.
type limitedReader struct {
	data []byte
	pos  int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.pos >= len(l.data) {
		return 0, io.EOF
	}
	n := copy(p, l.data[l.pos:])
	l.pos += n
	return n, nil
}

// =============================================================================
// Reader tests
// =============================================================================

func TestNewReader(t *testing.T) {
	buf := bytes.NewReader([]byte{0xAB})
	r := NewReader(buf)
	if r == nil {
		t.Fatal("NewReader returned nil")
	}
	if r.r != buf {
		t.Error("Reader.r not set correctly")
	}
	if r.cnt != 0 {
		t.Errorf("Reader.cnt = %d, want 0", r.cnt)
	}
}

func TestReader_ReadBit(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []int
	}{
		{
			name:     "single byte all zeros",
			data:     []byte{0x00},
			expected: []int{0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name:     "single byte all ones",
			data:     []byte{0xFF},
			expected: []int{1, 1, 1, 1, 1, 1, 1, 1},
		},
		{
			name:     "alternating bits 10101010",
			data:     []byte{0xAA},
			expected: []int{1, 0, 1, 0, 1, 0, 1, 0},
		},
		{
			name:     "alternating bits 01010101",
			data:     []byte{0x55},
			expected: []int{0, 1, 0, 1, 0, 1, 0, 1},
		},
		{
			name:     "high nibble set",
			data:     []byte{0xF0},
			expected: []int{1, 1, 1, 1, 0, 0, 0, 0},
		},
		{
			name:     "low nibble set",
			data:     []byte{0x0F},
			expected: []int{0, 0, 0, 0, 1, 1, 1, 1},
		},
		{
			name:     "multiple bytes",
			data:     []byte{0x80, 0x01},
			expected: []int{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReader(bytes.NewReader(tt.data))
			for i, want := range tt.expected {
				got, err := r.ReadBit()
				if err != nil {
					t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
				}
				if got != want {
					t.Errorf("ReadBit() at position %d = %d, want %d", i, got, want)
				}
			}
		})
	}
}

func TestReader_ReadBit_EOF(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{}))
	_, err := r.ReadBit()
	if err == nil {
		t.Error("ReadBit() on empty reader should return error")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ReadBit() error = %v, want EOF or ErrUnexpectedEOF", err)
	}
}

func TestReader_ReadBit_EOFMidStream(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0xFF}))
	// Read all 8 bits successfully
	for i := 0; i < 8; i++ {
		_, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
		}
	}
	// Next read should fail with EOF
	_, err := r.ReadBit()
	if err == nil {
		t.Error("ReadBit() after exhausting buffer should return error")
	}
}

func TestReader_ReadBits(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		n        uint
		expected uint32
	}{
		{
			name:     "read 1 bit",
			data:     []byte{0x80},
			n:        1,
			expected: 1,
		},
		{
			name:     "read 4 bits",
			data:     []byte{0xF0},
			n:        4,
			expected: 0x0F,
		},
		{
			name:     "read 8 bits",
			data:     []byte{0xAB},
			n:        8,
			expected: 0xAB,
		},
		{
			name:     "read 16 bits",
			data:     []byte{0xAB, 0xCD},
			n:        16,
			expected: 0xABCD,
		},
		{
			name:     "read 24 bits",
			data:     []byte{0x12, 0x34, 0x56},
			n:        24,
			expected: 0x123456,
		},
		{
			name:     "read 32 bits",
			data:     []byte{0x12, 0x34, 0x56, 0x78},
			n:        32,
			expected: 0x12345678,
		},
		{
			name:     "read 12 bits crossing byte boundary",
			data:     []byte{0xAB, 0xCD},
			n:        12,
			expected: 0xABC,
		},
		{
			name:     "read 0 bits",
			data:     []byte{0xFF},
			n:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReader(bytes.NewReader(tt.data))
			got, err := r.ReadBits(tt.n)
			if err != nil {
				t.Fatalf("ReadBits(%d) returned error: %v", tt.n, err)
			}
			if got != tt.expected {
				t.Errorf("ReadBits(%d) = 0x%X, want 0x%X", tt.n, got, tt.expected)
			}
		})
	}
}

func TestReader_ReadBits_EOF(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0xFF}))
	// Try to read more bits than available
	_, err := r.ReadBits(16)
	if err == nil {
		t.Error("ReadBits() should return error when not enough bits available")
	}
}

func TestReader_Align(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0xFF, 0xAA}))

	// Read 3 bits
	for i := 0; i < 3; i++ {
		_, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() returned error: %v", err)
		}
	}

	// Align should discard remaining 5 bits
	r.Align()

	// Next read should get the first bit of second byte (0xAA = 10101010)
	bit, err := r.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit() after Align returned error: %v", err)
	}
	if bit != 1 {
		t.Errorf("ReadBit() after Align = %d, want 1", bit)
	}
}

func TestReader_Align_AtByteBoundary(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0xFF, 0x00}))

	// Read all 8 bits
	for i := 0; i < 8; i++ {
		_, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() returned error: %v", err)
		}
	}

	// Align at byte boundary should be a no-op
	r.Align()

	// Next read should get first bit of second byte
	bit, err := r.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit() after Align returned error: %v", err)
	}
	if bit != 0 {
		t.Errorf("ReadBit() after Align = %d, want 0", bit)
	}
}

// =============================================================================
// Writer tests
// =============================================================================

func TestNewWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	if w == nil {
		t.Fatal("NewWriter returned nil")
	}
	if w.w != buf {
		t.Error("Writer.w not set correctly")
	}
}

func TestWriter_WriteBit(t *testing.T) {
	tests := []struct {
		name     string
		bits     []int
		expected []byte
	}{
		{
			name:     "all zeros",
			bits:     []int{0, 0, 0, 0, 0, 0, 0, 0},
			expected: []byte{0x00},
		},
		{
			name:     "all ones",
			bits:     []int{1, 1, 1, 1, 1, 1, 1, 1},
			expected: []byte{0xFF},
		},
		{
			name:     "alternating 10101010",
			bits:     []int{1, 0, 1, 0, 1, 0, 1, 0},
			expected: []byte{0xAA},
		},
		{
			name:     "alternating 01010101",
			bits:     []int{0, 1, 0, 1, 0, 1, 0, 1},
			expected: []byte{0x55},
		},
		{
			name:     "16 bits",
			bits:     []int{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			expected: []byte{0x80, 0x01},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewWriter(buf)
			for i, bit := range tt.bits {
				if err := w.WriteBit(bit); err != nil {
					t.Fatalf("WriteBit(%d) at position %d returned error: %v", bit, i, err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush() returned error: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("Output = %v, want %v", buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestWriter_WriteBit_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewWriter(&errWriter{n: 0, err: testErr})

	// Write 8 bits to trigger a flush
	for i := 0; i < 7; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() at position %d returned error unexpectedly: %v", i, err)
		}
	}
	// 8th bit should trigger error on flush
	err := w.WriteBit(1)
	if !errors.Is(err, testErr) {
		t.Errorf("WriteBit() error = %v, want %v", err, testErr)
	}
}

func TestWriter_WriteBits(t *testing.T) {
	tests := []struct {
		name     string
		val      uint32
		n        uint
		expected []byte
	}{
		{
			name:     "write 1 bit",
			val:      1,
			n:        1,
			expected: []byte{0x80},
		},
		{
			name:     "write 4 bits",
			val:      0x0F,
			n:        4,
			expected: []byte{0xF0},
		},
		{
			name:     "write 8 bits",
			val:      0xAB,
			n:        8,
			expected: []byte{0xAB},
		},
		{
			name:     "write 16 bits",
			val:      0xABCD,
			n:        16,
			expected: []byte{0xAB, 0xCD},
		},
		{
			name:     "write 32 bits",
			val:      0x12345678,
			n:        32,
			expected: []byte{0x12, 0x34, 0x56, 0x78},
		},
		{
			name:     "write 12 bits",
			val:      0xABC,
			n:        12,
			expected: []byte{0xAB, 0xC0},
		},
		{
			name:     "write 0 bits",
			val:      0xFF,
			n:        0,
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewWriter(buf)
			if err := w.WriteBits(tt.val, tt.n); err != nil {
				t.Fatalf("WriteBits(0x%X, %d) returned error: %v", tt.val, tt.n, err)
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush() returned error: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("Output = %v, want %v", buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestWriter_WriteBits_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewWriter(&errWriter{n: 0, err: testErr})

	// Write enough bits to trigger flush
	err := w.WriteBits(0xFF, 8)
	if !errors.Is(err, testErr) {
		t.Errorf("WriteBits() error = %v, want %v", err, testErr)
	}
}

func TestWriter_Flush(t *testing.T) {
	tests := []struct {
		name      string
		bits      []int
		expected  []byte
	}{
		{
			name:     "flush partial byte with 1 bit",
			bits:     []int{1},
			expected: []byte{0x80},
		},
		{
			name:     "flush partial byte with 4 bits",
			bits:     []int{1, 0, 1, 0},
			expected: []byte{0xA0},
		},
		{
			name:     "flush partial byte with 7 bits",
			bits:     []int{1, 0, 1, 0, 1, 0, 1},
			expected: []byte{0xAA},
		},
		{
			name:     "flush complete byte (no padding needed)",
			bits:     []int{1, 0, 1, 0, 1, 0, 1, 0},
			expected: []byte{0xAA},
		},
		{
			name:     "flush empty (nothing written)",
			bits:     []int{},
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewWriter(buf)
			for _, bit := range tt.bits {
				if err := w.WriteBit(bit); err != nil {
					t.Fatalf("WriteBit() returned error: %v", err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush() returned error: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("Output = %v, want %v", buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestWriter_Flush_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewWriter(&errWriter{n: 0, err: testErr})

	// Write less than 8 bits and flush
	if err := w.WriteBit(1); err != nil {
		t.Fatalf("WriteBit() returned error: %v", err)
	}
	err := w.Flush()
	if !errors.Is(err, testErr) {
		t.Errorf("Flush() error = %v, want %v", err, testErr)
	}
}

func TestWriter_Flush_NoOp(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf)

	// Flush without writing anything should be no-op
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Flush() with no bits wrote %d bytes, want 0", buf.Len())
	}
}

// =============================================================================
// Round-trip tests for basic Reader/Writer
// =============================================================================

func TestRoundTrip_SingleBits(t *testing.T) {
	original := []int{1, 0, 1, 1, 0, 0, 1, 0, 1, 1, 1, 0, 0, 0, 1, 1}

	// Write
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	for _, bit := range original {
		if err := w.WriteBit(bit); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// Read back
	r := NewReader(bytes.NewReader(buf.Bytes()))
	for i, want := range original {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
		}
		if got != want {
			t.Errorf("ReadBit() at position %d = %d, want %d", i, got, want)
		}
	}
}

func TestRoundTrip_MultipleBits(t *testing.T) {
	tests := []struct {
		val uint32
		n   uint
	}{
		{0x0, 1},
		{0x1, 1},
		{0xF, 4},
		{0xFF, 8},
		{0xABCD, 16},
		{0x123456, 24},
		{0x12345678, 32},
	}

	for _, tt := range tests {
		// Write
		buf := &bytes.Buffer{}
		w := NewWriter(buf)
		if err := w.WriteBits(tt.val, tt.n); err != nil {
			t.Fatalf("WriteBits(0x%X, %d) returned error: %v", tt.val, tt.n, err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush() returned error: %v", err)
		}

		// Read back
		r := NewReader(bytes.NewReader(buf.Bytes()))
		got, err := r.ReadBits(tt.n)
		if err != nil {
			t.Fatalf("ReadBits(%d) returned error: %v", tt.n, err)
		}
		if got != tt.val {
			t.Errorf("ReadBits(%d) = 0x%X, want 0x%X", tt.n, got, tt.val)
		}
	}
}

func TestRoundTrip_MixedBitLengths(t *testing.T) {
	type item struct {
		val uint32
		n   uint
	}
	items := []item{
		{1, 1},      // 1 bit
		{5, 3},      // 3 bits
		{0xAB, 8},   // 8 bits
		{0x3, 2},    // 2 bits
		{0x1234, 16}, // 16 bits
		{7, 5},      // 5 bits
	}

	// Write
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	for _, it := range items {
		if err := w.WriteBits(it.val, it.n); err != nil {
			t.Fatalf("WriteBits(0x%X, %d) returned error: %v", it.val, it.n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// Read back
	r := NewReader(bytes.NewReader(buf.Bytes()))
	for i, it := range items {
		got, err := r.ReadBits(it.n)
		if err != nil {
			t.Fatalf("ReadBits(%d) at index %d returned error: %v", it.n, i, err)
		}
		if got != it.val {
			t.Errorf("ReadBits(%d) at index %d = 0x%X, want 0x%X", it.n, i, got, it.val)
		}
	}
}

// =============================================================================
// ByteStuffingReader tests
// =============================================================================

func TestNewByteStuffingReader(t *testing.T) {
	buf := bytes.NewReader([]byte{0xAB})
	r := NewByteStuffingReader(buf)
	if r == nil {
		t.Fatal("NewByteStuffingReader returned nil")
	}
}

func TestByteStuffingReader_ReadBit_NoStuffing(t *testing.T) {
	// No 0xFF bytes, should read all 8 bits per byte
	data := []byte{0xAA, 0x55}
	r := NewByteStuffingReader(bytes.NewReader(data))

	expected := []int{
		// 0xAA = 10101010
		1, 0, 1, 0, 1, 0, 1, 0,
		// 0x55 = 01010101
		0, 1, 0, 1, 0, 1, 0, 1,
	}

	for i, want := range expected {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
		}
		if got != want {
			t.Errorf("ReadBit() at position %d = %d, want %d", i, got, want)
		}
	}
}

func TestByteStuffingReader_ReadBit_WithStuffing(t *testing.T) {
	// After 0xFF, next byte has only 7 bits
	// 0xFF followed by 0x7F (01111111), only 7 bits count
	data := []byte{0xFF, 0x7F}
	r := NewByteStuffingReader(bytes.NewReader(data))

	// First byte: 0xFF = 11111111 (8 bits)
	for i := 0; i < 8; i++ {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
		}
		if got != 1 {
			t.Errorf("ReadBit() at position %d = %d, want 1", i, got)
		}
	}

	// Second byte: 0x7F = 01111111, but only 7 bits (MSB ignored due to byte stuffing)
	// So we read bits 6-0: 1111111
	for i := 8; i < 15; i++ {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() at position %d returned error: %v", i, err)
		}
		if got != 1 {
			t.Errorf("ReadBit() at position %d = %d, want 1", i, got)
		}
	}
}

func TestByteStuffingReader_ReadBit_StuffingSequence(t *testing.T) {
	// Test consecutive 0xFF bytes
	// Each 0xFF should be followed by only 7 bits from next byte
	data := []byte{0xFF, 0x80, 0xFF, 0x40}
	r := NewByteStuffingReader(bytes.NewReader(data))

	// 0xFF: 8 bits all 1s
	for i := 0; i < 8; i++ {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("Position %d: %v", i, err)
		}
		if got != 1 {
			t.Errorf("Position %d = %d, want 1", i, got)
		}
	}

	// 0x80: After 0xFF, only 7 bits. 0x80 = 10000000, reading bits 6-0 = 0000000
	for i := 8; i < 15; i++ {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("Position %d: %v", i, err)
		}
		if got != 0 {
			t.Errorf("Position %d = %d, want 0", i, got)
		}
	}

	// 0xFF: Not after another 0xFF (previous was 0x80), so 8 bits
	for i := 15; i < 23; i++ {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("Position %d: %v", i, err)
		}
		if got != 1 {
			t.Errorf("Position %d = %d, want 1", i, got)
		}
	}

	// 0x40: After 0xFF, only 7 bits. 0x40 = 01000000, reading bits 6-0 = 1000000
	expected := []int{1, 0, 0, 0, 0, 0, 0}
	for i, want := range expected {
		got, err := r.ReadBit()
		if err != nil {
			t.Fatalf("Position %d: %v", 23+i, err)
		}
		if got != want {
			t.Errorf("Position %d = %d, want %d", 23+i, got, want)
		}
	}
}

func TestByteStuffingReader_ReadBit_EOF(t *testing.T) {
	r := NewByteStuffingReader(bytes.NewReader([]byte{}))
	_, err := r.ReadBit()
	if err == nil {
		t.Error("ReadBit() on empty reader should return error")
	}
}

func TestByteStuffingReader_ReadBits(t *testing.T) {
	data := []byte{0xAB, 0xCD}
	r := NewByteStuffingReader(bytes.NewReader(data))

	got, err := r.ReadBits(16)
	if err != nil {
		t.Fatalf("ReadBits(16) returned error: %v", err)
	}
	if got != 0xABCD {
		t.Errorf("ReadBits(16) = 0x%X, want 0xABCD", got)
	}
}

func TestByteStuffingReader_ReadBits_WithStuffing(t *testing.T) {
	// 0xFF followed by 0x00
	data := []byte{0xFF, 0x00}
	r := NewByteStuffingReader(bytes.NewReader(data))

	// Read 8 bits from first byte (0xFF = 11111111)
	got, err := r.ReadBits(8)
	if err != nil {
		t.Fatalf("ReadBits(8) returned error: %v", err)
	}
	if got != 0xFF {
		t.Errorf("ReadBits(8) = 0x%X, want 0xFF", got)
	}

	// Read 7 bits from second byte (0x00, only 7 bits due to stuffing)
	got, err = r.ReadBits(7)
	if err != nil {
		t.Fatalf("ReadBits(7) returned error: %v", err)
	}
	if got != 0x00 {
		t.Errorf("ReadBits(7) = 0x%X, want 0x00", got)
	}
}

func TestByteStuffingReader_ReadBits_EOF(t *testing.T) {
	r := NewByteStuffingReader(bytes.NewReader([]byte{0x00}))
	_, err := r.ReadBits(16)
	if err == nil {
		t.Error("ReadBits() should return error when not enough bits available")
	}
}

func TestByteStuffingReader_Align(t *testing.T) {
	// Test with a non-FF first byte so sawFF doesn't affect alignment
	data := []byte{0xAA, 0x55}
	r := NewByteStuffingReader(bytes.NewReader(data))

	// Read 3 bits from first byte (0xAA = 10101010): 1, 0, 1
	for i := 0; i < 3; i++ {
		_, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() returned error: %v", err)
		}
	}

	// Align - discards remaining 5 bits
	r.Align()

	// Next read should get first bit of second byte
	bit, err := r.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit() after Align returned error: %v", err)
	}
	// 0x55 = 01010101, first bit is 0
	if bit != 0 {
		t.Errorf("ReadBit() after Align = %d, want 0", bit)
	}
}

func TestByteStuffingReader_Align_AfterFF(t *testing.T) {
	// Test alignment after 0xFF - sawFF state persists
	data := []byte{0xFF, 0xAA}
	r := NewByteStuffingReader(bytes.NewReader(data))

	// Read 3 bits from first byte (0xFF): 1, 1, 1
	for i := 0; i < 3; i++ {
		_, err := r.ReadBit()
		if err != nil {
			t.Fatalf("ReadBit() returned error: %v", err)
		}
	}

	// Align - discards remaining 5 bits, but sawFF state persists
	r.Align()

	// Next byte (0xAA) is read with only 7 bits due to sawFF=true
	// 0xAA = 10101010, reading 7 bits starting from bit 6: 0101010
	// First bit (bit 6) is 0
	bit, err := r.ReadBit()
	if err != nil {
		t.Fatalf("ReadBit() after Align returned error: %v", err)
	}
	if bit != 0 {
		t.Errorf("ReadBit() after Align = %d, want 0 (bit 6 of 0xAA after 0xFF)", bit)
	}
}

// =============================================================================
// ByteStuffingWriter tests
// =============================================================================

func TestNewByteStuffingWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)
	if w == nil {
		t.Fatal("NewByteStuffingWriter returned nil")
	}
}

func TestByteStuffingWriter_WriteBit_NoStuffing(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	// Write 0xAA (10101010)
	bits := []int{1, 0, 1, 0, 1, 0, 1, 0}
	for _, bit := range bits {
		if err := w.WriteBit(bit); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), []byte{0xAA}) {
		t.Errorf("Output = %v, want [0xAA]", buf.Bytes())
	}
}

func TestByteStuffingWriter_WriteBit_WithStuffing(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	// Write 0xFF (8 bits)
	for i := 0; i < 8; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}

	// After 0xFF, next byte limited to 7 bits
	// Write 7 bits of 1s: this triggers an automatic flush after 7 bits
	// (since delay=true after writing 0xFF)
	for i := 0; i < 7; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// After writing 8 bits of 1s: byte 0xFF is written, delay=true
	// After writing 7 more bits of 1s: byte 0x7F is written (auto-flush at 7 bits due to delay)
	// The 7 bits (1111111) are stored as-is since they fill the 7-bit quota
	expected := []byte{0xFF, 0x7F}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Output = %v, want %v", buf.Bytes(), expected)
	}
}

func TestByteStuffingWriter_WriteBit_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewByteStuffingWriter(&errWriter{n: 0, err: testErr})

	// Write 8 bits to trigger flush
	for i := 0; i < 7; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() at position %d returned error unexpectedly: %v", i, err)
		}
	}
	err := w.WriteBit(1)
	if !errors.Is(err, testErr) {
		t.Errorf("WriteBit() error = %v, want %v", err, testErr)
	}
}

func TestByteStuffingWriter_WriteBits(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	if err := w.WriteBits(0xABCD, 16); err != nil {
		t.Fatalf("WriteBits() returned error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), []byte{0xAB, 0xCD}) {
		t.Errorf("Output = %v, want [0xAB 0xCD]", buf.Bytes())
	}
}

func TestByteStuffingWriter_WriteBits_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewByteStuffingWriter(&errWriter{n: 0, err: testErr})

	err := w.WriteBits(0xFF, 8)
	if !errors.Is(err, testErr) {
		t.Errorf("WriteBits() error = %v, want %v", err, testErr)
	}
}

func TestByteStuffingWriter_Flush(t *testing.T) {
	tests := []struct {
		name     string
		bits     []int
		expected []byte
	}{
		{
			name:     "flush partial byte",
			bits:     []int{1, 0, 1, 0},
			expected: []byte{0xA0},
		},
		{
			name:     "flush empty",
			bits:     []int{},
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewByteStuffingWriter(buf)
			for _, bit := range tt.bits {
				if err := w.WriteBit(bit); err != nil {
					t.Fatalf("WriteBit() returned error: %v", err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush() returned error: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("Output = %v, want %v", buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestByteStuffingWriter_Flush_AfterFF(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	// Write 0xFF
	for i := 0; i < 8; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}

	// Write 4 bits (less than 7)
	for i := 0; i < 4; i++ {
		if err := w.WriteBit(1); err != nil {
			t.Fatalf("WriteBit() returned error: %v", err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// After 0xFF, only 7 bits allowed, so 4 bits padded to 7 bits
	// 1111 -> 1111000 -> 11110000 (wait, that's 8 bits)
	// Actually with 7-bit limit: 4 bits of 1s, padded with 3 zeros: 1111000 = 0x78
	// But when written as a byte, it's left-aligned: 11110000 = 0xF0? No wait...
	// The flush pads to maxBits (7), so 4 bits become 7 bits: 1111 << 3 = 1111000 = 0x78
	expected := []byte{0xFF, 0x78}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Output = %v, want %v", buf.Bytes(), expected)
	}
}

func TestByteStuffingWriter_Flush_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewByteStuffingWriter(&errWriter{n: 0, err: testErr})

	if err := w.WriteBit(1); err != nil {
		t.Fatalf("WriteBit() returned error: %v", err)
	}
	err := w.Flush()
	if !errors.Is(err, testErr) {
		t.Errorf("Flush() error = %v, want %v", err, testErr)
	}
}

// =============================================================================
// ByteStuffing round-trip tests
// =============================================================================

func TestByteStuffing_RoundTrip_NoFF(t *testing.T) {
	// Write values that don't produce 0xFF
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	original := []uint32{0xAB, 0xCD, 0x12, 0x34}
	for _, val := range original {
		if err := w.WriteBits(val, 8); err != nil {
			t.Fatalf("WriteBits() returned error: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// Read back
	r := NewByteStuffingReader(bytes.NewReader(buf.Bytes()))
	for i, want := range original {
		got, err := r.ReadBits(8)
		if err != nil {
			t.Fatalf("ReadBits(8) at index %d returned error: %v", i, err)
		}
		if got != want {
			t.Errorf("ReadBits(8) at index %d = 0x%X, want 0x%X", i, got, want)
		}
	}
}

func TestByteStuffing_RoundTrip_WithFF(t *testing.T) {
	// Write values that include 0xFF
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	// Write 0xFF followed by some data
	if err := w.WriteBits(0xFF, 8); err != nil {
		t.Fatalf("WriteBits() returned error: %v", err)
	}
	// After 0xFF, write 7 bits
	if err := w.WriteBits(0x55, 7); err != nil {
		t.Fatalf("WriteBits() returned error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// Read back
	r := NewByteStuffingReader(bytes.NewReader(buf.Bytes()))

	got, err := r.ReadBits(8)
	if err != nil {
		t.Fatalf("ReadBits(8) returned error: %v", err)
	}
	if got != 0xFF {
		t.Errorf("First ReadBits(8) = 0x%X, want 0xFF", got)
	}

	got, err = r.ReadBits(7)
	if err != nil {
		t.Fatalf("ReadBits(7) returned error: %v", err)
	}
	if got != 0x55 {
		t.Errorf("Second ReadBits(7) = 0x%X, want 0x55", got)
	}
}

// =============================================================================
// VariableLengthReader tests
// =============================================================================

func TestNewVariableLengthReader(t *testing.T) {
	buf := bytes.NewReader([]byte{0x00})
	r := NewVariableLengthReader(buf)
	if r == nil {
		t.Fatal("NewVariableLengthReader returned nil")
	}
}

func TestVariableLengthReader_Read(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected uint32
	}{
		{
			name:     "single byte value 0",
			data:     []byte{0x00},
			expected: 0,
		},
		{
			name:     "single byte value 1",
			data:     []byte{0x01},
			expected: 1,
		},
		{
			name:     "single byte max (127)",
			data:     []byte{0x7F},
			expected: 127,
		},
		{
			name:     "two bytes value 128",
			data:     []byte{0x81, 0x00}, // 10000001 00000000 -> (1 << 7) | 0 = 128
			expected: 128,
		},
		{
			name:     "two bytes value 255",
			data:     []byte{0x81, 0x7F}, // (1 << 7) | 127 = 255
			expected: 255,
		},
		{
			name:     "two bytes value 16383",
			data:     []byte{0xFF, 0x7F}, // (127 << 7) | 127 = 16383
			expected: 16383,
		},
		{
			name:     "three bytes value 16384",
			data:     []byte{0x81, 0x80, 0x00}, // (1 << 14) = 16384
			expected: 16384,
		},
		{
			name:     "four bytes",
			data:     []byte{0x81, 0x80, 0x80, 0x00}, // (1 << 21) = 2097152
			expected: 2097152,
		},
		{
			name:     "five bytes max uint32",
			data:     []byte{0x8F, 0xFF, 0xFF, 0xFF, 0x7F}, // Maximum representable
			expected: 0xFFFFFFFF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVariableLengthReader(bytes.NewReader(tt.data))
			got, err := r.Read()
			if err != nil {
				t.Fatalf("Read() returned error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Read() = %d (0x%X), want %d (0x%X)", got, got, tt.expected, tt.expected)
			}
		})
	}
}

func TestVariableLengthReader_Read_EOF(t *testing.T) {
	r := NewVariableLengthReader(bytes.NewReader([]byte{}))
	_, err := r.Read()
	if err == nil {
		t.Error("Read() on empty reader should return error")
	}
}

func TestVariableLengthReader_Read_UnexpectedEOF(t *testing.T) {
	// Continuation bit set but no more data
	r := NewVariableLengthReader(bytes.NewReader([]byte{0x80}))
	_, err := r.Read()
	if err == nil {
		t.Error("Read() with incomplete sequence should return error")
	}
}

func TestVariableLengthReader_Read_Multiple(t *testing.T) {
	// Multiple values encoded in sequence
	data := []byte{
		0x00,       // 0
		0x7F,       // 127
		0x81, 0x00, // 128
	}
	r := NewVariableLengthReader(bytes.NewReader(data))

	expected := []uint32{0, 127, 128}
	for i, want := range expected {
		got, err := r.Read()
		if err != nil {
			t.Fatalf("Read() at index %d returned error: %v", i, err)
		}
		if got != want {
			t.Errorf("Read() at index %d = %d, want %d", i, got, want)
		}
	}
}

// =============================================================================
// VariableLengthWriter tests
// =============================================================================

func TestNewVariableLengthWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewVariableLengthWriter(buf)
	if w == nil {
		t.Fatal("NewVariableLengthWriter returned nil")
	}
}

func TestVariableLengthWriter_Write(t *testing.T) {
	tests := []struct {
		name     string
		val      uint32
		expected []byte
	}{
		{
			name:     "value 0",
			val:      0,
			expected: []byte{0x00},
		},
		{
			name:     "value 1",
			val:      1,
			expected: []byte{0x01},
		},
		{
			name:     "value 127",
			val:      127,
			expected: []byte{0x7F},
		},
		{
			name:     "value 128",
			val:      128,
			expected: []byte{0x81, 0x00},
		},
		{
			name:     "value 255",
			val:      255,
			expected: []byte{0x81, 0x7F},
		},
		{
			name:     "value 16383",
			val:      16383,
			expected: []byte{0xFF, 0x7F},
		},
		{
			name:     "value 16384",
			val:      16384,
			expected: []byte{0x81, 0x80, 0x00},
		},
		{
			name:     "value 2097152",
			val:      2097152,
			expected: []byte{0x81, 0x80, 0x80, 0x00},
		},
		{
			name:     "max uint32",
			val:      0xFFFFFFFF,
			expected: []byte{0x8F, 0xFF, 0xFF, 0xFF, 0x7F},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			w := NewVariableLengthWriter(buf)
			if err := w.Write(tt.val); err != nil {
				t.Fatalf("Write(%d) returned error: %v", tt.val, err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("Write(%d) output = %v, want %v", tt.val, buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestVariableLengthWriter_Write_Error(t *testing.T) {
	testErr := errors.New("write error")
	w := NewVariableLengthWriter(&errWriter{n: 0, err: testErr})

	err := w.Write(128) // Requires 2 bytes
	if !errors.Is(err, testErr) {
		t.Errorf("Write() error = %v, want %v", err, testErr)
	}
}

func TestVariableLengthWriter_Write_Multiple(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewVariableLengthWriter(buf)

	values := []uint32{0, 127, 128}
	for _, val := range values {
		if err := w.Write(val); err != nil {
			t.Fatalf("Write(%d) returned error: %v", val, err)
		}
	}

	expected := []byte{
		0x00,       // 0
		0x7F,       // 127
		0x81, 0x00, // 128
	}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Output = %v, want %v", buf.Bytes(), expected)
	}
}

// =============================================================================
// VariableLength round-trip tests
// =============================================================================

func TestVariableLength_RoundTrip(t *testing.T) {
	values := []uint32{
		0, 1, 127, 128, 255, 256,
		16383, 16384,
		2097151, 2097152,
		268435455, 268435456,
		0x7FFFFFFF, 0x80000000, 0xFFFFFFFF,
	}

	for _, original := range values {
		// Write
		buf := &bytes.Buffer{}
		w := NewVariableLengthWriter(buf)
		if err := w.Write(original); err != nil {
			t.Fatalf("Write(%d) returned error: %v", original, err)
		}

		// Read back
		r := NewVariableLengthReader(bytes.NewReader(buf.Bytes()))
		got, err := r.Read()
		if err != nil {
			t.Fatalf("Read() for original %d returned error: %v", original, err)
		}
		if got != original {
			t.Errorf("Round-trip: wrote %d (0x%X), got %d (0x%X)", original, original, got, got)
		}
	}
}

func TestVariableLength_RoundTrip_Sequence(t *testing.T) {
	original := []uint32{0, 1, 127, 128, 255, 16383, 16384, 0xFFFFFFFF}

	// Write all values
	buf := &bytes.Buffer{}
	w := NewVariableLengthWriter(buf)
	for _, val := range original {
		if err := w.Write(val); err != nil {
			t.Fatalf("Write(%d) returned error: %v", val, err)
		}
	}

	// Read all values back
	r := NewVariableLengthReader(bytes.NewReader(buf.Bytes()))
	for i, want := range original {
		got, err := r.Read()
		if err != nil {
			t.Fatalf("Read() at index %d returned error: %v", i, err)
		}
		if got != want {
			t.Errorf("Read() at index %d = %d, want %d", i, got, want)
		}
	}
}

// =============================================================================
// Edge case tests
// =============================================================================

func TestReader_ReadBits_ZeroBits(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0xFF}))
	got, err := r.ReadBits(0)
	if err != nil {
		t.Fatalf("ReadBits(0) returned error: %v", err)
	}
	if got != 0 {
		t.Errorf("ReadBits(0) = %d, want 0", got)
	}
}

func TestWriter_WriteBits_ZeroBits(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf)

	if err := w.WriteBits(0xFFFFFFFF, 0); err != nil {
		t.Fatalf("WriteBits(_, 0) returned error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("WriteBits(_, 0) wrote %d bytes, want 0", buf.Len())
	}
}

func TestByteStuffingReader_ReadBits_ZeroBits(t *testing.T) {
	r := NewByteStuffingReader(bytes.NewReader([]byte{0xFF}))
	got, err := r.ReadBits(0)
	if err != nil {
		t.Fatalf("ReadBits(0) returned error: %v", err)
	}
	if got != 0 {
		t.Errorf("ReadBits(0) = %d, want 0", got)
	}
}

func TestByteStuffingWriter_WriteBits_ZeroBits(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	if err := w.WriteBits(0xFFFFFFFF, 0); err != nil {
		t.Fatalf("WriteBits(_, 0) returned error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("WriteBits(_, 0) wrote %d bytes, want 0", buf.Len())
	}
}

func TestWriter_WriteBit_MasksInput(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf)

	// WriteBit should mask input to 0 or 1
	// Write value 2 (binary 10), should be masked to 0
	if err := w.WriteBit(2); err != nil {
		t.Fatalf("WriteBit(2) returned error: %v", err)
	}
	// Write value 3 (binary 11), should be masked to 1
	if err := w.WriteBit(3); err != nil {
		t.Fatalf("WriteBit(3) returned error: %v", err)
	}
	// Write value -1 (all bits set), should be masked to 1
	if err := w.WriteBit(-1); err != nil {
		t.Fatalf("WriteBit(-1) returned error: %v", err)
	}
	// Pad with zeros
	for i := 0; i < 5; i++ {
		if err := w.WriteBit(0); err != nil {
			t.Fatalf("WriteBit(0) returned error: %v", err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// Expected: 0, 1, 1, 0, 0, 0, 0, 0 = 0x60
	expected := []byte{0x60}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Output = %v (0b%08b), want %v (0b%08b)", buf.Bytes(), buf.Bytes()[0], expected, expected[0])
	}
}

func TestByteStuffingWriter_Consecutive_FF(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewByteStuffingWriter(buf)

	// Write two consecutive 0xFF bytes
	for j := 0; j < 2; j++ {
		for i := 0; i < 8; i++ {
			if err := w.WriteBit(1); err != nil {
				t.Fatalf("WriteBit() returned error: %v", err)
			}
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() returned error: %v", err)
	}

	// First 0xFF written normally
	// Second 0xFF: after first 0xFF, only 7 bits allowed
	// So we need to write more than 8 bits of 1s to get a second 0xFF
	// First byte: 0xFF (8 bits)
	// After 0xFF: next byte limited to 7 bits
	// Writing 8 more 1-bits: 7 go into second byte (0xFE), 1 goes into third byte
	// But since second byte is 0xFE (not 0xFF), third byte gets 8 bits
	// Wait, 7 bits of all 1s: 1111111 = 0x7F << 1 = 0xFE
	// Then we have 1 bit left: 1, which needs flush padding

	// Actually let's trace through:
	// Write 8 1-bits: byte 1 = 0xFF, flushed
	// delay = true
	// Write 8 more 1-bits:
	//   bit 1: cnt=1, buf=1
	//   bit 2: cnt=2, buf=11
	//   bit 3: cnt=3, buf=111
	//   bit 4: cnt=4, buf=1111
	//   bit 5: cnt=5, buf=11111
	//   bit 6: cnt=6, buf=111111
	//   bit 7: cnt=7, buf=1111111 = 0x7F, maxBits=7, flush!
	//   Flush: byte 2 = 0x7F, delay = false
	//   bit 8: cnt=1, buf=1
	// After loop: cnt=1, buf=1
	// Flush: buf << (8-1) = 1 << 7 = 0x80

	expected := []byte{0xFF, 0x7F, 0x80}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Output = %v, want %v", buf.Bytes(), expected)
	}
}

// =============================================================================
// Benchmark tests
// =============================================================================

func BenchmarkReader_ReadBit(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 0xAA
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(data))
		for j := 0; j < len(data)*8; j++ {
			r.ReadBit()
		}
	}
}

func BenchmarkReader_ReadBits(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 0xAA
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(data))
		for j := 0; j < len(data)/4; j++ {
			r.ReadBits(32)
		}
	}
}

func BenchmarkWriter_WriteBit(b *testing.B) {
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewWriter(buf)
		for j := 0; j < 8192; j++ {
			w.WriteBit(j & 1)
		}
		w.Flush()
	}
}

func BenchmarkWriter_WriteBits(b *testing.B) {
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewWriter(buf)
		for j := 0; j < 256; j++ {
			w.WriteBits(uint32(j), 32)
		}
		w.Flush()
	}
}

func BenchmarkByteStuffingReader_ReadBit(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 0xAA
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewByteStuffingReader(bytes.NewReader(data))
		for j := 0; j < len(data)*8; j++ {
			r.ReadBit()
		}
	}
}

func BenchmarkByteStuffingWriter_WriteBit(b *testing.B) {
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewByteStuffingWriter(buf)
		for j := 0; j < 8192; j++ {
			w.WriteBit(j & 1)
		}
		w.Flush()
	}
}

func BenchmarkVariableLengthReader_Read(b *testing.B) {
	// Pre-encode a variety of values
	buf := &bytes.Buffer{}
	w := NewVariableLengthWriter(buf)
	for i := 0; i < 1000; i++ {
		w.Write(uint32(i * 137)) // Various sizes
	}
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewVariableLengthReader(bytes.NewReader(data))
		for j := 0; j < 1000; j++ {
			r.Read()
		}
	}
}

func BenchmarkVariableLengthWriter_Write(b *testing.B) {
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w := NewVariableLengthWriter(buf)
		for j := 0; j < 1000; j++ {
			w.Write(uint32(j * 137))
		}
	}
}
