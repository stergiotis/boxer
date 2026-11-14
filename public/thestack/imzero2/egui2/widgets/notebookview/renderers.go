package notebookview

import (
	"sort"

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
// calls it once per cell when a notebook is set, so what a renderer keeps
// between frames (a layout, a camera, a fold of the rows) is that cell's
// own. ids is the view's stack; at render time it is scoped to the cell.
type RendererFactory func(ids *c.WidgetIdStack) CellRenderer

var renderers = map[string]RendererFactory{}

// RegisterRenderer makes build the renderer of every `sql <name>` cell in
// the process. It is called from an init function, by the package that owns
// the drawing: a host that shows notebooks links that package and does not
// import it. A name registered twice, or the built-in `table`, panics.
func RegisterRenderer(name string, build RendererFactory) {
	if name == "" || build == nil {
		panic("notebookview: RegisterRenderer needs a name and a factory")
	}
	if name == mdnotebook.RendererTable {
		panic("notebookview: the renderer `table` is built in")
	}
	if _, taken := renderers[name]; taken {
		panic("notebookview: renderer `" + name + "` registered twice")
	}
	renderers[name] = build
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
