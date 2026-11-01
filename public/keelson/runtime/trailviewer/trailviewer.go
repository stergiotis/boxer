// Package trailviewer is the trail viewer (ADR-0350, proposed): a runtime
// system app that reads the trail marts on boxer.facts by case — a run, a
// window, an app, a conversation, a task, a data subject, a principal, an
// entity, an egress destination, or every row — and time window, into a
// count strip, a windowed table, a facets pane and a detail pane with the
// identity chain, the rows around the selected one, and for an audit event
// or a grant the previous-state diff. Above the table one verdict line per
// writer run says whether the run's sealed record holds.
//
// The window never queries the store from a frame: a bgjob worker reads
// through the host's executor (cache.go, probe.go) and a later frame
// adopts the result. Without ClickHouse the window draws one line saying
// the trail is not persisted, and why. The source is shared (one per
// process); Register is the host's entry point.
package trailviewer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/presets"
)

const (
	detailPanelHeight = 260.0
	facetsPanelWidth  = 260.0
	stripHeight       = 120.0
	// stripFallbackWidth sizes the strip before the pane probe answers.
	stripFallbackWidth = 720.0
	// stripKeys bounds the strip's series; the rest are summed as other,
	// since the qualitative cycle has seven hues.
	stripKeys = 6
)

var (
	detailMutedFg = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	detailErrorFg = color.Hex(styletokens.ErrorDefault.AsHex())
	detailBgClear = color.Transparent
)

// App is one trail viewer window.
type App struct {
	manifest app.Manifest
	ids      *c.WidgetIdStack
	density  styletokens.DensityE

	frameCtx app.FrameContextI
	bus      app.BusI
	appId    string
	instance uint64
	inited   bool

	// The view: the case, the window, the filter, the strip's colouring,
	// the detail pane's tab. caseValue is the applied case; the combo and
	// the value box are its draft until Apply.
	caseValue  Case
	draftKind  CaseKindE
	draftValue string
	draftStale bool
	window     Window
	filter     Filter
	strip      StripByE
	tab        TabE
	grouped    bool

	// selected is a copy of the selected row, kept by facts id so it
	// survives a refill.
	selected    Row
	hasSelected bool

	cache    tableCache
	verdicts verdictCache
	detail   detailCache
	picker   pickerState

	valueH widgethandle.WidgetHandle

	// The grid's record and state; the record is rebuilt when the cache's
	// generation moves.
	grid      gridState
	stripW    float32
	fvState   fieldview.State
	drawnGen  uint64
	settleRem int
}

var _ app.AppI = (*App)(nil)

func newInstance(m app.Manifest) (out *App) {
	out = &App{
		manifest: m,
		ids:      c.NewWidgetIdStack(),
		density:  styletokens.ActiveDensity(),
		window:   defaultWindow,
		stripW:   stripFallbackWidth,
		picker:   newPickerState(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = inst.manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.appId = string(ctx.AppId())
	inst.instance = ctx.InstanceKey()
	inst.picker.runner.bus = inst.bus
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.cache.close()
	inst.verdicts.close()
	inst.detail.close()
	inst.picker.close()
	inst.grid.release()
	return
}

// ensureInit opens the window on the current run once it is known, every
// row otherwise.
func (inst *App) ensureInit() {
	if inst.inited {
		return
	}
	inst.inited = true
	if run := sourceRef.Load().run(); run != "" {
		inst.applyCase(Case{Kind: CaseRun, Value: run})
		return
	}
	inst.applyCase(Case{Kind: CaseAll})
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.frameCtx = ctx
	inst.tick(time.Now())
	return inst.render()
}

// tick advances the caches; the render goroutine's only contact with the
// workers.
func (inst *App) tick(now time.Time) {
	inst.ensureInit()
	src := sourceRef.Load()
	if w, ok := inst.picker.take(); ok {
		inst.setWindowGesture(w)
	}
	inst.cache.tick(now, src, inst.scopeValue(), readSpec{c: inst.caseValue, filter: inst.filter, buckets: stripBuckets(inst.stripW)},
		inst.probeFunc(src))
	inst.refreshSelected()
	inst.verdicts.tick(now, src, runsOf(inst.cache.rows))
	var sel *Row
	if inst.hasSelected {
		sel = &inst.selected
	}
	inst.detail.tick(src, sel, inst.probeFunc(src))
}

func (inst *App) probeFunc(src *Source) func() (*probe, error) {
	appId, instance := inst.appId, inst.instance
	if appId == "" {
		appId = string(inst.manifest.Id)
	}
	return func() (*probe, error) { return newProbe(src, appId, instance) }
}

func (inst *App) scopeValue() scope {
	return scope{Case: inst.caseValue, Window: inst.window, Filter: inst.filter.key(), Strip: inst.strip}
}

// --- state the commands set ---

func (inst *App) applyCase(cs Case) {
	inst.caseValue = cs
	inst.draftKind, inst.draftValue, inst.draftStale = cs.Kind, cs.Value, true
}

func (inst *App) applyWindow(w Window) {
	inst.window = w
}

func (inst *App) applyFilter(f Filter) {
	inst.filter = f.normalized()
}

func (inst *App) selectionId() (id uint64) {
	if inst.hasSelected {
		id = inst.selected.Id
	}
	return
}

func (inst *App) clearSelection() {
	inst.selected, inst.hasSelected = Row{}, false
}

// selectById selects a held row, or a row of the selected row's context.
func (inst *App) selectById(id uint64) (ok bool) {
	if id == 0 {
		inst.clearSelection()
		return true
	}
	for i := range inst.cache.rows {
		if inst.cache.rows[i].Id == id {
			inst.selected, inst.hasSelected = inst.cache.rows[i], true
			return true
		}
	}
	if inst.hasSelected {
		if d := inst.detail.current(inst.selected.Id); d != nil {
			for _, rs := range [][]Row{d.before, d.after} {
				for i := range rs {
					if rs[i].Id == id {
						inst.selected, inst.hasSelected = rs[i], true
						return true
					}
				}
			}
		}
	}
	return false
}

// refreshSelected replaces the selected row's copy with the cache's when
// a refill holds it, so its audit columns follow; a row the refill does
// not hold stays selected as it was.
func (inst *App) refreshSelected() {
	if !inst.hasSelected {
		return
	}
	for i := range inst.cache.rows {
		if inst.cache.rows[i].Id == inst.selected.Id {
			if inst.selected.Audit == nil || inst.cache.rows[i].Audit != nil {
				inst.selected = inst.cache.rows[i]
			}
			return
		}
	}
}

// --- rendering ---

func (inst *App) render() (err error) {
	inst.density = styletokens.ActiveDensity()
	src := sourceRef.Load()
	if !src.persisted() {
		reason := notPersistedReason(src)
		c.Label(strings.ToUpper(reason[:1]) + reason[1:] + ".").Wrap().Send()
		return
	}
	if inst.draftStale {
		inst.draftStale = false
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.draftValue)
	}
	inst.requestRepaint()
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderControls()
		inst.renderVerdicts()
		inst.renderStrip()
	}
	if inst.hasSelected {
		for range c.PanelBottomInside(inst.ids.PrepareStr("btm")).DefaultSize(detailPanelHeight).Resizable(true).KeepIter() {
			inst.renderDetail()
		}
	}
	for range c.PanelRightInside(inst.ids.PrepareStr("facets")).DefaultSize(facetsPanelWidth).Resizable(true).KeepIter() {
		inst.renderFacets()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderTable()
	}
	return
}

