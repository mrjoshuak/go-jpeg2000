// Package tcd - t2.go implements Tier-2 packet coding.
//
// Tier-2 handles the organization of code-block data into packets
// according to the progression order. Each packet contains data for
// a specific layer, resolution, component, and precinct.
package tcd

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/mrjoshuak/go-jpeg2000/internal/bio"
	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// PacketIterator iterates over packets in progression order.
type PacketIterator struct {
	// Image parameters
	numComponents  int
	numResolutions int
	numLayers      int
	precincts      [][][]int // [component][resolution]numPrecincts

	// Current position
	layer      int
	resolution int
	component  int
	precinct   int

	// Progression order
	order codestream.ProgressionOrder

	// Bounds
	resStart, resEnd int
	compStart, compEnd int
	layStart, layEnd int
}

// NewPacketIterator creates a packet iterator.
func NewPacketIterator(
	numComponents, numResolutions, numLayers int,
	precincts [][][]int,
	order codestream.ProgressionOrder,
) *PacketIterator {
	return &PacketIterator{
		numComponents:  numComponents,
		numResolutions: numResolutions,
		numLayers:      numLayers,
		precincts:      precincts,
		order:          order,
		resEnd:         numResolutions,
		compEnd:        numComponents,
		layEnd:         numLayers,
	}
}

// Packet represents the current packet position.
type Packet struct {
	Layer      int
	Resolution int
	Component  int
	Precinct   int
}

// Next advances to the next packet position.
// Returns false when all packets have been visited.
func (pi *PacketIterator) Next() (Packet, bool) {
	for {
		if !pi.hasMore() {
			return Packet{}, false
		}

		p := Packet{
			Layer:      pi.layer,
			Resolution: pi.resolution,
			Component:  pi.component,
			Precinct:   pi.precinct,
		}

		pi.advance()
		return p, true
	}
}

func (pi *PacketIterator) hasMore() bool {
	switch pi.order {
	case codestream.LRCP:
		return pi.layer < pi.layEnd
	case codestream.RLCP:
		return pi.resolution < pi.resEnd
	case codestream.RPCL:
		return pi.resolution < pi.resEnd
	case codestream.PCRL:
		return pi.precinct < pi.maxPrecincts()
	case codestream.CPRL:
		return pi.component < pi.compEnd
	}
	return false
}

func (pi *PacketIterator) maxPrecincts() int {
	max := 0
	for c := 0; c < pi.numComponents; c++ {
		for r := 0; r < pi.numResolutions; r++ {
			if len(pi.precincts) > c && len(pi.precincts[c]) > r {
				if pi.precincts[c][r][0] > max {
					max = pi.precincts[c][r][0]
				}
			}
		}
	}
	return max
}

func (pi *PacketIterator) advance() {
	switch pi.order {
	case codestream.LRCP:
		pi.advanceLRCP()
	case codestream.RLCP:
		pi.advanceRLCP()
	case codestream.RPCL:
		pi.advanceRPCL()
	case codestream.PCRL:
		pi.advancePCRL()
	case codestream.CPRL:
		pi.advanceCPRL()
	}
}

func (pi *PacketIterator) advanceLRCP() {
	pi.precinct++
	numPrec := 1
	if len(pi.precincts) > pi.component && len(pi.precincts[pi.component]) > pi.resolution {
		numPrec = pi.precincts[pi.component][pi.resolution][0]
	}
	if pi.precinct >= numPrec {
		pi.precinct = 0
		pi.component++
		if pi.component >= pi.compEnd {
			pi.component = pi.compStart
			pi.resolution++
			if pi.resolution >= pi.resEnd {
				pi.resolution = pi.resStart
				pi.layer++
			}
		}
	}
}

func (pi *PacketIterator) advanceRLCP() {
	pi.precinct++
	numPrec := 1
	if len(pi.precincts) > pi.component && len(pi.precincts[pi.component]) > pi.resolution {
		numPrec = pi.precincts[pi.component][pi.resolution][0]
	}
	if pi.precinct >= numPrec {
		pi.precinct = 0
		pi.component++
		if pi.component >= pi.compEnd {
			pi.component = pi.compStart
			pi.layer++
			if pi.layer >= pi.layEnd {
				pi.layer = pi.layStart
				pi.resolution++
			}
		}
	}
}

