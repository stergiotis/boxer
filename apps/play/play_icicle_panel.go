package play

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_icicle_panel.go is the Icicle dock tab: the active result drawn as an
// icicle plot or a flamegraph over the icicle widget (ADR-0160), whose §SD8
// deferred "a generic `play` panel over a stack/value column convention" as one
// of the two obvious next slices. This is that slice.
//
// TWO CONTRACTS, discriminated by the columns present, because the hierarchies
// that reach a SQL result arrive in two shapes and neither converts to the
// other in a line of SQL:
//
//   - FOLDED — `stack` (an Array) + `value`: one row per root-to-leaf path,
//     the path carried as an array. The panel interns the paths into a trie and
//     synthesises the interior frames. This is what a pprof capture already is
//     (pprofarrow emits `stack List<String>` root-first, one row per unique
//     stack, `value` that stack's own samples), and what any delimited path
//     reaches with one splitByChar.
//   - NODES — `id` + `parent` + `value`: one row per node, mapping 1:1 onto
//     icicle.Tree. What a recursive CTE or a self-join emits, and the only one
//     of the two in which an INTERIOR node can carry a value of its own.
//
// A folded `stack` wins when a schema satisfies both, being the more specific
// claim; the status line names the mode it took rather than leaving the reader
// to infer it from the picture.
//
// SELECTION IS LOCAL, though the panel binds the active result (chMain) as
// Table and World do and could therefore publish the row cursor. It does not:
// in folded mode a frame is a path PREFIX, so an interior frame spans many rows
// and a leaf frame is one row only when the stacks happen to be unique — a
// cursor derived from a frame would point at an arbitrary member of its
// subtree. A clicked frame publishes its label as `selection_key` instead, a
// value rather than a cursor, which is also what a follow-up query wants:
// `WHERE has(stack, {selection_key:String})`.

const (
	// The folded contract. `stack` must be list-typed; the elements are
	// stringified, so an Array(UInt64) of ids is as good a path as an
	// Array(String) of frame names.
	icicleStackCol = "stack"

	// The node contract. `parent` is the discriminator and `id` is what it
	// refers to; an empty (or NULL) parent marks a root.
	icicleIDCol     = "id"
	icicleParentCol = "parent"
	// label overrides the drawn text in node mode. Folded mode has no use for
	// it — a frame's label IS its path element.
	icicleLabelCol = "label"

	// Shared by both contracts. value is required and is the frame's OWN
	// quantity, excluding its children; unit is optional and only labels the
	// value axis.
	icicleValueCol = "value"
	icicleUnitCol  = "unit"

	// icicleMaxNodes bounds the tree. This is a cost limit, not a readability
	// one: the view culls what cannot be seen (ADR-0160 §SD7), so a deep tree
	// stays cheap to DRAW however big it is, but interning and laying it out
	// are paid per node on every rebuild. Twenty thousand is inside the
	// widget's stated envelope and above any profile this panel has been
	// pointed at.
	icicleMaxNodes = 20000
	// icicleMaxDepth caps a single path. Deeper than this and the depth axis
	// is a scrollbar with a picture attached; pprof truncates its own stacks
	// well before here, so reaching it means a path column that is not really
	// a hierarchy.
	icicleMaxDepth = 256
)

// icicleIDSalt namespaces the panel's ui-rect probe — distinct from the other
// panels' salts; the per-instance idSeed (nextVizSeed) keeps two live PlayApps
// apart.
const icicleIDSalt uint64 = 0x5a11c0de17f10006

// icicleModeE is which contract a schema satisfied.
type icicleModeE uint8

const (
	icicleModeNone icicleModeE = iota
	icicleModeFolded
	icicleModeNodes
)

func (m icicleModeE) String() string {
	switch m {
	case icicleModeFolded:
		return "folded"
	case icicleModeNodes:
		return "nodes"
	}
	return "none"
}

// iciclePruneE is the layout-time pruning control, mapped onto
// icicle.Options.MinFraction. Pruning is resolution-independent and
// reproducible, deliberately distinct from the view's sub-pixel culling
// (ADR-0160 §SD7): culling only skips what is currently invisible, where a
// pruned subtree is gone from the layout and counted in the Report, so the
// status line can say how much of the total is missing.
type iciclePruneE uint8

