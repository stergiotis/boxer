package mdnotebook_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
)

func readFixture(t *testing.T) (nb *mdnotebook.Notebook, ld *mdnotebook.Loaded) {
	t.Helper()
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err = mdnotebook.Read(src)
	require.NoError(t, err)
	ld = mdnotebook.Load(nb, fixture.FS())
	t.Cleanup(ld.Release)
	return
}

func TestReadFindsTheFixturesCells(t *testing.T) {
	nb, _ := readFixture(t)
	assert.Equal(t, "Ingest lag, week 45", nb.Title)
	assert.Empty(t, nb.Problems)

	var kinds []string
	for _, c := range nb.Cells {
		kinds = append(kinds, c.Kind.String())
	}
	assert.Equal(t, []string{
		"prose",
		"record", "record", "record",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "presentation",
		"prose", "media",
		"prose",
	}, kinds)

	var names []string
	for _, c := range nb.Cells {
		if c.Kind == mdnotebook.CellKindRecord {
			names = append(names, c.Name)
		}
	}
	assert.Equal(t, []string{"stats", "p99_by_hour", "calls"}, names, "a record's name is its id with - written _")

	last := nb.Cells[len(nb.Cells)-1]
	assert.Contains(t, nb.Text(len(nb.Cells)-1), "```sql table", "a fence in a list item stays in its prose cell")
	assert.Equal(t, mdnotebook.CellKindProse, last.Kind)

	require.Len(t, nb.Vals, 3)
	assert.Equal(t, mdnotebook.ValRef{Cell: 0, Label: "412.3 ms", Record: "stats", Column: "p99_ms", Gloss: "gloss/duration;unit=ms"}, nb.Vals[0])
	assert.Empty(t, nb.Vals[2].Gloss, "a link without a title shows the value plainly")
}

// The cells tile the text: every byte outside a cell is white space, so
// writing a notebook back is its cells joined by what lay between them.
func TestCellsTileTheText(t *testing.T) {
	nb, _ := readFixture(t)
	at := nb.BodyStart
	for i, c := range nb.Cells {
		require.GreaterOrEqual(t, c.Start, at, "cell %d overlaps the one before", i)
		assert.Empty(t, strings.TrimSpace(string(nb.Src[at:c.Start])), "text before cell %d is in no cell", i)
		at = c.Stop
	}
	assert.Empty(t, strings.TrimSpace(string(nb.Src[at:])))
}

func TestLoadChecksBlobsPinAndStaleness(t *testing.T) {
	nb, ld := readFixture(t)
	assert.Empty(t, ld.Problems)
	assert.Empty(t, ld.Stale)
	require.Len(t, ld.Records, 3)
	assert.EqualValues(t, 48, ld.Records["p99_by_hour"].NumRows())

	// An edit to a record's statement makes that record stale and no other.
	edited := strings.Replace(string(nb.Src), "region, quantile(0.99)(lag_ms) AS p99", "region, quantile(0.95)(lag_ms) AS p99", 1)
	require.NotEqual(t, string(nb.Src), edited)
	nb2, err := mdnotebook.Read([]byte(edited))
	require.NoError(t, err)
	ld2 := mdnotebook.Load(nb2, fixture.FS())
	defer ld2.Release()
	assert.Equal(t, map[string]bool{"p99_by_hour": true}, ld2.Stale)
	assert.Equal(t, mdnotebook.PinOf(nb.Records), mdnotebook.PinOf(nb2.Records), "an edited statement does not change what is stored")

	// A record listed under another record's blob is a different pin, and a
	// blob whose content is not its name is refused.
	other := append([]mdnotebook.RecordRef(nil), nb.Records...)
	other[0].Blob, other[1].Blob = other[1].Blob, other[0].Blob
	assert.NotEqual(t, mdnotebook.PinOf(nb.Records), mdnotebook.PinOf(other))
	_, err = mdnotebook.ReadBlob(fixture.FS(), fixture.Name)
	assert.Error(t, err)
}

