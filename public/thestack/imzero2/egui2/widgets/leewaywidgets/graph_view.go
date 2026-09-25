package leewaywidgets

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// GraphOptions are the encoding choices a GraphModel is drawn under — the
// settings the Experiments graph sink declares (ADR-0257, proposed, §SD2).
type GraphOptions struct {
	Layout      graphview.LayoutE
	Orientation graphview.OrientationE
	// Spacing scales every layout's distances: the force layout's ideal
	// edge length, the hierarchical row and column gaps, the radial rings.
	Spacing float32
	// LabelsAlways labels every node; off, labels appear on hover only.
	LabelsAlways bool
	Directed     bool
	// ColorGroups tones nodes by group; off, every node takes the default.
	ColorGroups bool
}

// GraphLayoutSteps is the force layout's budget: the layout runs this many
// steps on its first frame and then holds. A force layout steps once per
// frame, so without a budget two captures of one graph taken a few frames
// apart are two different pictures; with it, one batch is one picture.
const GraphLayoutSteps = 1500

// Default distances the spacing multiplies (graphview's own defaults).
const (
	graphHierRowDist float32 = 60
	graphHierColDist float32 = 50
	graphRingDist    float32 = 60
)

// GraphView draws GraphModels with a retained graphview.View: positions,
// camera and layout state persist across frames, and a new model or new
// options restart the layout from the same seed positions.
type GraphView struct {
	ids   *c.WidgetIdStack
	view  *graphview.View
	model *GraphModel
	opts  GraphOptions
	nodes []graphview.NodeSpec
	edges []graphview.EdgeSpec
}

// NewGraphView returns a view drawing on ids.
func NewGraphView(ids *c.WidgetIdStack) *GraphView {
	return &GraphView{ids: ids}
}

func (inst *GraphView) rebuild(m *GraphModel, o GraphOptions) {
	inst.view = graphview.New(inst.ids, "leeway-graph", graphview.Options{})
	inst.model, inst.opts = m, o
	groups := make([]string, 0, 8)
	inst.nodes = inst.nodes[:0]
	for i, label := range m.Nodes {
		spec := graphview.NodeSpec{Id: uint64(i + 1), Label: label}
		if o.ColorGroups && m.Groups[i] != "" {
			g := slices.Index(groups, m.Groups[i])
			if g < 0 {
				g = len(groups)
				groups = append(groups, m.Groups[i])
			}
			spec.Color = color.Hex(styletokens.QualitativeCycle(g).AsHex())
		}
		inst.nodes = append(inst.nodes, spec)
	}
	inst.edges = inst.edges[:0]
	seen := make(map[[2]int]uint64, len(m.Edges))
	for _, e := range m.Edges {
		k := [2]int{e.From, e.To}
		seen[k]++
		inst.edges = append(inst.edges, graphview.EdgeSpec{
			From: uint64(e.From + 1), To: uint64(e.To + 1), Id: seen[k], Label: e.Kind,
		})
	}
	inst.view.FastForward(GraphLayoutSteps)
}

// Render draws the model in a w×h box.
func (inst *GraphView) Render(m *GraphModel, o GraphOptions, w, h float32) {
	if m == nil || m.Empty() {
		c.Label("The batch has no entities to draw.").Send()
		return
	}
	if inst.view == nil || inst.model != m || inst.opts != o {
		inst.rebuild(m, o)
	}
	opts := &inst.view.Opts
	opts.Layout = o.Layout
	opts.Hier.Orientation = o.Orientation
	opts.Force.KScale = 2 * o.Spacing
	opts.Hier.RowDist, opts.Hier.ColDist = graphHierRowDist*o.Spacing, graphHierColDist*o.Spacing
	opts.Radial.RingDist = graphRingDist * o.Spacing
	opts.LabelsAlways = o.LabelsAlways
	opts.Undirected = !o.Directed
	// Hold after the budget (see GraphLayoutSteps); the first frame runs it.
	opts.Force.Paused = inst.view.Metrics().Steps >= GraphLayoutSteps
	inst.view.Render(inst.nodes, inst.edges, w, h)
}
