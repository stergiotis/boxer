package play

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	sankeyview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_sankey_panel.go is the Sankey dock tab: a result set drawn as a
// flow-quantity diagram over the sankey widget (ADR-0159), whose §SD6 deferred
// exactly this panel so the channel plumbing could be reviewed on its own.
//
// The two inputs are convention-named CTEs of the user's query — `flows`
// (required) and `nodes` (optional) — each pulled off the split graph on its
// own lane, the mechanism the Kanban `lanes` CTE (ADR-0122 §SD6) introduced and
// the Network panel (ADR-0129 §SD1) applied twice. The final SELECT stays the
// user's own, so the Table can show something else entirely.
//
// The contract is named columns rather than detection, like Network and Kanban:
// flows carry `source`/`target`/`value` (+ optional `label`, `tone`); nodes
// carry `id` (+ optional `label`, `stage`, `order`, `group`, `tone`). Nothing
// but intent separates a source column from a target column, so the panel asks
// for the names. All ten parse as bare ClickHouse aliases — none needs
// backticking.
//
// SELECTION IS LOCAL, for the reason ADR-0129 §SD4 recorded: the inputs come
// from private lanes rather than observable split nodes, so a `selection`
// cursor emitted here is clamped away (syncSelectionClamp sends a cursor on an
// unbound node home) and would jerk Table and Detail to row 0. A clicked node
// publishes its id as `selection_key` — a value, not a cursor, which is why it
// can cross the boundary the cursor cannot.

const (
	// Flow columns (chFlows). source/target match the Network panel's edge
	// vocabulary — the same relation, differently drawn — and avoid the `from`
	// SQL keyword for the same reason (ADR-0129 §SD2).
	sankeySourceCol = "source"
	sankeyTargetCol = "target"
	// value is the conserved quantity the ribbon thickness encodes. It is
	// required: a flow diagram without a quantity is a node-link graph, and
	// that is what the Network tab is for.
	sankeyValueCol = "value"

	// Node columns (chNodes).
	sankeyIDCol = "id"
	// stage is what makes a diagram alluvial: given stages fix the columns and
	// the panel switches mode on their presence (see sankeyChoiceAuto).
	sankeyStageCol = "stage"
	// order sorts within a stage in alluvial mode; ties fall back to the
	// widget's own rule (descending value, then id).
	sankeyOrderCol = "order"
	// group colours by category from the qualitative palette — the affordance
	// that keeps one category the same hue across every stage it appears in.
	sankeyGroupCol = "group"

	// label and tone are shared by both contracts: the two live in different
	// CTEs, so one name serves both.
	sankeyLabelCol = "label"
	sankeyToneCol  = "tone"

	// sankeyFlowsNodeID / sankeyNodesNodeID are the CTEs the two channels bind
	// to. Nodes of the user's own split graph, demanded on their own lanes —
	// not panel-authored queries.
	sankeyFlowsNodeID NodeID = "flows"
	sankeyNodesNodeID NodeID = "nodes"

	// sankeyMaxNodes / sankeyMaxLinks bound the diagram. These are readability
	// limits before they are cost limits: the qualitative palette is seven hues
	// and a bar needs a few pixels to carry a label, so a diagram past this size
	// is a texture rather than a reading. The excess is dropped and counted in
	// the status line rather than silently truncated.
	sankeyMaxNodes = 300
	sankeyMaxLinks = 1500
)

// sankeyIDSalt namespaces the panel's pane probe — distinct from the other
// panels' salts; the per-instance idSeed (nextVizSeed) keeps two live PlayApps
// apart.
const sankeyIDSalt uint64 = 0x5a11c0de17f10005

// sankeyPaneFill is the diagram's box — the shared pane rule
// (play_pane_box.go). Both implot axes are hidden here, so the box has no tick
// labels to clip and the floor is a readability one: under it the bars stop
// carrying their labels and the diagram is a texture.
var sankeyPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 1600, minH: 240,
	fallbackW: 760, fallbackH: 460,
}

// sankeyChoiceE is the mode control's value. The default is Auto because the
// data answers the question on its own: supplying a `stage` for every node IS
// the request for an alluvial reading, and a `flows` CTE with no `nodes` CTE
// cannot be alluvial at all. The two explicit settings exist for the case Auto
// gets wrong — a staged diagram the user would rather see with derived stages,
// or a fixed-stage reading of data whose stages are incomplete.
type sankeyChoiceE uint8

const (
	sankeyChoiceAuto sankeyChoiceE = iota
	sankeyChoiceSankey
	sankeyChoiceAlluvial
)

// sankeyFlowsClaim / sankeyNodesClaim are the resolved column indices a
// channel's schema yields in AcceptForChannel and Render consumes. -1 marks an
// absent optional column.
type sankeyFlowsClaim struct {
	srcCol, tgtCol, valCol, labelCol, toneCol int
}

