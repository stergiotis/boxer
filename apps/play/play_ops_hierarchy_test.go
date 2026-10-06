package play

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
)

func TestHierarchyOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, pane := range []struct{ id, get, set, sel string }{
		{treemapPaneId, opGetTreemap, opSetTreemapOptions, opSelectTreemapNode},
		{iciclePaneId, opGetIcicle, opSetIcicleOptions, opSelectIcicleFrame},
	} {
		get, ok := m.Operations.Lookup(pane.get)
		require.True(t, ok, pane.get)
		assert.Equal(t, app.OperationEffectNone, get.Effect, pane.get)
		assert.True(t, get.Untrusted, pane.get)
		assert.Equal(t, []string{pane.id, opsResResult, opsResPanes}, get.Reads, pane.get)
		set, ok := m.Operations.Lookup(pane.set)
		require.True(t, ok, pane.set)
		assert.Equal(t, app.OperationEffectDocument, set.Effect, pane.set)
		assert.Equal(t, []string{pane.id}, set.Writes, pane.set)
		sel, ok := m.Operations.Lookup(pane.sel)
		require.True(t, ok, pane.sel)
		assert.Equal(t, app.OperationClassCommand, sel.Class, pane.sel)
		assert.Equal(t, app.OperationEffectDocument, sel.Effect, pane.sel)
		assert.False(t, sel.Untrusted, pane.sel)
		assert.True(t, sel.Agents, pane.sel)
		assert.Equal(t, []string{opsResSignals, pane.id}, sel.Writes, pane.sel)
		assert.NotEmpty(t, sel.Gesture, pane.sel)
	}
}

// treemapForest is two roots: disk (db 1, other 2) and net (eth0 4).
func treemapForest(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"disk", "db"}, {"disk", "other"}, {"net", "eth0"}}},
		icicleTestCol{name: "value", num: []float64{1, 2, 4}},
	)
	cl, reason := resolveHierarchy(rec.Schema(), treemapForm)
	require.Empty(t, reason)
	p.treemapDriver.noteExecuted(time.Unix(1, 0))
	p.treemapDriver.syncTree(rec, rec.Schema(), cl, nil)
	drawnPane(p, treemapPaneId, 3, rec.Schema())
	return rec
}

func TestGetTreemapListsAContainersChildren(t *testing.T) {
	l, h := opsLauncher(t)
	rec := treemapForest(t, l.inner)
	defer rec.Release()

	r := queryOp[TreemapReading](t, h, opGetTreemap, GetTreemapArgs{})
	assert.Equal(t, uint64(3), r.Drawn.ResultId)
	assert.Empty(t, r.Drawn.CannotDraw)
	assert.Equal(t, "folded", r.Input)
	require.NotNil(t, r.Total)
	assert.Equal(t, 7.0, *r.Total)
	assert.Equal(t, "depth", r.ColourBy, "no color column")
	assert.Equal(t, "drill", r.Show)
	assert.Nil(t, r.Colour)
	require.Len(t, r.Nodes, 2)
	assert.Equal(t, []string{"net"}, r.Nodes[0].Path, "largest first")
	assert.Equal(t, []string{"disk"}, r.Nodes[1].Path)
	assert.InDelta(t, 3.0/7, *r.Nodes[1].Share, 1e-9)
	assert.Equal(t, int32(2), r.Nodes[1].Children)

	r = queryOp[TreemapReading](t, h, opGetTreemap, GetTreemapArgs{Focus: []string{"disk"}, Limit: 1})
	require.NotNil(t, r.Focus)
	assert.Equal(t, []string{"disk"}, r.Focus.Path)
	require.Len(t, r.Nodes, 1)
	assert.Equal(t, []string{"disk", "other"}, r.Nodes[0].Path)
	assert.Equal(t, int32(1), r.More)

	require.Error(t, queryErr(t, h, opGetTreemap, GetTreemapArgs{Focus: []string{"nope"}}))
}

func TestSetTreemapOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := treemapForest(t, p)
	defer rec.Release()
	before := h.ResourceValue(opsResTreemap)
	show := "full"
	require.NoError(t, applyOp(t, h, opSetTreemapOptions, SetTreemapOptionsArgs{Show: &show}))
	assert.Equal(t, treemapNestAll, p.treemapDriver.nesting)
	assert.NotEqual(t, before, h.ResourceValue(opsResTreemap))

	value, bad := "value", "4 levels"
	err := applyOp(t, h, opSetTreemapOptions, SetTreemapOptionsArgs{ColourBy: &value})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no `color` column")
	require.Error(t, applyOp(t, h, opSetTreemapOptions, SetTreemapOptionsArgs{Show: &bad}))
	require.Error(t, applyOp(t, h, opSetTreemapOptions, SetTreemapOptionsArgs{}))
	depth := "depth"
	require.NoError(t, applyOp(t, h, opSetTreemapOptions, SetTreemapOptionsArgs{ColourBy: &depth}))
}

