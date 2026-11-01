package logviewer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

const fixtureRun = "run-now"

// fakeStore stands in for boxer.facts: rows newest first, every query
// filter applied as the store applies it, and each query recorded.
type fakeStore struct {
	mu      sync.Mutex
	rows    []Entry
	queries []Query
	fail    error
}

func (inst *fakeStore) read(_ context.Context, q Query) (out []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.queries = append(inst.queries, q)
	if inst.fail != nil {
		err = inst.fail
		return
	}
	for _, e := range inst.rows {
		r := e.Row
		switch {
		case len(q.Levels) > 0 && !slices.Contains(q.Levels, r.Level),
			q.App != "" && !strings.Contains(strings.ToLower(string(r.AppId)), strings.ToLower(q.App)),
			q.Message != "" && !strings.Contains(strings.ToLower(r.Message), strings.ToLower(q.Message)),
			q.RunId != "" && e.RunId != q.RunId,
			q.InstanceKey != 0 && r.InstanceKey != q.InstanceKey,
			r.Ts.Before(q.Since):
			continue
		}
		out = append(out, e)
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
	}
	return
}

func (inst *fakeStore) prepend(e Entry) {
	inst.mu.Lock()
	inst.rows = append([]Entry{e}, inst.rows...)
	inst.mu.Unlock()
}

func (inst *fakeStore) lastQuery() (q Query, n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = len(inst.queries)
	if n > 0 {
		q = inst.queries[n-1]
	}
	return
}

// opsFixture registers a source over a store holding four rows of this
// run from two apps, newest first, and one row of an earlier run; it
// removes the source when the test ends.
func opsFixture(t *testing.T) (store *fakeStore, sink *logbridge.Sink) {
	t.Helper()
	sink, err := logbridge.NewSink(factsstore.NewInMemoryFactsStore(), logbridge.Config{})
	require.NoError(t, err)
	const play, imztop = "github.com/example/play", "github.com/example/imztop"
	t0 := time.Now().UTC().Add(-time.Minute)
	store = &fakeStore{rows: []Entry{
		{RunId: fixtureRun, Row: factsstore.LogRow{AppId: play, Level: "error", Message: "connection refused",
			Error: "dial tcp: connection refused", InstanceKey: 4, Ts: t0.Add(3 * time.Second),
			Fields: []factsstore.LogField{{Name: "host", Kind: factsstore.LogFieldKindString, Str: "db"}}}},
		{RunId: fixtureRun, Row: factsstore.LogRow{AppId: imztop, Level: "warn", Message: "high cpu", Ts: t0.Add(2 * time.Second)}},
		{RunId: fixtureRun, Row: factsstore.LogRow{AppId: play, Level: "info", Message: "query ok", Ts: t0.Add(time.Second),
			Fields: []factsstore.LogField{{Name: "rows", Kind: factsstore.LogFieldKindInt, Int: 3}}}},
		{RunId: fixtureRun, Row: factsstore.LogRow{AppId: play, Level: "debug", Message: "starting", Ts: t0}},
		{RunId: "run-before", Row: factsstore.LogRow{AppId: play, Level: "info", Message: "yesterday", Ts: t0.Add(-time.Hour)}},
	}}
	prev := sourceRef.Load()
	Register(&Source{Sink: sink, Read: store.read, Run: func() string { return fixtureRun }})
	t.Cleanup(func() {
		Register(prev)
		_ = sink.Close()
	})
	return
}

// fill runs the window's refresh as a frame does — start it, let the
// worker read, adopt the result on the next tick — under the window's
// current filter.
func fill(t *testing.T, inst *LogViewerApp) {
	t.Helper()
	want := inst.filterValue()
	now := time.Now()
	inst.cache.refreshNow = true
	// A changed scope waits to settle; ticking past the settle starts it.
	inst.cache.tick(now, sourceRef.Load(), want)
	inst.cache.tick(now.Add(scopeSettle), sourceRef.Load(), want)
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
	inst.cache.tick(now.Add(scopeSettle), sourceRef.Load(), want)
}

