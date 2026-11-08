package trailviewer

import (
	"iter"
	"math"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
)

// Time navigation (ADR-0350 §SD13, proposed): the window is one value with
// several faces — the picker, the context band, the detail strip, the
// navigation row's buttons, the history — and every face goes through the
// one set_range command (setWindowGesture). Nothing here reads: a gesture
// changes the window, the cache's settle window follows it, and one read
// lands later. This file holds the arithmetic, pure and table-tested; the
// rendering that calls it is in trailviewer.go.

const (
	// contextFactor is the context extent's span as a multiple of the
	// window's.
	contextFactor = 5
	// The extent is recomputed when the window's bounds leave its middle
	// three fifths or the span ratio leaves [contextRatioMin, contextRatioMax].
	contextRatioMin = 3.0
	contextRatioMax = 8.0
	// historyDepth bounds the windows Back can return to.
	historyDepth = 16
	// zoomInMin is the narrowest window Zoom in makes: ten of the smallest
	// strip step.
	zoomInMin = 10 * time.Second
	// bandHeight is the context band's height.
	bandHeight = 80
	// detailLaneHeight sizes the detail strip's lanes so six fit about
	// 100 px with the default gap.
	detailLaneHeight = 13
	// detailIntensityFloor is where the detail strip's counts start on the
	// opacity scale, so a bucket of one row is visible and still reads as
	// less than a bucket of a hundred. The timeline's density rug starts at
	// the same place (rugDensityMinTint).
	detailIntensityFloor = 0.3
	// outOfHoursAlpha is the band tint's alpha behind the detail strip.
	outOfHoursAlpha = 0x22
	// outOfHoursMaxDays bounds the bands yielded for one view.
	outOfHoursMaxDays = 400
)

// liveSpans are the spans Live rounds to.
var liveSpans = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour,
	12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour}

// --- the context extent ---

// contextExtent places the context extent for a window: contextFactor times
// the window's span, centred on it, then shifted left so its end is at
// most now plus one step — an audit trail has no future, so a window that
// ends now sits in the right fifth. clamped says the shift happened.
func contextExtent(from, to, now time.Time, step time.Duration) (cf, ct time.Time, clamped bool) {
	span := to.Sub(from)
	ext := contextFactor * span
	cf = from.Add(-(ext - span) / 2)
	ct = cf.Add(ext)
	if limit := now.Add(step); ct.After(limit) {
		ct, cf, clamped = limit, limit.Add(-ext), true
	}
	return
}

// contextState is the extent the window's reads use, kept on the App with
// hysteresis so panning by the band is continuous.
type contextState struct {
	from, to time.Time
	clamped  bool
	has      bool
}

// stale reports whether the window has left the extent's middle three
// fifths or its span ratio left [contextRatioMin, contextRatioMax]. An
// extent clamped at now has the window in its right fifth by construction,
// so its right-hand test is the extent's end itself: a relative window then
// recomputes once per step, as now passes it, not every frame.
func (inst contextState) stale(from, to time.Time) bool {
	if !inst.has {
		return true
	}
	ext, span := inst.to.Sub(inst.from), to.Sub(from)
	if span <= 0 || ext <= 0 {
		return true
	}
	if ratio := float64(ext) / float64(span); ratio < contextRatioMin || ratio > contextRatioMax {
		return true
	}
	fifth := ext / contextFactor
	if from.Before(inst.from.Add(fifth)) {
		return true
	}
	right := inst.to.Add(-fifth)
	if inst.clamped {
		right = inst.to
	}
	return to.After(right)
}

// updateContext recomputes the extent when the resolved window has left
// it. A relative window re-resolves every frame, so the extent follows now
// one step at a time.
func (inst *App) updateContext(now time.Time) {
	from, to := inst.window.Resolve(now)
	if !inst.ctx.stale(from, to) {
		return
	}
	step := stepFor(contextFactor*to.Sub(from), stripBuckets(inst.stripW))
	cf, ct, clamped := contextExtent(from, to, now, step)
	inst.ctx = contextState{from: cf, to: ct, clamped: clamped, has: true}
}