func TestEvaluate(t *testing.T) {
	_, ld := readFixture(t)
	names := func(rec arrow.RecordBatch) (out []string) {
		for _, f := range rec.Schema().Fields() {
			out = append(out, f.Name)
		}
		return
	}
	cases := []struct {
		name      string
		statement string
		cols      []string
		rows      int64
		members   []string
		refusal   error
		found     string
	}{
		{name: "verbatim", statement: "SELECT * FROM p99_by_hour", cols: []string{"t", "region", "p99"}, rows: 48},
		{name: "limit", statement: "SELECT * FROM p99_by_hour LIMIT 8", cols: []string{"t", "region", "p99"}, rows: 8},
		{name: "limit offset", statement: "SELECT * FROM p99_by_hour LIMIT 8 OFFSET 44", cols: []string{"t", "region", "p99"}, rows: 4},
		{name: "column subset", statement: "SELECT t, p99 FROM p99_by_hour", cols: []string{"t", "p99"}, rows: 48},
		{name: "rename", statement: `SELECT t AS hour, p99 AS "p99@gloss/duration;unit=ms" FROM p99_by_hour`, cols: []string{"hour", "p99@gloss/duration;unit=ms"}, rows: 48},
		{name: "one column twice", statement: "SELECT p99 AS y, p99 AS shown FROM p99_by_hour", cols: []string{"y", "shown"}, rows: 48},
		{name: "a column under its own name", statement: "SELECT p99 AS p99 FROM p99_by_hour", cols: []string{"p99"}, rows: 48},
		{name: "literals", statement: "SELECT 'radial' AS layout, 2 AS ring_dist", cols: []string{"layout", "ring_dist"}, rows: 1},
		{name: "members", statement: "WITH edges AS (SELECT source, target FROM calls), graph_opts AS (SELECT 'radial' AS layout) SELECT * FROM edges",
			cols: []string{"source", "target"}, rows: 6, members: []string{"edges", "graph_opts"}},
		{name: "member reads member", statement: "WITH a AS (SELECT source, target FROM calls), b AS (SELECT target AS t FROM a) SELECT * FROM b", cols: []string{"t"}, rows: 6, members: []string{"a", "b"}},

		{name: "where", statement: "SELECT * FROM p99_by_hour WHERE region = 'emea'", refusal: mdnotebook.ErrNotPresentation, found: "WHERE: a record filters, a cell selects"},
		{name: "order by", statement: "SELECT * FROM p99_by_hour ORDER BY p99", refusal: mdnotebook.ErrNotPresentation, found: "ORDER BY: a record orders, a cell selects"},
		{name: "aggregate", statement: "SELECT region, max(p99) FROM p99_by_hour GROUP BY region", refusal: mdnotebook.ErrNotPresentation, found: "GROUP BY"},
		{name: "expression", statement: "SELECT p99 * 2 AS double FROM p99_by_hour", refusal: mdnotebook.ErrNotPresentation, found: "an expression"},
		{name: "cast", statement: "SELECT toString(p99) AS p FROM p99_by_hour", refusal: mdnotebook.ErrNotPresentation, found: "an expression"},
		{name: "join", statement: "SELECT * FROM p99_by_hour JOIN calls ON 1 = 1", refusal: mdnotebook.ErrNotPresentation, found: "JOIN"},
		{name: "server table", statement: "SELECT * FROM system.tables", refusal: mdnotebook.ErrNotPresentation, found: "a table of a database (system.tables)"},
		{name: "table function", statement: "SELECT * FROM url('http://x/y', CSV)", refusal: mdnotebook.ErrNotPresentation, found: "a table function"},
		{name: "same name twice", statement: "SELECT t, t FROM p99_by_hour", refusal: mdnotebook.ErrNotPresentation, found: "two columns named t"},
		{name: "a column another alias names", statement: "SELECT t, p99 AS t FROM p99_by_hour", refusal: mdnotebook.ErrNotPresentation, found: "which is a column and an alias"},
		{name: "a swap", statement: "SELECT source AS target, target AS source FROM calls", refusal: mdnotebook.ErrNotPresentation, found: "which is a column and an alias"},
		{name: "computed without source", statement: "SELECT now() AS t", refusal: mdnotebook.ErrNotPresentation, found: "an expression in the SELECT list (now())"},
		{name: "not a select", statement: "DROP TABLE p99_by_hour", refusal: mdnotebook.ErrNotPresentation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := mdnotebook.Evaluate(tc.statement, ld.Records)
			if tc.refusal != nil {
				require.ErrorIs(t, err, tc.refusal)
				assert.Contains(t, err.Error(), tc.found)
				assert.Nil(t, res)
				return
			}
			require.NoError(t, err)
			defer res.Release()
			assert.Equal(t, tc.cols, names(res.Main))
			assert.Equal(t, tc.rows, res.Main.NumRows())
			var members []string
			for _, m := range res.Members {
				members = append(members, m.Name)
			}
			assert.Equal(t, tc.members, members)
		})
	}

	// A statement that is wrong where it stands is an error, not a refusal:
	// ClickHouse would reject it too.
	for name, statement := range map[string]string{
		"unknown relation":             "SELECT * FROM nowhere",
		"unknown column":               "SELECT nope FROM p99_by_hour",
		"member takes a record's name": "WITH calls AS (SELECT * FROM p99_by_hour) SELECT * FROM calls",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mdnotebook.Evaluate(statement, ld.Records)
			require.Error(t, err)
			assert.NotErrorIs(t, err, mdnotebook.ErrNotPresentation)
		})
	}
	t.Run("a rename shares the record's column", func(t *testing.T) {
		res, err := mdnotebook.Evaluate("SELECT p99 AS y FROM p99_by_hour", ld.Records)
		require.NoError(t, err)
		defer res.Release()
		assert.Same(t, ld.Records["p99_by_hour"].Column(2).Data(), res.Main.Column(0).Data(), "nothing is copied")
	})
}

