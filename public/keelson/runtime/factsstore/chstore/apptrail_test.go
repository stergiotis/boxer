package chstore_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	evaudit "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// newLiveRunStore is newLiveStore with a run identity, so the rows it
// writes are attributed to a run as a host's are (ADR-0191 §SD3).
func newLiveRunStore(t *testing.T, runId string) (s *chstore.Store, cleanup func()) {
	t.Helper()
	cfg := chstore.ConfigFromEnv()
	cfg.Database = "runtime_chstore_test"
	cfg.RunId = runId
	ctx := context.Background()
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	if err := s.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	require.NoError(t, s.DropTable(ctx))
	require.NoError(t, s.SetupTable(ctx, "MergeTree() ORDER BY tuple()"))
	cleanup = func() { _ = s.DropTable(context.Background()) }
	return
}

// Sessions pair across runs: a closed one carries both times and its
// reason, an open one no stop, and rows older than Since leave a session
// with no start rather than dropping it.
func TestStore_AppRuns_PairsAcrossRuns_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	write := func(run string, id string, key uint64, phase factsstore.AppLifecyclePhaseE, reason string, at time.Time) {
		_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{RunId: run, AppId: app.AppIdT("github.com/example/" + id),
			TileKey: key, Phase: phase, StopReason: reason, Ts: at})
		require.NoError(t, err)
	}
	write("run-a", "play", 1, factsstore.AppLifecyclePhaseStarted, "", t0)
	write("run-a", "play", 1, factsstore.AppLifecyclePhaseStopped, "user-close", t0.Add(time.Minute))
	write("run-b", "play", 1, factsstore.AppLifecyclePhaseStarted, "", t0.Add(2*time.Minute))
	write("run-b", "tally", 3, factsstore.AppLifecyclePhaseStopped, "shutdown", t0.Add(3*time.Minute))
	write("run-b", "tally", 3, factsstore.AppLifecyclePhaseStarted, "", t0.Add(-48*time.Hour))
	// Two sessions from before runs were recorded: without a run they cannot
	// be told apart, and must not fold into one.
	write("", "old", 1, factsstore.AppLifecyclePhaseStarted, "", t0)
	write("", "old", 1, factsstore.AppLifecyclePhaseStopped, "user-close", t0.Add(4*time.Minute))

	// run-b's process was last heard from after its open session started;
	// run-a wrote no heartbeat at all.
	for _, at := range []time.Time{t0.Add(3 * time.Minute), t0.Add(5 * time.Minute)} {
		_, err := s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{RunId: "run-b", Ts: at})
		require.NoError(t, err)
	}

	rows, err := s.AppRuns(context.Background(), factsstore.AppTrailFilter{Since: t0.Add(-time.Minute)})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, t0.Add(5*time.Minute), rows[1].RunSeenAt, "the open session's process was last seen at its latest heartbeat")
	assert.True(t, rows[2].RunSeenAt.IsZero(), "a process that wrote no heartbeat has no seen time")

	assert.Equal(t, "github.com/example/tally", string(rows[0].AppId), "most recent first")
	assert.True(t, rows[0].StartedAt.IsZero(), "the start fell before Since")
	assert.Equal(t, "shutdown", rows[0].StopReason)

	assert.Equal(t, "run-b", rows[1].RunId)
	assert.Equal(t, t0.Add(2*time.Minute), rows[1].StartedAt)
	assert.True(t, rows[1].StoppedAt.IsZero(), "still open")

	assert.Equal(t, "run-a", rows[2].RunId)
	assert.EqualValues(t, 1, rows[2].InstanceKey)
	assert.Equal(t, t0, rows[2].StartedAt)
	assert.Equal(t, t0.Add(time.Minute), rows[2].StoppedAt)
	assert.Equal(t, "user-close", rows[2].StopReason)
}

