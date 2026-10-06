package nanopass

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
)

// nanopass_scope_derived.go answers one question for schema-bound passes: may
// a CTE or FROM subquery stand in for the stored table it reads?
//
// A pass that turns a name into physical columns (a leeway section, a column
// handle) needs a stored table's schema. Analytical SQL usually reaches that
// table through a CTE that narrows its rows — `WITH e AS (SELECT * FROM t
// WHERE …) SELECT … FROM e` — and the table's columns arrive in the outer
// SELECT under their own names. These two helpers let such a pass follow the
// derived source to the table and decide whether the columns made it through.

// DerivedBodies returns the body scopes of a CTE or FROM-subquery source of
// this scope, one per UNION branch.
//
// why is non-empty when the source is one a pass must not follow: a CTE bound
// twice in one WITH clause (ClickHouse rejects the query, see CTEDef.Ambiguous)
// or a recursive one, whose columns are not its first branch's. A base table,
// a table function, or a CTE name that resolves to nothing returns no bodies
// and no reason.
func (inst *SelectScope) DerivedBodies(ts *TableSource) (bodies []*SelectScope, why string) {
	switch {
	case ts.IsSubquery:
		bodies = ts.Scopes
	case ts.IsCTE:
		def, found := inst.ResolveCTE(ts.Table)
		switch {
		case !found:
		case def.Ambiguous:
			why = "its name is bound more than once in one WITH clause"
		case def.Recursive:
			why = "a recursive CTE is not followed"
		default:
			bodies = def.Scopes
		}
	}
	return
}

// StarPassthrough reports why body does NOT hand the columns of its source
// named carrier to its reader under their own names, or "" when it does.
//
// It does when the projection holds a bare `*` or `carrier.*`, and nothing
// drops or replaces a column: no `* EXCEPT`, no ARRAY JOIN (which can replace
// an array column with one element under the same name), no GROUP BY. carrier
// is the source's alias, or its table name when unaliased — the name a
// qualified star in the body would use.
//
// The check is syntactic and errs closed. A later projection item aliased to
// one of the carrier's column names would shadow it; it is not detected.
func StarPassthrough(body *SelectScope, carrier string) (why string) {
	stmt := body.Node
	if stmt == nil {
		return "its body could not be read"
	}
	if stmt.ArrayJoinClause() != nil {
		return "its body has an ARRAY JOIN, which can replace a column with one element"
	}
	if stmt.GroupByClause() != nil {
		return "its body aggregates"
	}
	proj, isProj := stmt.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !isProj || proj == nil {
		return "its body has no projection"
	}
	if proj.ProjectionExceptClause() != nil {
		return "its body projects * EXCEPT, which may drop the columns"
	}
	list, isList := proj.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !isList || list == nil {
		return "its body has no projection"
	}
	for _, item := range list.AllColumnsExpr() {
		star, isStar := item.(*grammar1.ColumnsExprAsteriskContext)
		if !isStar {
			continue
		}
		tid, qualified := star.TableIdentifier().(*grammar1.TableIdentifierContext)
		if !qualified || tid == nil {
			return ""
		}
		if id := tid.Identifier(); id != nil && DecodeIdentifier(id.GetText()) == carrier {
			return ""
		}
	}
	return "its body does not project * from the table it reads"
}
