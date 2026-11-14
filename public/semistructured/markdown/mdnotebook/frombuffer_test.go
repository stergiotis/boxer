package mdnotebook_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
)

// bufferCases are buffers a notebook can hold. The integration lane runs
// each beside the buffer of its notebook (frombuffer_integration_test.go).
var bufferCases = []string{
	"SELECT 1 AS one",
	"WITH a AS (SELECT number AS n FROM numbers(3)) SELECT sum(n) AS s FROM a",
	"WITH a AS (SELECT number AS n FROM numbers(5)),\n     by_two AS (SELECT n * 2 AS d FROM a)\nSELECT d FROM by_two ORDER BY d DESC LIMIT 2",
	"WITH a AS (WITH inner AS (SELECT 1 AS x) SELECT x FROM inner), b AS (SELECT x + 1 AS y FROM a) SELECT * FROM b",
	"WITH a AS (SELECT 1 AS x UNION ALL SELECT 2 AS x) SELECT count() AS n FROM a;",
	"WITH `t` AS (SELECT 'a`b' AS s) SELECT s FROM t",
}

func records(t *testing.T, nb *mdnotebook.Notebook) (names []string, statements []string) {
	t.Helper()
	for i := range nb.Cells {
		if c := &nb.Cells[i]; c.Kind == mdnotebook.CellKindRecord {
			names = append(names, c.Name)
			statements = append(statements, strings.TrimSpace(c.Body))
		}
	}
	return
}

func TestFromBuffer(t *testing.T) {
	text, err := mdnotebook.FromBuffer(bufferCases[2], "")
	require.NoError(t, err)
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	names, statements := records(t, nb)
	assert.Equal(t, []string{"a", "by_two", "main"}, names)
	assert.Equal(t, []string{
		"SELECT number AS n FROM numbers(5)",
		"SELECT n * 2 AS d FROM a",
		"SELECT d FROM by_two ORDER BY d DESC LIMIT 2",
	}, statements)
	last := nb.Cells[len(nb.Cells)-1]
	assert.Equal(t, mdnotebook.CellKindPresentation, last.Kind)
	assert.Equal(t, mdnotebook.RendererTable, last.Renderer)
	assert.Equal(t, "SELECT * FROM main", strings.TrimSpace(last.Body))
	// Not sealed: each record waits for collection, and nothing else is
	// wrong with the text.
	require.Len(t, nb.Problems, 3)
	for _, p := range nb.Problems {
		assert.Contains(t, p.Text, "has no stored result")
	}

	text, err = mdnotebook.FromBuffer("WITH main AS (SELECT 1 AS x) SELECT * FROM main", "shown")
	require.NoError(t, err)
	nb, err = mdnotebook.Read(text)
	require.NoError(t, err)
	names, _ = records(t, nb)
	assert.Equal(t, []string{"main", "shown"}, names)
}

// Buffer and FromBuffer read each other's output: the buffer of a buffer's
// notebook is a buffer of the same notebook, and the notebook of a
// notebook's buffer has its records.
func TestFromBufferAndBufferAreInverse(t *testing.T) {
	for _, sql := range bufferCases {
		text, err := mdnotebook.FromBuffer(sql, "")
		require.NoError(t, err, sql)
		nb, err := mdnotebook.Read(text)
		require.NoError(t, err, sql)
		buffer, err := mdnotebook.Buffer(nb)
		require.NoError(t, err, sql)
		again, err := mdnotebook.FromBuffer(buffer, "")
		require.NoError(t, err, buffer)
		assert.Equal(t, string(text), string(again), sql)
	}

	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(src)
	require.NoError(t, err)
	wantNames, wantStatements := records(t, nb)
	buffer, err := mdnotebook.Buffer(nb)
	require.NoError(t, err)
	text, err := mdnotebook.FromBuffer(buffer, wantNames[len(wantNames)-1])
	require.NoError(t, err)
	back, err := mdnotebook.Read(text)
	require.NoError(t, err)
	names, statements := records(t, back)
	assert.Equal(t, wantNames, names)
	assert.Equal(t, wantStatements, statements)
}

func TestFromBufferSaysWhatANotebookCannotHold(t *testing.T) {
	for sql, want := range map[string]string{
		"":                                  "empty",
		"SELECT FROM WHERE":                 "does not parse",
		"SET param_x = 1; SELECT {x:Int64}": "starts with SET",
		"SELECT {x:Int64} AS x":             "reads the parameter",
		"WITH a AS (SELECT {lo:Int64} AS x) SELECT * FROM a":                              "reads the parameter",
		"WITH 5 AS k SELECT k":                                                            "holds an expression",
		"WITH RECURSIVE r AS (SELECT 1 AS x) SELECT * FROM r":                             "RECURSIVE",
		"SELECT 1 AS x FORMAT JSON":                                                       "FORMAT",
		"WITH `my query` AS (SELECT 1 AS x) SELECT * FROM `my query`":                     "a name a record cannot take",
		"WITH main AS (SELECT 1 AS x) SELECT * FROM main":                                 "give the SELECT another",
		"WITH a AS (WITH RECURSIVE r AS (SELECT 1 AS x) SELECT * FROM r) SELECT * FROM a": "RECURSIVE",
	} {
		_, err := mdnotebook.FromBuffer(sql, "")
		require.Error(t, err, sql)
		assert.Contains(t, err.Error(), want, sql)
	}
	_, err := mdnotebook.FromBuffer("SELECT 1 AS x", "not a name")
	assert.Error(t, err)
}

// A statement that holds a fence of its own is written inside a longer one.
func TestFromBufferFencesAStatementThatHoldsAFence(t *testing.T) {
	sql := "SELECT '\n```\n' AS s"
	text, err := mdnotebook.FromBuffer(sql, "")
	require.NoError(t, err)
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	_, statements := records(t, nb)
	assert.Equal(t, []string{sql}, statements)
}
