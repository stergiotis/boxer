package play

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_flow_model.go is the ADR-0153 flow-graph derivation: a pure, UI-free
// mapping from one statement's SQL text to a clause-level dataflow graph —
// sources feed the join tree, whose output passes through the clause stages in
// ClickHouse's logical order. Derived statically from the grammar1 CST (the
// play_split.go precedent); nothing here executes SQL or touches the UI.
//
// The IR deliberately contains no CST types: it is the seam future lenses
// (EXPLAIN AST / PLAN / PIPELINE parsers) target with the same output shape
// (ADR-0153 §SD2).

type flowNodeKind uint8

const (
	flowSourceTable flowNodeKind = iota
	flowSourceCTE
	flowSourceSubquery
	flowSourceFunction
	flowJoin      // binary join or ARRAY JOIN
	flowFilter    // PREWHERE / WHERE / HAVING / QUALIFY (distinguished by label)
	flowAggregate // GROUP BY
	flowProject   // the SELECT list (window definitions folded in)
	flowDistinct
	flowSort  // ORDER BY
	flowLimit // LIMIT / LIMIT BY / OFFSET
	flowUnion // UNION / EXCEPT / INTERSECT merge
	flowResult
	flowOp        // a lens operator (an EXPLAIN plan step, pipeline processor, AST node)
	flowColumnSrc // lineage: a source column (table/CTE/subquery side)
	flowColumnOut // lineage: an output column (a SELECT-list item)
)

// isSource reports whether the kind is one of the four source kinds (used for
// styling; sources are where the dataflow begins).
func (k flowNodeKind) isSource() bool {
	switch k {
	case flowSourceTable, flowSourceCTE, flowSourceSubquery, flowSourceFunction, flowColumnSrc:
		return true
	}
	return false
}

// flowNode is one vertex of the derived graph. ID is positional and
// deterministic (see flowBuilder); Label is short (truncated); Detail carries
// the whitespace-collapsed clause snippet plus markers (FINAL, aliases, …).
// Start/End are the byte range of the node's clause within the SQL text the
// graph was derived from (zero range = no source anchor — union/result nodes,
// and every lens-produced node); the editor highlight rebases and re-verifies
// them before acting (ADR-0153, flowSelectionSection).
type flowNode struct {
	ID     string
	Kind   flowNodeKind
	Label  string
	Detail string
	Start  int
	End    int
}

// flowEdge is a directed dataflow edge. Label marks join inputs ("l"/"r").
type flowEdge struct {
	From, To string
	Label    string
}

// flowGraph is the derived graph. Capped reports that a bound (node count or
// nesting depth) stopped the expansion, so the drawing is a prefix of the
// statement's real structure.
type flowGraph struct {
	Nodes  []flowNode
	Edges  []flowEdge
	Capped bool
}

const (
	// flowMaxNodes / flowMaxDepth bound the derivation (ADR-0153 §SD6): the
	// layered layout is a tens-to-low-hundreds instrument, and FROM-subquery
	// nesting is the one recursion. Beyond a cap the graph stays consistent —
	// a subquery stays opaque, a stage is skipped — and Capped says so.
	flowMaxNodes = 160
	flowMaxDepth = 4

	// flowSnippetRunes / flowLabelRunes clamp Detail / Label text. Labels feed
	// Graphviz box widths, so they are short; the full clause text lives in
	// Detail (shown in the selection line, ADR-0153 §SD4).
	flowSnippetRunes = 160
	flowLabelRunes   = 28

	// flowResultNodeID is the single terminal node every chain drains into.
	flowResultNodeID = "result"
)