func (inst *App) requestRepaint() {
	running := inst.cache.runner.Running() || inst.verdicts.runner.Running() || inst.detail.runner.Running() || inst.picker.runner.runner.Running()
	switch {
	case running:
		c.RequestRepaintAfter(0.1)
	case inst.drawnGen != inst.cache.gen:
		inst.drawnGen, inst.settleRem = inst.cache.gen, 3
		c.RequestRepaint()
	case inst.settleRem > 0:
		inst.settleRem--
		c.RequestRepaint()
	default:
		c.RequestRepaintAfter(refreshInterval().Seconds())
	}
}

// renderControls is the case selector, the time range picker, Refresh and
// the cache's freshness.
func (inst *App) renderControls() {
	c.AddSpace(styletokens.PaddingHair(inst.density))
	for range c.Horizontal().KeepIter() {
		c.Label("Case:").Send()
		for range c.ComboBox(inst.ids.PrepareStr("case-kind"), c.WidgetText().Text("case").Keep(),
			c.WidgetText().Text(inst.draftKind.String()).Keep()).KeepIter() {
			for _, k := range AllCaseKinds {
				sel := k == inst.draftKind
				if c.Button(inst.ids.PrepareStr("case-"+k.String()), c.Atoms().Text(k.String()).Keep()).
					Selected(sel).FrameWhenInactive(!sel).Frame(true).SendResp().HasPrimaryClicked() {
					inst.draftKind = k
					if k == CaseAll {
						inst.draftValue, inst.draftStale = "", true
					}
				}
			}
		}
		inst.valueH = widgethandle.Make(inst.ids.PrepareStr("case-value").Derive())
		c.TextEdit(inst.ids.PrepareStr("case-value"), inst.draftValue, false).
			HintText(inst.draftKind.Hint()).DesiredWidth(260).SendRespVal(&inst.draftValue)
		if c.Button(inst.ids.PrepareStr("case-apply"), c.Atoms().Text("Apply").Keep()).SendResp().HasPrimaryClicked() {
			cs := Case{Kind: inst.draftKind, Value: strings.TrimSpace(inst.draftValue)}
			if err := cs.validate(); err != nil {
				inst.picker.err = err
			} else {
				inst.picker.err = nil
				inst.setCaseGesture(cs)
			}
		}
		c.AddSpace(styletokens.GapPanels(inst.density))
		inst.picker.render(inst.ids, inst.window)
		c.AddSpace(styletokens.GapPanels(inst.density))
		if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.cache.refreshNow = true
		}
		fresh, tone := inst.freshness(time.Now())
		badge.New(inst.ids.PrepareStr("fresh"), fresh).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Monospace().
			Tooltip("when the window last read its case from the trail marts (BOXER_TRAILVIEWER_REFRESH)").Send()
	}
	for _, e := range []error{inst.cache.lastErr, inst.picker.err} {
		if e != nil {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, e.Error()).Small().End().Keep()).Wrap().Send()
		}
	}
}

