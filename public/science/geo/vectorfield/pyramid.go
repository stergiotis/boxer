package vectorfield

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Grid is one step's native planes as a loader hands them over: rows north to
// south, columns west to east, earth-relative east and north components, NaN
// for missing. West and North are the position of sample (0, 0).
//
// A grid that repeats its first column as its last — a gridline-registered
// file spanning the full 360° — is accepted, and the repeat is dropped.
type Grid struct {
	West, North float64
	DLon, DLat  float64
	Cols, Rows  int
	U, V        []float32
}

// StepLoaderI reads one step's native planes. It is called off the render
// thread, at most once at a time per step, and the pyramid keeps what it
// returns: the loader must not reuse the slices.
type StepLoaderI interface {
	LoadStepE(ctx context.Context, step int) (grid Grid, err error)
}

// PyramidOptions tunes a [Pyramid]; the zero value is usable.
type PyramidOptions struct {
	// CacheBytes bounds the decoded steps held in memory, in bytes and not in
	// entries: a regional model's step is over a hundred megabytes. The two
	// most recently used steps are kept whatever the budget, since a renderer
	// blends between two. Zero takes 1 GiB.
	CacheBytes int64
}

const (
	defaultCacheBytes    = int64(1) << 30
	defaultValidFraction = float32(0.5)
	// gutter is how many samples a window reaches beyond the requested bounds.
	gutter = 1
	// minLevelSize stops the halving: below this a level is a handful of
	// samples and a further one serves nothing a reduction on the fly cannot.
	minLevelSize = 8
	// periodTolerance is the slack, in cells, within which a grid's width is
	// taken to be the full circle.
	periodTolerance = 1e-6
)

// level is one resolution of one step.
type level struct {
	west, north float64
	dLon, dLat  float64
	cols, rows  int
	u, v, s     []float32
	// c is how much of what each sample covers was valid, 0 to 1. nil when
	// the step has no missing sample, which then costs nothing.
	c []float32
}

// Pyramid is a [SourceI] over steps held in memory, each halved level by
// level (ADR-0249 SD2).
//
// A level's sample is the mean of the four below it, weighted by how much of
// each was valid, so a mean over missing data composes from level to level.
// A sample is served as valid when at least Meta.ValidFraction of what it
// covers was: "any valid" would flood an ocean current over the land as
// levels coarsen, "all valid" would erode the coast.
//
// A box mean puts a coarse sample at the centre of its children, so each
// level has an origin of its own. An odd row or column count leaves a last
// sample that averages what exists. A periodic grid halves in longitude only
// while its column count is even, so the seam stays a sample boundary; past
// that, and for a request coarser than the top level, the window is reduced
// on the fly by the same weighted mean.
type Pyramid struct {
	meta   Meta
	loader StepLoaderI
	budget int64

	mu      sync.Mutex
	entries map[int]*stepEntry
	clock   uint64
	held    int64
	serial  uint64
}

type stepEntry struct {
	ready   chan struct{} // closed when levels and err are set
	levels  []level
	err     error
	bytes   int64
	used    uint64
	version uint64
}

var _ SourceI = (*Pyramid)(nil)

// NewPyramidE builds a pyramid over loader. meta names the field and lists
// its steps; its geometry (West, East, South, North, DLon, DLat, PeriodicLon)
// is taken from the first step loaded when left zero, and every later step
// must match it. A zero ValidFraction takes one half.
func NewPyramidE(ctx context.Context, meta Meta, loader StepLoaderI, opts PyramidOptions) (inst *Pyramid, err error) {
	if loader == nil {
		err = eh.Errorf("a pyramid needs a step loader")
		return
	}
	if len(meta.Steps) == 0 {
		err = eh.Errorf("a pyramid needs at least one step")
		return
	}
	if meta.ValidFraction <= 0 || meta.ValidFraction > 1 {
		meta.ValidFraction = defaultValidFraction
	}
	budget := opts.CacheBytes
	if budget <= 0 {
		budget = defaultCacheBytes
	}
	inst = &Pyramid{
		meta:    meta,
		loader:  loader,
		budget:  budget,
		entries: make(map[int]*stepEntry, 4),
	}
	if meta.DLon > 0 && meta.DLat > 0 {
		return
	}
	// Geometry was left to the data: read the first step that exists.
	var entry *stepEntry
	for step := range meta.Steps {
		entry, err = inst.entryE(ctx, step)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrStepMissing) {
			inst = nil
			return
		}
	}
	if err != nil {
		inst = nil
		return
	}
	base := &entry.levels[0]
	inst.mu.Lock()
	inst.meta.West = base.west
	inst.meta.North = base.north
	inst.meta.DLon = base.dLon
	inst.meta.DLat = base.dLat
	inst.meta.East = base.west + float64(base.cols-1)*base.dLon
	inst.meta.South = base.north - float64(base.rows-1)*base.dLat
	inst.meta.PeriodicLon = isPeriodic(base.cols, base.dLon)
	inst.mu.Unlock()
	return
}

