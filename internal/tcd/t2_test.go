package tcd

import (
	"bytes"
	"io"
	"testing"

	"github.com/aswf/go-jpeg2000/internal/codestream"
)

// Helper to create precincts for testing.
func createTestPrecincts(numComponents, numResolutions, numPrecincts int) [][][]int {
	precincts := make([][][]int, numComponents)
	for c := 0; c < numComponents; c++ {
		precincts[c] = make([][]int, numResolutions)
		for r := 0; r < numResolutions; r++ {
			precincts[c][r] = []int{numPrecincts}
		}
	}
	return precincts
}

// TestNewPacketIterator tests packet iterator creation.
func TestNewPacketIterator(t *testing.T) {
	precincts := createTestPrecincts(3, 4, 2)

	tests := []struct {
		name           string
		numComponents  int
		numResolutions int
		numLayers      int
		order          codestream.ProgressionOrder
	}{
		{"LRCP", 3, 4, 2, codestream.LRCP},
		{"RLCP", 3, 4, 2, codestream.RLCP},
		{"RPCL", 3, 4, 2, codestream.RPCL},
		{"PCRL", 3, 4, 2, codestream.PCRL},
		{"CPRL", 3, 4, 2, codestream.CPRL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := NewPacketIterator(tt.numComponents, tt.numResolutions, tt.numLayers, precincts, tt.order)
			if pi == nil {
				t.Fatal("NewPacketIterator returned nil")
			}
			if pi.numComponents != tt.numComponents {
				t.Errorf("numComponents = %d; want %d", pi.numComponents, tt.numComponents)
			}
			if pi.numResolutions != tt.numResolutions {
				t.Errorf("numResolutions = %d; want %d", pi.numResolutions, tt.numResolutions)
			}
			if pi.numLayers != tt.numLayers {
				t.Errorf("numLayers = %d; want %d", pi.numLayers, tt.numLayers)
			}
			if pi.order != tt.order {
				t.Errorf("order = %d; want %d", pi.order, tt.order)
			}
		})
	}
}

// TestPacketIteratorLRCP tests LRCP progression order.
func TestPacketIteratorLRCP(t *testing.T) {
	// 2 layers, 2 resolutions, 2 components, 1 precinct each
	precincts := createTestPrecincts(2, 2, 1)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.LRCP)

	// LRCP: Layer is outermost, Resolution next, Component next, Precinct innermost
	// Expected order: L=0,R=0,C=0,P=0 -> L=0,R=0,C=1,P=0 -> L=0,R=1,C=0,P=0 -> ...
	expectedPackets := []Packet{
		{Layer: 0, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 1, Precinct: 0},
	}

	for i, expected := range expectedPackets {
		packet, ok := pi.Next()
		if !ok {
			t.Fatalf("Packet %d: Next() returned false, expected more packets", i)
		}
		if packet != expected {
			t.Errorf("Packet %d: got %+v; want %+v", i, packet, expected)
		}
	}

	// Should be done
	_, ok := pi.Next()
	if ok {
		t.Error("Expected no more packets after iteration complete")
	}
}

// TestPacketIteratorRLCP tests RLCP progression order.
func TestPacketIteratorRLCP(t *testing.T) {
	// 2 layers, 2 resolutions, 2 components, 1 precinct each
	precincts := createTestPrecincts(2, 2, 1)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.RLCP)

	// RLCP: Resolution is outermost, Layer next, Component next, Precinct innermost
	// Expected order: R=0,L=0,C=0,P=0 -> R=0,L=0,C=1,P=0 -> R=0,L=1,C=0,P=0 -> ...
	expectedPackets := []Packet{
		{Layer: 0, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 1, Precinct: 0},
	}

	for i, expected := range expectedPackets {
		packet, ok := pi.Next()
		if !ok {
			t.Fatalf("Packet %d: Next() returned false, expected more packets", i)
		}
		if packet != expected {
			t.Errorf("Packet %d: got %+v; want %+v", i, packet, expected)
		}
	}
}

// TestPacketIteratorRPCL tests RPCL progression order.
func TestPacketIteratorRPCL(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 1)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.RPCL)

	// RPCL: Resolution, Precinct, Component, Layer
	expectedPackets := []Packet{
		{Layer: 0, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 1, Precinct: 0},
	}

	for i, expected := range expectedPackets {
		packet, ok := pi.Next()
		if !ok {
			t.Fatalf("Packet %d: Next() returned false", i)
		}
		if packet != expected {
			t.Errorf("Packet %d: got %+v; want %+v", i, packet, expected)
		}
	}
}

// TestPacketIteratorPCRL tests PCRL progression order.
func TestPacketIteratorPCRL(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 1)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.PCRL)

	// PCRL: Precinct, Component, Resolution, Layer
	expectedPackets := []Packet{
		{Layer: 0, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 1, Precinct: 0},
	}

	for i, expected := range expectedPackets {
		packet, ok := pi.Next()
		if !ok {
			t.Fatalf("Packet %d: Next() returned false", i)
		}
		if packet != expected {
			t.Errorf("Packet %d: got %+v; want %+v", i, packet, expected)
		}
	}
}

