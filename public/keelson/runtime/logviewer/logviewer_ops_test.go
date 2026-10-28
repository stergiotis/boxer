package logviewer

import (
	"errors"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// writeEvent hands the sink one event as zerolog would under the binary_log
// tag: a CBOR map with the standard field names. The tests do not build
// with that tag, so they encode the event themselves.
func writeEvent(t *testing.T, sink *logbridge.Sink, level zerolog.Level, appId string, msg string, fields map[string]any) {
	t.Helper()
	ev := map[string]any{
		zerolog.LevelFieldName:     level.String(),
		zerolog.MessageFieldName:   msg,
		zerolog.TimestampFieldName: sink.Decoded()*1000 + 1_700_000_000_000,
		logbridge.AppIdFieldName:   appId,
	}
	for k, v := range fields {
		ev[k] = v
	}
	p, err := cbor.Marshal(ev)
	require.NoError(t, err)
	_, err = sink.WriteLevel(level, p)
	require.NoError(t, err)
}

// opsFixture installs a sink holding four rows from two apps, and removes
// it when the test ends.
func opsFixture(t *testing.T) (sink *logbridge.Sink) {
	t.Helper()
	sink, err := logbridge.NewSink(factsstore.NewInMemoryFactsStore(), logbridge.Config{TailCapacity: 8})
	require.NoError(t, err)
	const play, imztop = "github.com/example/play", "github.com/example/imztop"
	writeEvent(t, sink, zerolog.DebugLevel, play, "starting", nil)
	writeEvent(t, sink, zerolog.InfoLevel, play, "query ok", map[string]any{"rows": 3})
	writeEvent(t, sink, zerolog.WarnLevel, imztop, "high cpu", nil)
	writeEvent(t, sink, zerolog.ErrorLevel, play, "connection refused",
		map[string]any{zerolog.ErrorFieldName: "dial tcp: connection refused", "host": "db"})
	require.Len(t, sink.Tail(0), 4)
	prev := sinkRef.Load()
	RegisterSink(sink)
	t.Cleanup(func() {
		RegisterSink(prev)
		_ = sink.Close()
	})
	return
}

func query[In any, Out any](t *testing.T, inst *LogViewerApp, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *LogViewerApp, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetCounters, opListRows, opGetRow, opGetView, opSetFilter, opSetFollow, opSelectRow} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestListRowsReadsItsOwnArgumentsNotTheView(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	inst.filterLevel = zerolog.ErrorLevel

	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)
	assert.Equal(t, 4, out.Matched, "the window's level floor is not the query's")
	require.Len(t, out.Rows, 4)
	assert.Equal(t, "starting", out.Rows[0].Message, "oldest first")
	assert.Equal(t, 1, out.Rows[1].Fields)
	assert.True(t, out.Rows[3].HasError)
	assert.Equal(t, 4, out.TailRows)
	assert.Equal(t, 8, out.TailCapacity)
	keys := map[string]bool{}
	for _, r := range out.Rows {
		assert.NotEmpty(t, r.Key)
		keys[r.Key] = true
	}
	assert.Len(t, keys, 4, "keys are distinct")

	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{App: "PLAY", Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Matched)
	require.Len(t, out.Rows, 1)
	assert.Equal(t, "connection refused", out.Rows[0].Message, "the limit keeps the newest")

	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "warn", Message: "cpu"})
	require.NoError(t, err)
	require.Len(t, out.Rows, 1)
	assert.Equal(t, "warn", out.Rows[0].Level)

	_, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "loud"})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, "trace, debug")
	_, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Limit: -1})
	assert.NotNil(t, refusal(err))
}

func TestListRowsIsBounded(t *testing.T) {
	sink := opsFixture(t)
	for range 10 {
		writeEvent(t, sink, zerolog.InfoLevel, "github.com/example/play", "more", nil)
	}
	inst := newInstance(manifest)
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Limit: opsMaxRows + 1})
	require.NoError(t, err)
	assert.Equal(t, 8, out.Matched, "the ring holds its capacity")
	assert.Len(t, out.Rows, 8)
	long := make([]byte, opsMaxMessageLen+10)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, cutText(string(long), opsMaxMessageLen), opsMaxMessageLen+len("…"))
}

