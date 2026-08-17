package play

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

// obsByName indexes a trace for assertions that do not care about position.
func obsByName(obs []passreg.ApplyObservation) map[string]passreg.ApplyObservation {
	m := make(map[string]passreg.ApplyObservation, len(obs))
	for _, o := range obs {
		m[o.Name] = o
	}
	return m
}

// TestRewriteTraceCoversTheWholeClientRewrite pins that the trace accounts for
// play's own degrade points as well as the registry stage — extract-params and
// set-format degrade exactly the way a registered pass does, and are no more
// visible in the result.
func TestRewriteTraceCoversTheWholeClientRewrite(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	obs := cl.RewriteTrace(`SET param_id = 7; SELECT LW_ID_BODY({id:UInt64})`)

	byName := obsByName(obs)
	for _, name := range []string{rewriteStepExtractParams, rewriteStepSetFormat} {
		o, ok := byName[name]
		if !ok {
			t.Fatalf("trace missing play's own %q step: %+v", name, obs)
		}
		if o.Outcome != passreg.ApplyOutcomeApplied {
			t.Errorf("%s outcome = %v, want applied", name, o.Outcome)
		}
		if !o.Changed {
			t.Errorf("%s must report Changed: it harvested/rewrote the statement", name)
		}
	}
	// The standard set's identsql expansion is a registered entry and must
	// appear between them, having rewritten the macro.
	found := false
	for _, o := range obs {
		if o.Name == rewriteStepExtractParams || o.Name == rewriteStepSetFormat {
			continue
		}
		if o.Outcome == passreg.ApplyOutcomeApplied && o.Changed {
			found = true
		}
	}
	if !found {
		t.Errorf("no registered pass reported rewriting the LW_ID_ macro: %+v", obs)
	}
	// The steps arrive in execution order: harvest, then the stage, then the
	// FORMAT rewrite last.
	if obs[0].Name != rewriteStepExtractParams {
		t.Errorf("first observation = %q, want %q", obs[0].Name, rewriteStepExtractParams)
	}
	if last := obs[len(obs)-1]; last.Name != rewriteStepSetFormat {
		t.Errorf("last observation = %q, want %q", last.Name, rewriteStepSetFormat)
	}
}

// TestRewriteTraceReportsSkippedPass is the gap this closes: a pass that fails
// is skipped, the statement ships anyway, and the only record of it used to be
// a warn line in the process log.
func TestRewriteTraceReportsSkippedPass(t *testing.T) {
	cl := NewClient(ClientConfig{URL: "http://127.0.0.1:1"}, nil)
	reg := passreg.NewRegistry()
	broken := nanopass.LiftBodyPass("Broken", func(string) (string, error) {
		return "", errors.New("grammar1 cannot model this")
	}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody})
	if err := reg.Register(passreg.Entry{Pass: broken, Stage: passreg.StagePreExecute, Order: 100}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cl.passes = reg

	body, _ := cl.BuildStatement(`SELECT 1`)
	if !strings.Contains(body, "SELECT 1") {
		t.Fatalf("a failing pass must not block the statement, got %q", body)
	}

	obs := cl.RewriteTrace(`SELECT 1`)
	skipped := skippedRewrites(obs)
	if len(skipped) != 1 {
		t.Fatalf("skippedRewrites = %+v, want exactly the broken pass", skipped)
	}
	if skipped[0].Name != "Broken" {
		t.Errorf("skipped name = %q, want %q", skipped[0].Name, "Broken")
	}
	if skipped[0].Err == nil || !strings.Contains(skipped[0].Err.Error(), "grammar1 cannot model this") {
		t.Errorf("skipped observation must carry the pass error, got %v", skipped[0].Err)
	}
	if got := rewriteOutcomeSummary(obs); !strings.Contains(got, "1 skipped") {
		t.Errorf("summary = %q, want it to count the skip", got)
	}
}

// TestRewriteTraceDoesNotChangeWhatShips guards the property that makes the
// trace worth showing: observing is passive, so the traced statement is the
// statement BuildStatement ships.
func TestRewriteTraceDoesNotChangeWhatShips(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	const sql = `SET param_id = 7; SELECT LW_ID_BODY({id:UInt64})`

	body, params := cl.BuildStatement(sql)
	var tracedBody string
	var tracedParams map[string]string
	tracedBody, tracedParams = cl.buildStatementObserved(sql, func(passreg.ApplyObservation) {})
	if body != tracedBody {
		t.Errorf("observed body diverged:\n plain = %q\n obs   = %q", body, tracedBody)
	}
	if len(params) != len(tracedParams) {
		t.Errorf("observed params diverged: %v vs %v", params, tracedParams)
	}
}

// awaitTrace drives rewriteTraceFor the way a render loop does — poll until the
// off-thread computation lands (ADR-0192 §SD3's 2026-08-17 update). The budget
// is sized for the -race lane, where this package runs roughly 4.6x slower;
// a rewrite of the test buffer is milliseconds in the plain lane.
func awaitTrace(t *testing.T, app *PlayApp) []passreg.ApplyObservation {
	t.Helper()
	var obs []passreg.ApplyObservation
	require.Eventually(t, func() bool {
		var ok bool
		obs, ok = app.rewriteTraceFor()
		return ok
	}, 30*time.Second, time.Millisecond, "the trace never landed")
	return obs
}