const (
	iciclePruneOff iciclePruneE = iota
	iciclePruneTenth
	iciclePrunePercent
)

func (p iciclePruneE) fraction() float64 {
	switch p {
	case iciclePruneTenth:
		return 0.001
	case iciclePrunePercent:
		return 0.01
	}
	return 0
}

// icicleClaim is the resolved contract a schema yielded: the mode plus the
// column indices Render consumes. -1 marks an absent optional column.
type icicleClaim struct {
	mode                       icicleModeE
	stackCol                   int
	idCol, parentCol, labelCol int
	valueCol, unitCol          int
}

// icicleStats is what one build noticed: the tree it produced and everything it
// had to drop, demote or truncate to get there. Reported in the status line — a
// picture scaled against a total must say when rows did not reach it.
type icicleStats struct {
	mode icicleModeE
	// nodes is how many tree nodes the build produced, BEFORE layout-time
	// pruning; the Report carries what survived it.
	nodes int
	// droppedValue counts rows whose value was missing, unreadable, negative
	// or not finite. droppedPath counts rows carrying no usable path (folded)
	// or no id (nodes).
	droppedValue int
	droppedPath  int
	// droppedDup counts node-mode rows repeating an id already taken; the
	// first row wins and the rest are a real loss, unlike two folded rows
	// sharing a path, whose values simply sum.
	droppedDup int
	// reparented counts node-mode rows whose `parent` names no row in the
	// result, or names themselves. They are laid out as ROOTS rather than
	// dropped: a forest is a shape the widget draws (§SD1), so a subtree whose
	// own root a WHERE clause filtered away still shows the value it carries.
	reparented int
	// truncated counts folded paths cut at icicleMaxDepth.
	truncated int
	// capped reports the node cap. Past it a folded path's value is attributed
	// to the deepest ancestor already interned rather than dropped, so the
	// picture understates DEPTH instead of quantity; a node-mode result stops
	// reading rows, which does drop value, and says so.
	capped bool
	// droppedCapped counts the one case the cap DOES cost quantity: a folded
	// row whose very first frame could not be interned, so there is no
	// ancestor to attribute it to. Counted apart from droppedPath, which is
	// about the row rather than about the cap.
	droppedCapped int
	// unit is the first non-empty `unit` cell, labelling the value axis.
	unit string
}

// icicleNodeKey identifies a trie node by its parent and its own label — the
// interning key that turns one row per path into one node per distinct prefix.
type icicleNodeKey struct {
	parent int32
	label  string
}

// icicleIsPathColumn reports whether a column can carry a path: any list of
// anything, since the elements go through formatArrayElem. The three list
// families are the ones play's formatter already cases.
func icicleIsPathColumn(dt arrow.DataType) bool {
	switch dt.(type) {
	case *arrow.ListType, *arrow.LargeListType, *arrow.FixedSizeListType:
		return true
	}
	return false
}

// icicleStackAt appends the non-null elements of the list cell at row to dst.
// EMPTY elements are skipped: splitByChar('/', '/usr/bin') yields an empty
// leading element, and an unnamed frame is not a frame. Skipping conserves the
// total — the value still lands on the deepest element that survived — where
// drawing an unlabelled rectangle would read as a rendering fault.
//
// ok is false for a null cell or a column that is not list-typed.
func icicleStackAt(arr arrow.Array, row int, dst []string) (out []string, ok bool) {
	out = dst
	if arr == nil || row < 0 || row >= arr.Len() || arr.IsNull(row) {
		return out, false
	}
	var inner arrow.Array
	var beg, end int64
	switch a := arr.(type) {
	case *array.List:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.LargeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.FixedSizeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	default:
		return out, false
	}
	for i := beg; i < end; i++ {
		if inner.IsNull(int(i)) {
			continue
		}
		if s := formatArrayElem(inner, i); s != "" {
			out = append(out, s)
		}
	}
	return out, true
}

