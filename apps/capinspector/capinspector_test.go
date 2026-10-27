package capinspector

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func TestRegistry_HasAllShippedCaps(t *testing.T) {
	wantIds := []CapId{CapRun, CapFacts, CapBus, CapFs, CapPersist, CapTask, CapWatchbill, CapAppState, CapKeelsonQuery, CapAdhoc, CapLLM, CapHTTP, CapAgent}
	for _, id := range wantIds {
		_, ok := Registry[id]
		assert.True(t, ok, "Registry must contain %q", id)
	}
	assert.Len(t, Registry, 13, "exactly thirteen shipped caps; adding a cap means appending to caps.go")
}

func TestAllCapIdsOrdered_StableAcrossCalls(t *testing.T) {
	a := allCapIdsOrdered()
	b := allCapIdsOrdered()
	assert.Equal(t, a, b, "ordered list must be deterministic — UI shimmies otherwise")
}

func TestPushPopSelection_FifoSemantics(t *testing.T) {
	// Drain whatever is left from concurrent tests so we start clean.
	for popSelection() != "" {
	}
	PushSelection(CapFs)
	PushSelection(CapPersist)
	PushSelection(CapBus)
	assert.Equal(t, CapFs, popSelection())
	assert.Equal(t, CapPersist, popSelection())
	assert.Equal(t, CapBus, popSelection())
	assert.Equal(t, CapId(""), popSelection(), "drained queue must return empty")
}

func TestNewApp_CapturesSelectionAtConstruction(t *testing.T) {
	for popSelection() != "" {
	}
	PushSelection(CapFs)
	a := newApp()
	assert.Equal(t, CapFs, a.selectedCap)
	// Second app gets the next selection — independent windows.
	PushSelection(CapPersist)
	b := newApp()
	assert.Equal(t, CapPersist, b.selectedCap)
}

// The OpenOrRaise half of a status-bar click: no constructor runs, so the
// LIVE instance adopts the pushed selection at its next Frame. Without the
// drain the raised inspector would ignore the clicked chip and the push
// would leak to some later fresh open.
func TestConsumePendingSelection_AdoptsPushAfterRaise(t *testing.T) {
	for popSelection() != "" {
	}
	PushSelection(CapFs)
	a := newApp() // the original open consumed its click
	require.Equal(t, CapFs, a.selectedCap)

	// A later click while the window is already showing: push, raise —
	// no ctor. The next Frame's drain navigates.
	PushSelection(CapBus)
	a.consumePendingSelection()
	assert.Equal(t, CapBus, a.selectedCap)

	// An empty queue leaves the current selection alone — ordinary
	// frames must not reset what the in-window picker chose.
	a.selectedCap = CapPersist
	a.consumePendingSelection()
	assert.Equal(t, CapPersist, a.selectedCap)

	// A status-bar segment the Registry does not know must not blank the
	// page it was clicked over.
	PushSelection(CapId("no-such-cap"))
	a.consumePendingSelection()
	assert.Equal(t, CapPersist, a.selectedCap)
}

func TestNewApp_NoSelection_EmptyCap(t *testing.T) {
	for popSelection() != "" {
	}
	a := newApp()
	assert.Equal(t, CapId(""), a.selectedCap,
		"opened-without-click case must surface as empty cap so Frame can render the picker")
}

func TestCapSpec_FsAppFilter(t *testing.T) {
	spec := Registry[CapFs]
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.dialog.read"}))
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.handle.>"}))
	assert.False(t, spec.AppFilter(app.SubjectFilter{Pattern: "runtime.persist.x.>"}))
}

func TestCapSpec_PersistAppFilterAndHostInjected(t *testing.T) {
	spec := Registry[CapPersist]
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "runtime.persist.foo.>"}))
	assert.False(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.dialog.read"}))

	// HostInjected reflects manifest.PersistedKeys.
	// SubjectAlias takes the last path segment, so "github.com/example/play"
	// → alias "play" → pattern "runtime.persist.play.>".
	mPersist := app.Manifest{Id: "github.com/example/play", PersistedKeys: []string{"k"}}
	pat := spec.HostInjected(mPersist)
	assert.Equal(t, "runtime.persist.play.>", pat)

	mNone := app.Manifest{Id: "github.com/example/q"}
	assert.Empty(t, spec.HostInjected(mNone), "no keys → no injected cap")
}

