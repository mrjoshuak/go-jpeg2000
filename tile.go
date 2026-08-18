package jpeg2000

// Tiled encoding.
//
// A tiled image is not one codestream with a larger SIZ: it is one tile-part
// per tile, each with its own SOT/SOD pair and its own packets, and each tile
// is transformed and coded independently of its neighbours. The encoder used
// to write the tile grid into SIZ and then ignore it, emitting a single
// tile-part that held the whole image's packets. Every conforming decoder
// reads that as tile 0 of a grid it was told is smaller — OpenJPH stops with
// "Error decoding a codeblock" (ojph_codeblock.cpp:221) — while this
// library's own decoder, sharing the same mistake, read it back perfectly.

import (
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/dwt"
)

// tileDims returns the tile size to record in SIZ. A tile larger than the
// image is legal and yields a 1x1 grid.
func (e *encoder) tileDims() (int, int) {
	tw, th := e.width, e.height
	if e.options != nil {
		if e.options.TileSize.X > 0 {
			tw = e.options.TileSize.X
		}
		if e.options.TileSize.Y > 0 {
			th = e.options.TileSize.Y
		}
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	return tw, th
}

// tileExtent returns the size of the largest tile the grid actually contains,
// which is the tile size clipped to the image.
func (e *encoder) tileExtent() (int, int) {
	tw, th := e.tileDims()
	return min(tw, e.width), min(th, e.height)
}

// tileGrid returns the number of tiles across and down.
func (e *encoder) tileGrid() (int, int) {
	tw, th := e.tileDims()
	if e.width <= 0 || e.height <= 0 {
		return 1, 1
	}
	return (e.width + tw - 1) / tw, (e.height + th - 1) / th
}

// numTiles returns the number of tiles in the grid.
func (e *encoder) numTiles() int {
	nx, ny := e.tileGrid()
	return nx * ny
}

// tileBounds returns the image-coordinate bounds of one tile. The encoder
// always writes zero image and tile offsets, so tile (tx, ty) starts at
// (tx*XTsiz, ty*YTsiz) and is clipped to the image.
func (e *encoder) tileBounds(tileIdx int) (x0, y0, x1, y1 int) {
	nx, _ := e.tileGrid()
	tw, th := e.tileDims()
	tx, ty := tileIdx%nx, tileIdx/nx
	x0, y0 = tx*tw, ty*th
	return x0, y0, min(x0+tw, e.width), min(y0+th, e.height)
}

// encodeTileGrid encodes every tile of a multi-tile image, in raster order,
// as a tile-part of its own.
func (e *encoder) encodeTileGrid() ([]byte, error) {
	var buf []byte
	for idx := 0; idx < e.numTiles(); idx++ {
		part, err := e.encodeTileAt(idx)
		if err != nil {
			return nil, fmt.Errorf("tile %d: %w", idx, err)
		}
		buf = append(buf, part...)
	}
	return buf, nil
}

// encodeTileAt encodes one tile of a multi-tile image.
//
// The tile's samples are cut out of the (level-shifted, colour-transformed)
// component planes, transformed at the tile's own absolute origin, and coded
// with the subband geometry that origin implies.
func (e *encoder) encodeTileAt(tileIdx int) ([]byte, error) {
	x0, y0, x1, y1 := e.tileBounds(tileIdx)
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty bounds [%d,%d)x[%d,%d)", x0, x1, y0, y1)
	}

	var comps [][]int32
	var comps64 [][]int64
	if e.wide {
		comps64 = e.wideTiles[tileIdx]
	} else {
		comps = make([][]int32, e.numComponents)
		for c := 0; c < e.numComponents; c++ {
			plane := e.componentData[c]
			tile := make([]int32, w*h)
			for row := 0; row < h; row++ {
				src := (y0+row)*e.width + x0
				if src+w > len(plane) {
					return nil, fmt.Errorf("component %d is short of samples", c)
				}
				copy(tile[row*w:(row+1)*w], plane[src:src+w])
			}
			e.transformTile(tile, w, h, x0, y0)
			comps[c] = tile
		}
	}

	jobs, layout := e.collectJobs(comps, comps64, x0, y0, x1, y1)
	encoded, numBPS, passes := e.encodeJobs(jobs)
	return e.createTileHeader(tileIdx, e.assembleTileData(layout, jobs, encoded, numBPS, passes)), nil
}

// transformTile applies the wavelet decomposition to one tile-component,
// in place, at the tile's absolute origin.
//
// This is the same transform preprocess applies to a single-tile image, with
// the origin made explicit: the subband a sample lands in depends on whether
// its coordinate is even, and a tile does not generally start at an even
// coordinate once it has been halved a few times.
func (e *encoder) transformTile(data []int32, w, h, x0, y0 int) {
	numLevels := e.numResolutions() - 1
	if numLevels <= 0 {
		return
	}

	if e.options.Lossless {
		dwt.DecomposeMultiLevel53Tile(data, w, h, x0, y0, numLevels)
		return
	}

	dataFloat := make([]float64, len(data))
	for i, v := range data {
		dataFloat[i] = float64(v)
	}
	dwt.DecomposeMultiLevel97Tile(dataFloat, w, h, x0, y0, numLevels)
	quality := e.options.Quality
	if quality <= 0 {
		quality = 100 // Default to lossless if quality not set
	}
	stepSize := 1.0 / float64(quality)
	for i, v := range dataFloat {
		if v >= 0 {
			data[i] = int32(v/stepSize + 0.5)
		} else {
			data[i] = int32(v/stepSize - 0.5)
		}
	}
}
