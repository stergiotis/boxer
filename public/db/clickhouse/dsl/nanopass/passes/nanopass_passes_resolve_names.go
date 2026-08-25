package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ResolveKind classifies what a ColumnResolverI made of one identifier.
type ResolveKind uint8

const (
	// ResolveNotAHandle: the identifier is not a resolvable handle (e.g. an
	// ordinary column, an alias, or a table whose schema is unknown). Leave it
	// untouched and say nothing.
	ResolveNotAHandle ResolveKind = iota
	// ResolveOK: Physical carries one or more physical column names to
	// substitute (several for a whole-section `:*` expansion).
	ResolveOK
	// ResolveUnknownSection: a handle whose section part names no known section.
	ResolveUnknownSection
	// ResolveUnknownColumn: the section is known but the column is not one of
	// its columns; Candidates lists the section's columns.
	ResolveUnknownColumn
)

// ResolveResult is a ColumnResolverI's verdict for one identifier.
type ResolveResult struct {
	Kind       ResolveKind
	Physical   []string // ResolveOK: the physical name(s) to splice in
	Section    string   // display form, for diagnostics
	Column     string   // display form, for diagnostics
	Candidates []string // ResolveUnknownColumn: the section's column names
}

// ColumnResolverI maps a user-written column handle within a table to its
// physical name(s), or reports why it could not. It is domain-agnostic — the
// leeway implementation (and the policy for what even counts as a handle) lives
// in leeway/lwsql. The pass calls Resolve for every bare/qualified identifier;
// a ResolveNotAHandle verdict means "leave it alone", so ordinary SQL passes
// through untouched.
type ColumnResolverI interface {
	Resolve(dbName string, tableName string, handle string) ResolveResult
}

// HandleSyntaxI optionally reports whether an identifier is SPELLED as a
// handle, independent of any table. It is what lets the pass warn about a
// handle whose source has no catalog schema — a CTE, a subquery, a table
// function — without warning about every ordinary column beside it: the
// resolver is never consulted in that case, so nothing else in the pass can
// tell a handle from plain SQL.
//
// Asserted off the ColumnResolverI rather than added to it, so an
// implementation that does not offer the distinction keeps working and simply
// gets no such diagnostic (the same optional-capability shape the pass
// registry uses for its bindings).
type HandleSyntaxI interface {
	IsHandleSyntax(name string) bool
}

// ColumnDiagnostic is a warning about one handle that a ResolveColumnNames pass
// could not resolve. It is emitted only when a sink is supplied (the execution
// path passes none), so a host can surface it — e.g. play's Diagnostics pane —
// before the query round-trips to the server.
type ColumnDiagnostic struct {
	Handle     string   // the handle as written, e.g. "geoPoint:lat"
	Message    string   // human-readable explanation
	Candidates []string // suggested column names (may be empty)
}