func (inst *App) freshness(now time.Time) (label string, tone badge.ToneE) {
	tone = badge.ToneNeutral
	switch {
	case !inst.cache.filled && inst.cache.runner.Running():
		label = "Reading…"
	case !inst.cache.filled:
		label = "Not read yet"
	default:
		label = fmt.Sprintf("%d rows · read %s", len(inst.cache.rows), inst.picker.format(inst.cache.refreshedAt, "15:04:05"))
		if inst.cache.more {
			label += " · older rows follow"
		}
		if inst.cache.runner.Running() {
			label += " · reading…"
		}
	}
	if inst.cache.lastErr != nil {
		tone = badge.ToneWarning
	}
	return
}

// renderVerdicts is the verdict line per writer run (SD6), above the
// table: a run's state never hides its rows.
func (inst *App) renderVerdicts() {
	runs := runsOf(inst.cache.rows)
	if len(runs) == 0 {
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("verdicts")) {
		for range c.HorizontalWrapped().KeepIter() {
			c.Label("Evidence:").Send()
			for i, run := range runs {
				l := inst.verdicts.line(run)
				badge.New(inst.ids.PrepareSeq(uint64(i)), shortRun(run)+": "+l.Label()).
					Tone(toneOf(l.Tone())).Variant(badge.VariantSoft).Size(badge.SizeSm).
					Tooltip("writer run " + run + "; law " + lawText(l)).Send()
			}
		}
	}
}

func lawText(l verdict.Line) string {
	if l.Law() == "" {
		return "none: unverified"
	}
	return l.Law()
}

func toneOf(t verdict.ToneE) badge.ToneE {
	switch t {
	case verdict.ToneOk:
		return badge.ToneSuccess
	case verdict.ToneWarning:
		return badge.ToneWarning
	case verdict.ToneError:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

func shortRun(run string) string {
	if run == "" {
		return "(no run)"
	}
	if len(run) > 8 {
		return run[:8]
	}
	return run
}

// renderStrip draws the counts per bucket, coloured by kind or status, and
// the one text line a scene asserts on. A click on a bar narrows the
// window to its bucket.
func (inst *App) renderStrip() {
	for range c.IdScope(inst.ids.PrepareStr("strip")) {
		if w, _, ok := c.CapturePaneSize(inst.ids.ProbeSeq("strip-pane")); ok && w > 100 {
			inst.stripW = w
		}
		series := stripSeries(inst.cache.buckets, inst.cache.step)
		p := implot.Begin(inst.ids, "##trail-strip", inst.stripW, stripHeight)
		p.SetupAxes("", "rows", implot.AxisFlagsNone, implot.AxisFlagsAutoFit)
		p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
		from, to := inst.cache.from, inst.cache.to
		if !from.IsZero() {
			p.SetupAxisLimits(implot.AxisX1, float64(from.Unix()), float64(to.Unix()), implot.CondAlways)
		}
		if x, _, ok := p.Clicked(); ok && inst.cache.step > 0 {
			start := time.Unix(int64(x), 0).UTC().Truncate(inst.cache.step)
			inst.setWindowGesture(Window{From: start, To: start.Add(inst.cache.step)})
		}
		width := inst.cache.step.Seconds() * 0.9
		// Stacked by painting the cumulative tops largest first: each
		// series covers the ones below it.
		for i := len(series.keys) - 1; i >= 0; i-- {
			p.SetNextColor(seriesColor(i))
			p.Bars(series.keys[i], series.xs, series.tops[i], width)
		}
		p.End()
		for range c.Horizontal().KeepIter() {
			label := "by kind"
			if inst.strip == StripByStatus {
				label = "by status"
			}
			if c.Button(inst.ids.PrepareStr("strip-by"), c.Atoms().Text("Colour "+label).Keep()).SendResp().HasPrimaryClicked() {
				if inst.strip == StripByKind {
					inst.strip = StripByStatus
				} else {
					inst.strip = StripByKind
				}
			}
			c.Label(stripLineIn(inst.cache.buckets, inst.cache.step, inst.picker.loc)).Send()
		}
	}
}

// seriesColor is the design system's qualitative cycle; the legend names
// every series, so colour is never the only channel.
func seriesColor(i int) uint32 {
	rgba := styletokens.QualitativeCycle(i)
	return uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
}

// stripData is the strip's bars: one x per bucket, and per key the
// cumulative top of its stack.
type stripData struct {
	keys []string
	xs   []float64
	tops [][]float64
}

// stripSeries folds buckets into at most stripKeys series — the busiest
// keys, the rest summed as other — with cumulative tops.
func stripSeries(buckets []Bucket, step time.Duration) (out stripData) {
	if len(buckets) == 0 || step <= 0 {
		return
	}
	totals := map[string]uint64{}
	var order []string
	for _, b := range buckets {
		if _, ok := totals[b.Key]; !ok {
			order = append(order, b.Key)
		}
		totals[b.Key] += b.N
	}
	sortKeys(order, totals)
	keys := order
	other := false
	if len(keys) > stripKeys {
		keys, other = order[:stripKeys-1], true
	}
	index := map[string]int{}
	for i, k := range keys {
		index[k] = i
	}
	out.keys = append([]string(nil), keys...)
	if other {
		out.keys = append(out.keys, "other")
	}
	xi := map[int64]int{}
	for _, b := range buckets {
		if _, ok := xi[b.Bucket]; !ok {
			xi[b.Bucket] = len(out.xs)
			out.xs = append(out.xs, float64(b.Bucket)+step.Seconds()/2)
		}
	}
	counts := make([][]float64, len(out.keys))
	for i := range counts {
		counts[i] = make([]float64, len(out.xs))
	}
	for _, b := range buckets {
		k, ok := index[b.Key]
		if !ok {
			k = len(out.keys) - 1
		}
		counts[k][xi[b.Bucket]] += float64(b.N)
	}
	out.tops = make([][]float64, len(out.keys))
	for i := range out.keys {
		out.tops[i] = make([]float64, len(out.xs))
		for x := range out.xs {
			out.tops[i][x] = counts[i][x]
			if i > 0 {
				out.tops[i][x] += out.tops[i-1][x]
			}
		}
	}
	return
}

func sortKeys(keys []string, totals map[string]uint64) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0; j-- {
			a, b := keys[j-1], keys[j]
			if totals[a] > totals[b] || (totals[a] == totals[b] && a <= b) {
				break
			}
			keys[j-1], keys[j] = b, a
		}
	}
}

