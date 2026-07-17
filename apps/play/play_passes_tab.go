package play

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview"
	pview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview/view"
)

// play_passes_tab.go is the ADR-0119 M3 consumer: the Passes dock tab draws
// the passreg pre-execute pipeline — the rewrite sequence every executed
// statement runs through (ADR-0108) — as a pipelineview schematic. Passes sit
// on the spine in (Order, Name) application order, the editor's SQL enters
// west, the executor sits east; a pass declaring NeedsFixedPoint carries a
// dashed self-feedback loop, and late-bound factory descriptors are tinted
// recessed — they apply only where the client's binding accepts them
// (ADR-0116 §SD6), so on the unbound path they are catalog-only. Clicking a
// stage selects it and the section below shows its catalog row.
//
// The drawing is the registry's catalog, not a per-run trace: it is what the
// client WOULD apply. Per-run outcomes (pass failed-and-skipped, factory
// declined) are a deferred slice — they need an observed apply seam in
// passreg.

// passesVizIDSalt namespaces the Passes canvas ids; composed with the
// per-instance vizSeed so two PlayApp instances do not collide, and distinct
// from vizIDSalt so the two drawings within one instance do not either.
const passesVizIDSalt uint64 = 0x7a55e50000000000

const (
	passesSrcEndpointID  = "src/editor"
	passesSinkEndpointID = "sink/executor"
	passesStagePrefix    = "pass/"
)

// passesTabState is the Passes tab's render-thread state (slice-6 D2: state
// lives on PlayApp).
type passesTabState struct {
	key      string // catalog fingerprint the cached layout was built for
	rows     []passreg.CatalogRow
	layout   *pipelineview.Layout
	err      error
	selected string // selected catalog row name ("" = none)
}

func passStageID(name string) string { return passesStagePrefix + name }

// passesCatalogKey fingerprints what the drawing depends on: pass identity,
// order, late-boundness, the fixed-point flag (the label set and edge set),
// and the executor URL the sink endpoint displays — switching endpoints
// relayouts.
func passesCatalogKey(rows []passreg.CatalogRow, sinkURL string) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s|%d|%t|%t;", r.Name, r.Order, r.LateBound, r.Properties.NeedsFixedPoint)
	}
	b.WriteString(sinkURL)
	return b.String()
}

// passesPipeline models the catalog as a pipeline: one spine stage per row,
// editor → first pass, last pass → executor (sublabelled with its URL), and
// a dashed self-loop on every fixed-point pass.
func passesPipeline(rows []passreg.CatalogRow, sinkURL string) pipelineview.Pipeline {
	els := make([]pipelineview.Element, 0, len(rows))
	edges := make([]pipelineview.Edge, 0, len(rows)+2)
	for _, r := range rows {
		id := passStageID(r.Name)
		els = append(els, pipelineview.Stage{ID: id, Label: r.Name})
		if r.Properties.NeedsFixedPoint {
			// "fixed point", not "fixpoint": the fi-ligature drops a glyph in
			// the SVG-export → cairosvg path used by scripted captures.
			edges = append(edges, pipelineview.Edge{
				From: pipelineview.Ref{Stage: id}, To: pipelineview.Ref{Stage: id}, Label: "fixed point",
			})
		}
	}
	edges = append(edges,
		pipelineview.Edge{From: pipelineview.Ref{Endpoint: passesSrcEndpointID}, To: pipelineview.Ref{Stage: passStageID(rows[0].Name)}},
		pipelineview.Edge{From: pipelineview.Ref{Stage: passStageID(rows[len(rows)-1].Name)}, To: pipelineview.Ref{Endpoint: passesSinkEndpointID}},
	)
	return pipelineview.Pipeline{
		Root: pipelineview.Group{Children: els},
		Endpoints: []pipelineview.Endpoint{
			{ID: passesSrcEndpointID, Label: "editor", Kind: pipelineview.EndpointStream},
			{ID: passesSinkEndpointID, Label: "ClickHouse", Sublabel: sinkURL, Kind: pipelineview.EndpointStore},
		},
		Edges: edges,
	}
}

// envRegionsText names the set bits of an EnvRegions bitset.
func envRegionsText(r nanopass.EnvRegions) string {
	names := []struct {
		bit  nanopass.EnvRegions
		name string
	}{
		{nanopass.RegionBody, "body"},
		{nanopass.RegionSessionSettings, "session-settings"},
		{nanopass.RegionStatementSettings, "statement-settings"},
		{nanopass.RegionParams, "params"},
		{nanopass.RegionFormat, "format"},
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if r&n.bit != 0 {
			parts = append(parts, n.name)
		}
	}
	return strings.Join(parts, ",")
}

// passPropsText is the one-line properties summary under the selection.
func passPropsText(p nanopass.PassProperties) string {
	var parts []string
	if p.Idempotent {
		parts = append(parts, "idempotent")
	}
	if p.NeedsFixedPoint {
		parts = append(parts, "fixed-point")
	}
	if p.Reads != 0 {
		parts = append(parts, "reads="+envRegionsText(p.Reads))
	}
	if p.Writes != 0 {
		parts = append(parts, "writes="+envRegionsText(p.Writes))
	}
	if len(p.Requires) > 0 {
		parts = append(parts, "requires="+joinFormTags(p.Requires))
	}
	if len(p.Produces) > 0 {
		parts = append(parts, "produces="+joinFormTags(p.Produces))
	}
	if len(parts) == 0 {
		return "no declared properties"
	}
	return strings.Join(parts, " · ")
}

