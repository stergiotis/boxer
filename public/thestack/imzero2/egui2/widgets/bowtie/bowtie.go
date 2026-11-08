package bowtie

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// Options configures a Diagram. Every zero value is a default, and the
// Diagram re-reads Opts on every Render, so a change is an assignment.
type Options struct {
	// Engine lays the diagram out; nil takes goccyengine.Shared(). A change
	// takes effect at the next re-layout, which a model change triggers.
	Engine layeredgraph.Engine
	// FontSize is the node-label size in points; 0 takes 13.
	FontSize float64
	// HideLegend drops the status legend above the drawing.
	HideLegend bool
	// NoPanZoom keeps the drawing fitted to its box: no drag to pan, no
	// ctrl+scroll to zoom.
	NoPanZoom bool
}

// Events is what one Render reports. Hovered and Clicked are from the
// previous frame's input (the canvas's one-frame lag); the zero Element,
// Kind ElementNone, is nothing.
type Events struct {
	Hovered Element
	Clicked Element
	// Err is why nothing was drawn: a layout failure. The message is drawn
	// in place as well.
	Err error
}

// Diagram is a bow-tie diagram, a semi-retained widget (ADR-0267): it keeps
// the layout of the last model it drew and the pan and zoom across frames.
// Construct with New, call Render every frame.
type Diagram struct {
	// Opts is re-read on every Render.
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string

	graph     diagramGraph
	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error
	viewState view.ViewState
}

// defaultScopeKey is the scope an empty scopeKey takes.
const defaultScopeKey = "bowtie"

// defaultFontPt is the node-label size when Options.FontSize is unset, and
// topEventScale how much larger the top event's label is.
const (
	defaultFontPt = 13.0
	topEventScale = 1.3
)

// New builds a Diagram whose ids are scoped under scopeKey on ids; empty
// takes "bowtie". Two diagrams under one stack differ by scope key alone.
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) (inst *Diagram) {
	if scopeKey == "" {
		scopeKey = defaultScopeKey
	}
	inst = &Diagram{Opts: opts, ids: ids, scopeKey: scopeKey}
	return
}

// Render draws m into a w×h canvas, the legend above it unless hidden. A nil
// or empty model draws a short message. The layout is recomputed only when
// the model's structure, names or statuses change; evidence is read every
// frame.
func (inst *Diagram) Render(m *Model, w, h float32) (ev Events) {
	ev.Hovered, ev.Clicked = noElement, noElement
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		if m == nil || (m.TopEvent == "" && len(m.Threats) == 0 && len(m.Consequences) == 0) {
			weak("Nothing to draw: the bow-tie has no top event and no paths.")
			return
		}
		inst.ensureLayout(m)
		if inst.layout == nil {
			ev.Err = inst.layoutErr
			msg := "bow-tie layout unavailable"
			if inst.layoutErr != nil {
				msg += ": " + firstLine(inst.layoutErr.Error())
			}
			weak(msg)
			return
		}
		if !inst.Opts.HideLegend {
			inst.renderLegend()
		}
		res := view.Render(inst.ids.PrepareStr("canvas").Derive(), inst.layout, inst.renderOpts(m, w, h))
		ev.Hovered = inst.element(res.Hovered)
		ev.Clicked = inst.element(res.Clicked)
	}
	return
}

// RenderFill is Render sized to the pane: the room left in the enclosing Ui
// last frame, less the legend, with fallbackW × fallbackH until the probe
// answers.
func (inst *Diagram) RenderFill(m *Model, fallbackW, fallbackH float32) (ev Events) {
	w, h := fallbackW, fallbackH
	chrome := float32(0)
	if !inst.Opts.HideLegend {
		chrome = legendH
	}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		if pw, ph, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane")); ok && pw > 0 && ph > chrome+minFillH {
			w, h = pw, ph-chrome
		}
	}
	return inst.Render(m, w, h)
}

// legendH is what RenderFill holds back for the legend row; minFillH the
// smallest drawing it will size to the pane rather than fall back.
const (
	legendH  float32 = 34
	minFillH float32 = 80
)

// ensureLayout re-lays-out when the graph's fingerprint moved. A failed
// layout is remembered under its key, so a broken engine is not retried
// every frame.
func (inst *Diagram) ensureLayout(m *Model) {
	g := buildGraph(m)
	fontPt := inst.Opts.FontSize
	if fontPt <= 0 {
		fontPt = defaultFontPt
	}
	key := g.key + "|" + formatFloat(fontPt)
	inst.graph = g
	if key == inst.layoutKey {
		return
	}
	inst.layoutKey = key
	inst.layout, inst.layoutErr = nil, nil
	eng := inst.Opts.Engine
	if eng == nil {
		shared, err := goccyengine.Shared()
		if err != nil {
			inst.layoutErr = err
			return
		}
		eng = shared
	}
	inst.layout, inst.layoutErr = eng.Layout(context.Background(), g.model, layeredgraph.LayoutOpts{
		RankDir:  layeredgraph.RankDirLeftRight,
		RankSep:  0.3,
		NodeSep:  0.45,
		FontSize: fontPt,
		NodeFontSize: func(weight float64) (pt float64, ok bool) {
			if weight > 0 {
				return fontPt * topEventScale, true
			}
			return
		},
	})
	if inst.layoutErr != nil {
		inst.layout = nil
	}
}