type sankeyNodesClaim struct {
	idCol, labelCol, stageCol, orderCol, groupCol, toneCol int
}

// sankeyTone maps a `tone` cell to a design-system colour, as 0xRRGGBBAA for
// the widget's Color fields. The vocabulary is the six semantic families and an
// unknown value returns ok=false, leaving the group palette or the widget's own
// qualitative cycle in charge — an unknown tone must not blank a bar.
//
// Both a node bar and a ribbon take the Default variant, where the Network
// panel gives a node body the Subtle one: a graph node is a large background
// region, but a sankey bar is a thin saturated mark and a ribbon is drawn
// translucent over its neighbours, so a subtle background tone would read as
// nothing in either role.
func sankeyTone(s string) (rgba uint32, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "accent":
		return styletokens.AccentDefault.AsHex(), true
	case "info":
		return styletokens.InfoDefault.AsHex(), true
	case "success":
		return styletokens.SuccessDefault.AsHex(), true
	case "warning":
		return styletokens.WarningDefault.AsHex(), true
	case "error":
		return styletokens.ErrorDefault.AsHex(), true
	case "neutral":
		return styletokens.NeutralDefault.AsHex(), true
	}
	return 0, false
}

// quantityCellValue reads one cell as a quantity — a sankey flow's value, an
// icicle frame's. It tries the numeric arrays first, then falls back to parsing
// the formatted cell, which is what carries ClickHouse Decimal (and a
// dictionary-encoded numeric) through, since those arrive as Arrow types the
// numeric switch does not case. A `value` column produced by `sum()` over a
// Decimal is ordinary enough that dropping every row would read as the panel
// being broken.
func quantityCellValue(rec arrow.RecordBatch, col int, row int64) (v float64, ok bool) {
	arr := rec.Column(col)
	if v, ok = numericCellValue(arr, row); ok {
		return
	}
	s := formatArrayElem(arr, row)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// sankeyStats is what one build noticed: the shape of the diagram and
// everything it had to drop or fold to get there. Reported in the status line —
// a flow diagram that quietly discards rows misstates its own total.
type sankeyStats struct {
	nodes int
	links int
	// collapsed counts duplicate (source,target) rows folded into one ribbon.
	collapsed int
	// droppedValue counts rows whose value was missing, unreadable or not
	// strictly positive; droppedSelf counts source==target rows, which a
	// stage-ordered diagram cannot draw.
	droppedValue int
	droppedSelf  int
	// droppedEndpoint counts rows dropped because an endpoint could not be
	// added — the node cap was already reached.
	droppedEndpoint int
	capped          bool
	// stagesGiven reports that every node carries an explicit stage, which is
	// what makes the alluvial reading available.
	stagesGiven bool
}

// sankeyBuild is the outcome of mapping the two result sets to a Diagram.
type sankeyBuild struct {
	diagram sankey.Diagram
	stats   sankeyStats
}

// buildSankeyDiagram maps the flows/nodes records to a Diagram: nodes are
// de-duplicated by id, a flow endpoint with no `nodes` row synthesises one (so
// a partial or absent `nodes` CTE still draws every flow), duplicate
// (source,target) pairs are SUMMED rather than drawn twice, and both inputs are
// capped. Deterministic given the records, so the layout key is stable frame to
// frame.
//
// Summing duplicates is the one place this differs from the Network panel,
// which collapses parallel edges by keeping the first. A parallel edge is one
// relation stated twice; two rows carrying a quantity are two quantities, and
// dropping the second would understate the total the whole diagram is scaled
// against.
func buildSankeyDiagram(flowsRec arrow.RecordBatch, fc sankeyFlowsClaim,
	nodesRec arrow.RecordBatch, nc sankeyNodesClaim, choice sankeyChoiceE) (b sankeyBuild) {
	nodes := make([]sankey.Node, 0, 64)
	index := make(map[string]int, 64)
	// staged tracks which nodes got an explicit stage; a diagram is alluvial
	// only when every one of them did, synthesised endpoints included.
	staged := make(map[string]bool, 64)
	groupIdx := make(map[string]int, 8)

	// addSynth adds a flow endpoint with no `nodes` row; false means the node
	// cap is reached, so the caller must drop the flow rather than leave it
	// referencing a node the diagram does not contain.
	addSynth := func(id string) bool {
		if _, ok := index[id]; ok {
			return true
		}
		if len(nodes) >= sankeyMaxNodes {
			return false
		}
		index[id] = len(nodes)
		nodes = append(nodes, sankey.Node{ID: id, Label: id})
		return true
	}

	if nodesRec != nil && nc.idCol >= 0 {
		for row := range nodesRec.NumRows() {
			if len(nodes) >= sankeyMaxNodes {
				b.stats.capped = true
				break
			}
			id := formatCell(nodesRec, nc.idCol, row)
			if id == "" {
				continue
			}
			if _, dup := index[id]; dup {
				continue
			}
			n := sankey.Node{ID: id, Label: id}
			if nc.labelCol >= 0 {
				if l := formatCell(nodesRec, nc.labelCol, row); l != "" {
					n.Label = l
				}
			}
			if nc.stageCol >= 0 {
				if s, ok := quantityCellValue(nodesRec, nc.stageCol, row); ok && s >= 0 && !math.IsInf(s, 0) {
					n.Stage = int(s)
					staged[id] = true
				}
			}
			if nc.orderCol >= 0 {
				if o, ok := quantityCellValue(nodesRec, nc.orderCol, row); ok {
					n.Order = o
				}
			}
			// An explicit tone wins over the group palette: `group` says "these
			// belong together", `tone` says "this one means something", and a
			// query that bothered to name a meaning meant it. Leaving Color at
			// zero defers to the widget's qualitative cycle (ADR-0156).
			if nc.toneCol >= 0 {
				if col, ok := sankeyTone(formatCell(nodesRec, nc.toneCol, row)); ok {
					n.Color = col
				}
			}
			if n.Color == 0 && nc.groupCol >= 0 {
				if g := formatCell(nodesRec, nc.groupCol, row); g != "" {
					idx, ok := groupIdx[g]
					if !ok {
						idx = len(groupIdx)
						groupIdx[g] = idx
					}
					n.Color = styletokens.QualitativeCycle(idx).AsHex()
				}
			}
			index[id] = len(nodes)
			nodes = append(nodes, n)
		}
	}

	links := make([]sankey.Link, 0, 64)
	linkAt := make(map[[2]string]int, 64)
	if flowsRec != nil {
		for row := range flowsRec.NumRows() {
			src := formatCell(flowsRec, fc.srcCol, row)
			tgt := formatCell(flowsRec, fc.tgtCol, row)
			if src == "" || tgt == "" {
				b.stats.droppedValue++
				continue
			}
			v, ok := quantityCellValue(flowsRec, fc.valCol, row)
			if !ok || v <= 0 || math.IsInf(v, 0) {
				// Validate would reject the diagram outright over one such row;
				// dropping it keeps the rest readable. A zero flow is also
				// nothing to draw, so nothing is lost by leaving it out.
				b.stats.droppedValue++
				continue
			}
			if src == tgt {
				// A stage-ordered diagram cannot show a flow returning to where
				// it started, and Validate rejects it by name; drop and count.
				b.stats.droppedSelf++
				continue
			}
			key := [2]string{src, tgt}
			if at, dup := linkAt[key]; dup {
				links[at].Value += v
				b.stats.collapsed++
				continue
			}
			if len(links) >= sankeyMaxLinks {
				b.stats.capped = true
				break
			}
			if !addSynth(src) || !addSynth(tgt) {
				b.stats.capped = true
				b.stats.droppedEndpoint++
				continue
			}
			l := sankey.Link{Source: src, Target: tgt, Value: v}
			if fc.labelCol >= 0 {
				l.Label = formatCell(flowsRec, fc.labelCol, row)
			}
			if fc.toneCol >= 0 {
				if col, ok := sankeyTone(formatCell(flowsRec, fc.toneCol, row)); ok {
					l.Color = col
				}
			}
			linkAt[key] = len(links)
			links = append(links, l)
		}
	}

	b.stats.nodes = len(nodes)
	b.stats.links = len(links)
	b.stats.stagesGiven = len(nodes) > 0 && len(staged) == len(nodes)

	b.diagram = sankey.Diagram{Nodes: nodes, Links: links, Mode: sankeyMode(choice, b.stats.stagesGiven)}
	return
}

// sankeyMode resolves the mode control against what the data supports. Auto
// takes the alluvial reading whenever the stages are all there — supplying them
// is the request — and falls back to the derived-stage reading otherwise. A
// forced alluvial choice is honoured even without stages, so that Compute
// reports why it cannot be drawn instead of the panel silently ignoring the
// setting.
func sankeyMode(choice sankeyChoiceE, stagesGiven bool) sankey.Mode {
	switch choice {
	case sankeyChoiceSankey:
		return sankey.ModeSankey
	case sankeyChoiceAlluvial:
		return sankey.ModeAlluvial
	}
	if stagesGiven {
		return sankey.ModeAlluvial
	}
	return sankey.ModeSankey
}

// computeSankeyLayout lays the diagram out, falling back from alluvial to
// Sankey when the stages do not support it — a link spanning two stages, or a
// stage column that covers only part of the graph. The fallback carries the
// rejection message, because "this is not alluvial data, and here is the link
// that proves it" is the only useful thing to say; silently redrawing in the
// other mode would leave the mode control lying about what is on screen.
//
// A Sankey-mode failure has nowhere to fall back to (a cycle is the usual
// cause) and is returned as the error.
func computeSankeyLayout(d sankey.Diagram) (lay *sankey.Layout, used sankey.Mode, fallback string, err error) {
	used = d.Mode
	lay, err = sankey.Compute(d, sankey.Options{})
	if err == nil || d.Mode != sankey.ModeAlluvial {
		return
	}
	alluvialErr := err
	d.Mode = sankey.ModeSankey
	lay, err = sankey.Compute(d, sankey.Options{})
	if err != nil {
		return nil, sankey.ModeAlluvial, "", alluvialErr
	}
	return lay, sankey.ModeSankey, sankeyReason(alluvialErr), nil
}

// sankeyReason trims the widget's package prefix off an error for the status
// line: the pane already says which panel this is.
func sankeyReason(err error) string {
	if err == nil {
		return ""
	}
	return truncateRunes(strings.TrimPrefix(firstLineOf(err.Error()), "sankey: "), 140)
}

// sankeyDiagramKey fingerprints the diagram — the layout cache key. Everything
// the geometry depends on is in it (mode, ids, stages, orders, endpoints,
// values); colours are not, so recolouring never re-lays-out.
//
// Written against the hasher rather than through fmt so that a diagram at the
// cap costs one scratch buffer per frame instead of a few thousand format
// calls.
func sankeyDiagramKey(d sankey.Diagram) string {
	h := fnv.New64a()
	var scratch []byte
	put := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	putNum := func(f float64) {
		scratch = strconv.AppendFloat(scratch[:0], f, 'g', -1, 64)
		_, _ = h.Write(scratch)
		_, _ = h.Write([]byte{0})
	}
	putNum(float64(d.Mode))
	for i := range d.Nodes {
		n := &d.Nodes[i]
		put(n.ID)
		put(n.Label)
		putNum(float64(n.Stage))
		putNum(n.Order)
	}
	for i := range d.Links {
		l := &d.Links[i]
		put(l.Source)
		put(l.Target)
		putNum(l.Value)
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// SankeyDriver owns the Sankey tab state: the two input lanes, the cached
// layout (recomputed only when the diagram changes, so a hover never re-lays
// out), the draw options, and the locally-pinned selection.
type SankeyDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	// flowsLane / nodesLane run the `flows` / `nodes` CTEs of the user's split
	// on their own lanes (nil for an unwired host — tests). The status mirrors
	// let a failed lane say so rather than reading as "no flows".
	flowsLane    *nodeLane
	nodesLane    *nodeLane
	flowsLoading bool
	nodesLoading bool
	flowsErr     error
	nodesErr     error

	// paneW / paneH are the last box the pane probe reported. Held across
	// frames rather than read fresh: the probe answers nothing on the first
	// frame and again on the frame a hidden tab comes back (a seq that did not
	// capture is absent from the drain), and resizing the plot to a fallback on
	// those frames would flash.
	paneW, paneH float32

	// renderer holds the ribbon-sampling scratch across frames — one per pane,
	// per ADR-0159; the free functions allocate a throwaway per call.
	renderer   sankeyview.Renderer
	fill       sankeyview.FillMode
	gradient   bool
	hideLabels bool
	choice     sankeyChoiceE

	// selected is the click-pinned node or ribbon. Local to the panel; a node
	// click additionally publishes `selection_key`. hover carries last frame's
	// pointer hit, since the readout line is drawn above the plot that produces
	// it.
	selected sankeyview.Hit
	hover    sankeyview.Hit

	layout    *sankey.Layout
	layoutKey string
	layoutErr error
	// modeUsed / modeFallback describe the CACHED layout, so they survive the
	// frames on which the build is memo-hit and nothing is recomputed.
	modeUsed     sankey.Mode
	modeFallback string

	stats sankeyStats
}

// NewSankeyDriver builds the driver. client may be nil (tests, an unwired
// host): the lanes are then absent and the panel shows its empty state.
func NewSankeyDriver(ids *c.WidgetIdStack, client *Client) (inst *SankeyDriver) {
	inst = &SankeyDriver{ids: ids, idSeed: nextVizSeed()}
	if client != nil {
		inst.flowsLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("sankey-flows")},
			memory.NewGoAllocator(), 0)
		inst.nodesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("sankey-nodes")},
			memory.NewGoAllocator(), 0)
	}
	return
}

