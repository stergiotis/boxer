package play

import (
	"fmt"
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

	// OwnWith marks a CTE node whose body opens with its own WITH clause
	// (a nested CTE or scalar alias inside the body). fuseNode must then
	// CONTINUE that WITH list with the transitive dep definitions instead
	// of prepending a second `WITH` — two WITH clauses are invalid SQL.
	// Guaranteed by construction: when set, SQL's first token is the WITH
	// keyword (the body text starts at the body query's first token).
	OwnWith bool
}

// splitResult is the recovered node graph for an editor buffer.
type splitResult struct {
	Nodes   []splitNode
	Sink    NodeID   // the node panels bind to — the last statement's terminal SELECT
	Prelude []string // the SET statements (param bindings), prepended when fusing any node
}

// splitGraph recovers the node graph from an editor buffer (ADR-0097 3a). The
// single non-SET statement is the sink; its top-level CTEs become nodes. A
// buffer with more than one non-SET statement is rejected — executing only one
// of them would silently drop the rest (statements-as-sibling-nodes is a future
// slice of SD3). Pure: it parses and analyses, it does not execute or rewrite SQL.
func splitGraph(sql string) (res splitResult, err error) {
	stmts := statementSplit(sql)
	sinkIdx := -1
	nonSet := 0
	for i, s := range stmts {
		if isSetStatement(s) {
			res.Prelude = append(res.Prelude, s)
			continue
		}
		nonSet++
		sinkIdx = i
	}
	if sinkIdx < 0 {
		err = eh.Errorf("splitGraph: no query statement to split")
		return
	}
	if nonSet > 1 {
		err = eh.Errorf("splitGraph: multi-statement buffers are not supported by the query graph (%d statements); write a SET prelude plus one statement", nonSet)
		return
	}
	stmt := stmts[sinkIdx]

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

	// topLevel maps each lifted CTE's definition node to its graph node id.
	// Data edges are derived by RESOLUTION against this map (a reference is an
	// edge iff it resolves to a lifted definition), not by name matching: a
	// nested CTE inside a body is not a graph node, and a nested name that
	// shadows a lifted one must not read as an edge to it (review finding —
	// phantom edges).
	topLevel := make(map[antlr.ParserRuleContext]NodeID, len(root.CTEDefs))
	for i := range root.CTEDefs {
		topLevel[root.CTEDefs[i].Node] = NodeID(root.CTEDefs[i].Name)
	}

	// Each top-level CTE → a node: its body SQL, the lifted CTEs its body
	// (including any nested subqueries/CTEs of its own) resolves references to
	// (data edges), and the param slots its body reads (signal edges).
	for i := range root.CTEDefs {
		cte := root.CTEDefs[i]
		res.Nodes = append(res.Nodes, splitNode{
			ID:        NodeID(cte.Name),
			Kind:      splitNodeCTE,
			SQL:       cteBodyText(pr, cte),
			DependsOn: resolvedDeps(cte.Scopes, topLevel),
			Reads:     paramSlotsOf(pr, cte.Node),
			OwnWith:   bodyOpensWithClause(cte),
		})
	}

	// The sink node carries the whole statement; fuse-to-sink (3c) executes it
	// verbatim, so a single statement round-trips to the original query. Its id
	// is synthetic (a lookup key and label — never emitted into SQL), so it
	// steps aside when a user CTE claimed the default name: CTE ids must stay
	// their SQL names (fuseNode emits `WITH <id> AS`), the sink key is free.
	// Its data edges come from every top-level UNION member and their nested
	// subqueries (a CTE referenced only inside a derived table previously drew
	// no edge — review finding); the lifted CTE bodies are excluded, those
	// references belong to the CTE nodes.
	sinkID := uniqueSinkID(res.Nodes)
	sinkReads := make([]antlr.ParserRuleContext, 0, len(scopes))
	for _, sc := range scopes {
		sinkReads = append(sinkReads, sc.Node)
	}
	res.Nodes = append(res.Nodes, splitNode{
		ID:        sinkID,
		Kind:      splitNodeStatement,
		SQL:       stmt,
		DependsOn: resolvedDeps(scopes, topLevel),
		Reads:     paramSlotsOfAll(pr, sinkReads),
	})
	res.Sink = sinkID

	err = checkUniqueIDs(res.Nodes)
	if err != nil {
		return
	}
	err = checkAcyclic(res.Nodes)
	return
}

