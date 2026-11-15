package notebookview_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// people is a relation of (name, lag, site), one row each.
func people(t testing.TB, names ...string) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "lag", Type: arrow.PrimitiveTypes.Float64},
		{Name: "site", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	for i, name := range names {
		b.Field(0).(*array.StringBuilder).Append(name)
		b.Field(1).(*array.Float64Builder).Append(412.3 + float64(i))
		b.Field(2).(*array.StringBuilder).Append("entity:" + name)
	}
	return b.NewRecordBatch()
}

// report is a notebook as a programme writes one over what it holds: no
// frontmatter and no record cell. extra puts a section in above the others.
func report(t testing.TB, extra bool) *mdnotebook.Notebook {
	t.Helper()
	var w mdnotebook.Writer
	w.Heading(1, "One of them")
	w.Markdown("Lag was " + mdnotebook.Figure("one", "lag", "gloss/duration;unit=ms") + ".")
	if extra {
		w.Heading(2, "Everyone")
		w.Cell("table", "SELECT * FROM everyone")
	}
	w.Heading(2, "Facts")
	w.Cell("kv", "SELECT name, lag AS "+mdnotebook.Alias("lag", "gloss/duration;unit=ms")+", site AS "+mdnotebook.Alias("site", "gloss/url")+" FROM one")
	w.Heading(2, "Lag")
	w.Cell("chart", "WITH cell_opts AS (SELECT 120 AS height)\nSELECT name, lag FROM everyone")
	require.NoError(t, w.Err())
	nb, err := mdnotebook.Read(append([]byte(nil), w.Bytes()...))
	require.NoError(t, err)
	return nb
}

// bind is the records a report reads, for the row of everyone it shows:
// the row is a slice of the relation, not a copy.
func bind(everyone arrow.RecordBatch, row int64) (records map[string]arrow.RecordBatch, release func()) {
	one := everyone.NewSlice(row, row+1)
	return map[string]arrow.RecordBatch{"everyone": everyone, "one": one}, one.Release
}

// A notebook over records its host holds draws without a blob: nothing is
// wrong with it, it is not sealed, and a figure reads as its value.
func TestABoundNotebookDrawsWhatItsHostHolds(t *testing.T) {
	t.Cleanup(scenetest.Install())
	everyone := people(t, "ada", "bo")
	defer everyone.Release()
	records, release := bind(everyone, 1)
	defer release()
	nb := report(t, false)

	p := notebookview.PrepareFrom(nb, notebookview.Source{Records: records})
	ck := p.Check()
	assert.True(t, ck.Bound)
	assert.True(t, ck.Clean(), "%+v", ck)
	assert.False(t, ck.Sealed(), "a bound notebook stores nothing")
	assert.Contains(t, string(p.Notebook().Src), "Lag was [413.3 ms](val:one.lag")

	for _, plain := range []bool{true, false} {
		v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{Plain: plain, Statements: notebookview.StatementsHidden})
		v.Set(notebookview.PrepareFrom(nb, notebookview.Source{Records: records}))
		assert.Empty(t, v.Problems())
		for _, mode := range notebookview.AllStatements {
			v.Opts.Statements = mode
			frame(v)
			frame(v)
		}
		v.FoldAll(true)
		frame(v)
		v.Close()
	}
	p.Release()

	// A record the host did not hand over is said so, in the host's terms.
	delete(records, "one")
	p = notebookview.PrepareFrom(nb, notebookview.Source{Records: records})
	defer p.Release()
	ck = p.Check()
	assert.False(t, ck.Clean())
	require.NotEmpty(t, ck.Problems)
	assert.Contains(t, ck.Problems[0], "the record one is not bound")
}

// Sealing what was bound gives a notebook that reads the same from blobs.
func TestABoundNotebookSealed(t *testing.T) {
	everyone := people(t, "ada", "bo")
	defer everyone.Release()
	records, release := bind(everyone, 0)
	defer release()
	nb := report(t, true)

	out, err := mdnotebook.Seal(nb, records, "r.blobs", nil)
	require.NoError(t, err)
	fsys := fstest.MapFS{}
	for name, blob := range out.Blobs {
		fsys[name] = &fstest.MapFile{Data: blob}
	}
	sealed, err := mdnotebook.Read(out.Text)
	require.NoError(t, err)
	p := notebookview.Prepare(sealed, fsys)
	defer p.Release()
	assert.True(t, p.Check().Sealed(), "%+v", p.Check())
}

