//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripComments(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "SELECT a /* column a */ FROM t",
			expected: "SELECT a   FROM t",
		},
		{
			input:    "SELECT a -- column a\nFROM t",
			expected: "SELECT a  FROM t",
		},
		{
			input:    "SELECT a // column a\nFROM t",
			expected: "SELECT a  FROM t",
		},
		{
			input:    "/* leading comment */ SELECT a FROM t",
			expected: "  SELECT a FROM t",
		},
		{
			input:    "SELECT /* c1 */ a /* c2 */ FROM /* c3 */ t",
			expected: "SELECT   a   FROM   t",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.StripComments(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestStripCommentsIdempotent(t *testing.T) {
	sql := "SELECT a /* comment */ FROM t -- trailing\n"
	pass1, err := passes.StripComments(sql)
	require.NoError(t, err)
	pass2, err := passes.StripComments(pass1)
	require.NoError(t, err)
	assert.Equal(t, pass1, pass2)
}

func TestStripCommentsOutputValidity(t *testing.T) {
	sqls := []string{
		"SELECT /* inline */ a FROM t",
		"SELECT a -- comment\nFROM t WHERE b > 1",
		"/* header */ SELECT a FROM t /* trailer */",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("validity_%d", i), func(t *testing.T) {
			out, err := passes.StripComments(sql)
			require.NoError(t, err)
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "produced invalid SQL: %s", out)
		})
	}
}
