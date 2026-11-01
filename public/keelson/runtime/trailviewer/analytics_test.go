package trailviewer

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// chainRows are a task's rows, newest first: a model call, the action it
// asked for, a second call under that action and its action; and an action
// whose call the window does not hold.
func chainRows() []Row {
	ts := func(m int) int64 { return t0.Add(time.Duration(m) * time.Minute).UnixNano() }
	return []Row{
		{Id: 5, TsNs: ts(4), Kind: "agentAction", Task: "task-1", Ref: "act-2", ParentRef: "call-2", Headline: "second action", Status: "ok", ElapsedMs: 12},
		{Id: 4, TsNs: ts(3), Kind: "llmCall", Task: "task-1", Ref: "call-2", ParentRef: "act-1", Headline: "second call", Status: "stop"},
		{Id: 3, TsNs: ts(2), Kind: "agentAction", Task: "task-1", Ref: "act-1", ParentRef: "call-1", Headline: "first action", Status: "ok"},
		{Id: 2, TsNs: ts(1), Kind: "agentAction", Task: "task-1", ParentRef: "call-gone", Headline: "orphan action", Status: "denied"},
		{Id: 1, TsNs: ts(0), Kind: "llmCall", Task: "task-1", Ref: "call-1", Headline: "first call", Status: "tool_calls"},
	}
}

// The tree hangs each row off the row its parent-ref names; a row whose
// parent the window does not hold is a root, and get_tree answers in
// depth-first order, oldest sibling first.
func TestTheTreeIsBuiltFromParentRefs(t *testing.T) {
	tr := buildTree(chainRows())
	assert.Equal(t, 2, tr.roots)
	assert.Equal(t, 1, tr.orphan)
	nodes, err := tr.depthFirst()
	require.NoError(t, err)
	type nd struct {
		id     uint64
		depth  int
		parent uint64
	}
	var got []nd
	for _, n := range nodes {
		got = append(got, nd{n.Id, n.Depth, n.Parent})
	}
	assert.Equal(t, []nd{{1, 0, 0}, {3, 1, 1}, {4, 2, 3}, {5, 3, 4}, {2, 0, 0}}, got)
	assert.Equal(t, "5 nodes, 2 roots; 1 rows name a parent the window does not hold and are roots here", tr.line())

	// A cycle, which a well-formed trail does not hold, does not hang the
	// build: the node that closes it becomes a root.
	cyc := []Row{{Id: 2, Ref: "b", ParentRef: "a"}, {Id: 1, Ref: "a", ParentRef: "b"}}
	ct := buildTree(cyc)
	require.NoError(t, ct.t.Validate())
	assert.Equal(t, 1, ct.roots)
}

// A refill keeps the tree's selection by row id and does not reveal the
// selected node again, which would take back the reader's scroll every
// refresh; a new selection, or a selected row the refill first brings in,
// is revealed.
func TestARefillKeepsTheTreeSelectionWithoutRevealingIt(t *testing.T) {
	var tv treeView
	rows := chainRows()
	tr := tv.ensure(rows, 1)
	tv.project(3)
	node := tr.byId[3]
	require.Equal(t, node, tv.state.PendingReveal())
	assert.True(t, tv.state.IsSelected(node))
	tv.state.Reveal(-1) // the render consumed it

	// The refill shifts every node index: a newer row arrives and the
	// oldest leaves.
	newer := Row{Id: 6, TsNs: t0.Add(5 * time.Minute).UnixNano(), Kind: "llmCall", Task: "task-1", Ref: "call-3", ParentRef: "act-2"}
	rows2 := append([]Row{newer}, rows[:len(rows)-1]...)
	tr = tv.ensure(rows2, 2)
	tv.project(3)
	assert.Equal(t, int32(-1), tv.state.PendingReveal(), "a refill does not reveal again")
	assert.True(t, tv.state.IsSelected(tr.byId[3]), "the selection follows the row id")
	assert.Equal(t, 1, tv.state.SelectionLen())

	tv.project(5)
	assert.Equal(t, tr.byId[5], tv.state.PendingReveal(), "a new selection is revealed")
	tv.state.Reveal(-1)

	// A selected row the tree did not hold is revealed when a refill brings it.
	tv.project(9)
	assert.Equal(t, int32(-1), tv.state.PendingReveal())
	late := Row{Id: 9, TsNs: t0.Add(6 * time.Minute).UnixNano(), Kind: "agentAction", Task: "task-1", ParentRef: "call-3"}
	tr = tv.ensure(append([]Row{late}, rows2...), 3)
	tv.project(9)
	assert.Equal(t, tr.byId[9], tv.state.PendingReveal())
}

