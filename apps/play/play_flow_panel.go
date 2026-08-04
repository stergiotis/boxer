package play

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_flow_panel.go is the ADR-0153 Flow dock tab: a dataflow graph of the
// ACTIVE node's SQL, drawn with the layeredgraph widget. The graph comes from
// the selected LENS — the static clause-level derivation (play_flow_model.go,
// the default), or one of the ClickHouse EXPLAIN lenses (play_flow_lens.go),
// each on its own lane. A tool pane like Docs: no PanelI, no signal writes.
// Selection is local (the ADR-0129 lesson); on the statement lens it also
// tints the clicked clause's bytes in the editor (flowSelectionSection).

// flowIDSalt namespaces the tab's canvas + sense-region + probe ids — distinct
// from vizIDSalt (System graph) and networkIDSalt so the three drawings never
// collide; per-instance idSeed (nextVizSeed) keeps two live PlayApps apart.
const flowIDSalt uint64 = 0xf10a11ce5c0ffee1

// flowDriver owns the Flow tab state: per-lens derivation (the statement
// lens memoised on the node's identity/SQL/deps, the EXPLAIN lenses parsed
// once per served lane result), the cached layout (recomputed only on
// topology or rank-direction change), the pan/zoom view and the local
// selection.
type flowDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	lens flowLens
	// lensView switches a remote lens between the parsed graph and the raw
	// EXPLAIN text the server returned (the graph is a reading of that text;
	// the text is the full detail, and what a bug report should carry).
	lensView flowLensView
	// srcMode picks what the tab derives from: the last Run's split (the
	// default, §SD5), or the statement under the editor caret in the CURRENT
	// buffer — re-derived as the buffer changes, with the caret also picking
	// the node inside the statement (a CTE body under the caret shows that
	// CTE's flow).
	srcMode flowSrcMode
	rankDir layeredgraph.RankDir
	view    view.ViewState

	// selectedID highlights the last-clicked node, feeds the detail line,
	// and — on the statement lens — the editor tint.
	selectedID string

	// Statement lens: the derivation memo and the node it derived from
	// (activeNode.SrcOff anchors the editor highlight).
	memoKey    string
	activeNode splitNode
	graph      flowGraph
	graphErr   error

	// Lineage lens: its own memo over the same active node (both local
	// lenses may alternate without invalidating each other).
	lineageKey  string
	lineageGraph flowGraph
	lineageErr  error
	lineageNote string

	// Caret mode: the live split of the statement under the caret, memoised
	// on its text — the same per-edit parse-cost class as the editor's own
	// statement machinery.
	liveKey   string
	liveSplit splitResult
	liveErr   error

	// EXPLAIN lenses: one lane per lens (nil map for an unwired host —
	// tests), the parse memoised on the lane's served key. lensShown tracks
	// which lens the parsed graph belongs to, so switching lenses clears the
	// display instead of showing the previous lens's graph while the new one
	// loads.
	lanes        map[flowLens]*nodeLane
	lensShown    flowLens
	lensKey      string
	lensGraph    flowGraph
	lensParseErr error

	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error
}

// newFlowDriver builds the driver. client may be nil (tests, an unwired
// host): the EXPLAIN lanes are then absent and the remote lenses show their
// empty-state.
func newFlowDriver(ids *c.WidgetIdStack, client *Client) (inst *flowDriver) {
	// Left-right by default: a clause spine reads like a pipeline, and the
	// System graph made the same choice (ADR-0153 §SD3).
	inst = &flowDriver{ids: ids, idSeed: nextVizSeed(), rankDir: layeredgraph.RankDirLeftRight}
	if client != nil {
		lane := func(label string, l flowLens) *nodeLane {
			// The lane compiles the PLAIN fused statement; the EXPLAIN wrap
			// is wire-body-only, applied by the transport to the residual
			// (ExecOptions.WrapStatement) — so routing, rewrites and params
			// are exactly the wrapped statement's own.
			opts := newExecOptions(label)
			opts.WrapStatement = explainWrap(l)
			return newNodeLane(clientExecutor{client: client, opts: opts}, memory.NewGoAllocator(), 0)
		}
		inst.lanes = map[flowLens]*nodeLane{
			lensAST:      lane("flow-ast", lensAST),
			lensPlan:     lane("flow-plan", lensPlan),
			lensPipeline: lane("flow-pipeline", lensPipeline),
			lensEstimate: lane("flow-estimate", lensEstimate),
			lensIndexes:  lane("flow-indexes", lensIndexes),
		}
	}
	return
}

