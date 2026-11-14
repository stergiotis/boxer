package play

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
)

const notebookTestBuffer = "WITH recent AS (SELECT number AS n FROM numbers(10)),\n" +
	"     by_kind AS (SELECT n % 2 AS kind, count() AS c FROM recent GROUP BY kind)\n" +
	"SELECT * FROM by_kind ORDER BY kind"

// get_notebook reads the buffer as a notebook's records, and a notebook
// loaded as the buffer reads back as the same records (ADR-0371 §SD6).
func TestGetNotebookAndLoadAreInverse(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.sql = notebookTestBuffer

	out := queryOp[BufferNotebook](t, h, opGetNotebook, nil)
	assert.Equal(t, []string{"recent", "by_kind", "main"}, out.Records)
	assert.Contains(t, out.Markdown, "^by-kind\n")
	assert.Contains(t, out.Markdown, "```sql table\nSELECT * FROM main\n```")

	loaded, isNotebook, err := bufferOfNotebook([]byte(out.Markdown))
	require.NoError(t, err)
	require.True(t, isNotebook)
	l.inner.sql = loaded
	again := queryOp[BufferNotebook](t, h, opGetNotebook, nil)
	assert.Equal(t, out, again, "the notebook of a notebook's buffer is that notebook")

	named := queryOp[BufferNotebook](t, h, opGetNotebook, NotebookArgs{Sink: "shown"})
	assert.Equal(t, []string{"recent", "by_kind", "shown"}, named.Records)
}

// A file without a record cell is SQL and is loaded as it is; the fixture
// notebook is loaded as the buffer of its three records.
func TestLoadReadsANotebookAsItsBuffer(t *testing.T) {
	_, isNotebook, err := bufferOfNotebook([]byte("-- a query\nSELECT 1 AS one\n"))
	require.NoError(t, err)
	assert.False(t, isNotebook)
	_, isNotebook, _ = bufferOfNotebook([]byte("# Notes\n\n```sql\nSELECT 1\n```\n"))
	assert.False(t, isNotebook, "a sql fence without an id is prose")

	src, err := fixture.FS().Open(fixture.Name)
	require.NoError(t, err)
	defer src.Close()
	var body strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rErr := src.Read(buf)
		body.Write(buf[:n])
		if rErr != nil {
			break
		}
	}
	sql, isNotebook, err := bufferOfNotebook([]byte(body.String()))
	require.NoError(t, err)
	require.True(t, isNotebook)
	nb, err := mdnotebook.Read([]byte(body.String()))
	require.NoError(t, err)
	want, err := mdnotebook.Buffer(nb)
	require.NoError(t, err)
	assert.Equal(t, want, sql)
	split, err := splitGraph(sql)
	require.NoError(t, err, "the loaded buffer splits into play's nodes")
	assert.Len(t, split.Nodes, 3)
}

// What no notebook holds is refused with the part that it cannot hold.
func TestGetNotebookRefusesWhatANotebookCannotHold(t *testing.T) {
	l, h := opsLauncher(t)
	for sql, want := range map[string]string{
		"SET param_lo = 1; SELECT {lo:Int64} AS x": "SET",
		"SELECT {lo:Int64} AS x":                   "parameter",
		"WITH 5 AS k SELECT k":                     "expression",
		"WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 64) FROM b) SELECT 1": "computed in play",
		"WITH main AS (SELECT 1 AS x) SELECT * FROM main":                                      "another",
	} {
		l.inner.sql = sql
		var refusal *app.OperationRefusal
		_, err := h.Snapshot().Query(opGetNotebook, nil)
		require.ErrorAs(t, err, &refusal, sql)
		assert.Contains(t, refusal.Reason, want, sql)
	}
}

// set_notebook is Load for an agent: the notebook's records become the
// buffer, and get_notebook reads them back.
func TestSetNotebookReplacesTheBufferWithTheRecords(t *testing.T) {
	l, h := opsLauncher(t)
	text, err := fs.ReadFile(samples.FS(), "minimal.md")
	require.NoError(t, err)
	args, err := buscodec.Encode(SetNotebookArgs{Markdown: string(text)})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opSetNotebook, args)
	require.NoError(t, err)

	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	want, err := mdnotebook.Buffer(nb)
	require.NoError(t, err)
	assert.Equal(t, want, l.inner.sql)
	back := queryOp[BufferNotebook](t, h, opGetNotebook, NotebookArgs{Sink: "totals"})
	assert.Equal(t, []string{"readings", "totals"}, back.Records)

	// The buffer written back into the text it came from is that text.
	again, err := mdnotebook.SetRecords(nb, l.inner.sql, "")
	require.NoError(t, err)
	assert.Equal(t, string(text), string(again))

	held := l.inner.sql
	for markdown, want := range map[string]string{
		"SELECT 1 AS one":                                          "holds no record cell",
		"```sql\nDROP TABLE t\n```\n^gone\n":                       "the notebook's records",
		"```sql\nSELECT 1\n```\n^a\n\n```sql\nSELECT 2\n```\n^a\n": "the notebook's records",
	} {
		args, err = buscodec.Encode(SetNotebookArgs{Markdown: markdown})
		require.NoError(t, err)
		_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opSetNotebook, args)
		require.Error(t, err, markdown)
		assert.Contains(t, err.Error(), want, markdown)
		assert.Equal(t, held, l.inner.sql, "a refused text leaves the buffer as it was")
	}
}
