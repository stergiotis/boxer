package hostboot

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// stallServer stands in for ClickHouse: it answers the facts store's
// version and surface probes, every other statement with an empty body,
// and from the first statement that names the state table on, holds every
// statement until healed, as a server whose disk stalls does. It keeps the
// bodies of the inserts it took.
type stallServer struct {
	mu      sync.Mutex
	stalled bool
	healed  chan struct{}
	inserts [][]byte
	// insertsAfterHeal counts the inserts taken once healed.
	insertsAfterHeal int
}

func newStallServer(t *testing.T) (srv *stallServer, url string) {
	t.Helper()
	srv = &stallServer{healed: make(chan struct{})}
	hs := httptest.NewServer(http.HandlerFunc(srv.handle))
	t.Cleanup(func() {
		srv.heal()
		hs.Close()
	})
	return srv, hs.URL + "/"
}

func (inst *stallServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	q := r.URL.Query().Get("query")
	stmt := q + " " + string(body)
	if strings.Contains(stmt, "persiststate") {
		inst.mu.Lock()
		if inst.healed != nil {
			select {
			case <-inst.healed:
			default:
				inst.stalled = true
			}
		}
		inst.mu.Unlock()
	}
	inst.mu.Lock()
	stalled, healed := inst.stalled, inst.healed
	inst.mu.Unlock()
	if stalled {
		select {
		case <-healed:
		case <-r.Context().Done():
			return
		}
	}
	if strings.HasPrefix(strings.TrimSpace(q), "INSERT") {
		inst.mu.Lock()
		inst.inserts = append(inst.inserts, body)
		if stalled {
			inst.insertsAfterHeal++
		}
		inst.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
	switch {
	case strings.Contains(stmt, "LW_SURFACE_VERSION()"):
		_, _ = io.WriteString(w, "26.10.1.1\t3\t"+lwsqlsurface.Digest()+"\n")
	case strings.Contains(stmt, "SELECT version()"):
		_, _ = io.WriteString(w, "26.10.1.1\n")
	}
}

// heal ends the stall: held statements answer, and later ones at once.
func (inst *stallServer) heal() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	select {
	case <-inst.healed:
	default:
		close(inst.healed)
	}
}

// landed says an insert taken after the heal carries every needle.
func (inst *stallServer) landed(needles ...string) bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.insertsAfterHeal == 0 {
		return false
	}
	for _, b := range inst.inserts[len(inst.inserts)-inst.insertsAfterHeal:] {
		all := true
		for _, n := range needles {
			all = all && bytes.Contains(b, []byte(n))
		}
		if all {
			return true
		}
	}
	return false
}

// fastRetry shortens the boot's bound and the retry's waits for a test.
func fastRetry(t *testing.T) {
	t.Helper()
	bound, first, ceiling := persistOpenTimeout, storeRetryFirst, storeRetryCeiling
	persistOpenTimeout, storeRetryFirst, storeRetryCeiling = 300*time.Millisecond, 20*time.Millisecond, 80*time.Millisecond
	t.Cleanup(func() { persistOpenTimeout, storeRetryFirst, storeRetryCeiling = bound, first, ceiling })
}

// syncBuffer is a log sink safe for the host's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (inst *syncBuffer) Write(p []byte) (int, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.Write(p)
}

func (inst *syncBuffer) String() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.String()
}

// retryGoroutines counts the goroutines running a store retry.
func retryGoroutines() (n int) {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), "hostboot.(*storeRetry).run")
}