// sumBuckets folds the strip's (bucket, key) rows to one count per bucket,
// ascending, the key dropped.
func sumBuckets(buckets []Bucket) (out []Bucket) {
	per := map[int64]uint64{}
	for _, b := range buckets {
		per[b.Bucket] += b.N
	}
	out = make([]Bucket, 0, len(per))
	for k, n := range per {
		out = append(out, Bucket{Bucket: k, N: n})
	}
	slices.SortFunc(out, func(a, b Bucket) int { return int(a.Bucket - b.Bucket) })
	return
}

// --- the band's steps ---

// contextSteps is the band's steps: one per bucket of the read context
// extent, empty buckets included so the axis is regular, each held with
// the bucket's count; while loading, the steps inside [loadFrom, loadTo)
// are loading — what the move costs. dst is reused.
func contextSteps(ctxFrom, ctxTo time.Time, step time.Duration, counts []Bucket, loading bool, loadFrom, loadTo time.Time, dst []timescrubber.Step) []timescrubber.Step {
	dst = dst[:0]
	if step <= 0 || !ctxTo.After(ctxFrom) {
		return dst
	}
	per := make(map[int64]uint64, len(counts))
	for _, b := range counts {
		per[b.Bucket] += b.N
	}
	bound := 4 * stripBuckets(math.MaxFloat32)
	nan := float32(math.NaN())
	for at := bucketStart(float64(ctxFrom.Unix()), step); at.Before(ctxTo) && len(dst) < bound; at = at.Add(step) {
		s := timescrubber.Step{At: at, State: timescrubber.StepStateHeld, Value: float32(per[at.Unix()]), Peak: nan}
		if loading && at.Add(step).After(loadFrom) && at.Before(loadTo) {
			s.State = timescrubber.StepStateLoading
		}
		dst = append(dst, s)
	}
	return dst
}

// stepIndexOf is the step whose bucket holds t: the last step at or before
// it, clamped to the steps; -1 with none.
func stepIndexOf(steps []timescrubber.Step, t time.Time) int {
	n := len(steps)
	if n == 0 {
		return -1
	}
	i := sort.Search(n, func(i int) bool { return steps[i].At.After(t) }) - 1
	return min(max(i, 0), n-1)
}

// windowToRange snaps a window to the steps enclosing it: lo holds from, hi
// holds the last instant before to.
func windowToRange(steps []timescrubber.Step, from, to time.Time) (lo, hi int) {
	return stepIndexOf(steps, from), stepIndexOf(steps, to.Add(-time.Nanosecond))
}

// rangeToWindow is the fixed window steps lo through hi cover.
func rangeToWindow(steps []timescrubber.Step, lo, hi int, step time.Duration) Window {
	return Window{From: steps[lo].At, To: steps[hi].At.Add(step)}
}

// inBucket reports whether t lies in the bucket starting at at.
func inBucket(t, at time.Time, step time.Duration) bool {
	return !t.Before(at) && t.Before(at.Add(step))
}

// newestRowIn is the newest held row in [from, to); the rows are newest
// first.
func newestRowIn(rows []Row, from, to time.Time) (id uint64, ok bool) {
	for i := range rows {
		if ts := rows[i].Ts(); !ts.Before(from) && ts.Before(to) {
			return rows[i].Id, true
		}
	}
	return
}

// --- the band's commit rule ---

// bandRange is the band's range as the transport holds it, normalised: off
// carries no ends.
type bandRange struct {
	on     bool
	lo, hi int
}

func rangeOf(t *timescrubber.Transport, n int) (r bandRange) {
	if !t.RangeOn {
		return
	}
	r.lo, r.hi = t.Bounds(n)
	r.on = r.lo < r.hi
	if !r.on {
		r.lo, r.hi = 0, 0
	}
	return
}

// bandActionE is what one frame does with the band.
type bandActionE uint8

const (
	bandNothing bandActionE = iota
	// bandMirror writes the window onto the band: the host's turn.
	bandMirror
	// bandCommitRange makes the band's range the window: the person's turn.
	bandCommitRange
	// bandCommitContext makes the context extent the window: the person
	// cleared the range.
	bandCommitContext
)

