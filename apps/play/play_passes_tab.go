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
// The schematic is the registry's catalog — what the client WOULD apply.
// Layered over it are the per-buffer outcomes of actually applying it
// (ADR-0108's 2026-07-27 update): a pass that failed and was skipped is tinted
// error-toned and listed with its message below, a factory that declined the
// client's binding stays recessed and is named as declined. Without that layer
// a skipped rewrite is invisible — §SD3 makes every unit degrade rather than
// fail, so the statement still ships and only a warn line in the process log
// records that it shipped un-rewritten.

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

// rewriteTraceState memoises Client.RewriteTrace by what the trace depends on:
// the statement that would run (the caret's, on a multi-statement buffer) and
// the selection-condition toggle, which rewrites without an edit. Recomputing
// costs a full rewrite — every pass re-parses — so it happens on a key change,
// not per frame.
type rewriteTraceState struct {
	forSQL     string
	conditions bool
	valid      bool
	obs        []passreg.ApplyObservation
}

// rewriteTrace returns the client-side rewrite's per-unit outcomes for the
// statement Run would ship, recomputing only when the buffer, the caret's
// statement or the conditions toggle moved. ok=false means there is nothing to
// describe — no client, or an empty buffer.
//
// Deliberately demand-driven rather than computed in updatePreview: the Passes
// and Diagnostics tabs are the only readers and both are lazy dock tabs, so a
// session with neither open never pays for it. The first reader in a frame
// computes; the second gets the memo.
func (inst *PlayApp) rewriteTraceFor() (obs []passreg.ApplyObservation, ok bool) {
	if inst.client == nil {
		return
	}
	runSQL, _, _ := inst.runBuffer()
	if strings.TrimSpace(runSQL) == "" {
		return
	}
	conds := inst.client.ExposeConditions()
	st := &inst.rewriteTrace
	if !st.valid || st.forSQL != runSQL || st.conditions != conds {
		st.forSQL, st.conditions, st.valid = runSQL, conds, true
		st.obs = inst.client.RewriteTrace(runSQL)
	}
	return st.obs, true
}

// skippedRewrites filters a trace to the units that did not run: a pass whose
// Run errored (its rewrite was dropped and the prior SQL shipped) and a factory
// whose Build declined the client's binding. These are what a user is looking
// for when the shipped statement does not match what they expected.
func skippedRewrites(obs []passreg.ApplyObservation) (out []passreg.ApplyObservation) {
	for _, o := range obs {
		if o.Outcome == passreg.ApplyOutcomeSkipped || o.Outcome == passreg.ApplyOutcomeDeclined {
			out = append(out, o)
		}
	}
	return
}

// rewriteOutcomeText names one unit's outcome for a detail line.
func rewriteOutcomeText(o passreg.ApplyObservation) string {
	switch o.Outcome {
	case passreg.ApplyOutcomeApplied:
		if o.Changed {
			return "applied — rewrote the statement"
		}
		return "applied — left the statement unchanged"
	case passreg.ApplyOutcomeSkipped:
		return "SKIPPED — it failed, and the statement shipped without its rewrite"
	case passreg.ApplyOutcomeDeclined:
		return "declined — this client's binding is not one it can build against"
	}
	return o.Outcome.String()
}

// rewriteOutcomeSummary is the one-line accounting of a trace: how many units
// ran, how many of those changed the SQL, and how many were skipped or
// declined.
func rewriteOutcomeSummary(obs []passreg.ApplyObservation) string {
	var applied, changed, skipped, declined int
	for _, o := range obs {
		switch o.Outcome {
		case passreg.ApplyOutcomeApplied:
			applied++
			if o.Changed {
				changed++
			}
		case passreg.ApplyOutcomeSkipped:
			skipped++
		case passreg.ApplyOutcomeDeclined:
			declined++
		}
	}
	parts := []string{fmt.Sprintf("%d applied (%d rewrote)", applied, changed)}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if declined > 0 {
		parts = append(parts, fmt.Sprintf("%d declined", declined))
	}
	return strings.Join(parts, " · ")
}

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

