package imageview

import (
	"context"
	"image"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// HistogramSourceI is a source that can count a plane's histogram over the
// whole image itself, without the viewer holding every sample: the viewer
// then offers the histogram strip and percentile limits for it, as it does
// for an image in memory.
type HistogramSourceI interface {
	SourceI
	Histogram(ctx context.Context, plane int) (stats.Histogram, error)
}

// RangeSourceI is a source that knows each plane's range of stored samples,
// so a single plane opens on its data range rather than its kind's nominal
// one. ok is false while the range is not known yet.
type RangeSourceI interface {
	SourceI
	StoredRange(plane int) (lo, hi float64, ok bool)
}

// tryTileI is a source that can answer a tile at once when it has it, which
// the readout uses so the frame never waits.
type tryTileI interface {
	tryTile(level, col, row int) (TileData, bool)
}

// StoreSource shows a tiles store — the one the decoder fills — as a
// resident source with its reduced levels (ADR-0374 §SD7). A store still
// being built is shown as it fills: a tile not built yet is waited for on
// the loader's workers, and appears when it is.
func StoreSource(s *tiles.Store) SourceI { return &storeSource{s: s} }

type storeSource struct{ s *tiles.Store }

var (
	_ HistogramSourceI = (*storeSource)(nil)
	_ RangeSourceI     = (*storeSource)(nil)
)

func (inst *storeSource) Info() SourceInfo {
	s := inst.s
	tr := make([]imaging.ValueTransform, s.Model().PlaneCount())
	for i := range tr {
		tr[i] = s.Transform(i)
	}
	return SourceInfo{Width: s.Width(), Height: s.Height(), Model: s.Model(), Kind: s.Kind(), Transforms: tr,
		Reduced: s.Reduced(), Resident: true, Provenance: s.Provenance()}
}

func (inst *storeSource) Tile(ctx context.Context, level, col, row int) (d TileData, err error) {
	t, err := inst.s.Tile(ctx, -level, col, row)
	if err != nil {
		return d, err
	}
	return TileData{Samples: t.Mean, Region: image.Rect(0, 0, t.Mean.Width(), t.Mean.Height())}, nil
}

func (inst *storeSource) tryTile(level, col, row int) (d TileData, ok bool) {
	t, ok := inst.s.TryTile(-level, col, row)
	if !ok {
		return d, false
	}
	return TileData{Samples: t.Mean, Region: image.Rect(0, 0, t.Mean.Width(), t.Mean.Height())}, true
}

func (inst *storeSource) Histogram(ctx context.Context, plane int) (stats.Histogram, error) {
	return inst.s.Histogram(ctx, plane, image.Rect(0, 0, inst.s.Width(), inst.s.Height()))
}

func (inst *storeSource) StoredRange(plane int) (lo, hi float64, ok bool) {
	return inst.s.StoredRange(plane)
}
