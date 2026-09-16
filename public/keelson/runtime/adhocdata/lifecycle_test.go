package adhocdata

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// ADR-0240 §SD5: a dataset lives as long as the window that published it,
// unless the publisher said otherwise. Closing the instance's bus client
// retracts what it published; KeepAfterClose survives; a sibling instance
// of the same app is untouched.
func TestInstanceCloseRetractsWhatItPublished(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}

	window := bus.NewClient("test.app", caps)
	window.SetInstanceKey(1)
	sibling := bus.NewClient("test.app", caps)
	sibling.SetInstanceKey(2)

	mine, err := PublishRequest(window, PublishInput{Alias: "mine", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	kept, err := PublishRequest(window, PublishInput{Alias: "kept", KeepAfterClose: true, ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err)
	theirs, err := PublishRequest(sibling, PublishInput{Alias: "theirs", ArrowIPCStream: int64Stream(t, false, 3)})
	require.NoError(t, err)
	require.Equal(t, 3, svc.LiveCount())

	require.NoError(t, window.Close())
	assert.False(t, svc.IsLive(mine.Handle), "the window's dataset went with it, before Close returned")
	assert.True(t, svc.IsLive(kept.Handle), "kept after close: the app's, not the window's")
	assert.True(t, svc.IsLive(theirs.Handle), "a sibling instance's dataset is untouched")

	// The kept dataset is now republishable and retractable by the sibling.
	_, err = PublishRequest(sibling, PublishInput{Alias: "kept", Handle: kept.Handle, ArrowIPCStream: int64Stream(t, false, 4)})
	require.NoError(t, err)
	require.NoError(t, sibling.Close())
	assert.False(t, svc.IsLive(theirs.Handle))
	assert.True(t, svc.IsLive(kept.Handle), "and outlives every instance until the service closes")
}