// uniqueSinkID returns the sink node's id: mainNodeID, disambiguated when a
// user CTE claimed that name ("main (sink)", then "main (sink2)", …). Without
// this, a CTE literally named "main" aliased the sink — fuseNode fused the CTE
// body instead of the statement, or checkAcyclic saw a bogus self-cycle.
func uniqueSinkID(nodes []splitNode) (id NodeID) {
	taken := func(cand NodeID) bool {
		for i := range nodes {
			if nodes[i].ID == cand {
				return true
			}
		}
		return false
	}
	id = mainNodeID
	if !taken(id) {
		return
	}
	id = mainNodeID + " (sink)"
	for n := 2; taken(id); n++ {
		id = NodeID(fmt.Sprintf("%s (sink%d)", mainNodeID, n))
	}
	return
}

// checkUniqueIDs rejects duplicate node ids (two same-named CTEs — ClickHouse
// rejects the query anyway). checkAcyclic keys its adjacency by id, so a
// duplicate would otherwise be mis-diagnosed as a dependency cycle.
func checkUniqueIDs(nodes []splitNode) (err error) {
	seen := make(map[NodeID]struct{}, len(nodes))
	for i := range nodes {
		if _, dup := seen[nodes[i].ID]; dup {
			err = eh.Errorf("splitGraph: duplicate node id %q", nodes[i].ID)
			return
		}
		seen[nodes[i].ID] = struct{}{}
	}
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
	if node.OwnWith {
		// The body opens its own WITH list; continue it with a comma after the
		// dep definitions instead of opening a second `WITH` — two WITH clauses
		// are invalid SQL (review finding). Deps precede the body's own items,
		// which may reference them; the reverse cannot occur (a lifted body
		// cannot see a nested definition). OwnWith guarantees the body text
		// starts with the WITH keyword token, so the slice is exact.
		body := strings.TrimSpace(node.SQL[len("WITH"):])
		parts = append(parts, "WITH "+strings.Join(withDefs, ",\n")+",\n"+body)
		return strings.Join(parts, ";\n")
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

// isSetStatement classifies one split fragment as a SET statement. Fast path:
// the canonical spelling ("SET" then a space — a statement can only start that
// way if it IS a SET). Slow path: grammar-based, the same classification
// ExtractParams applies at execution, so the splitter can no longer disagree
// with the client about what is a prelude. The former purely textual check
// read a comment-prefixed or newline-broken SET as a query statement and
// falsely rejected the buffer as multi-statement (review finding).
//
// Grammar1 accepts a SET only as the prelude of a following statement
// (`SET …; <query>` — a lone `SET …;` does not parse), so the probe completes
// the fragment with `;\nSELECT 1` and checks for a SetStmt in the parse. Only
// a genuine single SET fragment parses in that shape: a query fragment
// followed by the appended SELECT is a two-statement input, which the grammar
// rejects. The leading newline keeps a trailing line comment in the fragment
// from swallowing the terminator. An unparseable fragment is not a SET: it
// stays the sink candidate and the sink parse reports the syntax error.
func isSetStatement(stmt string) bool {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "SET ") {
		return true
	}
	pr, err := nanopass.Parse(stmt + "\n;\nSELECT 1")
	if err != nil {
		return false
	}
	found := false
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar1.SetStmtContext); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

// bodyQueryCtx returns the CTE body's query context (`name AS (body)` → body),
// or nil when the definition has an unexpected shape.
func bodyQueryCtx(cte nanopass.CTEDef) *grammar1.QueryContext {
	for i := 0; i < cte.Node.GetChildCount(); i++ {
		if q, ok := cte.Node.GetChild(i).(*grammar1.QueryContext); ok {
			return q
		}
	}
	return nil
}

// cteBodyText returns a CTE's inner query text (the body of `name AS (body)`).
func cteBodyText(pr *nanopass.ParseResult, cte nanopass.CTEDef) string {
	if q := bodyQueryCtx(cte); q != nil {
		return strings.TrimSpace(nanopass.NodeText(pr, q))
	}
	return strings.TrimSpace(nanopass.NodeText(pr, cte.Node))
}

// bodyOpensWithClause reports whether a CTE body starts with its own WITH
// clause. Checked on the token stream (first token of the body query), not
// the text, so an identifier that merely starts with "WITH" can't false-
// positive. When true, the body text (which starts at that token) begins
// with the WITH keyword — the contract fuseNode's merge relies on.
func bodyOpensWithClause(cte nanopass.CTEDef) bool {
	q := bodyQueryCtx(cte)
	return q != nil && q.GetStart() != nil &&
		q.GetStart().GetTokenType() == grammar1.ClickHouseLexerWITH
}

// resolvedDeps collects the data edges of a node given its scope trees: every
// CTE reference that RESOLVES to a lifted (top-level) definition, in the scope
// where the reference occurs. Resolution — not name matching — is what keeps
// the edges faithful: a reference to a nested CTE (including one shadowing a
// lifted name) resolves to the nested definition and contributes no edge.
// Descent covers FROM subqueries, expression subqueries, and the node's own
// nested CTE bodies, but not the lifted definitions' bodies — those references
// are the corresponding CTE nodes' own edges. Deduped and sorted.
func resolvedDeps(roots []*nanopass.SelectScope, topLevel map[antlr.ParserRuleContext]NodeID) (deps []NodeID) {
	seen := make(map[NodeID]bool, 4)
	visited := make(map[*nanopass.SelectScope]bool, 8)
	var visit func(sc *nanopass.SelectScope)
	visit = func(sc *nanopass.SelectScope) {
		if sc == nil || visited[sc] {
			return
		}
		visited[sc] = true
		for i := range sc.Tables {
			ts := &sc.Tables[i]
			if ts.IsCTE {
				if def, found := sc.ResolveCTE(ts.Table); found {
					if id, lifted := topLevel[def.Node]; lifted && !seen[id] {
						seen[id] = true
						deps = append(deps, id)
					}
				}
			}
			for _, sub := range ts.Scopes {
				visit(sub)
			}
		}
		for _, sub := range sc.Subqueries {
			visit(sub)
		}
		for i := range sc.CTEDefs {
			// The visible-defs list is shared: it holds this node's own nested
			// definitions AND the inherited lifted ones. Descend only into the
			// former — a lifted body is another node's territory.
			if _, lifted := topLevel[sc.CTEDefs[i].Node]; lifted {
				continue
			}
			for _, sub := range sc.CTEDefs[i].Scopes {
				visit(sub)
			}
		}
	}
	for _, sc := range roots {
		visit(sc)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i] < deps[j] })
	return
}

// paramSlotsOf collects the unbound `{name:Type}` param-slot names within a CST
// node — the signal edges. Deduped and sorted.
func paramSlotsOf(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (reads []SignalID) {
	return paramSlotsOfAll(pr, []antlr.ParserRuleContext{ctx})
}

// paramSlotsOfAll is paramSlotsOf over several CST nodes (the sink's top-level
// UNION members), deduped across all of them.
func paramSlotsOfAll(pr *nanopass.ParseResult, ctxs []antlr.ParserRuleContext) (reads []SignalID) {
	seen := make(map[string]bool, 4)
	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
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