// resolveIcicleColumns applies both contracts to a schema and reports which one
// it satisfied. Pure and schema-only, so it can run every frame.
//
// The precedence is deliberate. A list-typed `stack` takes folded mode; failing
// that, `id`+`parent` take node mode — which is also the fallback for a column
// NAMED `stack` that is not a list, so a result that carries both a scalar
// `stack` label and a real parent column still draws. Only when neither shape
// is available does a mistyped `stack` become the message, because "wrap it in
// an array" is a more useful thing to say than "add a stack column" to a query
// that visibly has one.
func resolveIcicleColumns(schema *arrow.Schema) (cl icicleClaim, reason string) {
	cl = icicleClaim{stackCol: -1, idCol: -1, parentCol: -1, labelCol: -1, valueCol: -1, unitCol: -1}
	stackIsPath := false
	for ci, f := range schema.Fields() {
		switch f.Name {
		case icicleStackCol:
			cl.stackCol = ci
			stackIsPath = icicleIsPathColumn(f.Type)
		case icicleIDCol:
			cl.idCol = ci
		case icicleParentCol:
			cl.parentCol = ci
		case icicleLabelCol:
			cl.labelCol = ci
		case icicleValueCol:
			cl.valueCol = ci
		case icicleUnitCol:
			cl.unitCol = ci
		}
	}
	switch {
	case cl.stackCol >= 0 && stackIsPath:
		cl.mode = icicleModeFolded
	case cl.idCol >= 0 && cl.parentCol >= 0:
		cl.mode = icicleModeNodes
	case cl.stackCol >= 0:
		reason = "The flame view's `stack` column must be an array of frames, outermost first — " +
			"wrap it, e.g. `splitByChar('/', path) AS stack`."
		return
	default:
		reason = "Run a query with a hierarchy to see a flame view: either a `stack` array plus a `value` " +
			"— e.g. WITH s AS (SELECT splitByChar('/', path) AS stack, sum(bytes) AS value FROM t GROUP BY 1) " +
			"SELECT * FROM s — or one row per node carrying `id`, `parent` and `value`."
		return
	}
	if cl.valueCol < 0 {
		cl.mode = icicleModeNone
		reason = "The flame view needs a `value` column carrying each frame's own quantity — " +
			"add one, e.g. `sum(bytes) AS value`."
	}
	return
}

// buildIcicleTree maps the result to the widget's columnar Tree, per the mode
// the claim resolved.
func buildIcicleTree(rec arrow.RecordBatch, cl icicleClaim) (t icicle.Tree, st icicleStats) {
	switch cl.mode {
	case icicleModeFolded:
		t, st = buildIcicleFolded(rec, cl)
	case icicleModeNodes:
		t, st = buildIcicleNodes(rec, cl)
	default:
		return
	}
	st.unit = icicleUnitOf(rec, cl)
	return
}

// icicleUnitOf reads the first non-empty `unit` cell. One unit labels the whole
// axis, so a result disagreeing with itself row to row is read by its first
// answer rather than rejected — the column is a label, not a quantity.
func icicleUnitOf(rec arrow.RecordBatch, cl icicleClaim) string {
	if rec == nil || cl.unitCol < 0 {
		return ""
	}
	for row := range rec.NumRows() {
		if u := formatCell(rec, cl.unitCol, row); u != "" {
			return truncateRunes(u, 24)
		}
	}
	return ""
}

