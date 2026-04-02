//go:build llm_generated_opus46

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// QualifyTables returns a Pass that qualifies all unqualified table references
// with the given default database. It is scope-aware: it handles UNION ALL branches,
// skips CTE references, and recurses into CTE bodies and subqueries.
func QualifyTables(defaultDB string) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("QualifyTables: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		scopes := nanopass.BuildScopes(pr)
		for _, scope := range scopes {
			qualifyTablesInScope(rw, scope, defaultDB)
		}

		result = nanopass.GetText(rw)
		return
	}
}

func qualifyTablesInScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, defaultDB string) {
	for _, ts := range scope.Tables {
		if ts.IsCTE {
			continue
		}
		if ts.IsSubquery {
			// Recurse into the subquery's scope
			if ts.Scope != nil {
				qualifyTablesInScope(rw, ts.Scope, defaultDB)
			}
			continue
		}
		if ts.Database != "" {
			continue
		}
		tid, ok := ts.Node.(*grammar1.TableIdentifierContext)
		if !ok {
			continue
		}
		nanopass.ReplaceNode(rw, tid, defaultDB+"."+ts.Table)
	}

	// Recurse into CTE body scopes
	for _, cte := range scope.CTEDefs {
		if cte.Scope != nil {
			qualifyTablesInScope(rw, cte.Scope, defaultDB)
		}
	}

	// Recurse into expression subqueries (WHERE, SELECT list, HAVING, etc.)
	for _, sub := range scope.Subqueries {
		qualifyTablesInScope(rw, sub, defaultDB)
	}
}