func TestGetTreeAnswersFromTheCache(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.applyCase(Case{Kind: CaseTask, Value: "task-1"})
	hold(inst, chainRows())
	out, err := query[GetTreeArgs, TreeResult](t, inst, opGetTree, GetTreeArgs{Case: "task", Value: "task-1"})
	require.NoError(t, err)
	assert.Empty(t, out.Diagnostic)
	assert.Equal(t, 5, out.Nodes)
	assert.Equal(t, 2, out.Roots)
	var ids []uint64
	for _, n := range out.Tree {
		ids = append(ids, n.Id)
	}
	assert.Equal(t, []uint64{1, 3, 4, 5, 2}, ids)
	assert.Equal(t, uint64(12), out.Tree[3].ElapsedMs)
	_, err = query[GetTreeArgs, TreeResult](t, inst, opGetTree, GetTreeArgs{Case: "run", Value: "r"})
	assert.NotNil(t, refusal(err), "a run has no chain")
	out, err = query[GetTreeArgs, TreeResult](t, inst, opGetTree, GetTreeArgs{Case: "task", Value: "task-2"})
	require.NoError(t, err)
	assert.NotEmpty(t, out.Diagnostic, "a case the window did not read")

	// The tree tab is a value of the tab resource, for a chained case only;
	// leaving the case for one without a chain leaves the tab.
	require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: "tree"}))
	require.NoError(t, command(t, inst, opSetCase, SetCaseArgs{Case: "run", Value: fixtureRun}))
	assert.Equal(t, TabFields, inst.tab)
}

func TestTheMatrixPivotsAndStatesItsLine(t *testing.T) {
	cells := []MatrixCell{
		{Actor: "apps/a", Object: "rules.updated", N: 5, Last: "ok"},
		{Actor: "apps/b", Object: "rules.updated", N: 2, Last: "denied"},
		{Actor: "apps/a", Object: "rules.created", N: 1, Last: "ok"},
		{Actor: "apps/c", Object: "bus.request", N: 1, Last: "ok"},
	}
	m := pivotMatrix(cells, ActorApp, ObjectOperation, matrixShown, false)
	assert.Equal(t, []string{"apps/a", "apps/b", "apps/c"}, m.actors)
	assert.Equal(t, []string{"rules.updated", "bus.request", "rules.created"}, m.objects)
	assert.Equal(t, [][]uint64{{5, 0, 1}, {2, 0, 0}, {0, 1, 0}}, m.counts)
	assert.Equal(t, "denied", m.last[1][0])
	assert.Equal(t, "3 actors × 3 objects, 9 rows in 4 pairs; busiest app apps/a → operation rules.updated with 5", m.line())

	cut := pivotMatrix(cells, ActorApp, ObjectOperation, 2, true)
	assert.Len(t, cut.actors, 2)
	assert.Contains(t, cut.line(), "the grid shows the 50 busiest of each")
	assert.Contains(t, cut.line(), "the read stopped at 10000 pairs")
	empty := pivotMatrix(nil, ActorPrincipal, ObjectEntity, matrixShown, false)
	assert.Equal(t, "0 pairs: no row in the case and window has both a principal and a entity", empty.line())
}