// element maps a node id the view reported back to the model.
func (inst *Diagram) element(id string) Element {
	if id == "" {
		return noElement
	}
	if el, ok := inst.graph.elementOf[id]; ok {
		return el
	}
	return noElement
}

// renderOpts styles the drawing: neutral nodes, the top event inverted, and
// each barrier's status row filled in its status tone with the ink that
// reads on it — the badge's solid look.
func (inst *Diagram) renderOpts(m *Model, w, h float32) (opts view.RenderOpts) {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	st := view.DefaultStyle()
	st.TitleFill = color.Color{}                       // the name sits on the node's own fill
	st.TitleText = hex(styletokens.NeutralTextPrimary) // and keeps neutral ink: rows take the status ink
	pathFill := hex(styletokens.NeutralBgFaint)
	topFill, topInk := hex(styletokens.NeutralTextPrimary), hex(styletokens.NeutralBgPanel)
	opts = view.RenderOpts{
		Style:   st,
		CanvasW: w,
		CanvasH: h,
		NodeFill: func(id string) (col color.Color, ok bool) {
			switch inst.graph.elementOf[id].Kind {
			case ElementTopEvent:
				return topFill, true
			case ElementPath:
				return pathFill, true
			}
			return
		},
		NodeText: func(id string) (col color.Color, ok bool) {
			el := inst.graph.elementOf[id]
			switch el.Kind {
			case ElementTopEvent:
				return topInk, true
			case ElementBarrier:
				if b, found := barrierOf(m, el); found {
					return hex(b.Status.Tone().TextOnFill()), true
				}
			}
			return
		},
		RowFill: func(nodeID, rowID string) (col color.Color, ok bool) {
			if rowID != statusRowID {
				return
			}
			if b, found := barrierOf(m, inst.graph.elementOf[nodeID]); found {
				return hex(b.Status.Tone().Fill()), true
			}
			return
		},
		NodeRegion: func(id string) (r view.NodeRegion, ok bool) {
			return regionFor(m, inst.graph.elementOf[id])
		},
	}
	if !inst.Opts.NoPanZoom {
		opts.State = &inst.viewState
	}
	return
}

// renderLegend names the four statuses as solid badges, in the colours,
// icons and labels the barriers carry, and the reading direction.
func (inst *Diagram) renderLegend() {
	for range c.HorizontalTop().KeepIter() {
		for _, s := range [...]StatusE{StatusEffective, StatusDegraded, StatusFailed, StatusUntested} {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(s))) {
				badge.New(inst.ids.PrepareStr("status"), s.Label()).
					Tone(s.Tone()).Variant(badge.VariantSoft).Size(badge.SizeSm).Icon(s.Icon()).Send()
			}
		}
		for rt := range c.RichTextLabel("threats → prevention barriers → top event → mitigation barriers → consequences") {
			rt.Small().Weak()
		}
	}
}

// barrierOf resolves an element to its barrier.
func barrierOf(m *Model, el Element) (b Barrier, ok bool) {
	if el.Kind != ElementBarrier {
		return
	}
	paths := m.Paths(el.Side)
	if el.Path < 0 || el.Path >= len(paths) {
		return
	}
	bs := paths[el.Path].Barriers
	if el.Barrier < 0 || el.Barrier >= len(bs) {
		return
	}
	return bs[el.Barrier], true
}

// ---- hover, tooltip and accessible names

// regionFor is the hit region a node declares (view.RenderOpts.NodeRegion):
// its accessible name, which is the tooltip's first line, and the tooltip's
// body. A barrier is named by its name and says its status and evidence, a
// path its name and barrier count, the top event its name. Every element is
// a Tab stop; ok is false for a node that maps to no element.
func regionFor(m *Model, el Element) (r view.NodeRegion, ok bool) {
	switch el.Kind {
	case ElementBarrier:
		b, found := barrierOf(m, el)
		if !found {
			return
		}
		r.Label, r.Tooltip = b.Name, b.Status.Label()
		if b.Evidence != "" {
			r.Tooltip += "\n" + b.Evidence
		} else if b.Status != StatusUntested {
			r.Tooltip += "\nno evidence recorded"
		}
	case ElementPath:
		paths := m.Paths(el.Side)
		if el.Path < 0 || el.Path >= len(paths) {
			return
		}
		p := paths[el.Path]
		what := "threat"
		if el.Side == SideMitigation {
			what = "consequence"
		}
		r.Label, r.Tooltip = p.Name, what+" · "+plural(len(p.Barriers), "barrier")+" in series"
	case ElementTopEvent:
		r.Label, r.Tooltip = m.TopEvent, "top event"
	default:
		return
	}
	r.Focusable, ok = true, true
	return
}

// ---- small helpers

// weak draws a one-line note in the weak small style the panes use for an
// empty or failed state.
func weak(msg string) {
	for rt := range c.RichTextLabel(msg) {
		rt.Small().Weak()
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
