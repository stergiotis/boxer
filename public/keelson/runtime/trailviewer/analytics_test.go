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
	inst.applyTab(TabMatrix)
	inst.saveView("my task")
	settle(inst)
	stored, _ = loadViews(st)
	require.Len(t, stored, 1)
	assert.Equal(t, "matrix", stored[0].Tab)

	other := newInstance(manifest)
	other.views.store = st
	other.views.wantOpen = "my task"
	settle(other)
	other.tickViews()
	require.NoError(t, other.views.err)
	assert.Equal(t, Case{Kind: CaseTask, Value: "task-1"}, other.caseValue)
	assert.Equal(t, Window{Last: 6 * time.Hour}, other.window)
	assert.Equal(t, inst.filter.key(), other.filter.key())
	assert.Equal(t, TabMatrix, other.tab)
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