// buildFlowGraph derives the clause-level dataflow graph of one SELECT-shaped
// statement. siblingCTEs are the split-level dependencies of the node
// (splitNode.DependsOn): a lifted CTE referenced by a node's *body* parses as
// a plain table — the body carries no WITH — so the sibling set is what marks
// it as a CTE reference. resultLabel names the terminal node (the split node
// id). Pure and deterministic: identical inputs yield identical graphs.
func buildFlowGraph(sql string, siblingCTEs map[string]struct{}, resultLabel string) (g flowGraph, err error) {
	if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
		err = eb.Build().Stringer("kind", kind).Errorf("flow graph: not a SELECT-shaped statement")
		return
	}
	pr, pErr := nanopass.Parse(sql)
	if pErr != nil {
		err = eh.Errorf("flow graph: parse: %w", pErr)
		return
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || qs.Query() == nil || qs.Query().SelectUnionStmt() == nil {
		err = eh.Errorf("flow graph: no query body")
		return
	}

	b := &flowBuilder{
		pr:       pr,
		siblings: siblingCTEs,
		ids:      make(map[string]struct{}, 32),
	}
	// Scope analysis classifies each FROM/JOIN source (base table, CTE,
	// subquery, table function). A scope failure is not fatal: the builder
	// falls back to local CST classification, which only loses the IsCTE
	// marking BuildScopes derives from the statement's own WITH clause.
	if scopes, sErr := nanopass.BuildScopes(pr, ""); sErr == nil {
		all := nanopass.FlattenScopes(scopes)
		b.scopeOf = make(map[*grammar1.SelectStmtContext]*nanopass.SelectScope, len(all))
		for _, sc := range all {
			b.scopeOf[sc.Node] = sc
		}
	}

	terminal := b.buildUnion(qs.Query().SelectUnionStmt(), "q", 0)
	label := resultLabel
	if label == "" {
		label = "result"
	}
	b.addNode(flowNode{ID: flowResultNodeID, Kind: flowResult, Label: truncateRunes(label, flowLabelRunes)})
	b.addEdge(terminal, flowResultNodeID, "")
	g = b.g
	return
}

// flowBuilder carries the walk state. Node IDs are positional paths — root
// "q", union member i appends "u<i>", FROM-subquery source k appends "f<k>",
// then ":<stage>" (":src<k>", ":join<n>", ":where", …). Positional ids keep a
// self-join as two distinct nodes and are identical across runs.
type flowBuilder struct {
	pr       *nanopass.ParseResult
	scopeOf  map[*grammar1.SelectStmtContext]*nanopass.SelectScope // nil on scope failure
	siblings map[string]struct{}
	g        flowGraph
	ids      map[string]struct{}
}

// addNode appends the node unless the cap is reached; false means the node was
// dropped (and the graph marked capped), so the caller must not edge to it.
func (b *flowBuilder) addNode(n flowNode) bool {
	if len(b.g.Nodes) >= flowMaxNodes {
		b.g.Capped = true
		return false
	}
	b.ids[n.ID] = struct{}{}
	b.g.Nodes = append(b.g.Nodes, n)
	return true
}

// addEdge appends the edge when both endpoints exist. A missing endpoint is a
// cap casualty — dropping the edge (and marking the graph capped) keeps the
// model consistent for the layout engine, which would otherwise synthesise the
// missing node.
func (b *flowBuilder) addEdge(from, to, label string) {
	if from == "" || to == "" {
		return
	}
	if _, ok := b.ids[from]; !ok {
		b.g.Capped = true
		return
	}
	if _, ok := b.ids[to]; !ok {
		b.g.Capped = true
		return
	}
	b.g.Edges = append(b.g.Edges, flowEdge{From: from, To: to, Label: label})
}

// snipRange returns the whitespace-collapsed, truncated source text of a CST
// context plus its byte range in the parsed SQL ("" and a zero range for
// nil/synthetic contexts).
func (b *flowBuilder) snipRange(ctx antlr.ParserRuleContext) (snip string, start, end int) {
	if ctx == nil {
		return "", 0, 0
	}
	r := b.pr.SourceRangeOf(ctx)
	if r.Empty() {
		return "", 0, 0
	}
	return truncateRunes(strings.Join(strings.Fields(b.pr.Source[r.Start:r.End]), " "), flowSnippetRunes),
		r.Start, r.End
}

// snippet is snipRange's text half, for labels and details that carry no
// anchor of their own.
func (b *flowBuilder) snippet(ctx antlr.ParserRuleContext) string {
	s, _, _ := b.snipRange(ctx)
	return s
}

