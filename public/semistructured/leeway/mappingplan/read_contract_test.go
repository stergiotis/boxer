package mappingplan

// Verdict is the part of the read contract with no Plan in it: given per-slot
// attribute counts, which PresenceE follows. Tested directly so the mapping is
// pinned independently of any DTO's shape.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlotAdmits(t *testing.T) {
	mandatory := Slot{MinAttrs: 1, MaxAttrs: 1}
	optional := Slot{MinAttrs: 0, MaxAttrs: 1}
	unbounded := Slot{MinAttrs: 0, MaxAttrs: ArityUnbounded}

	require.False(t, mandatory.Admits(0))
	require.True(t, mandatory.Admits(1))
	require.False(t, mandatory.Admits(2))

	require.True(t, optional.Admits(0))
	require.True(t, optional.Admits(1))
	require.False(t, optional.Admits(2))

	require.True(t, unbounded.Admits(0))
	require.True(t, unbounded.Admits(1_000))

	require.True(t, mandatory.Required())
	require.False(t, optional.Required())
}

func TestVerdict(t *testing.T) {
	mandatory := Slot{Section: "s", Membership: "a", MinAttrs: 1, MaxAttrs: 1}
	optional := Slot{Section: "s", Membership: "b", MinAttrs: 0, MaxAttrs: 1}

	cases := []struct {
		name   string
		slots  []Slot
		counts map[int]int
		want   PresenceE
	}{
		{
			name:   "nothing populated is absent",
			slots:  []Slot{optional},
			counts: map[int]int{0: 0},
			want:   PresenceAbsent,
		},
		{
			name:   "an optional-only kind with nothing populated is absent, not vacuously exact",
			slots:  []Slot{optional, optional},
			counts: map[int]int{},
			want:   PresenceAbsent,
		},
		{
			name:   "a missing required slot is absent even when something else is populated",
			slots:  []Slot{mandatory, optional},
			counts: map[int]int{0: 0, 1: 1},
			want:   PresenceAbsent,
		},
		{
			name:   "presence holds and arity holds is exact",
			slots:  []Slot{mandatory, optional},
			counts: map[int]int{0: 1, 1: 1},
			want:   PresenceExact,
		},
		{
			name:   "an optional slot left empty is still exact",
			slots:  []Slot{mandatory, optional},
			counts: map[int]int{0: 1, 1: 0},
			want:   PresenceExact,
		},
		{
			name:   "a slot carrying too many attributes is approximate",
			slots:  []Slot{mandatory},
			counts: map[int]int{0: 2},
			want:   PresenceApproximate,
		},
		{
			name:   "an over-full optional slot is approximate, not absent",
			slots:  []Slot{optional},
			counts: map[int]int{0: 3},
			want:   PresenceApproximate,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ReadContract{Kind: "k", Slots: tc.slots}
			require.Equal(t, tc.want, c.Verdict(tc.counts))
		})
	}
}

// The one-sided guarantee ADR-0075 states and ADR-0146 keeps: Exact implies the
// approximate check would also have passed. Verdict is total over counts, so
// this is checkable by construction rather than by example.
func TestExactImpliesPresence(t *testing.T) {
	slots := []Slot{
		{Section: "s", Membership: "a", MinAttrs: 1, MaxAttrs: 1},
		{Section: "s", Membership: "b", MinAttrs: 0, MaxAttrs: 1},
		{Section: "t", MinAttrs: 0, MaxAttrs: ArityUnbounded, OwnsSection: true},
	}
	c := ReadContract{Kind: "k", Slots: slots}
	for a := 0; a <= 3; a++ {
		for b := 0; b <= 3; b++ {
			for d := 0; d <= 3; d++ {
				counts := map[int]int{0: a, 1: b, 2: d}
				if c.Verdict(counts) != PresenceExact {
					continue
				}
				// Exact ⇒ every required slot populated, every slot in arity.
				for i, s := range slots {
					require.True(t, s.Admits(counts[i]))
					if s.Required() {
						require.Positive(t, counts[i])
					}
				}
			}
		}
	}
}

func TestPresenceString(t *testing.T) {
	require.Equal(t, "absent", PresenceAbsent.String())
	require.Equal(t, "approximate", PresenceApproximate.String())
	require.Equal(t, "exact", PresenceExact.String())
}

func TestDeriveReadContractRejectsNilPlan(t *testing.T) {
	_, err := DeriveReadContract(nil)
	require.Error(t, err)
}
