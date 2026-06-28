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
		for rt := range c.RichTextLabel("Run a query to see its node graph.") {
			rt.Small().Weak()
		}
		return
	}
	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel(fmt.Sprintf("%d node(s) · sink: %s", len(split.Nodes), split.Sink)) {
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
	if n.Kind == splitNodeStatement {
		kind = "sink"
	}
	header := fmt.Sprintf("%s · %s", n.ID, kind)
	for range c.CollapsingHeader(ids.PrepareStr("graphNode-"+string(n.ID)),
		c.WidgetText().Text(header).Keep()).DefaultOpen(true).KeepIter() {
		for range c.Vertical().KeepIter() {
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
