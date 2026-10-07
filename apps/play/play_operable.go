package play

// What an operable bundle view offers agents (ADR-0288 (proposed) §SD8): a
// subset of play's catalog, served over a play embedded in another app's
// window. The subset and the panes its operations belong to are declared
// here, beside the catalog they are taken from, so a pane operation added to
// play is either offered or excluded on purpose — TestOperableSubsetCovers
// every pane operation fails until it is one or the other.

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// OperableOperation is one operation an operable bundle view offers: play's
// spec, and the pane it belongs to, empty for one that belongs to none.
type OperableOperation struct {
	Spec app.OperationSpec
	Pane string
}

// operableCommon are the operations that belong to no pane: reading the
// window's state, its result and history, running, and its parameters.
// Changing the SQL, binding datasets and publishing are not offered — the
// document is the publisher's — nor are the authoring tools.
var operableCommon = []string{
	opGetState, opDescribeResult, opSampleRows, opListHistory, opListPanes,
	opRun, opCancelRun, opSetParam,
}

// operablePanes are, per pane, the operations an operable view offers for
// it when its document shows it: the pane's reading, its options and its
// selections.
var operablePanes = map[string][]string{
	tablePaneId:       {opGetTable, opSetTableOptions},
	projectionPaneId:  {opGetProjection},
	timelinePaneId:    {opGetTimeline, opSetTimelineOptions, opSetTimelineWindow},
	mapPaneId:         {opGetMap, opSetMapOptions, opSetMapView},
	vectorfieldPaneId: {opGetVectorfield, opSetVectorfieldView},
	worldPaneId:       {opGetWorld, opSetWorldOptions},
	kanbanPaneId:      {opGetKanban},
	chatPaneId:        {opGetChatPane},
	cardsPaneId:       {opGetCards, opSetCardsOptions},
	networkPaneId:     {opGetNetwork, opSetNetworkOptions, opSelectNetworkNode},
	graphviewPaneId:   {opGetGraphview, opSetGraphviewOptions, opSelectGraphviewNodes},
	sankeyPaneId:      {opGetSankey, opSetSankeyOptions, opSelectSankeyNode},
	distPaneId:        {opGetDist, opSetDistOptions},
	iciclePaneId:      {opGetIcicle, opSetIcicleOptions, opSelectIcicleFrame},
	seriesPaneId:      {opGetSeries, opSetSeriesOptions},
	treemapPaneId:     {opGetTreemap, opSetTreemapOptions, opSelectTreemapNode},
	chartPaneId:       {opGetChart, opSetChartOptions},
	filesPaneId:       {opGetFiles, opSetFilesOptions, opSelectFilesPath},
}

// operableExcluded are pane operations an operable view does not offer, and
// why: the projection's computation is authoring work with its own run and
// its own publish, and the detail pane is the window's side panel, which a
// bundle view does not show.
var operableExcluded = []string{
	opComputeProjection, opCancelProjection, opExplainClusters, opPublishProjection, opGetDetail,
}

// OperableOperations is the subset of play's catalog an operable bundle view
// offers, with play's own specs, in catalog order.
func OperableOperations() (ops []OperableOperation) {
	paneOf := make(map[string]string, 64)
	for pane, names := range operablePanes {
		for _, n := range names {
			paneOf[n] = pane
		}
	}
	for _, spec := range playOps.Catalog().Operations {
		pane, onPane := paneOf[spec.Name]
		if onPane || slices.Contains(operableCommon, spec.Name) {
			ops = append(ops, OperableOperation{Spec: spec, Pane: pane})
		}
	}
	return
}

// OperableResources are the resources the operable subset reads and writes,
// with play's descriptions.
func OperableResources() (resources []app.ResourceSpec) {
	used := make(map[string]bool, 32)
	for _, o := range OperableOperations() {
		for _, r := range slices.Concat(o.Spec.Reads, o.Spec.Writes) {
			used[r] = true
		}
	}
	for _, r := range playOps.Catalog().Resources {
		if used[r.Name] {
			resources = append(resources, r)
		}
	}
	return
}

// ServedOperations serves play's catalog over this instance, for a receiver
// that offers it to agents through its own catalog (ADR-0288 (proposed)
// §SD8). The handler is made once and kept: its resources' values are read
// across frames. A receiver that serves it frames the instance with
// FrameServed.
func (inst *PlayApp) ServedOperations() (h app.OperationsHandlerI) {
	if inst.served == nil {
		inst.served = playOps.Bind(&PlayLauncher{inner: inst})
	}
	return inst.served
}

// FrameServed is Frame for an instance whose operations a receiver serves:
// it judges the agent mark before the frame and settles it after, as a play
// window's own frame does (ADR-0270 §SD3), so a run an agent asked for is
// stamped as the task's and the person's edit takes the window back.
func (inst *PlayApp) FrameServed(ctx app.FrameContextI) (err error) {
	inst.checkAgentMark()
	err = inst.Frame(ctx)
	inst.settleAgentMark()
	return
}

// WorkPending reports work that needs this instance's frames to finish: a
// run requested for the next frame, or the main result still loading. A
// receiver that culls an embedded play keeps drawing it while this holds.
func (inst *PlayApp) WorkPending() (pending bool) {
	return inst.requestRun || inst.graph.MainLoading()
}

// SetDatasetOrigin records that the name local, under which a dataset is
// bound, stands for the dataset published as alias in bundle (empty when it
// is in none), so the agent limits judge a run by the names a grant lists
// it by, never by local (ADR-0288 (proposed) §SD3).
func (inst *PlayApp) SetDatasetOrigin(local string, alias string, bundle string) {
	if inst.client != nil {
		inst.client.setDatasetOrigin(local, alias, bundle)
	}
}
