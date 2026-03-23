//go:build llm_generated_opus46

package nanopass_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/require"
)

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

	// Test all permutations of 4 passes = 24 orderings
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