// writeEvent hands the sink one event as zerolog would under the binary_log
// tag: a CBOR map with the standard field names. The tests do not build
// with that tag, so they encode the event themselves.
func writeEvent(t *testing.T, sink *logbridge.Sink, level zerolog.Level, msg string) {
	t.Helper()
	p, err := cbor.Marshal(map[string]any{
		zerolog.LevelFieldName:   level.String(),
		zerolog.MessageFieldName: msg,
	})
	require.NoError(t, err)
	_, err = sink.WriteLevel(level, p)
	require.NoError(t, err)
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

// The window's first refresh reads this run with no level floor, bounded
// by the look-back and the row cap; the frame never reads the store.
func TestTheCacheReadsTheStoreOffTheFrame(t *testing.T) {
	store, _ := opsFixture(t)
	inst := newInstance(manifest)
	_, n := store.lastQuery()
	require.Zero(t, n, "constructing a window reads nothing")
	fill(t, inst)
	q, n := store.lastQuery()
	assert.Equal(t, 1, n)
	assert.Equal(t, fixtureRun, q.RunId)
	assert.Empty(t, q.Levels)
	assert.Equal(t, cacheCap(), q.Limit)
	assert.WithinDuration(t, time.Now().Add(-lookBack), q.Since, time.Minute)
	require.Len(t, inst.cache.rows, 4, "the earlier run is outside the window's scope")
	assert.Equal(t, "connection refused", inst.cache.rows[0].Row.Message, "newest first")
	assert.False(t, inst.cache.refreshedAt.IsZero())
}

// A changed filter is read once it has settled, with every filter in the
// query; the interval and Refresh read again under the same filter.
func TestTheCacheRefetchesOnAFilterChangeTheIntervalAndRefresh(t *testing.T) {
	store, _ := opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	src := sourceRef.Load()
	now := time.Now()

	inst.applyFilter(filterState{Level: zerolog.WarnLevel, AppId: "play", Message: "refused", AllRuns: true, Instance: 4})
	inst.cache.tick(now, src, inst.filterValue())
	inst.cache.tick(now.Add(scopeSettle/2), src, inst.filterValue())
	_, n := store.lastQuery()
	assert.Equal(t, 1, n, "a filter still being typed is not read")
	inst.cache.tick(now.Add(scopeSettle), src, inst.filterValue())
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
	inst.cache.tick(now.Add(scopeSettle), src, inst.filterValue())
	q, n := store.lastQuery()
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{"warn", "error", "fatal", "panic"}, q.Levels)
	assert.Equal(t, "play", q.App)
	assert.Equal(t, "refused", q.Message)
	assert.Empty(t, q.RunId, "every run")
	assert.EqualValues(t, 4, q.InstanceKey)
	require.Len(t, inst.cache.rows, 1)
	assert.Equal(t, inst.filterValue(), inst.cache.scope)

	// Nothing is read again before the interval.
	inst.cache.tick(now.Add(scopeSettle+time.Millisecond), src, inst.filterValue())
	_, n = store.lastQuery()
	assert.Equal(t, 2, n)
	// Refresh reads at once.
	inst.cache.refreshNow = true
	inst.cache.tick(now.Add(scopeSettle+2*time.Millisecond), src, inst.filterValue())
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
	_, n = store.lastQuery()
	assert.Equal(t, 3, n)
	// So does the interval, from when the last read began.
	inst.cache.tick(inst.cache.started.Add(refreshInterval()), src, inst.filterValue())
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
	_, n = store.lastQuery()
	assert.Equal(t, 4, n)
}

// A failed refresh keeps the rows of the one before, says why, and is
// retried on the interval rather than every frame.
func TestAFailedRefreshKeepsTheRowsAndSaysWhy(t *testing.T) {
	store, _ := opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	store.mu.Lock()
	store.fail = errors.New("connection refused by ClickHouse")
	store.mu.Unlock()
	fill(t, inst)
	inst.cache.tick(time.Now(), sourceRef.Load(), inst.filterValue())
	require.Error(t, inst.cache.lastErr)
	assert.Len(t, inst.cache.rows, 4)
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)
	assert.Len(t, out.Rows, 4)
	assert.Contains(t, out.Diagnostic, "the last refresh failed")
	assert.Contains(t, out.Diagnostic, "connection refused by ClickHouse")
}