// The pin and selection_key move together; a container is not a leaf.
func TestSelectTreemapNodePinsAndPublishes(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := treemapForest(t, p)
	defer rec.Release()

	require.NoError(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Label: "db"}))
	assert.Equal(t, "db", p.treemapDriver.selected)
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "db", key)
	assert.Equal(t, "task:t", writer)
	r := queryOp[TreemapReading](t, h, opGetTreemap, GetTreemapArgs{})
	assert.Equal(t, "db", r.Pinned)

	require.NoError(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Path: []string{"net", "eth0"}}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "eth0", key)

	err := applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Path: []string{"disk"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container")
	require.Error(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Label: "nope"}))
	require.Error(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{}))
	require.Error(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Clear: true, Label: "db"}))
	assert.Equal(t, "eth0", p.treemapDriver.selected, "a refused call applies nothing")

	require.NoError(t, applyOp(t, h, opSelectTreemapNode, SelectTreemapNodeArgs{Clear: true}))
	assert.Empty(t, p.treemapDriver.selected)
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Empty(t, key)
}

// iciclePane lays out main › run (1), main › emit (2), main › tiny
// (0.001), the last under the 0.1% prune.
func iciclePane(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"main", "run"}, {"main", "emit"}, {"main", "tiny"}}},
		icicleTestCol{name: "value", num: []float64{1, 2, 0.001}},
	)
	cl, reason := resolveIcicleColumns(rec.Schema())
	require.Empty(t, reason)
	d := p.icicleDriver
	d.noteExecuted(time.Unix(1, 0))
	d.syncTree(rec, rec.Schema(), cl, nil)
	d.syncLayout()
	require.NotNil(t, d.layout)
	drawnPane(p, iciclePaneId, 6, rec.Schema())
	return rec
}

func TestGetIcicleListsByTotalAndBySelf(t *testing.T) {
	l, h := opsLauncher(t)
	rec := iciclePane(t, l.inner)
	defer rec.Release()

	r := queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{})
	assert.Equal(t, uint64(6), r.Drawn.ResultId)
	assert.Equal(t, int64(4), r.Frames)
	assert.Equal(t, int32(2), r.Depth)
	assert.InDelta(t, 3.001, *r.Total, 1e-9)
	assert.Equal(t, "icicle", r.Orientation)
	assert.Equal(t, "off", r.Prune)
	assert.True(t, r.Labels)
	require.Len(t, r.List, 1)
	assert.Equal(t, []string{"main"}, r.List[0].Path)

	r = queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{Focus: []string{"main"}})
	require.NotNil(t, r.Focus)
	require.Len(t, r.List, 3)
	assert.Equal(t, []string{"main", "emit"}, r.List[0].Path)
	assert.Equal(t, int32(1), r.List[0].Depth)

	r = queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{By: "self", Limit: 2})
	require.Len(t, r.List, 2)
	assert.Equal(t, []string{"main", "emit"}, r.List[0].Path)
	assert.Equal(t, []string{"main", "run"}, r.List[1].Path)
	assert.Equal(t, int32(2), r.More)

	require.Error(t, queryErr(t, h, opGetIcicle, GetIcicleArgs{By: "width"}))
	require.Error(t, queryErr(t, h, opGetIcicle, GetIcicleArgs{Focus: []string{"main", "nope"}}))
}

// A pin survives a re-layout by its path, and a prune that removes the
// pinned frame drops the pin and reports it.
func TestIcicleSelectAndOptionsKeepThePinByPath(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := iciclePane(t, p)
	defer rec.Release()
	d := p.icicleDriver

	require.NoError(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Path: []string{"main", "run"}}))
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "run", key)
	assert.Equal(t, "task:t", writer)

	name := "name"
	require.NoError(t, applyOp(t, h, opSetIcicleOptions, SetIcicleOptionsArgs{Order: &name}))
	assert.False(t, d.syncLayout(), "the pinned frame is still laid out")
	assert.Equal(t, "run", d.selectedLabel())
	r := queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{})
	assert.Equal(t, []string{"main", "run"}, r.Pinned)
	assert.Equal(t, "name", r.Order)

	require.NoError(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Label: "tiny"}))
	prune := "0.1%"
	require.NoError(t, applyOp(t, h, opSetIcicleOptions, SetIcicleOptionsArgs{Prune: &prune}))
	assert.True(t, d.syncLayout(), "prune left the pinned frame out")
	assert.Equal(t, icicleview.Hit{}, d.selected)
	r = queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{})
	assert.Equal(t, int64(1), r.Pruned)

	flame, off, bad := "flame", false, "sideways"
	require.NoError(t, applyOp(t, h, opSetIcicleOptions, SetIcicleOptionsArgs{Orientation: &flame, Labels: &off}))
	assert.True(t, d.hideLabels)
	require.Error(t, applyOp(t, h, opSetIcicleOptions, SetIcicleOptionsArgs{Orientation: &bad}))
	require.Error(t, applyOp(t, h, opSetIcicleOptions, SetIcicleOptionsArgs{}))
	require.Error(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Label: "tiny"}), "pruned away")
	require.NoError(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Clear: true}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Empty(t, key)
}

