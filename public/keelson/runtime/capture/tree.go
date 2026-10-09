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
