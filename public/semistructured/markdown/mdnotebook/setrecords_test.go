package mdnotebook_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
)

func sample(t *testing.T, name string) *mdnotebook.Notebook {
	t.Helper()
	text, err := fs.ReadFile(samples.FS(), name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	return nb
}

// The text of a notebook's own buffer is the notebook, byte for byte.
func TestSetRecordsOfANotebooksBufferIsTheNotebook(t *testing.T) {
	for _, name := range samples.Names() {
		nb := sample(t, name)
		sql, err := mdnotebook.Buffer(nb)
		require.NoError(t, err)
		text, err := mdnotebook.SetRecords(nb, sql, "")
		require.NoError(t, err, name)
		assert.Equal(t, string(nb.Src), string(text), name)
	}
}

// A buffer worked on and written back: the statements are the buffer's,
// and every cell that is not a record keeps its text.
func TestSetRecordsWritesTheBufferIntoTheText(t *testing.T) {
	nb := sample(t, "minimal.md")
	sql, err := mdnotebook.Buffer(nb)
	require.NoError(t, err)
	// One statement edited, and a record added between the two.
	edited := strings.Replace(sql, "FROM numbers(24)", "FROM numbers(48)", 1)
	edited = strings.Replace(edited, "\nSELECT count() AS hours", ",\nslow AS (SELECT t, '```' AS fence FROM readings WHERE lag_ms > 400)\nSELECT count() AS hours", 1)
	require.NotEqual(t, sql, edited)

	text, err := mdnotebook.SetRecords(nb, edited, "")
	require.NoError(t, err)
	out, err := mdnotebook.Read(text)
	require.NoError(t, err)

	names, statements := records(t, out)
	assert.Equal(t, []string{"readings", "slow", "totals"}, names)
	assert.Contains(t, statements[0], "FROM numbers(48)")
	assert.Contains(t, statements[1], "'```' AS fence", "a fence long enough to hold the statement")
	back, err := mdnotebook.Buffer(out)
	require.NoError(t, err)
	want, err := mdnotebook.FromBuffer(edited, "totals")
	require.NoError(t, err)
	got, err := mdnotebook.FromBuffer(back, "totals")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "the buffer of the result is the buffer, statement for statement")

	// What is not a record keeps its text, and so does the frontmatter.
	var before, after []string
	for i := range nb.Cells {
		if nb.Cells[i].Kind != mdnotebook.CellKindRecord {
			before = append(before, nb.Text(i))
		}
	}
	for i := range out.Cells {
		if out.Cells[i].Kind != mdnotebook.CellKindRecord {
			after = append(after, out.Text(i))
		}
	}
	assert.Equal(t, before, after)
	assert.Equal(t, string(nb.Src[:nb.BodyStart]), string(out.Src[:out.BodyStart]))
	// The unchanged record keeps its cell as it stood.
	assert.Contains(t, string(text), "```sql\nSELECT count() AS hours, round(avg(lag_ms), 1) AS mean_ms FROM readings\n```\n\n^totals\n")

	// The edited record is stale, the one that reads it with it, and the
	// new one has nothing stored.
	ld := mdnotebook.Load(out, samples.FS())
	defer ld.Release()
	assert.True(t, ld.Stale["readings"])
	_, derived := ld.StaleSource("totals")
	assert.True(t, derived)
	assert.Nil(t, ld.Records["slow"])
}

func TestSetRecordsNamesTheBuffersOwnSelect(t *testing.T) {
	nb := sample(t, "minimal.md")
	// The buffer's SELECT became a query of the list, and a new SELECT
	// follows it: the new one needs a name.
	sql := "WITH readings AS (SELECT 1 AS lag_ms), totals AS (SELECT count() AS hours, 1.0 AS mean_ms FROM readings) SELECT hours * 2 AS doubled FROM totals"
	_, err := mdnotebook.SetRecords(nb, sql, "")
	require.ErrorContains(t, err, "give the SELECT another")
	text, err := mdnotebook.SetRecords(nb, sql, "doubled")
	require.NoError(t, err)
	out, err := mdnotebook.Read(text)
	require.NoError(t, err)
	names, _ := records(t, out)
	assert.Equal(t, []string{"readings", "totals", "doubled"}, names)

	// A record put before the first.
	text, err = mdnotebook.SetRecords(nb, "WITH seed AS (SELECT 7 AS s), readings AS (SELECT s AS lag_ms FROM seed) SELECT count() AS hours, 1.0 AS mean_ms FROM readings", "")
	require.NoError(t, err)
	out, err = mdnotebook.Read(text)
	require.NoError(t, err)
	names, _ = records(t, out)
	assert.Equal(t, []string{"seed", "readings", "totals"}, names)
}

func TestSetRecordsRefusesAnEditOfTheTextAroundTheStatements(t *testing.T) {
	nb := sample(t, "report.md")
	_, err := mdnotebook.SetRecords(nb, "SELECT 1 AS source, 2 AS target, 3 AS calls", "")
	require.ErrorContains(t, err, "does not hold the record p99_by_hour")

	// stats and by_region the other way round.
	swapped := "WITH p99_by_hour AS (SELECT 1 AS p99, 'a' AS region, now64() AS t), by_region AS (SELECT region FROM p99_by_hour), stats AS (SELECT 1 AS worst_ms), slowest AS (SELECT region FROM by_region) SELECT 1 AS calls"
	_, err = mdnotebook.SetRecords(nb, swapped, "")
	require.ErrorContains(t, err, "records are reordered in the text")

	_, err = mdnotebook.SetRecords(nb, "SET param_x = 1; SELECT 1", "")
	require.ErrorContains(t, err, "not a notebook's")
}
