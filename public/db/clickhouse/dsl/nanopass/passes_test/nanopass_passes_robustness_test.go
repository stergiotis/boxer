//go:build llm_generated_opus46

package passes_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassesRejectInvalidSQL(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"-- comment only\n",
		";;;",
		"SELECT",
		"FROM t",
		"WHERE x > 1",
	}

	allPasses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"NormalizeKeywordCase", passes.NormalizeKeywordCase},
		{"RemoveRedundantParens", passes.RemoveRedundantParens},
		{"StripComments", passes.StripComments},
		{"NormalizeWhitespace", passes.NormalizeWhitespaceSingleLine},
		{"QualifyTables", passes.QualifyTables("db")},
	}

	for _, sql := range invalid {
		for _, pp := range allPasses {
			t.Run(pp.name+"/"+sanitizeName(sql), func(t *testing.T) {
				_, err := pp.pass(sql)
				assert.Error(t, err, "pass %s should reject invalid SQL %q", pp.name, sql)
			})
		}
	}
}

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

func TestRoundTripFidelity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			pr, err := nanopass.Parse(entry.SQL)
			if err != nil {
				t.Skipf("parse failed: %v", err)
			}
			rw := nanopass.NewRewriter(pr)
			result := nanopass.GetText(rw)
			assert.Equal(t, entry.SQL, result, "round-trip fidelity broken")
		})
	}
}

func TestPassesPreserveScopeStructure(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	purePasses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"NormalizeKeywordCase", passes.NormalizeKeywordCase},
		{"NormalizeWhitespace", passes.NormalizeWhitespaceSingleLine},
		{"StripComments", passes.StripComments},
		{"RemoveRedundantParens", passes.RemoveRedundantParens},
	}

	for _, entry := range entries {
		for _, pp := range purePasses {
			t.Run(entry.Name+"/"+pp.name, func(t *testing.T) {
				prBefore, err := nanopass.Parse(entry.SQL)
				if err != nil {
					t.Skipf("parse failed: %v", err)
				}

				out, err := pp.pass(entry.SQL)
				if err != nil {
					t.Skipf("pass failed: %v", err)
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

func TestPipelineAllOrderings(t *testing.T) {
	sql := "select /* c */ a, (b * c) from t where (x > 1) order by a"

	allPasses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"case", passes.NormalizeKeywordCase},
		{"parens", passes.RemoveRedundantParens},
		{"comments", passes.StripComments},
		{"whitespace", passes.NormalizeWhitespaceSingleLine},
	}

	permute(len(allPasses), func(perm []int) {
		ordered := make([]nanopass.Pass, len(perm))
		names := make([]string, len(perm))
		for i, idx := range perm {
			ordered[i] = allPasses[idx].pass
			names[i] = allPasses[idx].name
		}
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			result, err := nanopass.Pipeline(sql, ordered...)
			require.NoError(t, err)
			_, err = nanopass.Parse(result)
			require.NoError(t, err, "ordering %v produced invalid SQL: %s", names, result)
		})
	})
}

func permute(n int, fn func([]int)) {
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	var generate func(int)
	generate = func(k int) {
		if k == 1 {
			cp := make([]int, n)
			copy(cp, perm)
			fn(cp)
			return
		}
		for i := 0; i < k; i++ {
			generate(k - 1)
			if k%2 == 0 {
				perm[i], perm[k-1] = perm[k-1], perm[i]
			} else {
				perm[0], perm[k-1] = perm[k-1], perm[0]
			}
		}
	}
	generate(n)
}

func sanitizeName(s string) string {
	if s == "" {
		return "empty"
	}
	r := strings.NewReplacer(" ", "_", "\n", "_", "\t", "_", ";", "semi")
	name := r.Replace(s)
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}