// decideBand is the rule that keeps the band and the window one value
// without the host clobbering a gesture: while dragging, nothing; when the
// window or the steps changed since the last mirror, mirror; when the
// band's range differs from what was last mirrored, the person moved it,
// and it commits once — as a window, or as the context extent when the
// range was cleared.
func decideBand(dragging bool, cur, mirrored bandRange, changed bool) bandActionE {
	switch {
	case dragging:
		return bandNothing
	case changed:
		return bandMirror
	case cur == mirrored:
		return bandNothing
	case cur.on:
		return bandCommitRange
	case mirrored.on:
		return bandCommitContext
	}
	return bandNothing
}

// --- the navigation row ---

// shiftWindow is Earlier (dir < 0) or Later (dir > 0): the resolved window
// moved by half its span, fixed. Later never passes now; at now it does
// nothing.
func shiftWindow(from, to, now time.Time, dir int) (w Window, ok bool) {
	span := to.Sub(from)
	if span <= 0 {
		return
	}
	half := span / 2
	if dir < 0 {
		return Window{From: from.Add(-half), To: to.Add(-half)}, true
	}
	if !to.Before(now) {
		return
	}
	nt := to.Add(half)
	if nt.After(now) {
		nt = now
	}
	return Window{From: nt.Add(-span), To: nt}, true
}

// zoomWindow is Zoom out (out) or Zoom in: the span doubled or halved about
// the anchor — the selected row's instant, or the centre — keeping the
// anchor's place in the window, then shifted left so To is at most now.
// Zoom in stops at zoomInMin.
func zoomWindow(from, to, now time.Time, anchor time.Time, hasAnchor bool, out bool) (w Window, ok bool) {
	span := to.Sub(from)
	if span <= 0 {
		return
	}
	next := span * 2
	if !out {
		next = span / 2
		if next < zoomInMin {
			next = zoomInMin
		}
		if next >= span {
			return
		}
	}
	f := 0.5
	if hasAnchor {
		f = min(max(float64(anchor.Sub(from))/float64(span), 0), 1)
	}
	nf := from.Add(time.Duration(f * float64(span))).Add(-time.Duration(f * float64(next)))
	nt := nf.Add(next)
	if nt.After(now) {
		nt, nf = now, now.Add(-next)
	}
	return Window{From: nf, To: nt}, true
}

// fitWindow is the window trimmed to the first and last non-empty context
// bucket; none when every bucket is empty.
func fitWindow(counts []Bucket, step time.Duration) (w Window, ok bool) {
	var lo, hi int64
	for _, b := range counts {
		if b.N == 0 {
			continue
		}
		if !ok || b.Bucket < lo {
			lo = b.Bucket
		}
		if !ok || b.Bucket > hi {
			hi = b.Bucket
		}
		ok = true
	}
	if !ok || step <= 0 {
		return Window{}, false
	}
	return Window{From: time.Unix(lo, 0).UTC(), To: time.Unix(hi, 0).UTC().Add(step)}, true
}

// roundLive is the relative span Live arms for a fixed window: the nearest
// of liveSpans no larger than one and a half times the span, else the span
// itself.
func roundLive(span time.Duration) (d time.Duration) {
	d = span
	best := time.Duration(math.MaxInt64)
	for _, s := range liveSpans {
		if float64(s) > 1.5*float64(span) {
			continue
		}
		if diff := (s - span).Abs(); diff < best {
			best, d = diff, s
		}
	}
	return
}

// --- the history ---

// windowHistory is the windows Back returns to, newest last, at most
// historyDepth.
type windowHistory struct {
	items []Window
}

func (inst *windowHistory) push(w Window) {
	if len(inst.items) == historyDepth {
		inst.items = slices.Delete(inst.items, 0, 1)
	}
	inst.items = append(inst.items, w)
}

func (inst *windowHistory) pop() (w Window, ok bool) {
	if w, ok = inst.top(); ok {
		inst.items = inst.items[:len(inst.items)-1]
	}
	return
}

func (inst *windowHistory) top() (w Window, ok bool) {
	if n := len(inst.items); n > 0 {
		return inst.items[n-1], true
	}
	return
}

// sameWindow compares two windows by instant, not by clock reading.
func sameWindow(a, b Window) bool {
	return a.Last == b.Last && a.From.Equal(b.From) && a.To.Equal(b.To)
}

// --- the detail strip ---

