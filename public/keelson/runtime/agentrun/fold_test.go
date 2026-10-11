package agentrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fold settles each unfinished act by its kind and effect class
// (ADR-0308 §SD4): a request without a reply is remade, a query is told
// not completed, a command is unknown, a consequential one is listed, a
// question is asked again, a grant lapses, an applied command whose
// completion never came is pending.
func TestTheFoldSettlesByEffectClass(t *testing.T) {
	entries := []Entry{
		{Ordinal: 1, Kind: KindUser, Turn: "t1", Label: Label{Level: LevelPerson}},
		{Ordinal: 2, Kind: KindRequest, Key: "llm-1", Turn: "t1"},
		{Ordinal: 3, Kind: KindReply, Key: "llm-1", Turn: "t1", Label: Label{Origin: []string{"remote"}}},
		{Ordinal: 4, Kind: KindIntent, Key: "llm-1#0", Effect: EffectNone, Turn: "t1"},
		{Ordinal: 5, Kind: KindOutcome, Key: "llm-1#0", Turn: "t1", Label: Label{Level: LevelExternal, Tier: TierAmber}},
		{Ordinal: 6, Kind: KindIntent, Key: "llm-1#1", Effect: EffectView, Turn: "t1"},
		{Ordinal: 7, Kind: KindIntent, Key: "llm-1#2", Effect: EffectDocument, Turn: "t1"},
		{Ordinal: 8, Kind: KindIntent, Key: "llm-1#3", Effect: EffectConsequential, Turn: "t1"},
		{Ordinal: 9, Kind: KindIntent, Key: "llm-1#4", Effect: EffectRun, Turn: "t1"},
		{Ordinal: 10, Kind: KindOutcome, Key: "llm-1#4", Form: FormPending, Turn: "t1"},
		{Ordinal: 11, Kind: KindAsked, Key: "q1", Turn: "t1"},
		{Ordinal: 12, Kind: KindAsked, Key: "g1", Form: FormGrant, Turn: "t1"},
		{Ordinal: 13, Kind: KindAsked, Key: "q0", Turn: "t1"},
		{Ordinal: 14, Kind: KindAnswered, Key: "q0", Turn: "t1"},
		{Ordinal: 15, Kind: KindRequest, Key: "llm-2", Turn: "t1"},
	}
	r := Fold(entries)
	var got []string
	for _, u := range r.Unsettled {
		got = append(got, u.Entry.Key+":"+u.Resolution.String())
	}
	assert.Equal(t, []string{"llm-1#1:not-completed", "llm-1#2:unknown", "llm-1#3:list-to-person", "llm-1#4:pending", "q1:ask-again", "g1:lapsed", "llm-2:remake"}, got)
	require.NotNil(t, r.LastRequest)
	assert.Equal(t, uint32(15), r.LastRequest.Ordinal)
	assert.Equal(t, Label{Level: LevelExternal, Tier: TierAmber, Origin: []string{"remote"}}, r.Label)
	assert.Equal(t, "t1", r.OpenTurn)
	assert.False(t, r.Deleted)

	closed := Fold(append(entries, Entry{Ordinal: 16, Kind: KindTurn, Turn: "t1"}, Entry{Ordinal: 17, Kind: KindDeleted}))
	assert.Equal(t, 1, closed.Turns)
	assert.Empty(t, closed.OpenTurn)
	assert.True(t, closed.Deleted)
	assert.Empty(t, closed.Unsettled, "a turn entry settles its turn's acts for good")
	assert.Empty(t, Fold(nil).Unsettled)
}

// Only the open turn's acts are unsettled: a request that failed in a
// turn since closed is not remade on every later resume, while one in
// the turn left open is.
func TestAClosedTurnLeavesNothingUnsettled(t *testing.T) {
	entries := []Entry{
		{Ordinal: 1, Kind: KindUser, Turn: "t1"},
		{Ordinal: 2, Kind: KindRequest, Key: "llm-1", Turn: "t1"},
		{Ordinal: 3, Kind: KindTurn, Turn: "t1", Body: "failed"},
		{Ordinal: 4, Kind: KindUser, Turn: "t2"},
		{Ordinal: 5, Kind: KindRequest, Key: "llm-2", Turn: "t2"},
	}
	r := Fold(entries)
	require.Len(t, r.Unsettled, 1)
	assert.Equal(t, "llm-2", r.Unsettled[0].Entry.Key)
	assert.Equal(t, ResolutionRemake, r.Unsettled[0].Resolution)
	assert.Equal(t, "t2", r.OpenTurn)
	assert.Empty(t, Fold(entries[:3]).Unsettled, "the failed request of the closed turn is not reported")
}
