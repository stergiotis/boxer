package play

// get_query_graph and observe_node: the Graph pane for an agent. The read
// is the last run's split — each node's kind, what it reads, which panes it
// feeds, its SQL — and the system graph's edges as text, so the write-back
// loop (a pane writes a signal a node reads) can be followed without the
// drawing. observe_node is the pane's "observe in panels": every panel
// without a binding draws the node's result.

import (
	"maps"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const (
	opGetQueryGraph = "get_query_graph"
	opObserveNode   = "observe_node"
)

// Bounds on get_query_graph's reading.
const (
	queryGraphMaxNodeSql = 2 << 10
	queryGraphMaxBytes   = 16 << 10
)

// QueryNode is one node of the split.
type QueryNode struct {
	Id           string   `desc:"the node, as bind_pane, observe_node and sample_rows name it"`
	Kind         string   `desc:"cte, recursive (lifted from WITH RECURSIVE), sink (the statement panels draw) or client (computed in play by a ts* call, never sent)"`
	Engine       string   `json:",omitzero" desc:"for a client node, what computes it and what is sent in its place"`
	DependsOn    []string `json:",omitzero" desc:"the nodes it reads"`
	Reads        []string `json:",omitzero" desc:"the parameters and signals it reads"`
	Pinned       []string `json:",omitzero" desc:"those of its reads a SET line binds, which no signal changes"`
	AlsoFills    []string `json:",omitzero" desc:"the channels it can fill besides the panels' main one, such as the Timeline's events"`
	Panes        []string `json:",omitzero" desc:"the panels it feeds now: bound to it, or following it while it is observed or the sink"`
	Sql          string   `desc:"its SQL, cut at 2 KiB"`
	SqlTruncated bool     `json:",omitzero" desc:"true when the cut shortened sql"`
}

// QueryGraphEdge is one edge of the system graph.
type QueryGraphEdge struct {
	From string `desc:"the source: node/<id>, sig/<name>, const/<name> (a SET-bound parameter), pnode/<name> (a pane's own query: bands, the map raster) or tab/<pane>"`
	To   string `desc:"the target, in the same form"`
	Kind string `desc:"data (node to node), constant, signal (a signal read), feeds (into a pane) or writes (a pane writing a signal)"`
}

// QueryBinding is one pane bound to a node.
type QueryBinding struct {
	Pane string `desc:"the pane"`
	Node string `desc:"the node it is bound to"`
}

// QueryGraph is get_query_graph's result.
type QueryGraph struct {
	Sink       string           `json:",omitzero" desc:"the node the panels draw unless one is observed or bound"`
	SplitError string           `json:",omitzero" desc:"why the buffer did not split into nodes; the graph is then the last split that did"`
	Observed   string           `json:",omitzero" desc:"the node every unbound panel draws instead of the sink, set by observe_node"`
	Bindings   []QueryBinding   `json:",omitzero" desc:"panes bound to one node with bind_pane"`
	Nodes      []QueryNode      `desc:"the split's nodes: the top-level CTEs and the statement, in buffer order"`
	Edges      []QueryGraphEdge `desc:"the system graph's edges, as the Graph pane draws them"`
	Unfilled   []string         `json:",omitzero" desc:"signals a node reads that nothing holds; a run needs them"`
	Truncated  bool             `desc:"true when the 16 KiB bound left out nodes' SQL or edges"`
}

// ObserveNodeArgs is observe_node's argument.
type ObserveNodeArgs struct {
	Node string `json:",omitzero" desc:"the node every unbound panel should draw, as get_query_graph names it; left out, or the sink, the panels draw the sink again"`
}

// queryGraphKey identifies what the graph's reading was built from.
type queryGraphKey struct {
	sql      string
	panes    string
	signals  uint64
	result   ResultID
	bandsSql string
	splitErr string
}

// queryGraphView is the reading, copied when its inputs move.
func (inst *PlayApp) queryGraphView() (out *QueryGraph) {
	key := queryGraphKey{sql: inst.sql, panes: inst.paneDigest(), signals: inst.graph.signals().Revision(),
		result: inst.frameResult, bandsSql: inst.timelineBandsSql}
	if inst.splitErr != nil {
		key.splitErr = inst.splitErr.Error()
	}
	cache := &inst.paneViews
	if cache.queryGraph != nil && cache.queryGraphKey == key {
		return cache.queryGraph
	}
	out = inst.buildQueryGraph()
	cache.queryGraph, cache.queryGraphKey = out, key
	return
}

// buildQueryGraph reads the split and the system graph model.
func (inst *PlayApp) buildQueryGraph() (out *QueryGraph) {
	out = &QueryGraph{}
	split := inst.currentSplit
	out.Sink = string(split.Sink)
	if inst.splitErr != nil {
		out.SplitError = truncateBytes(inst.splitErr.Error(), opsStatusMaxBytes)
	}
	if inst.observedNode != "" && inst.observedNode != split.Sink {
		if _, ok := findSplitNode(split, inst.observedNode); ok {
			out.Observed = string(inst.observedNode)
		}
	}
	for _, pane := range slices.Sorted(maps.Keys(inst.tabBindings)) {
		out.Bindings = append(out.Bindings, QueryBinding{Pane: pane, Node: string(inst.tabBindings[pane])})
	}
	feeds := make(map[NodeID][]string, len(split.Nodes))
	for _, spec := range inst.tabs.all() {
		if spec.Panel == nil || spec.Frameless {
			continue
		}
		n := inst.resolvedTabNode(spec.ID)
		feeds[n] = append(feeds[n], spec.ID)
	}
	budget := queryGraphMaxBytes
	for _, n := range split.Nodes {
		qn := QueryNode{Id: string(n.ID), Kind: "cte", DependsOn: nodeIDStrings(n.DependsOn), AlsoFills: nodeChannelEligibility(n),
			Panes: feeds[n.ID]}
		switch {
		case n.Client != nil:
			qn.Kind, qn.Engine = "client", clientNodeCaption(n.Client)
		case n.Kind == splitNodeStatement:
			qn.Kind = "sink"
		case n.Recursive:
			qn.Kind = "recursive"
		}
		for _, r := range n.Reads {
			qn.Reads = append(qn.Reads, r)
			if _, bound := inst.paramSyncedValues[r]; bound {
				qn.Pinned = append(qn.Pinned, r)
			}
		}
		qn.Sql = truncateBytes(n.SQL, queryGraphMaxNodeSql)
		qn.SqlTruncated = len(qn.Sql) < len(n.SQL)
		if len(qn.Sql) > budget {
			qn.Sql, qn.SqlTruncated, out.Truncated = "", true, true
		}
		budget -= len(qn.Sql) + 64
		out.Nodes = append(out.Nodes, qn)
	}
	m, meta := inst.buildSystemGraphModel()
	for _, e := range m.Edges {
		if budget < 0 {
			out.Truncated = true
			break
		}
		edge := QueryGraphEdge{From: e.From, To: e.To, Kind: systemEdgeKind(e.From, e.To)}
		budget -= len(edge.From) + len(edge.To) + 24
		out.Edges = append(out.Edges, edge)
	}
	for id, kind := range meta {
		if kind == vizUnfilled {
			out.Unfilled = append(out.Unfilled, strings.TrimPrefix(id, "sig/"))
		}
	}
	slices.Sort(out.Unfilled)
	return
}

// systemEdgeKind names an edge of the system graph by its endpoints' kinds.
func systemEdgeKind(from string, to string) (kind string) {
	switch {
	case strings.HasPrefix(from, "tab/"):
		return "writes"
	case strings.HasPrefix(to, "tab/"):
		return "feeds"
	case strings.HasPrefix(from, "const/"):
		return "constant"
	case strings.HasPrefix(from, "sig/"):
		return "signal"
	}
	return "data"
}

func nodeIDStrings(ids []NodeID) (out []string) {
	for _, id := range ids {
		out = append(out, string(id))
	}
	return
}

// observeNode sets the node every unbound panel draws; "" or the sink
// returns them to the sink. A node the split lacks is refused, as bind_pane
// refuses one.
func (inst *PlayApp) observeNode(node NodeID) (err error) {
	if node == "" || node == inst.currentSplit.Sink {
		inst.observedNode = ""
		return
	}
	if _, found := findSplitNode(inst.currentSplit, node); !found {
		names := make([]string, 0, len(inst.currentSplit.Nodes))
		for _, n := range inst.currentSplit.Nodes {
			names = append(names, string(n.ID))
		}
		return app.ConflictOperation("no node " + string(node) + " in the buffer; it has " + strings.Join(names, ", "))
	}
	inst.observedNode = node
	return
}

// refuseAgentObserve checks the statement an observed node's lane would
// send, the way run checks the buffer at the call: the class ceiling, then
// the agent limits. A node the sink never reads would otherwise reach the
// endpoint only through the observe. The sink and "" send nothing new.
func (inst *PlayApp) refuseAgentObserve(obo *app.OnBehalfOf, node NodeID) (err error) {
	split := inst.currentSplit
	if node == "" || node == split.Sink {
		return
	}
	n, found := findSplitNode(split, node)
	if !found {
		return
	}
	sql := compileNodeFor(split, n, inst.lastRunBound, inst.frameSig).SQL
	if reason := inst.exprCeilingRefusal(sql); reason != "" {
		return app.RefuseOperation(reason)
	}
	return inst.refuseAgentRunOf(obo, sql)
}

// personObserveNode is the Graph pane's "observe in panels" (ADR-0270
// §SD6): through observe_node, logged as the person's.
func (inst *PlayApp) personObserveNode(node NodeID) {
	playGesture(inst, opObserveNode, ObserveNodeArgs{Node: string(node)}, func() { inst.observedNode = node })
}

func addQueryGraphOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: node SQL quotes the person's buffer.
	appops.Query(s, app.OperationSpec{Name: opGetQueryGraph, Version: 1,
		Summary: "read the query graph: the buffer's nodes with their kind, what each reads, the panes each feeds and its SQL, the observed node, the bindings, and the system graph's edges",
		Reads:   []string{opsResSql, opsResResult, opsResPanes, opsResSignals}, Agents: true, Untrusted: true,
		Follows: []string{"the nodes are the last run's split; a buffer changed since splits again on the next run",
			"observe_node points every unbound panel at a node; bind_pane points one panel"}},
		func(sn opsSnap, in appops.None) (out QueryGraph, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			if sn.queryGraph == nil {
				return out, app.RefuseOperation("the window has no query graph yet")
			}
			return *sn.queryGraph, nil
		})
	appops.Command(s, app.OperationSpec{Name: opObserveNode, Version: 1,
		Summary: "make every panel without a binding draw one node's result, or the sink's again",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResPanes}, Reads: []string{opsResSql}, Agents: true,
		Gesture: "the observe in panels button of a node in the Graph pane",
		Follows: []string{"the node's statement is checked at the call against the class ceiling and the agent limits, as run checks the buffer; its lane then runs under the task's mark",
			"describe_result and sample_rows without pane or node read the observed node's result"}},
		func(inst *PlayLauncher, call app.OperationCall, in ObserveNodeArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if call.OnBehalfOf != nil {
				if err := p.refuseAgentObserve(call.OnBehalfOf, NodeID(in.Node)); err != nil {
					return appops.None{}, err
				}
			}
			if err := p.observeNode(NodeID(in.Node)); err != nil {
				return appops.None{}, err
			}
			// The node runs on its own lane; the mark keeps that run under
			// the agent limits until the person edits (ADR-0270 §SD3).
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