// forgetLanes clears both lane memos so the next demand re-executes, even for
// an unchanged (SQL, params) pair — the Run hook (executeRun), matching the
// Network and Kanban lanes. Without it a re-Run after a transient failure (a
// wrong endpoint, a server that was down) memo-hits the stored error — its key
// is the SQL, and the endpoint is not part of it — so the diagram never
// recovers though the main result does (ADR-0129's endpoint-switch bug).
func (inst *SankeyDriver) forgetLanes() {
	if inst == nil {
		return
	}
	if inst.flowsLane != nil {
		inst.flowsLane.forget()
	}
	if inst.nodesLane != nil {
		inst.nodesLane.forget()
	}
}

// sankeyPanel is the PanelI face. Acceptance is schema-only and cheap — it runs
// every frame — because the contract is a question about column names, which
// the schema answers on its own.
type sankeyPanel struct {
	driver *SankeyDriver
}

func (inst sankeyPanel) ID() PanelID { return "sankey" }

// Channels declares the required flows plus the optional decorating nodes. The
// panel renders as soon as chFlows is filled; chNodes, when present, supplies
// labels, stages, orders and colours, and is what makes the alluvial reading
// available.
func (inst sankeyPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chFlows, Required: true, Label: "flows"},
		{ID: chNodes, Required: false, Label: "nodes"},
	}
}

