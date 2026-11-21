package notebookview_test

import (
	"bytes"
	"image"
	"image/png"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func frame(v *notebookview.View) notebookview.Events {
	defer c.CurrentApplicationState.StateManager.ScriptReset()
	return v.Render()
}

// chartCalls counts, per instance the factory built, the frames it drew;
// chartsClosed the instances the view closed.
var (
	chartCalls   []*int
	chartsClosed int
)

func init() {
	notebookview.RegisterRenderer("chart", func(ids *c.WidgetIdStack) (notebookview.CellRenderer, func()) {
		n := new(int)
		chartCalls = append(chartCalls, n)
		return func(res *mdnotebook.Result) string {
			*n++
			if res.Main == nil {
				return "no relation"
			}
			return ""
		}, func() { chartsClosed++ }
	})
}

// A fresh view draws nothing much, and the fixture draws for several frames
// with every kind of cell, under the discard channel.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(egui2test.Install())
	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{})
	defer v.Close()
	assert.Empty(t, frame(v).Vals)

	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(src)
	require.NoError(t, err)
	before := len(chartCalls)
	v.SetNotebook(nb, fixture.FS())
	charts := chartCalls[before:]
	assert.Len(t, charts, 2, "one renderer instance per cell that names it")
	assert.Empty(t, v.Problems(), "the fixture is sealed and its labels match its values")

	for range 3 {
		ev := frame(v)
		assert.Empty(t, ev.Vals)
		assert.Empty(t, ev.Media)
	}
	for _, n := range charts {
		assert.Equal(t, 3, *n, "a cell's renderer is called once a frame")
	}

	for _, mode := range notebookview.AllStatements {
		v.Opts.Statements = mode
		frame(v)
		frame(v)
	}

	// A second notebook replaces the first.
	v.SetNotebook(nb, fixture.FS())
	frame(v)
	v.SetNotebook(nil, nil)
	frame(v)
}

// A host whose files take time prepares the notebook on a goroutine of its
// own and hands the result to the view on the render goroutine.
func TestPrepareOffTheRenderGoroutine(t *testing.T) {
	t.Cleanup(egui2test.Install())
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(src)
	require.NoError(t, err)
	done := make(chan *notebookview.Prepared)
	go func() { done <- notebookview.Prepare(nb, fixture.FS()) }()
	p := <-done
	assert.Same(t, nb, p.Notebook())

	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{})
	defer v.Close()
	v.Set(p)
	assert.Empty(t, v.Problems())
	frame(v)
	// A prepared notebook no view took is released by its maker.
	notebookview.Prepare(nb, fixture.FS()).Release()
}

// A label that is no longer its value's rendering, and a statement edited
// since collection, are both reported.
func TestProblemsAndStaleness(t *testing.T) {
	t.Cleanup(egui2test.Install())
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	edited := []byte(replaceOnce(t, string(src), "[412.3 ms](val:stats.p99_ms", "[400 ms](val:stats.p99_ms"))
	nb, err := mdnotebook.Read(edited)
	require.NoError(t, err)
	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{})
	defer v.Close()
	v.SetNotebook(nb, fixture.FS())
	require.Len(t, v.Problems(), 1)
	assert.Contains(t, v.Problems()[0], "400 ms")
	assert.Contains(t, v.Problems()[0], "412.3 ms")
	frame(v)
}

// A picture under an ordinary name is read as the file it is, as in any
// markdown; one that cannot be read or decoded is a problem the view lists.
func TestPicturesUnderOrdinaryNames(t *testing.T) {
	t.Cleanup(egui2test.Install())
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 4, 3))))
	fsys := fstest.MapFS{
		"img/plot.png":   {Data: picture.Bytes()},
		"img/broken.png": {Data: []byte("not a picture")},
	}
	nb, err := mdnotebook.Read([]byte("# Pictures\n\n![plot](img/plot.png)\n\n![broken](img/broken.png) ![gone](img/gone.png)\n"))
	require.NoError(t, err)
	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{})
	defer v.Close()
	v.SetNotebook(nb, fsys)
	require.Len(t, v.Problems(), 2)
	assert.Contains(t, v.Problems()[0], "image img/gone.png")
	assert.Contains(t, v.Problems()[1], "image img/broken.png")
	frame(v)
	frame(v)
}

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	i := indexOf(s, old)
	require.GreaterOrEqual(t, i, 0)
	return s[:i] + repl + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// One frame of the fixture, Go side only: what the view emits into a
// channel that discards it.
func BenchmarkRenderFixture(b *testing.B) {
	messages, zero, reset := egui2test.InstallCounting()
	defer reset()
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(b, err)
	nb, err := mdnotebook.Read(src)
	require.NoError(b, err)
	v := notebookview.New(c.NewWidgetIdStack(), "b", notebookview.Options{Statements: notebookview.StatementsHidden})
	defer v.Close()
	v.SetNotebook(nb, fixture.FS())
	frame(v)
	frame(v)
	zero()
	b.ReportAllocs()
	for b.Loop() {
		frame(v)
	}
	b.ReportMetric(float64(messages())/float64(b.N), "messages/frame")
}

// A folded cell builds nothing under its header: its renderer is not
// called. The fold is kept when the notebook is set again, and FoldAll
// folds and unfolds every cell that has a header.
func TestAFoldedCellBuildsNothing(t *testing.T) {
	t.Cleanup(egui2test.Install())
	v := notebookview.New(c.NewWidgetIdStack(), "fold", notebookview.Options{Statements: notebookview.StatementsHidden})
	defer v.Close()
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	require.NoError(t, err)
	nb, err := mdnotebook.Read(src)
	require.NoError(t, err)
	before := len(chartCalls)
	v.SetNotebook(nb, fixture.FS())
	charts := chartCalls[before:]
	require.Len(t, charts, 2)
	frame(v)
	assert.Equal(t, 1, *charts[0])
	assert.Zero(t, v.Folded())

	v.FoldAll(true)
	assert.Positive(t, v.Folded())
	frame(v)
	frame(v)
	assert.Equal(t, 1, *charts[0], "a folded cell's renderer is not called")
	assert.Equal(t, 1, *charts[1])

	// Set again: the folds are the reader's and are kept, and so is each
	// cell's renderer, since its text did not change.
	before = len(chartCalls)
	v.SetNotebook(nb, fixture.FS())
	assert.Len(t, chartCalls, before, "a cell whose text is unchanged keeps its renderer")
	again := charts
	assert.Positive(t, v.Folded())
	frame(v)
	assert.Equal(t, 1, *again[0])

	v.FoldAll(false)
	assert.Zero(t, v.Folded())
	frame(v)
	assert.Equal(t, 2, *again[0])
	assert.Equal(t, 2, *again[1])
}
