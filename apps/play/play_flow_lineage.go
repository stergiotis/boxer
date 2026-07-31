package play

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_flow_lineage.go is the ADR-0153 lineage lens: column-level provenance
// of the active node's SELECT list, derived statically like the statement
// lens. Each output column is a node fed by the source columns its expression
// references, resolved through scopes and aliases; what cannot be resolved is
// flagged, never guessed. Deliberate v1 scope: the SELECT list only — columns
// a WHERE or GROUP BY consumes without projecting are not drawn (that is
// filter provenance, not output lineage), and lineage stays inside the active
// node (a sibling CTE is an opaque source here exactly as on the statement
// lens).
//
// Resolution follows ClickHouse's own precedence: a bare identifier that
// names a select-list alias resolves to that alias (expression aliases
// shadow columns — the alias-in-WHERE behaviour), then to the single FROM
// source, and with several sources it is reported ambiguous.

// lineageRef is one column identifier occurrence inside a select item.
type lineageRef struct {
	table  string // qualifier as written ("" when bare)
	column string
	start  int // byte range of the identifier in the parsed SQL
	end    int
}

// buildLineageGraph derives the lineage graph. note carries a non-error
// caveat the panel shows (today: union statements trace their first member).
func buildLineageGraph(sql string, siblingCTEs map[string]struct{}) (g flowGraph, note string, err error) {
	if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
		err = eh.Errorf("lineage: not a SELECT-shaped statement (%s)", kind)
		return
	}
	pr, pErr := nanopass.Parse(sql)
	if pErr != nil {
		err = eh.Errorf("lineage: parse: %w", pErr)
		return
	}
	scopes, sErr := nanopass.BuildScopes(pr, "")
	if sErr != nil {
		err = eh.Errorf("lineage: scopes: %w", sErr)
		return
	}
	if len(scopes) == 0 || scopes[0].Node == nil {
		err = eh.Errorf("lineage: no root scope")
		return
	}
	scope := scopes[0]
	if len(scope.UnionMembers) > 1 {
		scope = scope.UnionMembers[0]
		note = "union statement — lineage of the first member"
	}
	pc := scope.Node.ProjectionClause()
	if pc == nil || pc.ColumnExprList() == nil {
		err = eh.Errorf("lineage: no select list")
		return
	}

	b := lineageBuilder{pr: pr, scope: scope, siblings: siblingCTEs,
		a: newLensGraphAssembler(), aliasIdx: map[string]int{}}
	items := pc.ColumnExprList().AllColumnsExpr()

	// First pass: the alias namespace. ClickHouse lets a later expression use
	// an earlier (or even later) alias, so the whole list registers before
	// any reference resolves.
	for i, it := range items {
		if name := itemAlias(it); name != "" {
			if _, dup := b.aliasIdx[name]; !dup {
				b.aliasIdx[name] = i
			}
		}
	}
	for i, it := range items {
		b.item(i, it)
	}
	g = b.a.g
	return
}

// lineageBuilder carries the per-statement walk state.
type lineageBuilder struct {
	pr       *nanopass.ParseResult
	scope    *nanopass.SelectScope
	siblings map[string]struct{}
	a        *lensGraphAssembler
	aliasIdx map[string]int
	edgeSeen map[[2]string]struct{}
}

// item adds one select-list item's output node and its incoming edges.
func (b *lineageBuilder) item(i int, it grammar1.IColumnsExprContext) {
	outID := "out:" + strconv.Itoa(i)
	switch t := it.(type) {
	case *grammar1.ColumnsExprAsteriskContext:
		b.star(outID, t)
		return
	case *grammar1.ColumnsExprSubqueryContext:
		snip, start, end := b.snip(t)
		b.a.addNode(flowNode{ID: outID, Kind: flowColumnOut, Label: "(subquery)",
			Detail: truncateRunes(snip, flowSnippetRunes) + " · scalar subquery — not traced",
			Start:  start, End: end})
		return
	case *grammar1.ColumnsExprColumnContext:
		expr := t.ColumnExpr()
		snip, start, end := b.snip(t)
		refs, sawSubquery := b.collectRefs(expr)
		label := itemAlias(it)
		if label == "" {
			if len(refs) == 1 && strings.EqualFold(strings.TrimSpace(snip), refText(refs[0])) {
				label = refs[0].column
			} else {
				label = snip
			}
		}
		detail := snip
		if sawSubquery {
			detail += " · subquery inside — not traced"
		}
		if !b.a.addNode(flowNode{ID: outID, Kind: flowColumnOut,
			Label: truncateRunes(label, flowLabelRunes),
			Detail: truncateRunes(detail, flowSnippetRunes), Start: start, End: end}) {
			return
		}
		for _, ref := range refs {
			b.edge(i, outID, ref)
		}
	}
}

