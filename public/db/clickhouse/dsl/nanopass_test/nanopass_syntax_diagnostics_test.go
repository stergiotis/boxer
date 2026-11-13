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
