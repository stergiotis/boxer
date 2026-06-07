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
		{MembershipChannelMixedLowCardRef, ChannelCardinalityLow, ChannelIdentityPerRow, true},
		{MembershipChannelMixedLowCardVerbatim, ChannelCardinalityLow, ChannelIdentityPerRow, true},
		{MembershipChannelLowCardRefParametrized, ChannelCardinalityLow, ChannelIdentityPerRow, true},
		{MembershipChannelHighCardRefParametrized, ChannelCardinalityHigh, ChannelIdentityPerRow, true},
	}
	for _, tc := range cases {
		if tc.ch.Cardinality() != tc.card || tc.ch.Identity() != tc.ident || tc.ch.HasParams() != tc.params {
			t.Errorf("%s: got (card=%d, ident=%d, params=%v), want (card=%d, ident=%d, params=%v)",
				tc.ch.String(), tc.ch.Cardinality(), tc.ch.Identity(), tc.ch.HasParams(), tc.card, tc.ident, tc.params)
		}
	}
}