func (inst sankeyPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	switch ch {
	case chFlows:
		if schema == nil {
			reason = "Run a query with a `flows` CTE (columns `source`, `target` and `value`) to see a flow diagram."
			return
		}
		fc, r := resolveSankeyFlows(schema)
		if r != "" {
			reason = r
			return
		}
		claim = fc
		return
	case chNodes:
		if schema == nil {
			reason = "no nodes result" // optional channel: the dispatcher swallows the reason
			return
		}
		nc, r := resolveSankeyNodes(schema)
		if r != "" {
			reason = r
			return
		}
		claim = nc
		return
	}
	reason = "unknown channel"
	return
}

func (inst sankeyPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	flows, ok := filled[chFlows]
	if !ok {
		return
	}
	fc, ok := flows.Claim.(sankeyFlowsClaim)
	if !ok {
		return
	}
	nc := sankeyNodesClaim{idCol: -1, labelCol: -1, stageCol: -1, orderCol: -1, groupCol: -1, toneCol: -1}
	var nodesRec arrow.RecordBatch
	if n, has := filled[chNodes]; has {
		if got, isC := n.Claim.(sankeyNodesClaim); isC {
			nc = got
			nodesRec = n.Rec
		}
	}
	inst.driver.render(flows.Rec, fc, nodesRec, nc, emit)
}

