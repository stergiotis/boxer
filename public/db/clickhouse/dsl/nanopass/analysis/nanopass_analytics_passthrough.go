package analysis

import (
	"sort"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// maxPassthroughDepth bounds the resolve-through recursion (subquery / CTE
// nesting) as defence-in-depth. The parser's own input guards already cap
// nesting depth well below this; the bound only matters if a future grammar
// change lets a cycle through.
const maxPassthroughDepth = 64

// ExtractPassthroughTables returns the base tables whose stored rows the query
// returns verbatim — "1:1 as stored". It separates pure information-retrieval
// reads (a table's rows surfacing unchanged, amenable to straightforward
// row/column security policy) from aggregates, derivations, and joins.
//
// A base table is reported iff, in the SELECT that reads it:
//
//   - the FROM has exactly one source — any JOIN / CROSS / comma join yields
//     nothing (a joined row is not a stored row of any single table);
//   - every projection item is a bare column (`c`, `t.c`) or a star (`*`,
//     `t.*`, `* EXCEPT c`) — any alias (`c AS x`, including a pure rename),
//     expression, function, CASE, cast, or scalar subquery taints the whole
//     table out. A column *subset* is still 1:1; a value or name change is not;
//   - the SELECT carries no GROUP BY, HAVING, DISTINCT, ARRAY JOIN, WINDOW, or
//     QUALIFY. WHERE / PREWHERE / ORDER BY / LIMIT are fine — they restrict or
//     reorder stored rows without transforming them;
//   - a subquery or non-recursive CTE source is followed down to its own base
//     tables, but only when that inner SELECT is itself a passthrough by these
//     same rules (a non-pure inner layer stops resolution and reports nothing).
//
// Multiple branches are combined only under `UNION ALL` (the reported set is
// the union of the branches' passthrough tables). Any `UNION DISTINCT`,
// `EXCEPT`, or `INTERSECT` combining a chain — top level or a resolved-through
// body — makes that whole chain non-1:1, since it dedups or set-combines the
// multiset.
//
// err is non-nil only when scope construction fails (a tree not produced by
// [nanopass.Parse]); "no passthrough tables" is an empty slice, not an error.
// A query that does not parse has no passthrough tables by definition: call
// [nanopass.Parse] first and treat a parse error as the empty result.
//
// deferred: the parenthesised column-modifier forms `* EXCEPT (a, b)`,
// `* REPLACE (…)`, and `* APPLY (…)` are not in Grammar1 and fail to parse
// (the bare `* EXCEPT c` form does parse and is handled); such a query
// therefore classifies as empty at the caller's Parse. REPLACE/APPLY transform
// columns and would never be 1:1 regardless.
func ExtractPassthroughTables(pr *nanopass.ParseResult, defaultDatabase string) (refs []TableRef, err error) {
	scopes, err := nanopass.BuildScopes(pr, defaultDatabase)
	if err != nil {
		err = eh.Errorf("ExtractPassthroughTables: %w", err)
		return
	}
	out := make(map[TableRef]struct{}, 4)
	collectChain(scopes, topLevelUnionStmt(pr), out, 0)
	refs = sortedTableRefs(out)
	return
}

// collectChain adds the passthrough base tables of one union chain to out. The
// chain contributes nothing unless every set operator combining its members is
// `UNION ALL` (unionNode carries the operators — nil means a single member, no
// operator). members are the per-branch scopes as flattened by BuildScopes.
func collectChain(members []*nanopass.SelectScope, unionNode *grammar1.SelectUnionStmtContext, out map[TableRef]struct{}, depth int) {
	if len(members) == 0 {
		return
	}
	if unionNode != nil && !unionChainIsUnionAll(unionNode) {
		return
	}
	for _, member := range members {
		collectSelect(member, out, depth)
	}
}

// collectSelect adds the base table read 1:1 by one SELECT scope to out,
// resolving through a subquery/CTE source when the whole scope qualifies.
func collectSelect(scope *nanopass.SelectScope, out map[TableRef]struct{}, depth int) {
	if depth > maxPassthroughDepth {
		return
	}
	if selectHasBlockingClause(scope) {
		return
	}
	if len(scope.Tables) != 1 {
		// no source (SELECT 1), or a join/comma/CROSS — not a single-relation scan.
		return
	}
	if !projectionIsAllVerbatim(scope) {
		return
	}
	ts := &scope.Tables[0]
	switch {
	case ts.IsFunction:
		// numbers(10), remote(…) — not a stored relation.
	case ts.IsCTE:
		def, found := scope.ResolveCTE(ts.Table)
		if !found || def.Recursive {
			// A recursive CTE is a fixpoint computation, not a stored relation.
			return
		}
		collectChain(def.Scopes, unionStmtOf(def.Node), out, depth+1)
	case ts.IsSubquery:
		collectChain(ts.Scopes, unionStmtOf(ts.Node), out, depth+1)
	default:
		out[TableRef{Database: ts.ResolvedDatabase(scope), Table: ts.Table}] = struct{}{}
	}
}

// selectHasBlockingClause reports whether the SELECT carries a clause that
// makes its output not a plain scan of stored rows: GROUP BY, HAVING, DISTINCT,
// ARRAY JOIN, WINDOW, or QUALIFY. WHERE / PREWHERE / ORDER BY / LIMIT are
// absent from this list on purpose — they restrict or reorder without
// transforming.
func selectHasBlockingClause(scope *nanopass.SelectScope) (blocked bool) {
	stmt := scope.Node
	if stmt.GroupByClause() != nil ||
		stmt.HavingClause() != nil ||
		stmt.ArrayJoinClause() != nil ||
		stmt.WindowClause() != nil ||
		stmt.QualifyClause() != nil {
		blocked = true
		return
	}
	if pc, ok := stmt.ProjectionClause().(*grammar1.ProjectionClauseContext); ok {
		if pc.DISTINCT() != nil {
			blocked = true
		}
	}
	return
}

// projectionIsAllVerbatim reports whether every projection item is a verbatim
// passthrough — a star (`*` / `t.*`) or a bare column identifier. A trailing
// `EXCEPT` clause (which only removes columns from a star) is intentionally
// ignored: `* EXCEPT c` is still a verbatim subset.
func projectionIsAllVerbatim(scope *nanopass.SelectScope) (ok bool) {
	pc, isPC := scope.Node.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !isPC {
		return
	}
	cel, isCEL := pc.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !isCEL {
		return
	}
	items := cel.AllColumnsExpr()
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		if !columnsExprIsVerbatim(item) {
			return
		}
	}
	ok = true
	return
}