// buildUnion walks one selectUnionStmt level: the head SELECT plus its
// UNION/EXCEPT/INTERSECT items. One member ⇒ its chain is the level's chain;
// more ⇒ every member terminal drains into one operator node. Returns the id
// of the node producing this level's stream ("" when capped away).
func (b *flowBuilder) buildUnion(u grammar1.ISelectUnionStmtContext, path string, depth int) (terminal string) {
	if u == nil {
		return ""
	}
	items := u.AllSelectUnionStmtItem()
	memberPath := func(i int) string {
		if len(items) == 0 {
			return path // a single member owns the level's path
		}
		return path + "u" + strconv.Itoa(i)
	}
	terms := make([]string, 0, 1+len(items))
	if t := b.buildWithParens(u.SelectStmtWithParens(), memberPath(0), depth); t != "" {
		terms = append(terms, t)
	}
	ops := make([]string, 0, len(items))
	for i, it := range items {
		if it == nil {
			continue
		}
		ops = append(ops, unionOpLabel(it))
		if t := b.buildWithParens(it.SelectStmtWithParens(), memberPath(i+1), depth); t != "" {
			terms = append(terms, t)
		}
	}
	if len(items) == 0 {
		if len(terms) == 0 {
			return ""
		}
		return terms[0]
	}
	id := path + ":union"
	label := "UNION"
	if len(ops) > 0 {
		label = ops[0]
		for _, op := range ops[1:] {
			if op != label {
				label += " …" // mixed operators at one level; Detail lists them
				break
			}
		}
	}
	if !b.addNode(flowNode{ID: id, Kind: flowUnion, Label: label, Detail: strings.Join(ops, ", ")}) {
		if len(terms) == 0 {
			return ""
		}
		return terms[0]
	}
	for _, t := range terms {
		b.addEdge(t, id, "")
	}
	return id
}

// unionOpLabel renders one union item's operator ("UNION ALL", "EXCEPT", …).
func unionOpLabel(it grammar1.ISelectUnionStmtItemContext) string {
	op := "UNION"
	switch {
	case it.EXCEPT() != nil:
		op = "EXCEPT"
	case it.INTERSECT() != nil:
		op = "INTERSECT"
	}
	switch {
	case it.ALL() != nil:
		op += " ALL"
	case it.DISTINCT() != nil:
		op += " DISTINCT"
	}
	return op
}

// buildWithParens resolves a selectStmtWithParens: a plain SELECT builds its
// chain; a parenthesised union recurses one union level down.
func (b *flowBuilder) buildWithParens(sp grammar1.ISelectStmtWithParensContext, path string, depth int) (terminal string) {
	if sp == nil {
		return ""
	}
	if st := sp.SelectStmt(); st != nil {
		stmt, ok := st.(*grammar1.SelectStmtContext)
		if !ok {
			return ""
		}
		return b.buildSelect(stmt, path, depth)
	}
	return b.buildUnion(sp.SelectUnionStmt(), path, depth)
}

// buildSelect builds one SELECT's chain: the join tree of its sources, then
// the clause stages in ClickHouse's logical order — ARRAY JOIN → PREWHERE →
// WHERE → GROUP BY → HAVING → SELECT list → QUALIFY → DISTINCT → ORDER BY →
// LIMIT BY → LIMIT. Stages absent from the statement are absent from the
// chain; SETTINGS is not dataflow and is skipped. Returns the id of the last
// stage (at minimum the projection node).
func (b *flowBuilder) buildSelect(stmt *grammar1.SelectStmtContext, path string, depth int) (terminal string) {
	var scope *nanopass.SelectScope
	if b.scopeOf != nil {
		scope = b.scopeOf[stmt]
	}

	cur := ""
	if fc := stmt.FromClause(); fc != nil {
		srcSeq, joinSeq := 0, 0
		cur = b.buildJoinTree(fc.JoinExpr(), path, scope, depth, &srcSeq, &joinSeq)
	}

	// stage chains cur through one clause node; a cap-dropped node is skipped
	// (cur unchanged) so the chain stays connected.
	stage := func(idSuffix string, kind flowNodeKind, label, detail string, start, end int) {
		id := path + ":" + idSuffix
		if !b.addNode(flowNode{ID: id, Kind: kind, Label: label, Detail: detail, Start: start, End: end}) {
			return
		}
		b.addEdge(cur, id, "")
		cur = id
	}
	clause := func(idSuffix string, kind flowNodeKind, label string, ctx antlr.ParserRuleContext) {
		detail, start, end := b.snipRange(ctx)
		stage(idSuffix, kind, label, detail, start, end)
	}

	if ac := stmt.ArrayJoinClause(); ac != nil {
		label := "ARRAY JOIN"
		if ac.LEFT() != nil {
			label = "LEFT ARRAY JOIN"
		}
		clause("arrayjoin", flowJoin, label, ac)
	}
	if pw := stmt.PrewhereClause(); pw != nil {
		clause("prewhere", flowFilter, "PREWHERE", pw)
	}
	if wc := stmt.WhereClause(); wc != nil {
		clause("where", flowFilter, "WHERE", wc)
	}
	if gc := stmt.GroupByClause(); gc != nil {
		label := "GROUP BY"
		switch {
		case len(gc.AllCUBE()) > 0:
			label += " CUBE"
		case len(gc.AllROLLUP()) > 0:
			label += " ROLLUP"
		}
		if gc.TOTALS() != nil {
			label += " +TOTALS"
		}
		clause("group", flowAggregate, label, gc)
	}
	if hc := stmt.HavingClause(); hc != nil {
		clause("having", flowFilter, "HAVING", hc)
	}

	// The projection node is unconditional — grammar1 requires the SELECT
	// list — so even `SELECT 1` yields a chain. A window definition is folded
	// into its detail: grammar1 admits a single named window (ADR-0153), not
	// a stage of its own.
	pc := stmt.ProjectionClause()
	{
		detail, start, end := b.snipRange(pc)
		if wc := stmt.WindowClause(); wc != nil {
			detail += " · " + b.snippet(wc)
		}
		stage("select", flowProject, "SELECT", detail, start, end)
	}

	if qc := stmt.QualifyClause(); qc != nil {
		clause("qualify", flowFilter, "QUALIFY", qc)
	}
	if pc != nil && pc.DISTINCT() != nil {
		stage("distinct", flowDistinct, "DISTINCT", "", 0, 0)
	}
	if oc := stmt.OrderByClause(); oc != nil {
		label := "ORDER BY"
		if oc.FILL() != nil {
			label += " +FILL"
		}
		clause("order", flowSort, label, oc)
	}
	if lb := stmt.LimitByClause(); lb != nil {
		clause("limitby", flowLimit, "LIMIT BY", lb)
	}
	if lc := stmt.LimitClause(); lc != nil {
		clause("limit", flowLimit, "LIMIT", lc)
	}
	return cur
}

