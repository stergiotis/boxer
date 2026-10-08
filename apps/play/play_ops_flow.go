package play

// play_ops_flow.go: two reads of a statement's structure that leave the
// Flow pane where the person put it (ADR-0270, update of 2026-10-05).
//
//   - sql_flow is the pane's two local lenses — the clause graph and the
//     column lineage — over a node of the buffer or a given statement. It
//     parses and runs nothing, so it works under any grant.
//   - explain_sql is the pane's remote lenses: the server's EXPLAIN of a
//     node or a statement, as text and optionally as the graph the pane
//     draws. It reaches the endpoint the statement routes to, so an agent's
//     call is checked as a run of that statement is (ADR-0270 §SD2) before
//     anything is sent, and the request carries the call's context.
//
// Both resolve a node against a fresh split of what run would ship, or of
// the sql argument, never against the person's caret.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
)

const (
	opSqlFlow    = "sql_flow"
	opExplainSql = "explain_sql"
)

// Bounds on what the flow reads return.
const (
	flowOpsMaxBytes   = 16 << 10
	flowOpsMaxDetail  = 200
	explainOpsTimeout = 15 * time.Second
)

// FlowArgs is sql_flow's argument.
type FlowArgs struct {
	Lens string `json:",omitzero" desc:"statement (the clause graph: sources, joins, filters, aggregate, projection, sort, limit, union) or lineage (where each output column of the SELECT list comes from); statement when left out"`
	Node string `json:",omitzero" desc:"the split node to derive, as list_panes names it (a CTE or the statement's sink); the sink when left out"`
	Sql  string `json:",omitzero" desc:"a statement to derive instead of the buffer; when left out, what run would ship"`
}

// FlowNode is one vertex of a flow graph.
type FlowNode struct {
	Id     string `desc:"the node's id, which edges name"`
	Kind   string `desc:"source table, cte, subquery or function; join, filter, aggregate, project, distinct, sort, limit, union, result; step (an EXPLAIN node); column source or column output (lineage)"`
	Label  string `desc:"its short label"`
	Detail string `json:",omitzero" desc:"the clause text or step detail, cut at 200 runes"`
	Start  int    `json:",omitzero" desc:"byte offset of the clause in the node's SQL; with end, zero when the node has no anchor"`
	End    int    `json:",omitzero" desc:"byte offset just past the clause"`
}

// FlowEdge is one directed edge of a flow graph.
type FlowEdge struct {
	From  string `desc:"the node data flows from"`
	To    string `desc:"the node it flows into"`
	Label string `json:",omitzero" desc:"l or r for a join's inputs"`
}

// FlowReading is sql_flow's result.
type FlowReading struct {
	Lens      string     `desc:"the lens derived"`
	Source    string     `desc:"buffer (what run would ship) or sql (the statement given)"`
	Node      string     `desc:"the split node derived"`
	Nodes     []string   `desc:"every node of the split, for picking another"`
	Sql       string     `json:",omitzero" desc:"the node's SQL, the text start and end index; cut at 4 KiB"`
	Graph     []FlowNode `json:",omitzero" desc:"the graph's nodes"`
	Edges     []FlowEdge `json:",omitzero" desc:"the graph's edges"`
	Capped    bool       `json:",omitzero" desc:"true when the derivation's own node or depth bound stopped it, so the graph is a prefix of the statement's structure"`
	Note      string     `json:",omitzero" desc:"a caveat of the derivation, such as a union traced by its first member"`
	Error     string     `json:",omitzero" desc:"why no graph could be derived"`
	Truncated bool       `json:",omitzero" desc:"true when the 16 KiB bound left nodes out; the edges listed join listed nodes only"`
}

// ExplainArgs is explain_sql's argument.
type ExplainArgs struct {
	Kind  string `desc:"ast, plan, pipeline, estimate (parts, rows and marks each table would read) or indexes (the plan with each read's index use)"`
	Node  string `json:",omitzero" desc:"the split node to explain, fused with the CTEs it reads; the sink when left out"`
	Sql   string `json:",omitzero" desc:"a statement to explain instead of the buffer; when left out, what run would ship"`
	Graph bool   `json:",omitzero" desc:"also return the output parsed into the graph the Flow pane draws"`
}

