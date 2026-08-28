package runtime

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

// ErrUnknownChannel is a membership channel outside the eight
// mappingplan.MembershipChannel values — an ordinal this build has no identity
// encoding for, so neither side can say what the identity payload's shape is.
var ErrUnknownChannel = errors.New("unknown membership channel")

// WriteMembership writes one membership item, `[channel:uint, identity]`
// (ADR-0210 SD4).
//
// The first element is the mappingplan.MembershipChannel ordinal; the second is
// the ADR-0201 SD5 identity payload for the channel's IdentityEncoding:
//
//	Ref         uint             -> [ch, ref]
//	Verbatim    bytes            -> [ch, verbatim]
//	PerRowId    [uint, bytes]    -> [ch, [ref, params]]
//	PerRowName  [bytes, bytes]   -> [ch, [verbatim, params]]
//	PerRowBlob  [bytes]          -> [ch, [params]]
//
// The carrier shapes nest inside the second element rather than flattening
// into the outer array, so the item is always two elements and the channel is
// always at position 0.
//
// The channel travels because the wire is lossless: cardinality is carriage,
// not content, but a form that drops it cannot restore the section's
// AddMembership<Channel>P call on the way back in. Which arguments an
// unrelated channel ignores is the caller's business — a Ref channel does not
// read verbatim or params, and passing them is not an error.
//
// A channel outside the eight sets the sticky error and writes nothing; an
// out-of-range ordinal reports membership.IdentityNone, which no real channel
// carries.
func (c *CborWriter) WriteMembership(ch mappingplan.MembershipChannel, ref uint64, verbatim []byte, params []byte) {
	if c.err != nil {
		return
	}
	identity := ch.Identity()
	switch identity {
	case membership.IdentityRef:
		c.ArrayHead(2)
		c.WriteUint(uint64(ch))
		c.WriteUint(ref)
	case membership.IdentityVerbatim:
		c.ArrayHead(2)
		c.WriteUint(uint64(ch))
		c.WriteBytes(verbatim)
	case membership.IdentityPerRowId:
		c.ArrayHead(2)
		c.WriteUint(uint64(ch))
		c.ArrayHead(2)
		c.WriteUint(ref)
		c.WriteBytes(params)
	case membership.IdentityPerRowName:
		c.ArrayHead(2)
		c.WriteUint(uint64(ch))
		c.ArrayHead(2)
		c.WriteBytes(verbatim)
		c.WriteBytes(params)
	case membership.IdentityPerRowBlob:
		c.ArrayHead(2)
		c.WriteUint(uint64(ch))
		c.ArrayHead(1)
		c.WriteBytes(params)
	default:
		c.failValue(eb.Build().Uint8("channel", uint8(ch)).Stringer("identity", identity).Errorf("unable to write the membership: %w", ErrUnknownChannel))
	}
}