func TestListRowsReadsItsOwnArgumentsNotTheView(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	inst.filterLevel = zerolog.ErrorLevel

	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)
	assert.Equal(t, 4, out.Matched, "the window's level floor is not the query's")
	require.Len(t, out.Rows, 4)
	assert.Equal(t, "starting", out.Rows[0].Message, "oldest first")
	assert.Equal(t, 1, out.Rows[1].Fields)
	assert.True(t, out.Rows[3].HasError)
	assert.Equal(t, 4, out.TailRows)
	assert.Equal(t, cacheCap(), out.TailCapacity)
	assert.True(t, out.Persisted)
	assert.False(t, out.RefreshedAt.IsZero())
	assert.GreaterOrEqual(t, out.AgeMs, int64(0))
	assert.Empty(t, out.Diagnostic)
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

// The cache holds what the window's filter kept; arguments wider than
// that are answered from it, and the result says rows may be missing.
func TestListRowsSaysWhenItsArgumentsReachOutsideTheCache(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	inst.applyFilter(filterState{Level: zerolog.WarnLevel})
	fill(t, inst)
	require.Len(t, inst.cache.rows, 2)

	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)
	assert.Equal(t, 2, out.Matched)
	assert.Contains(t, out.Diagnostic, "wider than the filter")
	assert.Equal(t, CacheScope{MinLevel: "warn"}, out.Scope)

	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "error"})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Matched)
	assert.Empty(t, out.Diagnostic, "a narrower query is answered in full")
}

func TestListRowsIsBounded(t *testing.T) {
	store, _ := opsFixture(t)
	for range 300 {
		store.prepend(Entry{RunId: fixtureRun, Row: factsstore.LogRow{AppId: "github.com/example/play", Level: "info",
			Message: "more", Ts: time.Now()}})
	}
	inst := newInstance(manifest)
	fill(t, inst)
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Limit: opsMaxRows + 1})
	require.NoError(t, err)
	assert.Equal(t, 304, out.Matched)
	assert.Len(t, out.Rows, opsMaxRows)
	long := make([]byte, opsMaxMessageLen+10)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, appops.CutText(string(long), opsMaxMessageLen), opsMaxMessageLen+len("…"))
}

func TestGetRowReturnsTheDetailAndRefusesAGoneKey(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	list, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{MinLevel: "error"})
	require.NoError(t, err)
	require.Len(t, list.Rows, 1)

	d, err := query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Key: list.Rows[0].Key})
	require.NoError(t, err)
	assert.Equal(t, list.Rows[0].Key, d.Key)
	assert.Equal(t, "github.com/example/play", d.App)
	assert.Equal(t, "connection refused", d.Message)
	assert.Contains(t, d.Error, "connection refused")
	assert.Equal(t, fixtureRun, d.RunId)
	assert.EqualValues(t, 4, d.InstanceKey)
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
	d := detailRow("k", Entry{Row: r})
	assert.Len(t, d.Fields, opsMaxFields)
	assert.Equal(t, opsMaxFields+5, d.FieldsTotal)
	require.Len(t, d.ErrorChain, 1)
	assert.Len(t, d.ErrorChain[0].Facts, opsMaxFacts)
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)

	in := SetFilterArgs{MinLevel: "warn", App: "play", Message: "refused", AllRuns: true, Instance: 4}
	for range 2 {
		require.NoError(t, command(t, inst, opSetFilter, in))
		assert.Equal(t, filterState{Level: zerolog.WarnLevel, AppId: "play", Message: "refused", AllRuns: true, Instance: 4}, inst.filterValue())
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
	assert.True(t, inst.cache.refreshedAt.Equal(v.RefreshedAt))
	v.RefreshedAt = time.Time{}
	assert.Equal(t, View{Installed: true, Persisted: true, MinLevel: "warn", Follow: false, Selected: key,
		Matched: 2, Shown: 2}, v)

	assert.Equal(t, rowGoneReason, refusal(command(t, inst, opSelectRow, SelectRowArgs{Key: "1-deadbeef"})).Reason)
	assert.True(t, inst.hasSelected, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{}))
	assert.False(t, inst.hasSelected)
	assert.Equal(t, "", inst.Operations().ResourceValue(resSelection))
}

