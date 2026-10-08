package play

// The Projection pane as an agent reads and drives it (ADR-0270, update of
// 2026-10-03): compute the neighbour graph and its clusters, read how the
// run went with the clusters and the layout, and read "why these clusters"
// — by the features the clustering used or by the card's attributes. The
// readings are the pane's own text, so a model sees what the person sees.

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const (
	opComputeProjection = "compute_projection"
	opGetProjection     = "get_projection"
	opExplainClusters   = "explain_clusters"

	opsResProjection = "projection"

	// projectionPaneId is the Projection pane's slug.
	projectionPaneId = "projection"

	// Bounds on get_projection's points.
	opsProjectionDefaultPoints = 100
	opsProjectionMaxPoints     = 1000
	// projectionPositionsEvery is how often the snapshot copies the layout
	// while it is still moving; a settled layout is copied once.
	projectionPositionsEvery = 250 * time.Millisecond
)

// ComputeProjectionArgs is compute_projection's argument. A field left out
// keeps the pane's setting.
type ComputeProjectionArgs struct {
	Neighbours int32  `json:",omitzero" desc:"neighbours per row in the graph, 2 to 50; the pane's setting when left out (15 by default)"`
	MinCluster int32  `json:",omitzero" desc:"the smallest group HDBSCAN calls a cluster, 2 to 100; the pane's setting when left out (5 by default)"`
	Features   string `json:",omitzero" desc:"what rows are compared by: structure (which sections and attributes it has; the default), shape (how big and skewed a record is) or components (which registered components it carries; facts-shaped results only); the pane's setting when left out"`
}

// ProjectionArgs is get_projection's argument.
type ProjectionArgs struct {
	Points int32 `json:",omitzero" desc:"how many points to return with their cluster and layout position, at most 1000; 100 when left out, -1 for none"`
	Offset int32 `json:",omitzero" desc:"the first point to return"`
}

// ClusterSize is one cluster's size.
type ClusterSize struct {
	Cluster int32 `desc:"the cluster, numbered from 1 as the pane and explain_clusters number it"`
	Rows    int32 `desc:"how many projected rows it holds"`
}

// ProjectionPoint is one projected row.
type ProjectionPoint struct {
	Row         int64   `desc:"the row of the result, from 0"`
	Cluster     int32   `desc:"its cluster, from 1; -1 for noise"`
	Probability float64 `desc:"how firmly HDBSCAN places it in its cluster, 0 to 1"`
	X           float32 `desc:"its layout position, in the layout's own units"`
	Y           float32 `desc:"its layout position, in the layout's own units"`
}

// ProjectionState is get_projection's result.
type ProjectionState struct {
	Status string `desc:"idle, extracting, running, done, failed, cancelling or cancelled"`
	Error  string `json:",omitzero" desc:"why the run failed"`
	// CannotDraw is the pane's own reason it cannot draw the result.
	CannotDraw string        `json:",omitzero" desc:"why the pane cannot draw the result it is fed; compute_projection refuses while it says so"`
	Rows       int64         `desc:"rows in the result the pane is fed"`
	Projected  int32         `desc:"rows the run projected; fewer than rows when the run sampled"`
	Neighbours int32         `desc:"the run's neighbours per row, or the pane's setting before a run"`
	MinCluster int32         `desc:"the run's minimum cluster size, or the pane's setting before a run"`
	Features   string        `desc:"the run's feature set, or the pane's setting before a run"`
	Clusters   []ClusterSize `desc:"the clusters with their sizes; explain_clusters says what sets them apart, get_archetypes what each holds and which rows break it"`
	Noise      int32         `desc:"projected rows in no cluster"`
	// Summary is the pane's status line.
	Summary string            `json:",omitzero" desc:"the pane's status line: the graph, the clustering and how far the layout has got"`
	Layout  string            `desc:"not drawn (raise the pane with show_pane), settling, settled, frozen or paused; positions are read from the drawn layout"`
	Points  []ProjectionPoint `desc:"the requested page of points"`
	// PointsTotal is how many points there are to page through.
	PointsTotal int32 `desc:"how many points there are to page through"`
	// RunBy names who asked for the run, which cancel_projection checks.
	RunBy string `json:",omitzero" desc:"who asked for the run: person, or task:<id>; cancel_projection stops only the calling task's own"`
	// The publish's outcome (publish_projection).
	Publishing   bool   `json:",omitzero" desc:"true while a publish_projection is in flight"`
	Published    string `json:",omitzero" desc:"the last publish: each dataset's handle, rows and revision; the window binds them as keelson('projection') and keelson('projection_rules')"`
	PublishError string `json:",omitzero" desc:"why the last publish failed"`
}