// ExplainResult is explain_sql's result.
type ExplainResult struct {
	Kind      string     `desc:"the EXPLAIN made"`
	Source    string     `desc:"buffer or sql"`
	Node      string     `desc:"the split node explained"`
	Lines     []string   `json:",omitzero" desc:"the server's output, one line per entry, indentation kept; a tabular kind's columns are tab-separated"`
	Graph     []FlowNode `json:",omitzero" desc:"with graph, the output as nodes"`
	Edges     []FlowEdge `json:",omitzero" desc:"with graph, its edges"`
	GraphNote string     `json:",omitzero" desc:"why the graph is missing or partial"`
	Truncated bool       `json:",omitzero" desc:"true when the 16 KiB bound cut the lines"`
	Confined  bool       `json:",omitzero" desc:"true when the statement reads confined data"`
}

// ResultConfined labels the outcome confined when the explained statement
// reads confined data: an EXPLAIN quotes its tables and literals.
func (inst ExplainResult) ResultConfined() bool { return inst.Confined }

var _ app.ConfinedResultI = ExplainResult{}

func addFlowOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: the graph quotes the statement, which get_state marks so.
	appops.Query(s, app.OperationSpec{Name: opSqlFlow, Version: 1,
		Summary: "derive a statement's clause graph or its column lineage, without running it or reaching the endpoint",
		Reads:   []string{opsResSql}, Agents: true, Untrusted: true,
		Follows: []string{"nothing runs and the Flow pane is unchanged; explain_sql asks the server for its plan"}},
		func(sn opsSnap, in FlowArgs) (FlowReading, error) {
			if !sn.mounted {
				return FlowReading{}, app.RefuseOperation("the window has not mounted")
			}
			return sqlFlow(sn.runSql, in)
		})
	appops.ExternalRead(s, app.OperationSpec{Name: opExplainSql, Version: 1,
		Summary: "ask the server to EXPLAIN a node of the buffer or a statement: syntax tree, plan, pipeline, read estimate or index use",
		Reads:   []string{opsResSql}, Agents: true, Untrusted: true,
		Follows: []string{"nothing is run but the EXPLAIN; the buffer, the result and the Flow pane are unchanged"}},
		func(sn opsSnap, call app.OperationCall, in ExplainArgs) (ExplainResult, error) {
			if !sn.mounted {
				return ExplainResult{}, app.RefuseOperation("the window has not mounted")
			}
			if sn.client == nil {
				return ExplainResult{}, app.RefuseOperation("the window has no endpoint")
			}
			return explainSql(sn.client, call.OnBehalfOf, sn.runSql, sn.state.Signals, in)
		})
}

// flowSource splits the statement a flow read derives: sql when given, else
// what run would ship, and picks the node.
func flowSource(runSql string, sql string, node string) (split splitResult, picked splitNode, source string, err error) {
	source = "buffer"
	text := runSql
	if strings.TrimSpace(sql) != "" {
		source, text = "sql", sql
	}
	if err = statementBounds(text); err != nil {
		return
	}
	split, serr := splitGraph(text)
	if serr != nil {
		err = app.RefuseOperation("the statement does not split into a query graph: " + truncateRunes(firstLine(serr.Error()), 200))
		return
	}
	want := NodeID(node)
	if want == "" || want == mainNodeID {
		want = split.Sink
	}
	picked, ok := findSplitNode(split, want)
	if !ok {
		err = app.RefuseOperation("no node " + node + " in the statement; it has " + strings.Join(splitNodeNames(split), ", "))
	}
	return
}

func splitNodeNames(split splitResult) (names []string) {
	for _, n := range split.Nodes {
		names = append(names, string(n.ID))
	}
	return
}

