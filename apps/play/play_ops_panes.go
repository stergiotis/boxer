package play

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// play_ops_panes.go is ADR-0270 M3: list_panes and bind_pane. A pane's
// verdict is the dock strip's (play_tab_marks.go) — the panel's own reason
// and the signals it writes that the buffer reads — so the agent and the
// person read the same answer.

const (
	opListPanes = "list_panes"
	opBindPane  = "bind_pane"
)

// PaneDrawE is whether a pane can draw what it would be handed.
type PaneDrawE uint8

const (
	// PaneDrawUnknown — nothing to judge yet: no run, or a node that has
	// not executed.
	PaneDrawUnknown PaneDrawE = iota
	PaneDrawYes
	PaneDrawNo
)

// OpEnumNames names the values on the model's side (opjson).
func (PaneDrawE) OpEnumNames() []string { return []string{"unknown", "yes", "no"} }

// PaneState is one pane as list_panes reports it.
type PaneState struct {
	Name     string    `desc:"the pane's name, as show_pane and bind_pane take it"`
	Title    string    `desc:"the pane's title"`
	Bindable bool      `desc:"whether bind_pane can point the pane at a split node"`
	Raised   bool      `desc:"whether the pane is the one play last raised"`
	BoundTo  string    `desc:"the split node the pane is bound to; empty when it draws the main result"`
	Draws    PaneDrawE `desc:"whether the pane can draw what it would be handed"`
	Reason   string    `desc:"the pane's own words for why it cannot draw, or what it waits for"`
	Writes   []string  `desc:"the signals the pane publishes when the person uses it"`
	Drives   []string  `desc:"the written signals the buffer reads"`
	Blocks   []string  `desc:"the written signals the buffer reads that nothing has filled: a run is refused until they are"`
}

// PaneList is list_panes' result.
type PaneList struct {
	Panes []PaneState `desc:"every pane, in the dock's order"`
	Nodes []string    `desc:"the split nodes of the last run, which bind_pane takes"`
}

// BindPaneArgs is bind_pane's argument.
type BindPaneArgs struct {
	Pane string `desc:"the pane's name, as list_panes gives it"`
	Node string `json:",omitzero" desc:"a split node of the last run; left out, the pane draws the main result again"`
}

// paneList reads the panes on the render goroutine, for the snapshot.
func paneList(p *PlayApp, mainSchema *arrow.Schema) (out PaneList) {
	raised, _ := p.tabs.slugForDockID(p.raisedTab)
	for i := range p.tabs.all() {
		spec := &p.tabs.all()[i]
		ps := PaneState{Name: spec.ID, Title: spec.Title, Bindable: spec.Panel != nil, Raised: spec.ID == raised,
			BoundTo: string(p.tabBindings[spec.ID])}
		// What the pane is handed: a bound node's result, else the main one.
		schema := mainSchema
		if node, bound := p.resolvedNodes[spec.ID]; bound && node != p.activeNodeID() {
			if view, ok := p.boundViews[node]; ok {
				schema = view.schema
			}
		}
		v := p.tabVerdictFor(spec, &TabFrame{Schema: schema})
		if spec.Panel != nil {
			ps.Draws, ps.Reason = paneDraws(spec.Panel, v)
		}
		for _, w := range declaredWrites(spec) {
			ps.Writes = append(ps.Writes, string(w))
		}
		ps.Drives, ps.Blocks = signalRelation(spec, v)
		out.Panes = append(out.Panes, ps)
	}
	for _, n := range p.currentSplit.Nodes {
		out.Nodes = append(out.Nodes, string(n.ID))
	}
	return
}

// paneDraws turns the strip's rejection ladder into a three-way answer.
// paneReject speaks only when a pane rejects, so acceptance is read off the
// offers: a pane draws when every required channel was offered a real
// schema and none rejected it; with any channel unoffered or pending it is
// unknown.
func paneDraws(panel PanelI, in tabVerdict) (draws PaneDrawE, reason string) {
	reason, rejected := paneReject(panel, in)
	if rejected && reason != "" {
		return PaneDrawNo, reason
	}
	for _, ch := range panel.Channels() {
		if !ch.Required {
			continue
		}
		if state, _ := requiredOffer(ch.ID, in); state != offerSchema {
			return PaneDrawUnknown, reason
		}
	}
	return PaneDrawYes, ""
}

// addPaneOps declares list_panes and bind_pane; playOps' initializer calls
// it.
func addPaneOps(playOps *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Query(playOps, app.OperationSpec{Name: opListPanes, Version: 1,
		Summary: "list the panes: whether each can draw the result and why not, and the signals it writes",
		Reads:   []string{opsResPanes, opsResResult}, Agents: true},
		func(sn opsSnap, _ appops.None) (PaneList, error) {
			if !sn.mounted {
				return PaneList{}, app.RefuseOperation("the window has not mounted")
			}
			return sn.panes, nil
		})
	appops.Command(playOps, app.OperationSpec{Name: opBindPane, Version: 1,
		Summary: "bind a pane to a split node of the last run, or back to the main result",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResPanes}, Agents: true,
		Gesture: "the pane's toggle beside the node in the Graph view"},
		func(inst *PlayLauncher, _ app.OperationCall, in BindPaneArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			var spec *TabSpec
			for i := range p.tabs.all() {
				if p.tabs.all()[i].ID == in.Pane {
					spec = &p.tabs.all()[i]
				}
			}
			switch {
			case spec == nil:
				return appops.None{}, app.RefuseOperation("no pane " + in.Pane)
			case spec.Panel == nil:
				return appops.None{}, app.RefuseOperation(in.Pane + " draws no result, so it binds to no node")
			case in.Node == "":
				p.unbindTab(spec.ID)
				return appops.None{}, nil
			}
			if _, ok := findSplitNode(p.currentSplit, NodeID(in.Node)); !ok {
				return appops.None{}, app.RefuseOperation("the last run has no split node " + in.Node + "; list_panes names them")
			}
			p.bindTab(spec.ID, NodeID(in.Node))
			return appops.None{}, nil
		})
}
