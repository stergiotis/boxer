package play

import (
	"fmt"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// play_graph_view.go is ADR-0097 slice 3e: the node Graph view — graph CHROME
// (not a PanelI, SD7). It renders the split graph recovered from the last-run
// buffer (PlayApp.currentSplit): each node is a collapsible header showing its
// data edges (nodes it reads), signal edges (params it reads), and its compiled
// SQL. This makes the reactive query-graph visible. Observing an intermediate
// node to drive a panel (materialization) is slice 3d.

// renderGraphTab draws the Graph dock tab body.
func (inst *PlayApp) renderGraphTab() {
	ids := inst.ids
	// The Signals section is store state, independent of a split — it
	// renders even before the first Run (slice 5e).
	inst.renderSignalsSection()
	split := inst.currentSplit
	if len(split.Nodes) == 0 {
		msg := "Run a query to see its node graph."
		if inst.splitErr != nil {
			// Pointer only — the split error's full text lives in the
			// Diagnostics tab's "Query graph" section.
			msg = "The buffer did not split into a graph (it executed as a single statement) — see the Diagnostics tab."
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}
	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel(fmt.Sprintf("%d node(s) · sink: %s", len(split.Nodes), split.Sink)) {
			rt.Small().Weak()
		}
		for rt := range c.RichTextLabel(inst.channelInventory()) {
			rt.Small().Weak()
		}
		c.Separator().Horizontal().Send()
		for i := range split.Nodes {
			inst.renderGraphNode(ids, split.Nodes[i])
		}
	}
}

// renderGraphNode draws one node: a collapsible header with its edges and its
// compiled SQL (the CTE body, or the whole statement for the sink).
func (inst *PlayApp) renderGraphNode(ids *c.WidgetIdStack, n splitNode) {
	kind := "CTE"
	if n.Recursive {
		kind = "CTE (recursive)"
	}
	if n.Kind == splitNodeStatement {
		kind = "sink"
	}
	header := fmt.Sprintf("%s · %s", n.ID, kind)
	for range c.CollapsingHeader(ids.PrepareStr("graphNode-"+string(n.ID)),
		c.WidgetText().Text(header).Keep()).DefaultOpen(true).KeepIter() {
		for range c.Vertical().KeepIter() {
			// Observe this node in the result panels (3d): clicking materialises
			// it on the intermediate lane and the panels render its result.
			observed := inst.observedNode == n.ID
			obsLabel := "observe in panels"
			if observed {
				obsLabel = "● observing in panels"
			}
			if c.Button(ids.PrepareStr("graphObs-"+string(n.ID)),
				c.Atoms().Text(obsLabel).Keep()).
				Selected(observed).
				SendResp().HasPrimaryClicked() {
				inst.observedNode = n.ID
			}
			// Channel eligibility (4c): observing fills the main channels
			// (Table/Projection/Detail); a _tl_*-shaped node also fills the
			// Timeline's events/bands channels.
			if elig := nodeChannelEligibility(n); len(elig) > 0 {
				for rt := range c.RichTextLabel("also fills: " + strings.Join(elig, ", ")) {
					rt.Small().Weak()
				}
			}
			if len(n.DependsOn) > 0 {
				for rt := range c.RichTextLabel("reads nodes: " + joinNodeIDs(n.DependsOn)) {
					rt.Small().Weak().Monospace()
				}
			}
			if len(n.Reads) > 0 {
				for rt := range c.RichTextLabel("reads params: " + strings.Join(n.Reads, ", ")) {
					rt.Small().Weak().Monospace()
				}
			}
			c.CodeView(ids.PrepareStr("graphSql-"+string(n.ID)),
				codeview.BuildSql(n.SQL)).Wrap().Send()
		}
	}
}

// renderSignalsSection is the Signals chrome (ADR-0097 slice 5e): the
// SD8 store made visible AND writable. One row per held-or-referenced name —
// declared type(s) with a cross-node conflict warning, an editable raw value
// (the human signal-writing path, D3's deferred half), write provenance, a
// pinned-by-SET hint (D1 shadowing), and an unfilled marker. The footer adds
// an arbitrary name, e.g. one a panel-authored node (bands, Map) references
// but nothing has written yet.
func (inst *PlayApp) renderSignalsSection() {
	ids := inst.ids
	rows := inst.collectSignalChrome()
	header := fmt.Sprintf("signals (%d)", len(rows))
	for range c.CollapsingHeader(ids.PrepareStr("signalsSection"),
		c.WidgetText().Text(header).Keep()).DefaultOpen(true).KeepIter() {
		for range c.Vertical().KeepIter() {
			if len(rows) == 0 {
				for rt := range c.RichTextLabel("no signals yet — panels write selection / vp_* / tl_* as you interact; add one below") {
					rt.Small().Weak()
				}
			}
			for _, r := range rows {
				inst.renderSignalRow(r)
			}
			inst.evictSignalDrafts(rows)
			for range c.Horizontal().KeepIter() {
				c.TextEdit(ids.PrepareStr("sigAddName"), inst.sigAddName, false).
					DesiredWidth(120).HintText("name").
					SendRespVal(&inst.sigAddName)
				c.TextEdit(ids.PrepareStr("sigAddValue"), inst.sigAddValue, false).
					DesiredWidth(160).HintText("value").
					SendRespVal(&inst.sigAddValue)
				if c.Button(ids.PrepareStr("sigAdd"), c.Atoms().Text("add signal").Keep()).
					SendResp().HasPrimaryClicked() {
					if name := strings.TrimSpace(inst.sigAddName); name != "" {
						inst.graph.setSignalRawFrom(name, inst.sigAddValue, signalWriterEditor)
					}
				}
			}
		}
	}
	c.Separator().Horizontal().Send()
}

// renderSignalRow draws one Signals row. The value TextEdit is a
// reseed-guarded draft: while the store value is unchanged the user's typing
// wins; when a panel moves the signal, the fresh value overwrites the draft
// (live-follow) via the Stubborn-Text override.
func (inst *PlayApp) renderSignalRow(r signalChromeRow) {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(r.Name) {
			rt.Monospace()
		}
		if len(r.Types) > 0 {
			label := strings.Join(r.Types, " | ")
			if r.Conflict {
				label += "  ⚠ type conflict across nodes"
			}
			for rt := range c.RichTextLabel(label) {
				rt.Small().Weak()
			}
		}
		draft := inst.sigValDraft(r)
		c.TextEdit(ids.PrepareStr("sigVal-"+r.Name), *draft, false).
			DesiredWidth(160).HintText("raw value").
			SendRespVal(draft)
		if c.Button(ids.PrepareStr("sigSet-"+r.Name), c.Atoms().Text("set").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.graph.setSignalRawFrom(r.Name, *draft, signalWriterEditor)
		}
		if r.Held {
			if c.Button(ids.PrepareStr("sigClear-"+r.Name), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.graph.deleteSignal(r.Name)
			}
		}
		var notes []string
		if r.Pinned {
			notes = append(notes, "pinned by SET (shadows the signal)")
		}
		if r.Unfilled {
			notes = append(notes, "unfilled input")
		}
		if r.Held {
			notes = append(notes, fmt.Sprintf("via %s · r%d", r.Writer, r.Rev))
		}
		if len(notes) > 0 {
			for rt := range c.RichTextLabel(strings.Join(notes, " · ")) {
				rt.Small().Weak()
			}
		}
	}
}

