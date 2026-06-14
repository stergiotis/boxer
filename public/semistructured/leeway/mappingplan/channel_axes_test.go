package mappingplan

import "testing"

// TestChannelAxes_Spot pins a few channels' axis triples directly, so a
// mis-edit of the table is caught even if it stays internally consistent.
// (The table's internal consistency is enforced at package init via
// validateChannelTable.)
func TestChannelAxes_Spot(t *testing.T) {
	cases := []struct {
		ch     MembershipChannel
		card   ChannelCardinalityE
		ident  ChannelIdentityE
		params bool
	}{
		{MembershipChannelLowCardRef, ChannelCardinalityLow, ChannelIdentityRef, false},
		{MembershipChannelLowCardVerbatim, ChannelCardinalityLow, ChannelIdentityVerbatim, false},
		{MembershipChannelHighCardRef, ChannelCardinalityHigh, ChannelIdentityRef, false},
		{MembershipChannelHighCardVerbatim, ChannelCardinalityHigh, ChannelIdentityVerbatim, false},
		{MembershipChannelMixedLowCardRef, ChannelCardinalityLow, ChannelIdentityPerRowId, true},
		{MembershipChannelMixedLowCardVerbatim, ChannelCardinalityLow, ChannelIdentityPerRowName, true},
		{MembershipChannelLowCardRefParametrized, ChannelCardinalityLow, ChannelIdentityPerRowBlob, true},
		{MembershipChannelHighCardRefParametrized, ChannelCardinalityHigh, ChannelIdentityPerRowBlob, true},
	}
	for _, tc := range cases {
		if tc.ch.Cardinality() != tc.card || tc.ch.Identity() != tc.ident || tc.ch.HasParams() != tc.params {
			t.Errorf("%s: got (card=%d, ident=%d, params=%v), want (card=%d, ident=%d, params=%v)",
				tc.ch.String(), tc.ch.Cardinality(), tc.ch.Identity(), tc.ch.HasParams(), tc.card, tc.ident, tc.params)
		}
	}
}

// TestValidateChannelDescriptor_CatchesBadRows pins the 2026-06-14 review
// hardening: validateChannelDescriptor (run over every row at package init)
// now also guards the runtime-coupling fields, so a new/mis-edited channel
// row that forgets or typos a method suffix / carrier type fails at load
// rather than silently naming a nonexistent method. The real table passing
// is enforced by init(); these cases prove each guard actually fires.
func TestValidateChannelDescriptor_CatchesBadRows(t *testing.T) {
	// A well-formed mixed-carrier (PerRowId) descriptor as the baseline.
	good := channelDescriptor{
		addMethodSuffix:   "MixedLowCardRef",
		readIterElemType:  "[]byte",
		carrierType:       "MixedLowCardRef",
		carrierReadSuffix: "LowCardRefHighCardParams",
		carrierSeq2Types:  "uint64, []byte",
		usesCarrier:       true,
		hasParams:         true,
		identity:          ChannelIdentityPerRowId,
	}
	if err := validateChannelDescriptor("baseline", good); err != nil {
		t.Fatalf("baseline descriptor must validate: %v", err)
	}

	bad := []struct {
		name string
		mut  func(d *channelDescriptor)
	}{
		{"empty addMethodSuffix", func(d *channelDescriptor) { d.addMethodSuffix = "" }},
		{"empty readIterElemType", func(d *channelDescriptor) { d.readIterElemType = "" }},
		{"carrier missing carrierType", func(d *channelDescriptor) { d.carrierType = "" }},
		{"carrier missing carrierReadSuffix", func(d *channelDescriptor) { d.carrierReadSuffix = "" }},
		{"mixed missing carrierSeq2Types", func(d *channelDescriptor) { d.carrierSeq2Types = "" }},
		{"axis/behaviour drift (needsKindVar)", func(d *channelDescriptor) { d.needsKindVar = true }},
		{"carrierType on non-carrier", func(d *channelDescriptor) {
			d.usesCarrier, d.hasParams, d.identity = false, false, ChannelIdentityRef
			d.carrierReadSuffix, d.carrierSeq2Types, d.needsKindVar = "", "", true
			// leaves carrierType set on a now-non-carrier row
		}},
	}
	for _, tc := range bad {
		d := good
		tc.mut(&d)
		if err := validateChannelDescriptor(tc.name, d); err == nil {
			t.Errorf("%s: expected a validation error, got nil", tc.name)
		}
	}
}