// ExplainClustersArgs is explain_clusters' argument.
type ExplainClustersArgs struct {
	Reading   string `json:",omitzero" desc:"features (the matrix the clustering used; the default) or attributes (the card's sections, values and components, as subgroups)"`
	Depth     int32  `json:",omitzero" desc:"for the features reading, the rule depth, 1 to 6; the pane's setting when left out (3 by default)"`
	Partition bool   `json:",omitzero" desc:"for the features reading, read the one partition tree instead of one tree per cluster against the rest"`
	Cluster   int32  `json:",omitzero" desc:"one cluster, from 1; every cluster when left out"`
}

// ClusterRule is one cluster's explanation.
type ClusterRule struct {
	Cluster int32  `desc:"the cluster, from 1"`
	Rows    int32  `desc:"its rows"`
	Rule    string `desc:"a SQL predicate over the result's columns that picks the cluster's rows, or why there is none"`
	Fit     string `json:",omitzero" desc:"how well the rule picks the cluster: precision and recall, and WRAcc for an attribute subgroup"`
	// Distinguishing is the strongest contrasts against the other rows.
	Distinguishing string `desc:"what sets the cluster apart from the other rows: the strongest features with their medians or shares, or the over- and under-represented attributes with their lift"`
}

// ClusterExplanation is explain_clusters' result.
type ClusterExplanation struct {
	Reading  string        `desc:"features or attributes"`
	Summary  string        `desc:"how the reading was made and how much of the clustering its rules reproduce"`
	Clusters []ClusterRule `desc:"one explanation per cluster"`
}

// projectionOpsSnap is what the projection operations read, copied on the
// render goroutine.
type projectionOpsSnap struct {
	snap       projectorSnapshot
	neighbours int
	minCluster int
	features   projectionFeatureSetE
	depth      int
	perCluster bool
	summary    string
	layout     string
	x, y       []float32
	rows       int64
	cannotDraw string
	runBy      string
	publishing bool
	published  string
	publishErr string
}

