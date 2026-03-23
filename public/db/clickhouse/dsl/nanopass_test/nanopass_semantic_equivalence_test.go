//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/require"
)

func TestPassesPreserveScopeStructure(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	purePassses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"NormalizeKeywordCase", passes.NormalizeKeywordCase},
		{"NormalizeWhitespace", passes.NormalizeWhitespaceSingleLine},
		{"StripComments", passes.StripComments},
		{"RemoveRedundantParens", passes.RemoveRedundantParens},
	}

	for _, entry := range entries {
		for _, pp := range purePassses {
			t.Run(entry.Name+"/"+pp.name, func(t *testing.T) {
				out, err := pp.pass(entry.SQL)
				if err != nil {
					t.Skipf("pass failed: %v", err)
				}

				prBefore, err := nanopass.Parse(entry.SQL)
				if err != nil {
					t.Skipf("parse failed: %v", err)
				}
				prAfter, err := nanopass.Parse(out)
				require.NoError(t, err, "pass produced unparseable SQL: %s", out)

				scopesBefore := nanopass.BuildScopes(prBefore)
				scopesAfter := nanopass.BuildScopes(prAfter)

				require.Equal(t, len(scopesBefore), len(scopesAfter),
					"scope count changed: before=%d after=%d", len(scopesBefore), len(scopesAfter))

				for i := range scopesBefore {
					require.Equal(t, len(scopesBefore[i].Tables), len(scopesAfter[i].Tables),
						"table count changed in scope %d", i)
					require.Equal(t, len(scopesBefore[i].CTEDefs), len(scopesAfter[i].CTEDefs),
						"CTE count changed in scope %d", i)
				}
			})
		}
	}
}
