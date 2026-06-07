package godepview

import (
	"context"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	lgview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// layeredIDSalt is a high-entropy base for the layered canvas + per-node
// sense-region ids. graphIDBase adds the per-instance seed so two open explorer
// windows do not collide their canvases (capinspector's idiom).
const layeredIDSalt uint64 = 0x9e3779b97f4a7c15

func (inst *App) graphIDBase() (id uint64) { id = layeredIDSalt + inst.seed; return }

// renderGraphLayered draws the focus neighborhood with the layeredgraph widget
// (ADR-0069): a static layered (Sugiyama) layout computed by Graphviz `dot`
// in-process (cgo-free WASM via goccyengine) and painted by the imzero2 painter,
// with opt-in pan/zoom. A dependency neighborhood is a DAG — exactly what `dot`
// lays out best. The dot layout is cached against the neighborhood signature
// (ensureNeighborhood clears it on change), so `dot` runs once per neighborhood,
// not once per frame.
func (inst *App) renderGraphLayered() {
	if inst.layeredLayout == nil && inst.layeredErr == nil {
		inst.layeredLayout, inst.layeredErr = layoutNeighborhood(inst.graphReached, inst.idx)
	}
	if inst.layeredErr != nil {
		c.Label("layered layout unavailable: " + inst.layeredErr.Error()).Send()
		for rt := range c.RichTextLabel("switch engine to 'live' to keep exploring") {
			rt.Weak().Small()
		}
		return
	}
	lay := inst.layeredLayout
	if lay == nil || len(lay.Nodes) == 0 {
		return
	}

	w, h := inst.paneAvail(640, 320)
	res := lgview.Render(inst.graphIDBase(), lay, lgview.RenderOpts{
		CanvasW:  w,
		CanvasH:  h - 6,
		NodeFill: inst.layeredNodeFill(),
		NodeText: inst.layeredNodeText(),
		State:    &inst.layeredView,
	})
	// A node click re-focuses, like the live engine. The node id is the decimal
	// package id (see layoutNeighborhood).
	if res.Clicked != "" {
		if id, err := strconv.ParseUint(res.Clicked, 10, 64); err == nil {
			inst.focus = id
		}
	}
}

// layoutNeighborhood builds the dot model from the reached set — one ellipse
// node per package keyed by its decimal id, one forward-import edge per pair
// whose both endpoints are in the set — and lays it out left→right. The
// both-endpoints test on edges is required: goccyengine errors on an edge whose
// target is not a declared node. The model is built in sorted id order so the
// layout is deterministic and safely cacheable.
func layoutNeighborhood(reached map[uint64]int, idx *godep.Index) (lay *layeredgraph.Layout, err error) {
	if len(reached) == 0 || idx == nil {
		return
	}
	ids := make([]uint64, 0, len(reached))
	for id := range reached {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	m := layeredgraph.GraphModel{
		Nodes: make([]layeredgraph.Node, 0, len(ids)),
		Edges: make([]layeredgraph.Edge, 0, len(ids)),
	}
	for _, id := range ids {
		m.Nodes = append(m.Nodes, layeredgraph.Node{
			ID:    strconv.FormatUint(id, 10),
			Label: shortPathFor(idx, id),
			Shape: layeredgraph.NodeShapeEllipse,
		})
	}
	for _, id := range ids {
		p, ok := idx.Node(id)
		if !ok {
			continue
		}
		for _, to := range p.Imports {
			if _, in := reached[to]; in {
				m.Edges = append(m.Edges, layeredgraph.Edge{
					From: strconv.FormatUint(id, 10),
					To:   strconv.FormatUint(to, 10),
				})
			}
		}
	}

	eng, err := goccyengine.Shared()
	if err != nil {
		return
	}
	lay, err = eng.Layout(context.Background(), m, layeredgraph.LayoutOpts{
		RankDir:  layeredgraph.RankDirLeftRight,
		FontSize: 13,
	})
	return
}

// layeredNodeFill colours each node by class (matching the live engine's
// palette); the focused package takes the success tone so "you are here" stands
// out.
func (inst *App) layeredNodeFill() func(id string) (col egcolor.Color, ok bool) {
	focusID := strconv.FormatUint(inst.focus, 10)
	return func(id string) (col egcolor.Color, ok bool) {
		if id == focusID {
			return egcolor.Hex(styletokens.SuccessDefault.AsHex()), true
		}
		class := ""
		if pid, perr := strconv.ParseUint(id, 10, 64); perr == nil {
			if p, found := inst.idx.Node(pid); found {
				class = p.Class
			}
		}
		return egcolor.Hex(classRGBA(class)), true
	}
}

// layeredNodeText keeps node labels readable: the class fills are light IDS
// *Default tones, so dark ink reads best on them (capinspector's per-node ink
// pairing).
func (inst *App) layeredNodeText() func(id string) (col egcolor.Color, ok bool) {
	dark := egcolor.Hex(styletokens.NeutralBgExtreme.AsHex())
	return func(id string) (col egcolor.Color, ok bool) {
		return dark, true
	}
}

// shortPathFor is the node label: the last two path segments, since a full
// import path is too long to sit inside a node.
func shortPathFor(idx *godep.Index, id uint64) (s string) {
	if p, ok := idx.Node(id); ok {
		return shortPath(p.ImportPath)
	}
	return "#" + strconv.FormatUint(id, 10)
}
