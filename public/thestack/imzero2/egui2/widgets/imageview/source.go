package imageview

import (
	"context"
	"image"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// tileSize is the side of a tile in image pixels (ADR-0373 §SD6).
const tileSize = 512

// checkerSize is the side of one square of the checkerboard under a
// transparent pixel, in cells of the level being drawn: image pixels at full
// resolution, and about as many screen pixels at every level below it.
const checkerSize = 8

// shown is everything a worker needs to map a tile, fixed at the moment the
// viewer published it: the source and what it said about itself, the
// generation its cached tiles are filed under, which plane or the image as
// stored, and the mappers compiled from the recipe. It is never modified
// once stored.
type shown struct {
	source  SourceI
	info    SourceInfo
	gen     uint64
	plane   int // the single plane shown, or asStored
	planes  *display.PlaneMapper
	colours *display.ColorMapper
	checker [2]uint32
	// cacheBytes bounds the sample cache for this source.
	cacheBytes int64
}

// tileSource answers portolan's loader with tiles of the source as currently
// shown. Workers read the current state through an atomic pointer; the
// viewer replaces it and then invalidates the map's tiles. The samples of a
// source that is not resident are kept in the cache, so a recipe change maps
// them again without asking the source.
type tileSource struct {
	cur   atomic.Pointer[shown]
	cache sampleCache
	// asked counts the tiles asked of a non-resident source, for tests and
	// for the status a host shows.
	asked atomic.Uint64
}

var _ portolan.TilePixelsI = (*tileSource)(nil)

// TilePixels maps the tile at (coords.X, coords.Y) of level coords.Z, which
// is the source's level: 0 its own pixels, below it halvings, above it
// doublings. Pixels outside the image are transparent; pixels inside it are
// opaque, a transparent sample having been laid over the checkerboard.
func (inst *tileSource) TilePixels(ctx context.Context, coords portolan.TileCoords) (px []uint32, w, h int, err error) {
	px = make([]uint32, tileSize*tileSize)
	sh := inst.cur.Load()
	if sh == nil || sh.source == nil {
		return px, tileSize, tileSize, nil
	}
	level := coords.Z
	if level < -sh.info.Reduced || level > sh.info.Magnified {
		return nil, 0, 0, eb.Build().Int("level", level).Int("reduced", sh.info.Reduced).Int("magnified", sh.info.Magnified).
			Errorf("source has no tiles at this level")
	}
	rect := TileRect(sh.info, level, coords.X, coords.Y)
	if rect.Empty() {
		return px, tileSize, tileSize, nil
	}
	d, err := inst.tile(ctx, sh, level, coords.X, coords.Y, rect)
	if err != nil {
		return nil, 0, 0, err
	}
	dst := px
	whole := rect.Dx() == tileSize && rect.Dy() == tileSize
	if !whole {
		dst = make([]uint32, rect.Dx()*rect.Dy())
	}
	from, region := d.Samples, d.Region
	switch {
	case sh.colours != nil:
		err = sh.colours.Map(dst, from, region)
	case sh.plane == asStored:
		alpha, _ := from.Alpha()
		err = sh.planes.Map(dst, from.Plane(0), alpha, region)
	default:
		err = sh.planes.Map(dst, from.Plane(sh.plane), imaging.Plane{}, region)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	overChecker(dst, rect, sh.checker)
	if !whole {
		for y := range rect.Dy() {
			copy(px[y*tileSize:y*tileSize+rect.Dx()], dst[y*rect.Dx():(y+1)*rect.Dx()])
		}
	}
	return px, tileSize, tileSize, nil
}

// tile is the samples of one tile: read in place from a resident source,
// from the cache, or asked of the source and kept.
func (inst *tileSource) tile(ctx context.Context, sh *shown, level, col, row int, rect image.Rectangle) (d TileData, err error) {
	key := cacheKey{gen: sh.gen, level: level, col: col, row: row}
	if !sh.info.Resident {
		if d, ok := inst.cache.get(key); ok {
			return d, nil
		}
		inst.asked.Add(1)
	}
	if d, err = sh.source.Tile(ctx, level, col, row); err != nil {
		return d, err
	}
	if err = checkTile(sh.info, d, rect); err != nil {
		return d, err
	}
	// A tile that lands after the source's content changed is still shown
	// for this request, but filed under its own generation, which nothing
	// asks for again.
	if !sh.info.Resident {
		inst.cache.put(key, d, sh.cacheBytes)
	}
	return d, nil
}

// cachedSample is the samples of pixel (col, row) of level 0 from the cache,
// when the tile holding it is there; it never asks the source.
func (inst *tileSource) cachedSample(sh *shown, col, row int) (d TileData, at image.Point, ok bool) {
	tc, tr := col/tileSize, row/tileSize
	d, ok = inst.cache.get(cacheKey{gen: sh.gen, level: 0, col: tc, row: tr})
	if !ok {
		return d, at, false
	}
	return d, d.Region.Min.Add(image.Pt(col-tc*tileSize, row-tr*tileSize)), true
}

// overChecker lays every pixel that is not opaque over a checkerboard fixed
// to the image's pixel grid and makes it opaque.
func overChecker(px []uint32, rect image.Rectangle, checker [2]uint32) {
	rw := rect.Dx()
	for i, p := range px {
		a := p & 0xff
		if a == 0xff {
			continue
		}
		col, row := rect.Min.X+i%rw, rect.Min.Y+i/rw
		k := checker[(col/checkerSize+row/checkerSize)&1]
		mix := func(shift uint) uint32 {
			return ((p>>shift&0xff)*a + (k>>shift&0xff)*(0xff-a) + 127) / 0xff
		}
		px[i] = mix(24)<<24 | mix(16)<<16 | mix(8)<<8 | 0xff
	}
}

// checkerColours are the two tones of the checkerboard, taken from the
// theme's neutral surfaces so it reads as "nothing here" in either theme.
func checkerColours() [2]uint32 {
	return [2]uint32{styletokens.NeutralBgSurface.AsHex(), styletokens.NeutralSubtle.AsHex()}
}
