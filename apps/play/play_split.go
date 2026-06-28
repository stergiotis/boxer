package play

import (
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_split.go is ADR-0097 slice 3a: the SPLIT CONTRACT. It recovers the
// reactive query-graph structure from the editor buffer by static analysis —
// pure, no execution. Two levels (SD3): a quote-aware top-level `;` statement
// split, then per-statement CTE-lift via nanopass BuildScopes. Every top-level
// CTE becomes a node, the terminal SELECT is the `main` sink node; CTE
// references are data edges and unbound `{name:Type}` param slots are signal
// edges. Fusion back to executable SQL (and execution) is slice 3c.

type splitNodeKind uint8

const (
	splitNodeStatement splitNodeKind = iota // a statement's terminal SELECT (the sink)
	splitNodeCTE                            // a lifted WITH-clause CTE
)

// splitNode is one node of the recovered graph: a SQL fragment plus its data
// edges (CTE nodes it reads) and signal edges (unbound param names it reads).
type splitNode struct {
	ID        NodeID
	Kind      splitNodeKind
	SQL       string     // CTE body text, or the full statement for the sink
	DependsOn []NodeID   // data edges: CTE nodes referenced
	Reads     []SignalID // signal edges: unbound param-slot names referenced
}

// splitResult is the recovered node graph for an editor buffer.
type splitResult struct {
	Nodes   []splitNode
	Sink    NodeID   // the node panels bind to — the last statement's terminal SELECT
	Prelude []string // the SET statements (param bindings), prepended when fusing any node
}

// splitGraph recovers the node graph from an editor buffer (ADR-0097 3a). The
// last non-SET statement is the sink; its top-level CTEs become nodes. Pure: it
// parses and analyses, it does not execute or rewrite SQL.
func splitGraph(sql string) (res splitResult, err error) {
	stmts := statementSplit(sql)
	sinkIdx := -1
	for i := len(stmts) - 1; i >= 0; i-- {
		if !isSetStatement(stmts[i]) {
			sinkIdx = i
			break
		}
	}
	if sinkIdx < 0 {
		err = eh.Errorf("splitGraph: no query statement to split")
		return
	}
	stmt := stmts[sinkIdx]
	for _, s := range stmts {
		if isSetStatement(s) {
			res.Prelude = append(res.Prelude, s)
		}
	}

	pr, pErr := nanopass.Parse(stmt)
	if pErr != nil {
		err = eh.Errorf("splitGraph: parse: %w", pErr)
		return
	}
	scopes, sErr := nanopass.BuildScopes(pr, "")
	if sErr != nil {
		err = eh.Errorf("splitGraph: scopes: %w", sErr)
		return
	}
	if len(scopes) == 0 {
		err = eh.Errorf("splitGraph: no root scope")
		return
	}
	root := scopes[0]

	// Each top-level CTE → a node: its body SQL, the CTEs its body references
	// (data edges), and the param slots its body reads (signal edges).
	for i := range root.CTEDefs {
		cte := root.CTEDefs[i]
		res.Nodes = append(res.Nodes, splitNode{
			ID:        NodeID(cte.Name),
			Kind:      splitNodeCTE,
			SQL:       cteBodyText(pr, cte),
			DependsOn: cteRefs(cte.Scopes),
			Reads:     paramSlotsOf(pr, cte.Node),
		})
	}

	// The sink node carries the whole statement; fuse-to-sink (3c) executes it
	// verbatim, so a single statement round-trips to the original query.
	res.Nodes = append(res.Nodes, splitNode{
		ID:        mainNodeID,
		Kind:      splitNodeStatement,
		SQL:       stmt,
		DependsOn: cteRefs([]*nanopass.SelectScope{root}),
		Reads:     paramSlotsOf(pr, root.Node),
	})
	res.Sink = mainNodeID

	err = checkAcyclic(res.Nodes)
	return
}

// fuseToSink produces the executable SQL for the fuse-to-sink first cut
// (ADR-0097 3c): the SET prelude followed by the sink statement. For a single
// statement this is the original query (the client's ExtractParams re-lifts the
// SET prelude to URL params either way, so the residual ClickHouse runs is
// unchanged — behaviour-identical). Materialized intermediates (3d) will instead
// rewrite the sink to read their results rather than inlining the CTEs.
func fuseToSink(sql string) (executable string, res splitResult, err error) {
	res, err = splitGraph(sql)
	if err != nil {
		return
	}
	executable = fuseNode(res, res.Sink)
	return
}

// fuseNode assembles the executable SQL for one node (ADR-0097 3d): the SET
// prelude, then — for the sink — the whole statement (CTEs inline), or for a CTE
// node a `WITH <transitive dep CTEs, topologically ordered> <node body>`. The
// client's ExtractParams re-lifts the prelude to URL params at execution.
func fuseNode(res splitResult, nodeID NodeID) (executable string) {
	parts := make([]string, 0, len(res.Prelude)+1)
	parts = append(parts, res.Prelude...)
	node, ok := findSplitNode(res, nodeID)
	if !ok {
		return strings.Join(parts, ";\n")
	}
	if node.Kind == splitNodeStatement {
		parts = append(parts, node.SQL)
		return strings.Join(parts, ";\n")
	}
	deps := transitiveDeps(res, nodeID)
	if len(deps) == 0 {
		parts = append(parts, node.SQL)
		return strings.Join(parts, ";\n")
	}
	withDefs := make([]string, 0, len(deps))
	for _, d := range deps {
		dn, found := findSplitNode(res, d)
		if !found {
			continue
		}
		withDefs = append(withDefs, string(d)+" AS (\n"+dn.SQL+"\n)")
	}
	parts = append(parts, "WITH "+strings.Join(withDefs, ",\n")+"\n"+node.SQL)
	return strings.Join(parts, ";\n")
}

func findSplitNode(res splitResult, id NodeID) (node splitNode, ok bool) {
	for i := range res.Nodes {
		if res.Nodes[i].ID == id {
			return res.Nodes[i], true
		}
	}
	return splitNode{}, false
}

// transitiveDeps returns the transitive data dependencies of a node in
// topological order (a dependency before anything that reads it), excluding the
// node itself — the order a WITH clause requires.
func transitiveDeps(res splitResult, nodeID NodeID) (ordered []NodeID) {
	seen := make(map[NodeID]bool, 4)
	var visit func(id NodeID)
	visit = func(id NodeID) {
		n, ok := findSplitNode(res, id)
		if !ok {
			return
		}
		for _, d := range n.DependsOn {
			if seen[d] {
				continue
			}
			seen[d] = true
			visit(d)
			ordered = append(ordered, d)
		}
	}
	visit(nodeID)
	return
}

// statementSplit splits sql on top-level `;` that are not inside a string
// literal, a quoted identifier, or a comment — ADR-0097 SD3's one new primitive,
// mirroring the quote-aware discard-marker scan. Whitespace-only fragments drop.
func statementSplit(sql string) (stmts []string) {
	rs := []rune(sql)
	n := len(rs)
	var b strings.Builder
	flush := func() {
		s := strings.TrimSpace(b.String())
		if s != "" {
			stmts = append(stmts, s)
		}
		b.Reset()
	}
	i := 0
	for i < n {
		c := rs[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			// string literal / quoted identifier: consume to the matching close,
			// honouring backslash escapes and doubled-quote (`''`) escapes.
			b.WriteRune(c)
			i++
			for i < n {
				d := rs[i]
				b.WriteRune(d)
				i++
				if d == '\\' && i < n {
					b.WriteRune(rs[i])
					i++
					continue
				}
				if d == c {
					if i < n && rs[i] == c {
						b.WriteRune(rs[i])
						i++
						continue
					}
					break
				}
			}
		case c == '-' && i+1 < n && rs[i+1] == '-':
			for i < n && rs[i] != '\n' {
				b.WriteRune(rs[i])
				i++
			}
		case c == '/' && i+1 < n && rs[i+1] == '*':
			b.WriteRune(rs[i])
			b.WriteRune(rs[i+1])
			i += 2
			for i < n {
				if rs[i] == '*' && i+1 < n && rs[i+1] == '/' {
					b.WriteRune(rs[i])
					b.WriteRune(rs[i+1])
					i += 2
					break
				}
				b.WriteRune(rs[i])
				i++
			}
		case c == ';':
			flush()
			i++
		default:
			b.WriteRune(c)
			i++
		}
	}
	flush()
	return
}

func isSetStatement(stmt string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "SET ")
}

