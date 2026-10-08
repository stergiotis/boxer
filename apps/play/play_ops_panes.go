package play

import (
	"strings"
)

// play_ops_panes.go: list_panes reads what each pane last drew (ADR-0270,
// update of 2026-10-05). The schema verdict (paneDraws) says whether a pane
// could draw what it is fed; a pane's own Status hook says what its last
// draw showed — its status line, or the reason the data, not the schema,
// left it with nothing to draw. Reads never draw or raise a pane: a lazy
// pane that is hidden keeps the status of its last draw, tagged with the
// result that draw was of.

// opsStatusMaxBytes bounds a status line in list_panes; a line quoting a
// failed lane's error can run long.
const opsStatusMaxBytes = 512

// paneDrawnMark is what renderTabBody records each time a pane's body
// draws: the frame, the result it was handed, and whether a result was
// there to draw at all.
type paneDrawnMark struct {
	frame  int
	result ResultID
	fed    bool
}

// markPaneDrawn records that the pane's body drew this frame from f.
func (inst *PlayApp) markPaneDrawn(pane string, f *TabFrame) {
	if inst.paneDrawn == nil {
		inst.paneDrawn = make(map[string]paneDrawnMark, 8)
	}
	inst.paneDrawn[pane] = paneDrawnMark{frame: inst.frame, result: f.Result, fed: f.Rec != nil}
}

// paneVisible reports whether the pane's body drew in the last frame: a
// lazy pane only while its tab is in front (its gate is live), any other
// pane whenever the dock drew it.
func (inst *PlayApp) paneVisible(spec *TabSpec) (visible bool) {
	if spec.Lazy {
		pane := inst.lazyPanes[spec.DockID]
		return pane != nil && pane.Live()
	}
	mark, ok := inst.paneDrawn[spec.ID]
	return ok && mark.frame+1 == inst.frame
}

// tabZoneName is the zone as a layout knob spells it.
func tabZoneName(z TabZoneE) (name string) {
	for n, v := range TabZoneNames {
		if v == z {
			return n
		}
	}
	return ""
}

// paneStatusState applies a pane's last draw to its list_panes row. The
// status is reported only while the schema verdict lets the pane draw:
// when it does not, the driver was not reached and its state describes an
// earlier result. The data-level reject turns Draws to no only for a draw
// of the last frame, so a hidden pane is not judged on a stale draw.
func (inst *PlayApp) paneStatusState(spec *TabSpec, ps *PaneState) {
	ps.Zone = tabZoneName(spec.Zone)
	ps.Lazy = spec.Lazy
	ps.Visible = inst.paneVisible(spec)
	if spec.Status == nil || ps.Draws == PaneDrawNo {
		return
	}
	mark, drew := inst.paneDrawn[spec.ID]
	if !drew || !(mark.fed || spec.Frameless) {
		return
	}
	line, reject := spec.Status()
	ps.Status = truncateBytes(strings.TrimSpace(line), opsStatusMaxBytes)
	if !spec.Frameless {
		// A frameless pane draws its own CTE lanes; the frame's result id
		// would name a result it did not draw.
		ps.StatusOf = uint64(mark.result)
	}
	if reject != "" && mark.frame+1 == inst.frame {
		ps.Draws = PaneDrawNo
		ps.Reason = truncateBytes(reject, opsStatusMaxBytes)
	}
}

// attachPaneStatus sets the Status hook of the built-in panes whose driver
// keeps a status line. Map has none in this form yet.
func attachPaneStatus(inst *PlayApp, spec *TabSpec) {
	switch spec.ID {
	case "chart":
		spec.Status = func() (string, string) { return inst.chartDriver.paneStatus() }
	case "timeline":
		spec.Status = func() (string, string) { return inst.timeline.paneStatus() }
	case "dist":
		spec.Status = func() (string, string) { return inst.distDriver.paneStatus() }
	case "series":
		spec.Status = func() (string, string) { return inst.seriesDriver.paneStatus() }
	case "treemap":
		spec.Status = func() (string, string) { return inst.treemapDriver.paneStatus() }
	case "icicle":
		spec.Status = func() (string, string) { return inst.icicleDriver.paneStatus() }
	case "world":
		spec.Status = func() (string, string) { return inst.worldDriver.drawnStatus, inst.worldDriver.drawnReject }
	case "kanban":
		spec.Status = func() (string, string) { return inst.kanbanDriver.statusLine(), "" }
	case "chat":
		spec.Status = func() (string, string) { return inst.chatDriver.statusLine(), "" }
	case "files":
		spec.Status = func() (string, string) { return inst.filesDriver.statusLine(), "" }
	case "network":
		spec.Status = func() (string, string) { return inst.networkDriver.statusLine(), "" }
	case "graphview":
		spec.Status = func() (string, string) { return inst.graphviewDriver.statusLine(), "" }
	case "sankey":
		spec.Status = func() (string, string) { return inst.sankeyDriver.paneStatus() }
	case "vectorfield":
		spec.Status = func() (string, string) { return inst.vectorFieldDriver.drawnStatus, "" }
	case "map":
		spec.Status = func() (string, string) { return inst.mapDriver.statusLine(), "" }
	}
}