// star handles `*` and `t.*`: one output node fed by a whole-table column
// node per matching source. Offline the panel cannot know the column set —
// the star stays a star.
func (b *lineageBuilder) star(outID string, t *grammar1.ColumnsExprAsteriskContext) {
	label := "*"
	var only string
	if ti := t.TableIdentifier(); ti != nil {
		only = nanopass.DecodeIdentifier(ti.Identifier().GetText())
		label = only + ".*"
	}
	_, start, end := b.snip(t)
	if !b.a.addNode(flowNode{ID: outID, Kind: flowColumnOut, Label: truncateRunes(label, flowLabelRunes),
		Detail: "all columns of the matching sources", Start: start, End: end}) {
		return
	}
	for k := range b.scope.Tables {
		ts := &b.scope.Tables[k]
		key := sourceKey(ts)
		if only != "" && only != key && only != ts.Table {
			continue
		}
		id := "src:" + key + ".*"
		if _, exists := b.a.ids[id]; !exists {
			if !b.a.addNode(flowNode{ID: id, Kind: flowColumnSrc, Label: truncateRunes(key+".*", flowLabelRunes),
				Detail: b.sourceDetail(ts)}) {
				return
			}
		}
		b.addEdgeOnce(id, outID)
	}
}

// edge resolves one identifier reference and draws its edge: select-alias
// first (CH shadowing), then the FROM sources, ambiguity flagged.
func (b *lineageBuilder) edge(item int, outID string, ref lineageRef) {
	if ref.table == "" {
		if j, isAlias := b.aliasIdx[ref.column]; isAlias && j != item {
			b.addEdgeOnce("out:"+strconv.Itoa(j), outID)
			return
		}
	}
	var srcID string
	switch {
	case ref.table != "":
		if ts, ok := b.scope.ResolveAlias(ref.table); ok {
			srcID = b.sourceColumn(&ts, ref)
		} else {
			srcID = b.flaggedColumn(ref, ref.table+"."+ref.column,
				"unresolved qualifier `"+ref.table+"`")
		}
	case len(b.scope.Tables) == 1:
		srcID = b.sourceColumn(&b.scope.Tables[0], ref)
	case len(b.scope.Tables) == 0:
		srcID = b.flaggedColumn(ref, ref.column, "no FROM source — unresolved")
	default:
		srcID = b.flaggedColumn(ref, ref.column,
			fmt.Sprintf("ambiguous — %d candidate sources", len(b.scope.Tables)))
	}
	b.addEdgeOnce(srcID, outID)
}

// sourceColumn adds (once) the column node of a resolved FROM source.
func (b *lineageBuilder) sourceColumn(ts *nanopass.TableSource, ref lineageRef) (id string) {
	key := sourceKey(ts)
	id = "src:" + key + "." + ref.column
	if _, exists := b.a.ids[id]; exists {
		return
	}
	if !b.a.addNode(flowNode{ID: id, Kind: flowColumnSrc,
		Label:  truncateRunes(key+"."+ref.column, flowLabelRunes),
		Detail: b.sourceDetail(ts), Start: ref.start, End: ref.end}) {
		return ""
	}
	return
}

// flaggedColumn adds (once) a column node lineage could not resolve — drawn,
// with the reason, rather than guessed or dropped.
func (b *lineageBuilder) flaggedColumn(ref lineageRef, label, why string) (id string) {
	id = "src:?." + label
	if _, exists := b.a.ids[id]; exists {
		return
	}
	if !b.a.addNode(flowNode{ID: id, Kind: flowColumnSrc, Label: truncateRunes(label, flowLabelRunes),
		Detail: why, Start: ref.start, End: ref.end}) {
		return ""
	}
	return
}