func TestMatchedApps_PicksPersistConsumers(t *testing.T) {
	// Build a throwaway registry to avoid leaking test apps into
	// app.DefaultRegistry; matchedApps reads from
	// app.DefaultRegistry directly, so a unit-test for the filtering
	// logic uses the predicates directly.
	spec := Registry[CapPersist]
	consumer := app.Manifest{
		Id:            "test.consumer",
		PersistedKeys: []string{"x"},
	}
	require.True(t, spec.HostInjected(consumer) != "")

	nonconsumer := app.Manifest{Id: "test.none"}
	require.False(t, spec.HostInjected(nonconsumer) != "")
}

func TestDiagramCapLabel_CoversEveryCap(t *testing.T) {
	for _, capId := range allCapIdsOrdered() {
		got := diagramCapLabel(capId)
		assert.NotEmpty(t, got, "every shipped cap must have a diagram label, capId=%q", capId)
		// Labels fit roughly 18 chars in a 152px box at 12.5pt.
		assert.LessOrEqual(t, len(got), 18, "diagram label too long for the box, capId=%q label=%q", capId, got)
	}
}

func TestRegistry_BackendsPopulatedForEveryCap(t *testing.T) {
	for _, capId := range allCapIdsOrdered() {
		s, ok := Registry[capId]
		require.True(t, ok)
		require.NotEmpty(t, s.Backends, "every cap needs ≥1 BackendImpl, capId=%q", capId)
		for _, b := range s.Backends {
			assert.NotEmpty(t, b.Id, "backend id required, capId=%q", capId)
			assert.NotEmpty(t, b.Display, "backend display required, capId=%q backendId=%q", capId, b.Id)
		}
	}
}

func TestActiveBackend_SetGetRoundtrip(t *testing.T) {
	resetActiveBackends()
	defer resetActiveBackends()
	SetActiveBackend(CapFacts, "chstore")
	assert.Equal(t, "chstore", ActiveBackend(CapFacts))
	SetActiveBackend(CapFacts, "inmem") // overwrite
	assert.Equal(t, "inmem", ActiveBackend(CapFacts))
	assert.Equal(t, "", ActiveBackend(CapBus), "unset cap returns empty string")
}

func TestShortAppName(t *testing.T) {
	cases := map[app.AppIdT]string{
		"github.com/example/play":                  "play",
		"github.com/stergiotis/boxer/apps/capdemo": "capdemo",
		"flat": "flat",
		"":     "",
	}
	for in, want := range cases {
		got := shortAppName(app.Manifest{Id: in})
		assert.Equal(t, want, got, "in=%q", in)
	}
}

func TestCapGraphModel_NodesUniqueAndComplete(t *testing.T) {
	m := capGraphModel()

	// One App root and one card per cap, an edge App→cap each; a card's
	// rows are its cap's backends.
	caps := allCapIdsOrdered()
	assert.Len(t, m.Nodes, 1+len(caps))
	assert.Len(t, m.Edges, len(caps))

	seen := map[string]bool{}
	for _, n := range m.Nodes {
		assert.False(t, seen[n.ID], "duplicate node id %q", n.ID)
		seen[n.ID] = true
		assert.NotEmpty(t, n.Label, "node %q needs a label", n.ID)
	}
	for _, e := range m.Edges {
		assert.True(t, seen[e.From], "edge from undeclared node %q", e.From)
		assert.True(t, seen[e.To], "edge to undeclared node %q", e.To)
	}
	for i, capId := range caps {
		n := m.Nodes[1+i]
		require.Len(t, n.Rows, len(Registry[capId].Backends), "card %q lists every backend", n.ID)
		rowSeen := map[string]bool{}
		for j, b := range Registry[capId].Backends {
			assert.Equal(t, b.Id, n.Rows[j].ID)
			assert.False(t, rowSeen[b.Id], "duplicate row id %q in card %q", b.Id, n.ID)
			rowSeen[b.Id] = true
		}
	}
}

func TestBuildNodeMeta_ResolvesEveryNode(t *testing.T) {
	m := capGraphModel()
	meta := buildNodeMeta()
	assert.Len(t, meta, len(m.Nodes), "one meta entry per node")

	for _, n := range m.Nodes {
		md, ok := meta[n.ID]
		require.True(t, ok, "no meta for node %q", n.ID)
		switch md.kind {
		case nodeApp:
			assert.Equal(t, appNodeID, n.ID)
		case nodeCap:
			assert.Equal(t, capNodeID(md.cap), n.ID)
			_, isCap := Registry[md.cap]
			assert.True(t, isCap, "cap node %q resolves to a real cap", n.ID)
		default:
			t.Fatalf("node %q has unknown kind %d", n.ID, md.kind)
		}
	}
}