// resolveSankeyFlows applies the flow contract to a schema. Pure and
// schema-only; source/target are read through formatCell (total over Arrow
// types), so they carry no type requirement — a numeric id is a fine key. The
// value column is not type-checked here either, because ClickHouse Decimal and
// LowCardinality both arrive as Arrow types a numeric-kind test would reject
// though quantityCellValue reads them; an unreadable value is reported per row.
func resolveSankeyFlows(schema *arrow.Schema) (fc sankeyFlowsClaim, reason string) {
	fc = sankeyFlowsClaim{srcCol: -1, tgtCol: -1, valCol: -1, labelCol: -1, toneCol: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case sankeySourceCol:
			fc.srcCol = ci
		case sankeyTargetCol:
			fc.tgtCol = ci
		case sankeyValueCol:
			fc.valCol = ci
		case sankeyLabelCol:
			fc.labelCol = ci
		case sankeyToneCol:
			fc.toneCol = ci
		}
	}
	var missing []string
	if fc.srcCol < 0 {
		missing = append(missing, "`source`")
	}
	if fc.tgtCol < 0 {
		missing = append(missing, "`target`")
	}
	if fc.valCol < 0 {
		missing = append(missing, "`value`")
	}
	if len(missing) > 0 {
		reason = fmt.Sprintf("The diagram's `flows` CTE needs a %s column. Name them in the query — e.g. "+
			"WITH flows AS (SELECT a AS source, b AS target, sum(v) AS value FROM t GROUP BY 1, 2) SELECT * FROM flows "+
			"— and optionally add a `nodes` CTE (`id`, `label`, `stage`, `order`, `group`, `tone`) to decorate them.",
			strings.Join(missing, ", a "))
	}
	return
}

// resolveSankeyNodes applies the node contract. Only `id` is required; a nodes
// CTE missing it is rejected, and because the channel is optional the panel
// simply draws from the flows alone (endpoint synthesis).
func resolveSankeyNodes(schema *arrow.Schema) (nc sankeyNodesClaim, reason string) {
	nc = sankeyNodesClaim{idCol: -1, labelCol: -1, stageCol: -1, orderCol: -1, groupCol: -1, toneCol: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case sankeyIDCol:
			nc.idCol = ci
		case sankeyLabelCol:
			nc.labelCol = ci
		case sankeyStageCol:
			nc.stageCol = ci
		case sankeyOrderCol:
			nc.orderCol = ci
		case sankeyGroupCol:
			nc.groupCol = ci
		case sankeyToneCol:
			nc.toneCol = ci
		}
	}
	if nc.idCol < 0 {
		reason = "the `nodes` CTE needs an `id` column"
	}
	return
}

