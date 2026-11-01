package verdict

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Every state has a name, a label in the ADR's wording, a tone and a law.
func TestTheVocabularyMapsEveryState(t *testing.T) {
	want := map[StateE]struct {
		label string
		tone  ToneE
		law   string
	}{
		StateUnverified: {"unverified: no store", ToneNeutral, ""},
		StateHolds:      {"holds", ToneOk, LawSeals},
		StateBroken:     {"broken at ordinal 7: a seal's digest is not over its rows and its predecessor", ToneError, LawSeals},
		StateGap:        {"gap: 3 rows no seal names", ToneWarning, LawSeals},
		StateFork:       {"fork: two seals follow one", ToneError, LawSeals},
		StateUnanchored: {"unanchored since ordinal 9", ToneWarning, LawAnchors},
	}
	require.Len(t, want, len(AllStates))
	for _, st := range AllStates {
		l := Line{Run: "r", State: st, Ordinal: 7, Count: 3, Reason: "no store"}
		switch st {
		case StateBroken:
			l.Reason = BreakReason(evidence.SealBreak{Why: evidence.SealBreakDigest})
		case StateUnanchored:
			l.Ordinal = 9
		}
		w := want[st]
		assert.Equal(t, w.label, l.Label(), st.String())
		assert.Equal(t, w.tone, l.Tone(), st.String())
		assert.Equal(t, w.law, l.Law(), st.String())
		back, ok := ParseState(st.String())
		assert.True(t, ok)
		assert.Equal(t, st, back)
		assert.NotEmpty(t, l.Mark())
	}
	assert.Equal(t, "✓", Line{State: StateHolds}.Mark())
}

// Short names the state without the Reason, so a badge's width is bounded
// whatever a read put in the line (ADR-0350 §SD12); Label keeps it.
func TestShortCarriesNoReason(t *testing.T) {
	reason := strings.Repeat("r", 300)
	want := map[StateE]string{
		StateUnverified: "unverified",
		StateHolds:      "holds",
		StateBroken:     "broken at ordinal 7",
		StateGap:        "gap: 3 rows no seal names",
		StateFork:       "fork",
		StateUnanchored: "unanchored since ordinal 7",
	}
	for _, st := range AllStates {
		l := Line{Run: "r", State: st, Ordinal: 7, Count: 3, Reason: reason}
		assert.Equal(t, want[st], l.Short(), st.String())
		assert.NotContains(t, l.Short(), reason, st.String())
		assert.Less(t, len([]rune(l.Short())), 40, st.String())
	}
	assert.Equal(t, "unverified: "+reason, Line{State: StateUnverified, Reason: reason}.Label(), "Label keeps the reason")
	assert.Equal(t, "broken at ordinal 7: "+reason, Line{State: StateBroken, Ordinal: 7, Reason: reason}.Label())
}

func TestFromSealsOrdersTheStates(t *testing.T) {
	assert.Equal(t, StateHolds, FromSeals("r", evidence.SealBreak{}, nil, nil, nil).State)
	l := FromSeals("r", evidence.SealBreak{Seal: 4, Why: evidence.SealBreakOrder}, []uint64{1}, nil, nil)
	assert.Equal(t, StateFork, l.State, "a break wins over unnamed rows")
	l = FromSeals("r", evidence.SealBreak{Seal: 4, Why: evidence.SealBreakRowMissing, Row: 12}, nil, nil, nil)
	assert.Equal(t, "broken at ordinal 4: a row the seal names is not on the store (row 12)", l.Label())
	l = FromSeals("r", evidence.SealBreak{}, []uint64{3, 5}, []uint64{2}, nil)
	assert.Equal(t, StateGap, l.State)
	assert.Equal(t, 2, l.Count)
	l = FromSeals("r", evidence.SealBreak{}, nil, []uint64{2, 3}, nil)
	assert.Equal(t, "unanchored since ordinal 2", l.Label())
	l = FromSeals("r", evidence.SealBreak{}, nil, nil, errors.New("read seals: timeout"))
	assert.Equal(t, "unverified: read seals: timeout", l.Label())
}

// The rows keelson('evidence_checks') writes for the trail read back as
// the same states FromSeals gives.
func TestFromChecksReadsTheRelationsRows(t *testing.T) {
	durable := Check{Store: "trail", Scope: "r", Law: LawDurable, Holds: true}
	cases := []struct {
		name   string
		checks []Check
		want   string
		ok     bool
	}{
		{"holds", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Holds: true}, {Store: "trail", Scope: "r", Law: LawAnchors, Holds: true}}, "holds", true},
		{"fork", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Broken: 6, Why: "the stream breaks: order"}}, "fork: two seals follow one", true},
		{"broken", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Broken: 6, Why: "the stream breaks: row-digest at row 40"}},
			"broken at ordinal 6: a row's digest on the store is not the one the seal names (row 40)", true},
		{"gap", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Broken: 2, Why: "3 rows no seal names"}}, "gap: 3 rows no seal names", true},
		{"unanchored", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Holds: true},
			{Store: "trail", Scope: "r", Law: LawAnchors, Broken: 5, Why: "anchored seals break at 5,6"}}, "unanchored since ordinal 5", true},
		{"not durable", []Check{{Store: "trail", Scope: "r", Law: LawDurable, Why: "no ClickHouse"}}, "unverified: no ClickHouse", true},
		{"failed", []Check{durable, {Store: "trail", Scope: "r", Law: LawSeals, Why: "read seals: timeout"}}, "unverified: read seals: timeout", true},
		{"another run", []Check{{Store: "trail", Scope: "other", Law: LawSeals, Holds: true}}, "unverified", false},
	}
	for _, tc := range cases {
		l, ok := FromChecks("r", tc.checks)
		assert.Equal(t, tc.ok, ok, tc.name)
		assert.Equal(t, tc.want, l.Label(), tc.name)
	}
}
