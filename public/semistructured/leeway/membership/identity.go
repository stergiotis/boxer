package membership

// IdentityEncoding is the shared identity-encoding vocabulary for a membership:
// how the identity value is carried on the wire, independent of dictionary
// cardinality. It is the single source for both the write-side channel's
// identity axis (mappingplan.ChannelIdentityE aliases this type) and the
// read-side value discriminator (MembershipValue.Encoding). Per ADR-0072 a
// channel is cardinality × identity × params, and the read-side value shape is
// "the channel minus cardinality" — so the five realized channels and the five
// driver-emitted value shapes are in bijection with the five non-None values
// here. Replacing the former parallel MembershipKindE / ChannelIdentityE
// enumerations with this one type retires that hand-maintained correspondence
// (ADR-0072 Open question 3).
type IdentityEncoding uint8

const (
	// IdentityNone is the zero value: "no membership". Channels never carry it;
	// a zero-valued MembershipValue reads as empty (see IsPlaceholder).
	IdentityNone IdentityEncoding = iota
	// IdentityRef resolves a registry uint64 id — the MembershipValue.Ref field.
	IdentityRef
	// IdentityVerbatim embeds the literal lw: name inline — the Verbatim field.
	IdentityVerbatim
	// IdentityPerRowId carries a per-row uint64 id alongside params (the
	// mixed-ref carrier: Ref + Params).
	IdentityPerRowId
	// IdentityPerRowName carries a per-row []byte name alongside params (the
	// mixed-verbatim carrier: Verbatim + Params).
	IdentityPerRowName
	// IdentityPerRowBlob carries an opaque per-row params blob (the parametrized
	// carrier: Params only — the ref is always 0).
	IdentityPerRowBlob
)

// HasParams reports whether this encoding carries a per-row params blob. True
// for the three PerRow encodings, false for Ref / Verbatim / None.
func (e IdentityEncoding) HasParams() bool {
	switch e {
	case IdentityPerRowId, IdentityPerRowName, IdentityPerRowBlob:
		return true
	}
	return false
}

// String returns a short stable label for the encoding.
func (e IdentityEncoding) String() string {
	switch e {
	case IdentityNone:
		return "none"
	case IdentityRef:
		return "ref"
	case IdentityVerbatim:
		return "verbatim"
	case IdentityPerRowId:
		return "perRowId"
	case IdentityPerRowName:
		return "perRowName"
	case IdentityPerRowBlob:
		return "perRowBlob"
	}
	return "unknown"
}
