package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// tupleClaim tells a tuple slot's attributes from other kinds' on a section
// they share (ADR-0326 §SD4), the rule the generated ReadRow applies: a
// dynamic tuple claims an attribute carrying any membership on one of its
// channels, a static nested section one carrying its membership. Detect and
// ReadComponent read by it; Unmarshal keeps ADR-0103's rule that a tuple
// owns its section.
type tupleClaim struct {
	channels   []mappingplan.MembershipChannel
	membership string
	channel    mappingplan.MembershipChannel
	id         uint64
	// all claims every attribute: a contract that does not say how to tell
	// the tuple's attributes apart (no channels, or an opaque per-row blob
	// identity).
	all bool
}

// slotNeedsID reports whether matching the slot's attributes needs its
// membership resolved to a registry id: a flat slot on a ref channel, or a
// static nested section whose identity is an id, per-row or not.
func slotNeedsID(s mappingplan.Slot) bool {
	if s.Membership == "" {
		return false
	}
	if s.OwnsSection {
		id := s.Channel.Identity()
		return id == mappingplan.ChannelIdentityRef || id == mappingplan.ChannelIdentityPerRowId
	}
	return !s.Channel.UsesCarrier() && s.Channel.NeedsKindVar()
}

// newTupleClaim builds the claim for a tuple slot; id is the slot's resolved
// membership id when slotNeedsID.
func newTupleClaim(s mappingplan.Slot, id uint64) (c tupleClaim) {
	if s.Membership == "" {
		c.channels = s.TupleChannels
		c.all = len(c.channels) == 0
		return
	}
	c.membership, c.channel, c.id = s.Membership, s.Channel, id
	c.all = s.Channel.Identity() == mappingplan.ChannelIdentityPerRowBlob
	return
}

// tupleClaims builds the claim of every tuple slot in the contract, keyed by
// section; ids are the slots' resolved membership ids (resolveSlotIDs).
func tupleClaims(c mappingplan.ReadContract, ids map[int]uint64) (out map[string]tupleClaim) {
	out = map[string]tupleClaim{}
	for si, s := range c.Slots {
		if s.OwnsSection {
			out[s.Section] = newTupleClaim(s, ids[si])
		}
	}
	return
}

// membershipKeys reads attribute attrJ's membership values on channel ch:
// the ids or names, without a mixed channel's parameters.
func membershipKeys(membs reflect.Value, ch mappingplan.MembershipChannel, i int, attrJ int64) []reflect.Value {
	if ch.UsesCarrier() {
		keys, _ := collectIterSeq2(mustCall(membs, "GetMembValue"+ch.CarrierReadMethodSuffix(), reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0])
		return keys
	}
	return collectIterSeq(mustCall(membs, "GetMembValue"+ch.AddMethodSuffix(), reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0])
}

// claims reports whether attribute attrJ of entity i is the tuple's.
func (inst tupleClaim) claims(membs reflect.Value, i int, attrJ int64) bool {
	if inst.all {
		return true
	}
	if inst.membership == "" {
		for _, ch := range inst.channels {
			if len(membershipKeys(membs, ch, i, attrJ)) > 0 {
				return true
			}
		}
		return false
	}
	byName := inst.channel.EmbedsLiteralName() || inst.channel.CarrierValueIsBytes()
	for _, v := range membershipKeys(membs, inst.channel, i, attrJ) {
		if byName && string(v.Bytes()) == inst.membership || !byName && v.Uint() == inst.id {
			return true
		}
	}
	return false
}

// count returns how many of entity i's attributes the tuple claims.
func (inst tupleClaim) count(attrs, membs reflect.Value, i int) (n int) {
	total := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
	for attrJ := range total {
		if inst.claims(membs, i, attrJ) {
			n++
		}
	}
	return
}