// paneStatus is the Chart's last draw: the fold's reject, an empty result,
// or the status line.
func (inst *ChartDriver) paneStatus() (line string, reject string) {
	switch {
	case !inst.foldedOnce:
	case inst.foldErr != "":
		reject = inst.foldErr
	case inst.points == 0:
		reject = "The query returned no rows, so there is nothing to chart."
	default:
		line = inst.statusLine()
	}
	return
}

// paneStatus is the Distribution's last draw.
func (inst *DistDriver) paneStatus() (line string, reject string) {
	switch {
	case inst.foldErr != "":
		reject = inst.foldErr
	case len(inst.series) == 0:
		reject = "The query returned no rows, so there is no distribution to draw."
	default:
		line = inst.statusLine()
	}
	return
}

// paneStatus is the Series' last draw.
func (inst *SeriesDriver) paneStatus() (line string, reject string) {
	switch {
	case inst.foldErr != "":
		reject = inst.foldErr
	case len(inst.t) == 0:
		reject = "The query returned no timed rows, so there is no series to draw."
	default:
		line = inst.statusLine()
	}
	return
}

// paneStatus is the Treemap's last draw.
func (inst *treemapDriver) paneStatus() (line string, reject string) {
	line = inst.statusLine()
	if inst.tree.Len() == 0 || inst.root == nil {
		reject = "No cells: every row was missing a path and an id, or carried a value that is not a finite, non-negative number."
	}
	return
}

// paneStatus is the Icicle's last draw.
func (inst *IcicleDriver) paneStatus() (line string, reject string) {
	line = inst.statusLine()
	switch {
	case inst.tree.Len() == 0:
		reject = "No frames: every row was missing a path and an id, or carried a value that is not a finite, non-negative number."
	case inst.layoutErr != nil:
		reject = "the rows cannot be laid out as a hierarchy: " + icicleReason(inst.layoutErr)
	}
	return
}

// paneStatus is the Sankey's last draw.
func (inst *SankeyDriver) paneStatus() (line string, reject string) {
	line = inst.statusLine()
	if inst.layoutErr != nil {
		reject = "the flows cannot be laid out as a diagram: " + sankeyReason(inst.layoutErr)
	}
	return
}

// paneOperations names, per pane id, the operations that read the pane or
// drive it, reads first, so list_panes can point an agent from a pane to
// them. A pane with none of its own is read through the result reads
// (describe_result, sample_rows) and driven by set_sql and set_signal.
var paneOperations = map[string][]string{
	"editor":          {opGetState, opSetSql, opSetParam, opRun, opCancelRun, opSetRunOptions},
	"history":         {opListHistory},
	"docs":            {opLookupDocs},
	"preview":         {opValidateSql},
	"flow":            {opSqlFlow, opExplainSql},
	"passes":          {opTraceRewrite},
	"diagnostics":     {opGetDiagnostics},
	"snippets":        {opListSnippets, opReadSnippet},
	"vocabulary":      {opListFunctions, opEndpointFunctions},
	"completion":      {opCompleteSql},
	"glosses":         {opListGlosses},
	"schema":          {opListTables, opDescribeTable},
	"graph":           {opGetQueryGraph, opObserveNode, opBindPane, opSetSignal, opDeleteSignal},
	"detail":          {opGetDetail},
	tablePaneId:       {opGetTable, opSetTableOptions},
	chartPaneId:       {opGetChart, opSetChartOptions},
	distPaneId:        {opGetDist, opSetDistOptions},
	seriesPaneId:      {opGetSeries, opSetSeriesOptions},
	timelinePaneId:    {opGetTimeline, opSetTimelineOptions, opSetTimelineWindow},
	treemapPaneId:     {opGetTreemap, opSetTreemapOptions, opSelectTreemapNode},
	iciclePaneId:      {opGetIcicle, opSetIcicleOptions, opSelectIcicleFrame},
	kanbanPaneId:      {opGetKanban},
	cardsPaneId:       {opGetCards, opSetCardsOptions},
	worldPaneId:       {opGetWorld, opSetWorldOptions},
	vectorfieldPaneId: {opGetVectorfield, opSetVectorfieldView},
	networkPaneId:     {opGetNetwork, opSetNetworkOptions, opSelectNetworkNode},
	graphviewPaneId:   {opGetGraphview, opSetGraphviewOptions, opSelectGraphviewNodes},
	sankeyPaneId:      {opGetSankey, opSetSankeyOptions, opSelectSankeyNode},
	filesPaneId:       {opGetFiles, opSetFilesOptions, opSelectFilesPath},
	chatPaneId:        {opGetChatPane},
	mapPaneId:         {opGetMap, opSetMapView, opSetMapOptions},
	projectionPaneId:  {opGetProjection, opExplainClusters, opGetArchetypes, opComputeProjection, opCancelProjection, opPublishProjection},
}
