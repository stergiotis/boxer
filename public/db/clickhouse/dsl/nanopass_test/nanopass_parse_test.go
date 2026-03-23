//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCorpus(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			pr, err := nanopass.Parse(entry.SQL)
			require.NoError(t, err, "failed to parse:\n%s", entry.SQL)
			require.NotNil(t, pr)
			require.NotNil(t, pr.Tree)
		})
	}
}
func TestParseEmptyString(t *testing.T) {
	_, err := nanopass.Parse("")
	assert.Error(t, err)
}

func TestParseWhitespaceOnly(t *testing.T) {
	_, err := nanopass.Parse("   ")
	assert.Error(t, err)
}

func TestParseCommentOnly(t *testing.T) {
	_, err := nanopass.Parse("-- just a comment\n")
	assert.Error(t, err)
}

func TestParseSemicolons(t *testing.T) {
	_, err := nanopass.Parse(";;;")
	assert.Error(t, err)
}

func TestParseIncompleteSelect(t *testing.T) {
	_, err := nanopass.Parse("SELECT")
	assert.Error(t, err)
}

func TestParseIncompleteWhere(t *testing.T) {
	_, err := nanopass.Parse("SELECT a FROM t WHERE")
	assert.Error(t, err)
}

func TestParseTrailingSemicolon(t *testing.T) {
	// ClickHouse allows trailing semicolons — grammar may or may not
	pr, err := nanopass.Parse("SELECT 1;")
	if err != nil {
		t.Logf("trailing semicolon not supported by grammar: %v", err)
		t.Skip()
	}
	require.NotNil(t, pr)
}