// snapshotProjection copies the projector for the operations. The layout
// lives in the widget: it is copied when a run lands, every
// projectionPositionsEvery while it moves, and once more when it settles.
func snapshotProjection(p *PlayApp, panes PanesState) (out projectionOpsSnap) {
	pj := p.projector
	if pj == nil {
		return
	}
	out.snap = pj.Snapshot()
	out.neighbours, out.minCluster, out.features = pj.params.K, pj.params.MinClusterSize, pj.params.FeatureSet
	out.depth, out.perCluster = pj.explainDepth, pj.explainPerCluster
	for _, pn := range panes.Panes {
		if pn.Pane == projectionPaneId && pn.Draws == PaneDrawNo {
			out.cannotDraw = pn.Reason
		}
	}
	out.rows = p.paneFedRows(projectionPaneId)
	if out.snap.status != projectorStatusIdle {
		out.runBy = "person"
		if pj.runTask != "" {
			out.runBy = "task:" + pj.runTask
		}
	}
	if p.projPublish != nil {
		var perr error
		out.publishing, out.published, _, perr = p.projPublish.status()
		if perr != nil {
			out.publishErr = perr.Error()
		}
	}
	res := out.snap.result
	if res == nil {
		return
	}
	out.neighbours, out.minCluster, out.features = res.params.K, res.params.MinClusterSize, res.params.FeatureSet
	out.summary = pj.statusLine(res)
	if pj.builtVersion != out.snap.version {
		out.layout = "not drawn"
		return
	}
	m := pj.view.Metrics()
	switch {
	case pj.paused:
		out.layout = "paused"
	case pj.frozen:
		out.layout = "frozen"
	case pj.view.IsSettled():
		out.layout = "settled"
	default:
		out.layout = "settling"
	}
	stale := pj.posVersion != out.snap.version || len(pj.posX) != len(res.rows) ||
		(!pj.posSettled && time.Since(pj.posAt) >= projectionPositionsEvery)
	if stale && m.Steps > 0 {
		_, x, y := pj.view.PositionColumns(nil, nil, nil)
		pj.posX, pj.posY = x, y
		pj.posVersion, pj.posAt, pj.posSettled = out.snap.version, time.Now(), out.layout == "settled"
	}
	if pj.posVersion == out.snap.version {
		out.x, out.y = pj.posX, pj.posY
	}
	return
}

// projectionState is get_projection's reading of a snapshot.
func projectionState(ps projectionOpsSnap, in ProjectionArgs) (out ProjectionState, err error) {
	out = ProjectionState{Status: ps.snap.status.String(), CannotDraw: ps.cannotDraw, Rows: ps.rows,
		Neighbours: int32(ps.neighbours), MinCluster: int32(ps.minCluster), Features: ps.features.String(), Layout: "not drawn",
		RunBy: ps.runBy, Publishing: ps.publishing, Published: ps.published, PublishError: ps.publishErr}
	if ps.snap.err != nil {
		out.Error = ps.snap.err.Error()
	}
	res := ps.snap.result
	if res == nil {
		return
	}
	out.Projected, out.Summary, out.Layout = int32(len(res.rows)), ps.summary, ps.layout
	sizes := make([]int, res.clusters.NumClusters)
	noise := 0
	for _, lb := range res.clusters.Label {
		if lb < 0 {
			noise++
		} else if int(lb) < len(sizes) {
			sizes[lb]++
		}
	}
	out.Noise = int32(noise)
	for i, n := range sizes {
		out.Clusters = append(out.Clusters, ClusterSize{Cluster: int32(i + 1), Rows: int32(n)})
	}
	out.PointsTotal = int32(len(res.rows))
	limit := int(in.Points)
	switch {
	case limit == 0:
		limit = opsProjectionDefaultPoints
	case limit < 0:
		return
	case limit > opsProjectionMaxPoints:
		return out, app.RefuseOperation("at most " + strconv.Itoa(opsProjectionMaxPoints) + " points per call; page with offset")
	}
	if in.Offset < 0 {
		return out, app.RefuseOperation("offset counts from 0")
	}
	for slot := int(in.Offset); slot < len(res.rows) && len(out.Points) < limit; slot++ {
		pt := ProjectionPoint{Row: res.rows[slot], Cluster: -1}
		if slot < len(res.clusters.Label) && res.clusters.Label[slot] >= 0 {
			pt.Cluster = res.clusters.Label[slot] + 1
		}
		if slot < len(res.clusters.Probability) {
			pt.Probability = float64(res.clusters.Probability[slot])
		}
		if slot < len(ps.x) && slot < len(ps.y) {
			pt.X, pt.Y = ps.x[slot], ps.y[slot]
		}
		out.Points = append(out.Points, pt)
	}
	return
}

