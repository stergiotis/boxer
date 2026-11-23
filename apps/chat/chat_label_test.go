package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
)

// labelRig is a coordinator over a journal holding entries.
func labelRig(t *testing.T, entries ...agentrun.Entry) (coord *coordinator, w *agentrun.Writer) {
	t.Helper()
	ctx := context.Background()
	w, err := agentrun.Open(ctx, agentrun.NewMemStore(), agentrun.Run{})
	require.NoError(t, err)
	if len(entries) > 0 {
		_, err = w.Append(ctx, entries...)
		require.NoError(t, err)
	}
	coord = &coordinator{confinedTier: agentrun.TierAmber, journal: w}
	return
}

// A conversation that read nothing but the person's words is CLEAR, with
// nothing to name.
func TestTheLabelOfAPlainConversationIsClear(t *testing.T) {
	coord, _ := labelRig(t,
		agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "hello", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "r1", Label: coord0ReplyLabel()})
	f := coord.labelFacts()
	assert.Equal(t, agentrun.TierClear, f.tier)
	assert.False(t, f.confined)
	assert.False(t, f.untrusted, "a model's own reply is not untrusted content")
	assert.Empty(t, f.tierBy)
	assert.Empty(t, f.untrustedBy)
	assert.Equal(t, "CLEAR: lets it be shared with anyone.", labelTierLine(f.tier))
	assert.Equal(t, labelNoUntrusted, labelUntrustedLine(f))
	assert.Equal(t, []string{labelNotSent}, labelSentLines(f))
}

func coord0ReplyLabel() agentrun.Label { return (&coordinator{}).replyLabel(harnessLabel()) }

// The inspector names the read that raised the tier and a read that brought
// untrusted content, each by its operation and window.
func TestTheLabelNamesWhatRaisedIt(t *testing.T) {
	coord, w := labelRig(t,
		agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "count", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "k1", Name: "list_windows", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "k1", Name: "c1", Role: projection.RoleTool, Label: outcomeLabel(true, false, agentrun.TierAmber)},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "k2", Name: "call_operation", Arguments: `{"window":2,"operation":"sample_rows","args":{}}`, Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "k2", Name: "c2", Role: projection.RoleTool, Label: outcomeLabel(false, true, agentrun.TierAmber)},
		agentrun.Entry{Kind: agentrun.KindRequest, Key: "q1", Label: agentrun.Label{Level: agentrun.LevelExternal, Tier: agentrun.TierAmber}.WithExposure("api.example.com")})
	coord.tainted = true
	f := coord.labelFacts()
	assert.Equal(t, agentrun.TierAmber, f.tier)
	assert.True(t, f.confined, "the tier reached the one the host confines")
	assert.Contains(t, f.tierBy, "sample_rows in window 2")
	assert.Contains(t, f.untrustedBy, "list_windows")
	assert.Contains(t, labelUntrustedLine(f), "from list_windows")
	assert.Equal(t, []string{"api.example.com"}, f.sentTo)
	assert.Equal(t, []string{"Sent to: api.example.com."}, labelSentLines(f))

	// The causes are read again when the journal grew, not on every frame.
	seen := coord.labelSeen
	assert.Equal(t, w.Len(), seen.n)
	_, err := w.Append(context.Background(), agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "more", Label: harnessLabel()})
	require.NoError(t, err)
	_ = coord.labelFacts()
	assert.Equal(t, w.Len(), coord.labelSeen.n)
}

// An image the person sent raises the label without a tool call.
func TestTheLabelNamesAnImageThePersonSent(t *testing.T) {
	coord, _ := labelRig(t,
		agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Form: formSent, Body: "see", Label: agentrun.Label{Level: agentrun.LevelExternal, Tier: agentrun.TierAmber}})
	coord.tainted = true
	f := coord.labelFacts()
	assert.Contains(t, f.tierBy, "an image you sent")
	assert.Contains(t, f.untrustedBy, "an image you sent")
}

// Without a journal the flags stand, and a confined conversation reads at
// the confined tier.
func TestTheLabelWithoutAJournalIsTheFlags(t *testing.T) {
	coord := &coordinator{confinedTier: agentrun.TierAmber, tainted: true, confined: true}
	f := coord.labelFacts()
	assert.Equal(t, agentrun.TierAmber, f.tier)
	assert.True(t, f.confined)
	assert.True(t, f.untrusted)
	assert.Empty(t, f.tierBy)
	assert.Contains(t, labelUntrustedLine(f), "Read untrusted content:")
}

// The export's sentence and the inspector's line share one wording per tier.
func TestTierWordsAreOneTable(t *testing.T) {
	assert.Equal(t, "Its label lets it be shared with anyone.", exportTierWords(agentrun.TierClear))
	assert.Equal(t, "Its label keeps it to your organisation and those it works with, as they need it.", exportTierWords(agentrun.TierAmber))
	assert.Equal(t, "AMBER+STRICT: keeps it to your organisation.", labelTierLine(agentrun.TierAmberStrict))
	assert.Contains(t, exportTierWords(agentrun.TierE(9)), "not one the chat knows")
	assert.Contains(t, labelTierLine(agentrun.TierE(9)), "not a tier the chat knows")
}
