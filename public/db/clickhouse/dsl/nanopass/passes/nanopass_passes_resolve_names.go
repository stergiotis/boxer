package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ColumnResolverI maps a user-written column handle within a specific table to
// that table's physical column name. It is deliberately domain-agnostic: the
// leeway implementation lives in leeway/lwsql, keeping this SQL framework free
// of any leeway dependency (the same reason SchemaProviderI is generic).
//
// Resolve reports ok=false to mean "leave this identifier untouched". That
// covers both "the handle is not a known alias for this table" and "the handle
// is ambiguous" — in either case the pass leaves the identifier as written, so
// real ClickHouse columns, SELECT-list aliases, and genuinely ambiguous
// references all fall through to the server unchanged rather than being
// mis-rewritten.
type ColumnResolverI interface {
	Resolve(dbName string, tableName string, handle string) (physical string, ok bool)
}

// ResolveColumnNames returns a Pass that rewrites bare and table-qualified
// column identifiers to their physical names via the resolver, wherever a
// column reference appears — the projection, WHERE, GROUP BY, ORDER BY, HAVING,
// and nested expressions — not only the SELECT list. That is the whole reason
// this is a substitution pass rather than a COLUMNS('…') wrapper: COLUMNS is
// projection-only, whereas an identifier substitution is legal everywhere a
// column is.
//
// Resolution is scope-aware. Each SELECT's own FROM/JOIN tables are the
// candidates for its bare identifiers; a qualified `alias.handle` is resolved
// against that alias's table only. A bare handle that resolves in exactly one
// in-scope table is rewritten; zero or multiple matches are left untouched.
// CTE, subquery, and table-function sources are skipped (they have no physical
// schema to resolve against). The rewrite is one-directional and never renames
// output columns — result-side friendly labels are a presentation concern.
func ResolveColumnNames(resolver ColumnResolverI, defaultDatabase string) nanopass.Pass {
	return nanopass.LiftBodyPass(
		"ResolveColumnNames",
		func(sql string) (result string, err error) {
			pr, err := nanopass.Parse(sql)
			if err != nil {
				err = eh.Errorf("ResolveColumnNames: %w", err)
				return
			}
			rw := nanopass.NewRewriter(pr)

			scopes, err := nanopass.BuildScopes(pr, defaultDatabase)
			if err != nil {
				err = eh.Errorf("ResolveColumnNames: %w", err)
				return
			}
			for _, scope := range nanopass.FlattenScopes(scopes) {
				resolveNamesInScope(rw, scope, resolver)
			}

			result = nanopass.GetText(rw)
			return
		},
		nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody,
			Writes:     nanopass.RegionBody,
		},
	)
}

// resolveNamesInScope walks one SELECT's whole subtree (not just its
// projection) and resolves every bare column identifier it owns. Nested scopes
// are pruned via isScopeBoundary — FlattenScopes visits each one in its own
// turn, so descending here would resolve them against the wrong table set.
func resolveNamesInScope(rw nanopass.RewriterI, scope *nanopass.SelectScope, resolver ColumnResolverI) {
	stmt := scope.Node
	nanopass.WalkCST(stmt, func(ctx antlr.ParserRuleContext) bool {
		// Prune nested scopes, but never the root stmt itself (it is a
		// SelectStmtContext, hence a scope boundary too).
		if ctx != antlr.ParserRuleContext(stmt) && isScopeBoundary(ctx) {
			return false
		}
		identExpr, ok := ctx.(*grammar1.ColumnExprIdentifierContext)
		if !ok {
			return true
		}
		resolveColumnIdentifier(rw, scope, resolver, identExpr)
		// A column identifier carries no further column references worth
		// descending into.
		return false
	})
}

func resolveColumnIdentifier(rw nanopass.RewriterI, scope *nanopass.SelectScope, resolver ColumnResolverI, identExpr *grammar1.ColumnExprIdentifierContext) {
	colId := identExpr.ColumnIdentifier()
	if colId == nil {
		return
	}
	colIdCtx, ok := colId.(*grammar1.ColumnIdentifierContext)
	if !ok {
		return
	}
	nested := colIdCtx.NestedIdentifier()
	if nested == nil {
		return
	}
	nestedCtx, ok := nested.(*grammar1.NestedIdentifierContext)
	if !ok {
		return
	}
	// Only a single-part identifier is a handle. A dotted nested identifier
	// (`col.field` tuple/nested access) is left alone.
	if len(nestedCtx.AllIdentifier()) != 1 {
		return
	}
	handle := nanopass.DecodeIdentifier(nestedCtx.GetText())
	if handle == "" {
		return
	}

	var physical string
	var resolved bool
	if tid := colIdCtx.TableIdentifier(); tid != nil {
		// Qualified `alias.handle` — resolve against that one table.
		src, found := scope.ResolveAlias(nanopass.DecodeIdentifier(tid.GetText()))
		if !found || src.IsCTE || src.IsSubquery || src.IsFunction {
			return
		}
		physical, resolved = resolver.Resolve(src.ResolvedDatabase(scope), src.Table, handle)
	} else {
		physical, resolved = resolveBareHandle(scope, resolver, handle)
	}
	if !resolved {
		return
	}
	// Rewrite only the nested-identifier node: for a bare reference this is the
	// whole column, and for a qualified `alias.handle` it keeps the `alias.`
	// prefix intact (which is what disambiguates a JOIN). Physical names carry
	// ':' separators, so they always need quoting — QuoteIdentifier handles it.
	nanopass.ReplaceNode(rw, nestedCtx, nanopass.QuoteIdentifier(physical))
}

// resolveBareHandle resolves an unqualified handle against every real table in
// scope. Exactly one match wins; zero or several leave the identifier
// untouched (a non-leeway column, or an ambiguous reference the server should
// report).
func resolveBareHandle(scope *nanopass.SelectScope, resolver ColumnResolverI, handle string) (physical string, ok bool) {
	matches := 0
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
			continue
		}
		p, r := resolver.Resolve(ts.ResolvedDatabase(scope), ts.Table, handle)
		if r {
			physical = p
			matches++
		}
	}
	if matches == 1 {
		return physical, true
	}
	return "", false
}
