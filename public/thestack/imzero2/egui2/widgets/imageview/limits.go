package imageview

import (
	"context"
	"image"
	"math"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/display"
)

// MaxZScaleSamples is the most grid points zscale limits are read from in a
// viewer: a sample that large holds a few megabytes of values, and zscale
// asks for a thousand by default.
const MaxZScaleSamples = 1 << 20

// allLayers is the target of limits asked for the single plane shown, or
// for every layer of a linked composite.
const allLayers = -1

// limitsWant is limits asked for that are found from the data off the
// frame: percentiles from a plane's histogram (ADR-0373 §SD4), zscale from a
// sample of its values (ADR-0401 §SD7). target is the composite layer they
// go to, or allLayers; plane is the plane they are found from; ask is the
// limits as asked, their Lo and Hi not read.
type limitsWant struct {
	target int
	plane  int
	ask    display.Limits
}

// zscaleKey names one zscale reading: the source's generation, the plane
// and the parameters asked for.
type zscaleKey struct {
	gen      uint64
	plane    int
	contrast float64
	samples  int32
}

func (inst zscaleKey) String() string {
	return strconv.FormatUint(inst.gen, 10) + "/" + strconv.Itoa(inst.plane) + "/" +
		strconv.FormatFloat(inst.contrast, 'g', -1, 64) + "/" + strconv.Itoa(int(inst.samples))
}

// zscaleFound is what a zscale reading found: limits, or ok false when the
// sample had no range.
type zscaleFound struct {
	limits display.Limits
	ok     bool
}

// SetRecipe shows the image through another recipe. One that cannot be
// compiled — limits that are not a range, a palette this package does not
// have — is refused and the view stays as it was. Of a composite it sets the
// recipe of the layer in hand, and of every layer when the composite is
// linked ([Viewer.SetLayerRecipe]).
//
// Limits whose Source is [display.LimitsSourcePercentile] or
// [display.LimitsSourceZScale] are found here, from PercentLo and PercentHi
// or from ZScaleContrast and ZScaleSamples; their Lo and Hi are not read.
// Percentiles need the histogram of the plane shown, and zscale a sample of
// its values at full resolution: for an image in memory zscale is found at
// once, and otherwise each is found off the frame — the rest of the recipe
// is applied at once and the limits by the Render that finds them,
// [Viewer.LimitsPending] reporting true meanwhile. zscale reads only the
// tiles its sample falls in, through the sample cache. Both are refused for
// a colour image as stored, whose limits are not a property of one plane;
// percentiles where the source counts no histogram, zscale for a rendered
// source, whose samples are drawn rather than measured. A later recipe of
// any other kind replaces limits still pending.
func (inst *Viewer) SetRecipe(recipe display.Recipe) error {
	if inst.info.IsZero() {
		return eb.Build().Errorf("no image to show a recipe on")
	}
	if inst.plane == asComposite {
		return inst.SetLayerRecipe(inst.comp.inHand, recipe)
	}
	return inst.setRecipeFor(allLayers, inst.shownPlaneIndex(), recipe)
}

// LimitsPending reports whether limits asked for through [Viewer.SetRecipe]
// — percentiles, zscale — wait for what they are found from; until they
// land, [Viewer.Recipe] carries the limits in force.
func (inst *Viewer) LimitsPending() bool { return len(inst.pending) > 0 }

// ZScaleJob is the sampling of a source's plane for zscale limits, as the
// standard job row draws it.
func (inst *Viewer) ZScaleJob() JobI { return &inst.zscaleJob }