func TestTheDottedChartSizesMarksByCount(t *testing.T) {
	b0 := t0.Unix()
	groups := []Bucket{
		{Bucket: b0, Key: "run-a", N: 1}, {Bucket: b0 + 60, Key: "run-a", N: 16},
		{Bucket: b0, Key: "run-b", N: 4}, {Bucket: b0 + 120, Key: "run-c", N: 9},
	}
	d := buildDotted(groups, DottedRun, time.Minute, dottedShown, false)
	assert.Equal(t, []string{"run-a", "run-c", "run-b"}, d.keys, "busiest first")
	assert.Equal(t, uint64(16), d.maxN)
	assert.Equal(t, "3 rows over 3 buckets of 1m0s; busiest run run-a with 17; the largest mark is 16 in one bucket", d.line())
	// The radius follows the square root of the count, in four bounded sizes.
	assert.Equal(t, 0, tierOf(1, 16))
	assert.Equal(t, 1, tierOf(4, 16))
	assert.Equal(t, 2, tierOf(9, 16))
	assert.Equal(t, 3, tierOf(16, 16))
	assert.InDelta(t, dottedMinRadius, d.tiers[0].radius, 1e-6)
	assert.InDelta(t, dottedMaxRadius, d.tiers[3].radius, 1e-6)
	// The busiest row is drawn on top.
	assert.Equal(t, []float64{2}, d.tiers[3].ys)
	assert.Equal(t, []float64{float64(b0+60) + 30}, d.tiers[3].xs)
	few := buildDotted(groups, DottedRun, time.Minute, 1, false)
	assert.Contains(t, few.line(), "the chart shows the 40 busiest")
}

// The day × hour grid is built from the cells the read returned, a missing
// cell zero; the line states the busiest cell, the share outside Mon–Fri
// working hours and the zone, and says when the zone fell back to UTC.
func TestTheDayHourGridCountsOffHours(t *testing.T) {
	cells := []DayHourCell{
		{Weekday: 1, Hour: 9, N: 10},  // Mon 09, working hours
		{Weekday: 3, Hour: 17, N: 20}, // Wed 17, working hours
		{Weekday: 3, Hour: 18, N: 5},  // Wed 18, after hours
		{Weekday: 5, Hour: 7, N: 5},   // Fri 07, before hours
		{Weekday: 7, Hour: 12, N: 10}, // Sun 12, weekend
		{Weekday: 9, Hour: 1, N: 99},  // not a weekday: dropped
	}
	d := buildDayHour(cells, "Europe/Berlin", false)
	assert.Equal(t, uint64(50), d.rows)
	assert.Equal(t, 5, d.cells)
	assert.Equal(t, uint64(20), d.counts[2][17])
	assert.Equal(t, uint64(0), d.counts[2][16], "a cell the read did not return is zero")
	assert.Equal(t, uint64(20), d.maxN)
	assert.Equal(t, 2, d.busiestD)
	assert.Equal(t, 17, d.busiestH)
	assert.Equal(t, uint64(20), d.off)
	assert.Equal(t, 40, d.offShare())
	assert.Equal(t, "50 rows; busiest Wed 17:00 with 20; 40% outside Mon–Fri 08:00–18:00 Europe/Berlin", d.line())
	fb := buildDayHour(cells[:1], "UTC", true)
	assert.Equal(t, "10 rows; busiest Mon 09:00 with 10; 0% outside Mon–Fri 08:00–18:00 UTC (the picker's zone has no IANA name)", fb.line())
	empty := buildDayHour(nil, "UTC", false)
	assert.Equal(t, "0 rows in the case and window; hours in UTC", empty.line())
	// The tint follows the count, translucent, with the design system's
	// palette; a zero count against zero is the palette's start.
	assert.NotEqual(t, dayHourTint(1, 20), dayHourTint(20, 20))
	assert.NotPanics(t, func() { dayHourTint(0, 0) })
}

// The series is built from the buckets the read returned, each point at
// its bucket's middle; the line states the pick, the buckets, the peak,
// the total for a sum, and what was cut.
func TestTheSeriesStatesItsPeakAndTotal(t *testing.T) {
	b0 := t0.Unix()
	points := []SeriesPoint{{Bucket: b0, N: 2, V: 10}, {Bucket: b0 + 60, N: 5, V: 42.5}, {Bucket: b0 + 180, N: 1, V: 7}}
	d := buildSeries(points, seriesPick{Measure: MeasureTokensIn, Stat: StatSum}, time.Minute, false)
	assert.Equal(t, []float64{float64(b0) + 30, float64(b0+60) + 30, float64(b0+180) + 30}, d.xs)
	assert.Equal(t, []float64{10, 42.5, 7}, d.ys)
	assert.Equal(t, uint64(8), d.rows)
	assert.Equal(t, b0+60, d.peakT)
	assert.Equal(t, 42.5, d.peakV)
	assert.Equal(t, 59.5, d.total)
	assert.Equal(t, "tokens-in sum over 3 buckets of 1m0s from 8 rows; peak 2026-10-31 12:01:00 UTC with 42.5; total 59.5", d.line())
	p95 := buildSeries(points, seriesPick{Measure: MeasureElapsedMs, Stat: StatP95}, time.Minute, true)
	assert.Equal(t, "elapsed-ms p95 over 3 buckets of 1m0s from 8 rows; peak 2026-10-31 12:01:00 UTC with 42.5; the read stopped at 2000 buckets", p95.line())
	rows := buildSeries(points[:1], seriesPick{Measure: MeasureRows}, time.Minute, false)
	assert.Equal(t, "rows over 1 buckets of 1m0s from 2 rows; peak 2026-10-31 12:00:00 UTC with 10; total 10", rows.line())
	noOut := buildSeries(nil, seriesPick{Measure: MeasureTokensOut, Stat: StatMax}, time.Minute, false)
	assert.Equal(t, "0 rows in the case and window carry tokens-out", noOut.line())
	none := buildSeries(nil, seriesPick{}, time.Minute, false)
	assert.Equal(t, "0 rows in the case and window", none.line())
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	assert.Contains(t, d.lineIn(loc), "peak 2026-10-31 13:01:00 CET")
}