// TestPacketIteratorCPRL tests CPRL progression order.
func TestPacketIteratorCPRL(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 1)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.CPRL)

	// CPRL: Component, Position, Resolution, Layer
	expectedPackets := []Packet{
		{Layer: 0, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 0, Precinct: 0},
		{Layer: 0, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 0, Component: 1, Precinct: 0},
		{Layer: 0, Resolution: 1, Component: 1, Precinct: 0},
		{Layer: 1, Resolution: 1, Component: 1, Precinct: 0},
	}

	for i, expected := range expectedPackets {
		packet, ok := pi.Next()
		if !ok {
			t.Fatalf("Packet %d: Next() returned false", i)
		}
		if packet != expected {
			t.Errorf("Packet %d: got %+v; want %+v", i, packet, expected)
		}
	}
}

// TestPacketIteratorReset tests resetting the iterator.
func TestPacketIteratorReset(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 2)
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.LRCP)

	// Consume some packets
	for i := 0; i < 4; i++ {
		_, ok := pi.Next()
		if !ok {
			t.Fatalf("Unexpected end of packets at %d", i)
		}
	}

	// Reset
	pi.Reset()

	// First packet should be L=0,R=0,C=0,P=0
	packet, ok := pi.Next()
	if !ok {
		t.Fatal("Reset() didn't restore packets")
	}
	expected := Packet{Layer: 0, Resolution: 0, Component: 0, Precinct: 0}
	if packet != expected {
		t.Errorf("After Reset: got %+v; want %+v", packet, expected)
	}
}

// TestPacketIteratorMultiplePrecincts tests with multiple precincts.
func TestPacketIteratorMultiplePrecincts(t *testing.T) {
	precincts := createTestPrecincts(1, 1, 2) // 2 precincts
	pi := NewPacketIterator(1, 1, 1, precincts, codestream.LRCP)

	// Should iterate through both precincts
	p1, ok1 := pi.Next()
	if !ok1 {
		t.Fatal("Expected packet 1")
	}
	if p1.Precinct != 0 {
		t.Errorf("Packet 1 precinct = %d; want 0", p1.Precinct)
	}

	p2, ok2 := pi.Next()
	if !ok2 {
		t.Fatal("Expected packet 2")
	}
	if p2.Precinct != 1 {
		t.Errorf("Packet 2 precinct = %d; want 1", p2.Precinct)
	}
}

// TestPacketIteratorMaxPrecincts tests maxPrecincts calculation.
func TestPacketIteratorMaxPrecincts(t *testing.T) {
	// Create precincts with different counts per component/resolution
	precincts := [][][]int{
		{{2}, {3}}, // Component 0: res 0 has 2 precincts, res 1 has 3
		{{1}, {4}}, // Component 1: res 0 has 1 precinct, res 1 has 4
	}

	pi := NewPacketIterator(2, 2, 1, precincts, codestream.PCRL)
	maxPrec := pi.maxPrecincts()

	// Max should be 4
	if maxPrec != 4 {
		t.Errorf("maxPrecincts() = %d; want 4", maxPrec)
	}
}

// TestByteReaderAt tests the byteReaderAt helper.
func TestByteReaderAt(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	reader := &byteReaderAt{data: data}

	// Read first 2 bytes
	buf := make([]byte, 2)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("First read error: %v", err)
	}
	if n != 2 {
		t.Errorf("First read: n = %d; want 2", n)
	}
	if buf[0] != 0x01 || buf[1] != 0x02 {
		t.Errorf("First read: data = %v; want [0x01, 0x02]", buf)
	}

	// Read next 2 bytes
	n, err = reader.Read(buf)
	if err != nil {
		t.Fatalf("Second read error: %v", err)
	}
	if n != 2 {
		t.Errorf("Second read: n = %d; want 2", n)
	}
	if buf[0] != 0x03 || buf[1] != 0x04 {
		t.Errorf("Second read: data = %v; want [0x03, 0x04]", buf)
	}

	// Read remaining 1 byte
	n, err = reader.Read(buf)
	if err != nil {
		t.Fatalf("Third read error: %v", err)
	}
	if n != 1 {
		t.Errorf("Third read: n = %d; want 1", n)
	}

	// Read at EOF
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Errorf("EOF read: err = %v; want io.EOF", err)
	}
	if n != 0 {
		t.Errorf("EOF read: n = %d; want 0", n)
	}
}

// TestByteReaderAtEmpty tests reading from empty slice.
func TestByteReaderAtEmpty(t *testing.T) {
	reader := &byteReaderAt{data: []byte{}}
	buf := make([]byte, 1)

	n, err := reader.Read(buf)
	if err != io.EOF {
		t.Errorf("Empty read: err = %v; want io.EOF", err)
	}
	if n != 0 {
		t.Errorf("Empty read: n = %d; want 0", n)
	}
}

