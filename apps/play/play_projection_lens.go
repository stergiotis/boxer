package play

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// play_projection_lens.go is the lens inside the Projection panel (ADR-0289
// §SD4): the run's rows read as slots, banded by the run's own
// clusters, and drawn as the clusters' archetypes, every row in its band, or
// one row among its peers — beside the neighbour graph, under one numbering.

// projectionViewE is what the panel shows under its status line.
type projectionViewE uint8

const (
	// projectionViewGraph is the neighbour graph and its layout.
	projectionViewGraph projectionViewE = iota
	// projectionViewArchetypes is each cluster as what it typically holds,
	// its extremes and the rows that break it.
	projectionViewArchetypes
	// projectionViewRows is every row, in its cluster's band.
	projectionViewRows
	// projectionViewRow is the selected row among its cluster's peers.
	projectionViewRow
)

var projectionViews = [...]projectionViewE{projectionViewGraph, projectionViewArchetypes, projectionViewRows, projectionViewRow}

func (inst projectionViewE) String() string {
	switch inst {
	case projectionViewGraph:
		return "graph"
	case projectionViewArchetypes:
		return "archetypes"
	case projectionViewRows:
		return "rows"
	case projectionViewRow:
		return "row"
	}
	return "?"
}

func (inst projectionViewE) form() leewaywidgets.LensFormE {
	switch inst {
	case projectionViewArchetypes:
		return leewaywidgets.LensFormArchetypes
	case projectionViewRow:
		return leewaywidgets.LensFormFocus
	}
	return leewaywidgets.LensFormRows
}

// The lens's intents as the panel opens: the gist of the values, each row on
// its own terms — the archetype form the lens's judged rounds converged on.
const (
	projectionLensDefaultValues = 0.5
	projectionLensDefaultStable = 0
)

// projectionLensProbeSalt namespaces the lens's pane probe.
const projectionLensProbeSalt uint64 = 0x2c61f0b8d3a7e945

// registryRenderer names memberships through the process's registries,
// so a ref membership reads as its name rather than its id.
func registryRenderer() *membership.Renderer {
	return membership.NewRenderer(providers.MembershipRefFormatter{}, nil, nil)
}

// projectionLens analyses the run's rows as slots under the run's clusters.
// full holds one row per entity of the batch; slotEntity[s] is the entity at
// graph slot s and labels[s] its cluster, so the lens's row s is the graph's
// slot s.
func projectionLens(ctx context.Context, full *lwlens.Model, slotEntity []int, labels []int32) (a *lwlens.Analysis, err error) {
	m := &lwlens.Model{Slots: full.Slots, Sections: full.Sections, Rows: make([]lwlens.Row, len(slotEntity))}
	for s, e := range slotEntity {
		if e < 0 || e >= len(full.Rows) {
			err = eb.Build().Int("entity", e).Int("rows", len(full.Rows)).Errorf("projection: the lens read fewer rows than the run")
			return
		}
		m.Rows[s] = full.Rows[e]
	}
	an, err := lwlens.Analyze(ctx, m, lwlens.AnalyzeOptions{Labels: labels})
	if err != nil {
		return
	}
	a = &an
	return
}

// projectionCoreDist is HDBSCAN's core distance read at the
// min(K, minCluster−1)-th neighbour of the run's lists (ADR-0289 §SD5): with
// the K-th, a kind smaller than K finds its core distance in another kind,
// every core distance of a batch of a few kinds is the same, and HDBSCAN
// finds nothing. The graph and its layout keep K.
func projectionCoreDist(g *knn.Result, minCluster int) []float32 {
	k := min(g.K, max(1, minCluster-1))
	if k >= g.K || len(g.Dists) < len(g.CoreDist)*g.K {
		return g.CoreDist
	}
	out := make([]float32, len(g.CoreDist))
	for s := range out {
		out[s] = g.Dists[s*g.K+k-1]
	}
	return out
}

// lensPlanKey is what a cached plan was made for.
type lensPlanKey struct {
	version        uint64
	values, stable float64
}