// buildIcicleFolded interns one row per root-to-leaf path into a trie. Two rows
// carrying the same path SUM, which is the defined reading rather than an
// anomaly: they are two measurements of one frame.
//
// Deterministic given the record — the interning walks rows in order — so the
// layout key is stable frame to frame.
func buildIcicleFolded(rec arrow.RecordBatch, cl icicleClaim) (t icicle.Tree, st icicleStats) {
	st.mode = icicleModeFolded
	if rec == nil {
		return
	}
	stackArr := rec.Column(cl.stackCol)
	intern := make(map[icicleNodeKey]int32, 1024)
	var path []string
	for row := range rec.NumRows() {
		var ok bool
		path, ok = icicleStackAt(stackArr, int(row), path[:0])
		if !ok || len(path) == 0 {
			st.droppedPath++
			continue
		}
		v, valOK := quantityCellValue(rec, cl.valueCol, row)
		if !valOK || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			st.droppedValue++
			continue
		}
		if len(path) > icicleMaxDepth {
			path = path[:icicleMaxDepth]
			st.truncated++
		}
		cur := int32(-1)
		for _, lbl := range path {
			key := icicleNodeKey{parent: cur, label: lbl}
			at, seen := intern[key]
			if !seen {
				if len(t.Labels) >= icicleMaxNodes {
					st.capped = true
					break
				}
				at = int32(len(t.Labels))
				t.Labels = append(t.Labels, lbl)
				t.Parents = append(t.Parents, cur)
				t.Self = append(t.Self, 0)
				intern[key] = at
			}
			cur = at
		}
		if cur < 0 {
			// The cap was reached before even this path's root could be
			// interned, so there is nothing to attribute the value to. This is
			// the only way a folded row loses its value to the cap.
			st.droppedCapped++
			continue
		}
		t.Self[cur] += v
	}
	st.nodes = t.Len()
	return
}

// buildIcicleNodes maps one row per node. Two passes, because no ordering is
// required of the result: demanding parents before children would reject the
// shape a recursive CTE most naturally emits, and the widget imposes no such
// order either (§SD1).
func buildIcicleNodes(rec arrow.RecordBatch, cl icicleClaim) (t icicle.Tree, st icicleStats) {
	st.mode = icicleModeNodes
	if rec == nil {
		return
	}
	index := make(map[string]int32, 256)
	parentOf := make([]string, 0, 256)
	for row := range rec.NumRows() {
		if len(t.Labels) >= icicleMaxNodes {
			st.capped = true
			break
		}
		id := formatCell(rec, cl.idCol, row)
		if id == "" {
			st.droppedPath++
			continue
		}
		if _, dup := index[id]; dup {
			st.droppedDup++
			continue
		}
		v, ok := quantityCellValue(rec, cl.valueCol, row)
		if !ok || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			st.droppedValue++
			continue
		}
		label := id
		if cl.labelCol >= 0 {
			if l := formatCell(rec, cl.labelCol, row); l != "" {
				label = l
			}
		}
		index[id] = int32(len(t.Labels))
		t.Labels = append(t.Labels, label)
		t.Parents = append(t.Parents, -1)
		t.Self = append(t.Self, v)
		parentOf = append(parentOf, formatCell(rec, cl.parentCol, row))
	}
	for i, p := range parentOf {
		if p == "" {
			continue // a root, which is what an empty or NULL parent means
		}
		at, ok := index[p]
		if !ok || at == int32(i) {
			// An unknown parent, or a row naming itself — which Validate would
			// reject the whole tree over. Demote to a root and count it.
			st.reparented++
			continue
		}
		t.Parents[i] = at
	}
	st.nodes = t.Len()
	return
}

// icicleTreeOpts is the layout half of the driver's controls.
func icicleTreeOpts(orient icicle.OrientationE, order icicle.OrderE, prune iciclePruneE, unit string) icicle.Options {
	return icicle.Options{
		Orientation: orient,
		Order:       order,
		MinFraction: prune.fraction(),
		Unit:        unit,
	}
}

// icicleLayoutKey is the layout cache key: the tree generation plus every
// control that changes GEOMETRY. Colour and label switches are draw-time only
// and deliberately absent, so recolouring never re-lays-out.
//
// A generation counter rather than a fingerprint of the tree: hashing twenty
// thousand nodes every frame to discover they did not change is the cost the
// cache exists to avoid.
type icicleLayoutKey struct {
	gen    uint64
	orient icicle.OrientationE
	order  icicle.OrderE
	prune  iciclePruneE
}

