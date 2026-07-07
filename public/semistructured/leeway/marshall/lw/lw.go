// Package lw declares the marker types for the leeway nested-struct marshalling
// front-end (doc/howto/leeway-marshalling-nested.md). A nested attribute struct
// carries its sub-column and membership roles by field TYPE; these markers are
// those types. They are pure data — no methods, no imports beyond the standard
// library — so a DTO can embed them anywhere.
//
// Slice-A Step 4 defines the value-shape markers: Single (the ,unit /
// BeginAttributeSingle shape) and the canonical lane types (the ,ct= relabels).
// The membership channel markers (Ref / Verbatim / carriers) arrive with the
// dynamic-membership step.
package lw

// Single is a value-shape marker for a container (array / set) sub-column that
// carries exactly ONE element per attribute, supplied inline as T — the ,unit /
// BeginAttributeSingle shape. At the entity level it is sugar for a flat `,unit`
// field: `Battery lw.Single[uint64]` ≡ `Battery uint64 `+"`lw:\"…,unit\"`"+`.
type Single[T any] struct{ Val T }

// One is the terse constructor for a Single[T].
func One[T any](v T) Single[T] { return Single[T]{Val: v} }

// The lane types relabel a field's canonical over the SAME bytes (the ,ct=
// override) so a DTO field stays tag-free while Plan-consuming tooling sees the
// richer network type. The Go byte shape is fixed by the type definition, so no
// reshape is possible.
type (
	IPv4 [4]byte  // canonical "v" — an IPv4 address as 4 bytes
	IPv6 [16]byte // canonical "w" — an IPv6 address as 16 bytes
)