// The tail is per app: a chatty app keeps its newest rows and cannot push a
// quiet one out, and each row carries its run and window.
func TestStore_AppLogTail_PerApp_LiveCH(t *testing.T) {
	s, cleanup := newLiveRunStore(t, "run-logs")
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := range 5 {
		_, err := s.WriteLog(factsstore.LogRow{AppId: "chatty", InstanceKey: 7, Level: "info",
			Message: string(rune('a' + i)), Caller: "c.go:1", Ts: t0.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	_, err := s.WriteLog(factsstore.LogRow{AppId: "quiet", Level: "error", Message: "only", Error: "boom",
		Stack: "never read", Ts: t0})
	require.NoError(t, err)

	rows, err := s.AppLogTail(context.Background(), factsstore.AppTrailFilter{}, 2)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	byApp := map[string][]factsstore.AppLogRow{}
	for _, r := range rows {
		byApp[string(r.AppId)] = append(byApp[string(r.AppId)], r)
	}
	require.Len(t, byApp["chatty"], 2)
	assert.Equal(t, "e", byApp["chatty"][0].Message, "newest first")
	assert.Equal(t, "d", byApp["chatty"][1].Message)
	assert.EqualValues(t, 7, byApp["chatty"][0].InstanceKey)
	assert.Equal(t, "run-logs", byApp["chatty"][0].RunId)
	require.Len(t, byApp["quiet"], 1)
	assert.Equal(t, "boom", byApp["quiet"][0].Error)

	_, err = s.AppLogTail(context.Background(), factsstore.AppTrailFilter{}, 0)
	assert.Error(t, err)
}

// writeTrailEvents lands audit events on the live store's table as the
// trail's recorder writes them: an origin and the event, one row each.
func writeTrailEvents(t *testing.T, rows []evaudit.Event) {
	t.Helper()
	exec, err := storeexec.New(chclient.New(chclient.ConfigFromEnv(), nil), nil)
	require.NoError(t, err)
	st := trail.NewTrailStore(exec, nil, trail.TrailStoreConfig{Table: "runtime_chstore_test.facts"})
	defer st.Close()
	for i, r := range rows {
		id := uint64(i + 1)
		b := st.Begin(id, r.At, trail.TrailEnvelope{NaturalKey: []byte("event-" + strconv.Itoa(i))})
		o := r.Origin
		o.Id = id
		b.AddOrigin(o)
		e := r.Row
		e.Id, e.Kind = id, "auditEvent"
		b.AddAuditEvent(e)
		require.NoError(t, b.Commit())
	}
	_, err = st.Flush(context.Background())
	require.NoError(t, err)
}

// The bus's requests, audit events of the bus domain, fold per (app,
// subject, result), the result the event's outcome; the mean skips
// requests that recorded no latency, and an audit event of another domain
// is not a bus request.
func TestStore_AppAuditSummary_Folds_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	origin := evaudit.Origin{Run: "run-bus", App: "reader", Instance: 3}
	var rows []evaudit.Event
	for i, lat := range []uint32{10, 20, 0} {
		rows = append(rows, evaudit.Event{At: t0.Add(time.Duration(i) * time.Second), Origin: origin, Row: busaudit.EventOf(busaudit.AuditRecord{
			Subject: "keelson.query.apps", Result: busaudit.AuditResultOk, LatencyMs: lat, RequestSizeB: 9})})
	}
	rows = append(rows,
		evaudit.Event{At: t0, Origin: origin, Row: busaudit.EventOf(busaudit.AuditRecord{Subject: "keelson.query.apps", Result: busaudit.AuditResultDenied})},
		evaudit.Event{At: t0, Origin: origin, Row: busaudit.EventOf(busaudit.AuditRecord{Subject: "keelson.query.apps", Result: busaudit.AuditResultTimeout, LatencyMs: 5})},
		evaudit.Event{At: t0, Origin: origin, Row: evaudit.AuditEvent{Domain: "dmdm", Action: "request", Outcome: evaudit.OutcomeOk,
			PrincipalBy: evaudit.PrincipalByNone, OriginBy: "self", Retention: "disclosure",
			RefTypes: []string{busaudit.BusRefSubject}, RefValues: []string{"keelson.query.apps"}}},
	)
	writeTrailEvents(t, rows)

	got, err := s.AppAuditSummary(context.Background(), factsstore.AppTrailFilter{})
	require.NoError(t, err)
	require.Len(t, got, 3, "the bus domain's requests only")
	ok := got[0]
	assert.Equal(t, "reader", string(ok.AppId))
	assert.Equal(t, "keelson.query.apps", ok.Subject)
	assert.Equal(t, "ok", ok.Result)
	assert.EqualValues(t, 3, ok.Requests)
	assert.InDelta(t, 15.0, ok.MeanLatencyMs, 1e-9)
	assert.EqualValues(t, 20, ok.MaxLatencyMs)
	assert.Equal(t, t0, ok.FirstAt)
	assert.Equal(t, t0.Add(2*time.Second), ok.LastAt)
	assert.Equal(t, "denied", got[1].Result)
	assert.Equal(t, "keelson.query.apps", got[1].Subject)
	assert.Zero(t, got[1].MeanLatencyMs)
	assert.Equal(t, "timeout", got[2].Result, "a timeout keeps its own outcome")
	assert.InDelta(t, 5.0, got[2].MeanLatencyMs, 1e-9)
}

// The log viewer's read returns whole rows newest first — envelope, run,
// window and every typed field — and applies each filter in the query, so
// the limit counts matching rows. It has its own database so it does not
// race the other live tests' table resets.
func TestStore_LogRows_RoundTripAndFilters_LiveCH(t *testing.T) {
	cfg := chstore.ConfigFromEnv()
	cfg.Database = "runtime_chstore_logrows_test"
	cfg.RunId = "run-a"
	ctx := context.Background()
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	if err := s.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	require.NoError(t, s.DropTable(ctx))
	require.NoError(t, s.SetupTable(ctx, "MergeTree() ORDER BY tuple()"))
	defer func() { _ = s.DropTable(context.Background()) }()

	t0 := time.Date(2026, 10, 30, 9, 0, 0, 123456789, time.UTC)
	at := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	full := factsstore.LogRow{AppId: "github.com/example/play", InstanceKey: 7, Level: "error",
		Message: `it's a \ "quoted" message`, Caller: "p.go:9", Service: "svc", Error: "boom", Stack: "a\nb",
		Ts: t0.Add(3 * time.Second),
		Fields: []factsstore.LogField{
			{Name: "s", Kind: factsstore.LogFieldKindString, Str: "x"},
			{Name: "i", Kind: factsstore.LogFieldKindInt, Int: -3},
			{Name: "u", Kind: factsstore.LogFieldKindUint, Uint: 18446744073709551615},
			{Name: "f", Kind: factsstore.LogFieldKindFloat, Float: 0.1},
			{Name: "b", Kind: factsstore.LogFieldKindBool, Bool: true},
			{Name: "y", Kind: factsstore.LogFieldKindBytes, Bytes: []byte{0, 1, 255}},
			{Name: "t", Kind: factsstore.LogFieldKindTime, Time: at},
		}}
	_, err = s.WriteLogs([]factsstore.LogRow{
		{AppId: "github.com/example/play", Level: "debug", Message: "starting", Ts: t0},
		{AppId: "github.com/example/imztop", Level: "warn", Message: "high cpu", Ts: t0.Add(time.Second), InstanceKey: 3},
		{Message: "level-less", Ts: t0.Add(2 * time.Second)},
		full,
	})
	require.NoError(t, err)

	rows, err := s.LogRows(ctx, chstore.LogRowsFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, "level-less", rows[1].Row.Message, "newest first")
	got := rows[0]
	assert.Equal(t, "run-a", got.RunId)
	assert.True(t, full.Ts.Equal(got.Row.Ts), "nanoseconds survive: %v", got.Row.Ts)
	assert.Equal(t, full.AppId, got.Row.AppId)
	assert.EqualValues(t, 7, got.Row.InstanceKey)
	assert.Equal(t, full.Message, got.Row.Message)
	assert.Equal(t, "p.go:9", got.Row.Caller)
	assert.Equal(t, "svc", got.Row.Service)
	assert.Equal(t, "boom", got.Row.Error)
	assert.Equal(t, "a\nb", got.Row.Stack)
	byName := map[string]factsstore.LogField{}
	for _, f := range got.Row.Fields {
		byName[f.Name] = f
	}
	require.Len(t, byName, 7)
	assert.Equal(t, "x", byName["s"].Str)
	assert.EqualValues(t, -3, byName["i"].Int)
	assert.EqualValues(t, uint64(18446744073709551615), byName["u"].Uint)
	assert.InDelta(t, 0.1, byName["f"].Float, 1e-12)
	assert.True(t, byName["b"].Bool)
	assert.Equal(t, []byte{0, 1, 255}, byName["y"].Bytes)
	assert.True(t, at.Equal(byName["t"].Time))
	assert.Equal(t, factsstore.LogFieldKindTime, byName["t"].Kind)

	pick := func(f chstore.LogRowsFilter) (msgs []string) {
		t.Helper()
		rs, rErr := s.LogRows(ctx, f)
		require.NoError(t, rErr)
		for _, r := range rs {
			msgs = append(msgs, r.Row.Message)
		}
		return
	}
	assert.Equal(t, []string{full.Message, "high cpu"}, pick(chstore.LogRowsFilter{Levels: []string{"warn", "error"}}),
		"a level floor drops level-less rows")
	assert.Equal(t, []string{"high cpu"}, pick(chstore.LogRowsFilter{AppContains: "IMZ"}))
	assert.Equal(t, []string{full.Message}, pick(chstore.LogRowsFilter{MessageContains: `S A \ "QUOTED`}),
		"quotes and backslashes in a needle stay inside the literal")
	assert.Empty(t, pick(chstore.LogRowsFilter{MessageContains: `'; DROP TABLE x; --`}))
	assert.Equal(t, []string{"high cpu"}, pick(chstore.LogRowsFilter{InstanceKey: 3}))
	assert.Len(t, pick(chstore.LogRowsFilter{RunId: "run-a"}), 4)
	assert.Empty(t, pick(chstore.LogRowsFilter{RunId: "run-b"}))
	assert.Equal(t, []string{full.Message, "level-less"}, pick(chstore.LogRowsFilter{Since: t0.Add(2 * time.Second)}))
	assert.Equal(t, []string{"high cpu", "starting"}, pick(chstore.LogRowsFilter{Until: t0.Add(2 * time.Second)}))
	assert.Equal(t, []string{full.Message}, pick(chstore.LogRowsFilter{Limit: 1}))
}