func TestValueOfMatchesTheLabelsInTheText(t *testing.T) {
	nb, ld := readFixture(t)
	for _, ref := range nb.Vals {
		v, err := mdnotebook.ValueOf(ld.Records, ref.Record, ref.Column, ref.Gloss)
		require.NoError(t, err)
		assert.Equal(t, ref.Label, v.Face.Text, "the label is the value's rendering when it was stored")
	}
	_, err := mdnotebook.ValueOf(ld.Records, "p99_by_hour", "p99", "")
	assert.Error(t, err, "a value is read from a record of one row")
	_, err = mdnotebook.ValueOf(ld.Records, "stats", "p99_ms", "gloss/nosuch")
	assert.Error(t, err, "a gloss the catalog does not hold")
	plain, err := mdnotebook.ValueOf(ld.Records, "stats", "p99_ms", "")
	require.NoError(t, err)
	assert.Equal(t, "412.3", plain.Face.Text, "the record's column declares no gloss")
}

// A record that reads an edited record was computed from the earlier
// statement too, and so was a record that reads that one (ADR-0371 §SD8).
func TestStalenessIsCarriedThroughRecords(t *testing.T) {
	const (
		a = "SELECT number AS n FROM numbers(3)"
		b = "SELECT sum(n) AS s FROM a"
		c = "WITH doubled AS (SELECT s * 2 AS d FROM b) SELECT d FROM doubled"
		d = "SELECT 1 AS one"
		// e names a column like a record and reads no record.
		e = "SELECT 2 AS a"
	)
	fence := func(statement, id string) string { return "```sql\n" + statement + "\n```\n^" + id + "\n\n" }
	entry := func(name, statement string) string {
		return "  " + name + ": {blob: n.blobs/" + mdnotebook.BlobHash([]byte(name)) + ".arrow, of: " + mdnotebook.StatementHash(statement) + "}\n"
	}
	text := "---\nrecords:\n" + entry("a", a) + entry("b", b) + entry("c", c) + entry("d", d) + entry("e", e) + "---\n\n" +
		fence(a, "a") + fence(b, "b") + fence(c, "c") + fence(d, "d") + fence(e, "e")

	nb, err := mdnotebook.Read([]byte(text))
	require.NoError(t, err)
	ld := mdnotebook.Load(nb, fstest.MapFS{})
	assert.Empty(t, ld.Stale)
	assert.Empty(t, ld.StaleVia)

	nb, err = mdnotebook.Read([]byte(strings.Replace(text, "numbers(3)", "numbers(4)", 1)))
	require.NoError(t, err)
	ld = mdnotebook.Load(nb, fstest.MapFS{})
	assert.Equal(t, map[string]bool{"a": true}, ld.Stale)
	assert.Equal(t, map[string]string{"b": "a", "c": "a"}, ld.StaleVia)
	assert.Equal(t, []string{"a"}, mdnotebook.StaleRecords(nb, ld))
	assert.Equal(t, []mdnotebook.DerivedStale{{Record: "b", Edited: "a"}, {Record: "c", Edited: "a"}}, mdnotebook.DerivedStaleRecords(nb, ld))
	for record, want := range map[string]string{"a": "a", "b": "a", "c": "a"} {
		edited, stale := ld.StaleSource(record)
		assert.True(t, stale, record)
		assert.Equal(t, want, edited, record)
	}
	_, stale := ld.StaleSource("d")
	assert.False(t, stale)
	_, stale = ld.StaleSource("e")
	assert.False(t, stale)
}

