package imztop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The replay session is process-wide (ADR-0197 §SD5), so these tests share one
// piece of state and must not run in parallel with each other. Each resets it.
func resetReplay(t *testing.T) {
	t.Helper()
	require.NoError(t, LeaveReplay())
	t.Cleanup(func() { require.NoError(t, LeaveReplay()) })
}

// installTestSession puts a session built over a synthetic source into the
// process-wide slot, standing in for what enterReplay does after it has dialled
// a database. closed reports whether the source's close ran.
func installTestSession(t *testing.T, opts ReplayOptions) (session *ReplaySampler, closed *bool) {
	t.Helper()
	session, err := NewReplaySampler(opts)
	require.NoError(t, err)
	session.Start(context.Background())

	flag := false
	closed = &flag
	require.True(t, beginOpening(), "the session slot should have been free")
	require.True(t, installReplay(session, nil, "test-host", "http://ch.example:8123",
		func() { flag = true }))
	return session, closed
}

func TestReplaySession_StartsOff(t *testing.T) {
	resetReplay(t)

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOff, st.State)
	assert.NoError(t, st.Err)
	assert.False(t, st.Empty)
	assert.Nil(t, ActiveReplay())
}

func TestReplayStateE_String(t *testing.T) {
	assert.Equal(t, "off", ReplayOff.String())
	assert.Equal(t, "opening", ReplayOpening.String())
	assert.Equal(t, "on", ReplayOn.String())
	assert.Equal(t, "failed", ReplayFailed.String())
}

// TestReplaySession_ActiveSamplerSwapsAndBack is the whole point of the
// session: the render path follows it without knowing it did.
func TestReplaySession_ActiveSamplerSwapsAndBack(t *testing.T) {
	resetReplay(t)

	live, err := activeSampler()
	require.NoError(t, err)
	require.NotNil(t, live)

	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	session, closed := installTestSession(t, ReplayOptions{Source: src, Speed: 10000})

	got, err := activeSampler()
	require.NoError(t, err)
	assert.Same(t, session, got, "the render path must draw the replay session")
	assert.Same(t, session, ActiveReplay())

	require.NoError(t, LeaveReplay())
	assert.True(t, *closed, "leaving must release the source")

	back, err := activeSampler()
	require.NoError(t, err)
	assert.Same(t, live, back, "leaving must return the render path to live data")
	assert.Nil(t, ActiveReplay())
}

func TestReplaySession_StatusReportsHostAndEndpoint(t *testing.T) {
	resetReplay(t)

	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	installTestSession(t, ReplayOptions{Source: src, Speed: 10000})

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOn, st.State)
	assert.Equal(t, "test-host", st.Host)
	assert.Equal(t, "http://ch.example:8123", st.Endpoint)
}

// TestReplaySession_EmptyWindowIsNotAnError is the "no tee, nothing to replay"
// case: a host the tee never ran for opens fine and has no history, which the
// UI must distinguish from a session that has not reached its first bundle.
func TestReplaySession_EmptyWindowIsNotAnError(t *testing.T) {
	resetReplay(t)

	session, _ := installTestSession(t, ReplayOptions{Source: newFakeSource(), Speed: 10000})
	require.Eventually(t, session.Exhausted, 5*time.Second, 5*time.Millisecond)

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOn, st.State, "an empty window is a healthy session")
	assert.NoError(t, st.Err)
	assert.True(t, st.Empty, "the UI has to be able to say there is nothing stored")
}

// TestReplaySession_NotEmptyBeforeTheFirstBundle pins the other half: a session
// that simply has not started playing is not "empty".
func TestReplaySession_NotEmptyBeforeTheFirstBundle(t *testing.T) {
	resetReplay(t)

	src := newFakeSource(replayBundles(replayBase, 1000, 3)...)
	installTestSession(t, ReplayOptions{Source: src, StartPaused: true})

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOn, st.State)
	assert.False(t, st.Empty, "paused at the start is not the same as nothing stored")
}

func TestReplaySession_LeaveIsSafeWhenOff(t *testing.T) {
	resetReplay(t)
	require.NoError(t, LeaveReplay())
	require.NoError(t, LeaveReplay())
	assert.Equal(t, ReplayOff, CurrentReplayStatus().State)
}