// A notebook set again over other records, or with a section put in above,
// keeps each unchanged cell's renderer and fold: the renderer is handed the
// new result.
func TestSetAgainKeepsWhatDidNotChange(t *testing.T) {
	t.Cleanup(scenetest.Install())
	everyone := people(t, "ada", "bo", "cy")
	defer everyone.Release()
	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{Plain: true, Statements: notebookview.StatementsHidden})
	defer v.Close()

	show := func(row int64, extra bool) {
		records, release := bind(everyone, row)
		defer release()
		v.Set(notebookview.PrepareFrom(report(t, extra), notebookview.Source{Records: records}))
	}
	before := len(chartCalls)
	show(0, false)
	require.Len(t, chartCalls, before+1)
	chart := chartCalls[before]
	frame(v)
	assert.Equal(t, 1, *chart)

	show(1, false)
	assert.Len(t, chartCalls, before+1, "the same text over another row builds no renderer")
	frame(v)
	assert.Equal(t, 2, *chart, "and the renderer it kept is the one drawn")

	v.FoldAll(true)
	folded := v.Folded()
	show(2, true)
	assert.Len(t, chartCalls, before+1, "a section put in above moves the cell and keeps its renderer")
	assert.Equal(t, folded, v.Folded(), "the folds follow the cells by their headings; the cell that is new is open")
	frame(v)
	assert.Equal(t, 2, *chart, "a folded cell is not drawn")

	// A cell that is gone from the notebook has its renderer closed, and
	// closing the view closes the rest.
	closed := chartsClosed
	var w mdnotebook.Writer
	w.Paragraph("Nothing to chart.")
	nb, err := mdnotebook.Read(w.Bytes())
	require.NoError(t, err)
	v.Set(notebookview.PrepareFrom(nb, notebookview.Source{Records: map[string]arrow.RecordBatch{}}))
	assert.Equal(t, closed+1, chartsClosed)
	show(0, false)
	v.Close()
	assert.Equal(t, closed+2, chartsClosed)
}

// A URL in a `kv` cell that the host claims is reported as a click, not
// opened.
func TestAClaimedLinkInAKVCell(t *testing.T) {
	t.Cleanup(scenetest.Install())
	everyone := people(t, "ada")
	defer everyone.Release()
	records, release := bind(everyone, 0)
	defer release()
	ids := c.NewWidgetIdStack()
	v := notebookview.New(ids, "t", notebookview.Options{
		Plain:      true,
		Statements: notebookview.StatementsHidden,
		LinkClaims: func(url string) bool { return strings.HasPrefix(url, "entity:") },
	})
	defer v.Close()
	v.Set(notebookview.PrepareFrom(report(t, false), notebookview.Source{Records: records}))
	assert.Empty(t, frame(v).Links)

	// The link is the third field of the cell under "Facts", row 0.
	var link widgethandle.WidgetHandle
	for range c.IdScope(ids.PrepareStr("t")) {
		for range c.IdScope(ids.PrepareStr("cell:kv:Facts")) {
			for range c.IdScope(ids.PrepareSeq(0)) {
				for range c.IdScope(ids.PrepareStr("fields")) {
					link = widgethandle.Make(ids.PrepareSeq(2).Derive())
				}
			}
		}
	}
	c.CurrentApplicationState.StateManager.ScriptResponse(link, c.PrimaryClickedResponseFlags)
	ev := frame(v)
	require.Len(t, ev.Links, 1)
	assert.Equal(t, "entity:ada", ev.Links[0].URL)
}

// Showing one row after another of the same shape, the two ways a host can
// hand a generated notebook its records: bound, and stored as blobs to be
// read back. Both arms write the text, read it, prepare and set it.
func BenchmarkShowNextRow(b *testing.B) {
	_, _, reset := scenetest.InstallCounting()
	defer reset()
	everyone := people(b, "ada", "bo", "cy", "di")
	defer everyone.Release()
	text := append([]byte(nil), report(b, true).Src...)

	bound := func(cat *gloss.Catalog) func(b *testing.B) {
		return func(b *testing.B) {
			v := notebookview.New(c.NewWidgetIdStack(), "b", notebookview.Options{Plain: true, Statements: notebookview.StatementsHidden})
			defer v.Close()
			row := int64(0)
			b.ReportAllocs()
			for b.Loop() {
				records, release := bind(everyone, row%everyone.NumRows())
				nb, _ := mdnotebook.Read(text)
				v.Set(notebookview.PrepareFrom(nb, notebookview.Source{Records: records, Glosses: cat}))
				release()
				row++
			}
		}
	}
	b.Run("bound", bound(nil))
	// A host that keeps a catalog hands it over and none is built per row.
	b.Run("bound-host-catalog", bound(gloss.Default()))
	b.Run("blobs", func(b *testing.B) {
		v := notebookview.New(c.NewWidgetIdStack(), "b", notebookview.Options{Plain: true, Statements: notebookview.StatementsHidden})
		defer v.Close()
		row := int64(0)
		b.ReportAllocs()
		for b.Loop() {
			records, release := bind(everyone, row%everyone.NumRows())
			nb, _ := mdnotebook.Read(text)
			out, err := mdnotebook.Seal(nb, records, "b", nil)
			if err != nil {
				b.Fatal(err)
			}
			fsys := fstest.MapFS{}
			for name, blob := range out.Blobs {
				fsys[name] = &fstest.MapFile{Data: blob}
			}
			sealed, _ := mdnotebook.Read(out.Text)
			v.Set(notebookview.Prepare(sealed, fsys))
			release()
			row++
		}
	})
}