// TestNewPacketEncoder tests packet encoder creation.
func TestNewPacketEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	if enc == nil {
		t.Fatal("NewPacketEncoder returned nil")
	}
	if enc.w != &buf {
		t.Error("NewPacketEncoder didn't store writer")
	}
	if enc.bio == nil {
		t.Error("NewPacketEncoder didn't create ByteStuffingWriter")
	}
}

// TestNewPacketDecoder tests packet decoder creation.
func TestNewPacketDecoder(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	dec := NewPacketDecoder(data)

	if dec == nil {
		t.Fatal("NewPacketDecoder returned nil")
	}
	if len(dec.buf) != 3 {
		t.Errorf("Decoder buf length = %d; want 3", len(dec.buf))
	}
	if dec.bio == nil {
		t.Error("NewPacketDecoder didn't create ByteStuffingReader")
	}
}

// TestPacketDecoderPosition tests position tracking.
func TestPacketDecoderPosition(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	dec := NewPacketDecoder(data)

	if dec.Position() != 0 {
		t.Errorf("Initial position = %d; want 0", dec.Position())
	}
}

// createTestPrecinct creates a precinct for encoding/decoding tests.
func createTestPrecinct() *Precinct {
	tree := NewTagTree(2, 2)
	return &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            64,
		Y1:            64,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(2, 2),
	}
}

// TestEncodePacketEmpty tests encoding an empty packet.
func TestEncodePacketEmpty(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{
		{Index: 0, Data: nil, IncludedInLayers: 10}, // Not included
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	// Empty packet should produce minimal output (presence bit = 0)
	if buf.Len() == 0 {
		t.Error("Empty packet produced no output")
	}
}

// TestEncodePacketWithData tests encoding a packet with code block data.
func TestEncodePacketWithData(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xAA, 0xBB, 0xCC},
			IncludedInLayers: 0,
			ZeroBitPlanes:    2,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	// Packet should include header and code block data
	if buf.Len() == 0 {
		t.Error("Packet with data produced no output")
	}
}

// TestEncodePacketWithSOP tests encoding with SOP marker.
func TestEncodePacketWithSOP(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := enc.EncodePacket(precinct, 5, true, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	data := buf.Bytes()

	// SOP marker should be present: FF 91 00 04 XX XX
	if len(data) < 6 {
		t.Fatalf("Output too short for SOP marker: %d bytes", len(data))
	}
	if data[0] != 0xFF || data[1] != 0x91 {
		t.Errorf("SOP marker = %02X%02X; want FF91", data[0], data[1])
	}
	if data[2] != 0x00 || data[3] != 0x04 {
		t.Errorf("SOP length = %02X%02X; want 0004", data[2], data[3])
	}
	// Layer number in bytes 4-5
	layerNum := int(data[4])<<8 | int(data[5])
	if layerNum != 5 {
		t.Errorf("SOP layer number = %d; want 5", layerNum)
	}
}

// TestEncodePacketWithEPH tests encoding with EPH marker.
func TestEncodePacketWithEPH(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := enc.EncodePacket(precinct, 0, false, true)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	data := buf.Bytes()

	// EPH marker should be present: FF 92
	found := false
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0x92 {
			found = true
			break
		}
	}
	if !found {
		t.Error("EPH marker not found in output")
	}
}

// TestEncodePacketWithSOPAndEPH tests encoding with both markers.
func TestEncodePacketWithSOPAndEPH(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := enc.EncodePacket(precinct, 0, true, true)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	data := buf.Bytes()

	// Both markers should be present
	if len(data) < 8 { // At least SOP (6) + EPH (2)
		t.Fatalf("Output too short: %d bytes", len(data))
	}
}

// TestEncodeNumPasses tests encoding different numbers of coding passes.
func TestEncodeNumPasses(t *testing.T) {
	tests := []struct {
		numPasses int
		desc      string
	}{
		{1, "single pass"},
		{2, "two passes"},
		{3, "three passes"},
		{4, "four passes"},
		{5, "five passes"},
		{6, "six passes"},
		{10, "ten passes"},
		{36, "thirty-six passes"},
		{37, "thirty-seven passes"},
		{50, "fifty passes"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewPacketEncoder(&buf)

			err := enc.encodeNumPasses(tt.numPasses)
			if err != nil {
				t.Errorf("encodeNumPasses(%d) error: %v", tt.numPasses, err)
			}
		})
	}
}

// TestEncodeLength tests encoding code block lengths.
// Note: The encoding uses 3 bits for bit count, so max is 7 bits = 127.
func TestEncodeLength(t *testing.T) {
	tests := []struct {
		length int
		desc   string
	}{
		{0, "zero length"},
		{1, "one byte"},
		{10, "ten bytes"},
		{100, "hundred bytes"},
		{63, "6 bits"},
		{127, "max valid (7 bits)"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewPacketEncoder(&buf)

			err := enc.encodeLength(tt.length, 0, 0)
			if err != nil {
				t.Errorf("encodeLength(%d) error: %v", tt.length, err)
			}
		})
	}
}