// render maps the two results into a diagram, lays it out (cached), draws it,
// and tracks the pinned hit.
func (inst *SankeyDriver) render(flowsRec arrow.RecordBatch, fc sankeyFlowsClaim,
	nodesRec arrow.RecordBatch, nc sankeyNodesClaim, emit SignalEmitterI) {
	b := buildSankeyDiagram(flowsRec, fc, nodesRec, nc, inst.choice)
	inst.stats = b.stats
	inst.renderControls(b.stats.stagesGiven)

	if len(b.diagram.Links) == 0 {
		// Drop the cached layout with the flows that produced it. Kept, it
		// would go on reporting the previous diagram's stages and total beside
		// a pane that is now empty — the one lie a status line must not tell.
		inst.layout, inst.layoutKey, inst.layoutErr, inst.modeFallback = nil, "", nil, ""
		c.Label(inst.statusLine()).Send()
		for rt := range c.RichTextLabel("The `flows` CTE produced no drawable flows: every row was missing an endpoint, " +
			"carried a value that is not a positive number, or flowed to itself.") {
			rt.Small().Weak()
		}
		return
	}

	if key := sankeyDiagramKey(b.diagram); key != inst.layoutKey || (inst.layout == nil && inst.layoutErr == nil) {
		inst.layoutKey = key
		inst.layout, inst.modeUsed, inst.modeFallback, inst.layoutErr = computeSankeyLayout(b.diagram)
	}
	if inst.layout == nil {
		c.Label(inst.statusLine()).Send()
		msg := "the flows cannot be laid out as a diagram"
		if inst.layoutErr != nil {
			msg += ": " + sankeyReason(inst.layoutErr)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	// The readouts go ABOVE the plot. Both are one frame behind — a register
	// read always is — so nothing is lost by drawing them before the frame's
	// own Show, and it puts the total, the warnings and the hover readout where
	// a pane too short for the diagram cannot push them out of sight.
	c.Label(inst.pointerLine(inst.hover)).Send()

	// The hint goes above the plot too, and here it is what lets the plot be the
	// LAST widget in the body: the probe below reports the room left for the
	// next widget, so with the hint underneath, taking that room would push it
	// past the fold and hold a scrollbar open — which narrows the pane, which
	// resizes the plot.
	for rt := range c.RichTextLabel("hover a bar or a ribbon, click to pin it; drag pans and the wheel zooms — " +
		"over the diagram the wheel is the plot's, elsewhere it scrolls the pane") {
		rt.Small().Weak()
	}

	// Fill the pane: a full-width separator, then a seq-keyed probe of the free
	// rect that reads back next frame (a per-seq r21 slot, so it contends with
	// nobody, unlike the single CaptureAvailableSize register that the frame's
	// last capture wins). Emitted after the chrome and BEFORE the plot, since
	// the rect is the room left for the NEXT widget.
	//
	// The height used to be a fixed 0.34 of the width, tuned so that a maximised
	// window's DEFAULT body leaf fitted the diagram without scrolling — the best
	// available answer while the pane's height was unreadable, and one that left
	// any other leaf either scrolling or part empty. captureUiAvailableRect
	// carries the height, so the leaf can simply say.
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(sankeyIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := sankeyPaneFill.box(inst.paneW, inst.paneH)

	hover, click, clicked := inst.renderer.Show(inst.ids, "flow##playsankey", w, h, inst.layout, sankeyview.Opts{
		Fill:       inst.fill,
		Gradient:   inst.gradient,
		HideLabels: inst.hideLabels,
		Selected:   inst.selected,
	})
	inst.hover = hover
	// Any click updates the pin, including one that landed on empty area — that
	// is what clears it. A pinned NODE also publishes its id; clearing the pin
	// publishes the empty string, which is the honest "nothing focused" value a
	// query reading `{selection_key:String}` sees before anything is clicked.
	if clicked {
		if click == inst.selected {
			inst.selected = sankeyview.Hit{}
		} else {
			inst.selected = click
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selectedNodeID())
		}
	}
}

// selectedNodeID is the pinned node's id, or "" when the pin is empty or on a
// ribbon. A ribbon is a pair of ids rather than one key, so it publishes
// nothing — the pointer line already names both ends.
func (inst *SankeyDriver) selectedNodeID() string {
	if inst.layout == nil || inst.selected.Kind != sankeyview.HitNode {
		return ""
	}
	i := inst.selected.Node()
	if i < 0 || i >= len(inst.layout.Nodes) {
		return ""
	}
	return inst.layout.Nodes[i].ID
}

// renderControls draws the mode, fill route and label toggles. Changing the
// mode re-keys the layout cache, so the next frame re-lays-out; the fill and
// label switches are draw-time only and do not.
//
// The groups are separated by AddSpace, never by c.Separator(): a separator in
// a horizontal row is a VERTICAL rule sized to the row's available height, and
// this pane's is the whole dock leaf. It balloons to the full pane height,
// makes the control row that tall, and shoves the diagram off the bottom — the
// trap the Table pane's options bar hit first.
func (inst *SankeyDriver) renderControls(stagesGiven bool) {
	gap := styletokens.GapSections(styletokens.DensityFromEnv())
	for range c.HorizontalTop().KeepIter() {
		c.Label("mode").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "sankey-mode", &inst.choice).
			Inline().
			Style(selector.StyleSelectable).
			Option(sankeyChoiceAuto, "auto").
			Option(sankeyChoiceSankey, "sankey").
			Option(sankeyChoiceAlluvial, "alluvial").
			SendResp()
		c.AddSpace(gap)
		c.Label("fill").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "sankey-fill", &inst.fill).
			Inline().
			Style(selector.StyleSelectable).
			Option(sankeyview.FillPolygon, "polygon").
			Option(sankeyview.FillColumns, "columns").
			SendResp()
		// A single polygon carries one colour, so the gradient is only offered
		// on the batched route (ADR-0159 §SD2).
		if inst.fill == sankeyview.FillColumns {
			c.AddSpace(gap)
			c.Checkbox(inst.ids.PrepareStr("sankey-gradient"), inst.gradient, "gradient").
				SendRespVal(&inst.gradient)
		}
		c.AddSpace(gap)
		c.Checkbox(inst.ids.PrepareStr("sankey-hidelabels"), inst.hideLabels, "hide labels").
			SendRespVal(&inst.hideLabels)
	}
	if inst.choice == sankeyChoiceAlluvial && !stagesGiven {
		for rt := range c.RichTextLabel("alluvial needs a `stage` for every node — add one to the `nodes` CTE, " +
			"and give it a row for every endpoint the `flows` CTE names") {
			rt.Small().Weak()
		}
	}
}

