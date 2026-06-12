//go:build llm_generated_opus47

package passes

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// QualifyTables returns a Pass that qualifies all unqualified table
// references with the given default database. Scope-aware: covers UNION
// branches (including parenthesised nested unions), CTE bodies, FROM
// subqueries, and expression subqueries; skips CTE references and table
// functions.
//
// defaultDB is spliced verbatim — pass a quoted identifier if the database
// name requires quoting.
func QualifyTables(defaultDB string) nanopass.Pass {
	return nanopass.LiftBodyPass(
		"QualifyTables",
		func(sql string) (result string, err error) {
			pr, err := nanopass.Parse(sql)
			if err != nil {
				err = eh.Errorf("QualifyTables: %w", err)
				return
			}
			rw := nanopass.NewRewriter(pr)

			scopes, err := nanopass.BuildScopes(pr, "")
			if err != nil {
				err = eh.Errorf("QualifyTables: %w", err)
				return
			}
			for _, scope := range nanopass.FlattenScopes(scopes) {
				qualifyTablesInScope(rw, scope, defaultDB)
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

func qualifyTablesInScope(rw nanopass.RewriterI, scope *nanopass.SelectScope, defaultDB string) {
	for _, ts := range scope.Tables {
		if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
			continue
		}
		if ts.Database != "" {
			continue
		}
		tid, ok := ts.Node.(*grammar1.TableIdentifierContext)
		if !ok {
			continue
		}
		// Splice the original identifier token text (which preserves the
		// source's quoting); ts.Table is the decoded name and would lose it.
		nanopass.ReplaceNode(rw, tid, defaultDB+"."+tid.Identifier().GetText())
	}
}