// columnsExprIsVerbatim classifies one projection item. Only a star and a bare
// column identifier pass the stored data through unchanged; an alias (rename or
// derivation), any expression, and a scalar subquery do not.
func columnsExprIsVerbatim(item grammar1.IColumnsExprContext) (verbatim bool) {
	switch c := item.(type) {
	case *grammar1.ColumnsExprAsteriskContext:
		verbatim = true
	case *grammar1.ColumnsExprColumnContext:
		_, verbatim = c.ColumnExpr().(*grammar1.ColumnExprIdentifierContext)
	case *grammar1.ColumnsExprSubqueryContext:
		verbatim = false
	}
	return
}

// unionChainIsUnionAll reports whether every set operator combining the members
// of the union chain rooted at union is `UNION ALL`. It mirrors
// buildUnionScopes' flattening — descending through parenthesised nested unions
// but stopping at each selectStmt leaf, whose own subqueries are separate
// chains. Any EXCEPT / INTERSECT / UNION DISTINCT / bare UNION fails the check.
func unionChainIsUnionAll(union *grammar1.SelectUnionStmtContext) (allUnionAll bool) {
	allUnionAll = true
	for i := 0; i < union.GetChildCount(); i++ {
		switch c := union.GetChild(i).(type) {
		case *grammar1.SelectStmtWithParensContext:
			if !selectStmtWithParensIsUnionAll(c) {
				allUnionAll = false
				return
			}
		case *grammar1.SelectUnionStmtItemContext:
			if c.UNION() == nil || c.ALL() == nil {
				allUnionAll = false
				return
			}
			for j := 0; j < c.GetChildCount(); j++ {
				if swp, ok := c.GetChild(j).(*grammar1.SelectStmtWithParensContext); ok {
					if !selectStmtWithParensIsUnionAll(swp) {
						allUnionAll = false
						return
					}
				}
			}
		}
	}
	return
}

// selectStmtWithParensIsUnionAll recurses through a parenthesised nested union,
// stopping at a plain selectStmt leaf.
func selectStmtWithParensIsUnionAll(node *grammar1.SelectStmtWithParensContext) (allUnionAll bool) {
	allUnionAll = true
	for i := 0; i < node.GetChildCount(); i++ {
		switch c := node.GetChild(i).(type) {
		case *grammar1.SelectStmtContext:
			return
		case *grammar1.SelectUnionStmtContext:
			allUnionAll = unionChainIsUnionAll(c)
			return
		}
	}
	return
}

// unionStmtOf finds the selectUnionStmt a subquery/CTE wrapper encloses: a
// FROM/expression subquery holds it as a direct child; a CTE's namedQuery holds
// it one level down, under the query rule. Returns nil if none is found.
func unionStmtOf(wrapper antlr.ParserRuleContext) (union *grammar1.SelectUnionStmtContext) {
	if wrapper == nil {
		return
	}
	for i := 0; i < wrapper.GetChildCount(); i++ {
		switch c := wrapper.GetChild(i).(type) {
		case *grammar1.SelectUnionStmtContext:
			union = c
			return
		case *grammar1.QueryContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if u, ok := c.GetChild(j).(*grammar1.SelectUnionStmtContext); ok {
					union = u
					return
				}
			}
		}
	}
	return
}

// topLevelUnionStmt returns the outermost selectUnionStmt (the one combining
// the query's top-level branches). Relies on the queryStmt → query →
// selectUnionStmt shape that BuildScopes has already validated.
func topLevelUnionStmt(pr *nanopass.ParseResult) (union *grammar1.SelectUnionStmtContext) {
	tree := pr.Tree
	if tree == nil || tree.GetChildCount() == 0 {
		return
	}
	if q, ok := tree.GetChild(0).(*grammar1.QueryContext); ok {
		union = unionStmtOf(q)
	}
	return
}

// sortedTableRefs flattens the set into a deterministic slice ordered by
// (Database, Table).
func sortedTableRefs(set map[TableRef]struct{}) (refs []TableRef) {
	refs = make([]TableRef, 0, len(set))
	for ref := range set {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Database != refs[j].Database {
			return refs[i].Database < refs[j].Database
		}
		return refs[i].Table < refs[j].Table
	})
	return
}
