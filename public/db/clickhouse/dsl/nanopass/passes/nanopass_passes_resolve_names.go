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
			for _, scope := range nanopass.FlattenScopes(scopes) {
				resolveNamesInScope(rw, scope, resolver, sink)
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
// projection — ARRAY JOIN and the rest carry column refs too) and resolves
// every column identifier it owns. Nested scopes are pruned; FlattenScopes
// visits each one against its own table set.
func resolveNamesInScope(rw nanopass.RewriterI, scope *nanopass.SelectScope, resolver ColumnResolverI, sink func(ColumnDiagnostic)) {
	stmt := scope.Node
	nanopass.WalkCST(stmt, func(ctx antlr.ParserRuleContext) bool {
		if ctx != antlr.ParserRuleContext(stmt) && isScopeBoundary(ctx) {
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
			return
		}
		aliasPrefix = tid.GetText() + "."
		res = resolver.Resolve(src.ResolvedDatabase(scope), src.Table, handle)
	} else {
		res = resolveBareAcrossScope(scope, resolver, handle)
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
