package goplan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// Direct coverage of the lw: tag grammar (SplitLW). Both front-ends parse
// tags through this one function, so its acceptance / rejection contract is
// the single point that keeps the codegen and reflect grammars aligned.

func TestSplitLW_Accept(t *testing.T) {
	ch := func(c mappingplan.MembershipChannel) mappingplan.FieldFlags {
		return mappingplan.FieldFlags{Channel: c}
	}
	cases := []struct {
		tag       string
		memb, sec string
		col       string
		flags     mappingplan.FieldFlags
	}{
		{tag: "m", memb: "m"},
		{tag: "m,s", memb: "m", sec: "s"},
		{tag: "m,s:col", memb: "m", sec: "s", col: "col"},
		{tag: ",s", sec: "s"},                 // empty membership tolerated (plain column)
		{tag: " m , s ", memb: "m", sec: "s"}, // whitespace trimmed
		{tag: "m,s,unit", memb: "m", sec: "s", flags: mappingplan.FieldFlags{Unit: true}},
		{tag: "m,s,verbatim", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelLowCardVerbatim)},
		{tag: "m,s,lowCardVerbatim", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelLowCardVerbatim)},
		{tag: "m,s,highCardRef", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelHighCardRef)},
		{tag: "m,s,highCardVerbatim", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelHighCardVerbatim)},
		{tag: "m,s,mixedLowCardRef", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelMixedLowCardRef)},
		{tag: "m,s,mixedLowCardVerbatim", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelMixedLowCardVerbatim)},
		{tag: "m,s,lowCardRefParametrized", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelLowCardRefParametrized)},
		{tag: "m,s,highCardRefParametrized", memb: "m", sec: "s", flags: ch(mappingplan.MembershipChannelHighCardRefParametrized)},
		{tag: "m,s,const=foo", memb: "m", sec: "s", flags: mappingplan.FieldFlags{HasConst: true, ConstValue: "foo"}},
		{tag: "m,s:col,unit,highCardRef", memb: "m", sec: "s", col: "col", flags: mappingplan.FieldFlags{Unit: true, Channel: mappingplan.MembershipChannelHighCardRef}},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			got, err := goplan.SplitLW(tc.tag)
			require.NoError(t, err)
			require.Equal(t, tc.memb, got.Membership, "membership")
			require.Equal(t, tc.sec, got.Section, "section")
			require.Equal(t, tc.col, got.Column, "column")
			require.Equal(t, tc.flags, got.Flags, "flags")
		})
	}
}

func TestSplitLW_Reject(t *testing.T) {
	cases := []struct {
		name, tag, substr string
	}{
		{"unknown token", "m,s,bogus", "unknown flag token"},
		{"unit twice", "m,s,unit,unit", "declared twice"},
		{"explode removed (ADR-0113 D1)", "m,s,explode", "removed (ADR-0113 D1)"},
		{"two channels", "m,s,verbatim,highCardRef", "channel flag declared twice"},
		{"two channels b", "m,s,highCardRef,lowCardVerbatim", "channel flag declared twice"},
		{"const twice", "m,s,const=a,const=b", "declared twice"},
		{"unknown kv flag", "m,s,foo=bar", "unknown key=value flag"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := goplan.SplitLW(tc.tag)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.substr)
		})
	}
}