// stripLine is the strip in words, UTC.
func stripLine(buckets []Bucket, step time.Duration) string {
	return stripLineIn(buckets, step, time.UTC)
}

// stripLineIn is the strip in words: rows, buckets of what size, and the
// busiest bucket with its time in loc.
func stripLineIn(buckets []Bucket, step time.Duration, loc *time.Location) string {
	per := map[int64]uint64{}
	var rows uint64
	for _, b := range buckets {
		per[b.Bucket] += b.N
		rows += b.N
	}
	if len(per) == 0 {
		return "0 rows in the window"
	}
	var busiest int64
	var most uint64
	for k, n := range per {
		if n > most || (n == most && k < busiest) {
			busiest, most = k, n
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	return fmt.Sprintf("%d rows in %d buckets of %s; busiest %s with %d", rows, len(per), step,
		time.Unix(busiest, 0).In(loc).Format("2006-01-02 15:04:05 MST"), most)
}

// renderFacets is the facets pane: the active filter, then per facet its
// values with counts. A click includes a value, a secondary or modified
// click (Ctrl/Cmd/Alt) excludes it, and a click on a value with a term
// removes the term.
func (inst *App) renderFacets() {
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		c.Label("Filter: " + inst.filter.String()).Wrap().Send()
		if len(inst.filter.Terms) > 0 {
			if c.Button(inst.ids.PrepareStr("filter-clear"), c.Atoms().Text("Clear filter").Keep()).SendResp().HasPrimaryClicked() {
				inst.setFilterGesture(Filter{})
			}
		}
		mods := c.CurrentApplicationState.StateManager.GetModifiers()
		modified := mods.Command || mods.Ctrl || mods.Alt
		for fi, f := range AllFacets {
			var vs []FacetValue
			if fi < len(inst.cache.facets) {
				vs = inst.cache.facets[fi]
			}
			for range c.IdScope(inst.ids.PrepareStr("facet-" + f.String())) {
				for range c.CollapsingHeader(inst.ids.PrepareStr("hdr"), c.WidgetText().Text(fmt.Sprintf("%s (%d)", f, len(vs))).Keep()).
					DefaultOpen(f != FacetPrincipal).KeepIter() {
					if len(vs) == 0 {
						c.Label("(none in the case and window)").Send()
					}
					for i, v := range vs {
						label := v.Value
						if label == "" {
							label = "(empty)"
						}
						excluded, has := inst.filter.term(f, v.Value)
						prefix := ""
						if has && excluded {
							prefix = "≠ "
						} else if has {
							prefix = "= "
						}
						resp := c.Button(inst.ids.PrepareSeq(uint64(i)), c.Atoms().Text(fmt.Sprintf("%s%s  %d", prefix, label, v.N)).Keep()).
							Selected(has && !excluded).SendResp()
						switch {
						case resp.HasSecondaryClicked() || (resp.HasPrimaryClicked() && modified):
							inst.setFilterGesture(inst.filter.with(f, v.Value, true))
						case resp.HasPrimaryClicked() && has:
							inst.setFilterGesture(inst.filter.without(f, v.Value))
						case resp.HasPrimaryClicked():
							inst.setFilterGesture(inst.filter.with(f, v.Value, false))
						}
					}
				}
			}
		}
	}
}

// --- the table ---

// gridState is the grid's record, built from the cache's rows.
type gridState struct {
	rec    arrow.RecordBatch
	rows   []Row
	gen    uint64
	built  bool
	state  recordtable.State
	sort   recordtable.SortState
	groups recordtable.GroupState
	order  []int64
	// lastPos is the display position of the last row drawn last frame.
	maxPos int64
}

func (inst *gridState) release() {
	if inst.rec != nil {
		inst.rec.Release()
		inst.rec = nil
	}
}

// gridCol is one column of the table: its header, its sort key and its
// cell.
type gridCol struct {
	header  string
	width   float32
	agentic bool
	str     func(r *Row) string
	num     func(r *Row) int64
}

// tableColumns are the table's columns: the verdict mark first, then time,
// kind, headline, status, subject, app, instance, and the conversation and
// task columns when the case is agentic. The time is kept as nanoseconds
// for the sort; its cell is drawn in the picker's zone.
var tableColumns = []gridCol{
	{header: "verdict", width: 80, str: func(r *Row) string { return r.Run }},
	{header: "time", width: 150, num: func(r *Row) int64 { return r.TsNs }},
	{header: "kind", width: 100, str: func(r *Row) string { return r.Kind }},
	{header: "headline", width: 520, str: func(r *Row) string { return r.Headline }},
	{header: "status", width: 90, str: func(r *Row) string { return r.Status }},
	{header: "subject", width: 160, str: func(r *Row) string { return r.Subject }},
	{header: "app", width: 120, str: func(r *Row) string { return r.App }},
	{header: "instance", width: 70, num: func(r *Row) int64 { return int64(r.Instance) }},
	{header: "conversation", width: 140, agentic: true, str: func(r *Row) string { return r.Conversation }},
	{header: "task", width: 140, agentic: true, str: func(r *Row) string { return r.Task }},
}

// build rebuilds the record from rows.
func (inst *gridState) build(rows []Row, gen uint64) {
	if inst.built && inst.gen == gen {
		return
	}
	inst.release()
	inst.rows, inst.gen, inst.built = rows, gen, true
	mem := memory.DefaultAllocator
	fields := make([]arrow.Field, 0, len(tableColumns))
	arrs := make([]arrow.Array, 0, len(tableColumns))
	for _, col := range tableColumns {
		if col.num != nil {
			b := array.NewInt64Builder(mem)
			for i := range rows {
				b.Append(col.num(&rows[i]))
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
			fields = append(fields, arrow.Field{Name: col.header, Type: arrow.PrimitiveTypes.Int64})
			continue
		}
		b := array.NewStringBuilder(mem)
		for i := range rows {
			b.Append(col.str(&rows[i]))
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: col.header, Type: arrow.BinaryTypes.String})
	}
	inst.rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(rows)))
	for _, a := range arrs {
		a.Release()
	}
}

