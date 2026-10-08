package opwire

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubjectRoundTrip(t *testing.T) {
	s := Subject("play", 42, "set_sql")
	assert.Equal(t, "app.play.42.op.set_sql", s)
	alias, inst, op, ok := ParseSubject(s)
	assert.True(t, ok)
	assert.Equal(t, "play", alias)
	assert.EqualValues(t, 42, inst)
	assert.Equal(t, "set_sql", op)
	for _, bad := range []string{"app.play.x.op.a", "app.play.1.request.a", "app.play.1.op", "net.http.fetch.x"} {
		_, _, _, ok = ParseSubject(bad)
		assert.False(t, ok, bad)
	}
}

func TestMatchesOperationSubjects(t *testing.T) {
	for p, want := range map[string]bool{
		">": true, "app.>": true, "app.*.>": true, "app.play.>": true, "app.play.1.op.x": true,
		"app.*.*.op.*": true, "app.play.*.*.*": true, "app.play.1.op.>": true,
		"app.play.event.x": false, "app.play.request.>": false, "runtime.>": false,
		"app.play.1.op": false, "app.play.1.op.x.y": false, "*.*.*.*.*": true, "*": false,
	} {
		assert.Equal(t, want, MatchesOperationSubjects(p), p)
	}
}

func TestPhaseNamesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range AllPhases {
		n := p.String()
		assert.NotEqual(t, "unspecified", n)
		assert.False(t, seen[n], n)
		seen[n] = true
	}
}

func TestGoroutineIdDiffersAcrossGoroutines(t *testing.T) {
	here := GoroutineId()
	assert.NotZero(t, here)
	ch := make(chan uint64)
	go func() { ch <- GoroutineId() }()
	assert.NotEqual(t, here, <-ch)
}

// Every phase has one result, and only applied, rendered and completed count
// as done: accepted and running are in flight (ADR-0277).
func TestEveryPhaseHasOneResult(t *testing.T) {
	var n int
	for _, r := range []ResultE{ResultInFlight, ResultDone, ResultWaiting, ResultNotDone} {
		n += len(PhasesOf(r))
	}
	assert.Equal(t, len(AllPhases), n)
	assert.Equal(t, []PhaseE{PhaseApplied, PhaseRendered, PhaseCompleted}, PhasesOf(ResultDone))
	assert.Equal(t, []PhaseE{PhaseAccepted, PhaseRunning}, PhasesOf(ResultInFlight))
	for _, p := range AllPhases {
		back, ok := ParsePhase(p.String())
		assert.True(t, ok)
		assert.Equal(t, p, back)
	}
	_, ok := ParsePhase("unspecified")
	assert.False(t, ok)
}