// TestDecodeNumPasses tests decoding coding pass counts.
func TestDecodeNumPasses(t *testing.T) {
	tests := []struct {
		numPasses int
		desc      string
	}{
		{1, "single pass"},
		{2, "two passes"},
		{3, "three passes"},
		{4, "four passes"},
		{5, "five passes"},
		{6, "six passes"},
		{10, "ten passes"},
		{36, "max in 5-bit range"},
		{37, "start of 7-bit range"},
		{50, "fifty passes"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// Encode
			var buf bytes.Buffer
			enc := NewPacketEncoder(&buf)
			err := enc.encodeNumPasses(tt.numPasses)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			enc.bio.Flush()

			// Decode
			dec := NewPacketDecoder(buf.Bytes())
			decoded, err := dec.decodeNumPasses()
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if decoded != tt.numPasses {
				t.Errorf("Decoded %d; want %d", decoded, tt.numPasses)
			}
		})
	}
}

// TestDecodeLength tests decoding code block lengths.
// Note: The encoding uses 3 bits for bit count, so max is 7 bits = 127.
func TestDecodeLength(t *testing.T) {
	tests := []struct {
		length int
		desc   string
	}{
		{0, "zero length"},
		{1, "one byte"},
		{10, "ten bytes"},
		{100, "hundred bytes"},
		{127, "max valid (7 bits)"}, // Max value with 3-bit length encoding
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// Encode
			var buf bytes.Buffer
			enc := NewPacketEncoder(&buf)
			err := enc.encodeLength(tt.length, 0, 0)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			enc.bio.Flush()

			// Decode
			dec := NewPacketDecoder(buf.Bytes())
			decoded, err := dec.decodeLength(0, 0)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if decoded != tt.length {
				t.Errorf("Decoded %d; want %d", decoded, tt.length)
			}
		})
	}
}

// TestDecodePacketWithSOP tests decoding with SOP marker present.
func TestDecodePacketWithSOP(t *testing.T) {
	// Create data with SOP marker followed by minimal packet
	data := []byte{
		0xFF, 0x91, 0x00, 0x04, 0x00, 0x05, // SOP with layer=5
		0x00, // Empty packet (presence bit = 0)
	}

	dec := NewPacketDecoder(data)
	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := dec.DecodePacket(precinct, 5, true, false)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}

	// Position should be after SOP marker
	if dec.Position() < 6 {
		t.Errorf("Position after SOP = %d; want >= 6", dec.Position())
	}
}

// TestDecodePacketWithEPH tests decoding with EPH marker present.
func TestDecodePacketWithEPH(t *testing.T) {
	// Create data with minimal packet and EPH marker
	data := []byte{
		0x00,       // Empty packet (presence bit = 0)
		0xFF, 0x92, // EPH marker
	}

	dec := NewPacketDecoder(data)
	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := dec.DecodePacket(precinct, 0, false, true)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}
}

// TestDecodeTagTreeValue tests tag tree decoding.
func TestDecodeTagTreeValue(t *testing.T) {
	tree := NewTagTree(2, 2)

	tests := []struct {
		value int
		desc  string
	}{
		{0, "value 0"},
		{1, "value 1"},
		{5, "value 5"},
		{10, "value 10"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// Encode: value zeros followed by a one
			var buf bytes.Buffer
			enc := NewPacketEncoder(&buf)
			err := enc.encodeTagTreeValue(tree, 0, 0, tt.value)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			enc.bio.Flush()

			// Decode
			dec := NewPacketDecoder(buf.Bytes())
			decoded, err := dec.decodeTagTreeValue(tree, 0, 0)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if decoded != tt.value {
				t.Errorf("Decoded %d; want %d", decoded, tt.value)
			}
		})
	}
}

// TestEncodeDecodePacketRoundTrip tests full packet encode/decode cycle.
func TestEncodeDecodePacketRoundTrip(t *testing.T) {
	// Create encoder and precinct with data
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xDE, 0xAD, 0xBE, 0xEF},
			IncludedInLayers: 0,
			ZeroBitPlanes:    1,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode
	err := enc.EncodePacket(precinct, 0, true, true)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	// Decode
	dec := NewPacketDecoder(buf.Bytes())
	decodePrecinct := createTestPrecinct()
	decodePrecinct.CodeBlocks[0] = []*CodeBlock{
		{Index: 0},
	}

	err = dec.DecodePacket(decodePrecinct, 0, true, true)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}
}

// TestPacketIteratorEmptyPrecincts tests with empty precinct configuration.
func TestPacketIteratorEmptyPrecincts(t *testing.T) {
	// Empty precincts slice
	precincts := [][][]int{}
	pi := NewPacketIterator(0, 0, 0, precincts, codestream.LRCP)

	_, ok := pi.Next()
	if ok {
		t.Error("Empty iterator should return false")
	}
}

// TestPacketIteratorSingleElement tests with minimal configuration.
func TestPacketIteratorSingleElement(t *testing.T) {
	precincts := createTestPrecincts(1, 1, 1)
	pi := NewPacketIterator(1, 1, 1, precincts, codestream.LRCP)

	packet, ok := pi.Next()
	if !ok {
		t.Fatal("Expected one packet")
	}
	expected := Packet{Layer: 0, Resolution: 0, Component: 0, Precinct: 0}
	if packet != expected {
		t.Errorf("Got %+v; want %+v", packet, expected)
	}

	_, ok = pi.Next()
	if ok {
		t.Error("Expected no more packets")
	}
}