func TestGetRowReturnsTheDetailAndRefusesAGoneKey(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	list, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "error"})
	require.NoError(t, err)
	require.Len(t, list.Rows, 1)

	d, err := query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Key: list.Rows[0].Key})
	require.NoError(t, err)
	assert.Equal(t, list.Rows[0].Key, d.Key)
	assert.Equal(t, "github.com/example/play", d.App)
	assert.Equal(t, "connection refused", d.Message)
	assert.Contains(t, d.Error, "connection refused")
	assert.Equal(t, 1, d.FieldsTotal)
	require.Len(t, d.Fields, 1)
	assert.Equal(t, FieldValue{Name: "host", Kind: "string", Value: "db"}, d.Fields[0])
	assert.False(t, d.Ts.IsZero())

	_, err = query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Key: "1-deadbeef"})
	require.NotNil(t, refusal(err))
	assert.Equal(t, rowGoneReason, refusal(err).Reason)
}

func TestDetailIsBounded(t *testing.T) {
	r := factsstore.LogRow{Message: "m"}
	for i := range opsMaxFields + 5 {
		r.Fields = append(r.Fields, factsstore.LogField{Name: "f", Kind: factsstore.LogFieldKindInt, Int: int64(i)})
	}
	facts := make([]factsstore.LogErrorFact, opsMaxFacts+3)
	r.ErrorContext = &factsstore.LogErrorContext{Streams: []factsstore.LogErrorStream{{Name: "no-stack", Facts: facts}}}
	d := detailRow("k", r)
	assert.Len(t, d.Fields, opsMaxFields)
	assert.Equal(t, opsMaxFields+5, d.FieldsTotal)
	require.Len(t, d.ErrorChain, 1)
	assert.Len(t, d.ErrorChain[0].Facts, opsMaxFacts)
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)

	in := SetFilterArgs{MinLevel: "warn", App: "play", Message: "refused"}
	for range 2 {
		require.NoError(t, command(t, inst, opSetFilter, in))
		assert.Equal(t, filterState{Level: zerolog.WarnLevel, AppId: "play", Message: "refused"}, inst.filterValue())
	}
	require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{}))
	assert.Equal(t, filterState{Level: zerolog.TraceLevel}, inst.filterValue(), "an empty level is the no-floor sentinel")
	assert.NotNil(t, refusal(command(t, inst, opSetFilter, SetFilterArgs{MinLevel: "loud"})))

	for range 2 {
		require.NoError(t, command(t, inst, opSetFollow, SetFollowArgs{Follow: false}))
		assert.False(t, inst.follow)
	}

	list, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "warn"})
	require.NoError(t, err)
	key := list.Rows[0].Key
	for range 2 {
		require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{Key: key}))
		assert.True(t, inst.hasSelected)
		assert.Equal(t, "high cpu", inst.selected.Message)
	}
	assert.Equal(t, key, inst.Operations().ResourceValue(resSelection))

	require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{MinLevel: "warn"}))
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, View{Installed: true, MinLevel: "warn", Follow: false, Selected: key, Matched: 2, Shown: 2}, v)

	assert.Equal(t, rowGoneReason, refusal(command(t, inst, opSelectRow, SelectRowArgs{Key: "1-deadbeef"})).Reason)
	assert.True(t, inst.hasSelected, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{}))
	assert.False(t, inst.hasSelected)
	assert.Equal(t, "", inst.Operations().ResourceValue(resSelection))
}

func TestUndoRestoresTheViewWhileTheRowIsInTheRing(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	h := inst.Operations()
	list, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)

	assert.True(t, h.Restore(resSelection, list.Rows[1].Key))
	assert.Equal(t, "query ok", inst.selected.Message)
	assert.False(t, h.Restore(resSelection, "1-deadbeef"), "a row that left the ring cannot come back")
	assert.True(t, h.Restore(resSelection, ""))
	assert.False(t, inst.hasSelected)
	assert.True(t, h.Restore(resFilter, filterState{Level: zerolog.InfoLevel, AppId: "a"}))
	assert.Equal(t, zerolog.InfoLevel, inst.filterLevel)
	assert.True(t, h.Restore(resFollow, false))
	assert.False(t, inst.follow)
	assert.False(t, h.Restore(resFollow, "no"))
}

func TestWithoutASinkTheTailIsUnavailable(t *testing.T) {
	prev := sinkRef.Load()
	RegisterSink(nil)
	t.Cleanup(func() { RegisterSink(prev) })
	inst := newInstance(manifest)
	snap := inst.Operations().Snapshot()
	for _, name := range []string{opListRows, opGetRow, opSelectRow} {
		ok, reason := snap.Available(name)
		assert.False(t, ok, name)
		assert.Equal(t, notInstalledReason, reason)
	}
	for _, name := range []string{opGetCounters, opGetView, opSetFilter, opSetFollow} {
		ok, _ := snap.Available(name)
		assert.True(t, ok, name)
	}
	cnt, err := query[struct{}, Counters](t, inst, opGetCounters, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, Counters{}, cnt)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.False(t, v.Installed)
	assert.Equal(t, notInstalledReason, refusal(command(t, inst, opSelectRow, SelectRowArgs{Key: "x"})).Reason)
}

