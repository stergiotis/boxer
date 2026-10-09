package capture

import (
	"encoding/json"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A window tree (ADR-0301): the widgets the captured windows drew, each
// under the message of the FFFI2 stream that drew it. The client writes it
// from the same replay as a pixel capture, so it holds what the pixels
// show: the granted windows and their popups, and no widget clipped out of
// view.

// MediaTypeTree is a window tree's media type.
const MediaTypeTree = "application/vnd.boxer.window-tree+json"

// TreeVersion is the document version this package reads and writes.
const TreeVersion uint32 = 1

// TreeBlockOp is the Op of a row standing for one replay of a deferred
// block — a table cell or row, a popup's or a tooltip's body — rather than a
// message of the stream.
const TreeBlockOp = "DeferredBlock"

// Tree is a window tree document.
type Tree struct {
	V uint32 `json:"v"`
	// Taken is when the frame the tree was drawn from was recorded, RFC 3339
	// in UTC; the capture service sets it.
	Taken string   `json:"taken,omitempty"`
	Ops   []TreeOp `json:"ops"`
}

// TreeOp is one message of the stream that drew at least one visible
// widget, itself or below it. Messages that drew nothing are left out, and
// Parent skips them.
type TreeOp struct {
	// Op is the message's opcode by its Go binding name (Button, Window,
	// LabelAtoms); it follows the IDL.
	Op string `json:"op"`
	// Parent indexes Ops; -1 at the top.
	Parent int `json:"parent"`
	// Rect is the union of every visible widget below the message, in
	// logical points of the viewport: x, y, w, h.
	Rect [4]float32 `json:"rect"`
	// Window is the key of the window whose span the message lies in, and
	// WindowRect that window's outer rect when its frame was recorded; set
	// by the capture service on top-level rows (Parent -1), from the
	// client's At.
	Window     uint64      `json:"window,omitempty"`
	WindowRect *[4]float32 `json:"window_rect,omitempty"`
	// At is where a top-level row's message starts in the replayed stream,
	// as the client writes it; the capture service replaces it with Window.
	At *int `json:"at,omitempty"`
	// Clipped counts widgets drawn below the message but clipped to nothing:
	// scrolled out of view or cut by their container. Their names are not in
	// the tree.
	Clipped int `json:"clipped,omitempty"`
	// Blocks counts the deferred blocks the message received; the
	// TreeBlockOp rows below it are the ones drawn. A table receives one per
	// row or cell and draws those in view.
	Blocks  int          `json:"blocks,omitempty"`
	Widgets []TreeWidget `json:"widgets"`
}

// TreeWidget is a widget the message registered itself.
type TreeWidget struct {
	// Id is the egui id; it is stable across frames only as egui's ids are.
	Id uint64 `json:"id"`
	// Rect is the part of the widget on screen, in logical points.
	Rect [4]float32 `json:"rect"`
	// Role, Name and Value come from the AccessKit tree of the same replay;
	// empty for a widget without a node there. Role is lower snake case, the
	// headless driver's vocabulary.
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// ParseTree reads a window tree, refusing a version it does not know and
// parents that do not point back into the document.
func ParseTree(b []byte) (t Tree, err error) {
	err = json.Unmarshal(b, &t)
	if err != nil {
		err = eh.Errorf("capture: undecodable window tree: %w", err)
		return
	}
	if t.V != TreeVersion {
		err = eb.Build().Uint32("v", t.V).Errorf("capture: unknown window tree version")
		return
	}
	for i, o := range t.Ops {
		if o.Parent < -1 || o.Parent >= i {
			err = eb.Build().Int("op", i).Int("parent", o.Parent).Errorf("capture: a window tree's parent must come before its child")
			return
		}
	}
	return
}

// SpanWindow is one window's span in a replayed stream: the window it
// draws, where the span lies in the stream, and the window's outer rect when
// the frame was recorded.
type SpanWindow struct {
	Window     uint64
	Begin, End int
	Rect       [4]float32
}

// StampWindows names the window of every top-level row whose At lies in one
// of spans, and drops At: an offset into a stream means nothing outside its
// replay.
func StampWindows(t *Tree, spans []SpanWindow) {
	for i := range t.Ops {
		op := &t.Ops[i]
		if op.At == nil {
			continue
		}
		for _, s := range spans {
			if *op.At >= s.Begin && *op.At < s.End {
				r := s.Rect
				op.Window, op.WindowRect = s.Window, &r
				break
			}
		}
		op.At = nil
	}
}

// WindowOf is the window an op lies in, and that window's rect when the
// frame was recorded: those of its top-level ancestor. ok is false when the
// ancestor names none.
func (inst Tree) WindowOf(op int) (window uint64, rect [4]float32, ok bool) {
	for op >= 0 && op < len(inst.Ops) {
		o := inst.Ops[op]
		if o.Parent < 0 {
			if o.Window == 0 || o.WindowRect == nil {
				return
			}
			return o.Window, *o.WindowRect, true
		}
		op = o.Parent
	}
	return
}

// TreeHandlerI carries out one obligation on a window tree.
type TreeHandlerI interface {
	Phase() PhaseE
	Check(o Obligation) error
	Apply(t *Tree, o Obligation) error
}

// scopeTree: the source draws only the scope's windows; a tree is not
// cropped.
type scopeTree struct{}

func (inst scopeTree) Phase() PhaseE { return PhaseScope }

func (inst scopeTree) Check(o Obligation) error {
	if o.Scope == nil || len(o.Scope.Windows) == 0 {
		return eh.Errorf("a scope names at least one window")
	}
	if o.Scope.Crop != nil {
		return eh.Errorf("a window tree is not cropped")
	}
	return nil
}

func (inst scopeTree) Apply(*Tree, Obligation) error { return nil }