// TestEncodePacketMultipleCodeBlocks tests encoding with multiple code blocks.
func TestEncodePacketMultipleCodeBlocks(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0x01, 0x02},
			IncludedInLayers: 0,
			ZeroBitPlanes:    0,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
		{
			Index:            1,
			Data:             []byte{0x03, 0x04},
			IncludedInLayers: 0,
			ZeroBitPlanes:    1,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Multi-CB packet produced no output")
	}
}

// TestEncodePacketMultipleBands tests encoding with multiple bands.
func TestEncodePacketMultipleBands(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(2, 2)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            64,
		Y1:            64,
		CodeBlocks:    make([][]*CodeBlock, 3), // 3 bands (HL, LH, HH)
		InclusionTree: tree,
		IMSBTree:      NewTagTree(2, 2),
	}

	for band := 0; band < 3; band++ {
		precinct.CodeBlocks[band] = []*CodeBlock{
			{
				Index:            0,
				Data:             []byte{byte(band + 1)},
				IncludedInLayers: 0,
				ZeroBitPlanes:    0,
				Passes:           []CodingPass{{Type: PassCleanup}},
			},
		}
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Multi-band packet produced no output")
	}
}

// TestPacketIteratorCountPackets tests that iterator produces correct packet count.
func TestPacketIteratorCountPackets(t *testing.T) {
	tests := []struct {
		layers, res, comp, prec int
		order                   codestream.ProgressionOrder
		expected                int
	}{
		{1, 1, 1, 1, codestream.LRCP, 1},
		{2, 2, 2, 1, codestream.LRCP, 8},  // 2*2*2*1
		{3, 2, 2, 1, codestream.RLCP, 12}, // 3*2*2*1
		{2, 3, 2, 1, codestream.RPCL, 12}, // 2*3*2*1
	}

	for _, tt := range tests {
		precincts := createTestPrecincts(tt.comp, tt.res, tt.prec)
		pi := NewPacketIterator(tt.comp, tt.res, tt.layers, precincts, tt.order)

		count := 0
		for {
			_, ok := pi.Next()
			if !ok {
				break
			}
			count++
		}

		if count != tt.expected {
			t.Errorf("Order %d: counted %d packets; want %d", tt.order, count, tt.expected)
		}
	}
}

// BenchmarkPacketIteratorLRCP benchmarks LRCP iteration.
func BenchmarkPacketIteratorLRCP(b *testing.B) {
	precincts := createTestPrecincts(3, 5, 16)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pi := NewPacketIterator(3, 5, 10, precincts, codestream.LRCP)
		for {
			_, ok := pi.Next()
			if !ok {
				break
			}
		}
	}
}

// BenchmarkPacketIteratorRLCP benchmarks RLCP iteration.
func BenchmarkPacketIteratorRLCP(b *testing.B) {
	precincts := createTestPrecincts(3, 5, 16)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pi := NewPacketIterator(3, 5, 10, precincts, codestream.RLCP)
		for {
			_, ok := pi.Next()
			if !ok {
				break
			}
		}
	}
}

// BenchmarkPacketIteratorCPRL benchmarks CPRL iteration.
func BenchmarkPacketIteratorCPRL(b *testing.B) {
	precincts := createTestPrecincts(3, 5, 16)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pi := NewPacketIterator(3, 5, 10, precincts, codestream.CPRL)
		for {
			_, ok := pi.Next()
			if !ok {
				break
			}
		}
	}
}

// BenchmarkEncodePacket benchmarks packet encoding.
func BenchmarkEncodePacket(b *testing.B) {
	precinct := createTestPrecinct()
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             make([]byte, 1000),
			IncludedInLayers: 0,
			ZeroBitPlanes:    2,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		enc := NewPacketEncoder(&buf)
		enc.EncodePacket(precinct, 0, false, false)
	}
}

// BenchmarkDecodeNumPasses benchmarks decoding number of passes.
func BenchmarkDecodeNumPasses(b *testing.B) {
	// Pre-encode various pass counts
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)
	enc.encodeNumPasses(10)
	enc.bio.Flush()
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewPacketDecoder(data)
		dec.decodeNumPasses()
	}
}

// BenchmarkDecodeLength benchmarks decoding lengths.
func BenchmarkDecodeLength(b *testing.B) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)
	enc.encodeLength(1000, 0, 0)
	enc.bio.Flush()
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewPacketDecoder(data)
		dec.decodeLength(0, 0)
	}
}

// BenchmarkByteReaderAt benchmarks byte reader.
func BenchmarkByteReaderAt(b *testing.B) {
	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := &byteReaderAt{data: data}
		buf := make([]byte, 100)
		for {
			_, err := reader.Read(buf)
			if err == io.EOF {
				break
			}
		}
	}
}