// fakeSized stands in for the engine's sizing: an App box and n cards of
// varying height, at arbitrary places, as dot would return them.
func fakeSized(n int) *layeredgraph.Layout {
	lay := &layeredgraph.Layout{FontSize: 14}
	lay.Nodes = append(lay.Nodes, layeredgraph.NodeLayout{ID: appNodeID, Center: layeredgraph.Point{X: 500, Y: 20}, W: 50, H: 36})
	for i := range n {
		h := 40.0 + 20*float64(i%3)
		cx := 100 + 150*float64(i)
		node := layeredgraph.NodeLayout{
			ID: capNodeID(CapId(string(rune('a' + i)))), Center: layeredgraph.Point{X: cx, Y: 80}, W: 100 + float64(i%4)*10, H: h,
			Title: layeredgraph.Rect{Min: layeredgraph.Point{X: cx - 50, Y: 80 - h/2}, Max: layeredgraph.Point{X: cx + 50, Y: 80 - h/2 + 20}},
		}
		node.Rows = []layeredgraph.RowLayout{{ID: "r", Rect: layeredgraph.Rect{Min: layeredgraph.Point{X: cx - 50, Y: 80 - h/2 + 20}, Max: layeredgraph.Point{X: cx + 50, Y: 80 + h/2}}}}
		lay.Nodes = append(lay.Nodes, node)
	}
	return lay
}

func TestPackArchGrid_CardsAlignAndStayInBounds(t *testing.T) {
	sized := fakeSized(13)
	for cols := 2; cols <= 13; cols++ {
		g := packArchGrid(sized, cols)
		require.Len(t, g.Nodes, 14)
		require.Len(t, g.Edges, 13)
		cardW := g.Nodes[1].W
		for i, n := range g.Nodes {
			assert.GreaterOrEqual(t, n.Center.X-n.W/2, 0.0, "cols=%d node %d left", cols, i)
			assert.LessOrEqual(t, n.Center.X+n.W/2, g.Width, "cols=%d node %d right", cols, i)
			assert.GreaterOrEqual(t, n.Center.Y-n.H/2, 0.0, "cols=%d node %d top", cols, i)
			assert.LessOrEqual(t, n.Center.Y+n.H/2, g.Height, "cols=%d node %d bottom", cols, i)
			if i == 0 {
				continue
			}
			assert.Equal(t, cardW, n.W, "cols=%d: cards share one width", cols)
			// Title and rows move with the card and stay inside it.
			top := n.Center.Y - n.H/2
			assert.Equal(t, top, n.Title.Min.Y, "cols=%d node %d title", cols, i)
			assert.LessOrEqual(t, n.Rows[0].Rect.Max.Y, n.Center.Y+n.H/2+1e-9)
		}
		for _, e := range g.Edges {
			require.Equal(t, 1, len(e.Points)%3, "cubic control points: 1+3k")
			require.NotNil(t, e.ArrowHead)
			to := g.Nodes[e.Index+1]
			assert.Equal(t, to.ID, e.To)
			assert.InDelta(t, to.Center.Y-to.H/2, e.ArrowHead.Y, 1e-9, "the arrow lands on the card's top")
			// The trunk runs in a gutter: it never crosses a card.
			x := e.Points[0].X
			for _, n := range g.Nodes[1:] {
				assert.False(t, x > n.Center.X-n.W/2 && x < n.Center.X+n.W/2, "cols=%d trunk crosses card %q", cols, n.ID)
			}
		}
	}
}

func TestPickArchGrid_PrefersFewestRowsAtNaturalSize(t *testing.T) {
	sized := fakeSized(13)
	grids := map[int]*layeredgraph.Layout{}
	wide := pickArchGrid(grids, sized, 1e6, 1e6)
	assert.Equal(t, packArchGrid(sized, 13).Width, wide.Width, "room for everything: one row")
	narrow := pickArchGrid(grids, sized, 500, 680)
	assert.Less(t, narrow.Width, wide.Width)
	assert.Greater(t, narrow.Height, wide.Height, "a narrow canvas wraps the cards")
}

// TestHelpCorpusDropsNothing is the ADR-0180 M2 zero-drops gate over the
// capability inspector's help corpus. A construct the markdown lowering has no
// case for is dropped silently, taking the prose it covered with it; asserting
// zero drops over a shipped corpus is what makes that visible.
func TestHelpCorpusDropsNothing(t *testing.T) {
	b, err := help.NewBook(ManifestId, help.MustSub(helpFS, "help"))
	require.NoError(t, err)
	docs := b.Docs()
	require.NotEmpty(t, docs, "help corpus must not be empty")
	for _, info := range docs {
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v",
			info.Path, doc.Dropped())
	}
}
