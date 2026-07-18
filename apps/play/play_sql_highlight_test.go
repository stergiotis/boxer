package play

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
)

// fakeClock is a manually advanced time source for sqlSemanticHl tests.
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time                { return f.t }
func (f *fakeClock) advance(d time.Duration)       { f.t = f.t.Add(d) }
func newFakeClock() *fakeClock                     { return &fakeClock{t: time.Unix(1000, 0)} }
func spansFor(src string) []highlight.Span         { return highlight.HighlightLex(src) }
func waitIdleOrDone(t *testing.T, s *sqlSemanticHl) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.runner.Running() {
		if time.Now().After(deadline) {
			t.Fatal("background parse did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSqlSemanticUpgradeAfterQuiescence: no launch while the buffer is
// fresh; launch after the quiescence window; install on drain; cached
// thereafter without relaunching.
func TestSqlSemanticUpgradeAfterQuiescence(t *testing.T) {
	clock := newFakeClock()
	parsed := 0
	s := &sqlSemanticHl{
		now: clock.now,
		parse: func(src string) []highlight.Span {
			parsed++
			return spansFor(src)
		},
	}
	const src = "SELECT number FROM system.numbers"

	if _, ok := s.jobFor(src); ok {
		t.Fatal("fresh buffer must not have a semantic job")
	}
	if s.runner.Running() {
		t.Fatal("must not launch before quiescence")
	}

	clock.advance(sqlSemanticQuiescence + time.Millisecond)
	if _, ok := s.jobFor(src); ok {
		t.Fatal("job cannot be ready the same call that launches it")
	}
	waitIdleOrDone(t, s)

	if _, ok := s.jobFor(src); !ok {
		t.Fatal("finished parse for the current buffer must install")
	}
	if parsed != 1 {
		t.Fatalf("want exactly one parse, got %d", parsed)
	}
	// cached: repeated calls neither relaunch nor lose the job
	if _, ok := s.jobFor(src); !ok || s.runner.Running() {
		t.Fatal("installed job must be served from cache without relaunch")
	}
}

// TestSqlSemanticSupersession: a result for an edited-away buffer is
// dropped, and the tier recovers by parsing the new content.
func TestSqlSemanticSupersession(t *testing.T) {
	clock := newFakeClock()
	release := make(chan struct{})
	var parsedSrcs []string
	s := &sqlSemanticHl{
		now: clock.now,
		parse: func(src string) []highlight.Span {
			<-release
			parsedSrcs = append(parsedSrcs, src)
			return spansFor(src)
		},
	}
	const srcA = "SELECT 1"
	const srcB = "SELECT 2"

	s.jobFor(srcA)
	clock.advance(sqlSemanticQuiescence + time.Millisecond)
	s.jobFor(srcA) // launches A, worker blocked
	if !s.runner.Running() {
		t.Fatal("A must be in flight")
	}

	// user edits to B; A's eventual result must not install
	s.jobFor(srcB)
	clock.advance(sqlSemanticQuiescence + time.Millisecond)
	release <- struct{}{} // let A finish
	waitIdleOrDone(t, s)

	if _, ok := s.jobFor(srcB); ok {
		t.Fatal("stale result for A must not install for B")
	}
	if s.jobOk {
		t.Fatal("no job may be installed from a stale result")
	}
	// the same call freed the slot and relaunched for B
	if !s.runner.Running() {
		t.Fatal("B must have been launched after dropping A's result")
	}
	release <- struct{}{}
	waitIdleOrDone(t, s)
	if _, ok := s.jobFor(srcB); !ok {
		t.Fatal("B's own result must install")
	}
	if len(parsedSrcs) != 2 || parsedSrcs[0] != srcA || parsedSrcs[1] != srcB {
		t.Fatalf("want parses [A B], got %v", parsedSrcs)
	}
}

// TestSqlSemanticEditResetsQuiescence: every content change restamps the
// edit clock, so continuous typing never launches.
func TestSqlSemanticEditResetsQuiescence(t *testing.T) {
	clock := newFakeClock()
	s := &sqlSemanticHl{
		now:   clock.now,
		parse: spansFor,
	}
	src := "SELECT"
	for i := 0; i < 5; i++ {
		s.jobFor(src)
		clock.advance(sqlSemanticQuiescence - time.Millisecond)
		src += "x" // keystroke just before the window elapses
	}
	if s.runner.Running() {
		t.Fatal("typing faster than the quiescence window must never launch")
	}
}
