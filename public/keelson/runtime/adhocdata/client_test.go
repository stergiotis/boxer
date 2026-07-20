package adhocdata

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func TestPublishAndRetractViaBus(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{
		Bus: bus, Registry: introspect.NewRegistry(), Keys: newFakeKeys(), Dir: t.TempDir(), Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	caller := bus.NewClient("test.embedder", []app.SubjectFilter{
		{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"},
	})

	res, err := PublishRequest(caller, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1, 2, 3)})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(res.Handle, "adhoc_"), res.Handle)
	assert.Equal(t, uint64(3), res.Rows)

	// Republish under the same handle bumps the revision.
	res2, err := PublishRequest(caller, PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 4)})
	require.NoError(t, err)
	assert.Equal(t, res.Handle, res2.Handle)
	assert.Equal(t, uint64(2), res2.Revision)

	require.NoError(t, RetractRequest(caller, res.Handle))
	require.Error(t, RetractRequest(caller, res.Handle), "retracting a gone handle errors over the bus")
}