// IcicleDriver owns the Icicle tab state: the built tree and its cache, the
// laid-out geometry and its cache, the draw options, and the locally-pinned
// selection.
type IcicleDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	// renderer holds the frame and label batch buffers across frames — one per
	// pane, per ADR-0160; the free functions allocate a throwaway per call.
	renderer icicleview.Renderer

	orient     icicle.OrientationE
	order      icicle.OrderE
	colorBy    icicleview.ColorModeE
	prune      iciclePruneE
	hideLabels bool

	// Tree cache: the result identity (the executed timestamp, the same
	// freshness token the pager, World and Kanban use) plus the schema the
	// claim was resolved from. Interning a big result is per-node work and has
	// no business running every frame.
	tree        icicle.Tree
	forExecuted time.Time
	forSchema   *arrow.Schema
	treeGen     uint64

	layout    *icicle.Layout
	layoutKey icicleLayoutKey
	layoutErr error
	// resetView is raised for exactly one frame whenever the layout is
	// recomputed. implot retains a plot's ranges per plot id and applies the
	// initial limits CondOnce, so a new tree — or the same tree pruned, or
	// flipped — would otherwise be viewed through the previous one's value
	// window (ADR-0160 §SD3).
	resetView bool

	// selected is the click-pinned frame; hover carries last frame's pointer
	// hit, since the readout line is drawn above the plot that produces it.
	// Both are LOCAL — see the file comment on why no cursor is published.
	selected icicleview.Hit
	hover    icicleview.Hit

	stats icicleStats

	// pendingExecuted is stashed by renderIcicleTab before dispatch — the
	// PanelI Render signature carries no result metadata (the World pane's
	// noteExecuted handoff).
	pendingExecuted time.Time
}

// NewIcicleDriver builds the driver. It takes no client: unlike Sankey and
// Network the panel reads the ACTIVE RESULT, so it has no lane of its own.
func NewIcicleDriver(ids *c.WidgetIdStack) (inst *IcicleDriver) {
	return &IcicleDriver{ids: ids, idSeed: nextVizSeed()}
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the tree cache keys on it.
func (inst *IcicleDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// iciclePanel is the PanelI face. Acceptance is schema-only and cheap — it runs
// every frame — because both contracts are questions about column names and
// one column's type, which the schema answers on its own.
type iciclePanel struct {
	driver *IcicleDriver
}

func (inst iciclePanel) ID() PanelID { return "icicle" }

func (inst iciclePanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "frames"}}
}

func (inst iciclePanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query with a `stack` array and a `value` (or `id`/`parent`/`value`) to see a flame view."
		return
	}
	cl, r := resolveIcicleColumns(schema)
	if r != "" {
		reason = r
		return
	}
	claim = cl
	return
}

func (inst iciclePanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main, ok := filled[chMain]
	if !ok {
		return
	}
	cl, isClaim := main.Claim.(icicleClaim)
	if !isClaim {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), cl, emit)
}

