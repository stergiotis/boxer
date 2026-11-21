package play

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// sampleRenderers is the sample that holds one cell per renderer.
const sampleRenderers = "renderers.md"

// Every presentation cell of every sample names a renderer that exists and
// is drawn by it: the panel takes the cell's relations and paints. The
// `renderers` sample has a cell for each panel play registers, so a panel
// added to the notebook's renderers without a cell there fails here.
func TestSampleNotebooksAreDrawnByPlaysPanels(t *testing.T) {
	messages, zero, reset := egui2test.InstallCounting()
	t.Cleanup(reset)
	drawn := map[string]map[string]int{}
	for _, name := range samples.Names() {
		text, err := fs.ReadFile(samples.FS(), name)
		require.NoError(t, err)
		nb, err := mdnotebook.Read(text)
		require.NoError(t, err)
		ld := mdnotebook.Load(nb, samples.FS())
		t.Cleanup(ld.Release)
		require.Empty(t, ld.Problems, name)
		drawn[name] = map[string]int{}
		for i := range nb.Cells {
			cell := &nb.Cells[i]
			if cell.Kind != mdnotebook.CellKindPresentation {
				continue
			}
			drawn[name][cell.Renderer]++
			if cell.Renderer == mdnotebook.RendererTable {
				continue
			}
			build, registered := notebookPanels[PanelID(cell.Renderer)]
			if !assert.Truef(t, registered, "%s: no renderer named %s", name, cell.Renderer) {
				continue
			}
			res, err := mdnotebook.Evaluate(cell.Body, ld.Records)
			require.NoErrorf(t, err, "%s: %s", name, cell.Body)
			zero()
			reason := renderNotebookCell(build(c.NewWidgetIdStack()), res)
			painted := messages()
			c.CurrentApplicationState.StateManager.ScriptReset()
			res.Release()
			assert.Emptyf(t, reason, "%s: the %s cell is refused", name, cell.Renderer)
			assert.Positivef(t, painted, "%s: the %s cell paints nothing", name, cell.Renderer)
		}
	}
	for _, id := range append(notebookview.RegisteredRenderers(), mdnotebook.RendererTable) {
		assert.Positivef(t, drawn[sampleRenderers][id], "the %s sample has no %s cell", sampleRenderers, id)
	}
}

// helpRenderersDoc is the help topic that lists the renderers a notebook
// cell can name.
const helpRenderersDoc = "reference-notebook-renderers"

// helpRendererFence matches a presentation cell quoted in the topic.
var helpRendererFence = regexp.MustCompile("(?ms)^```sql [a-z]+\n.*?\n```\n")

// The renderers topic and the registered renderers are the same set, in
// both directions: a panel registered for notebooks without a row, or a row
// kept past its panel, fails here. Every cell the topic quotes stands in
// the renderers sample as quoted, which is where it is drawn and checked.
func TestHelpCorpusNamesEveryNotebookRenderer(t *testing.T) {
	raw, err := fs.ReadFile(helpFS, "help/"+helpRenderersDoc+".md")
	require.NoError(t, err)
	documented := map[string]int{}
	for _, m := range helpOperationRow.FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]]++
	}
	// The table is the view's own and is not registered.
	registered := append(notebookview.RegisteredRenderers(), mdnotebook.RendererTable)
	for _, id := range registered {
		assert.Equalf(t, 1, documented[id], "the renderer %s has one row in help/%s.md", id, helpRenderersDoc)
	}
	for id := range documented {
		assert.Containsf(t, registered, id, "help/%s.md lists %s, which no renderer is registered as", helpRenderersDoc, id)
	}

	sample, err := fs.ReadFile(samples.FS(), sampleRenderers)
	require.NoError(t, err)
	quoted := helpRendererFence.FindAllString(string(raw), -1)
	require.NotEmpty(t, quoted)
	for _, cell := range quoted {
		assert.Containsf(t, string(sample), cell, "the topic quotes a cell that is not in %s", sampleRenderers)
	}
}