// lensLane returns the current lens's lane (nil for the statement lens or an
// unwired host).
func (inst *flowDriver) lensLane() *nodeLane {
	if inst == nil || inst.lanes == nil {
		return nil
	}
	return inst.lanes[inst.lens]
}

// forgetLanes clears the EXPLAIN lane memos so the next demand re-executes —
// the Run hook, for the same reason as the Network's (a re-Run after a
// transient failure memo-hits the stored error otherwise).
func (inst *flowDriver) forgetLanes() {
	if inst == nil {
		return
	}
	for _, l := range inst.lanes {
		l.forget()
	}
}

// closeLanes closes the EXPLAIN lanes (the app Close hook).
func (inst *flowDriver) closeLanes() {
	if inst == nil {
		return
	}
	for _, l := range inst.lanes {
		l.close()
	}
}

// flowMemoKey is the statement-lens memo identity: the node id (it names the
// result), its SQL, and the sorted sibling set — deleting a sibling CTE
// re-classifies a source without changing the node's own SQL, so the deps are
// part of the key.
func flowMemoKey(node splitNode) string {
	deps := make([]string, len(node.DependsOn))
	for i, d := range node.DependsOn {
		deps[i] = string(d)
	}
	sort.Strings(deps)
	return string(node.ID) + "\x00" + node.SQL + "\x00" + strings.Join(deps, "\x01")
}

// ensure re-derives the statement-lens graph when the memo key moves; a
// selection whose node vanished is dropped.
func (inst *flowDriver) ensure(node splitNode) {
	key := flowMemoKey(node)
	if key == inst.memoKey {
		return
	}
	inst.memoKey = key
	inst.activeNode = node
	sibs := make(map[string]struct{}, len(node.DependsOn))
	for _, d := range node.DependsOn {
		sibs[string(d)] = struct{}{}
	}
	inst.graph, inst.graphErr = buildFlowGraph(node.SQL, sibs, string(node.ID))
	if inst.graphErr != nil {
		inst.graph = flowGraph{}
	}
	inst.pruneSelection(inst.graph)
}

// syncLens clears the shown lens graph — and the selection — when the
// selected lens changed: the parse memo is per served key, but a key from
// another lens's lane must not keep rendering under the new selection, and a
// node id from another lens's graph must not linger invisibly (ids do not
// carry across lenses).
func (inst *flowDriver) syncLens() {
	if inst.lensShown == inst.lens {
		return
	}
	inst.lensShown = inst.lens
	inst.lensKey = ""
	inst.lensGraph = flowGraph{}
	inst.lensParseErr = nil
	inst.selectedID = ""
}

// ensureLineage re-derives the lineage graph when the memo key moves —
// the statement-lens discipline, on its own key.
func (inst *flowDriver) ensureLineage(node splitNode) {
	key := flowMemoKey(node)
	if key == inst.lineageKey {
		return
	}
	inst.lineageKey = key
	inst.activeNode = node
	sibs := make(map[string]struct{}, len(node.DependsOn))
	for _, d := range node.DependsOn {
		sibs[string(d)] = struct{}{}
	}
	inst.lineageGraph, inst.lineageNote, inst.lineageErr = buildLineageGraph(node.SQL, sibs)
	if inst.lineageErr != nil {
		inst.lineageGraph = flowGraph{}
	}
	inst.pruneSelection(inst.lineageGraph)
}

// ensureLensGraph parses a lens lane's served lines once per served key; a
// selection whose node vanished is dropped. The memo key is lens-scoped:
// with the wrap on the transport, all three lanes compile the SAME plain
// statement, so their served keys collide across lenses by construction.
func (inst *flowDriver) ensureLensGraph(key string, lines []string) {
	if key != "" {
		key = inst.lens.String() + "\x00" + key
	}
	if key == "" || key == inst.lensKey {
		return
	}
	inst.lensKey = key
	inst.lensGraph, inst.lensParseErr = parseLensRecord(inst.lens, lines)
	if inst.lensParseErr != nil {
		inst.lensGraph = flowGraph{}
	}
	inst.pruneSelection(inst.lensGraph)
}

