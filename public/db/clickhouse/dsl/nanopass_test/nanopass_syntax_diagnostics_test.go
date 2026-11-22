package nanopass_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// TestSyntaxErrorDiagnostics checks that a rejected statement's line:column
// diagnostics are readable from the error, and absent for other errors.
func TestSyntaxErrorDiagnostics(t *testing.T) {
	_, err := nanopass.Parse("SELECT FROM WHERE")
	require.Error(t, err)
	d := nanopass.SyntaxErrorDiagnostics(err)
	require.NotEmpty(t, d)
	assert.True(t, strings.HasPrefix(d[0], "1:"), d[0])

	_, err = nanopass.ParseCanonical("SELECT CASE WHEN a THEN 1 END FROM t")
	require.Error(t, err)
	assert.NotEmpty(t, nanopass.SyntaxErrorDiagnostics(err))

	assert.Nil(t, nanopass.SyntaxErrorDiagnostics(nil))
	assert.Nil(t, nanopass.SyntaxErrorDiagnostics(assert.AnError))
}

// TestSyntaxErrorHintsAScalarWithItem: a scalar written the way a subquery
// is named, `WITH name AS <expression>` or `WITH name AS (<expression>)`, is
// refused by the server as by the grammar; the diagnostics end with how a
// scalar is written. Statements that fail for another reason get no hint.
func TestSyntaxErrorHintsAScalarWithItem(t *testing.T) {
	hinted := []string{
		"WITH x AS [1, 2] SELECT x",
		"WITH x AS ([1, 2]) SELECT x",
		"WITH x AS 5 SELECT x",
		"WITH x AS (1 + 2) SELECT x",
		"WITH a AS (SELECT 1), b AS ([1, 2]) SELECT 1",
		"WITH lo AS quantile(0.05)(L), hi AS quantile(0.95)(L) SELECT count() FROM t WHERE L BETWEEN lo AND hi",
	}
	for _, sql := range hinted {
		_, err := nanopass.Parse(sql)
		require.Error(t, err, sql)
		d := nanopass.SyntaxErrorDiagnostics(err)
		require.NotEmpty(t, d, sql)
		assert.True(t, strings.HasPrefix(d[0], "1:"), "the parser's diagnostics come first: %v", d)
		last := d[len(d)-1]
		assert.True(t, strings.HasPrefix(last, "hint: "), sql)
		assert.Contains(t, last, "WITH <expression> AS name", sql)
	}
	for _, sql := range []string{
		"SELECT 1, x AS 5",
		"WITH x AS (SELECT 1) SELECT FROM WHERE",
		"WITH a AS b SELECT FROM WHERE",
		"SELECT x FROM t WHERE y IN (SELECT a AS 5)",
	} {
		_, err := nanopass.Parse(sql)
		require.Error(t, err, sql)
		for _, d := range nanopass.SyntaxErrorDiagnostics(err) {
			assert.False(t, strings.HasPrefix(d, "hint: "), "%s: %s", sql, d)
		}
	}
}
