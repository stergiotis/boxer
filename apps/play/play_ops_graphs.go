package play

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// The Network and Graphview panes as an agent reads and sets them (ADR-0270,
// update of 2026-10-05). Both draw the graph contract (the `edges`,
// `vertices` and `graph_opts` CTEs, each on its own lane), so their reads
// share the vertex and edge pages and the lanes' state; each pane keeps its
// own command and its own selection command, since the two select
// differently: the Network pane highlights one vertex, the Graphview pane a
// set. Graphview's reading adds what play computes and SQL does not give:
// the graph metrics, the settle state, and which of the pane's settings the
// query or the pane decided.

const (
	opGetNetwork           = "get_network"
	opSetNetworkOptions    = "set_network_options"
	opSelectNetworkNode    = "select_network_node"
	opGetGraphview         = "get_graphview"
	opSetGraphviewOptions  = "set_graphview_options"
	opSelectGraphviewNodes = "select_graphview_nodes"
	networkPaneId          = "network"
	graphviewPaneId        = "graphview"
	opsResNetwork          = networkPaneId
	opsResGraphview        = graphviewPaneId
	graphReadDefaultNodes  = 100
	graphReadDefaultEdges  = 200
	graphReadMaxItems      = 500
	// graphReadMaxBytes bounds one listed page (vertices, edges, nodes or
	// flows) by its text, as opsSampleMaxBytes bounds sample_rows: a page
	// stops early and its More count says how many are past it.
	graphReadMaxBytes = 24 << 10
	// graphReadMaxMetrics bounds the metrics one get_graphview computes;
	// betweenness is the costly one.
	graphReadMaxMetrics = 4
	// graphMetricTimeout bounds a metric computed for a read.
	graphMetricTimeout = 10 * time.Second
	// graphReadMaxGroups bounds the group names a read lists.
	graphReadMaxGroups = 24
)

// GraphLanesReading is the state of the lanes that feed a graph pane.
type GraphLanesReading struct {
	EdgesError    string `json:",omitzero" desc:"the edges CTE's error"`
	VerticesError string `json:",omitzero" desc:"the vertices CTE's error"`
	Loading       bool   `json:",omitzero" desc:"a lane is still running; the reading is of what it served before"`
}

func graphLanesOf(src *networkSource) (r GraphLanesReading) {
	if src == nil {
		return
	}
	if src.edgesErr != nil {
		r.EdgesError = truncateBytes(src.edgesErr.Error(), opsStatusMaxBytes)
	}
	if src.verticesErr != nil {
		r.VerticesError = truncateBytes(src.verticesErr.Error(), opsStatusMaxBytes)
	}
	r.Loading = src.edgesLoading || src.verticesLoading
	return
}

// GraphMetricValue is one metric of a vertex.
type GraphMetricValue struct {
	Metric string   `desc:"the metric's name"`
	Value  *float64 `json:",omitzero" desc:"absent where the metric has no value for this vertex: an unreached vertex under distance, no neighbour pair under clustering"`
}

// GraphVertexReading is one vertex as the pane drew it.
type GraphVertexReading struct {
	Id       string             `desc:"the vertex id as the query declared it, cut at 128 bytes; give it back to the pane's select command"`
	Label    string             `json:",omitzero" desc:"its label, when it differs from the id"`
	Group    string             `json:",omitzero" desc:"its group"`
	Weight   *float64           `json:",omitzero" desc:"its weight; absent where the query gave none"`
	Selected bool               `json:",omitzero" desc:"selected in the pane"`
	Metrics  []GraphMetricValue `json:",omitzero" desc:"the metrics asked for, in the order asked"`
}

// GraphEdgeReading is one edge as the pane drew it.
type GraphEdgeReading struct {
	Source string   `desc:"the source vertex id"`
	Target string   `desc:"the target vertex id"`
	Label  string   `json:",omitzero" desc:"the edge's label"`
	Weight *float64 `json:",omitzero" desc:"its weight; absent where the query gave none"`
}

// graphPage bounds one page of vertices or edges.
func graphPage(offset, limit int32, def int32, n int, what string) (start, end int, err error) {
	switch {
	case limit < 0 || limit > graphReadMaxItems:
		err = app.RefuseOperation(what + " limit is at most " + strconv.Itoa(graphReadMaxItems))
		return
	case limit == 0:
		limit = def
	}
	if offset < 0 {
		err = app.RefuseOperation(what + " offset is a count to skip, 0 or more")
		return
	}
	start = min(int(offset), n)
	end = min(start+int(limit), n)
	return
}

func positiveOrNil(w float64) *float64 {
	if w > 0 {
		return finite(w)
	}
	return nil
}

// ---- Network ----

