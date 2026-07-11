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

func joinNodeIDs(ids []NodeID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = string(id)
	}
	return strings.Join(ss, ", ")
}

// resultPanels lists the PanelI result panels, for the channel inventory +
// eligibility (4c). Editor/Preview/History/Snippets/Graph are chrome (SD7), not
// panels. Constructed cheaply; Channels() needs no live data (the timeline
// driver may be nil here — Channels() does not touch it).
func (inst *PlayApp) resultPanels() []PanelI {
	return []PanelI{
		tablePanel{app: inst},
		projectionPanel{app: inst},
		detailPanel{app: inst},
		timelinePanel{driver: inst.timeline},
	}
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