// explainClusters is explain_clusters' reading of a snapshot.
func explainClusters(ps projectionOpsSnap, in ExplainClustersArgs) (out ClusterExplanation, err error) {
	res := ps.snap.result
	if res == nil {
		return out, app.RefuseOperation("no projection to explain: compute_projection runs one, get_projection says when it is done")
	}
	ex := res.explanation
	var rows []explanationRow
	switch strings.ToLower(strings.TrimSpace(in.Reading)) {
	case "", "features":
		out.Reading = "features"
		depth := ps.depth
		if in.Depth != 0 {
			if in.Depth < 1 || in.Depth > projectionExplainFitDepth {
				return out, app.RefuseOperation("depth runs from 1 to " + strconv.Itoa(projectionExplainFitDepth))
			}
			depth = int(in.Depth)
		}
		perCluster := ps.perCluster
		if in.Partition {
			perCluster = false
		}
		switch {
		case ex.err != nil:
			return out, app.RefuseOperation("the explanation failed: " + ex.err.Error())
		case ex.tree == nil:
			out.Summary = "no clusters to explain: lower min_cluster and compute again"
			return
		}
		out.Summary = explanationSummary(ex, depth, perCluster)
		rows = explanationRows(ex, depth, perCluster)
	case "attributes":
		out.Reading = "attributes"
		out.Summary = itemsSummary(ex.items)
		rows = itemsRows(ex.items, res.clusters.NumClusters)
	default:
		return out, app.RefuseOperation("reading is features or attributes")
	}
	if in.Cluster < 0 || int(in.Cluster) > len(rows) {
		return out, app.RefuseOperation("cluster runs from 1 to " + strconv.Itoa(len(rows)))
	}
	for i, r := range rows {
		if in.Cluster != 0 && int(in.Cluster) != i+1 {
			continue
		}
		n, _ := strconv.Atoi(r.rows)
		out.Clusters = append(out.Clusters, ClusterRule{Cluster: int32(i + 1), Rows: int32(n), Rule: r.rule, Fit: r.fit, Distinguishing: r.features})
	}
	return
}

// paneFedRows is the row count of the result a pane is fed: the node it is
// bound to, or the result the panels draw (the main result, or the node
// observed in the panels). A bound node without a result counts 0.
func (inst *PlayApp) paneFedRows(pane string) (n int64) {
	if inst.graph == nil {
		return
	}
	rec, _ := inst.selectionRecord(inst.resolvedTabNode(pane))
	if rec != nil {
		n = rec.NumRows()
		rec.Release()
	}
	return
}

// parseFeatureSet reads a feature set's name.
func parseFeatureSet(s string) (fs projectionFeatureSetE, ok bool) {
	for _, f := range projectionFeatureSets {
		if strings.EqualFold(strings.TrimSpace(s), f.String()) {
			return f, true
		}
	}
	return
}

