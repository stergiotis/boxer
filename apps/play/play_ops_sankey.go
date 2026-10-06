package play

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	sankeyview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey/view"
)

// The Sankey pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05). The read is the last layout: the conserved total (the
// outflow of the sources, not the sum of the ribbons, which overstates a
// staged diagram by about its stage count), each node's in and out, each
// flow's share, and what the build summed, dropped or capped. The command
// sets the mode; the fill, gradient and label switches change no reading
// and stay the person's. A pin is the pane's own: select_sankey_node pins a
// node or a ribbon, and a node's id becomes selection_key.

const (
	opGetSankey           = "get_sankey"
	opSetSankeyOptions    = "set_sankey_options"
	opSelectSankeyNode    = "select_sankey_node"
	sankeyPaneId          = "sankey"
	opsResSankey          = sankeyPaneId
	sankeyReadDefaultRows = 100
	sankeyReadDefaultLink = 200
	sankeyReadMaxNames    = 50
)

// SankeyNodeReading is one node of the laid-out diagram.
type SankeyNodeReading struct {
	Id    string   `desc:"the node id, cut at 128 bytes; give it back to select_sankey_node"`
	Label string   `json:",omitzero" desc:"its label, when it differs from the id"`
	Stage int32    `desc:"its column, 0 at the left"`
	Index int32    `desc:"its place within the column, 0 at the top"`
	In    *float64 `json:",omitzero" desc:"the summed inflow"`
	Out   *float64 `json:",omitzero" desc:"the summed outflow"`
	Value *float64 `desc:"max(in, out), what the bar's height encodes"`
}

// SankeyFlowReading is one ribbon.
type SankeyFlowReading struct {
	Source string   `desc:"the source node id"`
	Target string   `desc:"the target node id"`
	Value  *float64 `desc:"the quantity the ribbon carries, duplicates summed"`
	Share  *float64 `json:",omitzero" desc:"value as a fraction of the conserved total"`
	Label  string   `json:",omitzero" desc:"the flow's label"`
}

// SankeyPin is the pane's pinned node or ribbon.
type SankeyPin struct {
	Node   string `json:",omitzero" desc:"the pinned node's id"`
	Source string `json:",omitzero" desc:"the pinned ribbon's source"`
	Target string `json:",omitzero" desc:"the pinned ribbon's target"`
}