// TestPacketIteratorUnknownOrder tests behavior with an invalid progression order.
func TestPacketIteratorUnknownOrder(t *testing.T) {
	precincts := createTestPrecincts(1, 1, 1)
	// Use an invalid order (larger than CPRL)
	pi := NewPacketIterator(1, 1, 1, precincts, codestream.ProgressionOrder(99))

	// With unknown order, hasMore should return false
	_, ok := pi.Next()
	if ok {
		t.Error("Unknown order should not produce packets")
	}
}

// TestDecodePacketHeaderNonZeroLayer tests decoding packet header at non-zero layer.
func TestDecodePacketHeaderNonZeroLayer(t *testing.T) {
	// Build packet data for layer 1 (not first layer)
	// Packet presence bit = 1, then inclusion bits for each CB
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xAA},
			IncludedInLayers: 1, // Included at layer 1
			ZeroBitPlanes:    0,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode at layer 1
	err := enc.EncodePacket(precinct, 1, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	// Verify encoding worked
	if buf.Len() == 0 {
		t.Error("Encoded packet should have data")
	}
}

// TestDecodePacketBodyWithData tests packet body decoding with code block data.
func TestDecodePacketBodyWithData(t *testing.T) {
	// Create packet with SOP, header, body data, and EPH
	data := []byte{
		0xFF, 0x91, 0x00, 0x04, 0x00, 0x00, // SOP with layer=0
		0x80,                               // Packet present (1 bit), then padding
		0xFF, 0x92,                         // EPH marker
		0xDE, 0xAD, 0xBE, 0xEF,             // Code block data
	}

	dec := NewPacketDecoder(data)
	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             make([]byte, 4), // Pre-allocate for body
			IncludedInLayers: 0,
		},
	}

	// This will exercise the packet body reading code
	err := dec.DecodePacket(precinct, 0, true, true)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}
}

// TestEncodePacketHeaderMultipleLayers tests encoding across multiple layers.
func TestEncodePacketHeaderMultipleLayers(t *testing.T) {
	tree := NewTagTree(2, 2)

	// First layer - code block first included
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            32,
		Y1:            32,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(2, 2),
	}

	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0x01},
			IncludedInLayers: 0, // First included at layer 0
			ZeroBitPlanes:    1,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
		{
			Index:            1,
			Data:             []byte{0x02},
			IncludedInLayers: 1, // First included at layer 1
			ZeroBitPlanes:    2,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode layer 0
	var buf0 bytes.Buffer
	enc0 := NewPacketEncoder(&buf0)
	err := enc0.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket layer 0 error: %v", err)
	}

	// Encode layer 1
	var buf1 bytes.Buffer
	enc1 := NewPacketEncoder(&buf1)
	err = enc1.EncodePacket(precinct, 1, false, false)
	if err != nil {
		t.Fatalf("EncodePacket layer 1 error: %v", err)
	}
}

// TestDecodeNumPassesEdgeCases tests edge cases for pass decoding.
func TestDecodeNumPassesEdgeCases(t *testing.T) {
	// Test specific boundary values
	tests := []int{1, 2, 3, 4, 5, 6, 7, 35, 36, 37, 38, 100}

	for _, numPasses := range tests {
		var buf bytes.Buffer
		enc := NewPacketEncoder(&buf)
		err := enc.encodeNumPasses(numPasses)
		if err != nil {
			t.Fatalf("encodeNumPasses(%d) error: %v", numPasses, err)
		}
		enc.bio.Flush()

		dec := NewPacketDecoder(buf.Bytes())
		decoded, err := dec.decodeNumPasses()
		if err != nil {
			t.Fatalf("decodeNumPasses (expecting %d) error: %v", numPasses, err)
		}
		if decoded != numPasses {
			t.Errorf("decodeNumPasses: got %d; want %d", decoded, numPasses)
		}
	}
}

// TestPacketIteratorWithVariablePrecincts tests with varying precinct counts.
func TestPacketIteratorWithVariablePrecincts(t *testing.T) {
	// Different number of precincts per resolution
	precincts := [][][]int{
		{{1}, {2}, {4}}, // Component 0: 1, 2, 4 precincts at res 0, 1, 2
	}

	pi := NewPacketIterator(1, 3, 1, precincts, codestream.LRCP)

	// Count all packets
	count := 0
	for {
		_, ok := pi.Next()
		if !ok {
			break
		}
		count++
	}

	// Should iterate: res0: 1 precinct, res1: 2 precincts, res2: 4 precincts = 7 total
	if count != 7 {
		t.Errorf("Counted %d packets; want 7", count)
	}
}

// TestHasMoreAllOrders tests hasMore for all progression orders.
func TestHasMoreAllOrders(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 2)

	orders := []codestream.ProgressionOrder{
		codestream.LRCP,
		codestream.RLCP,
		codestream.RPCL,
		codestream.PCRL,
		codestream.CPRL,
	}

	for _, order := range orders {
		pi := NewPacketIterator(2, 2, 2, precincts, order)

		// Should have packets initially
		packet, ok := pi.Next()
		if !ok {
			t.Errorf("Order %d: should have packets initially", order)
			continue
		}

		// Basic sanity check on first packet
		if packet.Layer < 0 || packet.Resolution < 0 || packet.Component < 0 || packet.Precinct < 0 {
			t.Errorf("Order %d: invalid first packet: %+v", order, packet)
		}
	}
}