// sourceDetail names what a source column belongs to.
func (b *lineageBuilder) sourceDetail(ts *nanopass.TableSource) string {
	switch {
	case ts.IsCTE:
		return "CTE " + ts.Table
	case ts.IsSubquery:
		return "FROM subquery"
	case ts.IsFunction:
		return "table function " + ts.Table
	}
	name := ts.Table
	if ts.Database != "" {
		name = ts.Database + "." + ts.Table
	}
	if _, sibling := b.siblings[ts.Table]; sibling && ts.Database == "" {
		return "CTE " + name
	}
	return "table " + name
}

func (b *lineageBuilder) addEdgeOnce(from, to string) {
	if from == "" || to == "" {
		return
	}
	if b.edgeSeen == nil {
		b.edgeSeen = map[[2]string]struct{}{}
	}
	key := [2]string{from, to}
	if _, dup := b.edgeSeen[key]; dup {
		return
	}
	b.edgeSeen[key] = struct{}{}
	b.a.addEdge(from, to)
}

// collectRefs gathers the column identifiers of one item's expression,
// pruning nested SELECTs — their identifiers belong to inner scopes and
// would otherwise read as outer-column lineage.
func (b *lineageBuilder) collectRefs(expr antlr.ParserRuleContext) (refs []lineageRef, sawSubquery bool) {
	if expr == nil {
		return
	}
	nanopass.WalkCST(expr, func(ctx antlr.ParserRuleContext) bool {
		if _, nested := ctx.(*grammar1.SelectStmtContext); nested {
			sawSubquery = true
			return false
		}
		cid, ok := ctx.(*grammar1.ColumnIdentifierContext)
		if !ok {
			return true
		}
		ref := lineageRef{}
		if ti := cid.TableIdentifier(); ti != nil {
			ref.table = nanopass.DecodeIdentifier(ti.Identifier().GetText())
		}
		if ni := cid.NestedIdentifier(); ni != nil {
			ids := ni.AllIdentifier()
			segs := make([]string, 0, len(ids))
			for _, id := range ids {
				segs = append(segs, nanopass.DecodeIdentifier(id.GetText()))
			}
			ref.column = strings.Join(segs, ".")
		}
		if ref.column == "" {
			return false
		}
		if r := b.pr.SourceRangeOf(cid); !r.Empty() {
			ref.start, ref.end = r.Start, r.End
		}
		refs = append(refs, ref)
		return false // the identifier is a leaf for our purposes
	})
	return
}

// snip is the item's collapsed source text plus its byte range.
func (b *lineageBuilder) snip(ctx antlr.ParserRuleContext) (string, int, int) {
	r := b.pr.SourceRangeOf(ctx)
	if r.Empty() {
		return "", 0, 0
	}
	return strings.Join(strings.Fields(b.pr.Source[r.Start:r.End]), " "), r.Start, r.End
}

// itemAlias returns a select item's alias name ("" when unaliased).
func itemAlias(it grammar1.IColumnsExprContext) string {
	col, ok := it.(*grammar1.ColumnsExprColumnContext)
	if !ok {
		return ""
	}
	al, ok := col.ColumnExpr().(*grammar1.ColumnExprAliasContext)
	if !ok {
		return ""
	}
	if a := al.Alias(); a != nil {
		return nanopass.DecodeIdentifier(a.GetText())
	}
	if id := al.Identifier(); id != nil {
		return nanopass.DecodeIdentifier(id.GetText())
	}
	return ""
}

// sourceKey is how a FROM source is spoken of in lineage node ids and labels:
// the alias when one exists (two joins of one table stay distinct), else the
// table name.
func sourceKey(ts *nanopass.TableSource) string {
	if ts.Alias != "" {
		return ts.Alias
	}
	return ts.Table
}

// refText renders a reference the way the buffer would spell it bare.
func refText(r lineageRef) string {
	if r.table != "" {
		return r.table + "." + r.column
	}
	return r.column
}