// computeProjection applies compute_projection on the render goroutine:
// the run's settings, then the request the pane's next draw starts — the
// pane owns the result it draws and the widget the layout lives in.
func (inst *PlayApp) computeProjection(in ComputeProjectionArgs) (err error) {
	pj := inst.projector
	if pj == nil {
		return app.RefuseOperation("this window has no Projection pane")
	}
	for _, row := range inst.paneRows(inst.frameSchema) {
		if row.TabID == projectionPaneId && row.Draws == PaneDrawNo {
			return app.RefuseOperation("the Projection pane cannot draw this result: " + row.Reject)
		}
	}
	switch pj.Snapshot().status {
	case projectorStatusExtracting, projectorStatusRunning, projectorStatusCancelling:
		return app.ConflictOperation("a projection is running; get_projection says when it is done")
	}
	// The rows of the result the pane draws, which a binding or an observed
	// node may make other than the main result's: the pane checks the same
	// count before it starts a run, and drops the request below it.
	rows := inst.paneFedRows(projectionPaneId)
	if rows < projectionMinRows {
		return app.RefuseOperation("a projection needs at least " + strconv.Itoa(projectionMinRows) + " rows; the result the pane is fed (" +
			string(inst.resolvedTabNode(projectionPaneId)) + ") has " + strconv.FormatInt(rows, 10))
	}
	if in.Neighbours != 0 && (in.Neighbours < 2 || in.Neighbours > 50) {
		return app.RefuseOperation("neighbours runs from 2 to 50")
	}
	if in.MinCluster != 0 && (in.MinCluster < 2 || in.MinCluster > 100) {
		return app.RefuseOperation("min_cluster runs from 2 to 100")
	}
	fs := pj.params.FeatureSet
	if in.Features != "" {
		var ok bool
		if fs, ok = parseFeatureSet(in.Features); !ok {
			return app.RefuseOperation("features is shape, structure or components")
		}
	}
	// The sliders re-derive the run's settings from their knobs every
	// frame, so the knobs are what is set.
	if in.Neighbours != 0 {
		pj.kKnob = float64(in.Neighbours)
	}
	if in.MinCluster != 0 {
		pj.mcsKnob = float64(in.MinCluster)
	}
	pj.params.K, pj.params.MinClusterSize = int(math.Round(pj.kKnob)), int(math.Round(pj.mcsKnob))
	pj.params.FeatureSet = fs
	pj.computeRequested = true
	// The pane is drawn only while raised, and the run starts from its draw.
	if err = inst.ActivateTab(projectionPaneId); err != nil {
		return app.RefuseOperation("this window has no Projection pane")
	}
	return
}

// projectionDigest moves the projection resource when a run starts, lands
// or fails.
func (inst *PlayApp) projectionDigest() (digest string) {
	if inst.projector == nil {
		return ""
	}
	s := inst.projector.Snapshot()
	return strconv.FormatUint(s.version, 10) + "/" + s.status.String()
}

func addProjectionOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResProjection, "the Projection pane's run: its clusters, layout and explanation", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		// The frame's view, not the projector's: a run lands on its own
		// goroutine, and its revision must move inside a frame, where the
		// change is the app's (ADR-0269 §SD4), as the result's does.
		return inst.inner.projFrameDigest
	})
	appops.Command(s, app.OperationSpec{Name: opComputeProjection, Version: 1,
		Summary: "compute the Projection pane over the result it is fed: the rows' neighbour graph, its clusters and a 2-D layout",
		Effect:  app.OperationEffectView, Writes: []string{opsResProjection, opsResPanes}, Reads: []string{opsResResult}, Agents: true,
		Gesture: "the Compute projection button",
		Follows: []string{"the run takes from a moment to a minute; get_projection reports it until done or failed", "the pane is raised: the layout moves only while it is drawn"}},
		func(inst *PlayLauncher, call app.OperationCall, in ComputeProjectionArgs) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := inst.inner.computeProjection(in); err != nil {
				return appops.None{}, err
			}
			inst.inner.projector.runTask = callTask(call)
			return appops.None{}, nil
		})
	appops.Query(s, app.OperationSpec{Name: opGetProjection, Version: 2,
		Summary: "read the Projection pane: the run's status, its clusters and sizes, the layout, and a page of points with their cluster and position",
		Reads:   []string{opsResProjection, opsResResult}, Agents: true, Untrusted: true},
		func(sn opsSnap, in ProjectionArgs) (ProjectionState, error) {
			if !sn.mounted {
				return ProjectionState{}, app.RefuseOperation("the window has not mounted")
			}
			return projectionState(sn.projection, in)
		})
	appops.Query(s, app.OperationSpec{Name: opExplainClusters, Version: 1,
		Summary: "read why the clusters are what they are: per cluster a SQL rule over the result's columns with its precision and recall, and what sets it apart",
		Reads:   []string{opsResProjection}, Agents: true, Untrusted: true},
		func(sn opsSnap, in ExplainClustersArgs) (ClusterExplanation, error) {
			if !sn.mounted {
				return ClusterExplanation{}, app.RefuseOperation("the window has not mounted")
			}
			return explainClusters(sn.projection, in)
		})
}
