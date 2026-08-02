package widgets

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
)

// The icicle / flamegraph demo (ADR-0160): a stack hierarchy drawn as implot
// custom items. The value axis carries the tree's own units, so implot's
// pointer-anchored wheel zoom is the zoom the form wants and a double-click
// fit is "back to the whole profile"; clicking a frame assigns that axis to
// the frame's span, which keeps its ancestors on screen above it.
//
// The depth axis is the one that had to be told to behave: it is declared
// AxisFlagsNoZoom — the per-axis lock this ADR added to implot — so a gesture
// scrolls it without scaling it, and its span comes from the plot-area height
// so rows keep a stable pixel height however deep the tree goes. The
// "recursive descent" dataset is deeper than the pane to show that.

type icicleDemoDataset struct {
	name string
	unit string
	tree icicle.Tree
}

type icicleDemoState struct {
	datasets   []icicleDemoDataset
	datasetIdx int
	orientIdx  int
	colorIdx   int
	orderIdx   int
	legend     bool
	hideLabels bool
	selected   icicleview.Hit
	layout     *icicle.Layout
	status     string
	// resetView carries "the layout was just replaced" to the next frame, so
	// the plot's retained axis ranges are re-applied instead of leaving the
	// previous tree's value window in place.
	resetView bool
	// One Renderer, kept across frames so its scratch buffers survive; one
	// per pane is the contract.
	renderer icicleview.Renderer
}

var icicleOrientations = []struct {
	label  string
	orient icicle.OrientationE
}{
	{"icicle (root at top)", icicle.OrientIcicle},
	{"flamegraph (root at bottom)", icicle.OrientFlame},
}

var icicleColorModes = []struct {
	label string
	mode  icicleview.ColorModeE
}{
	{"by frame name", icicleview.ColorByLabel},
	{"by depth", icicleview.ColorByDepth},
}

var icicleOrders = []struct {
	label string
	order icicle.OrderE
}{
	{"widest first", icicle.OrderValueDesc},
	{"alphabetical", icicle.OrderLabel},
	{"input order", icicle.OrderInput},
}

// icicleDemoFold turns folded stacks — one root-first frame list per sample
// plus the value it carried, which is the shape a profile arrives in — into
// the widget's flat columns. It lives here rather than in the widget because
// it is about the producer's format, and it is short enough to show that
// adopting the columnar input costs a caller very little.
func icicleDemoFold(stacks [][]string, values []float64) icicle.Tree {
	type key struct {
		parent int32
		name   string
	}
	var t icicle.Tree
	index := make(map[key]int32)
	for si, stack := range stacks {
		parent := int32(-1)
		for _, name := range stack {
			k := key{parent: parent, name: name}
			id, seen := index[k]
			if !seen {
				id = int32(len(t.Labels))
				t.Labels = append(t.Labels, name)
				t.Parents = append(t.Parents, parent)
				t.Self = append(t.Self, 0)
				index[k] = id
			}
			parent = id
		}
		// A sample's value belongs to the frame it was taken in, which is the
		// leaf; every ancestor picks it up through the roll-up.
		if parent >= 0 {
			t.Self[parent] += values[si]
		}
	}
	return t
}

// icicleDemoCPU is a synthetic CPU profile of a query service: a handler path
// that splits into parsing, planning and execution, plus a background
// compaction goroutine as a second root.
func icicleDemoCPU() icicle.Tree {
	stacks := [][]string{
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "sql.Parse", "lexer.Next"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "sql.Parse", "parser.expr"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "sql.Parse", "parser.expr", "runtime.mallocgc"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "plan.Build", "plan.pushdown"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "plan.Build", "plan.costModel"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "exec.Run", "exec.scan", "storage.readBlock"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "exec.Run", "exec.scan", "storage.decompress"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "exec.Run", "exec.aggregate"},
		{"main", "server.Serve", "http.conn.serve", "api.handleQuery", "exec.Run", "exec.aggregate", "runtime.mapassign"},
		{"main", "server.Serve", "http.conn.serve", "api.encodeJSON"},
		{"main", "server.Serve", "http.conn.serve", "api.encodeJSON", "runtime.mallocgc"},
		{"main", "server.Serve", "net.accept"},
		{"main", "runtime.gcBgMarkWorker"},
		{"compactor", "compact.Loop", "compact.merge", "storage.readBlock"},
		{"compactor", "compact.Loop", "compact.merge", "storage.writeBlock"},
		{"compactor", "compact.Loop", "runtime.gcBgMarkWorker"},
	}
	values := []float64{
		140, 310, 95,
		180, 120,
		620, 410, 350, 130,
		240, 85,
		60,
		220,
		280, 190, 70,
	}
	return icicleDemoFold(stacks, values)
}