// NetworkReading is get_network's result.
type NetworkReading struct {
	Drawn        PaneDraw             `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	RankDir      string               `desc:"the layout's direction: top-down or left-right"`
	Nodes        int32                `json:",omitzero" desc:"vertices drawn"`
	Edges        int32                `json:",omitzero" desc:"edges drawn"`
	Capped       bool                 `json:",omitzero" desc:"the graph reached the pane's cap of 400 vertices or 1,000 edges; the rest were dropped"`
	LayoutError  string               `json:",omitzero" desc:"why the layout engine laid nothing out"`
	Selected     string               `json:",omitzero" desc:"the highlighted vertex, which selection_key carries"`
	Declared     []string             `json:",omitzero" desc:"vertices the query declared selected"`
	Lanes        GraphLanesReading    `desc:"the state of the CTE lanes"`
	Vertices     []GraphVertexReading `json:",omitzero" desc:"a page of the vertices, in the order the build holds them"`
	MoreVertices int32                `json:",omitzero" desc:"vertices past the page; a page also stops at about 24 KiB of text"`
	EdgeList     []GraphEdgeReading   `json:",omitzero" desc:"a page of the edges"`
	MoreEdges    int32                `json:",omitzero" desc:"edges past the page; a page also stops at about 24 KiB of text"`
}

// GetGraphArgs pages a graph pane's vertices and edges.
type GetGraphArgs struct {
	Offset     int32 `json:",omitzero" desc:"vertices to skip"`
	Limit      int32 `json:",omitzero" desc:"vertices to list, 100 by default and at most 500"`
	EdgeOffset int32 `json:",omitzero" desc:"edges to skip"`
	EdgeLimit  int32 `json:",omitzero" desc:"edges to list, 200 by default and at most 500"`
}

// SetNetworkOptionsArgs is set_network_options' argument.
type SetNetworkOptionsArgs struct {
	RankDir *string `json:",omitzero" desc:"top-down or left-right"`
}

// SelectNetworkNodeArgs is select_network_node's argument.
type SelectNetworkNodeArgs struct {
	Id    string `json:",omitzero" desc:"the vertex to highlight, as get_network lists it"`
	Clear bool   `json:",omitzero" desc:"drop the highlight; selection_key becomes empty"`
}

// networkOpsView is what get_network reads: the last frame's build, shared,
// and the pane's state as it stands.
type networkOpsView struct {
	model       layeredgraph.GraphModel
	declaredSel map[string]struct{}
	rankDir     layeredgraph.RankDir
	selected    string
	capped      bool
	layoutErr   error
	lanes       GraphLanesReading
}

func (inst *PlayApp) networkView() networkOpsView {
	d := inst.networkDriver
	return networkOpsView{model: d.model, declaredSel: d.declaredSel, rankDir: d.rankDir, selected: d.selectedID,
		capped: d.capped, layoutErr: d.layoutErr, lanes: graphLanesOf(d.src)}
}

func networkRankDirName(rd layeredgraph.RankDir) string {
	if rd == layeredgraph.RankDirLeftRight {
		return "left-right"
	}
	return "top-down"
}

func networkReading(sn *opsSnap, in GetGraphArgs) (out NetworkReading, err error) {
	d, readable, err := paneDrawOf(sn, networkPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.network
	out = NetworkReading{Drawn: d, RankDir: networkRankDirName(v.rankDir), Selected: hierPathLabel(v.selected), Lanes: v.lanes}
	if v.layoutErr != nil {
		out.LayoutError = truncateBytes(v.layoutErr.Error(), opsStatusMaxBytes)
	}
	if !readable {
		return
	}
	nodes, edges := v.model.Nodes, v.model.Edges
	out.Nodes, out.Edges, out.Capped = int32(len(nodes)), int32(len(edges)), v.capped
	for id := range v.declaredSel {
		out.Declared = append(out.Declared, hierPathLabel(id))
	}
	slices.Sort(out.Declared)
	start, end, err := graphPage(in.Offset, in.Limit, graphReadDefaultNodes, len(nodes), "vertex")
	if err != nil {
		return
	}
	used := 0
	for i, n := range nodes[start:end] {
		r := GraphVertexReading{Id: hierPathLabel(n.ID), Weight: positiveOrNil(n.Weight), Selected: n.ID == v.selected}
		if n.Label != n.ID {
			r.Label = opsLabel(n.Label)
		}
		if used += pageBytes(r.Id, r.Label); used > graphReadMaxBytes && i > 0 {
			end = start + i
			break
		}
		out.Vertices = append(out.Vertices, r)
	}
	out.MoreVertices = int32(len(nodes) - end)
	start, end, err = graphPage(in.EdgeOffset, in.EdgeLimit, graphReadDefaultEdges, len(edges), "edge")
	if err != nil {
		return
	}
	used = 0
	for i, e := range edges[start:end] {
		r := GraphEdgeReading{Source: hierPathLabel(e.From), Target: hierPathLabel(e.To),
			Label: opsLabel(e.Label), Weight: positiveOrNil(e.Weight)}
		if used += pageBytes(r.Source, r.Target, r.Label); used > graphReadMaxBytes && i > 0 {
			end = start + i
			break
		}
		out.EdgeList = append(out.EdgeList, r)
	}
	out.MoreEdges = int32(len(edges) - end)
	return
}

// pinFor is the vertex a select highlights, checked against the last build.
func (inst *NetworkDriver) pinFor(in SelectNetworkNodeArgs) (id string, err error) {
	if in.Clear {
		if in.Id != "" {
			return "", app.RefuseOperation("clear drops the highlight; give it alone, or give an id without it")
		}
		return "", nil
	}
	if in.Id == "" {
		return "", app.RefuseOperation("name the vertex to highlight by its id, or clear the highlight")
	}
	if len(inst.model.Nodes) == 0 {
		return "", app.RefuseOperation("the Network pane has no vertices to highlight: run a graph query, and show_pane network")
	}
	nodes := inst.model.Nodes
	i, ambiguous := hierMatchLabel(in.Id, len(nodes), func(i int) string { return nodes[i].ID })
	if i >= 0 {
		return nodes[i].ID, nil
	}
	if ambiguous > 1 {
		return "", app.RefuseOperation(strconv.Itoa(ambiguous) + " vertices have ids that cut to " + strconv.Quote(in.Id) + "; give the whole id")
	}
	return "", app.RefuseOperation("no vertex " + strconv.Quote(in.Id) + " is drawn; get_network lists them")
}

// pageBytes is the rough JSON size of a listed item's text fields.
func pageBytes(fields ...string) (n int) {
	n = 48
	for _, f := range fields {
		n += len(f)
	}
	return
}

// requestSelect is a node click: through select_network_node when play's
// launcher routes it, directly through the pane's emitter otherwise.
func (inst *NetworkDriver) requestSelect(in SelectNetworkNodeArgs, emit SignalEmitterI) {
	if inst.onSelect != nil {
		inst.onSelect(in)
		return
	}
	id, err := inst.pinFor(in)
	if err != nil {
		return
	}
	inst.selectedID = id
	if emit != nil {
		emit.Emit(signalSelectionKey, id)
	}
}

// selectNetworkNode is select_network_node: the highlight and
// selection_key together.
func (inst *PlayApp) selectNetworkNode(in SelectNetworkNodeArgs, writer string) (err error) {
	id, err := inst.networkDriver.pinFor(in)
	if err != nil {
		return
	}
	inst.networkDriver.selectedID = id
	inst.graph.setSignalRawFrom(signalSelectionKey, id, writer)
	return
}

func (inst *NetworkDriver) setOptions(in SetNetworkOptionsArgs) error {
	if in.RankDir == nil {
		return noOptionsRefusal(networkPaneId, "rank_dir")
	}
	switch strings.TrimSpace(*in.RankDir) {
	case "top-down":
		inst.rankDir = layeredgraph.RankDirTopBottom
	case "left-right":
		inst.rankDir = layeredgraph.RankDirLeftRight
	default:
		return app.RefuseOperation("rank_dir is top-down or left-right")
	}
	return nil
}

// networkOptionsDigest is the network resource: the direction and the
// highlighted vertex.
func networkOptionsDigest(p *PlayApp) string {
	d := p.networkDriver
	if d == nil {
		return ""
	}
	return "rank=" + strconv.Itoa(int(d.rankDir)) + "|sel=" + d.selectedID
}

// ---- Graphview ----

// GraphviewSetting is one of the pane's settings and who decided it.
type GraphviewSetting struct {
	Value string `desc:"the setting in effect"`
	From  string `desc:"pane (set on the pane, by the person or set_graphview_options), query (graph_opts) or default; auto in set_graphview_options hands a pane setting back to the query"`
}

// GraphviewReading is get_graphview's result.
type GraphviewReading struct {
	Drawn         PaneDraw             `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Layout        GraphviewSetting     `desc:"force, gravity, tree, radial or random"`
	Orientation   GraphviewSetting     `desc:"top-down or left-right; applies to the tree layout"`
	SizeBy        GraphviewSetting     `desc:"what the node size encodes: weight, a metric or a column"`
	SizeNote      string               `json:",omitzero" desc:"why the size channel fell back to weight"`
	Auras         GraphviewSetting     `desc:"on or off: blobs drawn around each group"`
	AurasOverlap  bool                 `json:",omitzero" desc:"auras may overlap"`
	HoldDropped   GraphviewSetting     `desc:"on or off: a dragged vertex stays where it is dropped"`
	Basemap       *GraphviewSetting    `json:",omitzero" desc:"on or off, for a located graph only: drawn over a map"`
	Paused        bool                 `json:",omitzero" desc:"the simulation is paused on the pane"`
	Settle        string               `json:",omitzero" desc:"the simulation: settling, settled, frozen (stopped while still moving) or paused"`
	Located       bool                 `json:",omitzero" desc:"the vertices carry lat/lon"`
	Nodes         int32                `json:",omitzero" desc:"vertices drawn"`
	Edges         int32                `json:",omitzero" desc:"edges drawn"`
	Capped        bool                 `json:",omitzero" desc:"the graph reached the pane's cap of 2,000 vertices or 6,000 edges; the rest were dropped"`
	LabelsOnHover bool                 `json:",omitzero" desc:"past the label budget, labels show only on hover"`
	Groups        []string             `json:",omitzero" desc:"the groups, at most 24"`
	GroupCount    int32                `json:",omitzero" desc:"how many groups there are"`
	Selected      []string             `json:",omitzero" desc:"the vertices selected on the pane, as gv_selection carries them"`
	Declared      []string             `json:",omitzero" desc:"vertices the query declared selected"`
	Hover         string               `json:",omitzero" desc:"the vertex under the pointer, as gv_hover carries it"`
	Notes         []string             `json:",omitzero" desc:"the query's graph_opts and encoding selectors the pane could not honour"`
	Truncated     []string             `json:",omitzero" desc:"metrics computed for the pane that stopped short; their values are lower bounds"`
	Lanes         GraphLanesReading    `desc:"the state of the CTE lanes"`
	Metrics       []string             `json:",omitzero" desc:"the metrics computed for this read"`
	MetricNotes   []string             `json:",omitzero" desc:"why an asked-for metric has no values"`
	Vertices      []GraphVertexReading `json:",omitzero" desc:"a page of the vertices, in the order of their interned keys"`
	MoreVertices  int32                `json:",omitzero" desc:"vertices past the page; a page also stops at about 24 KiB of text"`
	EdgeList      []GraphEdgeReading   `json:",omitzero" desc:"a page of the edges"`
	MoreEdges     int32                `json:",omitzero" desc:"edges past the page; a page also stops at about 24 KiB of text"`
}

