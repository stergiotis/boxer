package nanopass_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nanopass_profile_test.go covers ADR-0192's cost tree. The assertions are on
// SHAPE, ORDER and ITERATION COUNTS, never on absolute durations: a timing
// assertion is flaky under CI load and this package's -race lane runs roughly
// 4.6x slower than its plain one. What the tests defend is that every
// invocation the tree makes is accounted for exactly once, in the order it ran.

// sleepPass is a leaf that costs a known minimum and optionally rewrites.
func sleepPass(name string, d time.Duration, rewrite bool) nanopass.Pass {
	return nanopass.Pass{
		Name: name,
		Apply: func(_ *env.Environment, body string) (string, error) {
			time.Sleep(d)
			if rewrite {
				return body + " " + name, nil
			}
			return body, nil
		},
	}
}

// stepNames flattens a cost tree to "  "-indented names, which is a readable
// way to assert a whole shape in one Equal.
func stepNames(cost nanopass.StepCost) (out []string) {
	cost.Walk(func(s nanopass.StepCost, depth int) {
		out = append(out, strings.Repeat("  ", depth)+s.Name)
	})
	return
}

// TestRunProfiledMirrorsTheExecutedTree is the core claim: the cost tree has
// one node per pass invocation, nested and ordered the way the combinators
// actually ran them — including inside a composite the caller did not build.
func TestRunProfiledMirrorsTheExecutedTree(t *testing.T) {
	inner := nanopass.Sequence("inner",
		sleepPass("a", time.Millisecond, false),
		sleepPass("b", time.Millisecond, false),
	)
	outer := nanopass.Sequence("outer", inner, sleepPass("c", time.Millisecond, false))

	_, cost, err := outer.RunProfiled("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"outer",
		"  inner",
		"    a",
		"    b",
		"  c",
	}, stepNames(cost))
}

// TestRunProfiledMatchesRunOutput pins that profiling is observation only. If
// this ever diverges the trace stops describing the code path that executes,
// which is the property the whole surface rests on.
func TestRunProfiledMatchesRunOutput(t *testing.T) {
	p := nanopass.Sequence("seq",
		sleepPass("one", 0, true),
		sleepPass("two", 0, true),
	)
	const src = "SET param_x = 1;\nSELECT 1"
	plain, plainErr := p.Run(src)
	profiled, _, profErr := p.RunProfiled(src)
	require.NoError(t, plainErr)
	require.NoError(t, profErr)
	assert.Equal(t, plain, profiled)
}

// TestRunProfiledCountsFixedPointIterations covers the number that explains a
// surprising duration: a converging loop runs one iteration more than it
// rewrote, to observe that nothing changed.
func TestRunProfiledCountsFixedPointIterations(t *testing.T) {
	var calls int
	grow := nanopass.Pass{
		Name:       "grow",
		Properties: nanopass.PassProperties{NeedsFixedPoint: true},
		Apply: func(_ *env.Environment, body string) (string, error) {
			calls++
			if strings.Count(body, "x") < 3 {
				return body + "x", nil
			}
			return body, nil
		},
	}
	_, cost, err := nanopass.Sequence("seq", grow).RunProfiled("SELECT 1")
	require.NoError(t, err)
	require.Len(t, cost.Children, 1)
	assert.Equal(t, "grow", cost.Children[0].Name)
	assert.Equal(t, calls, cost.Children[0].Iters,
		"every ApplyFunc invocation the loop made is counted")
	assert.Equal(t, 4, cost.Children[0].Iters, "three rewrites plus the convergence check")
	assert.True(t, cost.Children[0].Changed)
}

// TestRunProfiledRecordsChangedPerInvocation is what makes a slow pipeline
// actionable: a pass that costs a full re-parse and rewrites nothing is the
// first candidate for removal, and it is invisible without a per-node verdict.
func TestRunProfiledRecordsChangedPerInvocation(t *testing.T) {
	p := nanopass.Sequence("seq",
		sleepPass("touches", 0, true),
		sleepPass("inert", 0, false),
	)
	_, cost, err := p.RunProfiled("SELECT 1")
	require.NoError(t, err)
	require.Len(t, cost.Children, 2)
	assert.True(t, cost.Children[0].Changed, "touches rewrote the body")
	assert.False(t, cost.Children[1].Changed, "inert returned its input")
}

