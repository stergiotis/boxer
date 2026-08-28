package runtime

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

// ReadMembership reads one membership item, `[channel:uint, identity]` — the
// inverse of WriteMembership (ADR-0207 SD4).
//
// The channel ordinal decides the identity payload's shape, and the shape read
// is checked against the one the channel's IdentityEncoding declares, so a
// membership whose carrier does not match its channel is refused rather than
// silently returned with empty fields. The arguments a channel does not carry
// come back zero: a Ref channel yields no verbatim and no params.
//
// verbatim and params are views into the reader's buffer, valid until that
// buffer is reused; copy them to keep them.
func (c *CborReader) ReadMembership() (ch mappingplan.MembershipChannel, ref uint64, verbatim []byte, params []byte) {
	if n := c.ReadArrayHead(); c.err == nil && n != 2 {
		c.fail(eb.Build().Int("pos", c.pos).Int("elements", n).Errorf("a membership is a two-element array: %w", ErrOutOfRange))
	}
	if c.err != nil {
		return
	}
	ord := c.ReadUint()
	if c.err != nil {
		return
	}
	if ord > math.MaxUint8 {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("channel", ord).Errorf("channel ordinal is out of range: %w", ErrUnknownChannel))
		return
	}
	ch = mappingplan.MembershipChannel(ord)
	identity := ch.Identity()
	switch identity {
	case membership.IdentityRef:
		ref = c.ReadUint()
	case membership.IdentityVerbatim:
		verbatim = c.ReadBytes()
	case membership.IdentityPerRowId:
		c.expectIdentityArray(ch, 2)
		ref = c.ReadUint()
		params = c.ReadBytes()
	case membership.IdentityPerRowName:
		c.expectIdentityArray(ch, 2)
		verbatim = c.ReadBytes()
		params = c.ReadBytes()
	case membership.IdentityPerRowBlob:
		c.expectIdentityArray(ch, 1)
		params = c.ReadBytes()
	default:
		c.fail(eb.Build().Int("pos", c.pos).Uint64("channel", ord).Stringer("identity", identity).Errorf("unable to read the membership: %w", ErrUnknownChannel))
	}
	if c.err != nil {
		return 0, 0, nil, nil
	}
	return
}

// expectIdentityArray reads the head of a carrier channel's nested identity
// array and requires it to hold want elements.
func (c *CborReader) expectIdentityArray(ch mappingplan.MembershipChannel, want int) {
	n := c.ReadArrayHead()
	if c.err != nil {
		return
	}
	if n != want {
		c.fail(eb.Build().Int("pos", c.pos).Uint8("channel", uint8(ch)).Int("elements", n).Int("want", want).Errorf("membership identity is not the shape its channel declares: %w", ErrOutOfRange))
	}
}