// cteBodyText returns a CTE's inner query text (the body of `name AS (body)`).
func cteBodyText(pr *nanopass.ParseResult, cte nanopass.CTEDef) string {
	for i := 0; i < cte.Node.GetChildCount(); i++ {
		if q, ok := cte.Node.GetChild(i).(*grammar1.QueryContext); ok {
			return strings.TrimSpace(nanopass.NodeText(pr, q))
		}
	}
	return strings.TrimSpace(nanopass.NodeText(pr, cte.Node))
}

// cteRefs collects the CTE-reference table sources across the given scopes —
// the data edges. Deduped and sorted for deterministic output.
func cteRefs(scopes []*nanopass.SelectScope) (deps []NodeID) {
	seen := make(map[string]bool, 4)
	for _, sc := range scopes {
		if sc == nil {
			continue
		}
		for _, ts := range sc.Tables {
			if ts.IsCTE && !seen[ts.Table] {
				seen[ts.Table] = true
				deps = append(deps, NodeID(ts.Table))
			}
		}
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i] < deps[j] })
	return
}

// paramSlotsOf collects the unbound `{name:Type}` param-slot names within a CST
// node — the signal edges. Deduped and sorted.
func paramSlotsOf(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (reads []SignalID) {
	seen := make(map[string]bool, 4)
	slots := nanopass.FindAll(ctx, func(c antlr.ParserRuleContext) bool {
		_, ok := c.(*grammar1.ColumnExprParamSlotContext)
		return ok
	})
	for _, s := range slots {
		name := paramSlotName(nanopass.NodeText(pr, s))
		if name != "" && !seen[name] {
			seen[name] = true
			reads = append(reads, name)
		}
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
	return
}

// paramSlotName extracts the decoded name from a `{name: Type}` slot's text.
func paramSlotName(slotText string) string {
	s := strings.TrimSpace(slotText)
	s = strings.TrimPrefix(s, "{")
	if idx := strings.IndexByte(s, ':'); idx >= 0 {
		s = s[:idx]
	}
	return nanopass.DecodeIdentifier(strings.TrimSpace(s))
}

// checkAcyclic rejects a dependency cycle in the data edges (ADR-0097 SD9). CTE
// scoping forbids forward references so a cycle cannot arise from CTE-lift, but
// the guard is cheap and keeps the contract honest (and covers future node
// sources). Three-colour DFS.
func checkAcyclic(nodes []splitNode) (err error) {
	adj := make(map[NodeID][]NodeID, len(nodes))
	for _, n := range nodes {
		adj[n.ID] = n.DependsOn
	}
	const (
		white = iota
		gray
		black
	)
	color := make(map[NodeID]int, len(nodes))
	var visit func(id NodeID) error
	visit = func(id NodeID) (vErr error) {
		color[id] = gray
		for _, d := range adj[id] {
			switch color[d] {
			case gray:
				vErr = eh.Errorf("splitGraph: dependency cycle through node %q", d)
				return
			case white:
				vErr = visit(d)
				if vErr != nil {
					return
				}
			}
		}
		color[id] = black
		return
	}
	for _, n := range nodes {
		if color[n.ID] != white {
			continue
		}
		err = visit(n.ID)
		if err != nil {
			return
		}
	}
	return
}
