package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMembershipSpecE_Count(t *testing.T) {
	check := func(n int, m MembershipSpecE) {
		require.EqualValues(t, n, m.Count())
		if m != MembershipSpecNone {
			require.Len(t, strings.Split(m.String(), " | "), n)
		}
	}
	check(0, MembershipSpecNone)
	check(1, MembershipSpecLowCardRef)
	check(2, MembershipSpecLowCardRef.AddHighCardVerbatim())
	check(1, MembershipSpecMixedLowCardVerbatimHighCardParameters)
	check(3, MembershipSpecLowCardRef.AddHighCardVerbatim().AddMixedLowCardRefHighCardParameters())
}