// GetGraphviewArgs is get_graphview's argument.
type GetGraphviewArgs struct {
	Offset     int32    `json:",omitzero" desc:"vertices to skip"`
	Limit      int32    `json:",omitzero" desc:"vertices to list, 100 by default and at most 500"`
	EdgeOffset int32    `json:",omitzero" desc:"edges to skip"`
	EdgeLimit  int32    `json:",omitzero" desc:"edges to list, 200 by default and at most 500"`
	Metrics    []string `json:",omitzero" desc:"at most 4 metrics to give per vertex: degree, in_degree, out_degree, pagerank, betweenness, kcore, triangles, clustering, clique, component, scc, component_size, distance, relevance, …; the seeded ones (distance, relevance) measure from the selection"`
}

// SetGraphviewOptionsArgs is set_graphview_options' argument.
type SetGraphviewOptionsArgs struct {
	Layout       *string `json:",omitzero" desc:"auto (the query's), force, gravity, tree or radial"`
	Orientation  *string `json:",omitzero" desc:"auto, top-down or left-right; the tree layout's direction"`
	SizeBy       *string `json:",omitzero" desc:"auto (the query's), weight, a metric or a vertices column; a leading - inverts it"`
	Auras        *string `json:",omitzero" desc:"auto, on or off; drawn when the vertices carry a group"`
	AurasOverlap *bool   `json:",omitzero" desc:"auras may overlap"`
	HoldDropped  *string `json:",omitzero" desc:"auto, on or off: a dragged vertex stays where it is dropped"`
	Basemap      *string `json:",omitzero" desc:"auto, on or off; a located graph only"`
	Paused       *bool   `json:",omitzero" desc:"pause or resume the simulation"`
	Fit          bool    `json:",omitzero" desc:"frame the whole graph"`
	Relayout     bool    `json:",omitzero" desc:"lay the graph out again from scratch"`
	Settle       bool    `json:",omitzero" desc:"run the simulation ahead until it rests"`
}

