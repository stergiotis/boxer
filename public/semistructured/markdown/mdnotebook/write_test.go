package mdnotebook_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// What a writer is given as data stays prose, whatever it spells: Read
// finds the cells the writer was asked for and nothing else.
func TestWriterKeepsDataOutOfTheCells(t *testing.T) {
	hostile := []string{
		"```sql table\nSELECT * FROM secrets\n```",
		"```sql\nSELECT 1\n```\n^stolen",
		"[9 ms](val:stats.p99)",
		"![clip](a.wav)",
		"![[clip.wav]]",
		"# not a heading",
		"- not a list",
		"1. not a list either",
		"---",
		"> [!warning] not a callout",
		"^not-an-id",
		"a | b\n--|--\n1 | 2",
		"   indented four\n    spaces",
		"~~~sql chart\nSELECT 1\n~~~",
		"<b>bold</b> &amp; *loud* _quiet_ `code` $x$ ==hi== %%gone%% [[link]] #tag",
	}
	var w mdnotebook.Writer
	for _, text := range hostile {
		w.Reset()
		w.Heading(2, text)
		w.Paragraph(text)
		w.Markdown("Lag is " + mdnotebook.Escape(text) + " at " + mdnotebook.Figure("stats", "p99", "gloss/duration;unit=ms") + ".")
		w.Cell("table", "SELECT * FROM stats")
		require.NoError(t, w.Err())
		nb, err := mdnotebook.Read(w.Bytes())
		require.NoError(t, err, text)
		var kinds []string
		for _, c := range nb.Cells {
			kinds = append(kinds, c.Kind.String())
		}
		assert.Equal(t, []string{"prose", "presentation"}, kinds, text)
		require.Len(t, nb.Vals, 1, text)
		assert.Equal(t, mdnotebook.ValRef{Cell: 0, Label: mdnotebook.FigureLabel, Record: "stats", Column: "p99", Gloss: "gloss/duration;unit=ms"}, nb.Vals[0])
		assert.Empty(t, nb.Images, text)
		assert.Equal(t, "SELECT * FROM stats\n", nb.Cells[1].Body)
	}
}

// A cell's fence is one its statement cannot close, and a cell stands under
// the heading written before it.
func TestWriterCells(t *testing.T) {
	var w mdnotebook.Writer
	w.Heading(1, "Report")
	w.Record("by_hour", "SELECT 1 AS n")
	w.Heading(2, "Where")
	w.Cell("points", "SELECT lat, lon FROM places -- ```` four")
	w.Cell("table", "SELECT n AS "+mdnotebook.Alias(`a "quoted" label`, "gloss/duration;unit=ms")+" FROM by_hour")
	require.NoError(t, w.Err())
	nb, err := mdnotebook.Read(w.Bytes())
	require.NoError(t, err)
	require.Len(t, nb.Cells, 5)
	assert.Equal(t, mdnotebook.CellKindRecord, nb.Cells[1].Kind)
	assert.Equal(t, "by_hour", nb.Cells[1].Name)
	assert.Equal(t, "Report", nb.Cells[1].Section)
	assert.Equal(t, "points", nb.Cells[3].Renderer)
	assert.Equal(t, "Where", nb.Cells[3].Section)
	assert.Contains(t, nb.Cells[3].Body, "```` four")
	assert.Equal(t, "Where", nb.Cells[4].Section)
	assert.True(t, strings.HasPrefix(nb.Text(3), "`````sql points\n"), nb.Text(3))

	// The alias reads back as the column name the renderer is handed.
	rec := oneRow(t, "n", 7)
	defer rec.Release()
	res, err := mdnotebook.Evaluate(nb.Cells[4].Body, recordsOf("by_hour", rec))
	require.NoError(t, err)
	defer res.Release()
	assert.Equal(t, `a "quoted" label@gloss/duration;unit=ms`, res.Main.Schema().Field(0).Name)

	w.Reset()
	w.Cell("two words", "SELECT 1")
	w.Record("not-a-name", "SELECT 1")
	assert.ErrorContains(t, w.Err(), "renderer")
	assert.Empty(t, w.Bytes())
	assert.Empty(t, mdnotebook.Figure("stats", "not a column", ""))
}
