package notebookview

import (
	"sort"

	"github.com/stergiotis/boxer/public/keelson/runtime/registration"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// CellRenderer draws a presentation cell's result where it is called: the
// cell's own relation and the members of its WITH list, by name. A
// non-empty reason says the result is not one it draws; the view then shows
// the reason and the cell's own relation as a table. A renderer must
// tolerate a result it has not seen before: the notebook may be replaced.
type CellRenderer func(res *mdnotebook.Result) (reason string)

// RendererFactory builds the renderer of one presentation cell. The view
// calls it once per cell, and a cell whose text is unchanged keeps its
// renderer when the notebook is set again, so what a renderer keeps
// between frames (a layout, a camera, a fold of the rows) is that cell's
// own and outlives the records under it. ids is the view's stack; at render
// time it is scoped to the cell.
//
// close, when not nil, is called once when the view drops the renderer: its
// cell is gone from the notebook, or the view is closed. A renderer that
// owns workers stops them there.
type RendererFactory func(ids *c.WidgetIdStack) (draw CellRenderer, close func())

var renderers = map[string]RendererFactory{}

// RegisterRenderer makes build the renderer of every `sql <name>` cell in
// the process. It is called from an init function, by the package that owns
// the drawing: a host that shows notebooks links that package and does not
// import it. A name registered twice, or a built-in one, is recorded as a
// registration failure, which stops a host from starting (ADR-0396 §2).
func RegisterRenderer(name string, build RendererFactory) {
	registration.Fail("notebookview.RegisterRenderer", AddRenderer(name, build))
}

// AddRenderer is RegisterRenderer returning the refusal instead of
// recording it.
func AddRenderer(name string, build RendererFactory) (err error) {
	if name == "" || build == nil {
		err = eb.Build().Str("renderer", name).Errorf("notebookview: a renderer needs a name and a factory")
		return
	}
	if name == mdnotebook.RendererTable || name == RendererKV {
		err = eb.Build().Str("renderer", name).Errorf("notebookview: the renderer %q is built in", name)
		return
	}
	if _, taken := renderers[name]; taken {
		err = eb.Build().Str("renderer", name).Errorf("notebookview: renderer %q is registered twice", name)
		return
	}
	renderers[name] = build
	return
}

// RegisteredRenderers are the names renderers are registered under, sorted.
func RegisteredRenderers() (names []string) {
	for name := range renderers {
		names = append(names, name)
	}
	sort.Strings(names)
	return
}

func rendererNamed(name string) RendererFactory { return renderers[name] }