// setRecipeFor sets a recipe on a target — the single plane, every layer of
// a linked composite, or one layer — finding its limits from plane when
// they are to be found from the data.
func (inst *Viewer) setRecipeFor(target, plane int, recipe display.Recipe) error {
	src := recipe.Limits.Source
	if src != display.LimitsSourcePercentile && src != display.LimitsSourceZScale {
		if err := inst.applyRecipeTo(target, recipe); err != nil {
			return err
		}
		inst.dropPending(target)
		return nil
	}
	ask := recipe.Limits
	switch src {
	case display.LimitsSourcePercentile:
		if !inst.measurable() {
			return eb.Build().Errorf("percentile limits apply to a single plane; show one first")
		}
		if !inst.histogramAvailable() {
			return eb.Build().Errorf("percentile limits need every sample at hand, which a source does not offer; set the limits")
		}
		lo, hi := ask.PercentLo, ask.PercentHi
		if !(lo >= 0 && lo < hi && hi <= 100) {
			return eb.Build().Float64("lo", lo).Float64("hi", hi).Errorf("percentiles must be an ascending pair within 0 and 100")
		}
		if h := inst.hists[plane]; h != nil {
			limits, ok := display.PercentileLimits(*h, lo, hi)
			if !ok {
				return eb.Build().Float64("lo", lo).Float64("hi", hi).Errorf("the two percentiles fall on one value; there is no range between them")
			}
			return inst.setFound(target, recipe, limits)
		}
	case display.LimitsSourceZScale:
		if !inst.measurable() {
			return eb.Build().Errorf("zscale limits apply to a single plane or a composite's layer; show one first")
		}
		if inst.info.Rendered {
			return eb.Build().Errorf("zscale limits are found from measured samples, and this source's are drawn; set the limits")
		}
		if !(ask.ZScaleContrast >= 0) || math.IsInf(ask.ZScaleContrast, 0) || ask.ZScaleSamples < 0 {
			return eb.Build().Float64("contrast", ask.ZScaleContrast).Int32("samples", ask.ZScaleSamples).
				Errorf("zscale needs a positive contrast and sample count, or 0 for the defaults")
		}
		if ask.ZScaleSamples > MaxZScaleSamples {
			return eb.Build().Int32("samples", ask.ZScaleSamples).Int("max", MaxZScaleSamples).
				Errorf("zscale reads at most 1048576 samples; ask for fewer, or 0 for the default of 1000")
		}
		found, have := inst.zscaleAt(plane, ask)
		if have {
			if !found.ok {
				return eb.Build().Int("plane", plane).Errorf("zscale found no range in the plane: its sampled values are one value, or none is valid; set the limits")
			}
			return inst.setFound(target, recipe, found.limits)
		}
	}
	// Until the limits are found the limits in force stay.
	recipe.Limits = inst.recipeOf(target).Limits
	if err := inst.applyRecipeTo(target, recipe); err != nil {
		return err
	}
	inst.dropPending(target)
	inst.pending = append(inst.pending, limitsWant{target: target, plane: plane, ask: ask})
	return nil
}

// setFound applies a recipe with the limits found for it.
func (inst *Viewer) setFound(target int, recipe display.Recipe, limits display.Limits) error {
	recipe.Limits = limits
	if err := inst.applyRecipeTo(target, recipe); err != nil {
		return err
	}
	inst.dropPending(target)
	return nil
}

// recipeOf is the recipe a target is shown through.
func (inst *Viewer) recipeOf(target int) display.Recipe {
	if inst.plane == asComposite && target >= 0 && target < len(inst.comp.layers) {
		return inst.comp.layers[target].Recipe
	}
	return inst.recipe
}

// dropPending forgets the limits pending for a target: of a single plane or
// a linked composite, all of them.
func (inst *Viewer) dropPending(target int) {
	if target == allLayers || inst.plane != asComposite || inst.comp.linked {
		inst.pending = inst.pending[:0]
		return
	}
	inst.pending = slices.DeleteFunc(inst.pending, func(w limitsWant) bool { return w.target == target })
}