// TestRunProfiledOmitsAConditionalThatDidNotRun documents the tree's meaning:
// it reports invocations, not membership. A skipped body is absent rather than
// listed at zero, so a reader cannot mistake "did not run" for "was free".
func TestRunProfiledOmitsAConditionalThatDidNotRun(t *testing.T) {
	never := nanopass.Conditional("gate",
		func(*env.Environment) bool { return false },
		sleepPass("body", 0, true))
	_, cost, err := nanopass.Sequence("seq", never).RunProfiled("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, []string{"seq", "  gate"}, stepNames(cost))
}

// TestRunProfiledKeepsThePartialTreeOnFailure — the tree ending at the pass
// that failed is the one worth having.
func TestRunProfiledKeepsThePartialTreeOnFailure(t *testing.T) {
	boom := nanopass.Pass{
		Name:  "boom",
		Apply: func(*env.Environment, string) (string, error) { return "", assert.AnError },
	}
	p := nanopass.Sequence("seq", sleepPass("ok", 0, false), boom, sleepPass("never", 0, false))
	_, cost, err := p.RunProfiled("SELECT 1")
	require.Error(t, err)
	assert.Equal(t, []string{"seq", "  ok", "  boom"}, stepNames(cost))
	require.Len(t, cost.Children, 2)
	assert.Error(t, cost.Children[1].Err)
	assert.False(t, cost.Children[1].Changed, "a failed pass rewrote nothing")
}

// TestStepCostSelfDur pins the accounting rule the panes rely on: a parent's
// duration contains its children's, so self time is the remainder and never
// negative.
func TestStepCostSelfDur(t *testing.T) {
	cost := nanopass.StepCost{
		Dur: 100 * time.Millisecond,
		Children: []nanopass.StepCost{
			{Dur: 60 * time.Millisecond},
			{Dur: 30 * time.Millisecond},
		},
	}
	assert.Equal(t, 10*time.Millisecond, cost.SelfDur())
	assert.Equal(t, time.Duration(0), nanopass.StepCost{Dur: 1, Children: []nanopass.StepCost{{Dur: 5}}}.SelfDur())
}

// TestRunProfiledIsGoroutineSafe guards the recorder's keying. Two profiled
// runs in flight at once must each collect their own tree; a recorder found by
// anything coarser than the run's own environment would merge them.
func TestRunProfiledIsGoroutineSafe(t *testing.T) {
	slow := nanopass.Sequence("slow", sleepPass("s1", 20*time.Millisecond, false), sleepPass("s2", 20*time.Millisecond, false))
	fast := nanopass.Sequence("fast", sleepPass("f1", 0, false))

	var wg sync.WaitGroup
	trees := make([]nanopass.StepCost, 2)
	for i, p := range []nanopass.Pass{slow, fast} {
		wg.Go(func() {
			for range 20 {
				_, cost, err := p.RunProfiled("SELECT 1")
				assert.NoError(t, err)
				trees[i] = cost
			}
		})
	}
	wg.Wait()
	assert.Equal(t, []string{"slow", "  s1", "  s2"}, stepNames(trees[0]))
	assert.Equal(t, []string{"fast", "  f1"}, stepNames(trees[1]))
}

// TestRunIsNotProfiled pins ADR-0192 §SD2: the execute path installs no
// recorder, so a plain Run in flight next to a profiled one contributes nothing
// to its tree.
func TestRunIsNotProfiled(t *testing.T) {
	other := nanopass.Sequence("other", sleepPass("o1", 0, false))
	target := nanopass.Sequence("target", sleepPass("t1", 5*time.Millisecond, false))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			_, err := other.Run("SELECT 1")
			assert.NoError(t, err)
		}
	}()
	_, cost, err := target.RunProfiled("SELECT 1")
	<-done
	require.NoError(t, err)
	assert.Equal(t, []string{"target", "  t1"}, stepNames(cost))
}
