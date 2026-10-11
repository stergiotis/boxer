package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/anchorstore"
)

// Every turn ends with a chain head shipped to the anchor store and
// journaled (ADR-0314 §SD3), and the heads verify against the record (law
// L32); the request entries carry their digests and labels, the replies
// the model's level, and the chain verifies end to end (L31).
func TestATurnShipsItsChainHead(t *testing.T) {
	store := agentrun.NewMemStore()
	anchors := anchorstore.NewMemStore()
	inst := appOver(t, &fakeModel{}, store)
	inst.coord.anchors = anchors
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	w := inst.coord.journalNow()
	require.NotNil(t, w)
	entries := w.Entries()
	var heads, requests, replies int
	for _, e := range entries {
		switch e.Kind {
		case agentrun.KindHead:
			heads++
		case agentrun.KindRequest:
			requests++
			assert.Contains(t, e.Body, `"digest":"`)
		case agentrun.KindReply:
			replies++
			assert.Equal(t, agentrun.LevelOwnModel, e.Label.Level)
			assert.Equal(t, w.Run().Id, e.WriterRun)
		}
	}
	assert.Equal(t, 2, heads, "one head per turn end")
	assert.GreaterOrEqual(t, requests, 2)
	assert.GreaterOrEqual(t, replies, 2)
	for i, e := range entries {
		if e.Kind == agentrun.KindHead {
			require.Greater(t, i, 0)
			assert.Equal(t, agentrun.KindTurn, entries[i-1].Kind, "the head covers the turn's end")
			assert.Equal(t, headFormLocal, e.Form, "a head the process alone holds is recorded as local")
			rec, perr := anchorstore.ParseHeadBody(e.Body)
			require.NoError(t, perr)
			assert.False(t, rec.Durable)
		}
	}
	anchored, err := anchors.Read(context.Background(), w.Run().Cell, w.Run().Id)
	require.NoError(t, err)
	require.Len(t, anchored, 2)
	assert.Empty(t, anchorstore.VerifyHeads(w.Run().Digest, entries, anchored), "L32 holds")
	assert.Empty(t, anchorstore.VerifyHeads(w.Run().Digest, durableHeads(entries), nil), "L32 reads past local heads once their store is gone")
	broken, err := agentrun.Verify(w.Run().Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "L31 holds")
	assert.NotEqual(t, agentrun.Label{}, w.Run().Label, "the run's label floats with what it read")

	// A new conversation closes the run with a last head.
	inst.newConversation()
	require.Eventually(t, func() bool {
		r, _, _ := store.GetRun(context.Background(), w.Run().Id)
		return r.State == agentrun.StateClosed
	}, 5e9, 5e6)
	anchored, _ = anchors.Read(context.Background(), w.Run().Cell, w.Run().Id)
	assert.Len(t, anchored, 3)
}
