package adhocdata

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// ADR-0188 §SD3: retract is leave → notify → unload. A consumer subscribed
// to adhoc.event.> sees `published` on every publish and `retracted` at the
// leave step; the provider answers during the grace and not after.

type eventLog struct {
	mu  sync.Mutex
	evs []Event
}

func (l *eventLog) add(ev Event) {
	l.mu.Lock()
	l.evs = append(l.evs, ev)
	l.mu.Unlock()
}

func (l *eventLog) snapshot() (evs []Event) {
	l.mu.Lock()
	evs = append(evs, l.evs...)
	l.mu.Unlock()
	return
}

func waitEvents(t *testing.T, l *eventLog, n int) (evs []Event) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		evs = l.snapshot()
		if len(evs) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d events, got %d: %+v", n, len(evs), evs)
	return
}

func TestWithdrawal_TwoPhaseWithEvents(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	reg := introspect.NewRegistry()
	keys := newFakeKeys()
	svc, err := NewService(Config{
		Bus: bus, Registry: reg, Keys: keys, Dir: t.TempDir(), Log: logger,
		RetractGrace: 150 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	producer := bus.NewClient("test.producer", []app.SubjectFilter{
		{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"},
	})
	consumer := bus.NewClient("test.consumer", []app.SubjectFilter{
		{Pattern: SubjectEventAll, Direction: app.CapDirectionSub, Reason: "test"},
	})
	log := &eventLog{}
	unsub, err := SubscribeEvents(consumer, log.add)
	require.NoError(t, err)
	t.Cleanup(unsub)

	res, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1, 2)})
	require.NoError(t, err)
	evs := waitEvents(t, log, 1)
	assert.Equal(t, EventOpPublished, evs[0].Op)
	assert.Equal(t, res.Handle, evs[0].Handle)
	assert.Equal(t, "items", evs[0].Alias)
	assert.Equal(t, "test.producer", evs[0].Publisher, "the event attributes the publish to the bus sender")
	assert.Equal(t, uint64(1), evs[0].Revision)

	// A republish is a `published` event too, at the bumped revision.
	_, err = PublishRequest(producer, PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 3)})
	require.NoError(t, err)
	evs = waitEvents(t, log, 2)
	assert.Equal(t, EventOpPublished, evs[1].Op)
	assert.Equal(t, uint64(2), evs[1].Revision)

	// LEAVE + notify.
	require.NoError(t, RetractRequest(producer, res.Handle))
	evs = waitEvents(t, log, 3)
	assert.Equal(t, EventOpRetracted, evs[2].Op)
	assert.Equal(t, res.Handle, evs[2].Handle)
	assert.Equal(t, "items", evs[2].Alias)

	_, rErr := svc.Resolve("items")
	require.Error(t, rErr, "left: the alias no longer resolves")
	assert.Empty(t, svc.catalogRows(), "left: the catalog no longer lists it")
	_, ok := reg.Lookup(res.Handle)
	assert.True(t, ok, "grace: the provider still answers")
	assert.True(t, keys.has(res.Handle), "grace: the key is still with the broker")

	// UNLOAD after the grace.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, still := reg.Lookup(res.Handle); !still {
			break
		}
		require.True(t, time.Now().Before(deadline), "provider was not unloaded after the grace")
		time.Sleep(10 * time.Millisecond)
	}
	assert.False(t, keys.has(res.Handle), "unloaded: the key is gone")

	// A retracted handle cannot be republished; a fresh publish mints anew.
	_, err = PublishRequest(producer, PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 4)})
	require.Error(t, err)
	res2, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 4)})
	require.NoError(t, err)
	assert.NotEqual(t, res.Handle, res2.Handle)
}

func TestWithdrawal_CloseFlushesPendingUnloads(t *testing.T) {
	logger := testLogger(t)
	reg := introspect.NewRegistry()
	keys := newFakeKeys()
	svc, err := NewService(Config{
		Registry: reg, Keys: keys, Dir: t.TempDir(), Log: logger,
		RetractGrace: time.Hour, // never elapses inside the test
	})
	require.NoError(t, err)
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	require.NoError(t, svc.Retract(res.Handle))
	_, ok := reg.Lookup(res.Handle)
	require.True(t, ok, "in grace")
	require.NoError(t, svc.Close(context.Background()))
	_, ok = reg.Lookup(res.Handle)
	assert.False(t, ok, "Close unloads what the grace had not yet")
	assert.False(t, keys.has(res.Handle))
}

func TestWithdrawal_NoBusMeansNoEventsAndNoPanic(t *testing.T) {
	svc := newTestService(t)
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	require.NoError(t, svc.Retract(res.Handle))
	svc.FlushRetracts()
}

func TestResolveVerify_AnswersLivenessAndSuccessor(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{
		Bus: bus, Registry: introspect.NewRegistry(), Keys: newFakeKeys(), Dir: t.TempDir(), Log: logger,
		RetractGrace: time.Hour,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	producer := bus.NewClient("test.producer", []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth}})
	consumer := bus.NewClient("test.consumer", []app.SubjectFilter{{Pattern: SubjectResolve, Direction: app.CapDirectionPub}})

	// Nothing under the alias, and a handle nobody minted: not live, no successor.
	_, live, err := ResolveVerifyRequest(consumer, "items", "adhoc_nothing0000000000")
	require.Error(t, err)
	assert.False(t, live)

	res1, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	res, live, err := ResolveVerifyRequest(consumer, "items", res1.Handle)
	require.NoError(t, err)
	assert.True(t, live, "bound handle is live")
	assert.Equal(t, res1.Handle, res.Handle)

	// A newer sibling under the alias: ours is still live, resolve names the sibling.
	res2, err := PublishRequest(producer, PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err)
	res, live, err = ResolveVerifyRequest(consumer, "items", res1.Handle)
	require.NoError(t, err)
	assert.True(t, live, "a sibling does not end our binding")
	assert.Equal(t, res2.Handle, res.Handle)

	// Ours retracted (in grace, still queryable): not live; the sibling is the successor.
	require.NoError(t, RetractRequest(producer, res1.Handle))
	res, live, err = ResolveVerifyRequest(consumer, "items", res1.Handle)
	require.NoError(t, err)
	assert.False(t, live, "left the live set at the leave step, before unload")
	assert.Equal(t, res2.Handle, res.Handle)

	// Everything retracted: not live, nothing to bind.
	require.NoError(t, RetractRequest(producer, res2.Handle))
	_, live, err = ResolveVerifyRequest(consumer, "items", res2.Handle)
	require.Error(t, err)
	assert.False(t, live)
}