func TestUndoRestoresTheViewWhileTheRowIsInTheCache(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	h := inst.Operations()
	list, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)

	assert.True(t, h.Restore(resSelection, list.Rows[1].Key))
	assert.Equal(t, "query ok", inst.selected.Message)
	assert.False(t, h.Restore(resSelection, "1-deadbeef"), "a row the cache does not hold cannot come back")
	assert.True(t, h.Restore(resSelection, ""))
	assert.False(t, inst.hasSelected)
	assert.True(t, h.Restore(resFilter, filterState{Level: zerolog.InfoLevel, AppId: "a"}))
	assert.Equal(t, zerolog.InfoLevel, inst.filterLevel)
	assert.True(t, h.Restore(resFollow, false))
	assert.False(t, inst.follow)
	assert.False(t, h.Restore(resFollow, "no"))
}

// A host without BOXER_LOG_FACTS has nothing to read: list_rows answers an
// empty set and says why, a row cannot be read or selected, and the view
// and the counters still answer.
func TestWithoutPersistedLogsListRowsSaysWhy(t *testing.T) {
	prev := sourceRef.Load()
	src := &Source{NotPersisted: "BOXER_LOG_FACTS is not set"}
	Register(src)
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	inst.cache.tick(time.Now(), src, inst.filterValue())
	assert.False(t, inst.cache.runner.Running(), "nothing is read")

	snap := inst.Operations().Snapshot()
	for _, name := range []string{opGetRow, opSelectRow} {
		ok, reason := snap.Available(name)
		assert.False(t, ok, name)
		assert.Equal(t, notPersistedReason(src), reason)
	}
	for _, name := range []string{opListRows, opGetCounters, opGetView, opSetFilter, opSetFollow} {
		ok, _ := snap.Available(name)
		assert.True(t, ok, name)
	}
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{})
	require.NoError(t, err)
	assert.Empty(t, out.Rows)
	assert.False(t, out.Persisted)
	assert.EqualValues(t, -1, out.AgeMs)
	assert.Contains(t, out.Diagnostic, "BOXER_LOG_FACTS is not set")
	assert.Contains(t, out.Diagnostic, "set BOXER_LOG_FACTS=1")

	cnt, err := query[struct{}, Counters](t, inst, opGetCounters, struct{}{})
	require.NoError(t, err)
	assert.False(t, cnt.Installed)
	assert.False(t, cnt.Persisted)
	assert.Zero(t, cnt.Decoded)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.False(t, v.Installed)
	assert.False(t, v.Persisted)
	assert.Equal(t, notPersistedReason(src), refusal(command(t, inst, opSelectRow, SelectRowArgs{Key: "x"})).Reason)
}

func TestCountersReadTheSinkAndTheCache(t *testing.T) {
	_, sink := opsFixture(t)
	writeEvent(t, sink, zerolog.InfoLevel, "counted")
	inst := newInstance(manifest)
	fill(t, inst)
	cnt, err := query[struct{}, Counters](t, inst, opGetCounters, struct{}{})
	require.NoError(t, err)
	assert.True(t, cnt.Installed)
	assert.True(t, cnt.Persisted)
	assert.Equal(t, 4, cnt.TailRows)
	assert.Equal(t, cacheCap(), cnt.TailCapacity)
	assert.Equal(t, uint64(1), cnt.Decoded)
	assert.False(t, cnt.RefreshedAt.IsZero())
}