func (pi *PacketIterator) advanceRPCL() {
	pi.layer++
	if pi.layer >= pi.layEnd {
		pi.layer = pi.layStart
		pi.component++
		if pi.component >= pi.compEnd {
			pi.component = pi.compStart
			pi.precinct++
			numPrec := 1
			if len(pi.precincts) > pi.component && len(pi.precincts[pi.component]) > pi.resolution {
				numPrec = pi.precincts[pi.component][pi.resolution][0]
			}
			if pi.precinct >= numPrec {
				pi.precinct = 0
				pi.resolution++
			}
		}
	}
}

func (pi *PacketIterator) advancePCRL() {
	pi.layer++
	if pi.layer >= pi.layEnd {
		pi.layer = pi.layStart
		pi.resolution++
		if pi.resolution >= pi.resEnd {
			pi.resolution = pi.resStart
			pi.component++
			if pi.component >= pi.compEnd {
				pi.component = pi.compStart
				pi.precinct++
			}
		}
	}
}

func (pi *PacketIterator) advanceCPRL() {
	pi.layer++
	if pi.layer >= pi.layEnd {
		pi.layer = pi.layStart
		pi.resolution++
		if pi.resolution >= pi.resEnd {
			pi.resolution = pi.resStart
			pi.precinct++
			numPrec := 1
			if len(pi.precincts) > pi.component && len(pi.precincts[pi.component]) > pi.resolution {
				numPrec = pi.precincts[pi.component][pi.resolution][0]
			}
			if pi.precinct >= numPrec {
				pi.precinct = 0
				pi.component++
			}
		}
	}
}

// Reset resets the iterator to the beginning.
func (pi *PacketIterator) Reset() {
	pi.layer = pi.layStart
	pi.resolution = pi.resStart
	pi.component = pi.compStart
	pi.precinct = 0
}

// PacketEncoder encodes packets to a bit stream.
type PacketEncoder struct {
	w   io.Writer
	bio *bio.ByteStuffingWriter
}

// NewPacketEncoder creates a new packet encoder.
func NewPacketEncoder(w io.Writer) *PacketEncoder {
	return &PacketEncoder{
		w:   w,
		bio: bio.NewByteStuffingWriter(w),
	}
}