func (inst *App) renderTable() {
	g := &inst.grid
	g.build(inst.cache.rows, inst.cache.gen)
	for range c.Horizontal().KeepIter() {
		groupLabel := "Group by parent: off"
		if inst.grouped {
			groupLabel = "Group by parent: on"
		}
		if c.Button(inst.ids.PrepareStr("group"), c.Atoms().Text(groupLabel).Keep()).Selected(inst.grouped).SendResp().HasPrimaryClicked() {
			inst.setGrouped(!inst.grouped)
		}
		c.Label("Case: " + inst.caseValue.String() + " · " + inst.window.String()).Send()
	}
	if len(g.rows) == 0 {
		switch {
		case inst.cache.filled:
			c.Label("(no rows in the case, window and filter)").Send()
		default:
			c.Label("(reading the trail marts…)").Send()
		}
		return
	}
	agentic := inst.caseValue.Kind.agentic()
	cols := make([]recordtable.Column, 0, len(tableColumns))
	rows := g.rows
	g.maxPos = -1
	g.order = g.sort.OrderFor(g.rec, g.rec.NumRows())
	for i, col := range tableColumns {
		if col.agentic && !agentic {
			continue
		}
		cl := recordtable.Column{Arrow: i, Header: col.header, Width: col.width, WidthType: "text"}
		if col.num != nil {
			cl.WidthType, cl.DescFirst = "number", true
		}
		switch col.header {
		case "verdict":
			cl.Cell = func(row int64) recordtable.Cell {
				inst.noteDrawn(row)
				l := inst.verdicts.line(rows[row].Run)
				return recordtable.Cell{Text: l.Mark(), Hover: l.Label(), Tone: color.Hex(toneHex(l.Tone())), Toned: true}
			}
		case "time":
			cl.Cell = func(row int64) recordtable.Cell {
				return recordtable.Cell{Text: inst.picker.format(rows[row].Ts(), "2006-01-02 15:04:05.000"), LeftAlign: true}
			}
		default:
			str, num := col.str, col.num
			cl.Cell = func(row int64) recordtable.Cell {
				if num != nil {
					return recordtable.Cell{Text: strconv.FormatInt(num(&rows[row]), 10)}
				}
				return recordtable.Cell{Text: str(&rows[row]), LeftAlign: true}
			}
		}
		cols = append(cols, cl)
	}
	sel, has := int64(-1), false
	if inst.hasSelected {
		for i := range rows {
			if rows[i].Id == inst.selected.Id {
				sel, has = int64(i), true
				break
			}
		}
	}
	in := recordtable.Input{Ids: inst.ids, ScopeKey: "trail-rows", Record: g.rec, Columns: cols, Sort: &g.sort, Order: g.order,
		Selected: sel, HasSelection: has, State: &g.state, RowHeight: 22}
	if inst.grouped {
		in.Groups = &g.groups
	}
	res := recordtable.RenderGrid(in)
	g.sort.Apply(res)
	if inst.grouped {
		g.groups.Apply(res)
	}
	if res.Clicked >= 0 && res.Clicked < int64(len(rows)) {
		id := rows[res.Clicked].Id
		if inst.hasSelected && inst.selected.Id == id {
			id = 0
		}
		inst.selectGesture(id)
	}
	// The end of what is held is reached when the last row by display
	// position was drawn; the cursor then reads the rows after it.
	if !inst.grouped && g.maxPos >= int64(len(rows))-1 && inst.cache.more {
		inst.cache.extendNow = true
	}
}