// pruneSelection drops a selection that does not exist in the graph now shown.
func (inst *flowDriver) pruneSelection(g flowGraph) {
	if inst.selectedID == "" {
		return
	}
	for _, n := range g.Nodes {
		if n.ID == inst.selectedID {
			return
		}
	}
	inst.selectedID = ""
}

// statementSelection returns the clicked node and the split node the current
// LOCAL lens's graph derived from — the editor-highlight source. Both local
// lenses carry node-SQL-relative ranges (a clause on the statement lens, a
// select item or identifier occurrence on the lineage lens); remote lenses
// carry none.
func (inst *flowDriver) statementSelection() (n flowNode, node splitNode, ok bool) {
	if inst == nil || inst.selectedID == "" {
		return
	}
	var g flowGraph
	switch inst.lens {
	case lensStatement:
		if inst.graphErr != nil {
			return
		}
		g = inst.graph
	case lensLineage:
		if inst.lineageErr != nil {
			return
		}
		g = inst.lineageGraph
	default:
		return
	}
	for _, fn := range g.Nodes {
		if fn.ID == inst.selectedID {
			return fn, inst.activeNode, true
		}
	}
	return
}

// flowToModel projects the IR onto the widget model: boxes for sources and
// stages, ellipses for the union and result nodes (ADR-0153 §SD3).
func flowToModel(g flowGraph) layeredgraph.GraphModel {
	nodes := make([]layeredgraph.Node, len(g.Nodes))
	for i, n := range g.Nodes {
		shape := layeredgraph.NodeShapeBox
		if n.Kind == flowUnion || n.Kind == flowResult {
			shape = layeredgraph.NodeShapeEllipse
		}
		nodes[i] = layeredgraph.Node{ID: n.ID, Label: n.Label, Shape: shape}
	}
	edges := make([]layeredgraph.Edge, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = layeredgraph.Edge{From: e.From, To: e.To, Label: e.Label}
	}
	return layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
}

// renderFlowTab is the Flow dock tab body (ADR-0153): resolve what the tab
// derives from this frame (the last Run's split, or the caret statement,
// per the source toggle), then dispatch to the selected lens.
func (inst *PlayApp) renderFlowTab() {
	d := inst.flow
	split, active, srcErr := inst.flowFeed()
	d.renderControls(active)
	d.syncLens()
	if d.lens == lensLineage {
		d.renderLineage(split, active, srcErr)
		return
	}
	if d.lens.remote() {
		lines, feed := inst.demandFlowLens(split, active)
		d.renderLens(lines, feed)
		return
	}
	d.renderStatement(split, active, srcErr)
}

// flowFeed resolves the split and active node the Flow tab derives from.
//
// Run mode: the last Run's split; the active node follows the observe
// gesture, and a plain mainNodeID that misses a disambiguated sink id
// ("main (sink)") resolves to the split's sink before giving up.
//
// Caret mode: the statement under the editor caret in the CURRENT buffer,
// split live (memoised on its text); within it, the caret picks the node —
// a CTE body under the caret shows that CTE, anywhere else the sink. The
// resolution rides splitNode.SrcOff, the same verified anchor the editor
// highlight uses, so an unanchored body simply falls back to the sink.
func (inst *PlayApp) flowFeed() (split splitResult, active NodeID, srcErr error) {
	d := inst.flow
	if d.srcMode == flowSrcRun {
		active = inst.activeNodeID()
		if _, ok := findSplitNode(inst.currentSplit, active); !ok && active == mainNodeID {
			active = inst.currentSplit.Sink
		}
		return inst.currentSplit, active, inst.splitErr
	}
	stmt, _, _, ok := inst.caretStatement()
	if !ok || stmt.Src.Start < 0 || stmt.Src.End > len(inst.sql) || stmt.Src.Start >= stmt.Src.End {
		return splitResult{}, "", nil
	}
	slice := inst.sql[stmt.Src.Start:stmt.Src.End]
	text := strings.TrimSpace(slice)
	d.ensureLive(text)
	if d.liveErr != nil || len(d.liveSplit.Nodes) == 0 {
		return splitResult{}, "", d.liveErr
	}
	rel := inst.caretByte - (stmt.Src.Start + strings.Index(slice, text))
	return d.liveSplit, caretFlowNode(d.liveSplit, rel), nil
}