// render builds the tree (cached on the result identity), lays it out (cached
// on the tree generation and the geometry controls), draws it, and tracks the
// pinned frame.
func (inst *IcicleDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, cl icicleClaim, emit SignalEmitterI) {
	if schema != inst.forSchema || !inst.pendingExecuted.Equal(inst.forExecuted) || inst.treeGen == 0 {
		inst.tree, inst.stats = buildIcicleTree(rec, cl)
		inst.forSchema, inst.forExecuted = schema, inst.pendingExecuted
		inst.treeGen++
		// A new tree invalidates a pin taken against the old one: the index
		// would still resolve, and to a different frame.
		inst.selected, inst.hover = icicleview.Hit{}, icicleview.Hit{}
	}
	inst.renderControls()

	if inst.tree.Len() == 0 {
		// Drop the cached layout with the tree that produced it. Kept, it would
		// go on reporting the previous result's total beside a pane that is now
		// empty — the one lie a status line must not tell.
		inst.layout, inst.layoutKey, inst.layoutErr = nil, icicleLayoutKey{}, nil
		c.Label(inst.statusLine()).Send()
		for rt := range c.RichTextLabel("No frames: every row was missing a path and an id, or carried a value " +
			"that is not a finite, non-negative number.") {
			rt.Small().Weak()
		}
		return
	}

	if key := (icicleLayoutKey{gen: inst.treeGen, orient: inst.orient, order: inst.order, prune: inst.prune}); key != inst.layoutKey {
		inst.layoutKey = key
		inst.layout, inst.layoutErr = icicle.Compute(inst.tree,
			icicleTreeOpts(inst.orient, inst.order, inst.prune, inst.stats.unit))
		// One frame of CondAlways limits, on the frame the geometry changed.
		inst.resetView = true
	}
	if inst.layout == nil {
		c.Label(inst.statusLine()).Send()
		msg := "the rows cannot be laid out as a hierarchy"
		if inst.layoutErr != nil {
			msg += ": " + icicleReason(inst.layoutErr)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	// The readouts go ABOVE the plot. Both are one frame behind — a register
	// read always is — so nothing is lost by drawing them before the frame's own
	// Show, and it puts the total, the warnings and the hover readout where a
	// pane too short for the plot cannot push them out of sight (the note
	// ADR-0159's play Update left for the next consumer).
	c.Label(inst.pointerLine(inst.hover)).Send()

	sm := c.CurrentApplicationState.StateManager
	c.Separator().Horizontal().Send()
	probeSeq := icicleIDSalt ^ inst.idSeed ^ 0x1
	c.CaptureUiRect(probeSeq)
	paneW := float32(760)
	if r, ok := sm.GetUiRect(probeSeq); ok && r.MaxX > r.MinX {
		paneW = r.MaxX - r.MinX
	}
	w := min(max(paneW-12, 360), 1600)
	// Height follows a fixed aspect, clamped. The pane's own height used to be
	// unreadable here — the one register carrying it, CaptureAvailableSize, is
	// a single slot the frame's last capture wins, so a second writer corrupts
	// both. captureUiAvailableRect retired that: it reports the free rect into
	// the same seq-keyed r21 slot this probe already uses.
	//
	// The aspect is measured from a tour capture, and lands within a pixel of
	// the Sankey's for the same reason: it is the same dock leaf minus the same
	// control row, status line and hint. 0.42 was tried first and put the plot
	// about a hundred pixels past the leaf — which for THIS form is worse than
	// for the Sankey's, since the depth axis holds rows at RowPx and scrolls, so
	// the overflow is a whole row of frames below the fold rather than a
	// slightly cropped picture of everything.
	h := min(max(w*0.31, 260), 560)

	reset := inst.resetView
	inst.resetView = false
	// Opts.Hover is deliberately not set: Show overwrites it with what its own
	// Probe returned before drawing, so anything passed here is dead. The
	// driver's copy exists for the readout line above, which is drawn before
	// this call and so reads last frame's.
	hover, click, clicked := inst.renderer.Show(inst.ids, "frames##playicicle", w, h, inst.layout, icicleview.Opts{
		Color:      inst.colorBy,
		Selected:   inst.selected,
		HideLabels: inst.hideLabels,
		XLabel:     inst.stats.unit,
		ResetView:  reset,
	})
	inst.hover = hover
	// Any click updates the pin, including one that landed on no frame — that is
	// what clears it. Clicking the pinned frame again also clears it, so the
	// gesture is its own undo. The published key follows the pin, and an empty
	// one is the honest "nothing focused" value a query reading
	// `{selection_key:String}` sees before anything is clicked.
	if clicked {
		if click == inst.selected {
			inst.selected = icicleview.Hit{}
		} else {
			inst.selected = click
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selectedLabel())
		}
	}

	for rt := range c.RichTextLabel("hover a frame, click to zoom the value axis to it and pin it; " +
		"double-click fits the whole tree, drag scrolls the depth — over the plot the wheel is the plot's, " +
		"elsewhere it scrolls the pane") {
		rt.Small().Weak()
	}
}

// selectedLabel is the pinned frame's label, or "" when nothing is pinned.
func (inst *IcicleDriver) selectedLabel() string {
	n := inst.nodeAt(inst.selected)
	if n == nil {
		return ""
	}
	return n.Label
}

// nodeAt resolves a hit against the current layout, or nil.
func (inst *IcicleDriver) nodeAt(h icicleview.Hit) *icicle.Node {
	if inst.layout == nil || h.None() {
		return nil
	}
	i := int(h.Node)
	if i < 0 || i >= len(inst.layout.Nodes) {
		return nil
	}
	return &inst.layout.Nodes[i]
}

// renderControls draws the orientation, order, colour and prune switches.
// Changing one of the first three re-keys the layout cache, so the next frame
// re-lays-out; the colour and label switches are draw-time only and do not.
//
// The groups are separated by AddSpace, never by c.Separator(): a separator in a
// horizontal row is a VERTICAL rule sized to the row's available height, which
// in a dock leaf is the whole pane — it balloons, makes the control row that
// tall, and shoves the plot off the bottom. The Table pane's options bar hit it
// first and the Sankey's Update recorded it for the next consumer.
//
// The bars are StyleSelectable, not the segmented default: unselected options
// stay bare text and only the selected one takes a highlight, which is the
// densest readable form and what four bars plus a checkbox need to fit one row.
//
// They are emphatically NOT .Frameless(), which is the trap that used to render
// this row: StyleSegmented shows the selection by FILLING the selected segment,
// and Frame(false) is what draws no background, so a frameless segmented bar
// draws its selected and unselected options identically. Two tour captures of
// this pane differing only in orientation came back with pixel-identical
// control rows, which is how it was found — and how the Flow, Network and
// Sankey panels were then found to have it too.
func (inst *IcicleDriver) renderControls() {
	gap := styletokens.GapSections(styletokens.DensityFromEnv())
	for range c.HorizontalTop().KeepIter() {
		selector.Segmented(inst.ids, "icicle-orient", &inst.orient).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicle.OrientIcicle, "icicle").
			Option(icicle.OrientFlame, "flame").
			SendResp()
		c.AddSpace(gap)
		c.Label("order").Send()
		selector.Segmented(inst.ids, "icicle-order", &inst.order).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicle.OrderValueDesc, "value").
			Option(icicle.OrderLabel, "name").
			Option(icicle.OrderInput, "input").
			SendResp()
		c.AddSpace(gap)
		c.Label("colour").Send()
		selector.Segmented(inst.ids, "icicle-color", &inst.colorBy).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicleview.ColorByLabel, "label").
			Option(icicleview.ColorByDepth, "depth").
			SendResp()
		c.AddSpace(gap)
		c.Label("prune").Send()
		selector.Segmented(inst.ids, "icicle-prune", &inst.prune).
			Inline().
			Style(selector.StyleSelectable).
			Option(iciclePruneOff, "off").
			Option(iciclePruneTenth, "0.1%").
			Option(iciclePrunePercent, "1%").
			SendResp()
		c.AddSpace(gap)
		c.Checkbox(inst.ids.PrepareStr("icicle-hidelabels"), inst.hideLabels, "hide labels").
			SendRespVal(&inst.hideLabels)
	}
}

