package play

import (
	"maps"
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// play_ops_pane_views.go: the plumbing a pane's own operations share
// (ADR-0270, update of 2026-10-05). Each result pane that an agent reads
// gets a bespoke pair — get_<pane> (query) returns what the pane last drew,
// set_<pane>_options (command, document effect) sets its options and writes
// a resource named after the pane — rather than one generic op over every
// pane. What they share is here:
//
//   - PaneDraw, the header every get_<pane> result opens with, and
//     paneDrawOf, which builds it from list_panes' row and refuses a pane
//     that has not drawn ("not drawn: show_pane <id>"). A read never draws,
//     folds or raises a pane.
//   - paneViewCache, which holds each pane's copied fold keyed by the
//     driver's fold generation, so snapshotPlay copies a fold once and every
//     later snapshot shares the immutable copy.
//   - addPaneOps, which declares the pair and the resource, so each pane's
//     file names only its own types and functions.
//
// A pane's options are set, never toggled; a pointer field left out keeps
// the person's setting. The pane's in-frame buttons that change an option go
// through the command via playGesture, so the person's pick is logged as
// theirs and pauses a task that read the pane (ADR-0270 §SD6). Options bound
// with SendRespVal land at the write-back and need no routing: the
// resource's digest reads them.

// PaneDraw is the header of every get_<pane> reading: which draw it is of
// and whether that draw showed anything.
type PaneDraw struct {
	ResultId uint64 `desc:"the result the pane last drew; compare with get_state's result id, since a pane keeps its last draw until it draws again"`
	Node     string `json:",omitzero" desc:"the split node feeding the pane, when it is not the one the main result comes from"`
	// Status is the pane's own line, quoted: it can carry the data's values.
	Status string `json:",omitzero" desc:"the pane's status line as drawn"`
	// CannotDraw is the schema's or the fold's reason; the pane's own
	// fields of the reading are then left empty.
	CannotDraw string `json:",omitzero" desc:"why the pane drew nothing from its result; the reading's other fields then describe only the pane's settings"`
}

// paneViewCache is the copied folds of the panes an agent reads, each with
// the fold generation it was copied at. Render-goroutine state; the copies
// themselves are never mutated once built, so a snapshot shares them.
type paneViewCache struct {
	chartGen  uint64
	chart     *chartFoldView
	distGen   uint64
	dist      *distFoldView
	seriesGen uint64
	series    *seriesFoldView
	// seriesOverlayKey identifies the score lane the overlay copy was taken
	// of; the overlays are folded every frame, the score lane only when
	// the series' input lanes land.
	seriesOverlayKey seriesOverlayKey
	seriesOverlay    *seriesOverlayFold
	// The Table's column captions per schema and gloss resolution, and its
	// page as drawn per page, sort and resolution.
	tableColumns *tableColumnsView
	tablePageKey tablePageKey
	tablePage    *tablePageView
	// The gloss catalog's reading, built once, and the last column
	// resolution's copy (play_ops_glosses.go).
	glossCatalog *GlossCatalog
	glossColumns *glossColumnsOps
	// What the completion probes had landed, copied when completionProbeGen
	// moves (play_ops_completion.go).
	completionGen uint64
	completion    *completionOpsCopy
	// get_query_graph's reading, rebuilt when its inputs move.
	queryGraphKey queryGraphKey
	queryGraph    *QueryGraph
}

// paneViewsSnap is what the get_<pane> reads see: the frame the snapshot
// was taken in, every pane's last draw mark, and each pane's view.
type paneViewsSnap struct {
	frame    int
	drawn    map[string]paneDrawnMark
	chart    chartOpsView
	dist     distOpsView
	series   seriesOpsView
	timeline timelineOpsView
	treemap  treemapOpsView
	icicle   icicleOpsView
	kanban   kanbanOpsView
	cards    cardsOpsView
	world    worldOpsView
	// The frameless panes draw their own CTE lanes, not the frame.
	network     networkOpsView
	graphview   graphviewOpsView
	sankey      sankeyOpsView
	vectorfield vectorfieldOpsView
	table       tableOpsView
	files       filesOpsView
	chat        chatOpsView
	// The tool panes draw no result of their own.
	mapv mapOpsView
}

// snapshotPaneViews copies what the get_<pane> reads need, on the render
// goroutine. A pane's fold is copied only when it drew and its driver
// folded since the last copy.
func snapshotPaneViews(p *PlayApp) (out paneViewsSnap) {
	out.frame = p.frame
	out.drawn = maps.Clone(p.paneDrawn)
	if _, ok := p.paneDrawn[chartPaneId]; ok && p.chartDriver != nil {
		out.chart = p.chartView()
	}
	if _, ok := p.paneDrawn[distPaneId]; ok && p.distDriver != nil {
		out.dist = p.distView()
	}
	if _, ok := p.paneDrawn[seriesPaneId]; ok && p.seriesDriver != nil {
		out.series = p.seriesView()
	}
	// The panes below hand their reads the fold itself, which a rebuild
	// replaces rather than edits; the copy is a handful of fields.
	if _, ok := p.paneDrawn[timelinePaneId]; ok && p.timeline != nil {
		out.timeline = p.timelineView()
	}
	if _, ok := p.paneDrawn[treemapPaneId]; ok && p.treemapDriver != nil {
		out.treemap = p.treemapView()
	}
	if _, ok := p.paneDrawn[iciclePaneId]; ok && p.icicleDriver != nil {
		out.icicle = p.icicleView()
	}
	if _, ok := p.paneDrawn[kanbanPaneId]; ok && p.kanbanDriver != nil {
		out.kanban = p.kanbanView()
	}
	if _, ok := p.paneDrawn[cardsPaneId]; ok && p.cardgridDriver != nil {
		out.cards = p.cardsView()
	}
	if _, ok := p.paneDrawn[worldPaneId]; ok && p.worldDriver != nil {
		out.world = p.worldView()
	}
	if _, ok := p.paneDrawn[networkPaneId]; ok && p.networkDriver != nil {
		out.network = p.networkView()
	}
	if _, ok := p.paneDrawn[graphviewPaneId]; ok && p.graphviewDriver != nil {
		out.graphview = p.graphviewView()
	}
	if _, ok := p.paneDrawn[sankeyPaneId]; ok && p.sankeyDriver != nil {
		out.sankey = p.sankeyView()
	}
	if _, ok := p.paneDrawn[vectorfieldPaneId]; ok && p.vectorFieldDriver != nil {
		out.vectorfield = p.vectorfieldView()
	}
	if mark, ok := p.paneDrawn[tablePaneId]; ok {
		out.table = p.tableView(mark)
	}
	if _, ok := p.paneDrawn[filesPaneId]; ok && p.filesDriver != nil {
		out.files = p.filesView()
	}
	if _, ok := p.paneDrawn[chatPaneId]; ok && p.chatDriver != nil {
		out.chat = p.chatView()
	}
	if _, ok := p.paneDrawn[mapPaneId]; ok && p.mapDriver != nil {
		out.mapv = p.mapView()
	}
	return
}

// paneDrawOf is the header of pane's reading, from its list_panes row and
// its last draw. readable is false when the pane's draw showed nothing of
// its own (CannotDraw says why). A pane that has not drawn, or a lazy pane
// whose tab is not in front, is refused: its fold is either missing or of
// an earlier result, and the read does not raise it.
func paneDrawOf(sn *opsSnap, pane string) (d PaneDraw, readable bool, err error) {
	if !sn.mounted {
		err = app.RefuseOperation("the window has not mounted")
		return
	}
	var row *PaneState
	for i := range sn.panes.Panes {
		if sn.panes.Panes[i].Pane == pane {
			row = &sn.panes.Panes[i]
			break
		}
	}
	if row == nil {
		err = app.RefuseOperation("this window has no " + pane + " pane")
		return
	}
	mark, drew := sn.paneViews.drawn[pane]
	if !drew || (row.Lazy && !row.Visible) {
		err = app.RefuseOperation("not drawn: show_pane " + pane + " raises the pane, and its next frame draws it")
		return
	}
	// A frameless pane draws its own CTE lanes, not the frame it is
	// handed, so the frame's result neither gates nor names its draw; a
	// tool pane (the Map, Experiments) is no panel and draws without one.
	_, frameless := sn.results.frameless[pane]
	frameless = frameless || !row.Panel
	if !mark.fed && !frameless {
		err = app.RefuseOperation("the " + pane + " pane had no result to draw: run the buffer, or bind_pane it to a node that has run")
		return
	}
	d = PaneDraw{ResultId: uint64(mark.result), Node: row.Node, Status: row.Status}
	if frameless {
		d.ResultId, d.Node = 0, ""
	}
	if row.Draws == PaneDrawNo {
		d.CannotDraw = row.Reason
		return
	}
	readable = true
	return
}

// paneOpsSpec is one pane's pair of operations, declared by addPaneOps.
type paneOpsSpec[V any, In any, Set any] struct {
	pane string
	// resource is the pane's resource description; digest reads the
	// pane's option fields, render-goroutine side.
	resource string
	digest   func(p *PlayApp) string
	// get and set name the operations and say what they do.
	get, getSummary string
	set, setSummary string
	gesture         string
	follows         []string
	read            func(sn *opsSnap, in In) (V, error)
	apply           func(p *PlayApp, in Set) error
}

// addPaneOps declares a pane's resource, its get_<pane> query and its
// set_<pane>_options command. The read is untrusted: a pane's reading
// quotes labels, column names and values of the data. The command is of
// document effect (ADR-0269 §SD5: pane options are authored state).
//
// A pane with no options of its own (Kanban) leaves set empty: it then has
// no resource and no command, and its read reads only the result and the
// panes.
func addPaneOps[V any, In any, Set any](s *appops.Set[*PlayLauncher, opsSnap], spec paneOpsSpec[V, In, Set]) {
	reads := []string{opsResResult, opsResPanes}
	if spec.set != "" {
		reads = append([]string{spec.pane}, reads...)
		s.Resource(spec.pane, spec.resource, func(inst *PlayLauncher) any {
			if inst.inner == nil {
				return ""
			}
			return spec.digest(inst.inner)
		})
	}
	appops.Query(s, app.OperationSpec{Name: spec.get, Version: 1, Summary: spec.getSummary,
		Reads: reads, Agents: true, Untrusted: true},
		func(sn opsSnap, in In) (V, error) { return spec.read(&sn, in) })
	if spec.set == "" {
		return
	}
	appops.Command(s, app.OperationSpec{Name: spec.set, Version: 1, Summary: spec.setSummary,
		Effect: app.OperationEffectDocument, Writes: []string{spec.pane}, Agents: true,
		Gesture: spec.gesture, Follows: spec.follows},
		func(inst *PlayLauncher, call app.OperationCall, in Set) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			return appops.None{}, spec.apply(inst.inner, in)
		})
}