// Describe implements [SourceI].
func (inst *Pyramid) Describe() (meta Meta) {
	inst.mu.Lock()
	meta = inst.meta
	inst.mu.Unlock()
	return
}

// HeldBytes is how much decoded data the cache holds.
func (inst *Pyramid) HeldBytes() (held int64) {
	inst.mu.Lock()
	held = inst.held
	inst.mu.Unlock()
	return
}

// PrefetchE loads a step so that a later SampleE finds it; a renderer calls it
// for the next step in the direction of play.
func (inst *Pyramid) PrefetchE(ctx context.Context, step int) (err error) {
	_, err = inst.entryE(ctx, step)
	return
}

// SampleE implements [SourceI].
func (inst *Pyramid) SampleE(ctx context.Context, req Request) (win Window, err error) {
	if !(req.West < req.East) || !(req.South < req.North) {
		err = eb.Build().
			Float64("west", req.West).Float64("east", req.East).
			Float64("south", req.South).Float64("north", req.North).
			Errorf("a window request needs west < east and south < north")
		return
	}
	if req.MaxCols < 2 || req.MaxRows < 2 {
		err = eb.Build().Int("maxCols", req.MaxCols).Int("maxRows", req.MaxRows).
			Errorf("a window request needs room for at least two columns and two rows")
		return
	}
	var entry *stepEntry
	entry, err = inst.entryE(ctx, req.Step)
	if err != nil {
		return
	}
	meta := inst.Describe()

	// The coarsest spacing that still puts MaxCols columns across the span;
	// serve from the finest level no finer than that, so the window is never
	// finer than asked and at most twice as coarse.
	wantLon := (req.East - req.West) / float64(req.MaxCols)
	wantLat := (req.North - req.South) / float64(req.MaxRows)
	k := 0
	for k+1 < len(entry.levels) && (entry.levels[k].dLon < wantLon || entry.levels[k].dLat < wantLat) {
		k++
	}
	lv := &entry.levels[k]
	fx := reduction(wantLon, lv.dLon)
	fy := reduction(wantLat, lv.dLat)
	periodic := meta.PeriodicLon && isPeriodic(lv.cols, lv.dLon)

	win = extract(lv, req, fx, fy, periodic, meta.ValidFraction)
	win.Step = req.Step
	win.Level = k
	win.Version = entry.version
	return
}

// reduction is the whole factor by which a level still has to be reduced on
// the fly to be no finer than want.
func reduction(want, have float64) (factor int) {
	factor = int(math.Ceil(want/have - 1e-9))
	if factor < 1 {
		factor = 1
	}
	return
}

func isPeriodic(cols int, dLon float64) (periodic bool) {
	return math.Abs(float64(cols)*dLon-360) <= periodTolerance*dLon
}

// extract copies a window out of a level, reducing it by whole factors with
// the same coverage-weighted mean the levels are built with. Columns are
// addressed in the request's unwrapped frame and folded onto a periodic
// level, so a window across the seam comes out contiguous.
func extract(lv *level, req Request, fx, fy int, periodic bool, validFraction float32) (win Window) {
	// Group g covers level columns [g*fx, g*fx+fx) and sits at their centre.
	groupLon := func(g int) float64 {
		return lv.west + (float64(g*fx)+float64(fx-1)/2)*lv.dLon
	}
	groupLat := func(g int) float64 {
		return lv.north - (float64(g*fy)+float64(fy-1)/2)*lv.dLat
	}
	stepLon := float64(fx) * lv.dLon
	stepLat := float64(fy) * lv.dLat

	// The groups at or just outside each bound, then the gutter.
	g0 := int(math.Floor((req.West-groupLon(0))/stepLon)) - gutter
	g1 := int(math.Ceil((req.East-groupLon(0))/stepLon)) + gutter
	r0 := int(math.Floor((groupLat(0)-req.North)/stepLat)) - gutter
	r1 := int(math.Ceil((groupLat(0)-req.South)/stepLat)) + gutter

	lastRow := (lv.rows - 1) / fy
	r0 = max(r0, 0)
	r1 = min(r1, lastRow)
	if periodic {
		// One turn and its gutters; more would repeat itself. A renderer whose
		// view shows the world more than once folds longitudes into the window.
		turn := int(math.Ceil(360/stepLon)) + 2*gutter + 1
		if g1-g0+1 > turn {
			g1 = g0 + turn - 1
		}
	} else {
		g0 = max(g0, 0)
		g1 = min(g1, (lv.cols-1)/fx)
	}
	if g1 < g0 || r1 < r0 {
		return
	}

	win.Cols = g1 - g0 + 1
	win.Rows = r1 - r0 + 1
	win.West = groupLon(g0)
	win.North = groupLat(r0)
	win.DLon = stepLon
	win.DLat = stepLat
	n := win.Cols * win.Rows
	win.U = make([]float32, n)
	win.V = make([]float32, n)
	win.Speed = make([]float32, n)
	nan := float32(math.NaN())

	for wr := range win.Rows {
		rowBase := (r0 + wr) * fy
		for wc := range win.Cols {
			colBase := (g0 + wc) * fx
			var su, sv, ss, sc float32
			cells := 0
			for dy := range fy {
				row := rowBase + dy
				if row >= lv.rows {
					break
				}
				for dx := range fx {
					col := colBase + dx
					if periodic {
						col = ((col % lv.cols) + lv.cols) % lv.cols
					} else if col >= lv.cols {
						break
					}
					i := row*lv.cols + col
					cells++
					w := float32(1)
					if lv.c != nil {
						w = lv.c[i]
					}
					if w <= 0 {
						continue
					}
					su += w * lv.u[i]
					sv += w * lv.v[i]
					ss += w * lv.s[i]
					sc += w
				}
			}
			o := wr*win.Cols + wc
			if cells == 0 || sc <= 0 || sc/float32(cells) < validFraction {
				win.U[o], win.V[o], win.Speed[o] = nan, nan, nan
				continue
			}
			win.U[o] = su / sc
			win.V[o] = sv / sc
			win.Speed[o] = ss / sc
		}
	}
	return
}

