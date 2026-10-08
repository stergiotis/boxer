//go:build integration

package queryrunsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// scratchDb isolates the pipeline objects; dropped at test end. The
// destination table, MV, and extract watermark all live here — only
// system.query_log is shared with whatever else the server is doing.
const scratchDb = "queryruns_it"

// TestLivePipelineEndToEnd runs the ADR-0115 S1 acceptance list against
// a live ClickHouse (Ping-skip otherwise): reconciliation, capture of a
// stamped query, stateless double-read with structural dedup, and
// catch-up over an endpoint-down window. Cadence 1s keeps the wall
// clock tolerable; polls are bounded.
func TestLivePipelineEndToEnd(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if cli.Ping(ctx) != nil {
		t.Skip("no live ClickHouse at localhost:8123")
	}
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+scratchDb))
	t.Cleanup(func() { _ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+scratchDb) })

	svc, err := New(Config{
		Listen:   "127.0.0.1:0",
		Cadence:  time.Second,
		Scope:    queryrunfacts.ScopeAll,
		Database: scratchDb,
		// Start at now instead of backfilling the host's whole query_log
		// retention. Without this the test's duration is proportional to
		// however much history the machine happens to hold — the extract
		// drains oldest-first at BatchCap per refresh, and the probe is the
		// NEWEST row, so it lands only after the entire backlog does. That is
		// what made this test flaky: 119k rows of retention here, ~10k rows/s
		// normally but ~550/s under -race, i.e. minutes rather than seconds.
		// The catch-up half below is unaffected — the floor applies only while
		// the destination is empty, and by then it is not.
		BackfillFrom: time.Now(),
	}, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, svc.Start(ctx))
	stopped := false
	defer func() {
		if !stopped {
			_ = svc.Stop(context.Background())
		}
	}()

	// --- one stamped query becomes one fact, identity lifted ---
	// Probe ids are minted per run: query_log keeps days of history and
	// a fresh destination deliberately backfills it, so a reused probe
	// id would surface with yesterday's count already in the table.
	probe1 := fmt.Sprintf("it-queryruns-p1-%d", time.Now().UnixNano())
	runId := fmt.Sprintf("it-run-%d", time.Now().UnixNano())
	runTaggedQuery(t, probe1, fmt.Sprintf(
		`{"run_id":"%s","app":"it.app","lane":"l1","authored_fp":"af1","sent_fp":"sf1","chain_fp":"cf1","env_fp":"ef1"}`, runId))
	require.NoError(t, cli.Exec(ctx, "SYSTEM FLUSH LOGS"))
	waitForFactCount(t, cli, probe1, 1)

	// The lifted stamp: the run_id is a symbol value under the
	// MembRuntimeRun low-card membership, the shape chstore writes it in and
	// every run-id reader matches on.
	lrCol := "`tv:symbol:lr:lr:u64:1247:::0::data`"
	valCol := "`tv:symbol:value:val:s:124::I:0::data`"
	sql := fmt.Sprintf(
		"SELECT count() FROM %s.facts WHERE `id:naturalKey:y:4::0:` = '%s' AND has(%s, %d) AND has(%s, '%s')",
		scratchDb, probe1, lrCol, vocab.MembRuntimeRun.GetId().Value(), valCol, runId)
	require.Equal(t, "1", queryScalar(t, cli, sql), "the stamp's run_id must be lifted into the run membership")

	// --- S2 readback: the history pivots reconstruct the captured run ---
	// Read the full cap, not a small window. The scratch database is this
	// test's own, but the SOURCE is the shared system.query_log and the scope
	// is ScopeAll, so the facts table accumulates a row for every query the
	// server ran while the test was up — not just the two probes. The history
	// SELECT is newest-first, so a window of 50 asks that fewer than 50 queries
	// reached this server since the probe, which nothing guarantees: under a
	// parallel suite it did not hold, and the probe was simply pushed out.
	histSql, err := queryrunfacts.ComposeHistorySql(scratchDb+".facts", queryrunfacts.HistoryLimitCap)
	require.NoError(t, err)
	histRows, err := queryrunfacts.ParseHistoryRows([]byte(queryRaw(t, cli, histSql)))
	require.NoError(t, err)
	var probeRow *queryrunfacts.HistoryRow
	for i := range histRows {
		if histRows[i].QueryId == probe1 {
			probeRow = &histRows[i]
			break
		}
	}
	// Name the two ways this fails apart: the pivots dropped the row, or the
	// window overflowed. The fact itself is already known to exist — the
	// waitForFactCount above counted it.
	require.NotNilf(t, probeRow,
		"the stamped probe must appear in the history readback; %d of at most %d rows returned%s",
		len(histRows), queryrunfacts.HistoryLimitCap,
		map[bool]string{true: " — the window is full, so the probe may have been displaced rather than lost"}[len(histRows) >= queryrunfacts.HistoryLimitCap])
	require.Equal(t, "QueryFinish", probeRow.Event)
	require.Equal(t, "Select", probeRow.Kind)
	require.Equal(t, "it.app", probeRow.App)
	require.Equal(t, runId, probeRow.RunId)
	require.Equal(t, "l1", probeRow.Lane)
	require.Contains(t, probeRow.QueryText, "SELECT 42")
	require.NotZero(t, probeRow.Id&queryrunfacts.IdBand)
	require.NotZero(t, probeRow.NormalizedHash)
	require.Zero(t, probeRow.ExceptionCode)

	profSql, err := queryrunfacts.ComposeProfileEventsSql(scratchDb+".facts", probeRow.Id)
	require.NoError(t, err)
	// The drill-down comes back authored (handles and LW_ calls) for play's
	// editor to expand; the server has never heard of either, so expand it
	// here the way play's pipeline does before posting.
	profSql = expandAuthored(t, profSql, scratchDb+".facts")
	profRaw := queryRaw(t, cli, profSql+" FORMAT TabSeparated")
	require.NotEmpty(t, strings.TrimSpace(profRaw), "even SELECT 42 carries ProfileEvents counters")
	for line := range strings.SplitSeq(strings.TrimSpace(profRaw), "\n") {
		require.Len(t, strings.Split(line, "\t"), 2, "profile drill-down rows are (event, count) pairs")
	}

	// --- statelessness: back-to-back reads both answer; the table keeps one row ---
	// (No strict equality between the two reads: a refresh landing between
	// them legitimately advances the watermark. The property that matters —
	// re-served rows never duplicate — is the count assertion below.)
	pullRowCount(t, svc.PullURL())
	pullRowCount(t, svc.PullURL())
	time.Sleep(3 * time.Second) // a few refreshes over the overlap window
	require.Equal(t, "1", queryScalar(t, cli,
		fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:4::0:` = '%s'", scratchDb, probe1)),
		"anti-join must suppress re-served rows")

	// --- endpoint-down catch-up ---
	require.NoError(t, svc.Stop(ctx))
	stopped = true
	probe2 := fmt.Sprintf("it-queryruns-p2-%d", time.Now().UnixNano())
	runTaggedQuery(t, probe2, "")
	require.NoError(t, cli.Exec(ctx, "SYSTEM FLUSH LOGS"))
	time.Sleep(2 * time.Second) // refreshes fail against the dead endpoint

	svc2, err := New(Config{
		Listen:   "127.0.0.1:0", // a NEW port: reconcile must repoint the MV
		Cadence:  time.Second,
		Scope:    queryrunfacts.ScopeAll,
		Database: scratchDb,
	}, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, svc2.Start(ctx))
	defer func() { _ = svc2.Stop(context.Background()) }()
	waitForFactCount(t, cli, probe2, 1)

	// The down-window row must exist exactly once too (no double-capture
	// from the catch-up).
	time.Sleep(3 * time.Second)
	require.Equal(t, "1", queryScalar(t, cli,
		fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:4::0:` = '%s'", scratchDb, probe2)))
}

// startScratch runs a service against its own scratch database, dropped at
// test end, and returns it once the pipeline is reconciled.
func startScratch(t *testing.T, cli *chclient.Client, db string, cfg Config) (svc *Service) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db))
	t.Cleanup(func() { _ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	cfg.Listen = "127.0.0.1:0"
	cfg.Cadence = time.Second
	cfg.Scope = queryrunfacts.ScopeAll
	cfg.Database = db
	svc, err := New(cfg, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, svc.Start(ctx))
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return
}

// TestLiveSecondInstanceRefused pins the single-owner guard: while one
// instance serves the capture view a second one against the same database
// is refused, instead of repointing the view at itself; once the first
// stops, the second takes over — the restart path.
func TestLiveSecondInstanceRefused(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if cli.Ping(ctx) != nil {
		t.Skip("no live ClickHouse at localhost:8123")
	}
	const db = scratchDb + "_owner"
	first := startScratch(t, cli, db, Config{BackfillFrom: time.Now()})

	cfg := Config{Listen: "127.0.0.1:0", Cadence: time.Second, Database: db, BackfillFrom: time.Now()}
	second, err := New(cfg, zerolog.Nop())
	require.NoError(t, err)
	err = second.Start(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "another queryrunsd")
	require.Equal(t, first.PullURL(), errorField(t, err, "owner"))

	require.NoError(t, first.Stop(ctx))
	second, err = New(cfg, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, second.Start(ctx))
	t.Cleanup(func() { _ = second.Stop(context.Background()) })
}

// TestLiveDenseOverlapDoesNotStall pins progress through an overlap window
// holding more events than one batch: with the already-captured rows
// counted against the LIMIT, every batch after the first re-served the same
// rows and the watermark never moved. BatchCap stands in for load — 60
// probes inside a second against a cap of 20.
func TestLiveDenseOverlapDoesNotStall(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if cli.Ping(ctx) != nil {
		t.Skip("no live ClickHouse at localhost:8123")
	}
	const probes = 60
	from := time.Now().Add(-time.Second)
	prefix := fmt.Sprintf("it-queryruns-dense-%d-", time.Now().UnixNano())
	for i := range probes {
		runTaggedQuery(t, fmt.Sprintf("%s%d", prefix, i), "")
	}
	require.NoError(t, cli.Exec(ctx, "SYSTEM FLUSH LOGS"))

	const db = scratchDb + "_dense"
	startScratch(t, cli, db, Config{BatchCap: 20, BackfillFrom: from})
	sql := fmt.Sprintf("SELECT count() FROM %s.facts WHERE startsWith(`id:naturalKey:y:4::0:`, '%s')", db, prefix)
	require.Eventually(t, func() bool {
		return queryScalar(t, cli, sql) == fmt.Sprint(probes)
	}, factWaitBudget, 500*time.Millisecond, "capture must advance past a window denser than BatchCap")
}

// TestLiveOldBackfillStaysDuplicateFree pins the MV anti-join window to the
// watermark: backfilling history days old, every refresh re-serves at least
// the row at the watermark, and a window anchored on now() missed all of
// them — one permanent duplicate or more per tick. Needs query_log history
// older than a day; skips otherwise.
func TestLiveOldBackfillStaysDuplicateFree(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if cli.Ping(ctx) != nil {
		t.Skip("no live ClickHouse at localhost:8123")
	}
	oldest := queryScalar(t, cli,
		"SELECT toUnixTimestamp(min(event_time)) FROM system.query_log WHERE type != 'QueryStart' AND event_time < now() - INTERVAL 2 DAY")
	if oldest == "" || oldest == "0" {
		t.Skip("query_log holds no history older than two days")
	}
	var sec int64
	_, err := fmt.Sscan(oldest, &sec)
	require.NoError(t, err)

	const db = scratchDb + "_old"
	const batchCap = 50
	startScratch(t, cli, db, Config{BatchCap: batchCap, BackfillFrom: time.Unix(sec, 0)})
	count := fmt.Sprintf("SELECT count() FROM %s.facts", db)
	// Several refreshes' worth, each re-serving its predecessor's overlap.
	require.Eventually(t, func() bool {
		var n int
		_, _ = fmt.Sscan(queryScalar(t, cli, count), &n)
		return n >= 5*batchCap
	}, factWaitBudget, 500*time.Millisecond, "backfill did not advance")
	require.Equal(t, "0", queryScalar(t, cli,
		fmt.Sprintf("SELECT count() - uniqExact(`id:id:u64:47::0:`) FROM %s.facts", db)),
		"re-served backfill rows must not land twice")
}

// TestLiveReconcileRefusesDriftedDestination pins the schema-drift
// guard: a destination table from an older schema generation (here: one
// missing every current column) must fail reconciliation with the
// actionable message, not proceed into the MV's misleading
// correlated-subquery error.
func TestLiveReconcileRefusesDriftedDestination(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if cli.Ping(ctx) != nil {
		t.Skip("no live ClickHouse at localhost:8123")
	}
	const db = scratchDb + "_drift"
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db))
	t.Cleanup(func() { _ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	require.NoError(t, cli.Exec(ctx, "CREATE DATABASE "+db))
	require.NoError(t, cli.Exec(ctx,
		"CREATE TABLE "+db+".facts (`legacy` UInt64) ENGINE MergeTree() ORDER BY legacy"))

	svc, err := New(Config{Listen: "127.0.0.1:0", Cadence: time.Second, Database: db}, zerolog.Nop())
	require.NoError(t, err)
	err = svc.Start(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "older schema generation")
	// The table travels as an eb field, not in the message (house style keeps
	// the message bare), so the assertion reads the structured payload.
	require.Equal(t, db+".facts", errorField(t, err, "destination"),
		"the refusal must name the table it refused")
}

// errorField returns the first value the error chain carries under key in an
// eb-built structured payload, decoding with an independent CBOR library.
func errorField(t *testing.T, err error, key string) any {
	t.Helper()
	for e := err; e != nil; e = errors.Unwrap(e) {
		esd, ok := e.(eh.ErrorWithStructuredDataI)
		if !ok || len(esd.GetCBORStructuredData()) == 0 {
			continue
		}
		var v map[any]any
		require.NoError(t, cbor.Unmarshal(esd.GetCBORStructuredData(), &v))
		if val, present := v[key]; present {
			return val
		}
	}
	require.Failf(t, "field missing", "no %q field in the error chain of %v", key, err)
	return nil
}

// expandAuthored rewrites an authored readback query into the one that
// executes: column handles against the generated facts schema, then the LW_
// extraction calls against the membership lookup play binds.
func expandAuthored(t *testing.T, sql string, table string) string {
	t.Helper()
	database, _, _ := strings.Cut(table, ".")
	fields := dml.CreateSchemaFacts().Fields()
	names := make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{table: names}))
	for _, pass := range []nanopass.Pass{
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPassWithIds(resolver, providers.MembershipLookup{}, database),
	} {
		var err error
		sql, err = pass.Apply(env.NewEnvironment(), sql)
		require.NoError(t, err, "pass %s", pass.Name)
	}
	require.False(t, constructsql.HasExtractMarker(sql))
	return sql
}

