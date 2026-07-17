package queryrunsvc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
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
	waitForFactCount(t, cli, probe1, 1, 20*time.Second)

	// The lifted stamp: the run_id must be queryable via the
	// MembRuntimeRun mixed membership's high-card parameter.
	lmrCol := "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
	mrhpCol := "`tv:symbol:mrhp:mrhp:y:g:0:0:0::data`"
	sql := fmt.Sprintf(
		"SELECT count() FROM %s.facts WHERE `id:naturalKey:y:g:0:0:` = '%s' AND has(%s, %d) AND has(%s, '%s')",
		scratchDb, probe1, lmrCol, vocab.MembRuntimeRun.GetId().Value(), mrhpCol, runId)
	require.Equal(t, "1", queryScalar(t, cli, sql), "the stamp's run_id must be lifted into the mixed membership")

	// --- statelessness: back-to-back reads both answer; the table keeps one row ---
	// (No strict equality between the two reads: a refresh landing between
	// them legitimately advances the watermark. The property that matters —
	// re-served rows never duplicate — is the count assertion below.)
	pullRowCount(t, svc.PullURL())
	pullRowCount(t, svc.PullURL())
	time.Sleep(3 * time.Second) // a few refreshes over the overlap window
	require.Equal(t, "1", queryScalar(t, cli,
		fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:g:0:0:` = '%s'", scratchDb, probe1)),
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
	waitForFactCount(t, cli, probe2, 1, 20*time.Second)

	// The down-window row must exist exactly once too (no double-capture
	// from the catch-up).
	time.Sleep(3 * time.Second)
	require.Equal(t, "1", queryScalar(t, cli,
		fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:g:0:0:` = '%s'", scratchDb, probe2)))
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

// waitForFactCount polls until the naturalKey shows up n times in the
// scratch facts table (the MV refresh cadence is 1s; flush + refresh
// need a few of those).
func waitForFactCount(t *testing.T, cli *chclient.Client, naturalKey string, n int, timeout time.Duration) {
	t.Helper()
	sql := fmt.Sprintf("SELECT count() FROM %s.facts WHERE `id:naturalKey:y:g:0:0:` = '%s'", scratchDb, naturalKey)
	require.Eventually(t, func() bool {
		return queryScalar(t, cli, sql) == fmt.Sprint(n)
	}, timeout, 500*time.Millisecond, "fact for %s did not reach count %d", naturalKey, n)
}

// queryScalar runs sql (single value) and returns the trimmed result.
func queryScalar(t *testing.T, cli *chclient.Client, sql string) (out string) {
	t.Helper()
	body, err := cli.Query(context.Background(), sql+" FORMAT TabSeparated")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	out = strings.TrimSpace(string(raw))
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