func TestRewriteTraceForMemoisesByRunBufferAndToggle(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	app := &PlayApp{client: cl, sql: `SELECT LW_ID_IS_VALID(id) FROM t`}

	// The first call kicks the computation and reports nothing yet — a pane
	// showing stale or absent numbers for one frame is the trade §SD3 takes for
	// not blocking the render thread.
	if obs, ok := app.rewriteTraceFor(); ok {
		t.Errorf("first call answered synchronously: %+v", obs)
	}
	if !app.rewriteTracePending() {
		t.Error("first call did not mark the trace pending")
	}
	if obs := awaitTrace(t, app); len(obs) == 0 {
		t.Fatal("want a trace once the computation lands")
	}
	if app.rewriteTracePending() {
		t.Error("still pending after the trace landed")
	}

	// Unchanged buffer → memo, no recompute. Poison the cached slice to
	// prove the second call did not rebuild it.
	app.rewriteTrace.obs = []passreg.ApplyObservation{{Name: "sentinel"}}
	again, _ := app.rewriteTraceFor()
	if len(again) != 1 || again[0].Name != "sentinel" {
		t.Errorf("recomputed on an unchanged buffer: %+v", again)
	}

	// The conditions toggle rewrites without an edit, so it must invalidate.
	cl.SetExposeConditions(true)
	if fresh := awaitTrace(t, app); len(fresh) == 1 && fresh[0].Name == "sentinel" {
		t.Error("conditions toggle did not invalidate the memo")
	}

	// A buffer change invalidates too.
	app.rewriteTrace.obs = []passreg.ApplyObservation{{Name: "sentinel"}}
	app.sql = `SELECT 2`
	if changed := awaitTrace(t, app); len(changed) == 1 && changed[0].Name == "sentinel" {
		t.Error("buffer change did not invalidate the memo")
	}
}

// TestRewriteTraceForSupersedesAnInFlightBuffer is the latest-wins guard: while
// a computation is running the buffer keeps moving, and a result landing for an
// abandoned buffer must not be published under the current one.
func TestRewriteTraceForSupersedesAnInFlightBuffer(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	app := &PlayApp{client: cl, sql: `SELECT LW_ID_IS_VALID(id) FROM t`}
	_, _ = app.rewriteTraceFor()

	app.sql = `SELECT 2`
	_, _ = app.rewriteTraceFor()
	_ = awaitTrace(t, app)

	app.rewriteTrace.mu.Lock()
	forSQL := app.rewriteTrace.forSQL
	app.rewriteTrace.mu.Unlock()
	if forSQL != `SELECT 2` {
		t.Errorf("published trace is keyed to %q; want the current buffer", forSQL)
	}
}

func TestRewriteTraceForGuards(t *testing.T) {
	// No client (tests, legacy CLI) → nothing to describe, no panic.
	if _, ok := (&PlayApp{sql: `SELECT 1`}).rewriteTraceFor(); ok {
		t.Error("reported a trace without a client")
	}
	// Empty buffer → nothing to describe.
	cl := newTestClientWithStandardSet(t)
	if _, ok := (&PlayApp{client: cl, sql: "   "}).rewriteTraceFor(); ok {
		t.Error("reported a trace for an empty buffer")
	}
}

func TestRewriteOutcomeTextAndSummary(t *testing.T) {
	obs := []passreg.ApplyObservation{
		{Name: "A", Outcome: passreg.ApplyOutcomeApplied, Changed: true},
		{Name: "B", Outcome: passreg.ApplyOutcomeApplied},
		{Name: "C", Outcome: passreg.ApplyOutcomeSkipped, Err: errors.New("boom")},
		{Name: "D", Outcome: passreg.ApplyOutcomeDeclined, LateBound: true},
	}
	if got, want := rewriteOutcomeSummary(obs), "2 applied (1 rewrote) · 1 skipped · 1 declined"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if got := rewriteOutcomeText(obs[0]); !strings.Contains(got, "rewrote") {
		t.Errorf("applied+changed text = %q", got)
	}
	if got := rewriteOutcomeText(obs[1]); !strings.Contains(got, "unchanged") {
		t.Errorf("applied text = %q", got)
	}
	if got := rewriteOutcomeText(obs[2]); !strings.Contains(got, "SKIPPED") {
		t.Errorf("skipped text = %q", got)
	}
	if got := rewriteOutcomeText(obs[3]); !strings.Contains(got, "declined") {
		t.Errorf("declined text = %q", got)
	}

	// A clean trace says so without listing anything.
	clean := []passreg.ApplyObservation{{Name: "A", Outcome: passreg.ApplyOutcomeApplied}}
	if got := skippedRewrites(clean); got != nil {
		t.Errorf("skippedRewrites on a clean trace = %+v, want nil", got)
	}
	if got, want := rewriteOutcomeSummary(clean), "1 applied (0 rewrote)"; got != want {
		t.Errorf("clean summary = %q, want %q", got, want)
	}
}