func joinFormTags(tags []nanopass.FormTag) string {
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = string(t)
	}
	return strings.Join(parts, ",")
}

// renderPassesTab draws the Passes tab body (inside the dock's scroll host).
func (inst *PlayApp) renderPassesTab() {
	ids := inst.ids
	sm := c.CurrentApplicationState.StateManager
	reg := passreg.Default
	if inst.client != nil {
		reg = inst.client.PassRegistry()
	}
	all := reg.Catalog()
	rows := make([]passreg.CatalogRow, 0, len(all))
	for _, r := range all {
		if r.Stage == passreg.StagePreExecute {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		for rt := range c.RichTextLabel("No passes registered for the pre-execute stage.") {
			rt.Small().Weak()
		}
		return
	}

	sinkURL := ""
	if inst.client != nil {
		sinkURL = inst.client.URL()
	}
	st := &inst.passesTab
	if key := passesCatalogKey(rows, sinkURL); key != st.key || (st.layout == nil && st.err == nil) {
		st.key = key
		st.rows = rows
		st.layout, st.err = pipelineview.Compute(passesPipeline(rows, sinkURL), pipelineview.LayoutOpts{FontSize: 13})
	}
	if st.err != nil {
		for rt := range c.RichTextLabel("pass pipeline unavailable: " + truncateRunes(firstLine(st.err.Error()), 100)) {
			rt.Small().Weak()
		}
		return
	}
	if st.layout == nil {
		return
	}

	// Pane-width probe: the separator spans the full pane width, so the
	// captureUiRect snapshot right after it reports it (min_rect is the
	// placed-widget bbox — a probe with nothing placed reads degenerate,
	// which is why the separator comes first). Seq-keyed r21 slot, so this
	// does not contend with the editor's CaptureAvailableSize register.
	// One-frame lag; first frame falls back to a conservative width.
	c.Separator().Horizontal().Send()
	probeSeq := passesVizIDSalt ^ inst.vizSeed ^ 0x1
	c.CaptureUiRect(probeSeq)
	paneW := float32(700)
	if r, ok := sm.GetUiRect(probeSeq); ok && r.MaxX > r.MinX {
		paneW = r.MaxX - r.MinX
	}

	// Width fills the pane (clamped sane); height from the layout's aspect,
	// clamped — the drawing then fits inside without horizontal clipping.
	lw, lh := st.layout.Width, st.layout.Height
	if lw <= 0 || lh <= 0 {
		return
	}
	w := min(max(paneW-12, 320), 1400)
	hRatio := float32(lh / lw)
	h := min(max(w*hRatio, 120), 340)

	lateBound := make(map[string]bool, len(st.rows))
	for _, r := range st.rows {
		lateBound[passStageID(r.Name)] = r.LateBound
	}
	selectedID := ""
	if st.selected != "" {
		selectedID = passStageID(st.selected)
	}
	res := pview.Render(passesVizIDSalt+inst.vizSeed, st.layout, pview.RenderOpts{
		CanvasW: w,
		CanvasH: h,
		NodeFill: func(id string) (col color.Color, ok bool) {
			if id == selectedID {
				return color.Hex(styletokens.AccentDefault.AsHex()), true
			}
			if lateBound[id] {
				return color.Hex(styletokens.NeutralBgFaint.AsHex()), true
			}
			return
		},
		NodeText: func(id string) (col color.Color, ok bool) {
			if id == selectedID {
				return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
			}
			return
		},
	})
	if name, ok := strings.CutPrefix(res.Clicked, passesStagePrefix); ok {
		if name == st.selected {
			st.selected = ""
		} else {
			st.selected = name
		}
	}

	lateCount := 0
	for _, r := range st.rows {
		if r.LateBound {
			lateCount++
		}
	}
	status := fmt.Sprintf("%d pass(es) at pre-execute, in apply order (ADR-0108)", len(st.rows))
	if lateCount > 0 {
		status += fmt.Sprintf(" · %d late-bound (recessed)", lateCount)
	}
	for rt := range c.RichTextLabel(status + " · click a pass for details") {
		rt.Small().Weak()
	}

	if st.selected == "" {
		return
	}
	var row *passreg.CatalogRow
	for i := range st.rows {
		if st.rows[i].Name == st.selected {
			row = &st.rows[i]
			break
		}
	}
	if row == nil { // selection survived a catalog change that dropped the row
		st.selected = ""
		return
	}
	c.Separator().Horizontal().Send()
	for range c.IdScope(ids.PrepareStr("passesDetail")) {
		for rt := range c.RichTextLabel(row.Name) {
			rt.Strong()
		}
		if row.Description != "" {
			c.Label(row.Description).Send()
		}
		kind := "concrete entry"
		if row.LateBound {
			kind = "late-bound factory (realised per client binding, ADR-0116 §SD6)"
		}
		for rt := range c.RichTextLabel(fmt.Sprintf("order %d · %s · %s", row.Order, row.Stage.String(), kind)) {
			rt.Small().Weak()
		}
		for rt := range c.RichTextLabel(passPropsText(row.Properties)) {
			rt.Small().Weak()
		}
		if row.Provenance != "" {
			for rt := range c.RichTextLabel(row.Provenance) {
				rt.Small().Monospace()
			}
		}
	}
}