// flowLensFeed is what the app-side demand hands the driver about a remote
// lens's lane state this frame.
type flowLensFeed struct {
	reason  string // non-empty: why the lens cannot run (no endpoint, no node)
	loading bool
	err     error
	key     string
}

// demandFlowLens compiles `SELECT * FROM (EXPLAIN … <fused node>)` and demands
// it on the current lens's lane. The node's signal reads resolve like any
// other node's, so a `{p:Type}` slot inside the explained statement rides the
// URL — verified server-side substitution (play_flow_lens.go). Returns the
// result lines (the `explain` column's rows) plus the lane state.
func (inst *PlayApp) demandFlowLens(split splitResult, active NodeID) (lines []string, feed flowLensFeed) {
	d := inst.flow
	lane := d.lensLane()
	if lane == nil {
		feed.reason = "EXPLAIN lenses need a connected endpoint."
		return
	}
	node, ok := findSplitNode(split, active)
	if !ok {
		feed.reason = "Run a query first — the lens explains the active node's SQL."
		if d.srcMode == flowSrcCaret {
			feed.reason = "Place the caret in a statement — the lens explains the SQL under it."
		}
		return
	}
	// Caret mode asks the server only when the debounced pipeline has seen
	// exactly this buffer (the diagnostics-probe discipline) — a half-typed
	// statement keeps the last-good graph instead of streaming parse errors.
	// The lane memo already collapses repeats of a settled text.
	if d.srcMode == flowSrcCaret && inst.sql != inst.formattedFor {
		return
	}
	// The compiled SQL is the PLAIN fused node — the lane's transport applies
	// the EXPLAIN wrap to the residual (explainWrap), so the demand memo, the
	// routing decision and the rewrites all see the statement itself.
	v := lane.demand(compiledNode{
		SQL:    fuseNode(split, active),
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	feed.loading = v.loading
	feed.err = v.err
	feed.key = v.key
	if v.rec != nil {
		defer v.rec.Release()
		rows := v.rec.NumRows()
		cols := int(v.rec.NumCols())
		lines = make([]string, 0, rows)
		for row := range rows {
			// Tree-shaped EXPLAINs answer with one `explain` column; the
			// tabular ones (ESTIMATE) answer with several — tab-join those
			// so a line stays one row and the parser splits it back.
			if cols <= 1 {
				lines = append(lines, formatCell(v.rec, 0, row))
				continue
			}
			cells := make([]string, cols)
			for col := range cols {
				cells[col] = formatCell(v.rec, col, row)
			}
			lines = append(lines, strings.Join(cells, "\t"))
		}
	}
	return
}

// renderStatement is the static lens's body: the split-derived clause graph.
func (inst *flowDriver) renderStatement(split splitResult, active NodeID, splitErr error) {
	node, ok := findSplitNode(split, active)
	if !ok {
		msg := "Run a query to see its dataflow."
		if inst.srcMode == flowSrcCaret {
			msg = "Place the caret in a statement to see its dataflow."
		}
		if splitErr != nil {
			msg = "The statement did not split: " + truncateRunes(firstLine(splitErr.Error()), 120)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}
	inst.ensure(node)
	if inst.graphErr != nil {
		for rt := range c.RichTextLabel("no flow graph: " + truncateRunes(firstLine(inst.graphErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}
	inst.renderGraph(inst.graph)
}

// flowLensView selects how a remote lens shows its result.
type flowLensView uint8

const (
	flowViewGraph flowLensView = iota
	flowViewText // the raw EXPLAIN output, indentation preserved
)

// flowSrcMode selects what the tab derives from.
type flowSrcMode uint8

const (
	flowSrcRun   flowSrcMode = iota // the last Run's split (§SD5 default)
	flowSrcCaret                    // the statement under the caret, live
)

// ensureLive re-splits the caret statement when its text changes. Cost is
// bounded by the memo: an unchanged statement (caret travel, edits
// elsewhere) re-derives nothing.
func (inst *flowDriver) ensureLive(stmtText string) {
	if stmtText == inst.liveKey {
		return
	}
	inst.liveKey = stmtText
	if strings.TrimSpace(stmtText) == "" {
		inst.liveSplit, inst.liveErr = splitResult{}, nil
		return
	}
	inst.liveSplit, inst.liveErr = splitGraph(stmtText)
	if inst.liveErr != nil {
		inst.liveSplit = splitResult{}
	}
}

// caretFlowNode picks the node whose body contains the statement-relative
// caret offset — editing inside a CTE shows that CTE's flow — else the sink.
// Nodes without a verified source anchor never match.
func caretFlowNode(split splitResult, rel int) NodeID {
	for i := range split.Nodes {
		n := &split.Nodes[i]
		if n.ID == split.Sink || n.SrcOff < 0 {
			continue
		}
		if rel >= n.SrcOff && rel < n.SrcOff+len(n.SQL) {
			return n.ID
		}
	}
	return split.Sink
}

// renderLineage is the lineage lens's body: output-column provenance of the
// active node's SELECT list (play_flow_lineage.go), same discipline as the
// statement lens.
func (inst *flowDriver) renderLineage(split splitResult, active NodeID, splitErr error) {
	node, ok := findSplitNode(split, active)
	if !ok {
		msg := "Run a query to see its column lineage."
		if inst.srcMode == flowSrcCaret {
			msg = "Place the caret in a statement to see its column lineage."
		}
		if splitErr != nil {
			msg = "The statement did not split: " + truncateRunes(firstLine(splitErr.Error()), 120)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}
	inst.ensureLineage(node)
	if inst.lineageErr != nil {
		for rt := range c.RichTextLabel("no lineage: " + truncateRunes(firstLine(inst.lineageErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}
	if inst.lineageNote != "" {
		for rt := range c.RichTextLabel(inst.lineageNote) {
			rt.Small().Weak()
		}
	}
	inst.renderGraph(inst.lineageGraph)
}

// renderLens is a remote lens's body: the parsed EXPLAIN graph or the raw
// output text, with the lane's loading/error state. The last-parsed graph
// stays visible through a reload or a transient error — the lane holds
// last-good the same way.
func (inst *flowDriver) renderLens(lines []string, feed flowLensFeed) {
	if feed.reason != "" {
		for rt := range c.RichTextLabel(feed.reason) {
			rt.Small().Weak()
		}
		return
	}
	if explainUnsupportedByEndpoint(feed.err) {
		// Routing worked — the statement runs on this endpoint — but its SQL
		// surface has no EXPLAIN to answer with. Say that instead of
		// relaying the endpoint's parser error, and do not render a stale
		// graph from another endpoint under it.
		for rt := range c.RichTextLabel("This endpoint cannot EXPLAIN: the query itself runs here, " +
			"but its SQL surface has no EXPLAIN statement. The statement lens still works; " +
			"for plans, point at a full ClickHouse endpoint.") {
			rt.Small().Weak()
		}
		return
	}
	if feed.err != nil {
		for rt := range c.RichTextLabel("EXPLAIN failed: " + truncateRunes(firstLine(feed.err.Error()), 140)) {
			rt.Small().Weak()
		}
	}
	if inst.lensView == flowViewText {
		inst.renderLensText(lines, feed)
		return
	}
	if lines != nil {
		inst.ensureLensGraph(feed.key, lines)
	}
	if inst.lensParseErr != nil {
		for rt := range c.RichTextLabel("EXPLAIN output did not parse: " + truncateRunes(firstLine(inst.lensParseErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}
	if len(inst.lensGraph.Nodes) == 0 {
		switch {
		case feed.loading:
			for rt := range c.RichTextLabel("asking the server…") {
				rt.Small().Weak()
			}
		case inst.lensKey != "" && feed.err == nil:
			// Served and parsed, and there is genuinely nothing: an
			// ESTIMATE over no MergeTree reads is the common case.
			msg := "the server returned an empty result for this lens."
			if inst.lens == lensEstimate {
				msg = "nothing to estimate: the statement reads no MergeTree tables."
			}
			for rt := range c.RichTextLabel(msg) {
				rt.Small().Weak()
			}
		}
		return
	}
	inst.renderGraph(inst.lensGraph)
}

// renderLensText shows the raw EXPLAIN output as the server returned it —
// monospace, one label per line, indentation intact (the AST and PIPELINE
// dialects carry their structure in it). A result row may embed newlines
// (PLAN's json=1 document is one row), so rows are flattened to real lines
// first. Bounded by the lens result itself, which EXPLAIN keeps small.
func (inst *flowDriver) renderLensText(lines []string, feed flowLensFeed) {
	if len(lines) == 0 {
		if feed.loading {
			for rt := range c.RichTextLabel("asking the server…") {
				rt.Small().Weak()
			}
		}
		return
	}
	flat := make([]string, 0, len(lines))
	for _, ln := range lines {
		flat = append(flat, strings.Split(ln, "\n")...)
	}
	c.Label(fmt.Sprintf("%d lines · raw %s output", len(flat), inst.lens.String())).Send()
	c.Separator().Horizontal().Send()
	for _, ln := range flat {
		if ln == "" {
			c.AddSpace(6)
			continue
		}
		for rt := range c.RichTextLabel(ln) {
			rt.Monospace()
		}
	}
}

// renderGraph lays out (cached) and draws one graph, tracks the local
// selection and shows the detail line — shared by every lens.
func (inst *flowDriver) renderGraph(g flowGraph) {
	model := flowToModel(g)
	c.Label(inst.statusLine(g)).Send()
	if len(model.Nodes) == 0 {
		return
	}

	// Layout cached on the topology fingerprint (+ rank direction) — a click
	// changes only the highlight, never the layout (the play_graph_viz.go /
	// Network idiom; networkModelKey reused, same package).
	key := networkModelKey(model, inst.rankDir)
	if key != inst.layoutKey || (inst.layout == nil && inst.layoutErr == nil) {
		inst.layoutKey = key
		inst.layout = nil
		eng, err := goccyengine.Shared()
		if err == nil {
			inst.layout, err = eng.Layout(context.Background(), model,
				layeredgraph.LayoutOpts{RankDir: inst.rankDir, FontSize: 13})
		}
		inst.layoutErr = err
	}
	if inst.layout == nil {
		msg := "flow layout unavailable (layout engine)"
		if inst.layoutErr != nil {
			msg += ": " + truncateRunes(firstLine(inst.layoutErr.Error()), 80)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	lw, lh := inst.layout.Width, inst.layout.Height
	if lw <= 0 || lh <= 0 {
		return
	}
	// Fill the pane width via the separator + Seq-keyed UiRect probe (the
	// passes/Network idiom — per-seq R21 slot, never CaptureAvailableSize:
	// one register, won by the frame's last capture, so its readers size each
	// other). One-frame lag; the first frame falls back to a conservative
	// width. captureUiAvailableRect is the newer form: same slot, no
	// separator, and it reports the pane height as well.
	sm := c.CurrentApplicationState.StateManager
	c.Separator().Horizontal().Send()
	probeSeq := flowIDSalt ^ inst.idSeed ^ 0x1
	c.CaptureUiRect(probeSeq)
	paneW := float32(760)
	if r, ok := sm.GetUiRect(probeSeq); ok && r.MaxX > r.MinX {
		paneW = r.MaxX - r.MinX
	}
	w := min(max(paneW-12, 360), 1600)
	h := min(max(w*float32(lh/lw), 200), 720)

	res := view.Render(flowIDSalt+inst.idSeed, inst.layout, view.RenderOpts{
		Style:    view.DefaultStyle(),
		CanvasW:  w,
		CanvasH:  h,
		NodeFill: func(id string) (color.Color, bool) { return inst.nodeFill(g, id) },
		State:    &inst.view,
	})
	// A click toggles the LOCAL highlight (no shared-signal emit — ADR-0153
	// §SD4); clicking the highlighted node again clears it.
	if res.Clicked != "" {
		if inst.selectedID == res.Clicked {
			inst.selectedID = ""
		} else {
			inst.selectedID = res.Clicked
		}
	}

	inst.renderDetailLine(g)
	hint := "drag pans, ctrl+scroll zooms; click a node for its clause text"
	if inst.lens.remote() {
		hint = "drag pans, ctrl+scroll zooms; click a node for its step text"
	}
	for rt := range c.RichTextLabel(hint) {
		rt.Small().Weak()
	}
}

// nodeFill paints the selected node with the accent, sources with a neutral
// tint (the inputs read as one band) and the result like the System graph's
// sink; stages keep the default body.
func (inst *flowDriver) nodeFill(g flowGraph, id string) (col color.Color, ok bool) {
	if inst.selectedID != "" && id == inst.selectedID {
		return color.Hex(styletokens.AccentDefault.AsHex()), true
	}
	for _, n := range g.Nodes {
		if n.ID != id {
			continue
		}
		switch {
		case n.Kind.isSource():
			return color.Hex(styletokens.NeutralSubtle.AsHex()), true
		case n.Kind == flowResult:
			return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
		}
		break
	}
	return
}

// renderControls draws the lens selector on its own row, and the secondary
// toggles (source, layout, view) with the node badge on a second — one row
// clipped once seven lenses plus three toggles landed, and a control that
// renders off-pane cannot be clicked by anyone.
func (inst *flowDriver) renderControls(active NodeID) {
	for range c.Horizontal().KeepIter() {
		c.Label("lens").Send()
		// The framed segmented skin (the package default), not the frameless
		// one the secondary toggles use: the lens is the pane's primary
		// mode switch, and the frame is what says "this is a control" at a
		// glance.
		selector.Segmented(inst.ids, "flow-lens", &inst.lens).
			Inline().
			Option(lensStatement, "statement").
			Option(lensAST, "ast").
			Option(lensPlan, "plan").
			Option(lensPipeline, "pipeline").
			Option(lensEstimate, "estimate").
			Option(lensIndexes, "indexes").
			Option(lensLineage, "lineage").
			SendResp()
	}
	for range c.Horizontal().KeepIter() {
		c.Label("source").Send()
		selector.Segmented(inst.ids, "flow-src", &inst.srcMode).
			Inline().
			Style(selector.StyleSelectable).
			Option(flowSrcRun, "run").
			Option(flowSrcCaret, "caret").
			SendResp()
		c.Label("layout").Send()
		selector.Segmented(inst.ids, "flow-rank-dir", &inst.rankDir).
			Inline().
			Style(selector.StyleSelectable).
			Option(layeredgraph.RankDirLeftRight, "left-right").
			Option(layeredgraph.RankDirTopBottom, "top-down").
			SendResp()
		if inst.lens.remote() {
			c.Label("view").Send()
			selector.Segmented(inst.ids, "flow-lens-view", &inst.lensView).
				Inline().
				Style(selector.StyleSelectable).
				Option(flowViewGraph, "graph").
				Option(flowViewText, "text").
				SendResp()
		}
		badge := "node: " + string(active)
		if active == "" {
			badge = "node: —"
		}
		if inst.srcMode == flowSrcCaret {
			badge += " · live"
		}
		for rt := range c.RichTextLabel(badge) {
			rt.Small().Weak()
		}
	}
}

// renderDetailLine shows the selected node's clause/step text (ADR-0153 §SD4
// — on the statement lens the same selection also tints the editor bytes, see
// flowSelectionSection).
func (inst *flowDriver) renderDetailLine(g flowGraph) {
	if inst.selectedID == "" {
		return
	}
	for _, n := range g.Nodes {
		if n.ID != inst.selectedID {
			continue
		}
		text := n.Label
		if n.Detail != "" {
			text += " — " + n.Detail
		}
		for rt := range c.RichTextLabel(text) {
			rt.Small()
		}
		return
	}
}

func (inst *flowDriver) statusLine(g flowGraph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", len(g.Nodes), len(g.Edges))
	if g.Capped {
		fmt.Fprintf(&b, " · capped (%d-node / depth-%d bound)", flowMaxNodes, flowMaxDepth)
	}
	return b.String()
}