// ResolveColumnNames returns a Pass that rewrites column handles to their
// physical names via the resolver, wherever a column reference appears —
// projection, WHERE, GROUP BY, ORDER BY, HAVING, ARRAY JOIN, nested
// expressions. A `:*` handle expands to a comma-separated list of the section's
// columns, so it works in ARRAY JOIN (co-array unnest) and the projection
// alike; ClickHouse validates positions where a list is illegal.
//
// If sink is non-nil, unresolved handles (unknown section / unknown column)
// are reported through it instead of silently passing through; supply nil on
// the execution path and a collector where you want to warn the user first.
func ResolveColumnNames(resolver ColumnResolverI, defaultDatabase string, sink func(ColumnDiagnostic)) nanopass.Pass {
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
			flat := nanopass.FlattenScopes(scopes)
			for _, scope := range flat {
				resolveNamesInScope(rw, scope, resolver, sink)
			}
			resolveNamesInQueryCTEs(rw, pr, flat, resolver, sink)

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
// projection — ARRAY JOIN and the rest carry column refs too) and resolves
// every column identifier it owns. Nested scopes are pruned; FlattenScopes
// visits each one against its own table set.
func resolveNamesInScope(rw nanopass.RewriterI, scope *nanopass.SelectScope, resolver ColumnResolverI, sink func(ColumnDiagnostic)) {
	resolveNamesUnder(rw, scope.Node, scope, resolver, sink)
}

// resolveNamesUnder resolves every column identifier under root against scope,
// pruning nested scopes (which are visited separately, against their own table
// sets). root itself is never treated as a boundary.
func resolveNamesUnder(rw nanopass.RewriterI, root antlr.ParserRuleContext, scope *nanopass.SelectScope, resolver ColumnResolverI, sink func(ColumnDiagnostic)) {
	nanopass.WalkCST(root, func(ctx antlr.ParserRuleContext) bool {
		if ctx != root && isScopeBoundary(ctx) {
			return false
		}
		identExpr, ok := ctx.(*grammar1.ColumnExprIdentifierContext)
		if !ok {
			return true
		}
		resolveColumnIdentifier(rw, scope, resolver, sink, identExpr)
		return false
	})
}

// resolveNamesInQueryCTEs resolves handles in query-level WITH *expressions*.
//
// `WITH <expr> AS name SELECT …` parses as the query rule's `ctes`, which is a
// sibling of selectUnionStmt rather than a child of selectStmt — so a walk
// anchored at a scope's Node never reaches it, and a handle bound to an alias
// shipped unexpanded and failed UNKNOWN_IDENTIFIER. (A selectStmt-level
// `withClause` *is* inside that subtree and was already covered.)
//
// Each ctes node is resolved against the first SELECT it precedes: a
// query-level WITH expression is visible to every UNION member and has to be
// valid in all of them, so the first member's tables are the binding context.
// CTE bodies (`WITH c AS (SELECT …)`) are pruned — they are scope boundaries
// with their own entry in FlattenScopes.
func resolveNamesInQueryCTEs(rw nanopass.RewriterI, pr *nanopass.ParseResult, flat []*nanopass.SelectScope, resolver ColumnResolverI, sink func(ColumnDiagnostic)) {
	if pr.Tree == nil {
		return
	}
	byNode := make(map[*grammar1.SelectStmtContext]*nanopass.SelectScope, len(flat))
	for _, s := range flat {
		byNode[s.Node] = s
	}
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		ctes, ok := ctx.(*grammar1.CtesContext)
		if !ok {
			return true
		}
		if scope := scopeOfFirstSelect(ctx.GetParent(), byNode); scope != nil {
			resolveNamesUnder(rw, ctes, scope, resolver, sink)
		}
		return false
	})
}

// scopeOfFirstSelect finds the scope of the first selectStmt under the node a
// ctes clause hangs off: the selectUnionStmt it heads, or — for a non-first
// union arm — that arm's selectUnionStmtItem. ADR-0196 §SD5 put the clause on
// those two nodes; before it, the caller passed the enclosing `query`.
//
// Returns nil when the shape has no built scope — one BuildScopes declined to
// model, which is left alone rather than guessed at.
func scopeOfFirstSelect(withParent antlr.Tree, byNode map[*grammar1.SelectStmtContext]*nanopass.SelectScope) (scope *nanopass.SelectScope) {
	if withParent == nil {
		return
	}
	switch withParent.(type) {
	case *grammar1.SelectUnionStmtContext, *grammar1.SelectUnionStmtItemContext:
	default:
		return
	}
	nanopass.WalkCST(withParent, func(ctx antlr.ParserRuleContext) bool {
		if scope != nil {
			return false
		}
		stmt, ok := ctx.(*grammar1.SelectStmtContext)
		if !ok {
			return true
		}
		scope = byNode[stmt]
		return false
	})
	return
}

func resolveColumnIdentifier(rw nanopass.RewriterI, scope *nanopass.SelectScope, resolver ColumnResolverI, sink func(ColumnDiagnostic), identExpr *grammar1.ColumnExprIdentifierContext) {
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
	if !ok || len(nestedCtx.AllIdentifier()) != 1 {
		return
	}
	handle := nanopass.DecodeIdentifier(nestedCtx.GetText())
	if handle == "" {
		return
	}

	var aliasPrefix string
	var res ResolveResult
	if tid := colIdCtx.TableIdentifier(); tid != nil {
		src, found := scope.ResolveAlias(nanopass.DecodeIdentifier(tid.GetText()))
		if !found || src.IsCTE || src.IsSubquery || src.IsFunction {
			// An unknown alias is left alone: the scope may simply not model
			// the shape. A source that IS resolved and carries no catalog
			// schema is different — the handle cannot resolve in principle,
			// which is worth saying rather than letting the server answer
			// UNKNOWN_IDENTIFIER about a name the user never typed.
			if found {
				reportNoCatalogSource(sink, resolver, handle, sourceLabels([]nanopass.TableSource{src}))
			}
			return
		}
		aliasPrefix = tid.GetText() + "."
		res = resolver.Resolve(src.ResolvedDatabase(scope), src.Table, handle)
	} else {
		res = resolveBareAcrossScope(scope, resolver, handle)
		// Every source in scope is a CTE, a subquery or a table function, so
		// no resolver was consulted and the verdict above is a default rather
		// than an answer. Report it — but only here: with a real table in
		// scope, a NotAHandle verdict came from the resolver deciding the
		// name is ordinary SQL or the table is not leeway-shaped, and warning
		// on that would flag every plain column.
		if res.Kind == ResolveNotAHandle && len(scope.Tables) > 0 && !scopeHasCatalogTable(scope) {
			reportNoCatalogSource(sink, resolver, handle, sourceLabels(scope.Tables))
			return
		}
	}

	switch res.Kind {
	case ResolveOK:
		if len(res.Physical) == 0 {
			return
		}
		parts := make([]string, len(res.Physical))
		for i, p := range res.Physical {
			parts[i] = aliasPrefix + nanopass.QuoteIdentifier(p)
		}
		// Replace the whole identifier (not just its nested part), so a
		// qualified `:*` gets the alias on every expanded column.
		nanopass.ReplaceNode(rw, identExpr, strings.Join(parts, ", "))
	case ResolveUnknownSection:
		if sink != nil {
			sink(ColumnDiagnostic{Handle: handle, Message: fmt.Sprintf("unknown leeway section %q", res.Section)})
		}
	case ResolveUnknownColumn:
		if sink != nil {
			sink(ColumnDiagnostic{
				Handle:     handle,
				Message:    fmt.Sprintf("leeway section %q has no column %q", res.Section, res.Column),
				Candidates: res.Candidates,
			})
		}
	}
}