// applyRecipeTo compiles a recipe with resolved limits for a target and
// shows it: the single plane, every layer of a linked composite, or one
// layer of an unlinked one.
func (inst *Viewer) applyRecipeTo(target int, recipe display.Recipe) error {
	if inst.plane != asComposite {
		if recipe == inst.recipe {
			return nil
		}
		if err := inst.publish(inst.plane, recipe); err != nil {
			return err
		}
		inst.m.InvalidateTiles()
		return nil
	}
	layers := slices.Clone(inst.comp.layers)
	changed := false
	for i := range layers {
		if (inst.comp.linked || target == allLayers || i == target) && layers[i].Recipe != recipe {
			layers[i].Recipe, changed = recipe, true
		}
	}
	if !changed {
		return nil
	}
	if err := inst.publishComposite(layers, inst.comp.linked, inst.comp.inHand); err != nil {
		return err
	}
	inst.m.InvalidateTiles()
	return nil
}

// resolvePending applies limits that were waiting for their histogram or
// their zscale sample once it is in, and asks for what the next one waits
// for. Limits the data has no range for leave the limits in force: the call
// that asked has returned, so there is no one to refuse.
func (inst *Viewer) resolvePending() {
	for k := 0; k < len(inst.pending); {
		w := inst.pending[k]
		var limits display.Limits
		ready, ok := false, false
		switch w.ask.Source {
		case display.LimitsSourcePercentile:
			if h := inst.hists[w.plane]; h != nil {
				ready = true
				limits, ok = display.PercentileLimits(*h, w.ask.PercentLo, w.ask.PercentHi)
			}
		case display.LimitsSourceZScale:
			var found zscaleFound
			if found, ready = inst.zscaleAt(w.plane, w.ask); ready {
				limits, ok = found.limits, found.ok
			}
		}
		if !ready {
			k++
			continue
		}
		inst.pending = slices.Delete(inst.pending, k, k+1)
		if !ok {
			continue
		}
		recipe := inst.recipeOf(w.target)
		recipe.Limits = limits
		_ = inst.applyRecipeTo(w.target, recipe)
	}
	if len(inst.pending) > 0 {
		inst.wantHistogram()
	}
}

// zscaleAt is the zscale reading of a plane with the parameters asked for:
// at once for an image in memory, and otherwise from the reading kept or the
// job's result, the job started when neither is at hand; have is false
// until it is. A reading that failed — its tiles could not be read, or it
// was cancelled — is no range.
func (inst *Viewer) zscaleAt(plane int, ask display.Limits) (found zscaleFound, have bool) {
	key := zscaleKey{gen: inst.gen, plane: plane, contrast: ask.ZScaleContrast, samples: ask.ZScaleSamples}
	if f, ok := inst.zscales[key]; ok {
		return f, true
	}
	if !inst.img.IsZero() {
		found.limits, found.ok = display.ZScaleLimits(inst.img.Plane(plane), ask.ZScaleContrast, int(ask.ZScaleSamples))
		inst.zscales[key] = found
		return found, true
	}
	sh := inst.src.cur.Load()
	if sh == nil || sh.source == nil {
		return found, false
	}
	ts := inst.src
	inst.zscaleJob.Configure(inst.Opts.Tasks, zscaleKind, zscaleTitle)
	val, done, err, busy := inst.zscaleJob.Demand(key.String(), func(ctx context.Context) (*zscaleFound, error) {
		return zscaleOfSource(ctx, ts, sh, plane, ask.ZScaleContrast, int(ask.ZScaleSamples))
	})
	if busy || !done {
		return found, false
	}
	if err == nil && val != nil {
		found = *val
	}
	inst.zscales[key] = found
	return found, true
}