// A recording or a prose image named by a content hash is checked against
// it; one under any other name is read as the file it is. What cannot be
// read is a problem, and what the text only shows as code or links
// elsewhere is not the notebook's to read.
func TestLoadReadsTheFilesTheTextNames(t *testing.T) {
	plain := []byte("pixels")
	named := []byte("named by content")
	const other = "0123456789abcdef0123456789abcdef"
	text := strings.Join([]string{
		"# Pictures",
		"",
		"A plot ![plot](img/plot.png) and one that is gone ![gone](img/gone.png).",
		"",
		"![[" + mdnotebook.BlobHash(named) + ".png]] is named by its content, ![[" + other + ".png|200]] by another's.",
		"",
		"The spelling is `![alt](img/sample.png)`, a remote one is ![remote](https://example.org/a.png),",
		"and ![[Some Note]] embeds a note.",
		"",
		"```markdown",
		"![alt](img/fenced.png)",
		"```",
		"",
		"![a take](take.wav)",
		"",
		"![another](" + other + ".wav)",
		"",
	}, "\n")
	fsys := fstest.MapFS{
		"img/plot.png":                      {Data: plain},
		mdnotebook.BlobHash(named) + ".png": {Data: named},
		other + ".png":                      {Data: plain},
		"take.wav":                          {Data: plain},
		other + ".wav":                      {Data: plain},
		"img/sample.png":                    {Data: plain},
		"img/fenced.png":                    {Data: plain},
	}
	nb, err := mdnotebook.Read([]byte(text))
	require.NoError(t, err)
	var images []string
	for _, img := range nb.Images {
		images = append(images, img.Path)
	}
	assert.Equal(t, []string{"img/plot.png", "img/gone.png", mdnotebook.BlobHash(named) + ".png", other + ".png"}, images)

	ld := mdnotebook.Load(nb, fsys)
	assert.Equal(t, plain, ld.Assets["img/plot.png"].Data)
	assert.Equal(t, named, ld.Assets[mdnotebook.BlobHash(named)+".png"].Data)
	assert.Equal(t, plain, ld.Assets["take.wav"].Data)
	assert.Error(t, ld.Assets["img/gone.png"].Err)
	assert.Error(t, ld.Assets[other+".png"].Err)
	assert.Error(t, ld.Assets[other+".wav"].Err)
	require.Len(t, ld.Problems, 3)
	assert.Contains(t, ld.Problems[0].Text, "image img/gone.png: ")
	assert.Contains(t, ld.Problems[1].Text, "image "+other+".png: ")
	assert.Contains(t, ld.Problems[1].Text, "does not match its name")
	assert.Contains(t, ld.Problems[2].Text, "recording "+other+".wav: ")
	assert.Len(t, mdnotebook.Problems(nb, ld), 3, "what could not be read stands between the notebook and being sealed")
}
