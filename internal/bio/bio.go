// Package bio provides bit-level I/O for JPEG 2000 codestreams.
package bio

import (
	"io"
)

// Reader provides bit-level reading from a byte stream.
type Reader struct {
	r   io.Reader
	buf byte   // Current byte buffer
	cnt uint8  // Number of valid bits in buf (0-8)
}

// NewReader creates a new bit reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// ReadBit reads a single bit (0 or 1).
func (r *Reader) ReadBit() (int, error) {
	if r.cnt == 0 {
		var b [1]byte
		if _, err := io.ReadFull(r.r, b[:]); err != nil {
			return 0, err
		}
		r.buf = b[0]
		r.cnt = 8
	}
	r.cnt--
	return int((r.buf >> r.cnt) & 1), nil
}

// ReadBits reads n bits (1-32).
func (r *Reader) ReadBits(n uint) (uint32, error) {
	var result uint32
	for i := uint(0); i < n; i++ {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, err
		}
		result = (result << 1) | uint32(bit)
	}
	return result, nil
}

// Align discards any remaining bits in the current byte.
func (r *Reader) Align() {
	r.cnt = 0
}

// Writer provides bit-level writing to a byte stream.
type Writer struct {
	w   io.Writer
	buf byte   // Current byte buffer
	cnt uint8  // Number of valid bits in buf (0-7)
}

// NewWriter creates a new bit writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteBit writes a single bit.
func (w *Writer) WriteBit(bit int) error {
	w.buf = (w.buf << 1) | byte(bit&1)
	w.cnt++
	if w.cnt == 8 {
		if err := w.flushByte(); err != nil {
			return err
		}
	}
	return nil
}

// WriteBits writes n bits from the lowest n bits of val.
func (w *Writer) WriteBits(val uint32, n uint) error {
	for i := n; i > 0; i-- {
		bit := int((val >> (i - 1)) & 1)
		if err := w.WriteBit(bit); err != nil {
			return err
		}
	}
	return nil
}

// flushByte writes the current byte buffer.
func (w *Writer) flushByte() error {
	b := [1]byte{w.buf}
	_, err := w.w.Write(b[:])
	w.buf = 0
	w.cnt = 0
	return err
}

// Flush writes any remaining bits, padding with zeros.
func (w *Writer) Flush() error {
	if w.cnt > 0 {
		w.buf <<= (8 - w.cnt)
		return w.flushByte()
	}
	return nil
}

// ByteStuffingReader handles JPEG 2000 byte stuffing (0xFF followed by 0x00).
type ByteStuffingReader struct {
	r      io.Reader
	buf    byte
	cnt    uint8
	sawFF  bool
}

// NewByteStuffingReader creates a reader that handles byte stuffing.
func NewByteStuffingReader(r io.Reader) *ByteStuffingReader {
	return &ByteStuffingReader{r: r}
}

// ReadBit reads a single bit, handling byte stuffing.
func (r *ByteStuffingReader) ReadBit() (int, error) {
	if r.cnt == 0 {
		var b [1]byte
		if _, err := io.ReadFull(r.r, b[:]); err != nil {
			return 0, err
		}

		// Handle byte stuffing: after 0xFF, next byte has only 7 bits
		if r.sawFF {
			r.cnt = 7
		} else {
			r.cnt = 8
		}
		r.sawFF = (b[0] == 0xFF)
		r.buf = b[0]
	}
	r.cnt--
	return int((r.buf >> r.cnt) & 1), nil
}

// ReadBits reads n bits (1-32), handling byte stuffing.
func (r *ByteStuffingReader) ReadBits(n uint) (uint32, error) {
	var result uint32
	for i := uint(0); i < n; i++ {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, err
		}
		result = (result << 1) | uint32(bit)
	}
	return result, nil
}

// Align discards remaining bits in the current byte.
func (r *ByteStuffingReader) Align() {
	r.cnt = 0
}

// ByteStuffingWriter handles JPEG 2000 byte stuffing for writing.
type ByteStuffingWriter struct {
	w     io.Writer
	buf   byte
	cnt   uint8
	delay bool // Delay flag for byte stuffing
}

// NewByteStuffingWriter creates a writer that handles byte stuffing.
func NewByteStuffingWriter(w io.Writer) *ByteStuffingWriter {
	return &ByteStuffingWriter{w: w}
}

// WriteBit writes a single bit with byte stuffing.
func (w *ByteStuffingWriter) WriteBit(bit int) error {
	// After writing 0xFF, we must limit the next byte to 7 bits
	maxBits := uint8(8)
	if w.delay {
		maxBits = 7
	}

	w.buf = (w.buf << 1) | byte(bit&1)
	w.cnt++

	if w.cnt == maxBits {
		if err := w.flushByte(); err != nil {
			return err
		}
	}
	return nil
}

// WriteBits writes n bits with byte stuffing.
func (w *ByteStuffingWriter) WriteBits(val uint32, n uint) error {
	for i := n; i > 0; i-- {
		bit := int((val >> (i - 1)) & 1)
		if err := w.WriteBit(bit); err != nil {
			return err
		}
	}
	return nil
}

// flushByte writes the current byte buffer.
func (w *ByteStuffingWriter) flushByte() error {
	b := [1]byte{w.buf}
	_, err := w.w.Write(b[:])
	w.delay = (w.buf == 0xFF)
	w.buf = 0
	w.cnt = 0
	return err
}

// Flush writes any remaining bits with padding.
func (w *ByteStuffingWriter) Flush() error {
	if w.cnt > 0 {
		maxBits := uint8(8)
		if w.delay {
			maxBits = 7
		}
		w.buf <<= (maxBits - w.cnt)
		return w.flushByte()
	}
	return nil
}

// VariableLengthReader reads variable-length encoded values.
type VariableLengthReader struct {
	r io.Reader
}

// NewVariableLengthReader creates a new variable-length reader.
func NewVariableLengthReader(r io.Reader) *VariableLengthReader {
	return &VariableLengthReader{r: r}
}

// Read reads a variable-length encoded value.
// Values are encoded with continuation bit (bit 7) set for all bytes
// except the last.
func (v *VariableLengthReader) Read() (uint32, error) {
	var result uint32
	for {
		var b [1]byte
		if _, err := io.ReadFull(v.r, b[:]); err != nil {
			return 0, err
		}
		result = (result << 7) | uint32(b[0]&0x7F)
		if b[0]&0x80 == 0 {
			break
		}
	}
	return result, nil
}

// VariableLengthWriter writes variable-length encoded values.
type VariableLengthWriter struct {
	w io.Writer
}

// NewVariableLengthWriter creates a new variable-length writer.
func NewVariableLengthWriter(w io.Writer) *VariableLengthWriter {
	return &VariableLengthWriter{w: w}
}

// Write writes a value using variable-length encoding.
func (v *VariableLengthWriter) Write(val uint32) error {
	// Determine number of 7-bit groups needed
	var bytes [5]byte
	n := 0
	for {
		bytes[4-n] = byte(val & 0x7F)
		if n > 0 {
			bytes[4-n] |= 0x80 // Set continuation bit
		}
		val >>= 7
		n++
		if val == 0 {
			break
		}
	}
	_, err := v.w.Write(bytes[5-n:])
	return err
}
