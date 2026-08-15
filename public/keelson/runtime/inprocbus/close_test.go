package inprocbus

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// ADR-0188 §SD1: a client is the accumulator of one INSTANCE's bus effects.
// Closing it releases exactly its own subscriptions and grants — a sibling
// client under the same app id keeps everything.

func subCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth}}
	return
}

func TestClientClose_ReleasesOnlyOwnSubscriptions(t *testing.T) {
	bus := NewInst(zerolog.Nop())
	a := bus.NewClient("test.app", subCaps())
	b := bus.NewClient("test.app", subCaps())
	a.SetInstanceKey(1)
	b.SetInstanceKey(2)

	var gotA, gotB int
	_, err := a.Subscribe("t.x", func(msg *app.Msg) { gotA++ })
	require.NoError(t, err)
	_, err = a.Subscribe("t.y", func(msg *app.Msg) { gotA++ })
	require.NoError(t, err)
	_, err = b.Subscribe("t.x", func(msg *app.Msg) { gotB++ })
	require.NoError(t, err)
	require.Equal(t, 2, a.SubscriptionCount())
	require.Equal(t, 1, b.SubscriptionCount())

	rows := bus.Subscriptions()
	require.Len(t, rows, 3)
	keys := map[uint64]int{}
	for _, r := range rows {
		keys[r.InstanceKey]++
		assert.Equal(t, app.AppIdT("test.app"), r.AppId)
	}
	assert.Equal(t, map[uint64]int{1: 2, 2: 1}, keys, "subscriptions carry the instance key of the client that made them")

	require.NoError(t, a.Close())
	require.NoError(t, a.Close(), "Close is idempotent")
	assert.True(t, a.IsClosed())
	assert.Equal(t, 0, a.SubscriptionCount())

	rows = bus.Subscriptions()
	require.Len(t, rows, 1, "b's subscription survives a's close")
	assert.Equal(t, uint64(2), rows[0].InstanceKey)

	require.NoError(t, b.Publish("t.x", []byte("m")))
	assert.Equal(t, 0, gotA, "a's handlers are never invoked after Close")
	assert.Equal(t, 1, gotB)
}

func TestClientClose_OperationsReturnErrClosed(t *testing.T) {
	bus := NewInst(zerolog.Nop())
	c := bus.NewClient("test.app", subCaps())
	require.NoError(t, c.Close())

	err := c.Publish("t.x", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrClosed), "publish after close: %v", err)

	_, err = c.Subscribe("t.x", func(msg *app.Msg) {})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrClosed), "subscribe after close: %v", err)

	_, err = c.Request("t.x", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrClosed), "request after close: %v", err)
}

func TestClientClose_UnsubscribeAfterCloseIsNoop(t *testing.T) {
	// An app that already released its subscription in Unmount, or one
	// that releases it after the host closed the client, is fine either
	// way: the released id is gone and unsubscribing again is a no-op.
	bus := NewInst(zerolog.Nop())
	c := bus.NewClient("test.app", subCaps())
	unsub, err := c.Subscribe("t.x", func(msg *app.Msg) {})
	require.NoError(t, err)
	unsub()
	require.Equal(t, 0, c.SubscriptionCount())
	require.NoError(t, c.Close())
	unsub() // must not panic or resurrect anything
	assert.Empty(t, bus.Subscriptions())
}

func TestClientClose_GrantsDieWithTheClient(t *testing.T) {
	// The cap broker addresses grants to an app id and lands them on the
	// newest live client (AddCap). Once that client closes, the grant is
	// gone with it and the registry answers with the next live client —
	// not with a client whose window has been reaped.
	bus := NewInst(zerolog.Nop())
	older := bus.NewClient("test.app", nil)
	newer := bus.NewClient("test.app", nil)

	got, ok := bus.ClientByAppId("test.app")
	require.True(t, ok)
	require.Same(t, newer, got, "lookup lands on the newest live client")
	got.AddCap(app.SubjectFilter{Pattern: "fs.handle.abc.>", Direction: app.CapDirectionBoth})
	require.NoError(t, newer.Publish("fs.handle.abc.read", nil))

	require.NoError(t, newer.Close())
	got, ok = bus.ClientByAppId("test.app")
	require.True(t, ok, "an older live client is still addressable")
	require.Same(t, older, got)
	err := older.Publish("fs.handle.abc.read", nil)
	require.Error(t, err, "the grant belonged to the closed client, not to the app id")
	assert.True(t, errors.Is(err, ErrPermissionViolation))

	require.NoError(t, older.Close())
	_, ok = bus.ClientByAppId("test.app")
	assert.False(t, ok, "no live client remains under the app id")
	assert.Empty(t, bus.LiveClients())
}

func TestClientClose_RacingSubscribeDoesNotOutliveClient(t *testing.T) {
	// A Subscribe that lands after Close must not leave a subscription in
	// the router: the closed check up front catches the common case, and
	// trackSub's refusal catches the interleaving where Close ran between
	// the check and the router insert.
	bus := NewInst(zerolog.Nop())
	c := bus.NewClient("test.app", subCaps())
	// Simulate the interleaving where the up-front closed check passed but
	// Close completed before trackSub ran: the id set is already released
	// while the flag Subscribe read was still false.
	c.subMu.Lock()
	c.subIds = nil
	c.subMu.Unlock()
	_, err := c.Subscribe("t.x", func(msg *app.Msg) {})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrClosed))
	assert.Empty(t, bus.Subscriptions())
}