// SelectGraphviewNodesArgs is select_graphview_nodes' argument.
type SelectGraphviewNodesArgs struct {
	Ids   []string `json:",omitzero" desc:"the vertices to select, as get_graphview lists them; the selection becomes exactly these"`
	Clear bool     `json:",omitzero" desc:"select nothing; gv_selection becomes empty and selection_key empty"`
}

// graphviewOpsView is what get_graphview reads. The model, the interned ids
// and the metric graph are replaced by a rebuild, never edited, so they are
// shared; everything else is copied.
type graphviewOpsView struct {
	model                               *netModel
	nodes, edges                        int
	capped, labeled                     bool
	groups                              []string
	layout, orient, sizeBy, auras, hold GraphviewSetting
	basemap                             *GraphviewSetting
	sizeReason                          string
	overlap, paused, located            bool
	settle                              string
	selected, declared                  []string
	selKeys                             map[uint64]struct{}
	hover                               string
	notes, truncated                    []string
	lanes                               GraphLanesReading
	metricGraph                         *csr.Graph
	metricRows                          int
	undirected                          bool
	seeds                               []int32
	cols                                map[algo.MetricE][]float64
}

var graphviewLayoutNames = map[graphviewLayoutE]string{
	graphviewLayoutAuto: "auto", graphviewLayoutForce: "force", graphviewLayoutGravity: "gravity",
	graphviewLayoutTree: "tree", graphviewLayoutRadial: "radial", graphviewLayoutRandom: "random",
}

