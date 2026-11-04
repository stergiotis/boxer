package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
)

// A call a model asked for names the intent written before it (ADR-0358
// §SD7); each way the entry can fail to cover the call says so.
func TestAnIntentCoversOnlyItsOwnCall(t *testing.T) {
	ctx := context.Background()
	store := agentrun.NewMemStore()
	w, err := agentrun.Open(ctx, store, agentrun.Run{Conversation: "c", OwnerApp: "harness", Coordinator: "w"})
	require.NoError(t, err)
	written, err := w.Append(ctx,
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "call-1", Name: "query"},
		agentrun.Entry{Kind: agentrun.KindNote, Name: "n", Body: "b"})
	require.NoError(t, err)
	intent := cell.ApprovalKey(w.Run().Id, written[0].Ordinal)
	note := cell.ApprovalKey(w.Run().Id, written[1].Ordinal)
	check := StoreIntents(store)

	require.NoError(t, check.CheckIntent(ctx, "harness", intent, "call-1"))
	require.NoError(t, check.CheckIntent(ctx, "harness", intent, "call-1/a3"), "a call the tool makes on its way")
	for _, tc := range []struct {
		name, sender, key, callKey, why string
	}{
		{"no key", "harness", "", "call-1", "names no intent"},
		{"not a key", "harness", "nonsense", "call-1", "not <run>#<ordinal>"},
		{"another app's run", "other", intent, "call-1", "no such run"},
		{"another call", "harness", intent, "call-2", "another call"},
		{"a look-alike key", "harness", intent, "call-10", "another call"},
		{"another kind", "harness", note, "call-1", "not an intent"},
		{"no such entry", "harness", cell.ApprovalKey(w.Run().Id, 99), "call-1", "no such entry"},
	} {
		err := check.CheckIntent(ctx, tc.sender, tc.key, tc.callKey)
		require.Error(t, err, tc.name)
		assert.Contains(t, err.Error(), tc.why, tc.name)
	}
	require.NoError(t, w.Close(ctx, agentrun.StateClosed))
	err = check.CheckIntent(ctx, "harness", intent, "call-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}

// The rule rests on the call stating a model call: a coordinator driven by
// hand runs no model and keeps no journal, and its calls stay legal.
func TestOnlyACallAModelAskedForIsChecked(t *testing.T) {
	bare := &Service{}
	assert.Empty(t, bare.verifyIntent("console", wireCall{Key: "k"}), "by hand")
	assert.Contains(t, bare.verifyIntent("harness", wireCall{Key: "k", ModelCall: "m"}), "no run journal", "nothing wired refuses")
}