// entryPassForRow returns the registered concrete Pass behind a catalog row,
// so the detail panel can look inside composites. Late-bound factory rows
// have no process-global Pass (they are realised per client binding) and
// report ok=false.
func entryPassForRow(reg *passreg.Registry, row passreg.CatalogRow) (p nanopass.Pass, ok bool) {
	if row.LateBound {
		return
	}
	for _, e := range reg.Entries(row.Stage) {
		if e.Pass.Name == row.Name {
			return e.Pass, true
		}
	}
	return
}

// passChildrenLines flattens a composite pass's member tree (Pass.Children)
// into indented display lines ("name · properties"), depth-first, two spaces
// per level — a combinator wrapper is followed by its body, whose own line
// carries the properties the wrapper hides (e.g. the fixed-point flag).
// Empty for leaf passes; the caller skips the block entirely.
func passChildrenLines(p nanopass.Pass) (lines []string) {
	var walk func(ps []nanopass.Pass, depth int)
	walk = func(ps []nanopass.Pass, depth int) {
		for _, ch := range ps {
			lines = append(lines, strings.Repeat("  ", depth)+ch.Name+" · "+passPropsText(ch.Properties))
			walk(ch.Children, depth+1)
		}
	}
	walk(p.Children, 0)
	return
}

// renderPassesOutcomes is the outcome band under the schematic: the trace's
// accounting line, then one line per unit that did not run. It covers the whole
// client-side rewrite, not only the registry stage — play's own steps
// (extract-params, expose-conditions, set-format) degrade the same way and are
// no more visible in the result — so a named step with no stage on the spine
// above is one of those. Full error prose stays in the Diagnostics tab; these
// lines are truncated pointers.
func (inst *PlayApp) renderPassesOutcomes(trace []passreg.ApplyObservation, traced bool) {
	if !traced {
		for rt := range c.RichTextLabel("Type SQL in the Editor tab to see what these passes do to it.") {
			rt.Small().Weak()
		}
		return
	}
	total := rewriteTotalCost(trace)
	for rt := range c.RichTextLabel("on the statement that would run: " + rewriteOutcomeSummary(trace) +
		" · " + formatCostDur(total) + " to rewrite") {
		rt.Small().Weak()
	}
	if total >= rewriteCostWarn {
		// Amber rather than weak: this is the one line that says the buffer is
		// on the expensive part of the curve (ADR-0192).
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.WarningStrong.AsHex()), color.Transparent,
			fmt.Sprintf("slow rewrite — over the %s mark; the Diagnostics tab explains what it cost and why",
				formatCostDur(rewriteCostWarn))) {
			rt.Small()
		}
	}
	for _, o := range skippedRewrites(trace) {
		line := o.Name + " — " + rewriteOutcomeText(o)
		if o.Err != nil {
			line += ": " + truncateRunes(firstLine(o.Err.Error()), 120)
		}
		for rt := range c.RichTextLabel(line) {
			rt.Small().Monospace()
		}
	}
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
	// which is why the separator comes first). Seq-keyed r21 slot, so it
	// contends with nobody — unlike CaptureAvailableSize, whose one register
	// the frame's last capture wins. (captureUiAvailableRect is the same slot
	// without the separator, and carries the pane HEIGHT too.)
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
	// Per-buffer outcomes, keyed by stage id so the fill callbacks can reach
	// them. Absent (no client, empty buffer) leaves the drawing exactly as it
	// was before the trace existed.
	trace, traced := inst.rewriteTraceFor()
	outcome := make(map[string]passreg.ApplyObservation, len(trace))
	for _, o := range trace {
		outcome[passStageID(o.Name)] = o
	}
	selectedID := ""
	if st.selected != "" {
		selectedID = passStageID(st.selected)
	}
	res := pview.Render(passesVizIDSalt+inst.vizSeed, st.layout, pview.RenderOpts{
		CanvasW: w,
		CanvasH: h,
		NodeFill: func(id string) (col color.Color, ok bool) {
			// Selection wins over outcome: the user asked for this one.
			if id == selectedID {
				return color.Hex(styletokens.AccentDefault.AsHex()), true
			}
			if o, has := outcome[id]; has {
				switch {
				case o.Outcome == passreg.ApplyOutcomeSkipped:
					return color.Hex(styletokens.ErrorDefault.AsHex()), true
				case isSlowRewriteUnit(o):
					// Cost outranks "it rewrote something" (ADR-0192): on a
					// buffer that is slow to compile, which unit is expensive
					// is the thing the author came here to find, and a unit can
					// be both expensive and productive.
					return color.Hex(styletokens.WarningDefault.AsHex()), true
				case o.Outcome == passreg.ApplyOutcomeApplied && o.Changed:
					// The units that actually touched this buffer, set apart
					// from the ones that ran and found nothing to do.
					return color.Hex(styletokens.SuccessSubtle.AsHex()), true
				}
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
			if o, has := outcome[id]; has && (o.Outcome == passreg.ApplyOutcomeSkipped || isSlowRewriteUnit(o)) {
				// ErrorDefault and WarningDefault are light tints, so their
				// labels need dark text for the same reason the accent
				// selection fill does.
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
	// Counted over the drawn stages, not over the whole trace: play's own
	// rewrite steps carry costs too but have no box on the spine, so counting
	// them here would promise an amber node the reader cannot find.
	slowCount := 0
	for _, r := range st.rows {
		if isSlowRewriteUnit(outcome[passStageID(r.Name)]) {
			slowCount++
		}
	}
	status := fmt.Sprintf("%d pass(es) at pre-execute, in apply order (ADR-0108)", len(st.rows))
	if lateCount > 0 {
		status += fmt.Sprintf(" · %d late-bound (recessed)", lateCount)
	}
	if slowCount > 0 {
		status += fmt.Sprintf(" · %d over %s (amber)", slowCount, formatCostDur(rewriteCostStepWarn))
	}
	for rt := range c.RichTextLabel(status + " · click a pass for details") {
		rt.Small().Weak()
	}
	inst.renderPassesOutcomes(trace, traced)

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
		o, has := outcome[passStageID(row.Name)]
		if has {
			for rt := range c.RichTextLabel("on this buffer: " + rewriteOutcomeText(o) +
				" · " + formatCostDur(o.Dur)) {
				rt.Small().Weak()
			}
			if o.Err != nil {
				c.Label(o.Err.Error()).Wrap().Selectable(true).Send()
			}
		}
		inst.renderPassInternals(reg, *row, o, has)
	}
}

// renderPassInternals draws what is inside the selected unit. When the buffer
// was traced this is the MEASURED tree — every pass invocation the unit's Run
// actually made, with what it cost, how many fixed-point iterations it took,
// and whether it changed anything (ADR-0192). Untraced, it falls back to the
// registry's static Pass.Children listing, which says what the unit is composed
// of but nothing about what any of it costs.
//
// The measured tree is the more useful of the two and also the less complete:
// it shows the invocations that HAPPENED, so a Conditional whose predicate was
// false is simply absent rather than listed as inert.
func (inst *PlayApp) renderPassInternals(reg *passreg.Registry, row passreg.CatalogRow, o passreg.ApplyObservation, traced bool) {
	if traced && len(o.Cost.Children) > 0 {
		lines := costTreeLines(o.Cost)
		for rt := range c.RichTextLabel(fmt.Sprintf("%d pass invocations on this buffer, in the order they ran:", len(lines))) {
			rt.Small().Weak()
		}
		for _, ln := range lines {
			for rt := range c.RichTextLabel(ln) {
				rt.Small().Monospace()
			}
		}
		if s := costUnaccounted(o); s != "" {
			for rt := range c.RichTextLabel(s) {
				rt.Small().Weak()
			}
		}
		if n, wasted := wastedStepCount(o.Cost); n > 0 {
			// The standing suspicion about this pipeline, made checkable per
			// buffer: passes hand each other TEXT, so one that rewrites nothing
			// still pays a full re-parse to find that out.
			for rt := range c.RichTextLabel(fmt.Sprintf(
				"%d of them cost %s between them and rewrote nothing", n, formatCostDur(wasted))) {
				rt.Small().Weak()
			}
		}
		return
	}
	p, ok := entryPassForRow(reg, row)
	if !ok {
		return
	}
	lines := passChildrenLines(p)
	if len(lines) == 0 {
		return
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("composed of %d sub-passes, in apply order:", len(p.Children))) {
		rt.Small().Weak()
	}
	for _, ln := range lines {
		for rt := range c.RichTextLabel(ln) {
			rt.Small().Monospace()
		}
	}
}