// zscaleOfSource reads a source's plane at zscale's grid points
// ([display.ZScaleGrid]) and finds its limits. The points are collected
// first and each level-0 tile they fall in is read once, through the sample
// cache, so at most one tile's samples are held at a time beside the
// points' values; ctx is checked before every tile. A sample the source
// declares invalid is skipped as a plane's invalid sample is.
func zscaleOfSource(ctx context.Context, ts *tileSource, sh *shown, plane int, contrast float64, samples int) (*zscaleFound, error) {
	info := sh.info
	type point struct{ col, row int }
	byTile := make(map[image.Point][]point, 64)
	var order []image.Point
	for col, row := range display.ZScaleGrid(info.Width, info.Height, samples) {
		t := image.Pt(col/tileSize, row/tileSize)
		if _, seen := byTile[t]; !seen {
			order = append(order, t)
		}
		byTile[t] = append(byTile[t], point{col, row})
	}
	type reading struct {
		value float64
		valid bool
	}
	values := make(map[point]reading, len(order)*8)
	transform := info.Transform(plane)
	for _, t := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d, err := ts.tile(ctx, sh, 0, t.X, t.Y, TileRect(info, 0, t.X, t.Y))
		if err != nil {
			return nil, err
		}
		p := d.Samples.Plane(plane)
		for _, pt := range byTile[t] {
			at := d.Region.Min.Add(image.Pt(pt.col-t.X*tileSize, pt.row-t.Y*tileSize))
			stored, valid := p.StoredAt(at.X, at.Y)
			if info.declares(plane) && stored == info.Invalid {
				valid = false
			}
			values[pt] = reading{transform.Apply(stored), valid}
		}
	}
	limits, ok := display.ZScaleLimitsAt(info.Width, info.Height, func(col, row int) (float64, bool) {
		r := values[point{col, row}]
		return r.value, r.valid
	}, contrast, samples)
	return &zscaleFound{limits: limits, ok: ok}, nil
}

// ClippedShares is the share of the valid samples of the plane whose
// histogram the strip shows — the single plane, or a composite's layer in
// hand — that the limits in force clip, below the lower and above the upper
// (ADR-0401 §SD8), from the histogram already counted. ok is false until it
// is, and for a colour image as stored.
func (inst *Viewer) ClippedShares() (share display.ClippedShare, ok bool) {
	if !inst.measurable() {
		return share, false
	}
	h := inst.hists[inst.shownPlaneIndex()]
	if h == nil {
		return share, false
	}
	return display.ClippedShares(*h, inst.recipe.Limits)
}

// ClippedText says what limits clip, as the histogram strip writes it:
// "clipped 0.03 % below, 1.2 % above", each share with the digits that
// show it, and "about" before each when the shares are interpolated within
// a bin rather than counted.
func ClippedText(share display.ClippedShare) string {
	about := ""
	if !share.Exact {
		about = "about "
	}
	return "clipped " + about + formatShare(share.Below) + " below, " + about + formatShare(share.Above) + " above"
}

// formatShare writes a share as a percentage with the decimals that show
// its first significant digit, at least one for a share of a percent or
// more; nothing but 0 is written as 0, and nothing but the whole as 100.
func formatShare(share float64) string {
	p := share * 100
	switch {
	case !(p > 0):
		return "0 %"
	case p >= 100:
		return "100 %"
	case p < 1e-6:
		return "under 0.000001 %"
	case p > 99:
		// Enough decimals that what is not the whole does not read as it.
		d := min(int(-math.Floor(math.Log10(100-p))), 6)
		return strconv.FormatFloat(p, 'f', max(d, 1), 64) + " %"
	case p >= 1:
		return strconv.FormatFloat(p, 'f', 1, 64) + " %"
	}
	return strconv.FormatFloat(p, 'f', int(-math.Floor(math.Log10(p))), 64) + " %"
}

// clippedLine is the strip's statement of what is clipped, a composite's
// named by its layer in hand's plane.
func (inst *Viewer) clippedLine() (line string, ok bool) {
	share, ok := inst.ClippedShares()
	if !ok {
		return "", false
	}
	line = ClippedText(share)
	if inst.plane == asComposite {
		line = inst.info.PlaneLabel(inst.shownPlaneIndex()) + " · " + line
	}
	return line, true
}
