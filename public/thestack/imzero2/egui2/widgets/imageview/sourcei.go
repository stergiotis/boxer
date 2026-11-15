package imageview

import (
	"container/list"
	"context"
	"image"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
)

// SourceI is what a viewer shows (ADR-0374 §SD1): something that describes
// an image once and answers a tile of its samples at a level. An image in
// memory is one source ([Viewer.SetImage]); a renderer that draws a tile
// when asked, or a store a decoder filled, is another ([Viewer.SetSource]).
//
// Tile is called on the viewer's loader workers, several at once, and must
// honour ctx: a tile the view no longer needs is cancelled.
type SourceI interface {
	// Info describes the source. It is read once, when the source is set.
	Info() SourceInfo
	// Tile answers tile (col, row) of a level: the cells of
	// [TileRect](info, level, col, row), as a region of samples. Level 0 is
	// the source's pixel grid, level −k a 2^k reduction, level +k a 2^k
	// magnification; only the levels the source declares are asked for.
	Tile(ctx context.Context, level, col, row int) (TileData, error)
}

// SourceInfo is what a source says about itself.
type SourceInfo struct {
	// Width and Height are the size at level 0, in pixels.
	Width, Height int
	// Model and Kind are what every tile's samples are.
	Model imaging.ColorModelE
	Kind  imaging.SampleKindE
	// Transforms are the planes' value transforms, in the model's plane
	// order; nil or a shorter list means the identity for the rest.
	Transforms []imaging.ValueTransform
	// Reduced and Magnified are how many levels the source answers below and
	// above level 0. Beyond them the nearest level is drawn scaled.
	Reduced, Magnified int
	// Rendered marks a source whose samples are drawn rather than measured —
	// a page, a plot (ADR-0374 §SD2). The viewer then claims nothing about
	// them as a measurement: it says no level is "means", and it counts no
	// histogram.
	Rendered bool
	// Resident marks a source whose tiles are in memory already, read in
	// place: the viewer does not cache them again (§SD3).
	Resident bool
	// Provenance is where the samples come from.
	Provenance imaging.Provenance
}

// IsZero reports whether the info describes nothing.
func (inst SourceInfo) IsZero() bool { return inst.Width == 0 && inst.Height == 0 }

// Transform is plane i's value transform.
func (inst SourceInfo) Transform(i int) imaging.ValueTransform {
	if i >= 0 && i < len(inst.Transforms) && inst.Transforms[i].Scale != 0 {
		return inst.Transforms[i]
	}
	return imaging.IdentityTransform
}

// maxSourceLevels bounds the levels a source may declare each way: a
// halving 30 times reduces any image to one cell, and the view zooms in no
// further than maxZoom.
const maxSourceLevels = 30

// Validate refuses a description the viewer cannot show.
func (inst SourceInfo) Validate() error {
	switch {
	case inst.Width <= 0 || inst.Height <= 0:
		return eb.Build().Int("width", inst.Width).Int("height", inst.Height).Errorf("source has no size")
	case inst.Model.PlaneCount() == 0:
		return eb.Build().Errorf("source names no colour model")
	case inst.Kind == 0:
		return eb.Build().Errorf("source names no sample kind")
	case len(inst.Transforms) > inst.Model.PlaneCount():
		return eb.Build().Int("transforms", len(inst.Transforms)).Int("planes", inst.Model.PlaneCount()).Errorf("more value transforms than planes")
	case inst.Reduced < 0 || inst.Reduced > maxSourceLevels || inst.Magnified < 0 || inst.Magnified > maxZoom:
		return eb.Build().Int("reduced", inst.Reduced).Int("magnified", inst.Magnified).Errorf("source declares levels out of range")
	}
	return nil
}

// TileData is a tile's samples: Region is where in Samples the tile's
// cells are, the size of the tile's [TileRect]. A source may hand back a
// larger image it holds and the region of it, which costs no copy.
type TileData struct {
	Samples imaging.Image
	Region  image.Rectangle
}

// LevelSize is a level's size in its own cells: level −k is the reduction
// of [levels.Size], level +k the size times 2^k.
func LevelSize(width, height, level int) (w, h int) {
	if level <= 0 {
		return levels.Size(width, height, -level)
	}
	return width << level, height << level
}

// TileRect is the cells of level a tile covers, in that level's cells, cut at
// the level's edge; empty for a tile outside it.
func TileRect(info SourceInfo, level, col, row int) image.Rectangle {
	lw, lh := LevelSize(info.Width, info.Height, level)
	x0, y0 := col*tileSize, row*tileSize
	return image.Rect(x0, y0, x0+tileSize, y0+tileSize).Intersect(image.Rect(0, 0, lw, lh))
}