// EncodePacket encodes a single packet.
func (e *PacketEncoder) EncodePacket(
	precinct *Precinct,
	layer int,
	enableSOP bool,
	enableEPH bool,
) error {
	// Write SOP marker if enabled
	if enableSOP {
		sop := []byte{0xFF, 0x91, 0x00, 0x04, 0x00, 0x00}
		binary.BigEndian.PutUint16(sop[4:], uint16(layer))
		if _, err := e.w.Write(sop); err != nil {
			return err
		}
	}

	// Encode packet header
	if err := e.encodePacketHeader(precinct, layer); err != nil {
		return err
	}

	// Write EPH marker if enabled
	if enableEPH {
		eph := []byte{0xFF, 0x92}
		if _, err := e.w.Write(eph); err != nil {
			return err
		}
	}

	// Write packet body (code-block data)
	for _, bandCBs := range precinct.CodeBlocks {
		for _, cb := range bandCBs {
			if cb.IncludedInLayers <= layer && len(cb.Data) > 0 {
				if _, err := e.w.Write(cb.Data); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// encodePacketHeader encodes the packet header.
func (e *PacketEncoder) encodePacketHeader(precinct *Precinct, layer int) error {
	// Check if packet is empty
	hasData := false
	for _, bandCBs := range precinct.CodeBlocks {
		for _, cb := range bandCBs {
			if cb.IncludedInLayers <= layer && len(cb.Data) > 0 {
				hasData = true
				break
			}
		}
		if hasData {
			break
		}
	}

	// Write packet presence bit
	if hasData {
		if err := e.bio.WriteBit(1); err != nil {
			return err
		}
	} else {
		if err := e.bio.WriteBit(0); err != nil {
			return err
		}
		return e.bio.Flush()
	}

	// Encode inclusion and length for each code-block
	for bandIdx, bandCBs := range precinct.CodeBlocks {
		for cbIdx, cb := range bandCBs {
			// Inclusion
			included := cb.IncludedInLayers <= layer && len(cb.Data) > 0

			if layer == 0 {
				// First layer - use tag tree
				e.encodeTagTreeValue(precinct.InclusionTree, cbIdx%precinct.InclusionTree.width, cbIdx/precinct.InclusionTree.width, cb.IncludedInLayers)
			} else {
				// Subsequent layers - single bit
				if included {
					if err := e.bio.WriteBit(1); err != nil {
						return err
					}
				} else {
					if err := e.bio.WriteBit(0); err != nil {
						return err
					}
				}
			}

			if !included {
				continue
			}

			// Zero bit-planes (IMSB)
			if cb.IncludedInLayers == layer {
				e.encodeTagTreeValue(precinct.IMSBTree, cbIdx%precinct.IMSBTree.width, cbIdx/precinct.IMSBTree.width, cb.ZeroBitPlanes)
			}

			// Number of coding passes
			numPasses := len(cb.Passes)
			if err := e.encodeNumPasses(numPasses); err != nil {
				return err
			}

			// Length of code-block data
			if err := e.encodeLength(len(cb.Data), bandIdx, cbIdx); err != nil {
				return err
			}
		}
	}

	return e.bio.Flush()
}

// encodeTagTreeValue encodes a value using the tag tree.
func (e *PacketEncoder) encodeTagTreeValue(tree *TagTree, x, y, value int) error {
	// Simplified tag tree encoding
	for i := 0; i < value; i++ {
		if err := e.bio.WriteBit(0); err != nil {
			return err
		}
	}
	return e.bio.WriteBit(1)
}

// encodeNumPasses encodes the number of coding passes.
func (e *PacketEncoder) encodeNumPasses(n int) error {
	if n == 1 {
		return e.bio.WriteBit(0)
	}
	if err := e.bio.WriteBit(1); err != nil {
		return err
	}
	if n == 2 {
		return e.bio.WriteBit(0)
	}
	if err := e.bio.WriteBit(1); err != nil {
		return err
	}
	if n <= 5 {
		return e.bio.WriteBits(uint32(n-3), 2)
	}
	if err := e.bio.WriteBits(3, 2); err != nil {
		return err
	}
	if n <= 36 {
		return e.bio.WriteBits(uint32(n-6), 5)
	}
	if err := e.bio.WriteBits(31, 5); err != nil {
		return err
	}
	return e.bio.WriteBits(uint32(n-37), 7)
}

// encodeLength encodes the code-block data length.
func (e *PacketEncoder) encodeLength(length, bandIdx, cbIdx int) error {
	// Use variable length encoding
	// Number of bits needed
	if length == 0 {
		return e.bio.WriteBits(0, 3)
	}

	bits := 0
	temp := length
	for temp > 0 {
		bits++
		temp >>= 1
	}

	// Encode number of bits
	if err := e.bio.WriteBits(uint32(bits), 3); err != nil {
		return err
	}

	// Encode length
	return e.bio.WriteBits(uint32(length), uint(bits))
}

// PacketDecoder decodes packets from a bit stream.
type PacketDecoder struct {
	r   io.Reader
	bio *bio.ByteStuffingReader
	buf []byte
	pos int
}

// NewPacketDecoder creates a new packet decoder.
func NewPacketDecoder(data []byte) *PacketDecoder {
	return &PacketDecoder{
		buf: data,
		bio: bio.NewByteStuffingReader(&byteReaderAt{data: data}),
	}
}

// byteReaderAt implements io.Reader for a byte slice.
type byteReaderAt struct {
	data []byte
	pos  int
}

func (r *byteReaderAt) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// DecodePacket decodes a single packet.
func (d *PacketDecoder) DecodePacket(
	precinct *Precinct,
	layer int,
	sopEnabled bool,
	ephEnabled bool,
) error {
	// Check for SOP marker
	if sopEnabled {
		if d.pos+6 <= len(d.buf) && d.buf[d.pos] == 0xFF && d.buf[d.pos+1] == 0x91 {
			d.pos += 6
		}
	}

	// Decode packet header
	if err := d.decodePacketHeader(precinct, layer); err != nil {
		return err
	}

	// Check for EPH marker
	if ephEnabled {
		if d.pos+2 <= len(d.buf) && d.buf[d.pos] == 0xFF && d.buf[d.pos+1] == 0x92 {
			d.pos += 2
		}
	}

	// Read packet body (code-block data)
	for _, bandCBs := range precinct.CodeBlocks {
		for _, cb := range bandCBs {
			if cb.IncludedInLayers == layer && len(cb.Data) > 0 {
				dataLen := len(cb.Data)
				if d.pos+dataLen > len(d.buf) {
					return fmt.Errorf("unexpected end of packet data")
				}
				copy(cb.Data, d.buf[d.pos:d.pos+dataLen])
				d.pos += dataLen
			}
		}
	}

	return nil
}

// decodePacketHeader decodes the packet header.
func (d *PacketDecoder) decodePacketHeader(precinct *Precinct, layer int) error {
	// Read packet presence bit
	present, err := d.bio.ReadBit()
	if err != nil {
		return err
	}
	if present == 0 {
		return nil // Empty packet
	}

	// Decode inclusion and length for each code-block
	for bandIdx, bandCBs := range precinct.CodeBlocks {
		for cbIdx, cb := range bandCBs {
			var included bool

			if layer == 0 {
				// First layer - use tag tree
				val, err := d.decodeTagTreeValue(precinct.InclusionTree, cbIdx%precinct.InclusionTree.width, cbIdx/precinct.InclusionTree.width)
				if err != nil {
					return err
				}
				included = val == layer
				cb.IncludedInLayers = val
			} else {
				// Subsequent layers - single bit
				bit, err := d.bio.ReadBit()
				if err != nil {
					return err
				}
				included = bit == 1
				if included {
					cb.IncludedInLayers = layer
				}
			}

			if !included {
				continue
			}

			// Zero bit-planes (IMSB)
			if cb.IncludedInLayers == layer {
				val, err := d.decodeTagTreeValue(precinct.IMSBTree, cbIdx%precinct.IMSBTree.width, cbIdx/precinct.IMSBTree.width)
				if err != nil {
					return err
				}
				cb.ZeroBitPlanes = val
			}

			// Number of coding passes
			numPasses, err := d.decodeNumPasses()
			if err != nil {
				return err
			}

			// Length of code-block data
			length, err := d.decodeLength(bandIdx, cbIdx)
			if err != nil {
				return err
			}

			cb.Passes = make([]CodingPass, numPasses)
			cb.Data = make([]byte, length)
		}
	}

	return nil
}

// decodeTagTreeValue decodes a value from the tag tree.
func (d *PacketDecoder) decodeTagTreeValue(tree *TagTree, x, y int) (int, error) {
	// Simplified tag tree decoding
	value := 0
	for {
		bit, err := d.bio.ReadBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		value++
	}
	return value, nil
}

// decodeNumPasses decodes the number of coding passes.
func (d *PacketDecoder) decodeNumPasses() (int, error) {
	bit, err := d.bio.ReadBit()
	if err != nil {
		return 0, err
	}
	if bit == 0 {
		return 1, nil
	}

	bit, err = d.bio.ReadBit()
	if err != nil {
		return 0, err
	}
	if bit == 0 {
		return 2, nil
	}

	val, err := d.bio.ReadBits(2)
	if err != nil {
		return 0, err
	}
	if val < 3 {
		return int(val) + 3, nil
	}

	val, err = d.bio.ReadBits(5)
	if err != nil {
		return 0, err
	}
	if val < 31 {
		return int(val) + 6, nil
	}

	val, err = d.bio.ReadBits(7)
	if err != nil {
		return 0, err
	}
	return int(val) + 37, nil
}

// decodeLength decodes the code-block data length.
func (d *PacketDecoder) decodeLength(bandIdx, cbIdx int) (int, error) {
	numBits, err := d.bio.ReadBits(3)
	if err != nil {
		return 0, err
	}
	if numBits == 0 {
		return 0, nil
	}

	length, err := d.bio.ReadBits(uint(numBits))
	if err != nil {
		return 0, err
	}
	return int(length), nil
}

// Position returns the current position in the data.
func (d *PacketDecoder) Position() int {
	return d.pos
}