// The two tabs are values of the tab resource, by name.
func TestTheNewTabsParse(t *testing.T) {
	for name, tab := range map[string]TabE{"dayhour": TabDayHour, "series": TabSeries, "Series ": TabSeries} {
		got, ok := ParseTab(name)
		assert.True(t, ok, name)
		assert.Equal(t, tab, got, name)
		assert.True(t, tab.availableFor(CaseRun))
		assert.False(t, tab.detail())
	}
	assert.Equal(t, "Day × hour", TabDayHour.Label())
	assert.Equal(t, "Series", TabSeries.Label())
	assert.Contains(t, ViewTabs, TabDayHour)
	assert.Contains(t, ViewTabs, TabSeries)
}

// set_series sets the measure and statistic and never toggles; a statistic
// the measure has not is refused and changes nothing; the combos route
// through the same handler.
func TestSetSeriesSetsAndNeverToggles(t *testing.T) {
	inst, _ := opsFixture(t)
	assert.Equal(t, seriesPick{Measure: MeasureRows, Stat: StatSum}, inst.an.series.pick, "rows, the count, by default")
	for range 2 {
		require.NoError(t, command(t, inst, opSetSeries, SetSeriesArgs{Measure: "tokens-in", Statistic: "p95"}))
		assert.Equal(t, seriesPick{Measure: MeasureTokensIn, Stat: StatP95}, inst.an.series.pick)
	}
	assert.Equal(t, seriesPick{Measure: MeasureTokensIn, Stat: StatP95}, inst.Operations().ResourceValue(resSeries))
	assert.NotNil(t, refusal(command(t, inst, opSetSeries, SetSeriesArgs{Measure: "rows", Statistic: "p95"})))
	assert.NotNil(t, refusal(command(t, inst, opSetSeries, SetSeriesArgs{Measure: "bytes", Statistic: "sum"})))
	assert.NotNil(t, refusal(command(t, inst, opSetSeries, SetSeriesArgs{Measure: "elapsed-ms", Statistic: "mean"})))
	assert.Equal(t, seriesPick{Measure: MeasureTokensIn, Stat: StatP95}, inst.an.series.pick, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opSetSeries, SetSeriesArgs{Measure: "rows"}))
	assert.Equal(t, seriesPick{Measure: MeasureRows, Stat: StatSum}, inst.an.series.pick, "an empty statistic is sum")
	inst.setSeriesGesture(seriesPick{Measure: MeasureElapsedMs, Stat: StatMax})
	assert.Equal(t, seriesPick{Measure: MeasureElapsedMs, Stat: StatMax}, inst.an.series.pick)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "elapsed-ms max", v.Series)
	for _, name := range []string{"dayhour", "series"} {
		require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: name}))
		assert.Equal(t, name, inst.tab.String())
	}
}

