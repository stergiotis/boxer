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
// Only the carrier for the implemented channel lives here. The deferred
// channels' carriers (Parametrized{Params []byte} for the parametrized
// channels, MixedLowCardVerbatim{Name, Params []byte} for mixed-verbatim)
// are specified in ADR-0008's Cut-2 update and added when each channel
// lands, so the package never carries a carrier no codec path references.
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