// pointerLine describes what the pointer is over, falling back to the pinned
// hit and then to the diagram's own summary — the demo's idiom, since what a
// reader wants from a ribbon is its quantity and its share of the total.
func (inst *SankeyDriver) pointerLine(hover sankeyview.Hit) string {
	lay := inst.layout
	if lay == nil {
		return inst.statusLine()
	}
	describe := func(h sankeyview.Hit) string {
		switch h.Kind {
		case sankeyview.HitNode:
			if i := h.Node(); i >= 0 && i < len(lay.Nodes) {
				n := &lay.Nodes[i]
				return fmt.Sprintf("%s — %s in, %s out", n.Label, sankeyQty(n.In), sankeyQty(n.Out))
			}
		case sankeyview.HitLink:
			if i := h.Link(); i >= 0 && i < len(lay.Links) {
				l := &lay.Links[i]
				s := fmt.Sprintf("%s → %s — %s", lay.Nodes[l.Source].Label, lay.Nodes[l.Target].Label, sankeyQty(l.Value))
				if lay.Report.Total > 0 {
					s += fmt.Sprintf(" (%.1f%% of total)", 100*l.Value/lay.Report.Total)
				}
				if l.Label != "" {
					s += " · " + truncateRunes(l.Label, 60)
				}
				return s
			}
		}
		return ""
	}
	if s := describe(hover); s != "" {
		return s
	}
	if s := describe(inst.selected); s != "" {
		return "pinned: " + s
	}
	return inst.statusLine()
}

