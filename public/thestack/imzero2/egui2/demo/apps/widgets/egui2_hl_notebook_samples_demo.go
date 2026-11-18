package widgets

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "notebook-samples",
		Category: "Text & code",
		Title:    "notebook samples (to read and to start from)",
		Stage:    [2]float32{1024, 760},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The sample notebooks of package mdnotebook/samples (ADR-0371), each drawn from its text and the blobs beside it: " +
			"a notebook in two records, a report that leads with its figures, and one cell per renderer. Every record of a sample makes " +
			"its own rows, so the file collects against any ClickHouse. The renderers other than the table are play's panels, as in the " +
			"notebook demo: a host that does not link play shows those cells as tables and says so.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = newNotebookSamplesState(ids)
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoNotebookSamples(ids, state.(*notebookSamplesState))
		},
		SourceFunc: demoNotebookSamples,
	})
}

type notebookSamplesState struct {
	view *notebookview.View
	// chosen and shown index samples.Names(): the radio's choice and the
	// sample the view was last given.
	chosen, shown int
	clicked       bool
	statements    bool
	last          string
}

func newNotebookSamplesState(ids *c.WidgetIdStack) (st *notebookSamplesState) {
	st = &notebookSamplesState{shown: -1}
	st.view = notebookview.New(ids, "notebook-samples", notebookview.Options{Statements: notebookview.StatementsHidden})
	return
}

// load hands the view the chosen sample: its text, and the samples'
// directory as the place its blobs are found.
func (inst *notebookSamplesState) load() {
	inst.shown = inst.chosen
	inst.last = ""
	text, err := fs.ReadFile(samples.FS(), samples.Names()[inst.chosen])
	if err != nil {
		inst.last = err.Error()
		return
	}
	nb, err := mdnotebook.Read(text)
	if err != nil {
		inst.last = err.Error()
		return
	}
	inst.view.SetNotebook(nb, samples.FS())
}

func demoNotebookSamples(ids *c.WidgetIdStack, st *notebookSamplesState) {
	for range c.HorizontalTop().KeepIter() {
		c.Label("Sample").Send()
		for i, name := range samples.Names() {
			for range c.IdScope(ids.PrepareSeq(uint64(i))) {
				label := "sample " + strings.TrimSuffix(name, ".md")
				if c.RadioButton(ids.PrepareStr("sample"), c.Atoms().Text(label).Keep(), st.chosen == i).SendRespVal(&st.clicked).HasPrimaryClicked() {
					st.chosen = i
				}
			}
		}
		c.Checkbox(ids.PrepareStr("statements"), st.statements, "show the statements").SendRespVal(&st.statements)
	}
	if st.chosen != st.shown {
		st.load()
	}
	if st.last != "" {
		c.Label("last: " + st.last).Send()
		return
	}
	st.view.Opts.Statements = notebookview.StatementsHidden
	if st.statements {
		st.view.Opts.Statements = notebookview.StatementsShown
	}
	nb := st.view.Notebook()
	c.Label(fmt.Sprintf("sample: %d cells · %d records · %d problems", len(nb.Cells), len(nb.Records), len(st.view.Problems()))).Send()
	c.Separator().Send()
	st.view.Render()
}