// foldKeys orders a strip's keys busiest first and bounds them to
// stripKeys, the rest as other; index maps a key to its series, the rest to
// the last.
func foldKeys(buckets []Bucket) (keys []string, index func(key string) int) {
	totals := map[string]uint64{}
	var order []string
	for _, b := range buckets {
		if _, ok := totals[b.Key]; !ok {
			order = append(order, b.Key)
		}
		totals[b.Key] += b.N
	}
	sortKeys(order, totals)
	keys = order
	other := false
	if len(keys) > stripKeys {
		keys, other = order[:stripKeys-1], true
	}
	at := make(map[string]int, len(keys))
	for i, k := range keys {
		at[k] = i
	}
	keys = slices.Clone(keys)
	if other {
		keys = append(keys, "other")
	}
	last := len(keys) - 1
	index = func(key string) int {
		if i, ok := at[key]; ok {
			return i
		}
		return last
	}
	return
}

// detailIntervals is the detail strip's data: one interval per (bucket,
// series) with the series as its lane, its count against the largest mapped
// onto [detailIntensityFloor, 1] as intensity, and the count in words for the
// tooltip.
func detailIntervals(buckets []Bucket, step time.Duration) (ivs []*layout.IntervalEvent, keys []string) {
	if len(buckets) == 0 || step <= 0 {
		return
	}
	keys, index := foldKeys(buckets)
	type cell struct {
		bucket int64
		series int
	}
	counts := map[cell]uint64{}
	var order []cell
	var maxN uint64
	for _, b := range buckets {
		k := cell{b.Bucket, index(b.Key)}
		if _, ok := counts[k]; !ok {
			order = append(order, k)
		}
		counts[k] += b.N
		maxN = max(maxN, counts[k])
	}
	ivs = make([]*layout.IntervalEvent, 0, len(order))
	for _, k := range order {
		n := counts[k]
		if n == 0 {
			continue
		}
		ivs = append(ivs, &layout.IntervalEvent{FromMS: k.bucket * 1000, ToMS: (k.bucket + int64(step/time.Second)) * 1000, KindID: int32(k.series),
			Intensity: float32(detailIntensityFloor + (1-detailIntensityFloor)*float64(n)/float64(maxN)), LaneHint: keys[k.series], Note: rowCount(n)})
	}
	return
}

// rowCount is a bucket's count in words, for the strip's tooltip.
func rowCount(n uint64) string {
	if n == 1 {
		return "1 row"
	}
	return strconv.FormatUint(n, 10) + " rows"
}

// detailColors is the qualitative cycle as the detail strip's lane palette,
// one per series the strip can show.
func detailColors() (cs []color.Color) {
	for i := range stripKeys + 1 {
		rgba := styletokens.QualitativeCycle(i)
		cs = append(cs, color.RGBA(rgba.R, rgba.G, rgba.B, rgba.A))
	}
	return
}

// outOfHoursBands yields, for each day the view touches in the zone loc
// returns, the spans outside workStartHour–workEndHour and whole weekend
// days, in a subtle neutral tint labelled "out of hours".
func outOfHoursBands(loc func() *time.Location) timeline.BackgroundBandProducer {
	return func(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
		return func(yield func(layout.BackgroundBand) bool) {
			z := loc()
			if z == nil {
				z = time.UTC
			}
			tint := (styletokens.NeutralTextSecondary.AsHex() & 0xffffff00) | outOfHoursAlpha
			emit := func(a, b time.Time) bool {
				am, bm := max(a.UnixMilli(), viewMinMS), min(b.UnixMilli(), viewMaxMS)
				if am >= bm {
					return true
				}
				return yield(layout.BackgroundBand{FromMS: am, ToMS: bm, Color: tint, Label: "out of hours"})
			}
			y, m, d := time.UnixMilli(viewMinMS).In(z).Date()
			for i := 0; i < outOfHoursMaxDays; i++ {
				day := time.Date(y, m, d+i, 0, 0, 0, 0, z)
				if day.UnixMilli() >= viewMaxMS {
					return
				}
				next := time.Date(y, m, d+i+1, 0, 0, 0, 0, z)
				if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
					if !emit(day, next) {
						return
					}
					continue
				}
				if !emit(day, time.Date(y, m, d+i, workStartHour, 0, 0, 0, z)) || !emit(time.Date(y, m, d+i, workEndHour, 0, 0, 0, z), next) {
					return
				}
			}
		}
	}
}
