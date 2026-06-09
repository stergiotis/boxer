// Package marshalltypes holds the plain carrier structs for the leeway
// codec's Cut-2 parametrized / mixed membership channels (ADR-0008 D3).
//
// A carrier rides alongside a value field in a DTO, sharing the same
// (membership, section, channel) lw: triple. For these channels the
// membership identity is per-row carrier data — id / name / params read
// from the row — rather than a registry-resolved kindXxx. The codec
// front-ends (marshallgen via go/ast, marshallreflect via reflect)
// recognise a carrier by its Go type name + field shape, so the types are
// deliberately method-free plain data.
//
// All four Cut-2 channels are implemented; this package carries one struct
// per channel family — the mixed pair (value + params) and the parametrized
// pair (params only). See ADR-0008's Cut-2 update.
package marshalltypes

// MixedLowCardRef carries the (id, params) pair for the mixedLowCardRef
// channel. The codec emits AddMembershipMixedLowCardRefP(Id, Params) once
// per row, both fields taken from the carrier; there is no membership-id
// lookup. Params is wire-emitted even when empty (ADR-0008 SD8) — carrier
// presence, not params content, is the "attribute is here" signal.
type MixedLowCardRef struct {
	Id     uint64
	Params []byte
}

// MixedLowCardVerbatim carries the (name, params) pair for the
// mixedLowCardVerbatim channel: AddMembershipMixedLowCardVerbatimP(Name,
// Params) per row. Name is the verbatim membership label embedded literally
// on the wire ([]byte), distinct from MixedLowCardRef's uint64 Id. Same SD8
// semantics — Params is wire-emitted even when empty.
type MixedLowCardVerbatim struct {
	Name   []byte
	Params []byte
}

// Parametrized carries the opaque membership blob for the
// lowCardRefParametrized / highCardRefParametrized channels: the whole
// membership identity is the Params bytes (no separate id or name). The
// codec emits AddMembership<X>ParametrizedP(Params) per row and reads it
// back from the single-value GetMembValue<X>Parametrized Seq. Same channel
// flag selects low- vs high-card; the carrier struct is shared.
type Parametrized struct {
	Params []byte
}
