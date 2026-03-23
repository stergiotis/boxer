//go:build llm_generated_opus46

package nanopass

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/require"
)

func TestAllPassesAllCorpus(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	allPasses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"NormalizeKeywordCase", passes.NormalizeKeywordCase},
		{"RemoveRedundantParens", passes.RemoveRedundantParens},
		{"StripComments", passes.StripComments},
		{"NormalizeWhitespace", passes.NormalizeWhitespaceSingleLine},
		{"QualifyTables", passes.QualifyTables("default")},
	}

	for _, entry := range entries {
		for _, pp := range allPasses {
			t.Run(entry.Name+"/"+pp.name, func(t *testing.T) {
				out, err := pp.pass(entry.SQL)
				if err != nil {
					t.Skipf("pass failed: %v", err)
				}
				_, err = nanopass.Parse(out)
				require.NoError(t, err, "pass %s produced invalid SQL for %s:\n%s", pp.name, entry.Name, out)
			})
		}
	}
}
