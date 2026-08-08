// Package fieldview renders a hierarchical list of typed key-value
// pairs as an outline: one row per field, name and kind tag in the
// first column and the value (monospace) in the second. Container
// kinds (Object, Array) hold their Children beneath them, so deep
// trees stay collapsible.
//
// Originally lifted out of the logviewer's detail-pane fields
// section, so any caller that holds a slice of typed fields — log
// rows, card inspectors, debug dialogs — can render them with the
// same look without re-implementing the per-kind formatting.
//
// Usage:
//
//	r := fieldview.New(ids, "card-fld").BytesMax(128)  // once
//	r.Render(&state, fields)                           // per frame
//
// The Renderer is a value (not a pointer); fluent setters return a
// modified copy, so a base configuration is safe to share and vary
// per call:
//
//	base := fieldview.New(ids, "card").BytesMax(64)
//	base.Render(&headerState, headerFields)
//	base.ShowKind(false).Render(&footerState, footerFields)
//
// The [State] is the caller's, and one belongs to each place a list
// is shown — it holds which containers are open. Two lists shown at
// once need two States and two Renderers with different idPrefixes,
// since the prefix scopes the widget ids.
//
// A row is one line high, so a value that does not fit truncates and
// carries its full text as a tooltip; before ADR-0176 M3 the value
// had its own wrapping line under the name.
package fieldview

import "time"

// KindE discriminates the runtime type of a Field's value. Mirrors
// factsstore.LogFieldKindE for the primitive kinds, plus KindObject
// / KindArray for hierarchical containers whose value lives in
// Children rather than the typed slots.
//
// Numeric ordering is conventional, not load-bearing — switch arms
// must cover every constant; new kinds added here need matching arms
// in formatField and kindName below.
type KindE uint8

const (
	KindUnknown KindE = iota
	KindString
	KindInt
	KindUint
	KindFloat
	KindBool
	KindBytes
	KindTime
	// KindObject wraps a heterogeneous list of named children; Name
	// of each Child is the property key. Renders as a CollapsingHeader
	// titled by the parent Field's Name.
	KindObject
	// KindArray wraps an ordered list of children; convention is that
	// each Child's Name carries the index ("[0]", "[1]") so the array
	// reads as positional rather than as a degenerate Object. Renders
	// the same as KindObject (CollapsingHeader + indented children).
	KindArray
)

// Field is one tagged-union node. Leaf fields populate exactly one
// of the typed value slots matching Kind; container fields (Object
// / Array) populate Children and leave the typed slots zero. Mixed
// shapes are not supported — e.g. an Object Field's Str slot is
// ignored by the renderer.
//
// Construction note: callers can leave Kind == KindUnknown for a
// leaf with no specific type; formatField falls back to Str so a
// malformed field still shows something the operator can act on.
type Field struct {
	Name     string
	Kind     KindE
	Str      string
	Int      int64
	Uint     uint64
	Float    float64
	Bool     bool
	Bytes    []byte
	Time     time.Time
	Children []Field
}

// IsContainer reports whether this Field's value lives in Children
// rather than the typed slots. Used by the Renderer to pick between
// the leaf two-line layout and the CollapsingHeader-wrapped tree.
func (inst Field) IsContainer() (ok bool) {
	ok = inst.Kind == KindObject || inst.Kind == KindArray
	return
}