// renderViewSelector draws the view switch.
func (inst *Projector) renderViewSelector() {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("show") {
			rt.Small().Weak()
		}
		for _, v := range projectionViews {
			if c.Button(ids.PrepareSeq(uint64(0x3300)+uint64(v)), c.Atoms().Text(v.String()).Keep()).
				Selected(inst.show == v).
				SendResp().HasPrimaryClicked() {
				inst.show = v
			}
		}
	}
}

// renderLensControls draws the lens's two intents, and the publish
// affordance the graph's layout row carries in the graph view.
func (inst *Projector) renderLensControls() {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		c.SliderF64(ids.PrepareStr("projectionLensValues"), inst.lensValues, 0, 1).
			Text("structure ↔ values").SendRespVal(&inst.lensValues)
		if inst.show != projectionViewRow {
			c.SliderF64(ids.PrepareStr("projectionLensStable"), inst.lensStable, 0, 1).
				Text("each row on its own ↔ one frame").SendRespVal(&inst.lensStable)
		}
		if inst.renderPublish != nil {
			c.Separator().Vertical().Send()
			inst.renderPublish()
		}
	}
}

// renderLens draws the run's lens in the current view. The plan is made
// once per run and intent, on this goroutine: it is linear in the rows but
// for the seriation, which lwlens bounds.
func (inst *Projector) renderLens(snap projectorSnapshot, selectedRow int64) {
	res := snap.result
	switch {
	case res.lensErr != nil:
		c.Label("The lens could not read this run: " + res.lensErr.Error()).Wrap().Send()
		return
	case res.lens == nil:
		return
	}
	key := lensPlanKey{version: snap.version, values: inst.lensValues, stable: inst.lensStable}
	if !inst.lensPlanOk || inst.lensPlanFor != key {
		inst.lensPlan = lwlens.PlanRows(res.lens, lwlens.Intent{Values: key.values, Stable: key.stable})
		inst.lensPlanFor, inst.lensPlanOk = key, true
	}
	focus := int32(-1)
	if inst.show == projectionViewRow {
		for s, row := range res.rows {
			if row == selectedRow {
				focus = int32(s)
				break
			}
		}
		if focus < 0 {
			for rt := range c.RichTextLabel("Select a row — a node in the graph, or a row in the Table — to read it among its cluster's peers.") {
				rt.Small().Weak()
			}
			return
		}
	}
	if inst.lensView == nil {
		inst.lensView = leewaywidgets.NewLensView(inst.ids, "projection-lens")
	}
	if availW, availH, ok := c.CapturePaneSize(projectionLensProbeSalt ^ inst.idSeed); ok &&
		availW > 0 && availH > 0 && !math.IsNaN(float64(availW)) && !math.IsNaN(float64(availH)) {
		inst.lensPaneW, inst.lensPaneH = availW, availH
	}
	w, h := projectionLensPaneFill.box(inst.lensPaneW, inst.lensPaneH)
	// The lens lays out into projectionLensDrawHeight and counts what it
	// has no room for; the scroll area shows the pane's share of it, so a
	// short pane scrolls through every cluster instead of naming most of
	// them "not drawn".
	for range c.IdScope(inst.ids.PrepareStr("projectionLensScroll")) {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).MaxHeight(h).KeepIter() {
			inst.lensView.Render(res.lens, &inst.lensPlan, inst.show.form(), focus, w-14, projectionLensDrawHeight)
		}
	}
}

// projectionLensDrawHeight is the height the lens lays out into: every
// cluster's archetype at the row budget each keeps, and a few hundred rows of
// the rows view, whose rest it counts.
const projectionLensDrawHeight = 4000

// projectionLensPaneFill is the lens's box, the shared pane rule
// (play_pane_box.go): the lens counts the rows it has no room for, so its
// height is the room the pane gives it.
var projectionLensPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 2400, minH: 200,
	fallbackW: 900, fallbackH: 420,
}
