package play

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// The footprint conformance test (ADR-0315 §SD2, law L9; ADR-0310 §SD11):
// for each command the corpus below runs, the resources the engine
// observed changing after it are among the command's declared writes. The
// allowlist names play's known gaps (ADR-0315 §SD8) and nothing else; it
// can only shrink: an entry the corpus does not observe fails the test, so
// a declaration fixed without its dated Update on ADR-0269 or ADR-0270
// cannot leave a stale entry behind.
//
// The window runs against a capture server, so a run lands a result and
// Live re-runs when a signal the query reads moves: the live re-run gap is
// exercised, not assumed. The frame is the window host's command stage
// and the run pipeline of play's Frame, without the drawing: it draws no
// panes, so what a drawn pane emits (a signal pane's publication, the
// projector's invalidation) is outside what it observes, and it skips the
// editor's preview debounce. One gap §SD8 names is not reached: a bundle
// open that runs the buffer needs a bundle host, and without one
// open_bundle is refused before it writes, so it has no allowlist entry.

// footprintAllowlist is "<op> <resource>" per known gap.
var footprintAllowlist = map[string]string{
	// Writing or deleting a signal re-runs the live query: the result
	// changes.
	"set_signal result":    "ADR-0315 §SD8: parameter and signal writes trigger live re-runs",
	"delete_signal result": "ADR-0315 §SD8: parameter and signal writes trigger live re-runs",
	// The parameter slots follow the buffer, as set_sql's Follows says;
	// the declaration understates it.
	"set_sql params": "ADR-0315 §SD8: effect declarations understating what follows a write",
	// A new result re-feeds the panes, as run's Follows says.
	"run panes": "ADR-0315 §SD8: effect declarations understating what follows a write",
}

type footprintCommand struct {
	op     string
	args   any
	writer string
}

// footprintSignal is a reserved signal the map pane owns and does not
// republish, seeded with zero: the query runs from the first frame, a task
// may write it, and deleting it falls back to the seed, so a written
// signal and a deleted one both move what the next run ships.
var footprintSignal = string(signalAreaMinX)

var footprintSql = "SELECT * FROM t WHERE n > {" + footprintSignal + ":UInt32}"

func footprintCorpus() []footprintCommand {
	return []footprintCommand{
		{opSetSql, SetSqlArgs{Sql: footprintSql}, "task:t"},
		// The person's Run is the first run; Live re-runs from it.
		{opRun, RunArgs{}, opwire.WriterPerson},
		{opSetSignal, SetSignalArgs{Name: footprintSignal, Value: "7"}, "task:t"},
		{opSetSignal, SetSignalArgs{Name: footprintSignal, Value: "8"}, "task:t"},
		{opDeleteSignal, DeleteSignalArgs{Name: footprintSignal}, "task:t"},
		{opSetParam, SetParamArgs{Name: footprintSignal, Value: "3"}, "task:t"},
		{opShowPane, ShowPaneArgs{Pane: "table"}, "task:t"},
		{opShowPane, ShowPaneArgs{Pane: "chart"}, "task:t"},
		{opSetRunOptions, SetRunOptionsArgs{Conditions: ptr(true)}, "task:t"},
		{opSetTimelineWindow, SetTimelineWindowArgs{Clear: true}, "task:t"},
		{opBindPane, BindPaneArgs{Pane: "chart"}, "task:t"},
		// No bundle host: refused, which exercises the command and
		// declares nothing.
		{opOpenBundle, OpenBundleArgs{Alias: "nowhere"}, "task:t"},
	}
}

func ptr[T any](v T) *T { return &v }

// footprintFrame runs one frame as the window host does: the engine's
// command stage, then play's frame short of drawing — the parse the editor
// debounces, the agent mark, the Live gate and the run — and the engine's
// attribution of what changed. A run started in the frame lands before the
// frame ends, so the next frame sees the result.
func footprintFrame(t *testing.T, l *PlayLauncher, eng *opengine.Engine) {
	t.Helper()
	eng.BeginFrame()
	eng.ApplyQueued()
	eng.TakeSnapshot()
	p := l.inner
	if slots, params, err := extractSlotsAndParams(p.sql); err == nil {
		p.refreshParamSlotsFromParse(slots, params)
	}
	p.checkAgentMark()
	p.frameSig = p.graph.signals()
	// The result the frame draws is the one the engine reads as the result
	// resource (ADR-0269 §SD4): a run that landed on its goroutine moves
	// it here, inside the frame.
	_, _, _, _, _, _, _, _, resultID := p.activeSnapshot()
	p.audioFollowResult(resultID)
	if p.shouldAutoRun() {
		p.requestRun = true
		p.runIsAuto = true
		p.noteAutoRunFired()
	}
	if p.requestRun && !p.graph.MainLoading() {
		p.requestRun = false
		auto := p.runIsAuto
		p.runIsAuto = false
		sub := p.requestSubquery
		p.requestSubquery = false
		p.runAgent = p.takeAgentForRun(auto)
		p.executeRun(auto, sub)
		p.runAgent = nil
	}
	require.Eventually(t, func() bool { return !p.graph.MainLoading() }, 2*time.Second, time.Millisecond)
	p.settleAgentMark()
	eng.EndFrame()
}

func TestCommandsChangeOnlyWhatTheyDeclare(t *testing.T) {
	srv, _ := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	inner := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	defer inner.graph.close()
	inner.liveMain = true
	l := &PlayLauncher{inner: inner}
	eng := opengine.New(playOps.Catalog(), l.Operations())
	footprintFrame(t, l, eng)
	var violations []string
	observed := map[string]bool{}
	for i, cmd := range footprintCorpus() {
		args, err := buscodec.Encode(cmd.args)
		require.NoError(t, err)
		since := eng.LogSeq()
		spec, ok := playOps.Catalog().Lookup(cmd.op)
		require.True(t, ok, cmd.op)
		// A write expects the revisions of what it writes, as a model that
		// read first would send them.
		expects := make(map[string]uint64, len(spec.Writes))
		for _, r := range spec.Writes {
			expects[r] = eng.Revisions()[r]
		}
		out := eng.Submit(cmd.op, opwire.CallRequest{V: opwire.WireVersion, CallId: fmt.Sprintf("c%d", i), Writer: cmd.writer,
			Args: args, Expects: expects})
		require.Equal(t, opwire.PhaseAccepted, out.Phase, "%s: %s", cmd.op, out.Reason)
		// The command applies on the next frame; a few more let what it
		// started settle.
		for range 4 {
			footprintFrame(t, l, eng)
		}
		// Applied, or refused by the app itself, which changes nothing and
		// still exercises the command; a conflict means the test sent the
		// wrong revisions.
		settled := false
		for _, e := range eng.LogSince(since) {
			if e.Op == cmd.op {
				settled = true
				require.NotEqual(t, opwire.PhaseConflict, e.Phase, "%s: %s", cmd.op, e.Reason)
			}
		}
		require.True(t, settled, "%s: no entry", cmd.op)
		for _, w := range eng.UndeclaredWrites(since) {
			key := w.Op + " " + w.Resource
			if _, known := footprintAllowlist[key]; known {
				observed[key] = true
				continue
			}
			violations = append(violations, key)
		}
	}
	slices.Sort(violations)
	violations = slices.Compact(violations)
	assert.Empty(t, violations, "a command changed a resource it does not declare:\n"+strings.Join(violations, "\n"))
	for key := range footprintAllowlist {
		assert.True(t, observed[key], "the allowlist names a gap the corpus did not observe: %s — fixed, it leaves with its dated Update", key)
	}
}