// buildLevelsE turns a loaded grid into the step's levels.
func buildLevelsE(grid Grid, meta *Meta) (levels []level, err error) {
	n := grid.Cols * grid.Rows
	if grid.Cols < 2 || grid.Rows < 2 || !(grid.DLon > 0) || !(grid.DLat > 0) {
		err = eb.Build().Int("cols", grid.Cols).Int("rows", grid.Rows).
			Float64("dLon", grid.DLon).Float64("dLat", grid.DLat).
			Errorf("a step's grid needs at least two columns and rows and positive spacing")
		return
	}
	if len(grid.U) != n || len(grid.V) != n {
		err = eb.Build().Int("cols", grid.Cols).Int("rows", grid.Rows).
			Int("lenU", len(grid.U)).Int("lenV", len(grid.V)).
			Errorf("a step's planes do not match its dimensions")
		return
	}
	base := level{
		west: grid.West, north: grid.North,
		dLon: grid.DLon, dLat: grid.DLat,
		cols: grid.Cols, rows: grid.Rows,
		u: grid.U, v: grid.V,
	}
	// A repeated cyclic column is dropped, never counted twice.
	if math.Abs(float64(base.cols-1)*base.dLon-360) <= periodTolerance*base.dLon {
		base = dropLastColumn(base)
	}
	if meta.DLon > 0 && meta.DLat > 0 {
		if base.cols != int(math.Round((meta.East-meta.West)/meta.DLon))+1 ||
			base.rows != int(math.Round((meta.North-meta.South)/meta.DLat))+1 ||
			math.Abs(base.west-meta.West) > meta.DLon*1e-6 ||
			math.Abs(base.north-meta.North) > meta.DLat*1e-6 {
			err = eb.Build().Int("cols", base.cols).Int("rows", base.rows).
				Float64("west", base.west).Float64("north", base.north).
				Errorf("a step's grid differs from the field's geometry")
			return
		}
	}

	n = base.cols * base.rows
	base.s = make([]float32, n)
	missing := false
	for i := range n {
		u, v := base.u[i], base.v[i]
		if u != u || v != v || math.IsInf(float64(u), 0) || math.IsInf(float64(v), 0) {
			missing = true
			continue
		}
		base.s[i] = float32(math.Hypot(float64(u), float64(v)))
	}
	if missing {
		// One spelling of missing from here on: no coverage, and zeros in the
		// planes so that a weighted sum never meets a NaN.
		base.c = make([]float32, n)
		for i := range n {
			u, v := base.u[i], base.v[i]
			if u != u || v != v || math.IsInf(float64(u), 0) || math.IsInf(float64(v), 0) {
				base.u[i], base.v[i], base.s[i] = 0, 0, 0
				continue
			}
			base.c[i] = 1
		}
	}

	levels = make([]level, 0, 8)
	levels = append(levels, base)
	for {
		cur := &levels[len(levels)-1]
		if min(cur.cols, cur.rows) < 2*minLevelSize {
			break
		}
		// Halving an odd periodic level would put the seam inside a sample.
		if isPeriodic(cur.cols, cur.dLon) && cur.cols%2 != 0 {
			break
		}
		levels = append(levels, halve(cur))
	}
	return
}