// pointerLine describes what the pointer is over, falling back to the pinned
// frame and then to the tree's own summary. A frame is read by its share of the
// total and by how much of it is its own — self versus total is the question a
// stack profile is asked, and the one a treemap cannot answer.
func (inst *IcicleDriver) pointerLine(hover icicleview.Hit) string {
	lay := inst.layout
	if lay == nil {
		return inst.statusLine()
	}
	describe := func(h icicleview.Hit) string {
		n := inst.nodeAt(h)
		if n == nil {
			return ""
		}
		var b strings.Builder
		b.WriteString(icicleFramePath(lay, int(h.Node)))
		fmt.Fprintf(&b, " — %s", icicleQty(n.Total, inst.stats.unit))
		if t := lay.Report.Total; t > 0 {
			fmt.Fprintf(&b, " (%.1f%%)", 100*n.Total/t)
		}
		fmt.Fprintf(&b, " · self %s · depth %d", icicleQty(n.Self, inst.stats.unit), n.Depth)
		return b.String()
	}
	if s := describe(hover); s != "" {
		return s
	}
	if s := describe(inst.selected); s != "" {
		return "pinned: " + s
	}
	return inst.statusLine()
}

// icicleFramePath renders a frame's ancestry (PathTo is root-first) as
// `a › b › leaf`, dropping ancestors from the LEFT until it fits: the frame
// under the pointer is the leaf, and its nearest ancestors say more about it
// than the root does. The leaf itself is always kept, however long it is —
// truncated, but never dropped.
func icicleFramePath(lay *icicle.Layout, node int) string {
	path := lay.PathTo(node)
	parts := make([]string, 0, len(path))
	for _, i := range path {
		if int(i) < len(lay.Nodes) {
			parts = append(parts, truncateRunes(lay.Nodes[i].Label, 48))
		}
	}
	for from := 0; from < len(parts); from++ {
		s := strings.Join(parts[from:], " › ")
		if from > 0 {
			s = "… › " + s
		}
		if len([]rune(s)) <= icicleFramePathRunes || from == len(parts)-1 {
			return s
		}
	}
	return ""
}

