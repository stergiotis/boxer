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
	IPv4 uint32   // canonical "v" — an IPv4 address as a big-endian uint32 (the ClickHouse IPv4 Arrow type; fill from a netip.Addr via binary.BigEndian.Uint32(addr.As4()))
	IPv6 [16]byte // canonical "w" — an IPv6 address as 16 bytes

	// The CIDR lanes carry a per-value prefix packed as the address bytes
	// followed by one trailing prefix-length byte — the layout documented on
	// canonicaltypes.NetworkTypeAstNode.ByteWidth and decoded by the read side's
	// GetAttrValue<Col>Prefix. Fill them from a netip.Prefix as
	// {addr.As4()/As16()…, byte(p.Bits())}.
	IPv4Prefix [5]byte  // canonical "vc" — an IPv4 CIDR: 4 address bytes + 1 prefix-length byte
	IPv6Prefix [17]byte // canonical "wc" — an IPv6 CIDR: 16 address bytes + 1 prefix-length byte
)

// The membership channel markers make a nested attribute struct's field a
// per-attribute MEMBERSHIP rather than a value sub-column — the type-safe form
// of the tuple `@membership` tag. The field's TYPE fixes the channel; its value
// is the per-row identity, carried directly on the wire (ADR-0109): a ref id as
// uint64, a verbatim name as the literal string. A `[]Ref` field is a repeated
// membership (one attribute, many memberships); several marker fields are
// several memberships, possibly on heterogeneous channels.
type (
	Ref          uint64 // low-card ref  — a membership id, carried directly
	HighRef      uint64 // high-card ref
	Verbatim     string // low-card verbatim — the literal membership name
	HighVerbatim string // high-card verbatim
)