func dropLastColumn(lv level) (out level) {
	out = lv
	out.cols = lv.cols - 1
	out.u = make([]float32, out.cols*lv.rows)
	out.v = make([]float32, out.cols*lv.rows)
	for r := range lv.rows {
		copy(out.u[r*out.cols:(r+1)*out.cols], lv.u[r*lv.cols:r*lv.cols+out.cols])
		copy(out.v[r*out.cols:(r+1)*out.cols], lv.v[r*lv.cols:r*lv.cols+out.cols])
	}
	return
}

// halve builds the next level: each sample the coverage-weighted mean of the
// up to four below it, its coverage the mean of theirs over those that exist.
func halve(lv *level) (out level) {
	out.cols = (lv.cols + 1) / 2
	out.rows = (lv.rows + 1) / 2
	out.dLon = 2 * lv.dLon
	out.dLat = 2 * lv.dLat
	out.west = lv.west + lv.dLon/2
	out.north = lv.north - lv.dLat/2
	n := out.cols * out.rows
	out.u = make([]float32, n)
	out.v = make([]float32, n)
	out.s = make([]float32, n)
	if lv.c != nil {
		out.c = make([]float32, n)
	}
	for r := range out.rows {
		for c := range out.cols {
			var su, sv, ss, sc float32
			cells := 0
			for dy := range 2 {
				row := 2*r + dy
				if row >= lv.rows {
					break
				}
				for dx := range 2 {
					col := 2*c + dx
					if col >= lv.cols {
						break
					}
					i := row*lv.cols + col
					cells++
					w := float32(1)
					if lv.c != nil {
						w = lv.c[i]
					}
					su += w * lv.u[i]
					sv += w * lv.v[i]
					ss += w * lv.s[i]
					sc += w
				}
			}
			o := r*out.cols + c
			if sc > 0 {
				out.u[o] = su / sc
				out.v[o] = sv / sc
				out.s[o] = ss / sc
			}
			if out.c != nil {
				out.c[o] = sc / float32(cells)
			}
		}
	}
	return
}

func levelsBytes(levels []level) (bytes int64) {
	for i := range levels {
		lv := &levels[i]
		bytes += int64(len(lv.u)+len(lv.v)+len(lv.s)+len(lv.c)) * 4
	}
	return
}

// entryE returns a step's levels, loading them if they are not held. Loading
// happens outside the lock; a second caller for the same step waits for the
// first.
func (inst *Pyramid) entryE(ctx context.Context, step int) (entry *stepEntry, err error) {
	if step < 0 || step >= len(inst.meta.Steps) {
		err = eb.Build().Int("step", step).Int("steps", len(inst.meta.Steps)).Errorf("%w", ErrStepOutOfRange)
		return
	}
	inst.mu.Lock()
	entry = inst.entries[step]
	owner := entry == nil
	if owner {
		entry = &stepEntry{ready: make(chan struct{})}
		inst.entries[step] = entry
	}
	inst.clock++
	entry.used = inst.clock
	meta := inst.meta
	inst.mu.Unlock()

	if owner {
		var grid Grid
		grid, err = inst.loader.LoadStepE(ctx, step)
		if err == nil {
			entry.levels, err = buildLevelsE(grid, &meta)
		}
		entry.err = err
		inst.mu.Lock()
		if err != nil {
			// A failed load is not held: the step may exist later.
			delete(inst.entries, step)
		} else {
			inst.serial++
			entry.version = inst.serial
			entry.bytes = levelsBytes(entry.levels)
			inst.held += entry.bytes
			inst.evictLocked()
		}
		inst.mu.Unlock()
		close(entry.ready)
		if err != nil {
			entry = nil
		}
		return
	}

	select {
	case <-entry.ready:
	case <-ctx.Done():
		err = eh.Errorf("waiting for a step to load: %w", ctx.Err())
		entry = nil
		return
	}
	if entry.err != nil {
		err = entry.err
		entry = nil
	}
	return
}

// evictLocked drops the least recently used steps until the cache fits its
// budget, sparing the two most recent.
func (inst *Pyramid) evictLocked() {
	for inst.held > inst.budget && len(inst.entries) > 2 {
		oldest := -1
		var oldestUsed uint64
		newest, second := uint64(0), uint64(0)
		for _, e := range inst.entries {
			if e.used > newest {
				newest, second = e.used, newest
			} else if e.used > second {
				second = e.used
			}
		}
		for step, e := range inst.entries {
			if e.bytes == 0 || e.used >= second {
				continue // still loading, or one of the two most recent
			}
			if oldest < 0 || e.used < oldestUsed {
				oldest, oldestUsed = step, e.used
			}
		}
		if oldest < 0 {
			return
		}
		inst.held -= inst.entries[oldest].bytes
		delete(inst.entries, oldest)
	}
}