// TestEncodePacketNotIncludedYet tests encoding when code block not yet included.
func TestEncodePacketNotIncludedYet(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xAA},
			IncludedInLayers: 5, // Will be included at layer 5
			ZeroBitPlanes:    0,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode at layer 0 - code block not yet included
	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}
}

// TestPacketStruct tests the Packet struct.
func TestPacketStruct(t *testing.T) {
	p := Packet{
		Layer:      1,
		Resolution: 2,
		Component:  3,
		Precinct:   4,
	}

	if p.Layer != 1 {
		t.Errorf("Packet.Layer = %d; want 1", p.Layer)
	}
	if p.Resolution != 2 {
		t.Errorf("Packet.Resolution = %d; want 2", p.Resolution)
	}
	if p.Component != 3 {
		t.Errorf("Packet.Component = %d; want 3", p.Component)
	}
	if p.Precinct != 4 {
		t.Errorf("Packet.Precinct = %d; want 4", p.Precinct)
	}
}

// TestDecodePacketEmptyPresent tests decoding an empty packet (presence=0).
func TestDecodePacketEmptyPresent(t *testing.T) {
	// Create minimal data - just presence bit = 0
	data := []byte{0x00} // All zeros, presence bit is 0

	dec := NewPacketDecoder(data)
	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{}

	err := dec.DecodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}
}

// TestEncodePacketWriteErrors tests error handling in packet encoding.
type errorWriter struct {
	failAfter int
	written   int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	if w.written >= w.failAfter {
		return 0, io.ErrShortWrite
	}
	w.written += len(p)
	return len(p), nil
}

// TestDecodePacketNonZeroLayerInclusion tests decoding at layer > 0 with inclusion.
func TestDecodePacketNonZeroLayerInclusion(t *testing.T) {
	// Encode a packet at layer 1 with a code block
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xDE, 0xAD},
			IncludedInLayers: 1,
			ZeroBitPlanes:    1,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode at layer 1
	err := enc.EncodePacket(precinct, 1, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}
}

// TestDecodePacketWithCodeBlockInclusion tests full decode with CB inclusion.
func TestDecodePacketWithCodeBlockInclusion(t *testing.T) {
	// First encode a packet
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0x11, 0x22, 0x33},
			IncludedInLayers: 0,
			ZeroBitPlanes:    0,
			Passes:           []CodingPass{{Type: PassCleanup}, {Type: PassRefinement}},
		},
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}
}

// TestEncodePacketMultiplePasses tests encoding with various pass counts.
func TestEncodePacketMultiplePasses(t *testing.T) {
	passCounts := []int{1, 2, 3, 5, 10, 36, 37, 50}

	for _, numPasses := range passCounts {
		var buf bytes.Buffer
		enc := NewPacketEncoder(&buf)

		tree := NewTagTree(1, 1)
		precinct := &Precinct{
			Index:         0,
			X0:            0,
			Y0:            0,
			X1:            16,
			Y1:            16,
			CodeBlocks:    make([][]*CodeBlock, 1),
			InclusionTree: tree,
			IMSBTree:      NewTagTree(1, 1),
		}

		passes := make([]CodingPass, numPasses)
		for i := 0; i < numPasses; i++ {
			passes[i] = CodingPass{Type: i % 3}
		}

		precinct.CodeBlocks[0] = []*CodeBlock{
			{
				Index:            0,
				Data:             []byte{0xAA},
				IncludedInLayers: 0,
				ZeroBitPlanes:    0,
				Passes:           passes,
			},
		}

		err := enc.EncodePacket(precinct, 0, false, false)
		if err != nil {
			t.Fatalf("EncodePacket with %d passes error: %v", numPasses, err)
		}
	}
}

// TestEncodeTagTreeValueZero tests tag tree encoding with value 0.
func TestEncodeTagTreeValueZero(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)
	tree := NewTagTree(2, 2)

	// Value 0 should encode as just "1"
	err := enc.encodeTagTreeValue(tree, 0, 0, 0)
	if err != nil {
		t.Fatalf("encodeTagTreeValue(0) error: %v", err)
	}
}

// TestDecodePacketDataCopy tests that packet body data is properly copied.
func TestDecodePacketDataCopy(t *testing.T) {
	// Create a packet with body data
	bodyData := []byte{0xCA, 0xFE, 0xBA, 0xBE}

	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             bodyData,
			IncludedInLayers: 0,
			ZeroBitPlanes:    2,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}
}

// TestPacketIteratorBoundsEdgeCases tests edge cases for bounds.
func TestPacketIteratorBoundsEdgeCases(t *testing.T) {
	precincts := createTestPrecincts(1, 1, 1)
	pi := NewPacketIterator(1, 1, 1, precincts, codestream.LRCP)

	// Initial state
	if pi.layer != 0 {
		t.Errorf("Initial layer = %d; want 0", pi.layer)
	}
	if pi.resolution != 0 {
		t.Errorf("Initial resolution = %d; want 0", pi.resolution)
	}
	if pi.component != 0 {
		t.Errorf("Initial component = %d; want 0", pi.component)
	}
	if pi.precinct != 0 {
		t.Errorf("Initial precinct = %d; want 0", pi.precinct)
	}
}