// runTaggedQuery issues SELECT 42 under the given query_id (the natural
// key the assertions look up) and optional log_comment stamp.
func runTaggedQuery(t *testing.T, queryId string, logComment string) {
	t.Helper()
	u := "http://localhost:8123/?query_id=" + queryId
	if logComment != "" {
		u += "&log_comment=" + strings.ReplaceAll(logComment, `"`, "%22")
	}
	resp, err := http.Post(u, "text/plain", strings.NewReader("SELECT 42"))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "probe query failed: %s", string(body))
}

// factWaitBudget bounds the wait for a probe to traverse the pipeline:
// query_log flush, the MV, the extract watermark, and the 1s service cadence.
// The typical crossing is a few seconds, but the stages are wall-clock driven
// and the server is shared with whatever else the machine is doing, so the
// budget is a CEILING on the pathological case, not an estimate of the normal
// one. It is generous on purpose and costs nothing when things are healthy —
// require.Eventually polls every 500ms and returns on the first success, so a
// passing run is no slower for it. Only a genuine failure waits this long.
//
// 20s was too tight: it failed twice under a parallel suite, once here and
// once at the history read below. (It also fails under `-race`, but not for
// this reason — that failure survives this budget and is unexplained; the lane
// runner excludes -race and says so.)
const factWaitBudget = 60 * time.Second