func TestCountersReadTheSink(t *testing.T) {
	opsFixture(t)
	cnt, err := query[struct{}, Counters](t, newInstance(manifest), opGetCounters, struct{}{})
	require.NoError(t, err)
	assert.True(t, cnt.Installed)
	assert.Equal(t, 4, cnt.TailRows)
	assert.Equal(t, 8, cnt.TailCapacity)
	assert.Equal(t, uint64(4), cnt.Decoded)
}

func TestRowKeysFollowTheRow(t *testing.T) {
	ts := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	a := factsstore.LogRow{Ts: ts, Caller: "a.go:1", Message: "m"}
	b := factsstore.LogRow{Ts: ts, Caller: "a.go:1", Message: "n"}
	assert.Equal(t, opsRowKey(a), opsRowKey(a))
	assert.NotEqual(t, opsRowKey(a), opsRowKey(b), "coincident timestamps are told apart by caller and message")
	_, ok := findRow([]factsstore.LogRow{a, b}, opsRowKey(b))
	assert.True(t, ok)
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *LogViewerApp
	eng  *opengine.Engine
}

func newEngineWindow() (w *engineWindow) {
	inst := newInstance(manifest)
	eng := opengine.New(ops.Catalog(), inst.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	inst.frameCtx = ctx
	w = &engineWindow{inst: inst, eng: eng}
	w.frame(nil)
	return
}

func (inst *engineWindow) frame(body func()) {
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	if body != nil {
		body()
	}
	inst.eng.EndFrame()
}

func (inst *engineWindow) submit(t *testing.T, id string, op string, in any) (out opwire.Outcome) {
	t.Helper()
	args, err := buscodec.Encode(in)
	require.NoError(t, err)
	// The task read every resource: a revision never bumped is zero.
	revs := inst.eng.Revisions()
	expects := map[string]uint64{}
	for _, r := range []string{resFilter, resSelection, resFollow} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	opsFixture(t)
	w := newEngineWindow()

	// An agent's command is the task's, and writes only what it declares.
	out := w.submit(t, "c1", opSetFilter, SetFilterArgs{MinLevel: "warn"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resFilter))
	// Setting the value already set applies again (ADR-0269 §SD1).
	out = w.submit(t, "c2", opSetFilter, SetFilterArgs{MinLevel: "warn"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// Typing in a filter box commits inside the frame without a command, so
	// its writes are the app's (ADR-0345 §SD3); the last command declared
	// the filter, so the footprint check sees nothing undeclared.
	w.frame(func() { w.inst.filterAppId = "play" })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resFilter))
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// The level combo and the Follow checkbox are the person's, through the
	// same commands.
	w.frame(func() { w.inst.setLevelGesture(zerolog.InfoLevel) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFilter))
	log := w.eng.Log()
	assert.Equal(t, opSetFilter, log[len(log)-1].Op)
	// The Follow checkbox's binding lands at the frame's sync; the next
	// frame notices the draft and routes it as the person's.
	w.frame(func() { w.inst.followDraft = false })
	w.frame(func() { w.inst.noticeDrafts() })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFollow))
	assert.False(t, w.inst.follow)
	// An agent's set_follow moves the draft too, so no click is invented.
	out = w.submit(t, "f1", opSetFollow, SetFollowArgs{Follow: true})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	seq := w.eng.LogSeq()
	w.frame(func() { w.inst.noticeDrafts() })
	assert.Empty(t, w.eng.LogSince(seq), "a command's write is not re-routed as a gesture")
	assert.Equal(t, "task:t", w.eng.Writer(resFollow))

	// A row click selects through the command; a second click on the same
	// row clears, as the next state the click computed.
	tail, _ := liveTail()
	w.frame(func() { w.inst.selectGesture(tail[2], true) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, "high cpu", w.inst.selected.Message)
	w.frame(func() { w.inst.selectGesture(tail[2], false) })
	assert.False(t, w.inst.hasSelected)
	assert.Equal(t, "", w.inst.Operations().ResourceValue(resSelection))
}
