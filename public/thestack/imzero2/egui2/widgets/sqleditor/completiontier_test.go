package sqleditor

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitCompletionIdle(t *testing.T, s *completionTier) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.runner.Running() {
		if time.Now().After(deadline) {
			t.Fatal("background scope parse did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCompletionTierUpgradeAfterQuiescence(t *testing.T) {
	clock := newFakeClock()
	built := 0
	s := &completionTier{
		now: clock.now,
		build: func(stmt string, _ highlight.CaretSite, _ int) (*sqlcomplete.Scope, error) {
			built++
			return &sqlcomplete.Scope{Clause: "SELECT"}, nil
		},
	}
	const stmt = "SELECT tupleElement(m, '')"

	assert.Nil(t, s.scopeFor(stmt, highlight.CaretSite{}, 24), "fresh buffer has no scope yet")
	assert.False(t, s.runner.Running(), "no launch before quiescence")

	clock.advance(completionQuiescence + time.Millisecond)
	assert.Nil(t, s.scopeFor(stmt, highlight.CaretSite{}, 24), "a scope cannot be ready the call that launches it")
	waitCompletionIdle(t, s)

	sc := s.scopeFor(stmt, highlight.CaretSite{}, 24)
	require.NotNil(t, sc)
	assert.Equal(t, "SELECT", sc.Clause)
	assert.Equal(t, 1, built)

	// Cached: repeated calls neither relaunch nor lose the scope.
	assert.NotNil(t, s.scopeFor(stmt, highlight.CaretSite{}, 24))
	assert.False(t, s.runner.Running())
}

// Moving the caret without editing supersedes: the scope says which frame and
// which clause, so one installed for another caret is the wrong answer.
func TestCompletionTierSupersedesOnCaretMove(t *testing.T) {
	clock := newFakeClock()
	release := make(chan struct{})
	var carets []int
	s := &completionTier{
		now: clock.now,
		build: func(_ string, _ highlight.CaretSite, caret int) (*sqlcomplete.Scope, error) {
			<-release
			carets = append(carets, caret)
			return &sqlcomplete.Scope{Clause: "SELECT"}, nil
		},
	}
	const stmt = "SELECT tupleElement(m, '')"

	s.scopeFor(stmt, highlight.CaretSite{}, 24)
	clock.advance(completionQuiescence + time.Millisecond)
	s.scopeFor(stmt, highlight.CaretSite{}, 24)
	require.True(t, s.runner.Running(), "the first caret must be in flight")

	// The caret moves; the in-flight answer describes the old one.
	s.scopeFor(stmt, highlight.CaretSite{}, 8)
	clock.advance(completionQuiescence + time.Millisecond)
	release <- struct{}{}
	waitCompletionIdle(t, s)

	assert.Nil(t, s.scopeFor(stmt, highlight.CaretSite{}, 8), "a scope for another caret must not install")
	assert.True(t, s.runner.Running(), "the same call relaunches for the new caret")
	release <- struct{}{}
	waitCompletionIdle(t, s)
	assert.NotNil(t, s.scopeFor(stmt, highlight.CaretSite{}, 8))
	assert.Equal(t, []int{24, 8}, carets)
}

// A statement no repair parses installs an empty scope rather than
// relaunching forever: the site alone stays the model (§SD3), and the tier
// must not spin.
func TestCompletionTierInstallsOnRefusal(t *testing.T) {
	clock := newFakeClock()
	built := 0
	s := &completionTier{
		now: clock.now,
		build: func(string, highlight.CaretSite, int) (*sqlcomplete.Scope, error) {
			built++
			return nil, eh.Errorf("no repair parsed")
		},
	}
	const stmt = "SELECT a FROM t JOIN "

	s.scopeFor(stmt, highlight.CaretSite{}, len(stmt))
	clock.advance(completionQuiescence + time.Millisecond)
	s.scopeFor(stmt, highlight.CaretSite{}, len(stmt))
	waitCompletionIdle(t, s)

	sc := s.scopeFor(stmt, highlight.CaretSite{}, len(stmt))
	require.NotNil(t, sc)
	assert.Empty(t, sc.Clause)
	assert.False(t, s.runner.Running())
	assert.Equal(t, 1, built, "a refusal must not relaunch")
}

// The editor publishes the scope beside the site, and it is the caret's own
// statement that was parsed.
func TestBindPublishesTheScope(t *testing.T) {
	buf := "SELECT 1;\nSELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, 'Tot"
	e := New()
	n := uint64(len([]rune(buf)))
	e.SetCaretForTest(n)
	clock := newFakeClock()
	e.completion.now = clock.now

	res := e.Bind(Frame{IDSlot: "t", Value: &buf})
	assert.Nil(t, res.Scope, "a fresh buffer has no scope yet")

	clock.advance(completionQuiescence + time.Millisecond)
	res = e.Bind(Frame{IDSlot: "t", Value: &buf})
	assert.Nil(t, res.Scope, "not on the launching frame")
	waitCompletionIdle(t, &e.completion)

	res = e.Bind(Frame{IDSlot: "t", Value: &buf})
	require.NotNil(t, res.Scope)
	assert.Equal(t, "LW_COMPONENT('SysMem')", res.Scope.Aliases["m"])
	require.NotNil(t, res.Scope.Frame)
	assert.Equal(t, "tupleElement", res.Scope.Frame.Callee)
	assert.Equal(t, 1, res.Scope.Frame.Ordinal)
}