// waitForFactCount polls until the naturalKey shows up n times in the
// scratch facts table.
func waitForFactCount(t *testing.T, cli *chclient.Client, naturalKey string, n int) {
	t.Helper()
	sql := fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:4::0:` = '%s'", scratchDb, naturalKey)
	require.Eventually(t, func() bool {
		return queryScalar(t, cli, sql) == fmt.Sprint(n)
	}, factWaitBudget, 500*time.Millisecond, "fact for %s did not reach count %d", naturalKey, n)
}

// queryScalar runs sql (single value) and returns the trimmed result.
func queryScalar(t *testing.T, cli *chclient.Client, sql string) (out string) {
	t.Helper()
	out = strings.TrimSpace(queryRaw(t, cli, sql+" FORMAT TabSeparated"))
	return
}

// queryRaw runs sql (its own FORMAT clause included) and returns the body.
func queryRaw(t *testing.T, cli *chclient.Client, sql string) (out string) {
	t.Helper()
	body, err := cli.Query(context.Background(), sql)
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	out = string(raw)
	return
}

// pullRowCount GETs /pull directly and counts the rows in the served
// ArrowStream — the reader's view of the endpoint's statelessness.
func pullRowCount(t *testing.T, pullURL string) (n int64) {
	t.Helper()
	resp, err := http.Get(pullURL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rd, err := ipc.NewReader(resp.Body)
	require.NoError(t, err)
	defer rd.Release()
	for rd.Next() {
		n += rd.RecordBatch().NumRows()
	}
	require.NoError(t, rd.Err())
	return
}