// A leaf or frame click goes through its select command, as the person,
// and keeps the pane as selection_key's writer.
func TestHierarchyClicksGoThroughTheirCommands(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	rec := treemapForest(t, p)
	defer rec.Release()
	p.treemapDriver.requestSelect(SelectTreemapNodeArgs{Label: "other"}, nil)
	e := lastEntry(t, eng)
	assert.Equal(t, opSelectTreemapNode, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, "other", p.treemapDriver.selected)
	_, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, treemapPaneId, writer)

	irec := iciclePane(t, p)
	defer irec.Release()
	p.icicleDriver.requestSelect(SelectIcicleFrameArgs{Path: []string{"main", "emit"}}, nil)
	e = lastEntry(t, eng)
	assert.Equal(t, opSelectIcicleFrame, e.Op)
	assert.Equal(t, "emit", p.icicleDriver.selectedLabel())

	bare, _ := opsLauncher(t)
	brec := treemapForest(t, bare.inner)
	defer brec.Release()
	bare.inner.treemapDriver.requestSelect(SelectTreemapNodeArgs{Label: "db"}, nil)
	key, writer := signalOf(t, bare.inner, signalSelectionKey)
	assert.Equal(t, "db", key, "without a host the select applies directly")
	assert.Equal(t, treemapPaneId, writer)

	// A driver no launcher routes publishes through the pane's emitter.
	d := bare.inner.treemapDriver
	d.onSelect = nil
	probe := &emitProbe{}
	d.requestSelect(SelectTreemapNodeArgs{Label: "other"}, probe)
	assert.Equal(t, []SignalID{signalSelectionKey}, probe.ids)
	assert.Equal(t, "other", d.selected)
}

// A frame number pins that frame, and a path given beside it must still
// lead there, so a stale click cannot pin whatever took its number.
func TestSelectIcicleFrameByNumber(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := iciclePane(t, p)
	defer rec.Release()
	d := p.icicleDriver
	at, ok := icicleFrameByPath(d.layout, []string{"main", "emit"})
	require.True(t, ok)
	frame := int32(at)

	require.NoError(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Frame: &frame}))
	assert.Equal(t, "emit", d.selectedLabel())
	require.NoError(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Frame: &frame, Path: []string{"main", "emit"}}))
	require.Error(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Frame: &frame, Path: []string{"main", "run"}}))
	far := int32(len(d.layout.Nodes))
	require.Error(t, applyOp(t, h, opSelectIcicleFrame, SelectIcicleFrameArgs{Frame: &far}))
	assert.Equal(t, "emit", d.selectedLabel(), "a refused select keeps the pin")

	r := queryOp[IcicleReading](t, h, opGetIcicle, GetIcicleArgs{})
	for _, f := range r.List {
		if slices.Equal(f.Path, []string{"main", "emit"}) {
			assert.Equal(t, frame, f.Frame)
		}
	}
}

// An exact id wins over a cut one, and a cut id that two ids share names
// neither.
func TestHierMatchLabelPrefersTheWholeId(t *testing.T) {
	long := strings.Repeat("a", 200)
	cut := hierPathLabel(long)
	require.NotEqual(t, long, cut)
	labels := []string{long, cut}
	at, amb := hierMatchLabel(cut, len(labels), func(i int) string { return labels[i] })
	assert.Equal(t, 1, at, "the exact id, not the longer one whose cut equals it")
	assert.Zero(t, amb)

	at, _ = hierMatchLabel(cut, 1, func(i int) string { return labels[i] })
	assert.Equal(t, 0, at, "a cut id names the one id it cuts from")

	other := long + "b"
	two := []string{long, other}
	at, amb = hierMatchLabel(cut, len(two), func(i int) string { return two[i] })
	assert.Equal(t, -1, at)
	assert.Equal(t, 2, amb)
}