// statusLine reports the diagram's shape and everything the build or the layout
// noticed but could not decide: folded duplicates, dropped rows, flows too thin
// to see, and quantities that do not balance.
func (inst *SankeyDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d flows", inst.stats.nodes, inst.stats.links)
	if inst.layout != nil {
		fmt.Fprintf(&b, " · %d stages · %s total", inst.layout.Stages, sankeyQty(inst.layout.Report.Total))
		if inst.modeUsed == sankey.ModeAlluvial {
			b.WriteString(" · alluvial")
		}
	}
	if inst.modeFallback != "" {
		fmt.Fprintf(&b, " · not alluvial: %s", inst.modeFallback)
	}
	if inst.stats.collapsed > 0 {
		fmt.Fprintf(&b, " · %s duplicate flow(s) summed", humanize.Comma(int64(inst.stats.collapsed)))
	}
	if inst.stats.droppedValue > 0 {
		fmt.Fprintf(&b, " · %s row(s) without an endpoint or a positive value", humanize.Comma(int64(inst.stats.droppedValue)))
	}
	if inst.stats.droppedSelf > 0 {
		fmt.Fprintf(&b, " · %s self-flow(s) dropped", humanize.Comma(int64(inst.stats.droppedSelf)))
	}
	if inst.stats.capped {
		fmt.Fprintf(&b, " · capped at %d nodes / %d flows (add a LIMIT or aggregate)", sankeyMaxNodes, sankeyMaxLinks)
	}
	if inst.layout != nil {
		if n := inst.layout.Report.ThinLinks; n > 0 {
			fmt.Fprintf(&b, " · %d flow(s) too thin to read", n)
		}
		if n := sankeyview.PaletteRepeats(inst.layout, sankeyview.Opts{}); n > 0 {
			fmt.Fprintf(&b, " · %d bar(s) reuse a hue", n)
		}
		if nc := inst.layout.Report.NonConserving; len(nc) > 0 {
			fmt.Fprintf(&b, " · unbalanced: %s", truncateRunes(strings.Join(nc, ", "), 60))
		}
	}
	switch {
	case inst.flowsErr != nil:
		fmt.Fprintf(&b, " · flows query failed: %v", inst.flowsErr)
	case inst.nodesErr != nil:
		fmt.Fprintf(&b, " · nodes query failed: %v", inst.nodesErr)
	case inst.flowsLoading || inst.nodesLoading:
		b.WriteString(" · …")
	}
	return b.String()
}

// sankeyQty formats a quantity for the status and pointer lines. The diagram
// carries no unit — a `value` column is a bare number — so the job is only to
// keep a big total from crowding the line out.
func sankeyQty(v float64) string {
	switch av := math.Abs(v); {
	case av >= 1e9:
		return strconv.FormatFloat(v/1e9, 'f', 1, 64) + "G"
	case av >= 1e6:
		return strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
	case av >= 1e4:
		return strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	default:
		return strconv.FormatFloat(v, 'g', 4, 64)
	}
}

// renderSankeyTab is the Sankey dock tab body: the two named CTEs demanded on
// their lanes, then the PanelI dispatch. Like the Network tab it does not read
// the active result — its inputs are the `flows` and `nodes` CTEs by name.
func (inst *PlayApp) renderSankeyTab() {
	flowsRec, flowsSchema := inst.demandSankeyFlows()
	if flowsRec != nil {
		defer flowsRec.Release()
	}
	nodesRec, nodesSchema := inst.demandSankeyNodes()
	if nodesRec != nil {
		defer nodesRec.Release()
	}

	inputs := map[ChannelID]channelInput{
		chFlows: {node: sankeyFlowsNodeID, rec: flowsRec, schema: flowsSchema, sig: inst.frameSig},
	}
	// Offer the nodes channel only when the CTE exists (a schema-only view still
	// fills it, so an inventory that legitimately returned nothing reads as "no
	// nodes" rather than as pending).
	if nodesRec != nil || nodesSchema != nil {
		inputs[chNodes] = channelInput{node: sankeyNodesNodeID, rec: nodesRec, schema: nodesSchema, sig: inst.frameSig}
	}
	reject := dispatchPanel(sankeyPanel{driver: inst.sankeyDriver}, inputs, inst.sigEmit)
	if reject != "" {
		if inst.sankeyDriver != nil && inst.sankeyDriver.flowsLoading {
			for rt := range c.RichTextLabel("building the diagram…") {
				rt.Small().Weak()
			}
			return
		}
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// demandSankeyFlows compiles the query's `flows` CTE — if it has one — and
// demands it on the driver's flows lane, returning the retained result for the
// chFlows channel (the caller MUST Release rec). Mirrors demandNetworkEdges:
// the node comes from the last Run's split, so its signal reads resolve like
// any other node's and a SET-bound name travels inside the fused SQL.
func (inst *PlayApp) demandSankeyFlows() (rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.sankeyDriver
	if d == nil || d.flowsLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, sankeyFlowsNodeID)
	if !ok {
		d.flowsLoading = false
		d.flowsErr = nil
		return
	}
	v := d.flowsLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, sankeyFlowsNodeID),
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.flowsLoading = v.loading
	d.flowsErr = v.err // mirrored every demand — nil clears (no latch)
	return v.rec, v.schema
}

// demandSankeyNodes is demandSankeyFlows for the optional `nodes` CTE.
func (inst *PlayApp) demandSankeyNodes() (rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.sankeyDriver
	if d == nil || d.nodesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, sankeyNodesNodeID)
	if !ok {
		d.nodesLoading = false
		d.nodesErr = nil
		return
	}
	v := d.nodesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, sankeyNodesNodeID),
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.nodesLoading = v.loading
	d.nodesErr = v.err
	return v.rec, v.schema
}
