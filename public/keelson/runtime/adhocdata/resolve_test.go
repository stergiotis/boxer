package adhocdata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestResolveNewestPerAlias pins the resolution policy (ADR-0134 §SD4,
// update 2026-08-01): the newest live dataset published under the alias
// wins, a republish keeps its handle the target, a retract falls back to
// the survivor, and an alias nobody published errors.
func TestResolveNewestPerAlias(t *testing.T) {
	svc, err := NewService(Config{
		Registry: introspect.NewRegistry(), Keys: newFakeKeys(),
		Dir: t.TempDir(), Log: testLogger(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	first, err := svc.Publish(PublishInput{Alias: "prof", Publisher: "a", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	// Creation instants are microsecond-resolution; keep them distinct.
	time.Sleep(2 * time.Millisecond)
	second, err := svc.Publish(PublishInput{Alias: "prof", Publisher: "b", ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err)
	require.NotEqual(t, first.Handle, second.Handle)

	res, err := svc.Resolve("prof")
	require.NoError(t, err)
	assert.Equal(t, second.Handle, res.Handle, "newest publish wins")

	// A republish bumps the revision but keeps the creation instant, so
	// the newer dataset stays the target and reports its new revision.
	rep, err := svc.Publish(PublishInput{Alias: "prof", Handle: second.Handle, ArrowIPCStream: int64Stream(t, false, 3)})
	require.NoError(t, err)
	res, err = svc.Resolve("prof")
	require.NoError(t, err)
	assert.Equal(t, second.Handle, res.Handle)
	assert.Equal(t, rep.Revision, res.Revision)

	// Retracting the winner falls back to the older survivor.
	require.NoError(t, svc.Retract(second.Handle))
	res, err = svc.Resolve("prof")
	require.NoError(t, err)
	assert.Equal(t, first.Handle, res.Handle)

	_, err = svc.Resolve("nobody")
	require.Error(t, err)
}