// icicleDemoDisk is a directory tree — shallow, wide, and with real self size
// at interior nodes (files that live in a directory that also has
// subdirectories), which is what the uncovered tail of a bar shows.
func icicleDemoDisk() icicle.Tree {
	stacks := [][]string{
		{"/", "usr", "lib"},
		{"/", "usr", "share", "doc"},
		{"/", "usr", "share", "icons"},
		{"/", "usr", "bin"},
		{"/", "var", "log"},
		{"/", "var", "cache"},
		{"/", "var", "lib"},
		{"/", "home", "work", "repo"},
		{"/", "home", "work", "build"},
		{"/", "home", "media"},
		{"/", "boot"},
	}
	values := []float64{4200, 1600, 380, 720, 2100, 1450, 990, 5300, 2800, 3100, 260}
	t := icicleDemoFold(stacks, values)
	// Loose files directly in directories that also have subdirectories.
	for i, label := range t.Labels {
		switch label {
		case "usr", "var", "home", "work":
			t.Self[i] += 140
		}
	}
	return t
}

// icicleDemoRecursive is a deliberately deep tree: a recursive-descent parser
// nested far past what the pane can show at one row per 18 px, so the rows
// stay their fixed height and the depth axis scrolls instead of squashing.
func icicleDemoRecursive() icicle.Tree {
	var t icicle.Tree
	add := func(label string, parent int32, self float64) int32 {
		t.Labels = append(t.Labels, label)
		t.Parents = append(t.Parents, parent)
		t.Self = append(t.Self, self)
		return int32(len(t.Labels) - 1)
	}
	root := add("parse", -1, 30)
	parent := root
	for d := range 28 {
		// Alternate the two mutually recursive productions, and let each level
		// keep a little time of its own so every bar has a visible tail.
		name := "expr"
		if d%2 == 1 {
			name = "term"
		}
		node := add(fmt.Sprintf("%s@%d", name, d), parent, 25)
		if d%4 == 3 {
			add("token.Next", node, 60)
		}
		parent = node
	}
	add("literal", parent, 120)
	return t
}

func init() {
	registry.Register(registry.Demo{
		Name:        "icicle",
		Category:    "Graphics & canvas",
		Title:       icons.PhFlame + " icicle / flamegraph",
		Stage:       [2]float32{900, 600},
		Kind:        registry.DemoKindMixed,
		Description: "Stack hierarchies on the implot custom-item lane (ADR-0160): one row per depth, a frame's width is its value, and the part no child covers is its own self time. The value axis carries the tree's units, so implot supplies the gestures — the wheel zooms anchored at the pointer, clicking a frame assigns the axis to that frame's span (ancestors stay above it, full width), a double-click fits back. The depth axis is declared NoZoom, the per-axis lock this ADR added to implot, so a drag scrolls depth without scaling rows; the recursive-descent dataset is deeper than the pane and shows it. Colour is a hash of the frame name, so a function keeps its colour everywhere, or a ramp over depth.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &icicleDemoState{
				datasets: []icicleDemoDataset{
					{name: "CPU profile", unit: "samples", tree: icicleDemoCPU()},
					{name: "disk usage", unit: "MiB", tree: icicleDemoDisk()},
					{name: "recursive descent (deep)", unit: "samples", tree: icicleDemoRecursive()},
				},
			}
			st.recompute()
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoIcicle(ids, state.(*icicleDemoState))
		},
		SourceFunc: demoIcicle,
	})
}

// recompute lays the current dataset out again. The layout depends on the
// orientation and the sibling order, so it is rebuilt when either changes
// rather than every frame; a live host would memoize on a tree fingerprint
// the same way.
func (st *icicleDemoState) recompute() {
	ds := &st.datasets[st.datasetIdx]
	lay, err := icicle.Compute(ds.tree, icicle.Options{
		Orientation: icicleOrientations[st.orientIdx].orient,
		Order:       icicleOrders[st.orderIdx].order,
		Unit:        ds.unit,
	})
	if err != nil {
		st.layout, st.status = nil, "layout: "+err.Error()
		return
	}
	st.layout, st.status = lay, ""
	st.selected = icicleview.Hit{} // indices do not carry across layouts
	st.resetView = true
}