// noteDrawn records the display position of a row the grid drew.
func (inst *App) noteDrawn(row int64) {
	g := &inst.grid
	pos := g.sort.DisplayPos(g.order, row)
	if pos > g.maxPos {
		g.maxPos = pos
	}
}

func (inst *App) setGrouped(on bool) {
	inst.grouped = on
	if !on {
		inst.grid.groups.SetLevels(nil)
		return
	}
	rows := func() []Row { return inst.grid.rows }
	inst.grid.groups.SetLevels([]recordtable.GroupLevel{{Arrow: -1, Key: func(row int64) recordtable.Key {
		rs := rows()
		if row < 0 || row >= int64(len(rs)) || rs[row].ParentRef == "" {
			return recordtable.Key{Value: "", Label: "(no parent)", Null: true}
		}
		return recordtable.Key{Value: rs[row].ParentRef, Label: rs[row].ParentRef}
	}}})
}

func toneHex(t verdict.ToneE) uint32 {
	switch t {
	case verdict.ToneOk:
		return styletokens.SuccessDefault.AsHex()
	case verdict.ToneWarning:
		return styletokens.WarningDefault.AsHex()
	case verdict.ToneError:
		return styletokens.ErrorDefault.AsHex()
	}
	return styletokens.NeutralTextSecondary.AsHex()
}

// --- the detail pane ---

// breadcrumb is the identity chain as one line — person › window (app,
// instance) › task › model call › tool call — from the row's context
// columns, absent links omitted.
func breadcrumb(r *Row) string {
	var parts []string
	if r.Audit != nil && r.Audit.Principal != "" {
		parts = append(parts, "person "+r.Audit.Principal)
	}
	switch {
	case r.App != "" && r.Instance != 0:
		parts = append(parts, "window "+shortApp(r.App)+"#"+strconv.FormatUint(r.Instance, 10))
	case r.App != "":
		parts = append(parts, "window "+shortApp(r.App))
	case r.Instance != 0:
		parts = append(parts, "window #"+strconv.FormatUint(r.Instance, 10))
	}
	if r.Task != "" {
		parts = append(parts, "task "+r.Task)
	}
	modelCall := r.ParentRef
	if r.Kind == "llmCall" {
		modelCall = r.Ref
	}
	if r.Kind != kindAuditEvent && r.Kind != "httpFetch" && modelCall != "" {
		parts = append(parts, "model call "+modelCall)
	}
	if r.TaskCall != "" {
		parts = append(parts, "tool call "+r.TaskCall)
	}
	return strings.Join(parts, " › ")
}

func shortApp(id string) string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

func (inst *App) renderDetail() {
	r := &inst.selected
	l := inst.verdicts.line(r.Run)
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("d-verdict"), l.Mark()).Tone(toneOf(l.Tone())).Variant(badge.VariantSolid).Size(badge.SizeSm).
			Tooltip(l.Label()).Send()
		badge.New(inst.ids.PrepareStr("d-kind"), r.Kind).Tone(badge.TonePrimary).Variant(badge.VariantOutline).Size(badge.SizeSm).Send()
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear,
			inst.picker.format(r.Ts(), time.RFC3339Nano)+" · id "+strconv.FormatUint(r.Id, 10)).Small().Monospace().End().Keep()).Send()
		for _, t := range []TabE{TabFields, TabPrevious, TabContext} {
			if c.Button(inst.ids.PrepareStr("d-tab-"+t.String()), c.Atoms().Text(t.String()).Keep()).Selected(inst.tab == t).
				SendResp().HasPrimaryClicked() && inst.tab != t {
				inst.setTabGesture(t)
			}
		}
		if c.Button(inst.ids.PrepareStr("d-clear"), c.Atoms().Text("Clear selection").Keep()).SendResp().HasPrimaryClicked() {
			inst.selectGesture(0)
		}
	}
	if bc := breadcrumb(r); bc != "" {
		c.Label(bc).Wrap().Send()
	}
	c.Separator().Horizontal().Send()
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		switch inst.tab {
		case TabPrevious:
			inst.renderPrevious()
		case TabContext:
			inst.renderContext()
		default:
			inst.renderFields()
		}
	}
}

