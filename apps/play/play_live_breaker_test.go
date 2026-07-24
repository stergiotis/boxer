package play

// The Live circuit breaker (ADR-0097, the 2026-07-22 review remediation):
// SD9's acyclicity guard covers data edges, not emit feedback, so a query
// whose result moves its own input a little every run ratchets instead of
// settling. The breaker is the witness that tells that apart from a person
// driving a value, and suspends Live when it sees the machine case.

import (
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// breakerApp is a settled live app whose one referenced signal has moved
// since the last Run — the state a frame is in when an auto-run is about to
// fire. writer decides whether that move looks human or machine.
func breakerApp(t *testing.T, writer string) *PlayApp {
	t.Helper()
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.sql = "SELECT {tl_min:Int64} AS v"
	app.lastSentSql = app.sql
	app.autoRunStreakSql = app.sql
	app.paramSlots = []paramSlot{{Name: "tl_min", Type: "Int64"}}
	app.lastSentSigParams = map[string]string{"param_tl_min": "1"}
	app.graph.setSignalRawFrom("tl_min", "2", writer)
	app.frameSig = app.graph.signals()
	app.liveMain = true
	return app
}

// The witness, gate by gate: a machine-written divergence is a loop
// candidate; anything a person wrote is not, however fast it moves.
func TestMachineDrivenDivergenceGates(t *testing.T) {
	require.Equal(t, []string{"tl_min"}, breakerApp(t, "timeline").machineDrivenDivergence(),
		"a panel's own publication is machine-driven")
	require.Equal(t, []string{"tl_min"}, breakerApp(t, signalWriterClamp).machineDrivenDivergence())
	require.Equal(t, []string{"tl_min"}, breakerApp(t, signalWriterMap).machineDrivenDivergence())

	for _, human := range []string{signalWriterEditor, signalWriterParamWidget, signalWriterHistory} {
		require.Nil(t, breakerApp(t, human).machineDrivenDivergence(),
			"a human write is never a loop to break: "+human)
	}

	// One human among several diverging names is enough — a person is driving.
	mixed := breakerApp(t, "timeline")
	mixed.paramSlots = append(mixed.paramSlots, paramSlot{Name: "q", Type: "String"})
	mixed.graph.setSignalRawFrom("q", "typed", signalWriterParamWidget)
	mixed.frameSig = mixed.graph.signals()
	require.Nil(t, mixed.machineDrivenDivergence())

	// A diverging name the store does not hold has no writer to judge:
	// conservative, so the breaker does not fire.
	discarded := breakerApp(t, "timeline")
	discarded.graph.deleteSignal("tl_min")
	discarded.frameSig = discarded.graph.signals()
	require.Nil(t, discarded.machineDrivenDivergence())

	// Nothing diverging at all is not a streak either.
	settled := breakerApp(t, "timeline")
	settled.lastSentSigParams = map[string]string{"param_tl_min": "2"}
	require.Nil(t, settled.machineDrivenDivergence())
}

// divergedSignalNames itemises the witness runSignalsDiverged answers with a
// bool — the two must agree, or the breaker would name signals the staleness
// witness does not consider changed.
func TestDivergedSignalNamesAgreesWithWitness(t *testing.T) {
	app := breakerApp(t, "timeline")
	require.True(t, app.runSignalsDiverged())
	require.Equal(t, []string{"tl_min"}, app.divergedSignalNames())

	app.lastSentSigParams = map[string]string{"param_tl_min": "2"}
	require.False(t, app.runSignalsDiverged())
	require.Empty(t, app.divergedSignalNames())

	// A name the last Run shipped that no longer resolves is divergence too.
	app.lastSentSigParams = map[string]string{"param_tl_min": "2", "param_gone": "9"}
	require.True(t, app.runSignalsDiverged())
	require.Equal(t, []string{"gone"}, app.divergedSignalNames())
}

// Consecutive machine-driven auto-runs on an unchanged buffer trip the
// breaker: Live unchecks itself and says which signal was cycling.
func TestBreakerSuspendsLiveAfterMachineStreak(t *testing.T) {
	app := breakerApp(t, "timeline")
	for i := 1; i < autoRunLoopLimit; i++ {
		app.noteAutoRunFired()
		require.True(t, app.liveMain, "still under the limit at %d", i)
		require.Empty(t, app.liveSuspendReason)
	}
	app.noteAutoRunFired()
	require.False(t, app.liveMain, "the breaker unchecks Live rather than muting it")
	require.Contains(t, app.liveSuspendReason, "tl_min", "the reason names what was cycling")
	require.False(t, app.shouldAutoRun(), "…and the unchecked toggle stops the loop")
}

// A human write mid-streak resets the count: the breaker must never fire on
// someone dragging a slider.
func TestBreakerResetsOnHumanWrite(t *testing.T) {
	app := breakerApp(t, "timeline")
	for range autoRunLoopLimit - 1 {
		app.noteAutoRunFired()
	}
	// The person types a value in the pane.
	app.graph.setSignalRawFrom("tl_min", "99", signalWriterParamWidget)
	app.frameSig = app.graph.signals()
	app.noteAutoRunFired()
	require.True(t, app.liveMain, "a human write is not part of a machine streak")
	require.Equal(t, 0, app.autoRunStreak)

	// And the machine has to start over.
	app.graph.setSignalRawFrom("tl_min", "100", "timeline")
	app.frameSig = app.graph.signals()
	for range autoRunLoopLimit - 1 {
		app.noteAutoRunFired()
	}
	require.True(t, app.liveMain)
}

// A buffer edit resets the count: the streak is a claim about ONE query
// feeding itself, so a different query starts a new claim.
func TestBreakerResetsOnBufferEdit(t *testing.T) {
	app := breakerApp(t, "timeline")
	for range autoRunLoopLimit - 1 {
		app.noteAutoRunFired()
	}
	app.sql += " -- edited"
	app.noteAutoRunFired()
	require.True(t, app.liveMain)
	require.Equal(t, 1, app.autoRunStreak, "the streak restarts against the new buffer")
}

// A human Run clears the notice, and re-checking Live resumes.
func TestBreakerNoticeClearsOnHumanAction(t *testing.T) {
	app := breakerApp(t, "timeline")
	for range autoRunLoopLimit {
		app.noteAutoRunFired()
	}
	require.NotEmpty(t, app.liveSuspendReason)

	app.resumeLiveAfterHumanAction()
	require.Empty(t, app.liveSuspendReason)
	require.Equal(t, 0, app.autoRunStreak)
}

// The ratchet, end to end against a capture server: a query windowed by
// {tl_min} whose panel republishes a moved extent after every run. Each run
// is legitimate on its own — the value really did change — which is why the
// gate-level checks cannot catch it and a streak witness is needed.
func TestBreakerTripsOnRatchetingSelfFeedingQuery(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 10), "")
	defer app.graph.close()
	app.sql = "SELECT * FROM t WHERE ts > {tl_min:Int64}"
	app.paramSlots = []paramSlot{{Name: "tl_min", Type: "Int64"}}
	app.autoRunStreakSql = app.sql
	app.liveMain = true

	app.graph.setSignalRawFrom("tl_min", "1", signalWriterParamWidget)
	app.frameSig = app.graph.signals()
	app.executeRun(false) // the first run is the human's
	require.Eventually(t, func() bool { return len(got()) == 1 && !app.graph.MainLoading() },
		2*time.Second, time.Millisecond)

	runs := 0
	for i := range 40 {
		// The panel publishes an extent derived from the result it just
		// got — a little further along every time.
		app.graph.setSignalRawFrom("tl_min", strconv.Itoa(i+2), "timeline")
		app.frameSig = app.graph.signals()
		if !app.shouldAutoRun() {
			break
		}
		app.noteAutoRunFired()
		app.executeRun(true)
		runs++
		require.Eventually(t, func() bool { return !app.graph.MainLoading() },
			2*time.Second, time.Millisecond)
		app.frameSig = app.graph.signals()
	}

	require.False(t, app.liveMain, "the ratchet trips the breaker instead of running forever")
	require.Contains(t, app.liveSuspendReason, "tl_min")
	require.Equal(t, autoRunLoopLimit, runs, "it suspends at the limit, not before or long after")
	require.Len(t, got(), 1+autoRunLoopLimit, "and the server sees exactly those runs")
}
