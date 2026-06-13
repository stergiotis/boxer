package nanopass_test

// Tests for the bounded ANTLR DFA cache (ADR-0084): structurally novel SQL must
// not grow the cache without bound, templated SQL must never trigger a reset,
// and the reset path must be race-free under concurrent parsing.

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// randExpr builds a deterministic pseudo-random, guard-legal expression tree of
// bounded depth — enough structural novelty to grow the DFA cache.
func randExpr(r *rand.Rand, depth int) string {
	if depth <= 0 || r.Intn(3) == 0 {
		switch r.Intn(3) {
		case 0:
			return fmt.Sprintf("c%d", r.Intn(8))
		case 1:
			return fmt.Sprintf("%d", r.Intn(1000))
		default:
			return fmt.Sprintf("t%d.x", r.Intn(4))
		}
	}
	switch r.Intn(6) {
	case 0:
		ops := []string{"+", "-", "*", "/"}
		return fmt.Sprintf("(%s %s %s)", randExpr(r, depth-1), ops[r.Intn(len(ops))], randExpr(r, depth-1))
	case 1:
		ops := []string{"=", "<", ">", "<="}
		return fmt.Sprintf("(%s %s %s)", randExpr(r, depth-1), ops[r.Intn(len(ops))], randExpr(r, depth-1))
	case 2:
		return fmt.Sprintf("(%s AND %s)", randExpr(r, depth-1), randExpr(r, depth-1))
	case 3:
		fns := []string{"abs", "round", "length"}
		return fmt.Sprintf("%s(%s)", fns[r.Intn(len(fns))], randExpr(r, depth-1))
	case 4:
		return fmt.Sprintf("CASE WHEN %s THEN %s ELSE %s END", randExpr(r, depth-1), randExpr(r, depth-1), randExpr(r, depth-1))
	default:
		return fmt.Sprintf("%s IN (%s, %s)", randExpr(r, depth-1), randExpr(r, depth-1), randExpr(r, depth-1))
	}
}

func randQuery(r *rand.Rand) string {
	return fmt.Sprintf("SELECT %s FROM t%d WHERE %s",
		randExpr(r, 1+r.Intn(5)), r.Intn(4), randExpr(r, 1+r.Intn(5)))
}

// withDFALimits tightens the cache knobs for a test and restores them after.
func withDFALimits(t *testing.T, maxStates, checkInterval int64) {
	t.Helper()
	origMax, origInt := nanopass.MaxDFAStates, nanopass.DFACheckInterval
	nanopass.MaxDFAStates, nanopass.DFACheckInterval = maxStates, checkInterval
	t.Cleanup(func() { nanopass.MaxDFAStates, nanopass.DFACheckInterval = origMax, origInt })
}

// TestDFACacheBounded: a flood of structurally novel SQL stays bounded — the
// cache resets instead of growing without limit. Without the fix this reaches
// hundreds of thousands of states; with it, it sawtooths under the threshold.
func TestDFACacheBounded(t *testing.T) {
	withDFALimits(t, 2000, 64)
	g1Before, _ := nanopass.DFACacheStats()

	r := rand.New(rand.NewSource(1))
	// Cold-cache novel parses are intrinsically slow (full ALL(*) prediction);
	// a couple thousand is plenty to cross the 2000-state threshold many times.
	n := 2500
	if testing.Short() {
		n = 800
	}
	for i := 0; i < n; i++ {
		_, _ = nanopass.Parse(randQuery(r))
	}

	g1After, _ := nanopass.DFACacheStats()
	resetsDelta := g1After.Resets - g1Before.Resets
	if resetsDelta == 0 {
		t.Fatalf("expected the cache to reset under %d novel parses, but it never did (states=%d)", n, g1After.States)
	}
	// The last measured size must be bounded: threshold plus at most one
	// check-interval of growth. Use a generous ceiling — the point is that it
	// is O(MaxDFAStates), not O(parses).
	ceiling := nanopass.MaxDFAStates * 8
	if g1After.States > ceiling {
		t.Fatalf("cache not bounded: last measured states=%d exceeds ceiling %d", g1After.States, ceiling)
	}
	t.Logf("bounded: %d novel parses → %d resets, last measured states=%d (threshold %d)",
		n, resetsDelta, g1After.States, nanopass.MaxDFAStates)
}

// TestDFACacheTemplatedNoReset: templated SQL (one shape, varying literals)
// plateaus far below the threshold, so it must never reset — no warm-cache
// churn for the common proxy workload.
func TestDFACacheTemplatedNoReset(t *testing.T) {
	withDFALimits(t, 1<<20, 128) // threshold high enough to never trigger
	g1Before, _ := nanopass.DFACacheStats()

	for i := 0; i < 5000; i++ {
		pr, err := nanopass.Parse(fmt.Sprintf("SELECT c0, c1 FROM t WHERE x = %d AND y = 'v%d'", i, i))
		if err != nil {
			t.Fatalf("templated parse %d failed: %v", i, err)
		}
		if pr == nil {
			t.Fatalf("templated parse %d returned nil result", i)
		}
	}

	g1After, _ := nanopass.DFACacheStats()
	if d := g1After.Resets - g1Before.Resets; d != 0 {
		t.Fatalf("templated traffic should never reset the cache, got %d resets", d)
	}
}

// TestDFACacheConcurrent: concurrent parsing with a tight threshold exercises
// the reset path under contention. Run with -race to validate the locking.
func TestDFACacheConcurrent(t *testing.T) {
	withDFALimits(t, 1500, 32)
	g1Before, _ := nanopass.DFACacheStats()

	workers, perWorker := 8, 500
	if testing.Short() {
		perWorker = 150 // enough to force resets under -race without the wall-clock
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for i := 0; i < perWorker; i++ {
				_, _ = nanopass.Parse(randQuery(r))
			}
		}(int64(w) + 1)
	}
	wg.Wait()

	g1After, _ := nanopass.DFACacheStats()
	if g1After.Resets-g1Before.Resets == 0 {
		t.Fatalf("expected resets under concurrent novel load, got none (states=%d)", g1After.States)
	}
	if ceiling := nanopass.MaxDFAStates * 8; g1After.States > ceiling {
		t.Fatalf("cache not bounded under concurrency: states=%d > %d", g1After.States, ceiling)
	}
}
