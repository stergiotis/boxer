//go:build llm_generated_opus47

// Package fieldview renders a hierarchical list of typed key-value
// pairs as a compact two-line-per-leaf inspector: name + kind tag on
// one line, value (wrapping, monospace) below. Container kinds
// (Object, Array) wrap their Children in a CollapsingHeader so deep
// trees stay collapsible.
//
// Originally lifted out of the logviewer's detail-pane fields
// section, so any caller that holds a slice of typed fields — log
// rows, card inspectors, debug dialogs — can render them with the
// same look without re-implementing the per-kind formatting and the
// wrap discipline that bounds horizontal width.
//
// Usage:
//
//	r := fieldview.New(ids, "card-fld").BytesMax(128)
//	r.Render(fields)
//
// The Renderer is a value (not a pointer); fluent setters return a
// modified copy so configurations are safe to share across instances.
// All widget IDs are derived from the caller-supplied WidgetIdStack
// under the per-Renderer idPrefix, so two viewers on the same id
// stack don't collide.
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