// get_dayhour and get_series answer from the views' caches for the read
// the window made, and say when the view is not read or the arguments
// name another read.
func TestGetDayHourAndGetSeriesAnswerFromTheCache(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.cache.step = time.Minute
	args := GetDayHourArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{From: inst.window.From, To: inst.window.To}}
	out, err := query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour, args)
	require.NoError(t, err)
	assert.Contains(t, out.Diagnostic, "set_tab dayhour")
	assert.Equal(t, "Mon–Fri 08:00–18:00", out.WorkingHours)

	hkey := inst.dayHourKey()
	inst.an.dayhour.data = buildDayHour([]DayHourCell{{Weekday: 2, Hour: 9, N: 4}, {Weekday: 6, Hour: 23, N: 1}}, "UTC", false)
	inst.an.dayhour.has, inst.an.dayhour.key, inst.an.dayhour.asked = true, hkey, hkey
	out, err = query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour, args)
	require.NoError(t, err)
	assert.Empty(t, out.Diagnostic)
	assert.Equal(t, uint64(5), out.Rows)
	assert.Equal(t, []DayHourCellOut{{Weekday: "Tue", Hour: 9, Rows: 4}, {Weekday: "Sat", Hour: 23, Rows: 1}}, out.Cells)
	assert.Equal(t, "Tue", out.BusiestWeekday)
	assert.Equal(t, 9, out.BusiestHour)
	assert.Equal(t, 20, out.OffHoursShare)
	assert.Equal(t, "UTC", out.Zone)
	assert.Equal(t, inst.an.dayhour.data.line(), out.Line)
	// Another case, a narrower range or a wider filter is another read.
	other, err := query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour, GetDayHourArgs{Case: "run", Value: "other", Range: args.Range})
	require.NoError(t, err)
	assert.Contains(t, other.Diagnostic, "set_case")
	narrow, err := query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour,
		GetDayHourArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{From: inst.window.From, To: inst.window.To.Add(-time.Minute)}})
	require.NoError(t, err)
	assert.Contains(t, narrow.Diagnostic, "set_range")
	filtered, err := query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour,
		GetDayHourArgs{Case: "run", Value: fixtureRun, Range: args.Range, Filter: []TermArg{{Facet: "kind", Value: "llmCall"}}})
	require.NoError(t, err)
	assert.Contains(t, filtered.Diagnostic, "set_filter")
	// A fill since the read makes the view stale: answered, and said.
	inst.cache.gen++
	stale, err := query[GetDayHourArgs, DayHourResult](t, inst, opGetDayHour, args)
	require.NoError(t, err)
	assert.Contains(t, stale.Diagnostic, "ask again shortly")
	assert.Equal(t, uint64(5), stale.Rows)
	inst.cache.gen--

	sargs := GetSeriesArgs{Case: "run", Value: fixtureRun, Range: args.Range}
	sout, err := query[GetSeriesArgs, SeriesResult](t, inst, opGetSeries, sargs)
	require.NoError(t, err)
	assert.Contains(t, sout.Diagnostic, "set_tab series")
	assert.Equal(t, "rows", sout.Measure)
	skey := inst.seriesKey()
	inst.an.series.data.data = buildSeries([]SeriesPoint{{Bucket: t0.Unix(), N: 3, V: 3}, {Bucket: t0.Unix() + 60, N: 1, V: 1}}, seriesPick{}, time.Minute, false)
	inst.an.series.data.has, inst.an.series.data.key, inst.an.series.data.asked = true, skey, skey
	sout, err = query[GetSeriesArgs, SeriesResult](t, inst, opGetSeries, sargs)
	require.NoError(t, err)
	assert.Empty(t, sout.Diagnostic)
	assert.Equal(t, "rows", sout.Measure)
	assert.Equal(t, "sum", sout.Statistic)
	assert.Equal(t, int64(60000), sout.StepMs)
	assert.Equal(t, 2, sout.Buckets)
	assert.Equal(t, uint64(4), sout.Rows)
	assert.Equal(t, t0, sout.PeakAt)
	assert.Equal(t, 3.0, sout.Peak)
	assert.Equal(t, 4.0, sout.Total)
	assert.Equal(t, []SeriesBucketOut{{Start: t0, Rows: 3, Value: 3}, {Start: t0.Add(time.Minute), Rows: 1, Value: 1}}, sout.Series)
	sargs.Limit = 1
	sout, err = query[GetSeriesArgs, SeriesResult](t, inst, opGetSeries, sargs)
	require.NoError(t, err)
	assert.Len(t, sout.Series, 1, "bounded")
	assert.Equal(t, 2, sout.Buckets)
	// Another pick is read by set_series, not here.
	sargs.Measure, sargs.Statistic = "tokens-in", "p95"
	sout, err = query[GetSeriesArgs, SeriesResult](t, inst, opGetSeries, sargs)
	require.NoError(t, err)
	assert.Contains(t, sout.Diagnostic, "set_series reads tokens-in p95")
	assert.Empty(t, sout.Series)
	sargs.Statistic = "mean"
	_, err = query[GetSeriesArgs, SeriesResult](t, inst, opGetSeries, sargs)
	assert.NotNil(t, refusal(err))
}