// sigValDraft returns the row's stable draft pointer, reseeding it when the
// store value moved since the last seed (external writes win over an idle
// draft; mid-edit they win too — the value is live).
func (inst *PlayApp) sigValDraft(r signalChromeRow) *string {
	ptr, ok := inst.sigValDrafts[r.Name]
	if !ok {
		v := r.Raw
		ptr = &v
		inst.sigValDrafts[r.Name] = ptr
		inst.sigValSeeded[r.Name] = r.Raw
		return ptr
	}
	if inst.sigValSeeded[r.Name] != r.Raw {
		*ptr = r.Raw
		inst.sigValSeeded[r.Name] = r.Raw
		// Programmatic write to an interactive binding — tell the frontend
		// to drop its cached buffer (the setEndpoint idiom).
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(ptr)
	}
	return ptr
}

// evictSignalDrafts drops drafts for names no longer rendered, so a deleted
// and re-added signal starts from the store value, not a stale edit.
func (inst *PlayApp) evictSignalDrafts(rows []signalChromeRow) {
	if len(inst.sigValDrafts) == 0 {
		return
	}
	present := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		present[r.Name] = struct{}{}
	}
	for name := range inst.sigValDrafts {
		if _, ok := present[name]; !ok {
			delete(inst.sigValDrafts, name)
			delete(inst.sigValSeeded, name)
		}
	}
}

func joinNodeIDs(ids []NodeID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = string(id)
	}
	return strings.Join(ss, ", ")
}

// resultPanels lists the PanelI result panels for the channel inventory +
// eligibility (4c) — since slice 6a read off the tab registry (chrome tabs
// carry no PanelI, SD7), so it covers every registered panel, including an
// embedder's.
func (inst *PlayApp) resultPanels() []PanelI {
	return inst.tabs.panels()
}

// channelInventory is the one-line panel × channel summary atop the Graph view
// (4c) — the channel model made visible, read straight off Channels().
func (inst *PlayApp) channelInventory() string {
	parts := make([]string, 0, 4)
	for _, p := range inst.resultPanels() {
		chs := make([]string, 0, 2)
		for _, spec := range p.Channels() {
			chs = append(chs, string(spec.ID))
		}
		parts = append(parts, fmt.Sprintf("%s: %s", p.ID(), strings.Join(chs, "+")))
	}
	return "channels — " + strings.Join(parts, " · ")
}

// nodeChannelEligibility returns the notable channels a node could fill, inferred
// statically from its SQL (4c): a _tl_time projection ⇒ Timeline events, a
// _tl_band_from projection ⇒ Timeline bands. Every node fills the universal main
// channel (Table/Projection/Detail) when observed, so that is omitted. This is a
// heuristic on the SQL text — no execution — so a contract column named only in a
// WHERE could false-positive; the accurate check is AcceptForChannel at render.
func nodeChannelEligibility(n splitNode) []string {
	out := make([]string, 0, 2)
	if strings.Contains(n.SQL, timelineSlotTime) {
		out = append(out, "Timeline·events")
	}
	if strings.Contains(n.SQL, timelineSlotBandFrom) {
		out = append(out, "Timeline·bands")
	}
	return out
}