// icicleFramePathRunes bounds the breadcrumb. Wide enough for a handful of Go
// frames, short enough to leave the quantities on the same line visible in a
// half-width pane.
const icicleFramePathRunes = 96

// statusLine reports the tree's shape and everything the build or the layout
// noticed but could not decide.
func (inst *IcicleDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d frames", inst.stats.nodes)
	if lay := inst.layout; lay != nil {
		fmt.Fprintf(&b, " · %d deep · %s total", lay.Report.Rows, icicleQty(lay.Report.Total, inst.stats.unit))
		if lay.Report.Pruned > 0 {
			fmt.Fprintf(&b, " · %d frame(s) pruned (%s)", lay.Report.Pruned,
				icicleQty(lay.Report.PrunedValue, inst.stats.unit))
		}
	}
	fmt.Fprintf(&b, " · %s input", inst.stats.mode)
	if inst.stats.droppedPath > 0 {
		fmt.Fprintf(&b, " · %d row(s) without a path", inst.stats.droppedPath)
	}
	if inst.stats.droppedValue > 0 {
		fmt.Fprintf(&b, " · %d row(s) without a finite, non-negative value", inst.stats.droppedValue)
	}
	if inst.stats.droppedDup > 0 {
		fmt.Fprintf(&b, " · %d duplicate id(s) dropped", inst.stats.droppedDup)
	}
	if inst.stats.reparented > 0 {
		fmt.Fprintf(&b, " · %d row(s) with an unknown parent, drawn as roots", inst.stats.reparented)
	}
	if inst.stats.truncated > 0 {
		fmt.Fprintf(&b, " · %d path(s) cut at depth %d", inst.stats.truncated, icicleMaxDepth)
	}
	if inst.stats.capped {
		fmt.Fprintf(&b, " · capped at %d frames (prune, or aggregate the tail)", icicleMaxNodes)
		if inst.stats.droppedCapped > 0 {
			fmt.Fprintf(&b, ", %d row(s) past it", inst.stats.droppedCapped)
		}
	}
	return b.String()
}

// icicleReason trims the widget's package prefix off an error for the status
// line: the pane already says which panel this is.
func icicleReason(err error) string {
	if err == nil {
		return ""
	}
	return truncateRunes(strings.TrimPrefix(firstLineOf(err.Error()), "icicle: "), 140)
}

// icicleQty formats a quantity for the status and pointer lines, suffixing the
// unit when the result declared one. The job is only to keep a big total from
// crowding the line out.
func icicleQty(v float64, unit string) string {
	var s string
	switch av := math.Abs(v); {
	case av >= 1e9:
		s = strconv.FormatFloat(v/1e9, 'f', 1, 64) + "G"
	case av >= 1e6:
		s = strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
	case av >= 1e4:
		s = strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	default:
		s = strconv.FormatFloat(v, 'g', 4, 64)
	}
	if unit != "" {
		s += " " + unit
	}
	return s
}

// renderIcicleTab is the Icicle dock tab body (ADR-0160): the active result as
// an icicle plot or a flamegraph. A plain PanelI observer with the same guards
// as the World and Kanban tabs, plus the executed timestamp handed to the driver
// as its tree-cache key.
func (inst *PlayApp) renderIcicleTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query with a `stack` array and a `value` column — or one row per " +
			"node with `id`, `parent` and `value` — to see a flame view.") {
			rt.Small().Weak()
		}
		return
	}
	inst.icicleDriver.noteExecuted(executed)
	reject := dispatchPanel(iciclePanel{driver: inst.icicleDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("icicle"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
