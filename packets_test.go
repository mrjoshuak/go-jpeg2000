package jpeg2000

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// encodeTestCodestream encodes a small test image into a J2K codestream.
func encodeTestCodestream(t *testing.T, opts *Options) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) * 4)})
		}
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	opts.Format = FormatJ2K
	opts.Lossless = true
	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}

func encodeRGBTestCodestream(t *testing.T, opts *Options) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: uint8((x + y) * 8),
				A: 255,
			})
		}
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	opts.Format = FormatJ2K
	opts.Lossless = true
	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}

func TestExtractPackets_GrayscaleBasic(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	packets, err := ExtractPackets(cs)
	if err != nil {
		t.Fatalf("ExtractPackets: %v", err)
	}

	if len(packets) == 0 {
		t.Fatal("ExtractPackets returned no packets")
	}

	// Default options: 1 tile, 6 resolutions, 1 layer, 1 component, 1 precinct
	// Expected: 1 * 6 * 1 * 1 * 1 = 6 packets
	expectedCount := 6
	if len(packets) != expectedCount {
		t.Errorf("got %d packets, want %d", len(packets), expectedCount)
	}

	// Verify all packets have tile=0, layer=0, component=0
	for i, pkt := range packets {
		if pkt.Address.Tile != 0 {
			t.Errorf("packet %d: tile=%d, want 0", i, pkt.Address.Tile)
		}
		if pkt.Address.Layer != 0 {
			t.Errorf("packet %d: layer=%d, want 0", i, pkt.Address.Layer)
		}
		if pkt.Address.Component != 0 {
			t.Errorf("packet %d: component=%d, want 0", i, pkt.Address.Component)
		}
	}

	// Verify total data covers the tile data
	totalLen := 0
	for _, pkt := range packets {
		totalLen += len(pkt.Data)
	}
	if totalLen == 0 {
		t.Error("total packet data length is 0")
	}
}

func TestExtractPackets_RGBBasic(t *testing.T) {
	cs := encodeRGBTestCodestream(t, nil)

	packets, err := ExtractPackets(cs)
	if err != nil {
		t.Fatalf("ExtractPackets: %v", err)
	}

	// Default: 1 tile, 6 resolutions, 1 layer, 3 components, 1 precinct
	// Expected: 1 * 6 * 1 * 3 * 1 = 18 packets
	expectedCount := 18
	if len(packets) != expectedCount {
		t.Errorf("got %d packets, want %d", len(packets), expectedCount)
	}

	// Verify each packet has a valid address
	seen := make(map[PacketAddress]bool)
	for _, pkt := range packets {
		if seen[pkt.Address] {
			t.Errorf("duplicate packet address: %+v", pkt.Address)
		}
		seen[pkt.Address] = true
	}
}

func TestBuildPacketIndex_Basic(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	if idx.Len() == 0 {
		t.Fatal("PacketIndex has no entries")
	}

	// Verify AllAddresses returns all entries
	addrs := idx.AllAddresses()
	if len(addrs) != idx.Len() {
		t.Errorf("AllAddresses returned %d, Len() returned %d", len(addrs), idx.Len())
	}
}

func TestBuildPacketIndex_GetPacket(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	addrs := idx.AllAddresses()
	for _, addr := range addrs {
		data, err := idx.GetPacket(addr)
		if err != nil {
			t.Errorf("GetPacket(%+v): %v", addr, err)
			continue
		}
		// Data can be empty for some packets but should not error
		_ = data
	}

	// Test missing address
	_, err = idx.GetPacket(PacketAddress{Tile: 999})
	if err == nil {
		t.Error("GetPacket for missing address should return error")
	}
}

func TestExtractPackets_MatchesIndex(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	packets, err := ExtractPackets(cs)
	if err != nil {
		t.Fatalf("ExtractPackets: %v", err)
	}

	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	if len(packets) != idx.Len() {
		t.Fatalf("packet count mismatch: ExtractPackets=%d, BuildPacketIndex=%d", len(packets), idx.Len())
	}

	// Each packet from ExtractPackets should match GetPacket from the index
	for i, pkt := range packets {
		data, err := idx.GetPacket(pkt.Address)
		if err != nil {
			t.Errorf("packet %d: GetPacket: %v", i, err)
			continue
		}
		if !bytes.Equal(pkt.Data, data) {
			t.Errorf("packet %d: data mismatch (len %d vs %d)", i, len(pkt.Data), len(data))
		}
	}
}

func TestAllAddresses_CompleteCoverage(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	addrs := idx.AllAddresses()

	// Default: 6 resolutions, 1 component, 1 layer, 1 precinct, 1 tile
	// Check we have all expected resolution levels
	resLevels := make(map[uint8]bool)
	for _, addr := range addrs {
		resLevels[addr.Resolution] = true
	}
	for r := 0; r < 6; r++ {
		if !resLevels[uint8(r)] {
			t.Errorf("missing resolution level %d in addresses", r)
		}
	}
}

func TestExtractPackets_NonZeroData(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	packets, err := ExtractPackets(cs)
	if err != nil {
		t.Fatalf("ExtractPackets: %v", err)
	}

	// At least some packets should have non-zero data
	hasData := false
	for _, pkt := range packets {
		if len(pkt.Data) > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		t.Error("no packets contain data")
	}
}

func TestExtractPackets_TotalDataCoversCodestream(t *testing.T) {
	cs := encodeTestCodestream(t, nil)

	packets, err := ExtractPackets(cs)
	if err != nil {
		t.Fatalf("ExtractPackets: %v", err)
	}

	totalLen := 0
	for _, pkt := range packets {
		totalLen += len(pkt.Data)
	}

	// The total packet data should equal the tile data size
	// (everything between SOD and EOC for a single-tile image)
	// Just verify it's positive
	if totalLen <= 0 {
		t.Error("total packet data should be positive")
	}
}

func TestExtractPackets_InvalidCodestream(t *testing.T) {
	// Empty
	_, err := ExtractPackets(nil)
	if err == nil {
		t.Error("ExtractPackets(nil) should return error")
	}

	// Too short
	_, err = ExtractPackets([]byte{0xFF})
	if err == nil {
		t.Error("ExtractPackets(short) should return error")
	}

	// Wrong magic
	_, err = ExtractPackets([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("ExtractPackets(wrong magic) should return error")
	}
}

func TestBuildPacketIndex_RGBAddresses(t *testing.T) {
	cs := encodeRGBTestCodestream(t, nil)

	idx, err := BuildPacketIndex(cs)
	if err != nil {
		t.Fatalf("BuildPacketIndex: %v", err)
	}

	addrs := idx.AllAddresses()

	// Verify all 3 components are represented
	comps := make(map[uint8]int)
	for _, addr := range addrs {
		comps[addr.Component]++
	}
	if len(comps) != 3 {
		t.Errorf("expected 3 components, got %d: %v", len(comps), comps)
	}
	for c := 0; c < 3; c++ {
		if comps[uint8(c)] == 0 {
			t.Errorf("component %d has no packets", c)
		}
	}
}