// fakeStorage is an in-memory persist store.
type fakeStorage struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (inst *fakeStorage) Get(key string) ([]byte, bool, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	v, ok := inst.m[key]
	return v, ok, nil
}
func (inst *fakeStorage) Set(key string, value []byte) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.m == nil {
		inst.m = map[string][]byte{}
	}
	inst.m[key] = value
	return nil
}
func (inst *fakeStorage) Delete(key string) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	delete(inst.m, key)
	return nil
}

var _ app.StorageI = (*fakeStorage)(nil)

// A saved view keeps the case, the window, the filter and the tab, through
// the store and back into a second window.
func TestASavedViewRoundTrips(t *testing.T) {
	st := &fakeStorage{}
	inst, _ := opsFixture(t)
	inst.views.store = st
	inst.applyCase(Case{Kind: CaseTask, Value: "task-1"})
	inst.applyWindow(Window{Last: 6 * time.Hour})
	inst.applyFilter(Filter{}.with(FacetKind, "agentAction", false).with(FacetStatus, "ok", true))
	inst.applyTab(TabTree)
	inst.picker.fromExpr = "anchor_now - INTERVAL 6 HOUR"

	settle := func(a *App) {
		require.Eventually(t, func() bool { a.tickViews(); return a.views.loaded && !a.views.runner.Running() }, 5*time.Second, 5*time.Millisecond)
	}
	settle(inst)
	assert.Empty(t, inst.views.list)
	inst.saveView("  my task ")
	settle(inst)
	require.NoError(t, inst.views.err)
	assert.Equal(t, []string{"my task"}, inst.views.names())
	stored, err := loadViews(st)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "6h0m0s", stored[0].Last)
	assert.Equal(t, "tree", stored[0].Tab)

	// Saving under the same name replaces it.
	inst.applyTab(TabSeries)
	inst.saveView("my task")
	settle(inst)
	stored, _ = loadViews(st)
	require.Len(t, stored, 1)
	assert.Equal(t, "series", stored[0].Tab)

	other := newInstance(manifest)
	other.views.store = st
	other.views.wantOpen = "my task"
	settle(other)
	other.tickViews()
	require.NoError(t, other.views.err)
	assert.Equal(t, Case{Kind: CaseTask, Value: "task-1"}, other.caseValue)
	assert.Equal(t, Window{Last: 6 * time.Hour}, other.window)
	assert.Equal(t, inst.filter.key(), other.filter.key())
	assert.Equal(t, TabSeries, other.tab)
	assert.Equal(t, "anchor_now - INTERVAL 6 HOUR", other.picker.fromExpr)

	inst.writeViews(withoutView(inst.views.list, "my task"), "deleted")
	settle(inst)
	stored, _ = loadViews(st)
	assert.Empty(t, stored)
}

func TestGetViewListsTheSavedViews(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.views.list = []SavedView{{Name: "a"}, {Name: "b"}}
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, v.SavedViews)
}

func mountWith(t *testing.T, cfg []byte) *App {
	t.Helper()
	ctx := app.NewStaticMountContext(manifest.Id, zerolog.Nop(), &fakeStorage{}, nil, nil)
	ctx.SetLaunchConfig(cfg)
	inst := newInstance(manifest)
	require.NoError(t, inst.Mount(ctx))
	return inst
}