// resolveBareAcrossScope resolves an unqualified handle against every real
// table in scope. Exactly one table resolving it wins; several is ambiguous
// (left untouched). With none, the most specific failure is returned for a
// diagnostic — an unknown-column (the section exists somewhere) outranks an
// unknown-section.
func resolveBareAcrossScope(scope *nanopass.SelectScope, resolver ColumnResolverI, handle string) ResolveResult {
	oks := 0
	var win ResolveResult
	best := ResolveResult{Kind: ResolveNotAHandle}
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
			continue
		}
		r := resolver.Resolve(ts.ResolvedDatabase(scope), ts.Table, handle)
		switch r.Kind {
		case ResolveOK:
			oks++
			win = r
		case ResolveUnknownColumn:
			best = r
		case ResolveUnknownSection:
			if best.Kind == ResolveNotAHandle {
				best = r
			}
		}
	}
	if oks == 1 {
		return win
	}
	if oks > 1 {
		return ResolveResult{Kind: ResolveNotAHandle} // ambiguous — leave it
	}
	return best
}

// scopeHasCatalogTable reports whether any of scope's sources is a stored
// table — something a schema lookup can answer for. False with a non-empty
// Tables means every source is a CTE, a subquery or a table function.
func scopeHasCatalogTable(scope *nanopass.SelectScope) bool {
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		if !ts.IsCTE && !ts.IsSubquery && !ts.IsFunction {
			return true
		}
	}
	return false
}

// sourceLabels names non-catalog sources for a diagnostic — `c (CTE)`,
// `(subquery)`, `v (subquery)`, `numbers (table function)`. Stored tables are
// skipped: they are not why the handle failed.
func sourceLabels(sources []nanopass.TableSource) (labels []string) {
	for i := range sources {
		ts := &sources[i]
		var kind string
		switch {
		case ts.IsCTE:
			kind = "CTE"
		case ts.IsSubquery:
			kind = "subquery"
		case ts.IsFunction:
			kind = "table function"
		default:
			continue
		}
		name := ts.Alias
		if name == "" {
			name = ts.Table
		}
		if name == "" {
			labels = append(labels, "("+kind+")")
			continue
		}
		labels = append(labels, name+" ("+kind+")")
	}
	return
}

// reportNoCatalogSource warns that a handle's source carries no catalog schema,
// so the handle cannot be resolved at all.
//
// It fires only for an identifier the resolver spells as a handle — without
// that gate every ordinary column of a subquery-sourced SELECT would be
// flagged — and only where a sink wants diagnostics, so the execution path
// (nil sink) does no extra work.
func reportNoCatalogSource(sink func(ColumnDiagnostic), resolver ColumnResolverI, handle string, labels []string) {
	if sink == nil || len(labels) == 0 {
		return
	}
	syn, ok := resolver.(HandleSyntaxI)
	if !ok || !syn.IsHandleSyntax(handle) {
		return
	}
	sink(ColumnDiagnostic{
		Handle: handle,
		Message: fmt.Sprintf(
			"a column handle resolves against a stored table's schema, and this SELECT reads %s — spell the physical name here, or move the handle into the subselect that reads the table",
			strings.Join(labels, ", ")),
	})
}
