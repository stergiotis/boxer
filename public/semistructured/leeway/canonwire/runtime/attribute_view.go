package runtime

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The table-free view a generated decoder hands to the pluggable dispatch of
// ADR-0210 SD5. It carries what the wire said about one attribute — its
// memberships, its values as raw CBOR items, and the optional discriminator —
// and nothing about either endpoint's table description, so a dispatcher
// written against it works for any table that declares the signature.

// MembershipView is one membership of an attribute as the decoder read it
// (ADR-0210 SD4). The fields a channel does not carry are zero: a ref channel
// leaves Verbatim and Params nil.
//
// Verbatim and Params are views into the buffer handed to the decoder; they
// are valid for as long as that buffer is, which for one DecodeEntity call is
// the whole call. A dispatcher that keeps them past its own return must copy.
type MembershipView struct {
	Verbatim []byte
	Params   []byte
	Ref      uint64
	Channel  mappingplan.MembershipChannel
}

// AttributeView is one attribute as the decoder read it, with nothing of the
// table in it.
//
// Groups holds one membership list per membership group — a slot of k
// co-sections carries k groups in signature order, a standalone section one.
// Values holds one entry per value column of the slot's signature, in key
// order, as the column's raw CBOR item; a dispatcher that needs a decoded
// value reads it with a CborReader over that slice rather than being handed a
// value model it would have to agree with.
//
// The slices and the byte views inside them are the decoder's, reused across
// attributes: a dispatcher may read them and must not retain them.
type AttributeView struct {
	Groups           [][]MembershipView
	Values           [][]byte
	Discriminator    uint64
	HasDiscriminator bool
}