// ADR-0408: a server whose state table stalls past the boot's bound and
// then answers. The host boots with its trail held, refuses work that
// leaves the machine with a reason that says the store is being retried
// and no restart is needed, recovers without a restart, records the span
// it held, and admits work again with its rows landing on the server.
func TestBoot_StoreThatStallsThenAnswers_RecoversWithoutRestart(t *testing.T) {
	fastRetry(t)
	srv, url := newStallServer(t)
	envtest.Set(t, clickhouseenv.Endpoint, url)
	envtest.Set(t, trail.RequiredEnv, "true")
	logs := &syncBuffer{}
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Facts = nil
	opts.Log = zerolog.New(logs)
	opts.NoWindowHost = true
	opts.FactsRowIds = chstore.RowIds{Allocator: memalloc.NewAllocator()}
	opts.Services = Services{Persist: true, LLM: true, HTTP: true}

	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			rt.Close(context.Background())
		}
	})
	require.True(t, rt.IsChStore)
	assert.Equal(t, "mem", rt.PersistBackend, "app state stays in memory for the run")
	assert.Nil(t, rt.PersistExec)
	require.True(t, rt.Trail.Held(), "the trail holds its rows")
	why := rt.Trail.NotDurable()
	assert.Contains(t, why, "has not opened since")
	assert.Contains(t, why, "retrying it in the background")
	assert.Contains(t, why, "No restart is needed")
	assert.Equal(t, why, rt.Status.TrailWhy(), "the status bar reads it")

	refuse := rt.Trail.Admit()
	require.Error(t, refuse, "an egress fetch is refused")
	assert.Contains(t, refuse.Error(), "No restart is needed")
	_, refuse = rt.Trail.WriteAhead(context.Background())
	require.Error(t, refuse, "a model call's write-ahead is refused")
	assert.Contains(t, refuse.Error(), "the next attempt at")

	require.Eventually(t, func() bool { return strings.Contains(rt.Trail.NotDurable(), "attempts so far") },
		5*time.Second, 10*time.Millisecond, "the retry fails while the server stalls and says so")
	assert.True(t, rt.Trail.Held())

	srv.heal()
	require.Eventually(t, rt.Trail.Durable, 5*time.Second, 10*time.Millisecond, "the host recovers without a restart")
	assert.Empty(t, rt.Status.TrailWhy())
	assert.NoError(t, rt.Trail.Admit())
	durable, refuse := rt.Trail.WriteAhead(context.Background())
	require.NoError(t, refuse)
	assert.True(t, durable)
	require.Eventually(t, func() bool { return srv.landed(trail.ActionAuditHeld, "attempts", "refused") },
		5*time.Second, 10*time.Millisecond, "the held span lands as the trail's own row")
	assert.True(t, srv.landed(trail.ActionAuditConfig), "the settings row recorded at boot landed too")
	c := rt.Trail.Counts()
	assert.EqualValues(t, 2, c.Refused)
	assert.Positive(t, c.Seals)
	assert.Zero(t, c.Dropped)
	assert.Contains(t, logs.String(), "the trail reached ClickHouse and landed what it held")
	assert.Equal(t, "mem", rt.PersistBackend, "app state does not move")

	rt.Close(context.Background())
	closed = true
	assert.Zero(t, retryGoroutines())
}

// ADR-0408: a server that never answers. The retry backs off to its
// ceiling, the trail stays held and refuses, and Close stops the retry,
// an attempt in flight included, without leaving a goroutine.
func TestBoot_StoreThatNeverAnswers_BacksOffAndStopsAtClose(t *testing.T) {
	fastRetry(t)
	_, url := newStallServer(t)
	envtest.Set(t, clickhouseenv.Endpoint, url)
	envtest.Set(t, trail.RequiredEnv, "true")
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Facts = nil
	opts.NoWindowHost = true
	opts.FactsRowIds = chstore.RowIds{Allocator: memalloc.NewAllocator()}
	opts.Services = Services{Persist: true}
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, rt.Trail.Held())
	require.Eventually(t, func() bool { return strings.Contains(rt.Trail.NotDurable(), "3 attempts so far") },
		10*time.Second, 10*time.Millisecond)
	assert.Error(t, rt.Trail.Admit())
	assert.Equal(t, 1, retryGoroutines())

	start := time.Now()
	rt.Close(context.Background())
	assert.Less(t, time.Since(start), 5*time.Second, "Close does not wait out an attempt")
	assert.Zero(t, retryGoroutines())
}

// The retry's waits double from the first to the ceiling, each drawn
// between half and all of its value.
func TestJitteredStaysInItsBand(t *testing.T) {
	for range 1000 {
		d := jittered(time.Minute)
		assert.GreaterOrEqual(t, d, 30*time.Second)
		assert.LessOrEqual(t, d, time.Minute)
	}
	assert.Equal(t, time.Duration(0), jittered(0))
}

// The reason a held trail carries names when the store was last tried and
// when next, and that no restart is needed; a long error is clipped.
func TestHeldReason(t *testing.T) {
	since := time.Date(2026, 11, 22, 14, 2, 11, 0, time.UTC)
	why := heldReason(since, "ensure table boxer.persiststate: context deadline exceeded", 0, nil, since.Add(2*time.Second))
	assert.Equal(t, "the host's ClickHouse store has not opened since 14:02:11 UTC (at boot: ensure table boxer.persiststate: context deadline exceeded);"+
		" the host is retrying it in the background, the next attempt at 14:02:13 UTC. No restart is needed: try again once it opens", why)
	assert.Contains(t, heldReason(since, "x", 0, nil, time.Time{}), "(at boot: x); the host retries it in the background once it has started. No restart is needed")
	why = heldReason(since, "x", 3, io.ErrUnexpectedEOF, since.Add(time.Minute))
	assert.Contains(t, why, "(3 attempts so far, the last: unexpected EOF), the next attempt at 14:03:11 UTC")
	assert.LessOrEqual(t, len([]rune(heldReason(since, strings.Repeat("a", 1000), 0, nil, since))), 400)
}