func graphviewOrientName(o graphview.OrientationE) string {
	if o == graphview.OrientationLeftRight {
		return "left-right"
	}
	return "top-down"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func settingFrom(pane bool, query bool) string {
	switch {
	case pane:
		return "pane"
	case query:
		return "query"
	}
	return "default"
}

// opsView copies what get_graphview reads, on the render goroutine.
func (inst *GraphviewDriver) opsView() (v graphviewOpsView) {
	v = graphviewOpsView{model: inst.lastModel, nodes: inst.nodes.Len(), edges: inst.edges.Len(),
		capped: inst.capped, labeled: inst.labeled, groups: inst.groups, sizeReason: inst.sizeReason,
		overlap: inst.overlap, paused: inst.paused, located: inst.located(), lanes: graphLanesOf(inst.src)}
	v.layout = GraphviewSetting{Value: graphviewLayoutNames[inst.effectiveLayout()],
		From: settingFrom(inst.layout != graphviewLayoutAuto, inst.opts.LayoutSet)}
	v.orient = GraphviewSetting{Value: graphviewOrientName(inst.effectiveOrientation()),
		From: settingFrom(inst.orientSet, inst.opts.OrientationSet)}
	size := inst.effectiveSizeBy()
	if size == "" {
		size = networkWeightCol
	}
	v.sizeBy = GraphviewSetting{Value: size, From: settingFrom(inst.sizeBySet, inst.opts.SizeBy != "")}
	v.auras = GraphviewSetting{Value: onOff(inst.effectiveAuras()), From: settingFrom(inst.aurasSet, inst.aurasDefault())}
	v.hold = GraphviewSetting{Value: onOff(inst.effectivePinOnDrag()), From: settingFrom(inst.pinOnDragSet, inst.opts.PinOnDrag)}
	if v.located {
		v.basemap = &GraphviewSetting{Value: onOff(inst.effectiveBasemap()), From: settingFrom(inst.basemapSet, false)}
	}
	v.settle = strings.TrimPrefix(graphviewSettleStatus(inst.view, inst.paused, inst.frozen), " · ")
	v.selKeys = make(map[uint64]struct{}, 4)
	for id := range inst.view.SelectedNodes() {
		v.selKeys[id] = struct{}{}
	}
	if inst.names != nil {
		v.selected = slices.Clone(inst.publishedSelection())
		for id := range inst.declaredSel {
			if name := inst.names.name(id); name != "" {
				v.declared = append(v.declared, name)
			}
		}
		slices.Sort(v.declared)
		if id, ok := inst.view.HoveredNode(); ok {
			v.hover = inst.names.name(id)
		}
	}
	if note := strings.TrimPrefix(inst.opts.statusNote(), " · "); note != "" {
		v.notes = strings.Split(note, " · ")
	}
	v.notes = append(v.notes, inst.chanReasons...)
	if mt := inst.metrics; mt != nil {
		v.truncated = mt.truncatedMetrics()
		v.metricGraph, v.metricRows, v.undirected = mt.g, mt.rows, mt.undirected
		// The seeds are the selection as it stands, which a command may have
		// moved since the pane last derived its seeded columns.
		if inst.names != nil {
			v.seeds = slices.Clone(inst.effectiveSeeds())
		}
		seedKey := hashSlots(v.seeds)
		v.cols = make(map[algo.MetricE][]float64, len(mt.cols))
		for k, col := range mt.cols {
			if k.undirected == mt.undirected && (!k.metric.IsSeeded() || k.seed == seedKey) {
				v.cols[k.metric] = col.Values
			}
		}
	}
	return
}

func (inst *PlayApp) graphviewView() graphviewOpsView { return inst.graphviewDriver.opsView() }

// metricColumns resolves the metrics a read asks for: copied when the pane
// computed them, computed here otherwise, on the shared, immutable metric
// graph with an engine of the read's own.
func (v *graphviewOpsView) metricColumns(names []string) (got []string, cols [][]float64, notes []string, err error) {
	if len(names) > graphReadMaxMetrics {
		err = app.RefuseOperation("at most " + strconv.Itoa(graphReadMaxMetrics) + " metrics per read")
		return
	}
	var eng *engine.Engine
	for _, name := range names {
		m, ok := algo.ParseMetric(strings.TrimSpace(name))
		if !ok {
			err = app.RefuseOperation(strconv.Quote(name) + " is not a metric; graphview's size by combo lists them")
			return
		}
		if v.metricGraph == nil || v.model == nil || v.metricRows != v.model.NumVertices() {
			notes = append(notes, m.String()+": the pane has no metric graph for this result")
			continue
		}
		key := m
		if v.undirected {
			key = m.Symmetric()
		}
		col, cached := v.cols[key]
		if !cached {
			if key.IsSeeded() && len(v.seeds) == 0 {
				notes = append(notes, m.String()+" measures from the selection, and nothing is selected")
				continue
			}
			if eng == nil {
				eng = engine.New(0)
			}
			ctx, cancel := context.WithTimeout(context.Background(), graphMetricTimeout)
			mc, cerr := algo.Compute(ctx, eng, v.metricGraph, key, algo.ComputeOptions{Sources: v.seeds})
			cancel()
			if cerr != nil {
				notes = append(notes, m.String()+": "+truncateBytes(cerr.Error(), opsLabelMaxBytes*2))
				continue
			}
			col = mc.Values
		}
		got, cols = append(got, m.String()), append(cols, col)
	}
	return
}

func graphviewReading(sn *opsSnap, in GetGraphviewArgs) (out GraphviewReading, err error) {
	d, readable, err := paneDrawOf(sn, graphviewPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.graphview
	out = GraphviewReading{Drawn: d, Layout: v.layout, Orientation: v.orient, SizeBy: v.sizeBy, SizeNote: v.sizeReason,
		Auras: v.auras, AurasOverlap: v.overlap, HoldDropped: v.hold, Basemap: v.basemap, Paused: v.paused,
		Settle: v.settle, Located: v.located, Lanes: v.lanes}
	if !readable || v.model == nil {
		return
	}
	m := v.model
	out.Nodes, out.Edges, out.Capped, out.LabelsOnHover = int32(v.nodes), int32(v.edges), v.capped, !v.labeled
	out.GroupCount = int32(len(v.groups))
	for _, g := range v.groups[:min(len(v.groups), graphReadMaxGroups)] {
		out.Groups = append(out.Groups, opsLabel(g))
	}
	for _, s := range v.selected {
		out.Selected = append(out.Selected, hierPathLabel(s))
	}
	for _, s := range v.declared {
		out.Declared = append(out.Declared, hierPathLabel(s))
	}
	out.Hover = hierPathLabel(v.hover)
	for _, n := range v.notes {
		out.Notes = append(out.Notes, truncateBytes(n, opsStatusMaxBytes))
	}
	out.Truncated = v.truncated
	got, cols, notes, err := v.metricColumns(in.Metrics)
	if err != nil {
		return
	}
	out.Metrics, out.MetricNotes = got, notes
	n := m.NumVertices()
	start, end, err := graphPage(in.Offset, in.Limit, graphReadDefaultNodes, n, "vertex")
	if err != nil {
		return
	}
	used := 0
	for i := start; i < end; i++ {
		r := GraphVertexReading{Id: hierPathLabel(m.ID[i]), Weight: positiveOrNil(m.Weight[i])}
		if m.Label[i] != m.ID[i] {
			r.Label = opsLabel(m.Label[i])
		}
		if i < len(m.Group) {
			r.Group = opsLabel(m.Group[i])
		}
		_, r.Selected = v.selKeys[m.Key[i]]
		for j, col := range cols {
			mv := GraphMetricValue{Metric: got[j]}
			if i < len(col) {
				mv.Value = finite(col[i])
			}
			r.Metrics = append(r.Metrics, mv)
		}
		if used += pageBytes(r.Id, r.Label, r.Group) + 40*len(r.Metrics); used > graphReadMaxBytes && i > start {
			end = i
			break
		}
		out.Vertices = append(out.Vertices, r)
	}
	out.MoreVertices = int32(n - end)
	ne := m.NumEdges()
	start, end, err = graphPage(in.EdgeOffset, in.EdgeLimit, graphReadDefaultEdges, ne, "edge")
	if err != nil {
		return
	}
	used = 0
	for i := start; i < end; i++ {
		r := GraphEdgeReading{Source: hierPathLabel(m.FromID[i]), Target: hierPathLabel(m.ToID[i]),
			Label: opsLabel(m.EdgeLabel[i]), Weight: positiveOrNil(m.EdgeWeight[i])}
		if used += pageBytes(r.Source, r.Target, r.Label); used > graphReadMaxBytes && i > start {
			end = i
			break
		}
		out.EdgeList = append(out.EdgeList, r)
	}
	out.MoreEdges = int32(ne - end)
	return
}

// parseAutoOnOff reads an auto/on/off setting: set false means auto.
func parseAutoOnOff(raw string, what string) (val bool, set bool, err error) {
	switch strings.TrimSpace(raw) {
	case "auto":
		return false, false, nil
	case "on":
		return true, true, nil
	case "off":
		return false, true, nil
	}
	return false, false, app.RefuseOperation(what + " is auto, on or off")
}

// setOptions is set_graphview_options on the driver: everything is
// checked before anything is applied.
func (inst *GraphviewDriver) setOptions(in SetGraphviewOptionsArgs) (err error) {
	if in.Layout == nil && in.Orientation == nil && in.SizeBy == nil && in.Auras == nil && in.AurasOverlap == nil &&
		in.HoldDropped == nil && in.Basemap == nil && in.Paused == nil && !in.Fit && !in.Relayout && !in.Settle {
		return noOptionsRefusal(graphviewPaneId, "layout", "orientation", "size_by", "auras", "auras_overlap",
			"hold_dropped", "basemap", "paused", "fit", "relayout", "settle")
	}
	layout := inst.layout
	if in.Layout != nil {
		found := false
		for l, name := range graphviewLayoutNames {
			if l != graphviewLayoutRandom && name == strings.TrimSpace(*in.Layout) {
				layout, found = l, true
			}
		}
		if !found {
			return app.RefuseOperation("layout is auto, force, gravity, tree or radial")
		}
	}
	orient, orientSet := inst.orient, inst.orientSet
	if in.Orientation != nil {
		switch strings.TrimSpace(*in.Orientation) {
		case "auto":
			orientSet = false
		case "top-down":
			orient, orientSet = graphview.OrientationTopDown, true
		case "left-right":
			orient, orientSet = graphview.OrientationLeftRight, true
		default:
			return app.RefuseOperation("orientation is auto, top-down or left-right")
		}
	}
	sizeBy, sizeBySet := inst.sizeBy, inst.sizeBySet
	if in.SizeBy != nil {
		switch name := strings.TrimSpace(*in.SizeBy); name {
		case "auto":
			sizeBy, sizeBySet = "", false
		case networkWeightCol:
			sizeBy, sizeBySet = "", true
		default:
			var hasColumn func(string) bool
			if inst.lastModel != nil {
				hasColumn = inst.lastModel.hasColumn
			}
			if _, reason := parseGraphviewSelector(name, graphviewChannelSize, hasColumn); reason != "" {
				return app.RefuseOperation(reason + "; size_by is auto, " + strings.Join(graphviewSizeOptions(), ", ") + ", or a vertices column")
			}
			sizeBy, sizeBySet = name, true
		}
	}
	auras, aurasSet := inst.auras, inst.aurasSet
	if in.Auras != nil {
		if auras, aurasSet, err = parseAutoOnOff(*in.Auras, "auras"); err != nil {
			return
		}
	}
	hold, holdSet := inst.pinOnDrag, inst.pinOnDragSet
	if in.HoldDropped != nil {
		if hold, holdSet, err = parseAutoOnOff(*in.HoldDropped, "hold_dropped"); err != nil {
			return
		}
	}
	basemap, basemapSet := inst.basemap, inst.basemapSet
	if in.Basemap != nil {
		if !inst.located() {
			return app.RefuseOperation("basemap applies to a located graph, one whose vertices carry lat and lon")
		}
		if basemap, basemapSet, err = parseAutoOnOff(*in.Basemap, "basemap"); err != nil {
			return
		}
	}
	if (in.Fit || in.Relayout || in.Settle) && inst.names == nil {
		return app.RefuseOperation("the Graphview pane has no graph to fit or lay out: run a graph query, and show_pane graphview")
	}

	inst.layout = layout
	inst.orient, inst.orientSet = orient, orientSet
	if sizeBy != inst.sizeBy || sizeBySet != inst.sizeBySet {
		inst.sizeBy, inst.sizeBySet = sizeBy, sizeBySet
		// The declaration carries the radius, so a change of channel is a
		// rebuild rather than a repaint.
		inst.keyOk = false
	}
	inst.auras, inst.aurasSet = auras, aurasSet
	inst.pinOnDrag, inst.pinOnDragSet = hold, holdSet
	inst.basemap, inst.basemapSet = basemap, basemapSet
	if in.AurasOverlap != nil {
		inst.overlap = *in.AurasOverlap
	}
	if in.Paused != nil {
		inst.paused = *in.Paused
	}
	if in.Fit {
		inst.view.FitNow()
		inst.hostFitPending = true
	}
	if in.Relayout {
		inst.view.ResetLayout()
		inst.frozen = false
	}
	if in.Settle {
		inst.view.FastForward(graphviewSettleBudget(inst.nodes.Len()))
		inst.frozen = false
	}
	return nil
}

// requestOptions is an in-frame control: through set_graphview_options when
// play's launcher routes it, directly otherwise.
func (inst *GraphviewDriver) requestOptions(in SetGraphviewOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// selectionFor resolves a select's ids to keys, checked against the
// declaration drawn.
func (inst *GraphviewDriver) selectionFor(in SelectGraphviewNodesArgs) (keys []uint64, err error) {
	if in.Clear {
		if len(in.Ids) > 0 {
			return nil, app.RefuseOperation("clear selects nothing; give it alone, or give ids without it")
		}
		return nil, nil
	}
	if len(in.Ids) == 0 {
		return nil, app.RefuseOperation("name the vertices to select, or clear the selection")
	}
	if inst.names == nil || inst.lastModel == nil {
		return nil, app.RefuseOperation("the Graphview pane has no vertices to select: run a graph query, and show_pane graphview")
	}
	m := inst.lastModel
	for _, given := range in.Ids {
		i, ambiguous := hierMatchLabel(given, len(m.ID), func(i int) string { return m.ID[i] })
		switch {
		case i >= 0:
			keys = append(keys, m.Key[i])
		case ambiguous > 1:
			return nil, app.RefuseOperation(strconv.Itoa(ambiguous) + " vertices have ids that cut to " + strconv.Quote(given) + "; give the whole id")
		default:
			return nil, app.RefuseOperation("no vertex " + strconv.Quote(given) + " is drawn; get_graphview lists them")
		}
	}
	return
}

// selectGraphviewNodes is select_graphview_nodes: the widget's selection,
// gv_selection and selection_key together, as the pane would publish them.
func (inst *PlayApp) selectGraphviewNodes(in SelectGraphviewNodesArgs, writer string) (err error) {
	d := inst.graphviewDriver
	keys, err := d.selectionFor(in)
	if err != nil {
		return
	}
	d.view.ClearSelection()
	for _, k := range keys {
		d.view.SelectNode(k)
	}
	sel := slices.Clone(d.publishedSelection())
	last := ""
	if len(sel) > 0 {
		last = sel[len(sel)-1]
	}
	raw, _ := encodeSignalValue(sel)
	inst.graph.setSignalRawFrom(signalGvSelection, raw, writer)
	d.selectedID = last
	inst.graph.setSignalRawFrom(signalSelectionKey, last, writer)
	return
}

// graphviewOptionsDigest is the graphview resource: the pane's settings and
// its selection.
func graphviewOptionsDigest(p *PlayApp) string {
	d := p.graphviewDriver
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.Itoa(int(d.layout)) + "|" + strconv.FormatBool(d.orientSet) + strconv.Itoa(int(d.orient)) +
		"|" + strconv.FormatBool(d.sizeBySet) + d.sizeBy + "|" + strconv.FormatBool(d.aurasSet) + strconv.FormatBool(d.auras) +
		"|" + strconv.FormatBool(d.overlap) + "|" + strconv.FormatBool(d.pinOnDragSet) + strconv.FormatBool(d.pinOnDrag) +
		"|" + strconv.FormatBool(d.basemapSet) + strconv.FormatBool(d.basemap) + "|" + strconv.FormatBool(d.paused) + "|sel=")
	for id := range d.view.SelectedNodes() {
		b.WriteString(strconv.FormatUint(id, 36) + ",")
	}
	return b.String()
}

func addGraphOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[NetworkReading, GetGraphArgs, SetNetworkOptionsArgs]{
		pane:     networkPaneId,
		resource: "the Network pane's settings: the layout direction and the highlighted vertex",
		digest:   networkOptionsDigest,
		get:      opGetNetwork,
		getSummary: "read what the Network pane last laid out from the edges and vertices CTEs: the counts and the cap, " +
			"the layout's error, the highlighted vertex, the lanes' state, and a page of the vertices and edges",
		set:        opSetNetworkOptions,
		setSummary: "set the Network pane's layout direction",
		gesture:    "the layout switch above the network",
		follows:    []string{"the pane lays the graph out again from its next frame; the CTEs are not rerun"},
		read:       networkReading,
		apply:      func(p *PlayApp, in SetNetworkOptionsArgs) error { return p.networkDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectNetworkNode, Version: 1,
		Summary: "highlight a vertex of the Network pane, or drop the highlight; its id becomes selection_key",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResNetwork}, Agents: true,
		Gesture: "clicking a vertex of the network",
		Follows: []string{"Live reruns a query that reads selection_key"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectNetworkNodeArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectNetworkNode(in, paneSignalWriter(call, networkPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	addPaneOps(s, paneOpsSpec[GraphviewReading, GetGraphviewArgs, SetGraphviewOptionsArgs]{
		pane:     graphviewPaneId,
		resource: "the Graphview pane's settings and its selection",
		digest:   graphviewOptionsDigest,
		get:      opGetGraphview,
		getSummary: "read what the Graphview pane last drew from the edges, vertices and graph_opts CTEs: the counts and caps, " +
			"each setting and whether the pane or the query decided it, the settle state, the selection and hover, " +
			"the notes on what it could not honour, the lanes' state, and a page of the vertices with graph metrics play computes",
		set: opSetGraphviewOptions,
		setSummary: "set the Graphview pane's layout, orientation, size channel, auras, hold and basemap switches, or pause; " +
			"auto hands a setting back to the query's graph_opts; fit, relayout and settle act once",
		gesture: "the size by combo and the fit, re-lay-out and settle buttons above the graph",
		follows: []string{"the pane draws with the new settings from its next frame; the CTEs are not rerun",
			"a fit or a layout moves the camera, and the pane publishes the gv_min/max signals once it rests"},
		read:  graphviewReading,
		apply: func(p *PlayApp, in SetGraphviewOptionsArgs) error { return p.graphviewDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectGraphviewNodes, Version: 1,
		Summary: "select vertices of the Graphview pane, or select none; gv_selection carries them and selection_key the last",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResGraphview}, Agents: true,
		Gesture: "clicking vertices of the graph",
		Follows: []string{"Live reruns a query that reads gv_selection or selection_key",
			"the seeded metrics (distance, relevance) measure from the new selection"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectGraphviewNodesArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectGraphviewNodes(in, paneSignalWriter(call, graphviewPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