// SankeyReading is get_sankey's result.
type SankeyReading struct {
	Drawn           PaneDraw            `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Mode            string              `desc:"the mode setting: auto, sankey or alluvial"`
	ModeUsed        string              `json:",omitzero" desc:"the mode laid out: sankey (stages derived) or alluvial (stages given)"`
	NotAlluvial     string              `json:",omitzero" desc:"why alluvial fell back to sankey"`
	Nodes           int32               `json:",omitzero" desc:"nodes in the diagram"`
	Flows           int32               `json:",omitzero" desc:"ribbons in the diagram"`
	Stages          int32               `json:",omitzero" desc:"columns"`
	Total           *float64            `json:",omitzero" desc:"the conserved total: the outflow of every node with no inflow; shares are of it"`
	Summed          int32               `json:",omitzero" desc:"duplicate (source, target) rows summed into one ribbon"`
	DroppedValue    int32               `json:",omitzero" desc:"rows dropped for a missing endpoint or a value that is not positive"`
	DroppedSelf     int32               `json:",omitzero" desc:"self-flows dropped"`
	DroppedEndpoint int32               `json:",omitzero" desc:"rows dropped because the node cap was reached"`
	Capped          bool                `json:",omitzero" desc:"the diagram reached the cap of 300 nodes or 1,500 flows"`
	ThinFlows       int32               `json:",omitzero" desc:"ribbons too thin to see; aggregate them in SQL"`
	Unbalanced      []string            `json:",omitzero" desc:"nodes whose inflow and outflow disagree, at most 50"`
	LayoutError     string              `json:",omitzero" desc:"why the flows could not be laid out"`
	FlowsError      string              `json:",omitzero" desc:"the flows CTE's error"`
	NodesError      string              `json:",omitzero" desc:"the nodes CTE's error"`
	Loading         bool                `json:",omitzero" desc:"a lane is still running; the reading is of what it served before"`
	Pinned          *SankeyPin          `json:",omitzero" desc:"the pinned node or ribbon"`
	NodeList        []SankeyNodeReading `json:",omitzero" desc:"a page of the nodes, by stage and place"`
	MoreNodes       int32               `json:",omitzero" desc:"nodes past the page; a page also stops at about 24 KiB of text"`
	FlowList        []SankeyFlowReading `json:",omitzero" desc:"a page of the ribbons, largest first"`
	MoreFlows       int32               `json:",omitzero" desc:"ribbons past the page; a page also stops at about 24 KiB of text"`
}

// GetSankeyArgs pages get_sankey's nodes and ribbons.
type GetSankeyArgs struct {
	Offset     int32 `json:",omitzero" desc:"nodes to skip"`
	Limit      int32 `json:",omitzero" desc:"nodes to list, 100 by default and at most 500"`
	FlowOffset int32 `json:",omitzero" desc:"ribbons to skip"`
	FlowLimit  int32 `json:",omitzero" desc:"ribbons to list, 200 by default and at most 500"`
}

// SetSankeyOptionsArgs is set_sankey_options' argument.
type SetSankeyOptionsArgs struct {
	Mode *string `json:",omitzero" desc:"auto (alluvial when every node has a stage), sankey or alluvial"`
}

// SelectSankeyNodeArgs is select_sankey_node's argument.
type SelectSankeyNodeArgs struct {
	Node   string `json:",omitzero" desc:"the node to pin; its id becomes selection_key"`
	Source string `json:",omitzero" desc:"or the source of the ribbon to pin, with target; a ribbon leaves selection_key empty"`
	Target string `json:",omitzero" desc:"the target of the ribbon to pin"`
	Clear  bool   `json:",omitzero" desc:"drop the pin; selection_key becomes empty"`
}

var sankeyChoiceNames = []string{"auto", "sankey", "alluvial"}

func sankeyModeName(m sankey.Mode) string {
	if m == sankey.ModeAlluvial {
		return "alluvial"
	}
	return "sankey"
}

// sankeyOpsView is what get_sankey reads: the layout, which a re-layout
// replaces rather than edits, and the pane's state.
type sankeyOpsView struct {
	layout       *sankey.Layout
	stats        sankeyStats
	choice       sankeyChoiceE
	modeUsed     sankey.Mode
	modeFallback string
	layoutErr    error
	flowsErr     error
	nodesErr     error
	loading      bool
	selected     sankeyview.Hit
}

func (inst *PlayApp) sankeyView() sankeyOpsView {
	d := inst.sankeyDriver
	return sankeyOpsView{layout: d.layout, stats: d.stats, choice: d.choice, modeUsed: d.modeUsed,
		modeFallback: d.modeFallback, layoutErr: d.layoutErr, flowsErr: d.flowsErr, nodesErr: d.nodesErr,
		loading: d.flowsLoading || d.nodesLoading, selected: d.selected}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return truncateBytes(err.Error(), opsStatusMaxBytes)
}

// pinOf names a hit by ids.
func sankeyPinOf(lay *sankey.Layout, h sankeyview.Hit) *SankeyPin {
	if lay == nil {
		return nil
	}
	switch h.Kind {
	case sankeyview.HitNode:
		if i := h.Node(); i >= 0 && i < len(lay.Nodes) {
			return &SankeyPin{Node: hierPathLabel(lay.Nodes[i].ID)}
		}
	case sankeyview.HitLink:
		if i := h.Link(); i >= 0 && i < len(lay.Links) {
			l := &lay.Links[i]
			return &SankeyPin{Source: hierPathLabel(lay.Nodes[l.Source].ID), Target: hierPathLabel(lay.Nodes[l.Target].ID)}
		}
	}
	return nil
}

func sankeyReading(sn *opsSnap, in GetSankeyArgs) (out SankeyReading, err error) {
	d, readable, err := paneDrawOf(sn, sankeyPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.sankey
	out = SankeyReading{Drawn: d, Mode: sankeyChoiceNames[min(int(v.choice), len(sankeyChoiceNames)-1)],
		LayoutError: errText(v.layoutErr), FlowsError: errText(v.flowsErr), NodesError: errText(v.nodesErr), Loading: v.loading}
	if !readable {
		return
	}
	st := v.stats
	out.Nodes, out.Flows = int32(st.nodes), int32(st.links)
	out.Summed, out.DroppedValue, out.DroppedSelf, out.DroppedEndpoint = int32(st.collapsed), int32(st.droppedValue),
		int32(st.droppedSelf), int32(st.droppedEndpoint)
	out.Capped = st.capped
	lay := v.layout
	if lay == nil {
		return
	}
	out.ModeUsed, out.NotAlluvial = sankeyModeName(v.modeUsed), truncateBytes(v.modeFallback, opsStatusMaxBytes)
	out.Stages, out.Total, out.ThinFlows = int32(lay.Stages), finite(lay.Report.Total), int32(lay.Report.ThinLinks)
	for _, id := range lay.Report.NonConserving[:min(len(lay.Report.NonConserving), sankeyReadMaxNames)] {
		out.Unbalanced = append(out.Unbalanced, opsLabel(id))
	}
	out.Pinned = sankeyPinOf(lay, v.selected)

	order := make([]int, len(lay.Nodes))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		na, nb := &lay.Nodes[a], &lay.Nodes[b]
		if c := cmp.Compare(na.Stage, nb.Stage); c != 0 {
			return c
		}
		return cmp.Compare(na.Index, nb.Index)
	})
	start, end, err := graphPage(in.Offset, in.Limit, sankeyReadDefaultRows, len(order), "node")
	if err != nil {
		return
	}
	used := 0
	for k, i := range order[start:end] {
		n := &lay.Nodes[i]
		r := SankeyNodeReading{Id: hierPathLabel(n.ID), Stage: int32(n.Stage), Index: int32(n.Index),
			In: positiveOrNil(n.In), Out: positiveOrNil(n.Out), Value: finite(n.Value)}
		if n.Label != n.ID {
			r.Label = opsLabel(n.Label)
		}
		if used += pageBytes(r.Id, r.Label); used > graphReadMaxBytes && k > 0 {
			end = start + k
			break
		}
		out.NodeList = append(out.NodeList, r)
	}
	out.MoreNodes = int32(len(order) - end)

	links := make([]int, len(lay.Links))
	for i := range links {
		links[i] = i
	}
	slices.SortStableFunc(links, func(a, b int) int { return cmp.Compare(lay.Links[b].Value, lay.Links[a].Value) })
	start, end, err = graphPage(in.FlowOffset, in.FlowLimit, sankeyReadDefaultLink, len(links), "flow")
	if err != nil {
		return
	}
	used = 0
	for k, i := range links[start:end] {
		l := &lay.Links[i]
		r := SankeyFlowReading{Source: hierPathLabel(lay.Nodes[l.Source].ID), Target: hierPathLabel(lay.Nodes[l.Target].ID),
			Value: finite(l.Value), Label: opsLabel(l.Label)}
		if lay.Report.Total > 0 {
			r.Share = finite(l.Value / lay.Report.Total)
		}
		if used += pageBytes(r.Source, r.Target, r.Label); used > graphReadMaxBytes && k > 0 {
			end = start + k
			break
		}
		out.FlowList = append(out.FlowList, r)
	}
	out.MoreFlows = int32(len(links) - end)
	return
}

// selectArgsOf is a click's hit as select_sankey_node's argument.
func (inst *SankeyDriver) selectArgsOf(h sankeyview.Hit) (in SelectSankeyNodeArgs) {
	lay := inst.layout
	switch {
	case lay == nil:
		in.Clear = true
	case h.Kind == sankeyview.HitNode && h.Node() >= 0 && h.Node() < len(lay.Nodes):
		in.Node = lay.Nodes[h.Node()].ID
	case h.Kind == sankeyview.HitLink && h.Link() >= 0 && h.Link() < len(lay.Links):
		l := &lay.Links[h.Link()]
		in.Source, in.Target = lay.Nodes[l.Source].ID, lay.Nodes[l.Target].ID
	default:
		// A click on empty area clears the pin.
		in.Clear = true
	}
	return
}

// pinFor resolves a select against the layout drawn.
func (inst *SankeyDriver) pinFor(in SelectSankeyNodeArgs) (h sankeyview.Hit, err error) {
	if in.Clear {
		if in.Node != "" || in.Source != "" || in.Target != "" {
			return h, app.RefuseOperation("clear drops the pin; give it alone, or give a node or a ribbon without it")
		}
		return sankeyview.Hit{}, nil
	}
	lay := inst.layout
	if lay == nil {
		return h, app.RefuseOperation("the Sankey pane has no diagram to pin in: run a flows query, and show_pane sankey")
	}
	nodeOf := func(id string) int {
		for i := range lay.Nodes {
			if hierLabelNames(id, lay.Nodes[i].ID) {
				return i
			}
		}
		return -1
	}
	switch {
	case in.Node != "" && in.Source == "" && in.Target == "":
		i := nodeOf(in.Node)
		if i < 0 {
			return h, app.RefuseOperation("no node " + strconv.Quote(in.Node) + " is drawn; get_sankey lists them")
		}
		return sankeyview.NodeHit(i), nil
	case in.Node == "" && in.Source != "" && in.Target != "":
		s, t := nodeOf(in.Source), nodeOf(in.Target)
		for i := range lay.Links {
			if s >= 0 && lay.Links[i].Source == s && lay.Links[i].Target == t {
				return sankeyview.LinkHit(i), nil
			}
		}
		return h, app.RefuseOperation("no ribbon runs from " + strconv.Quote(in.Source) + " to " + strconv.Quote(in.Target))
	}
	return h, app.RefuseOperation("name a node, or a ribbon by its source and target, or clear the pin")
}

// requestSelect is a click: through select_sankey_node when play's
// launcher routes it, directly through the pane's emitter otherwise.
func (inst *SankeyDriver) requestSelect(in SelectSankeyNodeArgs, emit SignalEmitterI) {
	if inst.onSelect != nil {
		inst.onSelect(in)
		return
	}
	h, err := inst.pinFor(in)
	if err != nil {
		return
	}
	inst.selected = h
	if emit != nil {
		emit.Emit(signalSelectionKey, inst.selectedNodeID())
	}
}

// repin carries the pin across a re-layout by ids: a node by its id, a
// ribbon by its two ends. The layout's indexes name other nodes once the
// diagram changes. A pinned node that is gone empties selection_key.
func (inst *SankeyDriver) repin(prev *sankey.Layout, emit SignalEmitterI) {
	if inst.selected.None() {
		return
	}
	pin := sankeyPinOf(prev, inst.selected)
	wasNode := inst.selected.Kind == sankeyview.HitNode
	inst.selected = sankeyview.Hit{}
	if pin != nil && inst.layout != nil {
		if h, err := inst.pinFor(SelectSankeyNodeArgs{Node: pin.Node, Source: pin.Source, Target: pin.Target}); err == nil {
			inst.selected = h
			return
		}
	}
	if wasNode && emit != nil {
		emit.Emit(signalSelectionKey, "")
	}
}

// selectSankeyNode is select_sankey_node: the pin and selection_key
// together.
func (inst *PlayApp) selectSankeyNode(in SelectSankeyNodeArgs, writer string) (err error) {
	d := inst.sankeyDriver
	h, err := d.pinFor(in)
	if err != nil {
		return
	}
	d.selected = h
	inst.graph.setSignalRawFrom(signalSelectionKey, d.selectedNodeID(), writer)
	return
}

func (inst *SankeyDriver) setOptions(in SetSankeyOptionsArgs) error {
	if in.Mode == nil {
		return noOptionsRefusal(sankeyPaneId, "mode")
	}
	i := slices.Index(sankeyChoiceNames, strings.TrimSpace(*in.Mode))
	if i < 0 {
		return app.RefuseOperation("mode is auto, sankey or alluvial")
	}
	inst.choice = sankeyChoiceE(i)
	return nil
}

// sankeyOptionsDigest is the sankey resource: the mode and the pin.
func sankeyOptionsDigest(p *PlayApp) string {
	d := p.sankeyDriver
	if d == nil {
		return ""
	}
	return "mode=" + strconv.Itoa(int(d.choice)) + "|pin=" + strconv.Itoa(int(d.selected.Kind)) + ":" +
		strconv.Itoa(int(d.selected.Index))
}

func addSankeyOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[SankeyReading, GetSankeyArgs, SetSankeyOptionsArgs]{
		pane:     sankeyPaneId,
		resource: "the Sankey pane's settings: the mode and the pinned node or ribbon",
		digest:   sankeyOptionsDigest,
		get:      opGetSankey,
		getSummary: "read what the Sankey pane last laid out from the flows and nodes CTEs: the conserved total, " +
			"each node's inflow and outflow, each flow's share, the mode used, the unbalanced nodes, " +
			"and what the build summed, dropped or capped",
		set:        opSetSankeyOptions,
		setSummary: "set the Sankey pane's mode: auto, sankey (stages derived) or alluvial (stages given)",
		gesture:    "the mode switch above the diagram",
		follows: []string{"the pane lays the diagram out again from its next frame; the CTEs are not rerun",
			"alluvial without a stage for every node falls back to sankey, and get_sankey says why"},
		read:  sankeyReading,
		apply: func(p *PlayApp, in SetSankeyOptionsArgs) error { return p.sankeyDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectSankeyNode, Version: 1,
		Summary: "pin a node or a ribbon of the Sankey pane, or drop the pin; a pinned node's id becomes selection_key",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResSankey}, Agents: true,
		Gesture: "clicking a bar or a ribbon of the diagram",
		Follows: []string{"Live reruns a query that reads selection_key"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectSankeyNodeArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectSankeyNode(in, paneSignalWriter(call, sankeyPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