func demoIcicle(ids *c.WidgetIdStack, st *icicleDemoState) {
	// caption is drawn beside the box; the ComboBox's own label is left empty
	// so the widget id does not end up on screen as text.
	combo := func(key string, idBase uint64, caption string, count int, labelAt func(int) string, selected int, pick func(int)) {
		c.Label(caption).Send()
		c.AddSpace(padInner())
		for range c.ComboBox(ids.PrepareStr(key),
			c.WidgetText().Text("").Keep(),
			c.WidgetText().Text(labelAt(selected)).Keep()).KeepIter() {
			for i := range count {
				isSel := i == selected
				if c.Button(ids.PrepareSeq(idBase+uint64(i)),
					c.Atoms().Text(labelAt(i)).Keep()).
					Selected(isSel).
					FrameWhenInactive(!isSel).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					pick(i)
				}
			}
		}
	}

	for range c.HorizontalTop().KeepIter() {
		combo("ic-dataset-cb", 0x1C0000, "Dataset:", len(st.datasets),
			func(i int) string { return st.datasets[i].name }, st.datasetIdx,
			func(i int) {
				st.datasetIdx = i
				st.recompute()
			})
		c.AddSpace(gapSections())

		combo("ic-orient-cb", 0x1C1000, "Orientation:", len(icicleOrientations),
			func(i int) string { return icicleOrientations[i].label }, st.orientIdx,
			func(i int) {
				st.orientIdx = i
				st.recompute()
			})
		c.AddSpace(gapSections())

		combo("ic-order-cb", 0x1C2000, "Order:", len(icicleOrders),
			func(i int) string { return icicleOrders[i].label }, st.orderIdx,
			func(i int) {
				st.orderIdx = i
				st.recompute()
			})
	}
	for range c.HorizontalTop().KeepIter() {
		combo("ic-color-cb", 0x1C3000, "Colour:", len(icicleColorModes),
			func(i int) string { return icicleColorModes[i].label }, st.colorIdx,
			func(i int) { st.colorIdx = i })
		c.AddSpace(gapSections())

		c.Checkbox(ids.PrepareStr("ic-legend"), st.legend, "Layer legend").
			SendRespVal(&st.legend)
		c.AddSpace(gapSections())
		c.Checkbox(ids.PrepareStr("ic-hidelabels"), st.hideLabels, "Hide labels").
			SendRespVal(&st.hideLabels)
	}
	c.AddSpace(padInner())

	if st.layout == nil {
		c.LabelAtoms(c.Atoms().Text(st.status).Keep()).Send()
		return
	}

	hover, click, clicked := st.renderer.Show(ids, "stacks##icicledemo", 860, 300, st.layout, icicleview.Opts{
		Color:      icicleColorModes[st.colorIdx].mode,
		Legend:     st.legend,
		HideLabels: st.hideLabels,
		Selected:   st.selected,
		ResetView:  st.resetView,
	})
	st.resetView = false
	// Any click updates the pin, including one that landed on empty area —
	// that is what clears it. Show has already zoomed the value axis to a
	// clicked frame.
	if clicked {
		st.selected = click
	}

	c.LabelAtoms(c.Atoms().Text(icicleDemoStatus(st, hover)).Keep()).Send()
}

// icicleDemoStatus describes what the pointer is over, falling back to the
// layout's own report and a reminder of the gestures.
func icicleDemoStatus(st *icicleDemoState, hover icicleview.Hit) string {
	lay := st.layout
	rep := lay.Report
	describe := func(h icicleview.Hit) string {
		if !h.Ok {
			return ""
		}
		n := &lay.Nodes[h.Node]
		var path []string
		for _, ni := range lay.PathTo(int(h.Node)) {
			path = append(path, lay.Nodes[ni].Label)
		}
		return fmt.Sprintf("%s — %.0f %s total (%.1f%%), %.0f self · depth %d · %s",
			n.Label, n.Total, rep.Unit, 100*n.Total/rep.Total, n.Self, n.Depth,
			strings.Join(path, " › "))
	}
	if s := describe(hover); s != "" {
		return s
	}
	if s := describe(st.selected); s != "" {
		return "pinned: " + s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d frames · %d rows deep · %.0f %s total · wheel zooms, drag scrolls depth, click a frame to zoom to it, double-click to reset",
		rep.Nodes, rep.Rows, rep.Total, rep.Unit)
	if rep.Pruned > 0 {
		fmt.Fprintf(&b, " · %d frame(s) pruned (%.0f %s)", rep.Pruned, rep.PrunedValue, rep.Unit)
	}
	return b.String()
}