// Mount seeds the case, the window and the tab from a launch config, and
// ignores one that does not read, saying why.
func TestMountSeedsFromTheLaunchConfig(t *testing.T) {
	wire, err := buscodec.Encode(launchcfg.TrailViewerLaunch{Case: "conversation", Value: "conv-1", LastNanos: int64(48 * time.Hour), Tab: "tree", View: "v"})
	require.NoError(t, err)
	inst := mountWith(t, wire)
	require.NoError(t, inst.launchErr)
	assert.Equal(t, Case{Kind: CaseConversation, Value: "conv-1"}, inst.caseValue)
	assert.Equal(t, Window{Last: 48 * time.Hour}, inst.window)
	assert.Equal(t, TabTree, inst.tab)
	assert.Equal(t, "v", inst.views.wantOpen)
	assert.Equal(t, "anchor_now - INTERVAL 2 DAY", inst.picker.fromExpr)
	inst.ensureInit()
	assert.Equal(t, Case{Kind: CaseConversation, Value: "conv-1"}, inst.caseValue, "the default run does not replace a launched case")

	from := t0
	wire, err = buscodec.Encode(launchcfg.TrailViewerLaunch{Case: "entity", Value: "rule:r1", FromNanos: from.UnixNano(), ToNanos: from.Add(time.Hour).UnixNano()})
	require.NoError(t, err)
	inst = mountWith(t, wire)
	require.NoError(t, inst.launchErr)
	assert.Equal(t, Window{From: from, To: from.Add(time.Hour)}, inst.window)

	for name, cfg := range map[string]launchcfg.TrailViewerLaunch{
		"bad case":       {Case: "sideways", Value: "x"},
		"bad value":      {Case: "window", Value: "nocolon"},
		"half a window":  {Case: "run", Value: "r", FromNanos: from.UnixNano()},
		"bad tab":        {Case: "run", Value: "r", Tab: "sideways"},
		"tree for a run": {Case: "run", Value: "r", Tab: "tree"},
	} {
		wire, err = buscodec.Encode(cfg)
		require.NoError(t, err)
		inst = mountWith(t, wire)
		assert.Error(t, inst.launchErr, name)
		assert.Equal(t, Case{}, inst.caseValue, "%s: nothing is seeded", name)
		assert.Equal(t, defaultWindow, inst.window, name)
	}
	inst = mountWith(t, []byte("not a config"))
	assert.ErrorContains(t, inst.launchErr, "does not decode")
}

// The analytic views and the tree render under the discard channel.
func TestTheAnalyticViewsRender(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst, _ := opsFixture(t)
	inst.applyCase(Case{Kind: CaseTask, Value: "task-1"})
	hold(inst, chainRows())
	inst.cache.step = time.Minute
	key := inst.matrixKey()
	inst.an.matrix.data = pivotMatrix([]MatrixCell{{Actor: "a", Object: "o", N: 2, Last: "ok"}}, ActorApp, ObjectOperation, matrixShown, false)
	inst.an.matrix.has, inst.an.matrix.key, inst.an.matrix.asked, inst.an.matrix.ver = true, key, key, 1
	dkey := inst.dottedKey()
	inst.an.dotted.data = buildDotted([]Bucket{{Bucket: t0.Unix(), Key: "run-a", N: 3}}, DottedRun, time.Minute, dottedShown, false)
	inst.an.dotted.has, inst.an.dotted.key, inst.an.dotted.asked = true, dkey, dkey
	hkey := inst.dayHourKey()
	inst.an.dayhour.data = buildDayHour([]DayHourCell{{Weekday: 2, Hour: 9, N: 4}, {Weekday: 6, Hour: 23, N: 1}}, "UTC", false)
	inst.an.dayhour.has, inst.an.dayhour.key, inst.an.dayhour.asked, inst.an.dayhour.ver = true, hkey, hkey, 1
	skey := inst.seriesKey()
	inst.an.series.data.data = buildSeries([]SeriesPoint{{Bucket: t0.Unix(), N: 3, V: 3}}, seriesPick{}, time.Minute, false)
	inst.an.series.data.has, inst.an.series.data.key, inst.an.series.data.asked = true, skey, skey
	require.True(t, inst.selectById(3))
	frame := func(msg string) {
		for range c.IdScope(inst.ids.PrepareStr("window")) {
			assert.NotPanics(t, func() { require.NoError(t, inst.render()) }, msg)
		}
		c.CurrentApplicationState.StateManager.ScriptReset()
	}
	for _, tab := range ViewTabs {
		inst.applyTab(tab)
		frame(tab.String())
		frame(tab.String() + " again")
	}
	inst.side = sideViews
	inst.views.list = []SavedView{{Name: "v", Case: "run", Value: "r", Last: "1h0m0s"}}
	inst.views.loaded = true
	frame("views")
}
