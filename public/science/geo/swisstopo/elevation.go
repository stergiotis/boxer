//go:build llm_generated_opus46

package swisstopo

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	defaultCacheSize   int32   = 64
	tilePixelSize      int32   = 500
	tileResolutionM    float64 = 2.0
	tileSizeM          float64 = 1000.0
)

// ElevationSampler provides elevation lookups from swissALTI3D 2m COG tiles.
type ElevationSampler struct {
	tilesDir string
	cache    *lru.Cache[string, []float32]
	// tileIndex maps grid key "EEEE-NNNN" to the absolute file path.
	// Built once at construction time by scanning the tiles directory;
	// avoids O(N) filepath.Glob calls on every Sample (N = total tiles).
	tileIndex map[string]string
}

// NewElevationSampler creates a new sampler that reads tiles from tilesDir.
// The directory is scanned once at construction time to build an in-memory
// tile index, so the call is O(N) in the number of tiles and may block on
// I/O — honour ctx cancellation in callers that mount the sampler from a
// UI thread.
func NewElevationSampler(ctx context.Context, tilesDir string) (inst *ElevationSampler, err error) {
	var cache *lru.Cache[string, []float32]
	cache, err = lru.New[string, []float32](int(defaultCacheSize))
	if err != nil {
		err = eh.Errorf("unable to create LRU cache: %w", err)
		return
	}
	inst = &ElevationSampler{
		tilesDir:  tilesDir,
		cache:     cache,
		tileIndex: make(map[string]string, 65536),
	}
	err = inst.buildTileIndex(ctx)
	if err != nil {
		inst = nil
		return
	}
	return
}

// buildTileIndex scans tilesDir for swissALTI3D tile files and populates
// tileIndex with grid-key → absolute-path mappings. The filename pattern is
// `swissalti3d_YYYY_EEEE-NNNN_2_2056_5728.tif`; the glob restricts shape so
// the post-glob split is safe.
func (inst *ElevationSampler) buildTileIndex(ctx context.Context) (err error) {
	pattern := filepath.Join(inst.tilesDir, "swissalti3d_*_????-????_2_2056_5728.tif")
	var matches []string
	matches, err = filepath.Glob(pattern)
	if err != nil {
		err = eb.Build().Str("tilesDir", inst.tilesDir).Errorf("unable to glob tiles directory %q: %w", inst.tilesDir, err)
		return
	}
	for i, path := range matches {
		// Cheap ctx check between batches so a cancel doesn't have to wait
		// for the full directory walk to drain.
		if i&0x3ff == 0 {
			err = ctx.Err()
			if err != nil {
				return
			}
		}
		base := filepath.Base(path)
		// Glob shape guarantees parts[0]="swissalti3d", parts[1]=YYYY,
		// parts[2]="EEEE-NNNN" (the bit we want).
		parts := strings.SplitN(base, "_", 4)
		if len(parts) < 3 {
			continue
		}
		inst.tileIndex[parts[2]] = path
	}
	return
}

// tileGridKey computes the km-grid key (EEEE, NNNN) for a given LV95 coordinate.
func tileGridKey(lv LV95Coord) (eKm int32, nKm int32) {
	eKm = int32(math.Floor(lv.E / tileSizeM))
	nKm = int32(math.Floor(lv.N / tileSizeM))
	return
}

// loadTile loads and caches the decoded pixel data for a given km-grid key.
func (inst *ElevationSampler) loadTile(eKm int32, nKm int32) (pixels []float32, err error) {
	key := fmt.Sprintf("%d-%d", eKm, nKm)

	// check cache first
	var ok bool
	pixels, ok = inst.cache.Get(key)
	if ok {
		return
	}

	// find tile file via index (O(1) lookup instead of filepath.Glob)
	tilePath, found := inst.tileIndex[key]
	if !found {
		err = eb.Build().Str("tilesDir", inst.tilesDir).Str("gridKey", key).Errorf("no tile file in %q for grid key %s", inst.tilesDir, key)
		return
	}

	pixels, err = readSwissALTI3DTile(tilePath)
	if err != nil {
		err = eh.Errorf("unable to read tile %s: %w", tilePath, err)
		return
	}

	inst.cache.Add(key, pixels)
	return
}

// Sample returns the terrain elevation at the given LV95 coordinate.
func (inst *ElevationSampler) Sample(lv LV95Coord) (elevation float32, err error) {
	eKm, nKm := tileGridKey(lv)

	var pixels []float32
	pixels, err = inst.loadTile(eKm, nKm)
	if err != nil {
		err = eh.Errorf("unable to load tile for %s: %w", lv, err)
		return
	}

	// pixel coordinates within the tile
	// E = EEEE*1000 + col*2  =>  col = (E - EEEE*1000) / 2
	// N = (NNNN+1)*1000 - row*2  =>  row = ((NNNN+1)*1000 - N) / 2
	tileOriginE := float64(eKm) * tileSizeM
	tileOriginN := float64(nKm+1) * tileSizeM

	col := int32(math.Floor((lv.E - tileOriginE) / tileResolutionM))
	row := int32(math.Floor((tileOriginN - lv.N) / tileResolutionM))

	// clamp to valid range
	if col < 0 {
		col = 0
	}
	if col >= tilePixelSize {
		col = tilePixelSize - 1
	}
	if row < 0 {
		row = 0
	}
	if row >= tilePixelSize {
		row = tilePixelSize - 1
	}

	elevation = pixels[row*tilePixelSize+col]
	return
}

// SampleProfile samples elevation along the line from->to at the given step interval.
// Returns parallel arrays of (distance from start, elevation).
func (inst *ElevationSampler) SampleProfile(from LV95Coord, to LV95Coord, stepMeters float64) (distances []float64, elevations []float32, err error) {
	dE := to.E - from.E
	dN := to.N - from.N
	totalDist := math.Sqrt(dE*dE + dN*dN)

	if totalDist < 1e-6 {
		// from and to are the same point
		distances = make([]float64, 1)
		elevations = make([]float32, 1)
		elevations[0], err = inst.Sample(from)
		if err != nil {
			err = eh.Errorf("unable to sample elevation at %s: %w", from, err)
			return
		}
		return
	}

	numSteps := int64(math.Ceil(totalDist / stepMeters))
	numPoints := numSteps + 1

	distances = make([]float64, 0, numPoints)
	elevations = make([]float32, 0, numPoints)

	// unit direction vector
	uE := dE / totalDist
	uN := dN / totalDist

	for i := int64(0); i <= numSteps; i++ {
		dist := float64(i) * stepMeters
		if dist > totalDist {
			dist = totalDist
		}

		pt := LV95Coord{
			E: from.E + uE*dist,
			N: from.N + uN*dist,
		}

		var elev float32
		elev, err = inst.Sample(pt)
		if err != nil {
			err = eh.Errorf("unable to sample elevation at dist=%.1f (%s): %w", dist, pt, err)
			return
		}

		distances = append(distances, dist)
		elevations = append(elevations, elev)
	}

	return
}
