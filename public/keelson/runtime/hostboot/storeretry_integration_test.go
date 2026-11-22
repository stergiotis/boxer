//go:build integration

package hostboot

import (
	"bytes"
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// These tests boot hosts against a live server under the runtime's own
// names: each writes its run's rows into the server's boxer database and
// drops nothing. They run on a throwaway server (the marker database
// scripts/ci/clickhouse_server.sh creates), or on any server when
// hostRowsOptIn is set to 1 in the environment.
const hostRowsOptIn = "BOXER_HOSTBOOT_IT_HOST_ROWS"

// hostServer is the live server's client and URL, or a skip.
func hostServer(t *testing.T) (cli *chclient.Client, target string) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	cli = chclient.New(cfg, nil)
	if err := cli.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	if os.Getenv(hostRowsOptIn) != "1" &&
		serverScalar(t, cli, "SELECT count() FROM system.databases WHERE name = '"+throwawayMarker+"'") != "1" {
		t.Skipf("the server at %s is not a throwaway one and %s is not 1", cfg.URL, hostRowsOptIn)
	}
	return cli, cfg.URL
}

// stallProxy forwards to a live server; from the first statement that
// names the state table on, it holds every statement until healed.
type stallProxy struct {
	mu      sync.Mutex
	stalled bool
	healed  chan struct{}
	proxy   *httputil.ReverseProxy
}

func newStallProxy(t *testing.T, target string) (p *stallProxy, endpoint string) {
	t.Helper()
	u, err := url.Parse(target)
	require.NoError(t, err)
	p = &stallProxy{healed: make(chan struct{}), proxy: httputil.NewSingleHostReverseProxy(u)}
	hs := httptest.NewServer(http.HandlerFunc(p.handle))
	t.Cleanup(func() {
		p.heal()
		hs.Close()
	})
	return p, hs.URL + "/"
}

func (inst *stallProxy) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	inst.mu.Lock()
	if strings.Contains(r.URL.Query().Get("query")+string(body), "persiststate") {
		select {
		case <-inst.healed:
		default:
			inst.stalled = true
		}
	}
	stalled := inst.stalled
	inst.mu.Unlock()
	if stalled {
		select {
		case <-inst.healed:
		case <-r.Context().Done():
			return
		}
	}
	inst.proxy.ServeHTTP(w, r)
}

func (inst *stallProxy) heal() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	select {
	case <-inst.healed:
	default:
		close(inst.healed)
	}
}

// runAuditEvents are the audit events of run on the trail since since,
// oldest first. A test binary is one run, so since is what tells one
// test's host from another's.
func runAuditEvents(t *testing.T, rec *trail.Recorder, run string, since time.Time) (events []trail.AuditEvent) {
	t.Helper()
	opts := recordstore.ScanOpts{ExtraPredicate: trail.TrailColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")"}
	ents, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAuditEvent(context.Background(), opts)
	})
	require.NoError(t, err)
	slices.SortFunc(ents, func(a, b *trail.TrailEntity) int { return a.Ts.Compare(b.Ts) })
	for _, e := range ents {
		if e.Origin.Has && e.Origin.Val.Run == run && e.AuditEvent.Has {
			events = append(events, e.AuditEvent.Val)
		}
	}
	return
}

func liveOptions(t *testing.T) Options {
	t.Helper()
	return Options{Log: zerolog.New(os.Stderr).Level(zerolog.WarnLevel), Registry: app.NewRegistry(), NoWindowHost: true, KeepCoreDumps: true,
		Services: Services{Persist: true, LLM: true}}
}

// ADR-0408 against a live server: the state table's DDL stalls past the
// boot's bound, the host boots held and refuses, and once the server
// answers it recovers without a restart. On the server the run's trail
// starts with its settings row, carries the held span, and its seals
// verify.
func TestBoot_LiveStoreThatStallsThenAnswers(t *testing.T) {
	_, target := hostServer(t)
	p, endpoint := newStallProxy(t, target)
	bound, first, ceiling := persistOpenTimeout, storeRetryFirst, storeRetryCeiling
	persistOpenTimeout, storeRetryFirst, storeRetryCeiling = time.Second, 50*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { persistOpenTimeout, storeRetryFirst, storeRetryCeiling = bound, first, ceiling })
	envtest.Set(t, clickhouseenv.Endpoint, endpoint)
	envtest.Set(t, clickhouseenv.URL, endpoint)
	envtest.Set(t, trail.RequiredEnv, "true")
	boot := time.Now()
	rt, err := Boot(context.Background(), liveOptions(t))
	require.NoError(t, err)
	defer rt.Close(context.Background())
	require.True(t, rt.IsChStore)
	require.True(t, rt.Trail.Held(), rt.Trail.NotDurable())
	require.Error(t, rt.Trail.Admit())

	p.heal()
	require.Eventually(t, rt.Trail.Durable, 30*time.Second, 20*time.Millisecond)
	require.NoError(t, rt.Trail.Admit())
	require.NoError(t, rt.Trail.Flush(context.Background()))
	run := rt.RunInfo.RunId
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(runAuditEvents(t, rt.Trail, run, boot), func(e trail.AuditEvent) bool { return e.Action == trail.ActionAuditHeld })
	}, 30*time.Second, 100*time.Millisecond, "the held span lands")
	events := runAuditEvents(t, rt.Trail, run, boot)
	require.NotEmpty(t, events)
	assert.Equal(t, trail.ActionAuditConfig, events[0].Action, "the settings row is the run's first, as at any boot")
	held := events[slices.IndexFunc(events, func(e trail.AuditEvent) bool { return e.Action == trail.ActionAuditHeld })]
	assert.Equal(t, []string{"from", "to", "attempts", "refused", "cause"}, held.AttrKeys)
	assert.Equal(t, "1", held.AttrValues[3], "the one refused admission")
	assert.Contains(t, held.AttrValues[4], "persiststate")
	require.NoError(t, rt.Trail.Flush(context.Background()))
	broken, unnamed, _, err := rt.Trail.VerifySeals(context.Background(), run)
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreakNone, broken.Why)
	assert.Empty(t, unnamed, "every landed row is named by a seal")
}

// ADR-0408 against a live server that answers at once: the boot is as it
// was. The store opens, the trail is durable from the start with no retry,
// and the run's trail carries no held span.
func TestBoot_LiveStoreThatAnswersAtOnce(t *testing.T) {
	_, target := hostServer(t)
	envtest.Set(t, clickhouseenv.Endpoint, target)
	envtest.Set(t, trail.RequiredEnv, "true")
	boot := time.Now()
	rt, err := Boot(context.Background(), liveOptions(t))
	require.NoError(t, err)
	defer rt.Close(context.Background())
	require.True(t, rt.IsChStore)
	assert.Equal(t, "store", rt.PersistBackend)
	assert.NotNil(t, rt.PersistExec)
	assert.Nil(t, rt.storeRetry)
	assert.False(t, rt.Trail.Held())
	assert.True(t, rt.Trail.Durable())
	require.NoError(t, rt.Trail.Admit())
	require.NoError(t, rt.Trail.Flush(context.Background()))
	run := rt.RunInfo.RunId
	events := runAuditEvents(t, rt.Trail, run, boot)
	require.NotEmpty(t, events)
	assert.Equal(t, trail.ActionAuditConfig, events[0].Action)
	assert.False(t, slices.ContainsFunc(events, func(e trail.AuditEvent) bool { return e.Action == trail.ActionAuditHeld }))
	assert.Zero(t, retryGoroutines())
}