// routePaneOptions points the panes' in-frame option buttons at their
// commands. Where no host serves play's catalog, playGesture applies the
// change directly.
func (inst *PlayApp) routePaneOptions() {
	inst.chartDriver.onOptions = func(in SetChartOptionsArgs) {
		playGesture(inst, opSetChartOptions, in, func() { _ = inst.chartDriver.setOptions(in) })
	}
	inst.distDriver.onOptions = func(in SetDistOptionsArgs) {
		playGesture(inst, opSetDistOptions, in, func() { _ = inst.distDriver.setOptions(in) })
	}
	inst.seriesDriver.onOptions = func(in SetSeriesOptionsArgs) {
		playGesture(inst, opSetSeriesOptions, in, func() { _ = inst.seriesDriver.setOptions(in) })
	}
	inst.cardgridDriver.onOptions = func(in SetCardsOptionsArgs) {
		playGesture(inst, opSetCardsOptions, in, func() { _ = inst.cardgridDriver.setOptions(in) })
	}
	// The brush and the pins write signals: as the pane, as they always
	// did, when the person made them.
	inst.timeline.onWindow = func(in SetTimelineWindowArgs) {
		playGesture(inst, opSetTimelineWindow, in, func() { _ = inst.setTimelineWindow(in, timelinePaneId) })
	}
	inst.treemapDriver.onSelect = func(in SelectTreemapNodeArgs) {
		playGesture(inst, opSelectTreemapNode, in, func() { _ = inst.selectTreemapNode(in, treemapPaneId) })
	}
	inst.icicleDriver.onSelect = func(in SelectIcicleFrameArgs) {
		playGesture(inst, opSelectIcicleFrame, in, func() { _ = inst.selectIcicleFrame(in, iciclePaneId) })
	}
	inst.worldDriver.onOptions = func(in SetWorldOptionsArgs) {
		playGesture(inst, opSetWorldOptions, in, func() { _ = inst.worldDriver.setOptions(in) })
	}
	inst.networkDriver.onSelect = func(in SelectNetworkNodeArgs) {
		playGesture(inst, opSelectNetworkNode, in, func() { _ = inst.selectNetworkNode(in, networkPaneId) })
	}
	inst.graphviewDriver.onOptions = func(in SetGraphviewOptionsArgs) {
		playGesture(inst, opSetGraphviewOptions, in, func() { _ = inst.graphviewDriver.setOptions(in) })
	}
	inst.graphviewDriver.onSelect = func(in SelectGraphviewNodesArgs) {
		playGesture(inst, opSelectGraphviewNodes, in, func() { _ = inst.selectGraphviewNodes(in, graphviewPaneId) })
	}
	inst.sankeyDriver.onSelect = func(in SelectSankeyNodeArgs) {
		playGesture(inst, opSelectSankeyNode, in, func() { _ = inst.selectSankeyNode(in, sankeyPaneId) })
	}
	inst.filesDriver.onOptions = func(in SetFilesOptionsArgs) {
		playGesture(inst, opSetFilesOptions, in, func() { _ = inst.filesDriver.setOptions(in) })
	}
	inst.filesDriver.onSelect = func(in SelectFilesPathArgs) {
		playGesture(inst, opSelectFilesPath, in, func() { _ = inst.selectFilesPath(in, filesPaneId) })
	}
	inst.mapDriver.onOptions = func(in SetMapOptionsArgs) {
		playGesture(inst, opSetMapOptions, in, func() { _ = inst.setMapOptions(in, signalWriterMap, false) })
	}
}

// opsLabelMaxBytes bounds one label a pane reading quotes: a category, a
// lane, a series.
const opsLabelMaxBytes = 64

// opsLabel is a data-authored label as a reading quotes it.
func opsLabel(s string) string { return truncateBytes(s, opsLabelMaxBytes) }

// finite is v when it is a finite number, nil otherwise: a reading's JSON
// carries no NaN or infinity.
func finite(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// noOptionsRefusal is a set_<pane>_options call that names no option.
func noOptionsRefusal(pane string, names ...string) error {
	return app.RefuseOperation("name an option of the " + pane + " pane to set: " + strings.Join(names, ", "))
}