// sqlFlow derives the clause graph or the lineage of one node.
func sqlFlow(runSql string, in FlowArgs) (out FlowReading, err error) {
	lens := strings.ToLower(strings.TrimSpace(in.Lens))
	switch lens {
	case "", "statement":
		lens = "statement"
	case "lineage":
	default:
		return out, app.RefuseOperation("lens is statement or lineage; explain_sql asks the server for the other lenses")
	}
	split, node, source, err := flowSource(runSql, in.Sql, in.Node)
	if err != nil {
		return
	}
	out = FlowReading{Lens: lens, Source: source, Node: string(node.ID), Nodes: splitNodeNames(split), Sql: truncateBytes(node.SQL, 4<<10)}
	sibs := make(map[string]struct{}, len(node.DependsOn))
	for _, d := range node.DependsOn {
		sibs[string(d)] = struct{}{}
	}
	var g flowGraph
	var derr error
	if lens == "lineage" {
		g, out.Note, derr = buildLineageGraph(node.SQL, sibs)
	} else {
		g, derr = buildFlowGraph(node.SQL, sibs, string(node.ID))
	}
	if derr != nil {
		out.Error = truncateRunes(firstLine(derr.Error()), 300)
		return out, nil
	}
	out.Capped = g.Capped
	out.Graph, out.Edges, out.Truncated = flowGraphOps(g, flowOpsMaxBytes-len(out.Sql))
	return out, nil
}

// flowGraphOps copies a graph into its operation form within a byte budget
// counted over labels and details; edges whose ends were left out go too.
func flowGraphOps(g flowGraph, budget int) (nodes []FlowNode, edges []FlowEdge, truncated bool) {
	kept := make(map[string]bool, len(g.Nodes))
	used := 0
	for _, n := range g.Nodes {
		fn := FlowNode{Id: n.ID, Kind: flowKindName(n.Kind), Label: n.Label, Detail: truncateRunes(n.Detail, flowOpsMaxDetail),
			Start: n.Start, End: n.End}
		cost := len(fn.Id) + len(fn.Label) + len(fn.Detail) + 48
		if used+cost > budget {
			truncated = true
			break
		}
		used += cost
		kept[n.ID] = true
		nodes = append(nodes, fn)
	}
	for _, e := range g.Edges {
		if kept[e.From] && kept[e.To] {
			edges = append(edges, FlowEdge{From: e.From, To: e.To, Label: e.Label})
		}
	}
	return
}

// flowKindName spells a node kind for a caller.
func flowKindName(k flowNodeKind) string {
	switch k {
	case flowSourceTable:
		return "source table"
	case flowSourceCTE:
		return "cte"
	case flowSourceSubquery:
		return "subquery"
	case flowSourceFunction:
		return "function"
	case flowJoin:
		return "join"
	case flowFilter:
		return "filter"
	case flowAggregate:
		return "aggregate"
	case flowProject:
		return "project"
	case flowDistinct:
		return "distinct"
	case flowSort:
		return "sort"
	case flowLimit:
		return "limit"
	case flowUnion:
		return "union"
	case flowResult:
		return "result"
	case flowOp:
		return "step"
	case flowColumnSrc:
		return "column source"
	case flowColumnOut:
		return "column output"
	}
	return "node"
}

// explainLensOf maps explain_sql's kind onto the Flow pane's remote lens.
func explainLensOf(kind string) (l flowLens, ok bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ast":
		return lensAST, true
	case "plan":
		return lensPlan, true
	case "pipeline":
		return lensPipeline, true
	case "estimate":
		return lensEstimate, true
	case "indexes":
		return lensIndexes, true
	}
	return
}

// explainParams are the node's signal reads as the run would put them on the
// URL: the signal's value, or a reserved signal's seed. A SET of the same
// name in the prelude shadows it, as on a run.
func explainParams(node splitNode, signals []SignalState) (params map[string]string) {
	for _, name := range node.Reads {
		raw, found := "", false
		for _, s := range signals {
			if s.Name == string(name) {
				raw, found = s.Value, true
				break
			}
		}
		if !found {
			raw, found = signalSeedRaw(string(name))
		}
		if !found {
			continue
		}
		if params == nil {
			params = make(map[string]string, len(node.Reads))
		}
		params["param_"+string(name)] = raw
	}
	return
}