// buildJoinTree walks the left-deep join tree, returning the id of the node
// producing the joined stream. Source leaves consume scope.Tables entries by
// an incrementing counter — BuildScopes collects them in the same DFS
// left-to-right order this walk visits leaves (an order invariant pinned by
// TestFlowScopeTableOrderInvariant); on a counter overrun or absent scope the
// leaf is classified locally from the CST, which only loses the own-WITH
// IsCTE marking.
func (b *flowBuilder) buildJoinTree(je grammar1.IJoinExprContext, path string, scope *nanopass.SelectScope, depth int, srcSeq, joinSeq *int) string {
	switch t := je.(type) {
	case *grammar1.JoinExprTableContext:
		return b.buildSourceLeaf(t, path, scope, depth, srcSeq)
	case *grammar1.JoinExprParensContext:
		return b.buildJoinTree(t.JoinExpr(), path, scope, depth, srcSeq, joinSeq)
	case *grammar1.JoinExprOpContext:
		left := b.buildJoinTree(t.JoinExpr(0), path, scope, depth, srcSeq, joinSeq)
		right := b.buildJoinTree(t.JoinExpr(1), path, scope, depth, srcSeq, joinSeq)
		n := *joinSeq
		*joinSeq++
		id := path + ":join" + strconv.Itoa(n)
		label := "JOIN"
		if op := t.JoinOp(); op != nil {
			if s := b.snippet(op); s != "" {
				label = strings.ToUpper(s) + " JOIN"
			}
		}
		if t.GLOBAL() != nil {
			label = "GLOBAL " + label
		}
		detail, dStart, dEnd := b.snipRange(t.JoinConstraintClause())
		if !b.addNode(flowNode{ID: id, Kind: flowJoin, Label: truncateRunes(label, flowLabelRunes),
			Detail: detail, Start: dStart, End: dEnd}) {
			return left
		}
		b.addEdge(left, id, "l")
		b.addEdge(right, id, "r")
		return id
	case *grammar1.JoinExprCrossOpContext:
		left := b.buildJoinTree(t.JoinExpr(0), path, scope, depth, srcSeq, joinSeq)
		right := b.buildJoinTree(t.JoinExpr(1), path, scope, depth, srcSeq, joinSeq)
		n := *joinSeq
		*joinSeq++
		id := path + ":join" + strconv.Itoa(n)
		if !b.addNode(flowNode{ID: id, Kind: flowJoin, Label: "CROSS JOIN"}) {
			return left
		}
		b.addEdge(left, id, "l")
		b.addEdge(right, id, "r")
		return id
	}
	return ""
}

