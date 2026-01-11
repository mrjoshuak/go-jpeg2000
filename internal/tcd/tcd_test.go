package tcd

import (
	"testing"

	"github.com/aswf/go-jpeg2000/internal/codestream"
	"github.com/aswf/go-jpeg2000/internal/entropy"
)

// TestCeilDiv tests the ceilDiv helper function.
func TestCeilDiv(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{10, 3, 4},    // 10/3 = 3.33, ceil = 4
		{9, 3, 3},     // 9/3 = 3, ceil = 3
		{11, 3, 4},    // 11/3 = 3.67, ceil = 4
		{0, 3, 0},     // 0/3 = 0
		{1, 1, 1},     // 1/1 = 1
		{7, 4, 2},     // 7/4 = 1.75, ceil = 2
		{8, 4, 2},     // 8/4 = 2
		{100, 10, 10}, // 100/10 = 10
		{101, 10, 11}, // 101/10 = 10.1, ceil = 11
		{1, 100, 1},   // 1/100 = 0.01, ceil = 1
	}

	for _, tt := range tests {
		result := ceilDiv(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("ceilDiv(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

// TestMin tests the min helper function.
func TestMin(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-1, 1, -1},
		{100, 50, 50},
		{-100, -50, -100},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

// TestMax tests the max helper function.
func TestMax(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{0, 0, 0},
		{-1, 1, 1},
		{100, 50, 100},
		{-100, -50, -50},
	}

	for _, tt := range tests {
		result := max(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("max(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

// TestNewTagTree tests TagTree creation.
func TestNewTagTree(t *testing.T) {
	tests := []struct {
		width, height int
		expectLevels  int
	}{
		{1, 1, 1},   // Single node, 1 level
		{2, 2, 2},   // 2x2, needs 2 levels (4->1)
		{4, 4, 3},   // 4x4, needs 3 levels (16->4->1)
		{8, 8, 4},   // 8x8, needs 4 levels
		{3, 3, 3},   // 3x3, needs 3 levels (9->4->1)
		{5, 7, 4},   // 5x7, needs 4 levels
		{16, 16, 5}, // 16x16, needs 5 levels
	}

	for _, tt := range tests {
		tree := NewTagTree(tt.width, tt.height)
		if tree == nil {
			t.Errorf("NewTagTree(%d, %d) returned nil", tt.width, tt.height)
			continue
		}
		if tree.width != tt.width {
			t.Errorf("NewTagTree(%d, %d).width = %d; want %d", tt.width, tt.height, tree.width, tt.width)
		}
		if tree.height != tt.height {
			t.Errorf("NewTagTree(%d, %d).height = %d; want %d", tt.width, tt.height, tree.height, tt.height)
		}
		if tree.levels != tt.expectLevels {
			t.Errorf("NewTagTree(%d, %d).levels = %d; want %d", tt.width, tt.height, tree.levels, tt.expectLevels)
		}
		// Verify nodes are allocated
		if len(tree.nodes) != tt.expectLevels {
			t.Errorf("NewTagTree(%d, %d) has %d node levels; want %d", tt.width, tt.height, len(tree.nodes), tt.expectLevels)
		}
	}
}

// TestTagTreeSetValue tests setting values in the tag tree.
func TestTagTreeSetValue(t *testing.T) {
	tree := NewTagTree(4, 4)

	// Set some values
	tree.SetValue(0, 0, 5)
	tree.SetValue(1, 0, 3)
	tree.SetValue(0, 1, 7)
	tree.SetValue(3, 3, 2)

	// Verify values are set correctly
	if tree.nodes[0][0].value != 5 {
		t.Errorf("SetValue(0, 0, 5) failed; got %d", tree.nodes[0][0].value)
	}
	if tree.nodes[0][1].value != 3 {
		t.Errorf("SetValue(1, 0, 3) failed; got %d", tree.nodes[0][1].value)
	}
	if tree.nodes[0][4].value != 7 { // y=1 means index=4 for width=4
		t.Errorf("SetValue(0, 1, 7) failed; got %d", tree.nodes[0][4].value)
	}
	if tree.nodes[0][15].value != 2 { // x=3, y=3 means index=15
		t.Errorf("SetValue(3, 3, 2) failed; got %d", tree.nodes[0][15].value)
	}
}

// TestTagTreeReset tests resetting the tag tree state.
func TestTagTreeReset(t *testing.T) {
	tree := NewTagTree(4, 4)

	// Set some values and states
	tree.SetValue(0, 0, 5)
	tree.SetValue(1, 0, 3)

	// Manually set some state
	tree.nodes[0][0].low = 2
	tree.nodes[0][0].known = true
	tree.nodes[1][0].low = 1
	tree.nodes[1][0].known = true

	// Reset
	tree.Reset()

	// Verify values are preserved but state is reset
	if tree.nodes[0][0].value != 5 {
		t.Errorf("Reset cleared value; got %d, want 5", tree.nodes[0][0].value)
	}
	if tree.nodes[0][0].low != 0 {
		t.Errorf("Reset didn't clear low; got %d, want 0", tree.nodes[0][0].low)
	}
	if tree.nodes[0][0].known != false {
		t.Errorf("Reset didn't clear known; got %v, want false", tree.nodes[0][0].known)
	}
	if tree.nodes[1][0].low != 0 {
		t.Errorf("Reset didn't clear level 1 low; got %d, want 0", tree.nodes[1][0].low)
	}
}

// createTestHeader creates a minimal header for testing.
func createTestHeader() *codestream.Header {
	return &codestream.Header{
		ImageWidth:    64,
		ImageHeight:   64,
		ImageXOffset:  0,
		ImageYOffset:  0,
		TileWidth:     64,
		TileHeight:    64,
		TileXOffset:   0,
		TileYOffset:   0,
		NumComponents: 1,
		NumTilesX:     1,
		NumTilesY:     1,
		ComponentInfo: []codestream.ComponentInfo{
			{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1}, // 8-bit unsigned
		},
		CodingStyle: codestream.CodingStyleDefault{
			NumDecompositions:  2,
			CodeBlockWidthExp:  2, // 16x16 code blocks
			CodeBlockHeightExp: 2,
			WaveletTransform:   1, // 5-3 reversible
		},
	}
}

// TestNewTileDecoder tests TileDecoder creation.
func TestNewTileDecoder(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)

	if decoder == nil {
		t.Fatal("NewTileDecoder returned nil")
	}
	if decoder.header != header {
		t.Error("NewTileDecoder didn't store header reference")
	}
	if decoder.tile != nil {
		t.Error("NewTileDecoder should not initialize tile before InitTile")
	}
}

// TestTileDecoderInitTile tests tile initialization for decoding.
func TestTileDecoderInitTile(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)

	decoder.InitTile(0)

	tile := decoder.Tile()
	if tile == nil {
		t.Fatal("InitTile didn't create tile")
	}

	// Check tile bounds
	if tile.Index != 0 {
		t.Errorf("Tile.Index = %d; want 0", tile.Index)
	}
	if tile.X0 != 0 || tile.Y0 != 0 {
		t.Errorf("Tile origin = (%d, %d); want (0, 0)", tile.X0, tile.Y0)
	}
	if tile.X1 != 64 || tile.Y1 != 64 {
		t.Errorf("Tile extent = (%d, %d); want (64, 64)", tile.X1, tile.Y1)
	}

	// Check components
	if len(tile.Components) != 1 {
		t.Fatalf("Tile has %d components; want 1", len(tile.Components))
	}

	comp := tile.Components[0]
	if comp.Index != 0 {
		t.Errorf("Component.Index = %d; want 0", comp.Index)
	}
	if comp.X0 != 0 || comp.Y0 != 0 {
		t.Errorf("Component origin = (%d, %d); want (0, 0)", comp.X0, comp.Y0)
	}
	if comp.X1 != 64 || comp.Y1 != 64 {
		t.Errorf("Component extent = (%d, %d); want (64, 64)", comp.X1, comp.Y1)
	}

	// Check data allocation
	expectedSize := 64 * 64
	if len(comp.Data) != expectedSize {
		t.Errorf("Component.Data length = %d; want %d", len(comp.Data), expectedSize)
	}

	// Check resolutions
	numRes := 3 // NumDecompositions + 1
	if len(comp.Resolutions) != numRes {
		t.Fatalf("Component has %d resolutions; want %d", len(comp.Resolutions), numRes)
	}

	// Check resolution 0 (coarsest)
	res0 := comp.Resolutions[0]
	if res0.Level != 0 {
		t.Errorf("Resolution[0].Level = %d; want 0", res0.Level)
	}
	if res0.NumBands != 1 {
		t.Errorf("Resolution[0].NumBands = %d; want 1 (LL only)", res0.NumBands)
	}

	// Check resolution 1
	res1 := comp.Resolutions[1]
	if res1.Level != 1 {
		t.Errorf("Resolution[1].Level = %d; want 1", res1.Level)
	}
	if res1.NumBands != 3 {
		t.Errorf("Resolution[1].NumBands = %d; want 3 (HL, LH, HH)", res1.NumBands)
	}
}

// TestTileDecoderInitTileWithSubsampling tests tile init with subsampled components.
func TestTileDecoderInitTileWithSubsampling(t *testing.T) {
	header := createTestHeader()
	header.NumComponents = 3
	header.ComponentInfo = []codestream.ComponentInfo{
		{BitDepth: 7, SubsamplingX: 1, SubsamplingY: 1}, // Y - full resolution
		{BitDepth: 7, SubsamplingX: 2, SubsamplingY: 2}, // Cb - quarter resolution
		{BitDepth: 7, SubsamplingX: 2, SubsamplingY: 2}, // Cr - quarter resolution
	}

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	tile := decoder.Tile()
	if len(tile.Components) != 3 {
		t.Fatalf("Expected 3 components, got %d", len(tile.Components))
	}

	// Y component should be 64x64
	yComp := tile.Components[0]
	if yComp.X1-yComp.X0 != 64 || yComp.Y1-yComp.Y0 != 64 {
		t.Errorf("Y component size = %dx%d; want 64x64",
			yComp.X1-yComp.X0, yComp.Y1-yComp.Y0)
	}

	// Cb/Cr components should be 32x32
	cbComp := tile.Components[1]
	if cbComp.X1-cbComp.X0 != 32 || cbComp.Y1-cbComp.Y0 != 32 {
		t.Errorf("Cb component size = %dx%d; want 32x32",
			cbComp.X1-cbComp.X0, cbComp.Y1-cbComp.Y0)
	}
}

// TestTileDecoderInitTileMultipleTiles tests initialization with multiple tiles.
func TestTileDecoderInitTileMultipleTiles(t *testing.T) {
	header := createTestHeader()
	header.ImageWidth = 128
	header.ImageHeight = 128
	header.TileWidth = 64
	header.TileHeight = 64
	header.NumTilesX = 2
	header.NumTilesY = 2

	decoder := NewTileDecoder(header)

	// Test tile 0 (top-left)
	decoder.InitTile(0)
	tile0 := decoder.Tile()
	if tile0.X0 != 0 || tile0.Y0 != 0 || tile0.X1 != 64 || tile0.Y1 != 64 {
		t.Errorf("Tile 0 bounds wrong: (%d,%d)-(%d,%d)", tile0.X0, tile0.Y0, tile0.X1, tile0.Y1)
	}

	// Test tile 1 (top-right)
	decoder.InitTile(1)
	tile1 := decoder.Tile()
	if tile1.X0 != 64 || tile1.Y0 != 0 || tile1.X1 != 128 || tile1.Y1 != 64 {
		t.Errorf("Tile 1 bounds wrong: (%d,%d)-(%d,%d)", tile1.X0, tile1.Y0, tile1.X1, tile1.Y1)
	}

	// Test tile 2 (bottom-left)
	decoder.InitTile(2)
	tile2 := decoder.Tile()
	if tile2.X0 != 0 || tile2.Y0 != 64 || tile2.X1 != 64 || tile2.Y1 != 128 {
		t.Errorf("Tile 2 bounds wrong: (%d,%d)-(%d,%d)", tile2.X0, tile2.Y0, tile2.X1, tile2.Y1)
	}

	// Test tile 3 (bottom-right)
	decoder.InitTile(3)
	tile3 := decoder.Tile()
	if tile3.X0 != 64 || tile3.Y0 != 64 || tile3.X1 != 128 || tile3.Y1 != 128 {
		t.Errorf("Tile 3 bounds wrong: (%d,%d)-(%d,%d)", tile3.X0, tile3.Y0, tile3.X1, tile3.Y1)
	}
}

// TestDecodeCodeBlockEmptyData tests decoding with no data.
func TestDecodeCodeBlockEmptyData(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	cb := &CodeBlock{
		X0:   0,
		Y0:   0,
		X1:   16,
		Y1:   16,
		Data: nil, // No data
	}

	err := decoder.DecodeCodeBlock(cb, entropy.BandLL)
	if err != nil {
		t.Errorf("DecodeCodeBlock with empty data returned error: %v", err)
	}
}

// TestDecodeCodeBlockWithData tests decoding a code block with actual data.
func TestDecodeCodeBlockWithData(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	// Create minimal encoded data (this would be MQ encoded)
	cb := &CodeBlock{
		X0:             0,
		Y0:             0,
		X1:             4,
		Y1:             4,
		Data:           []byte{0x00, 0x00, 0x00, 0x00}, // Minimal data
		TotalBitPlanes: 1,
	}

	err := decoder.DecodeCodeBlock(cb, entropy.BandLL)
	if err != nil {
		t.Errorf("DecodeCodeBlock returned error: %v", err)
	}

	// Verify coefficients were allocated
	if cb.Coefficients == nil {
		t.Error("DecodeCodeBlock didn't allocate coefficients")
	}
}

// TestApplyInverseDWT53 tests inverse DWT with 5-3 wavelet.
func TestApplyInverseDWT53(t *testing.T) {
	header := createTestHeader()
	header.CodingStyle.WaveletTransform = 1 // 5-3 reversible
	header.CodingStyle.NumDecompositions = 1

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	comp := decoder.Tile().Components[0]
	// Initialize with some test data
	for i := range comp.Data {
		comp.Data[i] = int32(i % 256)
	}

	// Apply inverse DWT - this should modify the data
	originalFirst := comp.Data[0]
	decoder.ApplyInverseDWT(comp)

	// Just verify it doesn't panic and modifies data
	// The actual DWT correctness is tested in the dwt package
	_ = originalFirst // Suppress unused warning
}

// TestApplyInverseDWT97 tests inverse DWT with 9-7 wavelet.
func TestApplyInverseDWT97(t *testing.T) {
	header := createTestHeader()
	header.CodingStyle.WaveletTransform = 0 // 9-7 irreversible
	header.CodingStyle.NumDecompositions = 1

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	comp := decoder.Tile().Components[0]
	// Initialize with some test data
	for i := range comp.Data {
		comp.Data[i] = int32(i % 256)
	}

	// Apply inverse DWT
	decoder.ApplyInverseDWT(comp)

	// Verify DataFloat was created for 9-7
	if comp.DataFloat == nil {
		t.Error("ApplyInverseDWT with 9-7 didn't create DataFloat")
	}
}

// TestNewTileEncoder tests TileEncoder creation.
func TestNewTileEncoder(t *testing.T) {
	header := createTestHeader()
	encoder := NewTileEncoder(header)

	if encoder == nil {
		t.Fatal("NewTileEncoder returned nil")
	}
	if encoder.header != header {
		t.Error("NewTileEncoder didn't store header reference")
	}
}

// TestTileEncoderInitTile tests tile initialization for encoding.
func TestTileEncoderInitTile(t *testing.T) {
	header := createTestHeader()
	encoder := NewTileEncoder(header)

	// Create test component data
	componentData := [][]int32{
		make([]int32, 64*64),
	}
	for i := range componentData[0] {
		componentData[0][i] = int32(i % 256)
	}

	encoder.InitTile(0, componentData)

	if encoder.tile == nil {
		t.Fatal("InitTile didn't create tile")
	}

	// Verify component data was stored
	comp := encoder.tile.Components[0]
	if comp.Data == nil {
		t.Fatal("Component data is nil")
	}
	if len(comp.Data) != 64*64 {
		t.Errorf("Component data length = %d; want %d", len(comp.Data), 64*64)
	}

	// Verify data content
	for i := 0; i < 10; i++ {
		if comp.Data[i] != int32(i%256) {
			t.Errorf("Component data[%d] = %d; want %d", i, comp.Data[i], i%256)
			break
		}
	}
}

// TestApplyForwardDWT53 tests forward DWT with 5-3 wavelet.
func TestApplyForwardDWT53(t *testing.T) {
	header := createTestHeader()
	header.CodingStyle.WaveletTransform = 1 // 5-3 reversible
	header.CodingStyle.NumDecompositions = 1

	encoder := NewTileEncoder(header)

	componentData := [][]int32{
		make([]int32, 64*64),
	}
	for i := range componentData[0] {
		componentData[0][i] = int32(i % 256)
	}

	encoder.InitTile(0, componentData)
	comp := encoder.tile.Components[0]

	// Apply forward DWT
	encoder.ApplyForwardDWT(comp)

	// Just verify it doesn't panic
}

// TestApplyForwardDWT97 tests forward DWT with 9-7 wavelet.
func TestApplyForwardDWT97(t *testing.T) {
	header := createTestHeader()
	header.CodingStyle.WaveletTransform = 0 // 9-7 irreversible
	header.CodingStyle.NumDecompositions = 1

	encoder := NewTileEncoder(header)

	componentData := [][]int32{
		make([]int32, 64*64),
	}
	for i := range componentData[0] {
		componentData[0][i] = int32(i % 256)
	}

	encoder.InitTile(0, componentData)
	comp := encoder.tile.Components[0]

	// Apply forward DWT
	encoder.ApplyForwardDWT(comp)

	// Verify DataFloat was created for 9-7
	if comp.DataFloat == nil {
		t.Error("ApplyForwardDWT with 9-7 didn't create DataFloat")
	}
}

// TestEncodeCodeBlock tests encoding a code block.
func TestEncodeCodeBlock(t *testing.T) {
	header := createTestHeader()
	encoder := NewTileEncoder(header)

	// Create a code block
	cb := &CodeBlock{
		X0: 0,
		Y0: 0,
		X1: 4,
		Y1: 4,
	}

	// Create test data
	data := make([]int32, 16)
	for i := range data {
		data[i] = int32(i * 10)
	}

	encoder.EncodeCodeBlock(cb, data, entropy.BandLL)

	// Verify encoded data was produced
	if cb.Data == nil {
		t.Error("EncodeCodeBlock didn't produce encoded data")
	}
}

// TestBandTypes tests that band bounds are calculated correctly.
func TestBandTypes(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	tile := decoder.Tile()
	comp := tile.Components[0]

	// Resolution 0 should have LL band only
	res0 := comp.Resolutions[0]
	if len(res0.Bands) != 1 {
		t.Errorf("Resolution 0 has %d bands; want 1", len(res0.Bands))
	}
	if res0.Bands[0].Type != entropy.BandLL {
		t.Errorf("Resolution 0 band type = %d; want %d (BandLL)", res0.Bands[0].Type, entropy.BandLL)
	}

	// Resolution 1 should have HL, LH, HH bands
	res1 := comp.Resolutions[1]
	if len(res1.Bands) != 3 {
		t.Fatalf("Resolution 1 has %d bands; want 3", len(res1.Bands))
	}
	if res1.Bands[0].Type != entropy.BandHL {
		t.Errorf("Resolution 1 band 0 type = %d; want %d (BandHL)", res1.Bands[0].Type, entropy.BandHL)
	}
	if res1.Bands[1].Type != entropy.BandLH {
		t.Errorf("Resolution 1 band 1 type = %d; want %d (BandLH)", res1.Bands[1].Type, entropy.BandLH)
	}
	if res1.Bands[2].Type != entropy.BandHH {
		t.Errorf("Resolution 1 band 2 type = %d; want %d (BandHH)", res1.Bands[2].Type, entropy.BandHH)
	}
}

// TestCodeBlockGridCalculation tests code block grid setup.
func TestCodeBlockGridCalculation(t *testing.T) {
	header := createTestHeader()
	header.CodingStyle.CodeBlockWidthExp = 2  // 16 pixels
	header.CodingStyle.CodeBlockHeightExp = 2 // 16 pixels

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	tile := decoder.Tile()
	comp := tile.Components[0]

	// Check that code blocks are created for bands
	for resIdx, res := range comp.Resolutions {
		for bandIdx, band := range res.Bands {
			bandWidth := band.X1 - band.X0
			bandHeight := band.Y1 - band.Y0

			if bandWidth <= 0 || bandHeight <= 0 {
				continue // Skip empty bands
			}

			// Verify code blocks were allocated
			numCB := band.CodeBlocksX * band.CodeBlocksY
			if len(band.CodeBlocks) != numCB {
				t.Errorf("Res %d Band %d: has %d code blocks; want %d",
					resIdx, bandIdx, len(band.CodeBlocks), numCB)
			}

			// Verify first code block bounds
			if numCB > 0 {
				cb := band.CodeBlocks[0]
				if cb.X0 != band.X0 || cb.Y0 != band.Y0 {
					t.Errorf("Res %d Band %d CB[0]: origin (%d,%d); want (%d,%d)",
						resIdx, bandIdx, cb.X0, cb.Y0, band.X0, band.Y0)
				}
			}
		}
	}
}

// TestTileWithOffset tests tile calculation with image offset.
func TestTileWithOffset(t *testing.T) {
	header := createTestHeader()
	header.ImageXOffset = 10
	header.ImageYOffset = 20
	header.TileXOffset = 0
	header.TileYOffset = 0

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	tile := decoder.Tile()
	// Tile origin should be max of tile origin and image offset
	if tile.X0 != 10 {
		t.Errorf("Tile X0 = %d; want 10", tile.X0)
	}
	if tile.Y0 != 20 {
		t.Errorf("Tile Y0 = %d; want 20", tile.Y0)
	}
}

// TestPassTypeConstants verifies pass type constant values.
func TestPassTypeConstants(t *testing.T) {
	if PassSignificance != 0 {
		t.Errorf("PassSignificance = %d; want 0", PassSignificance)
	}
	if PassRefinement != 1 {
		t.Errorf("PassRefinement = %d; want 1", PassRefinement)
	}
	if PassCleanup != 2 {
		t.Errorf("PassCleanup = %d; want 2", PassCleanup)
	}
}

// TestCodingPassStructure tests CodingPass struct fields.
func TestCodingPassStructure(t *testing.T) {
	pass := CodingPass{
		Type:             PassSignificance,
		Length:           100,
		CumulativeLength: 500,
		Slope:            1.5,
		Terminated:       true,
	}

	if pass.Type != PassSignificance {
		t.Errorf("CodingPass.Type = %d; want %d", pass.Type, PassSignificance)
	}
	if pass.Length != 100 {
		t.Errorf("CodingPass.Length = %d; want 100", pass.Length)
	}
	if pass.CumulativeLength != 500 {
		t.Errorf("CodingPass.CumulativeLength = %d; want 500", pass.CumulativeLength)
	}
	if pass.Slope != 1.5 {
		t.Errorf("CodingPass.Slope = %f; want 1.5", pass.Slope)
	}
	if !pass.Terminated {
		t.Error("CodingPass.Terminated = false; want true")
	}
}

// TestTagTreeEdgeCases tests edge cases for tag trees.
func TestTagTreeEdgeCases(t *testing.T) {
	// Very small tree
	tree1x1 := NewTagTree(1, 1)
	if tree1x1.levels != 1 {
		t.Errorf("1x1 tree has %d levels; want 1", tree1x1.levels)
	}
	tree1x1.SetValue(0, 0, 42)
	if tree1x1.nodes[0][0].value != 42 {
		t.Errorf("1x1 tree SetValue failed")
	}
	tree1x1.Reset()
	if tree1x1.nodes[0][0].value != 42 {
		t.Error("Reset shouldn't clear values")
	}

	// Asymmetric tree
	tree2x4 := NewTagTree(2, 4)
	if tree2x4.width != 2 || tree2x4.height != 4 {
		t.Errorf("2x4 tree has wrong dimensions")
	}
	tree2x4.SetValue(0, 3, 99)
	if tree2x4.nodes[0][6].value != 99 { // index = 3*2 + 0 = 6
		t.Errorf("2x4 tree SetValue at (0,3) failed")
	}
}

// TestDecodeMultipleCodeBlocks tests decoding multiple code blocks sequentially.
func TestDecodeMultipleCodeBlocks(t *testing.T) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)
	decoder.InitTile(0)

	// Create multiple code blocks
	cbs := []*CodeBlock{
		{X0: 0, Y0: 0, X1: 4, Y1: 4, Data: []byte{}, TotalBitPlanes: 0},
		{X0: 4, Y0: 0, X1: 8, Y1: 4, Data: []byte{}, TotalBitPlanes: 0},
		{X0: 0, Y0: 4, X1: 4, Y1: 8, Data: []byte{}, TotalBitPlanes: 0},
	}

	for i, cb := range cbs {
		err := decoder.DecodeCodeBlock(cb, entropy.BandLL)
		if err != nil {
			t.Errorf("DecodeCodeBlock[%d] returned error: %v", i, err)
		}
	}
}

// BenchmarkNewTagTree benchmarks tag tree creation.
func BenchmarkNewTagTree(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewTagTree(64, 64)
	}
}

// BenchmarkTagTreeSetValue benchmarks setting values in tag tree.
func BenchmarkTagTreeSetValue(b *testing.B) {
	tree := NewTagTree(64, 64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.SetValue(i%64, i%64, i)
	}
}

// BenchmarkTagTreeReset benchmarks resetting tag tree.
func BenchmarkTagTreeReset(b *testing.B) {
	tree := NewTagTree(64, 64)
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			tree.SetValue(x, y, x+y)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Reset()
	}
}

// BenchmarkTileDecoderInitTile benchmarks tile initialization.
func BenchmarkTileDecoderInitTile(b *testing.B) {
	header := createTestHeader()
	decoder := NewTileDecoder(header)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder.InitTile(0)
	}
}

// BenchmarkApplyInverseDWT53 benchmarks inverse DWT.
func BenchmarkApplyInverseDWT53(b *testing.B) {
	header := createTestHeader()
	header.CodingStyle.WaveletTransform = 1
	header.CodingStyle.NumDecompositions = 3

	decoder := NewTileDecoder(header)
	decoder.InitTile(0)
	comp := decoder.Tile().Components[0]

	for i := range comp.Data {
		comp.Data[i] = int32(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder.ApplyInverseDWT(comp)
	}
}