func (inst *App) renderFields() {
	r := &inst.selected
	c.Label(r.Headline).Wrap().Send()
	if r.Text != "" {
		c.AddSpace(styletokens.PaddingHair(inst.density))
		c.Label("Text:").Send()
		c.Label(r.Text).Wrap().Selectable(true).Send()
	}
	fieldview.Render(fieldview.Input{Ids: inst.ids, ScopeKey: "tv-fields", State: &inst.fvState, Fields: rowFields(r)})
}

// rowFields is the row's columns for the field viewer: the timeline's,
// then an audit event's own, its attributes and its references.
func rowFields(r *Row) (fs []fieldview.Field) {
	str := func(n, v string) {
		if v != "" {
			fs = append(fs, fieldview.Field{Name: n, Kind: fieldview.KindString, Str: v})
		}
	}
	num := func(n string, v uint64) {
		if v != 0 {
			fs = append(fs, fieldview.Field{Name: n, Kind: fieldview.KindUint, Uint: v})
		}
	}
	str("kind", r.Kind)
	str("status", r.Status)
	str("subject", r.Subject)
	str("run", r.Run)
	str("app", r.App)
	num("instance", r.Instance)
	str("conversation", r.Conversation)
	str("turn", r.Turn)
	num("round", uint64(r.Round))
	str("task", r.Task)
	num("task-epoch", r.TaskEpoch)
	str("task-call", r.TaskCall)
	str("ref", r.Ref)
	str("parent-ref", r.ParentRef)
	num("tokens-in", r.TokensIn)
	num("tokens-out", r.TokensOut)
	num("elapsed-ms", r.ElapsedMs)
	if r.Confined {
		fs = append(fs, fieldview.Field{Name: "confined", Kind: fieldview.KindBool, Bool: true})
	}
	if r.Tainted {
		fs = append(fs, fieldview.Field{Name: "tainted", Kind: fieldview.KindBool, Bool: true})
	}
	if a := r.Audit; a != nil {
		str("domain", a.Domain)
		str("action", a.Action)
		str("outcome", a.Outcome)
		str("principal", a.Principal)
		str("principal-by", a.PrincipalBy)
		num("data subject", a.Subject)
		str("entity", a.Entity)
		str("from", a.From)
		str("from-by", a.FromBy)
		attrs := fieldview.Field{Name: "attributes", Kind: fieldview.KindObject}
		for i, k := range a.AttrKeys {
			if i < len(a.AttrValues) {
				attrs.Children = append(attrs.Children, fieldview.Field{Name: k, Kind: fieldview.KindString, Str: a.AttrValues[i]})
			}
		}
		if len(attrs.Children) > 0 {
			fs = append(fs, attrs)
		}
		refs := fieldview.Field{Name: "references", Kind: fieldview.KindObject}
		for i, t := range a.RefTypes {
			if i < len(a.RefValues) {
				refs.Children = append(refs.Children, fieldview.Field{Name: t, Kind: fieldview.KindString, Str: a.RefValues[i]})
			}
		}
		if len(refs.Children) > 0 {
			fs = append(fs, refs)
		}
	}
	return
}

func (inst *App) renderPrevious() {
	r := &inst.selected
	d := inst.detail.current(r.Id)
	switch {
	case d == nil:
		c.Label("(reading the previous state…)").Send()
		return
	case d.err != nil:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, d.err.Error()).End().Keep()).Wrap().Send()
		return
	case d.previous == nil:
		c.Label("This row has no previous state: it is neither an audit event naming an entity nor an agent grant of a task.").Wrap().Send()
		return
	}
	p := d.previous
	base := "No previous event; the field table shows this row's state."
	if p.Has {
		base = "Before is the previous event, id " + strconv.FormatUint(p.PrevId, 10) + " at " + inst.picker.format(p.PrevTs, time.RFC3339) +
			"; after is this row."
	}
	c.Label(base).Wrap().Send()
	tone := badge.ToneNeutral
	switch p.Claim {
	case ClaimMatches:
		tone = badge.ToneSuccess
	case ClaimDiffers:
		tone = badge.ToneError
	case ClaimGapBetween, ClaimNoPrevious:
		tone = badge.ToneWarning
	}
	badge.New(inst.ids.PrepareStr("p-verdict"), "from: "+p.Verdict).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
	for range c.IdScope(inst.ids.PrepareStr("p-table")) {
		for range c.Grid(inst.ids.PrepareStr("grid")).NumColumns(4).Striped(true).KeepIter() {
			for _, h := range []string{"field", "before", "after", "change"} {
				c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Send()
			}
			c.EndRow()
			for _, f := range p.Fields {
				c.Label(f.Field).Send()
				c.Label(f.Before).Send()
				c.Label(f.After).Send()
				fg := detailMutedFg
				switch f.Change {
				case ChangeAdded.String():
					fg = color.Hex(styletokens.SuccessDefault.AsHex())
				case ChangeRemoved.String():
					fg = color.Hex(styletokens.ErrorDefault.AsHex())
				case ChangeChanged.String():
					fg = color.Hex(styletokens.WarningDefault.AsHex())
				}
				c.LabelAtoms(c.Atoms().BeginRichTextColored(fg, detailBgClear, f.Change).End().Keep()).Send()
				c.EndRow()
			}
		}
	}
}

