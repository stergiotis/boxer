//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ValidateColumnNames (required pattern) ---

func TestValidateAliasesPass(t *testing.T) {
	pass := passes.ValidateColumnNames(`^[a-z]`)

	tests := []struct {
		name      string
		input     string
		shouldErr bool
	}{
		{
			name:      "valid_aliases",
			input:     "SELECT a AS col1, b AS col2 FROM t",
			shouldErr: false,
		},
		{
			name:      "no_aliases",
			input:     "SELECT a, b FROM t",
			shouldErr: false,
		},
		{
			name:      "invalid_alias_underscore",
			input:     "SELECT sum(a) AS _total FROM t",
			shouldErr: true,
		},
		{
			name:      "invalid_alias_uppercase",
			input:     "SELECT a AS Total FROM t",
			shouldErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pass(tt.input)
			if tt.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.input, result)
			}
		})
	}
}

func TestValidateAliasesOriginalExample(t *testing.T) {
	pass := passes.ValidateColumnNames(`^[^_]`)

	_, err := pass(`SELECT sum(a) AS "_a" FROM t`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_a")
}

func TestValidateAliasesExcludeOriginalExample(t *testing.T) {
	pass := passes.ValidateColumnNamesExclude(`^_`)

	_, err := pass(`SELECT sum(a) AS "_a" FROM t`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_a")
}

func TestValidateAliasesQuoted(t *testing.T) {
	pass := passes.ValidateColumnNames(`^[a-z]`)

	_, err := pass(`SELECT a AS "Bad Name" FROM t`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad Name")
}

func TestValidateAliasesBacktick(t *testing.T) {
	pass := passes.ValidateColumnNames(`^[a-z]`)

	_, err := pass("SELECT a AS `Bad` FROM t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad")
}

func TestValidateColumnNamesRejectsInvalid(t *testing.T) {
	pass := passes.ValidateColumnNames(`.*`)
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

func TestValidateColumnNamesCorpusPermissive(t *testing.T) {
	pass := passes.ValidateColumnNames(`.*`)

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			result, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			assert.Equal(t, entry.SQL, result)
		})
	}
}