// checkTile refuses tile data that does not answer the request it was given.
func checkTile(info SourceInfo, d TileData, rect image.Rectangle) error {
	s := d.Samples
	switch {
	case s.IsZero():
		return eb.Build().Errorf("source answered a tile with no samples")
	case s.Model() != info.Model || s.Kind() != info.Kind:
		return eb.Build().Stringer("model", s.Model()).Stringer("kind", s.Kind()).
			Stringer("declared model", info.Model).Stringer("declared kind", info.Kind).
			Errorf("source answered a tile of another model or kind than it declared")
	case d.Region.Dx() != rect.Dx() || d.Region.Dy() != rect.Dy():
		return eb.Build().Stringer("region", d.Region).Stringer("tile", rect).Errorf("source answered a tile of another size than asked")
	case !d.Region.In(image.Rect(0, 0, s.Width(), s.Height())):
		return eb.Build().Stringer("region", d.Region).Int("width", s.Width()).Int("height", s.Height()).
			Errorf("source's tile region is not inside its samples")
	}
	return nil
}

// imageSource is an image in memory with the levels built from it so far:
// level 0 and a built level are read in place, a level not built yet is
// reduced for the tile asked for — the same cells a build gives.
type imageSource struct {
	img     imaging.Image
	pyramid *levels.Pyramid
}

var _ SourceI = (*imageSource)(nil)

func (inst *imageSource) Info() SourceInfo {
	tr := make([]imaging.ValueTransform, inst.img.PlaneCount())
	for i := range tr {
		tr[i] = inst.img.Plane(i).Transform()
	}
	return SourceInfo{
		Width: inst.img.Width(), Height: inst.img.Height(), Model: inst.img.Model(), Kind: inst.img.Kind(),
		Transforms: tr, Reduced: levels.CountFor(inst.img.Width(), inst.img.Height(), levels.Options{}),
		Resident: true, Provenance: inst.img.Provenance(),
	}
}

func (inst *imageSource) Tile(_ context.Context, level, col, row int) (d TileData, err error) {
	if level > 0 {
		return d, eb.Build().Int("level", level).Errorf("an image in memory has no magnified levels")
	}
	rect := TileRect(inst.Info(), level, col, row)
	k := -level
	if k == 0 {
		return TileData{Samples: inst.img, Region: rect}, nil
	}
	if lv, ok := inst.pyramid.Level(k); ok {
		return TileData{Samples: lv.Mean, Region: rect}, nil
	}
	planes := make([]imaging.Plane, inst.img.PlaneCount())
	for i := range planes {
		if planes[i], _, _, err = levels.Reduce(inst.img.Plane(i), k, rect); err != nil {
			return d, err
		}
	}
	reduced, err := imaging.NewImage(inst.img.Model(), planes, imaging.Provenance{})
	return TileData{Samples: reduced, Region: image.Rect(0, 0, rect.Dx(), rect.Dy())}, err
}

// DefaultSampleCacheBytes bounds the samples a viewer keeps of a source that
// is not resident, when [Options.SampleCacheBytes] does not say: 128 MiB,
// about 170 RGB tiles of 512 × 512 at 8 bits.
const DefaultSampleCacheBytes int64 = 128 << 20

// sampleCache keeps the tiles a non-resident source answered, so a recipe
// change re-maps them without asking the source again (ADR-0374 §SD3). It
// is bounded in bytes and evicts the least recently used. A generation in
// the key separates one source, or one content of it, from the next.
type sampleCache struct {
	mu      sync.Mutex
	limit   int64
	used    int64
	entries map[cacheKey]*list.Element
	order   list.List
}

type cacheKey struct {
	gen             uint64
	level, col, row int
}

type cacheEntry struct {
	key   cacheKey
	data  TileData
	bytes int64
}

// tileBytes is what a tile's samples cost: the region's cells, its planes,
// the sample kind's size.
func tileBytes(d TileData) int64 {
	per := int64(1)
	switch d.Samples.Kind() {
	case imaging.SampleKindU16:
		per = 2
	case imaging.SampleKindF32:
		per = 4
	}
	return int64(d.Region.Dx()) * int64(d.Region.Dy()) * int64(d.Samples.PlaneCount()) * per
}

func (inst *sampleCache) get(key cacheKey) (d TileData, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if e, found := inst.entries[key]; found {
		inst.order.MoveToFront(e)
		return e.Value.(*cacheEntry).data, true
	}
	return d, false
}

// put keeps a tile, evicting the least recently used until the bound
// holds. A tile larger than the whole bound is not kept.
func (inst *sampleCache) put(key cacheKey, d TileData, limit int64) {
	n := tileBytes(d)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.entries == nil {
		inst.entries = make(map[cacheKey]*list.Element, 64)
	}
	inst.limit = limit
	if n > limit {
		return
	}
	if e, found := inst.entries[key]; found {
		old := e.Value.(*cacheEntry)
		inst.used += n - old.bytes
		old.data, old.bytes = d, n
		inst.order.MoveToFront(e)
	} else {
		inst.entries[key] = inst.order.PushFront(&cacheEntry{key: key, data: d, bytes: n})
		inst.used += n
	}
	for inst.used > inst.limit {
		back := inst.order.Back()
		e := back.Value.(*cacheEntry)
		inst.order.Remove(back)
		delete(inst.entries, e.key)
		inst.used -= e.bytes
	}
}

// clear drops every tile.
func (inst *sampleCache) clear() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	clear(inst.entries)
	inst.order.Init()
	inst.used = 0
}

// usage is the bytes kept and the tiles they are in.
func (inst *sampleCache) usage() (bytes int64, tiles int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.used, len(inst.entries)
}