func (inst *App) renderContext() {
	r := &inst.selected
	d := inst.detail.current(r.Id)
	switch {
	case d == nil:
		c.Label("(reading the rows around this one…)").Send()
		return
	case d.err != nil:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, d.err.Error()).End().Keep()).Wrap().Send()
		return
	}
	c.Label(fmt.Sprintf("The rows before and after this one in writer run %s, regardless of the filter; a click selects one.", r.Run)).Wrap().Send()
	for range c.IdScope(inst.ids.PrepareStr("ctx")) {
		n := uint64(0)
		line := func(x *Row, self bool) {
			text := inst.picker.format(x.Ts(), "15:04:05.000") + "  " + x.Kind + "  " + x.Headline
			if self {
				c.LabelAtoms(c.Atoms().BeginRichText("▸ " + text).Strong().End().Keep()).Send()
				return
			}
			if c.Button(inst.ids.PrepareSeq(n), c.Atoms().Text(text).Keep()).Frame(false).SendResp().HasPrimaryClicked() {
				inst.selectGesture(x.Id)
			}
			n++
		}
		for i := range d.before {
			line(&d.before[i], false)
		}
		line(r, true)
		for i := range d.after {
			line(&d.after[i], false)
		}
	}
}

// --- the time range picker ---

// pickerState is the picker's expressions, its zone, and the evaluation
// of an expression the viewer does not read itself.
type pickerState struct {
	fromExpr, toExpr string
	packed           string
	tzName           string
	loc              *time.Location
	presets          *presets.Registry
	runner           rangeRunner
	err              error
	pending          *Window
}

func newPickerState() pickerState {
	return pickerState{fromExpr: "anchor_now - INTERVAL 1 HOUR", toExpr: "anchor_now", tzName: "UTC", loc: time.UTC,
		presets: presets.DefaultGrafana75()}
}

// format renders t in the picker's zone.
func (inst *pickerState) format(t time.Time, layout string) string {
	loc := inst.loc
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format(layout)
}

// render draws the picker; a change is read here when it is a span ending
// now, and otherwise evaluated off the frame.
func (inst *pickerState) render(ids *c.WidgetIdStack, w Window) {
	f := c.TimeRangePicker(ids.PrepareStr("range"), inst.fromExpr, inst.toExpr).Tz(inst.tzName)
	if w.Last == 0 && !w.From.IsZero() {
		f = f.EvaluatedBounds(w.From.UnixMilli(), w.To.UnixMilli())
	}
	for _, p := range inst.presets.All() {
		f = f.AddPreset(p.Label(), p.FromSQL(), p.ToSQL())
	}
	if !f.SendRespVal(&inst.packed).HasChanged() || inst.packed == "" {
		return
	}
	tz, from, to := timerangepicker.UnpackRange(inst.packed)
	inst.fromExpr, inst.toExpr = from, to
	if tz != "" {
		if id, err := timerangepicker.LookupTz(tz); err == nil {
			if loc, lerr := timerangepicker.LoadTzLocation(id); lerr == nil {
				inst.tzName, inst.loc = tz, loc
			}
		}
	}
	if d, ok := relativeSpan(from, to); ok {
		inst.err = nil
		inst.pending = &Window{Last: d}
		return
	}
	inst.err = inst.runner.start(from, to, inst.tzName)
}

// take returns a window the picker resolved since the last frame.
func (inst *pickerState) take() (w Window, ok bool) {
	if inst.pending != nil {
		w, ok = *inst.pending, true
		inst.pending = nil
		return
	}
	if res, rerr, done := inst.runner.take(); done {
		if rerr != nil {
			inst.err = rerr
			return
		}
		inst.err = nil
		return res, true
	}
	return
}

func (inst *pickerState) close() { inst.runner.close() }

// relativeSpan reads "anchor_now - INTERVAL n UNIT" to "anchor_now" as a
// span ending now, which the window re-resolves on every refresh. Other
// expressions are evaluated once into a fixed window.
func relativeSpan(from, to string) (d time.Duration, ok bool) {
	if strings.TrimSpace(to) != "anchor_now" {
		return
	}
	f := strings.Fields(from)
	if len(f) != 5 || f[0] != "anchor_now" || f[1] != "-" || !strings.EqualFold(f[2], "INTERVAL") {
		return
	}
	n, err := strconv.Atoi(f[3])
	if err != nil || n <= 0 {
		return
	}
	units := map[string]time.Duration{"SECOND": time.Second, "MINUTE": time.Minute, "HOUR": time.Hour, "DAY": 24 * time.Hour,
		"WEEK": 7 * 24 * time.Hour, "MONTH": 30 * 24 * time.Hour, "YEAR": 365 * 24 * time.Hour}
	u, found := units[strings.ToUpper(strings.TrimSuffix(f[4], "S"))]
	if !found {
		return
	}
	return time.Duration(n) * u, true
}