// explainSql makes the EXPLAIN on the endpoint the statement routes to,
// under the agent limits when an agent asked.
func explainSql(client *Client, obo *app.OnBehalfOf, runSql string, signals []SignalState, in ExplainArgs) (out ExplainResult, err error) {
	lens, ok := explainLensOf(in.Kind)
	if !ok {
		return out, app.RefuseOperation("kind is ast, plan, pipeline, estimate or indexes; sql_flow derives the clause graph and the lineage locally")
	}
	split, node, source, err := flowSource(runSql, in.Sql, in.Node)
	if err != nil {
		return
	}
	stmt := fuseNode(split, node.ID)
	if obo != nil {
		// The statement the EXPLAIN wraps is checked as a run of it would
		// be: a read, under the grant (ADR-0270 §SD2).
		if err = refuseAgentStatement(client, obo, stmt); err != nil {
			return
		}
	}
	out = ExplainResult{Kind: lens.String(), Source: source, Node: string(node.ID)}
	opts := &ExecOptions{QueryID: runid.Mint("play", "explain-op"), Label: "explain-op", WrapStatement: explainWrap(lens), Agent: obo}
	ctx, cancel := context.WithTimeout(context.Background(), explainOpsTimeout)
	defer cancel()
	rec, _, _, confined, xerr := clientExecutor{client: client, opts: opts}.executeLabelled(ctx,
		compiledNode{SQL: stmt, NodeID: node.ID, Params: explainParams(node, signals)}, memory.NewGoAllocator(), nil)
	if xerr != nil {
		if explainUnsupportedByEndpoint(xerr) {
			return out, app.RefuseOperation("the statement routes to the host's introspection plane, which has no EXPLAIN")
		}
		if limit, isLimit := asAgentLimit(xerr); isLimit {
			if limit.Destination != "" {
				return out, app.RefuseForDestinations(limit.Error(), limit.Destination)
			}
			return out, app.RefuseOperation(limit.Error())
		}
		return out, app.RefuseOperation("the EXPLAIN failed: " + truncateRunes(xerr.Error(), 600))
	}
	out.Confined = confined
	var lines []string
	if rec != nil {
		defer rec.Release()
		cols := int(rec.NumCols())
		for row := range rec.NumRows() {
			if cols <= 1 {
				lines = append(lines, strings.Clone(formatCell(rec, 0, row)))
				continue
			}
			cells := make([]string, cols)
			for col := range cols {
				cells[col] = strings.Clone(formatCell(rec, col, row))
			}
			lines = append(lines, strings.Join(cells, "\t"))
		}
	}
	// A row may hold several lines (PLAN's json = 1 is one row).
	used := 0
	for _, ln := range lines {
		for part := range strings.SplitSeq(ln, "\n") {
			if used+len(part)+1 > flowOpsMaxBytes {
				out.Truncated = true
				break
			}
			used += len(part) + 1
			out.Lines = append(out.Lines, part)
		}
		if out.Truncated {
			break
		}
	}
	if in.Graph {
		g, perr := parseLensRecord(lens, lines)
		switch {
		case perr != nil:
			out.GraphNote = "the output did not parse into a graph: " + truncateRunes(perr.Error(), 200)
		default:
			var cut bool
			out.Graph, out.Edges, cut = flowGraphOps(g, max(flowOpsMaxBytes-used, 4<<10))
			if g.Capped || cut {
				out.GraphNote = "the graph is a prefix: " + strconv.Itoa(len(out.Graph)) + " of the output's nodes are listed"
			}
		}
	}
	return out, nil
}

// asAgentLimit finds an agent limit inside a wrapped executor error.
func asAgentLimit(err error) (limit *AgentLimitError, ok bool) {
	ok = errors.As(err, &limit)
	return
}
