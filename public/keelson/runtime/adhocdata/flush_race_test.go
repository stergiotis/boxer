package adhocdata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// FlushRetracts makes the two-phase withdrawal synchronous: when it returns,
// no dataset that has left is still registered. That includes one whose grace
// timer fired just before the flush and is still unloading on the timer's
// goroutine — the flush used to miss it, since the timer takes the handle off
// the pending set before it unregisters the provider, and a check right after
// the flush found a provider for a dataset nobody held (the windowhost
// interleaving lane failed this way under load). A grace of a nanosecond puts
// the timer and the flush in a race on every iteration.
func TestFlushRetractsWaitsForAnUnloadInFlight(t *testing.T) {
	reg := introspect.NewRegistry()
	svc, err := NewService(Config{Registry: reg, Dir: t.TempDir(), Log: testLogger(t), RetractGrace: time.Nanosecond})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	stream := int64Stream(t, false, 1)
	for i := range 3000 {
		res, pErr := svc.Publish(PublishInput{Alias: "racing", ArrowIPCStream: stream})
		require.NoError(t, pErr)
		require.NoError(t, svc.Retract(res.Handle, Identity{}))
		svc.FlushRetracts()
		_, ok := reg.Lookup(res.Handle)
		require.Falsef(t, ok, "iteration %d: a left dataset is still registered after FlushRetracts", i)
	}
}