func TestRowKeysFollowTheRow(t *testing.T) {
	ts := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	a := factsstore.LogRow{Ts: ts, Caller: "a.go:1", Message: "m"}
	b := factsstore.LogRow{Ts: ts, Caller: "a.go:1", Message: "n"}
	assert.Equal(t, opsRowKey(a), opsRowKey(a))
	assert.NotEqual(t, opsRowKey(a), opsRowKey(b), "coincident timestamps are told apart by caller and message")
	_, ok := findRow([]Entry{{Row: a}, {Row: b}}, opsRowKey(b))
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
	fill(t, w.inst)

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

	// The level combo, the run toggle and the Follow checkbox are the
	// person's, through the same commands.
	w.frame(func() { w.inst.setLevelGesture(zerolog.InfoLevel) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFilter))
	log := w.eng.Log()
	assert.Equal(t, opSetFilter, log[len(log)-1].Op)
	w.frame(func() { w.inst.setAllRunsGesture(true) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFilter))
	assert.True(t, w.inst.filterAllRuns)
	assert.Equal(t, "play", w.inst.filterAppId, "the toggle keeps the rest of the filter")
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
	warn := w.inst.cache.rows[1].Row
	w.frame(func() { w.inst.selectGesture(warn, true) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, "high cpu", w.inst.selected.Message)
	w.frame(func() { w.inst.selectGesture(warn, false) })
	assert.False(t, w.inst.hasSelected)
	assert.Equal(t, "", w.inst.Operations().ResourceValue(resSelection))
}

// The table draws the cache oldest first, and narrowing the filter row
// narrows the rows at once while the read under it is pending.
func TestTheTableShowsTheCacheOldestFirst(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	fill(t, inst)
	rows := chronological{newest: inst.shownRows()}
	require.Equal(t, 4, rows.len())
	assert.Equal(t, "starting", rows.at(0).Message)
	assert.Equal(t, "connection refused", rows.at(3).Message)
	inst.filterMessage = "cpu"
	require.Len(t, inst.shownRows(), 1)
	assert.Equal(t, "high cpu", inst.shownRows()[0].Row.Message)

	// Read every run, then switch back: the earlier run's row leaves at
	// once, before the read under This run lands.
	inst.filterMessage = ""
	inst.filterAllRuns = true
	fill(t, inst)
	require.Len(t, inst.shownRows(), 5)
	inst.filterAllRuns = false
	assert.Len(t, inst.shownRows(), 4)
	inst.filterInstance = 4
	require.Len(t, inst.shownRows(), 1)
	assert.Equal(t, "connection refused", inst.shownRows()[0].Row.Message)
}

// The run is read at each refresh, so a run the host adopts after the
// source was registered scopes the next read.
func TestTheRunIsReadAtQueryTime(t *testing.T) {
	store, _ := opsFixture(t)
	run := ""
	src := *sourceRef.Load()
	src.Run = func() string { return run }
	Register(&src)
	inst := newInstance(manifest)
	fill(t, inst)
	q, _ := store.lastQuery()
	assert.Empty(t, q.RunId, "an unknown run reads every run")
	run = fixtureRun
	fill(t, inst)
	q, _ = store.lastQuery()
	assert.Equal(t, fixtureRun, q.RunId)
}

func TestBuildQueryListsTheLevelsAtOrAboveTheFloor(t *testing.T) {
	now := time.Now()
	q := buildQuery(filterState{Level: zerolog.ErrorLevel}, "r", now, 10)
	assert.Equal(t, []string{"error", "fatal", "panic"}, q.Levels)
	assert.Equal(t, "r", q.RunId)
	assert.Equal(t, 10, q.Limit)
	q = buildQuery(filterState{Level: zerolog.TraceLevel, AllRuns: true}, "r", now, 10)
	assert.Empty(t, q.Levels)
	assert.Empty(t, q.RunId)
	assert.True(t, covers(filterState{Level: zerolog.WarnLevel, AppId: "pl"}, zerolog.ErrorLevel, "play", ""))
	assert.False(t, covers(filterState{Level: zerolog.WarnLevel}, zerolog.InfoLevel, "", ""))
	assert.False(t, covers(filterState{Message: "x"}, zerolog.TraceLevel, "", "y"))
}