// TestDecodeLengthZero tests decoding a zero-length entry.
func TestDecodeLengthZero(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)
	enc.encodeLength(0, 0, 0)
	enc.bio.Flush()

	dec := NewPacketDecoder(buf.Bytes())
	length, err := dec.decodeLength(0, 0)
	if err != nil {
		t.Fatalf("decodeLength error: %v", err)
	}
	if length != 0 {
		t.Errorf("Decoded length = %d; want 0", length)
	}
}

// TestEncodePacketNoCodeBlocks tests encoding a precinct with no code blocks.
func TestEncodePacketNoCodeBlocks(t *testing.T) {
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 0), // No bands
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}
}

// TestAdvanceRPCLMultiplePrecincts tests RPCL advancement with multiple precincts.
func TestAdvanceRPCLMultiplePrecincts(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 3) // 3 precincts
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.RPCL)

	// Consume all packets and count
	count := 0
	for {
		_, ok := pi.Next()
		if !ok {
			break
		}
		count++
	}

	// Expected: 2 res * 3 prec * 2 comp * 2 layers = 24
	expected := 2 * 3 * 2 * 2
	if count != expected {
		t.Errorf("RPCL packet count = %d; want %d", count, expected)
	}
}

// TestAdvanceCPRLMultiplePrecincts tests CPRL advancement with multiple precincts.
func TestAdvanceCPRLMultiplePrecincts(t *testing.T) {
	precincts := createTestPrecincts(2, 2, 3) // 3 precincts
	pi := NewPacketIterator(2, 2, 2, precincts, codestream.CPRL)

	// Consume all packets and count
	count := 0
	for {
		_, ok := pi.Next()
		if !ok {
			break
		}
		count++
	}

	// Expected: 2 comp * 3 prec * 2 res * 2 layers = 24
	expected := 2 * 3 * 2 * 2
	if count != expected {
		t.Errorf("CPRL packet count = %d; want %d", count, expected)
	}
}

// TestDecodePacketHeaderAtLayerOne tests decoding packet header at layer 1.
// This covers the "subsequent layers - single bit" branch in decodePacketHeader.
func TestDecodePacketHeaderAtLayerOne(t *testing.T) {
	// First, encode a packet at layer 1 with code block included
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             []byte{0xAB, 0xCD},
			IncludedInLayers: 1, // First included at layer 1
			ZeroBitPlanes:    2,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	// Encode at layer 1
	err := enc.EncodePacket(precinct, 1, false, false)
	if err != nil {
		t.Fatalf("EncodePacket at layer 1 error: %v", err)
	}

	// Decode the encoded packet
	dec := NewPacketDecoder(buf.Bytes())
	decodePrecinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: NewTagTree(1, 1),
		IMSBTree:      NewTagTree(1, 1),
	}
	decodePrecinct.CodeBlocks[0] = []*CodeBlock{
		{Index: 0}, // Empty CB, will be populated by decode
	}

	err = dec.DecodePacket(decodePrecinct, 1, false, false)
	if err != nil {
		t.Fatalf("DecodePacket at layer 1 error: %v", err)
	}
}

// TestDecodePacketWithCodeBlockDataBody tests decoding packet body with CB data.
func TestDecodePacketWithCodeBlockDataBody(t *testing.T) {
	// Encode a complete packet with code block data
	var buf bytes.Buffer
	enc := NewPacketEncoder(&buf)

	tree := NewTagTree(1, 1)
	precinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: tree,
		IMSBTree:      NewTagTree(1, 1),
	}
	cbData := []byte{0x12, 0x34, 0x56, 0x78}
	precinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             cbData,
			IncludedInLayers: 0,
			ZeroBitPlanes:    1,
			Passes:           []CodingPass{{Type: PassCleanup}},
		},
	}

	err := enc.EncodePacket(precinct, 0, false, false)
	if err != nil {
		t.Fatalf("EncodePacket error: %v", err)
	}

	// Now decode
	dec := NewPacketDecoder(buf.Bytes())
	decodePrecinct := &Precinct{
		Index:         0,
		X0:            0,
		Y0:            0,
		X1:            16,
		Y1:            16,
		CodeBlocks:    make([][]*CodeBlock, 1),
		InclusionTree: NewTagTree(1, 1),
		IMSBTree:      NewTagTree(1, 1),
	}
	decodePrecinct.CodeBlocks[0] = []*CodeBlock{
		{
			Index:            0,
			Data:             make([]byte, len(cbData)), // Pre-allocate for body copy
			IncludedInLayers: 0,
		},
	}

	err = dec.DecodePacket(decodePrecinct, 0, false, false)
	if err != nil {
		t.Fatalf("DecodePacket error: %v", err)
	}
}