// buildSourceLeaf adds one FROM/JOIN source node, recursing into a
// FROM-subquery's own chain (depth-capped; an unexpanded subquery stays an
// opaque node).
func (b *flowBuilder) buildSourceLeaf(t *grammar1.JoinExprTableContext, path string, scope *nanopass.SelectScope, depth int, srcSeq *int) string {
	k := *srcSeq
	*srcSeq++
	id := path + ":src" + strconv.Itoa(k)

	kind, label, detail := b.classifySource(t, scope, k)

	var marks []string
	if detail != "" {
		marks = append(marks, detail)
	}
	if t.FINAL() != nil {
		marks = append(marks, "FINAL")
	}
	if sc := t.SampleClause(); sc != nil {
		marks = append(marks, b.snippet(sc))
	}

	// A subquery source expands into its own chain feeding the collector node
	// unless the depth cap is reached (the one unbounded recursion).
	inner := subqueryOf(t.TableExpr())
	expand := kind == flowSourceSubquery && inner != nil && depth+1 <= flowMaxDepth
	if kind == flowSourceSubquery && inner != nil && !expand {
		b.g.Capped = true
		marks = append(marks, "not expanded (depth cap)")
	}

	srcStart, srcEnd := 0, 0
	if r := b.pr.SourceRangeOf(t); !r.Empty() {
		srcStart, srcEnd = r.Start, r.End
	}
	if !b.addNode(flowNode{ID: id, Kind: kind, Label: truncateRunes(label, flowLabelRunes),
		Detail: truncateRunes(strings.Join(marks, " · "), flowSnippetRunes),
		Start:  srcStart, End: srcEnd}) {
		return ""
	}
	if expand {
		innerTerm := b.buildUnion(inner, path+"f"+strconv.Itoa(k), depth+1)
		b.addEdge(innerTerm, id, "")
	}
	return id
}

// classifySource resolves a leaf's kind and labels, preferring the scope's
// classification (scope.Tables[k]) and falling back to the CST. A bare
// unqualified name in the sibling set is a lifted CTE reference either way —
// the body text carries no WITH, so BuildScopes cannot know (ADR-0153 §SD2).
func (b *flowBuilder) classifySource(t *grammar1.JoinExprTableContext, scope *nanopass.SelectScope, k int) (kind flowNodeKind, label, detail string) {
	inSiblings := func(name string) bool {
		_, ok := b.siblings[name]
		return ok
	}
	if scope != nil && k < len(scope.Tables) {
		ts := scope.Tables[k]
		switch {
		case ts.IsCTE:
			kind = flowSourceCTE
			label = ts.Table
		case ts.IsSubquery:
			kind = flowSourceSubquery
			label = "(subquery)"
			if ts.Alias != "" {
				label = "(subquery) " + ts.Alias
			}
		case ts.IsFunction:
			kind = flowSourceFunction
			label = ts.Table + "(…)"
		default:
			kind = flowSourceTable
			label = ts.Table
			if ts.Database != "" {
				label = ts.Database + "." + ts.Table
			} else if inSiblings(ts.Table) {
				kind = flowSourceCTE
			}
		}
		if ts.Alias != "" && kind != flowSourceSubquery {
			detail = "AS " + ts.Alias
		}
		return
	}

	// Fallback: classify from the CST alone (scope failed or the order
	// invariant broke — the counter overran).
	te := t.TableExpr()
	for {
		alias, ok := te.(*grammar1.TableExprAliasContext)
		if !ok {
			break
		}
		te = alias.TableExpr()
	}
	switch x := te.(type) {
	case *grammar1.TableExprSubqueryContext:
		return flowSourceSubquery, "(subquery)", ""
	case *grammar1.TableExprFunctionContext:
		return flowSourceFunction, b.snippet(x.TableFunctionExpr()), ""
	case *grammar1.TableExprIdentifierContext:
		name := b.snippet(x.TableIdentifier())
		if !strings.Contains(name, ".") && inSiblings(name) {
			return flowSourceCTE, name, ""
		}
		return flowSourceTable, name, ""
	}
	return flowSourceTable, b.snippet(te), ""
}

// subqueryOf unwraps alias layers and returns a FROM-subquery's inner union,
// or nil for non-subquery sources.
func subqueryOf(te grammar1.ITableExprContext) grammar1.ISelectUnionStmtContext {
	for {
		switch x := te.(type) {
		case *grammar1.TableExprAliasContext:
			te = x.TableExpr()
		case *grammar1.TableExprSubqueryContext:
			return x.SelectUnionStmt()
		default:
			return nil
		}
	}
}
