package widgets

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "notebook",
		Category: "Text & code",
		Title:    "notebook (sealed report)",
		Stage:    [2]float32{1024, 760},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/notebookview over a fixture notebook (ADR-0371): one markdown document of typed cells with the " +
			"blobs it names. Records are stored results, each checked against the content hash its blob is named by; a presentation cell is a SQL statement " +
			"that picks and renames a record's columns and is answered here, with no server; a figure in a sentence is a link to a stored " +
			"value. A cell that filters or computes is refused and says why. The table is built in; every other renderer is one a package " +
			"registered under the name a fence gives. This demo registers none: in a host that links play, the charts and the graph are " +
			"play's panels, and in one that does not, those cells fall back to a table and say so.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = newNotebookDemoState(ids)
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoNotebook(ids, state.(*notebookDemoState))
		},
		SourceFunc: demoNotebook,
	})
}

// =============================================================================
// notebook — widgets/notebookview showcase (ADR-0371).
//
// What the demo is trying to show:
//
//   - the text is the artefact: the view is handed the fixture's markdown
//     and its directory, nothing else;
//   - a report is the notebook with its statements folded, and "show
//     statements" is the only difference between the two;
//   - editing a record's statement makes what is stored stale, and every
//     cell that reads the record says so, while nothing is recomputed;
//   - a presentation renderer is registered under a name by the package
//     that owns the drawing, and this package imports none of them.
// =============================================================================

type notebookDemoState struct {
	view *notebookview.View
	src  []byte

	statements notebookview.StatementsE
	// modeClicked is where the radio buttons write; the mode is read from
	// their clicks.
	modeClicked bool
	edited      bool
	// applied is the edit state the view was last given.
	applied bool
	last    string
}

func newNotebookDemoState(ids *c.WidgetIdStack) (st *notebookDemoState) {
	st = &notebookDemoState{}
	src, err := fs.ReadFile(fixture.FS(), fixture.Name)
	if err != nil {
		st.last = "the fixture could not be read: " + err.Error()
		return
	}
	st.src = src
	st.statements = notebookview.StatementsHidden
	st.view = notebookview.New(ids, "notebook-demo", notebookview.Options{Statements: st.statements})
	st.load()
	return
}

// load hands the view the fixture's text, with one record's statement
// edited when the demo's switch is on.
func (inst *notebookDemoState) load() {
	text := string(inst.src)
	if inst.edited {
		text = strings.Replace(text, "region, quantile(0.99)(lag_ms) AS p99", "region, quantile(0.95)(lag_ms) AS p99", 1)
	}
	nb, err := mdnotebook.Read([]byte(text))
	if err != nil {
		inst.last = err.Error()
		return
	}
	inst.view.SetNotebook(nb, fixture.FS())
	inst.applied = inst.edited
}

func demoNotebook(ids *c.WidgetIdStack, st *notebookDemoState) {
	if st.view == nil {
		c.Label(st.last).Send()
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("statements").Send()
		for i, mode := range notebookStatementModes {
			for range c.IdScope(ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(ids.PrepareStr("mode"), c.Atoms().Text(mode.label).Keep(), st.statements == mode.value).SendRespVal(&st.modeClicked).HasPrimaryClicked() {
					st.statements = mode.value
				}
			}
		}
		c.Checkbox(ids.PrepareStr("edited"), st.edited, "edit a record's statement").SendRespVal(&st.edited)
	}
	if st.edited != st.applied {
		st.load()
	}
	st.view.Opts.Statements = st.statements
	nb := st.view.Notebook()
	c.Label(fmt.Sprintf("notebook: %d cells · %d records · %d problems", len(nb.Cells), len(nb.Records), len(st.view.Problems()))).Send()
	c.Label(fmt.Sprintf("renderers: %d registered", len(notebookview.RegisteredRenderers()))).Send()
	if st.last != "" {
		c.Label("last: " + st.last).Send()
	}
	c.Separator().Send()

	ev := st.view.Render()
	for _, v := range ev.Vals {
		st.last = fmt.Sprintf("figure %s.%s, shown as %s", v.Record, v.Column, v.Label)
	}
	for _, m := range ev.Media {
		st.last = fmt.Sprintf("recording %s at %.0f%%", m.Path, m.Fraction*100)
	}
}

var notebookStatementModes = []struct {
	label string
	value notebookview.StatementsE
}{
	{"hidden (report)", notebookview.StatementsHidden},
	{"folded", notebookview.StatementsFolded},
	{"open (notebook)", notebookview.StatementsOpen},
}