// TestReplaySession_SecondOpenIsRefused pins that the slot is claimed once: two
// windows both asking for replay must not build two sessions and two
// connections.
func TestReplaySession_SecondOpenIsRefused(t *testing.T) {
	resetReplay(t)

	require.True(t, beginOpening())
	assert.False(t, beginOpening(), "a second open while opening must be refused")

	src := newFakeSource(replayBundles(replayBase, 1000, 2)...)
	session, err := NewReplaySampler(ReplayOptions{Source: src, Speed: 10000})
	require.NoError(t, err)
	session.Start(context.Background())
	require.True(t, installReplay(session, nil, "h", "e", func() {}))

	assert.False(t, beginOpening(), "a second open while on must be refused")
}

// TestReplaySession_LeaveDuringOpenDiscardsTheSession is the race the install
// guard exists for: the user backs out while the connection is still being
// made, and the session that eventually finishes opening must not appear.
func TestReplaySession_LeaveDuringOpenDiscardsTheSession(t *testing.T) {
	resetReplay(t)

	require.True(t, beginOpening())
	require.NoError(t, LeaveReplay()) // user backs out mid-open

	src := newFakeSource(replayBundles(replayBase, 1000, 2)...)
	session, err := NewReplaySampler(ReplayOptions{Source: src, Speed: 10000})
	require.NoError(t, err)
	session.Start(context.Background())
	closed := false
	installed := installReplay(session, nil, "h", "e", func() { closed = true })

	assert.False(t, installed, "an abandoned open must not install")
	assert.Equal(t, ReplayOff, CurrentReplayStatus().State)
	assert.Nil(t, ActiveReplay())

	// enterReplay cleans up what it built when the install is refused; do the
	// same here so the test leaks no goroutine.
	require.NoError(t, session.Close())
	_ = closed
}

// TestReplaySession_FailedOpenKeepsLiveOnScreen pins the degradation: a
// replay that cannot open leaves the live sampler drawing, with the reason
// available to render beside it.
func TestReplaySession_FailedOpenKeepsLiveOnScreen(t *testing.T) {
	resetReplay(t)

	live, err := activeSampler()
	require.NoError(t, err)

	require.True(t, beginOpening())
	failOpening(errors.New("clickhouse unreachable"))

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayFailed, st.State)
	require.Error(t, st.Err)
	assert.Contains(t, st.Err.Error(), "unreachable")

	got, serr := activeSampler()
	require.NoError(t, serr)
	assert.Same(t, live, got, "a failed open must not blank the window")
	assert.Nil(t, ActiveReplay())
}

// TestReplaySession_FailAfterLeaveIsIgnored covers the mirror race: the open
// fails after the user already backed out, and must not resurrect an error
// state on a session nobody is waiting for.
func TestReplaySession_FailAfterLeaveIsIgnored(t *testing.T) {
	resetReplay(t)

	require.True(t, beginOpening())
	require.NoError(t, LeaveReplay())
	failOpening(errors.New("too late"))

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOff, st.State)
	assert.NoError(t, st.Err)
}

// TestReplaySession_ConcurrentStatusReads is a race-detector probe: the UI
// polls status every frame while the transport runs.
func TestReplaySession_ConcurrentStatusReads(t *testing.T) {
	resetReplay(t)

	src := newFakeSource(replayBundles(replayBase, 1000, 20)...)
	installTestSession(t, ReplayOptions{Source: src, Speed: 10000})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = CurrentReplayStatus()
				if s := ActiveReplay(); s != nil {
					_ = s.Latest()
					_, _ = s.Position()
				}
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestStoreSource_ReportsWhyThereIsNothingToReplay covers the unreachable-server
// path without a server: the message has to name the endpoint, because that is
// the only thing the person reading it can act on.
//
// The endpoint is passed rather than set in the environment. The CLICKHOUSE_*
// registry entries cache on first read, so t.Setenv is a no-op in any process
// that has already resolved one — which made an earlier version of this test
// pass alone and fail in the suite, depending on whether something had read the
// config first.
func TestStoreSource_ReportsWhyThereIsNothingToReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewStoreSource(ctx, StoreSourceOptions{
		Host:     "nobody",
		Endpoint: "http://127.0.0.1:1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1", "the endpoint must be in the message")
	assert.Contains(t, err.Error(), "ClickHouse")
}
