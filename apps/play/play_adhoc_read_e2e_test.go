package play

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// play_adhoc_read_e2e_test.go covers play READING a keelson() table — the
// half of ADR-0134 §SD4 that had no test at all.
//
// The publish side was covered; the read side was reachable only by driving
// the app, and a screenshot tour cannot say whether a query finished, only
// what was on screen when the shutter fired. That gap is not hypothetical: on
// 2026-08-06 a tour trace captured the fixture-lab scaffold ~350 ms after
// clicking Run and the loading spinner it caught was diagnosed as a hang in
// the introspection read path. There was no hang — the read takes ~0.3 s and
// the trace's `settleMs` pauses AFTER its step, so the wait it looked like it
// was applying was never applied. This test is what would have answered that
// in a second instead of a day, so it is written to answer exactly that
// question: the context deadline in [readThroughClient] IS the assertion. A
// read that stops finishing fails here as a timeout, not as a screenshot
// somebody has to interpret.
//
// It drives the production wiring rather than a mock — broker, capability
// service, loopback introspection endpoint over one shared registry, as
// adhocdata's own e2e does — and then the CLIENT's real path,
// ExecuteArrowStream + drainRun, which is what a lane calls.
//
// Default lane, skipping when clickhouse-local is absent: that is what every
// other clickhouse-local test does. The integration lane is for the SHARED
// server at localhost:8123, whose tests contend with each other; a one-shot
// local worker contends with nothing.

// adhocReadPlane wires the plane one keelson() read needs and returns the
// capability service plus its `/query` URL.
func adhocReadPlane(t *testing.T) (svc *adhocdata.Service, queryURL string) {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)

	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	svc, err = adhocdata.NewService(adhocdata.Config{
		Registry: reg, Keys: broker.KeyStore(), Dir: t.TempDir(), Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	caller := bus.NewClient("test.play.adhoc.read", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	runner := introspecthttp.RunnerFunc(func(ctx context.Context, sql string, params map[string]string) (body []byte, err error) {
		rep, e := chlocalbroker.ExecOnPool(ctx, caller, "introspect", chlocalbroker.ExecRequest{SQL: sql, Params: params})
		if e != nil {
			return nil, e
		}
		defer func() { _ = rep.Close() }()
		if re := rep.Err(); re != nil {
			return nil, re
		}
		return io.ReadAll(rep)
	})
	srv := introspecthttp.New(introspecthttp.Config{Registry: reg, Runner: runner, Decryptor: broker}, logger)
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	queryURL = srv.BaseURL() + "/query"
	// The host publishes this process global beside the server it started
	// (introspecthost). engineFor reads it back as the ADR-0145 §SD5 identity
	// exemption, which is what lets a CONFINED run reach this endpoint — so
	// setting it here is not test scaffolding, it is the condition under test.
	introspect.SetLocalQueryEndpoint(queryURL)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint("") })
	return
}

// readThroughClient runs sql the way a lane does and returns the row count.
//
// The 20 s context is the hang assertion: nothing here should take a
// measurable fraction of it, and a read that stops terminating reports a
// deadline rather than blocking the suite.
func readThroughClient(t *testing.T, cl *Client, queryURL string, sql string) (rows int64, err error) {
	t.Helper()
	dec := dispatchDecision{
		targetURL: queryURL,
		class:     dispatchClassIntrospection,
		// What the keelson resolver derives for a statement naming sealed
		// data (ADR-0145 §SD3) — carried so the engine's own refusal is on
		// the path rather than bypassed.
		sensitivity: queryengine.SensitivityConfined,
	}
	opts := newExecOptions("main")
	// Non-nil OnProgress is what the `main` lane sets (play_store.go), and it
	// swaps in chserver's hand-rolled incremental-header transport. Keeping it
	// on the path pins that that transport frames a response from an endpoint
	// which emits NO progress headers at all — the introspection /query
	// discards the knob and says so in its log.
	opts.OnProgress = func(runstream.Progress) {}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rdr, rs, _, err := cl.ExecuteArrowStream(ctx, sql, memory.NewGoAllocator(), opts, nil, dec)
	if err != nil {
		return
	}
	defer func() { _ = rs.Close() }()
	defer rdr.Release()
	batches, _, err := drainRun(rdr, rs)
	for _, b := range batches {
		rows += b.NumRows()
		b.Release()
	}
	return
}

// TestClientReadsKeelsonTablesEndToEnd is the read-side e2e: publish through
// the capability service, then read it back through the client, over the
// endpoint the app actually points at.
//
// One plane serves all three cases, so the ordinary table and the ad-hoc
// dataset are compared under identical conditions — which is the comparison
// that localises a failure to the ad-hoc half or clears it.
func TestClientReadsKeelsonTablesEndToEnd(t *testing.T) {
	svc, queryURL := adhocReadPlane(t)

	fixture, err := generateFixture(fixtureSpec{kind: adscore.AllAnomalyKinds[0], seed: 7})
	require.NoError(t, err)
	seriesIPC, err := encodeFixtureSeries(fixture, memory.NewGoAllocator())
	require.NoError(t, err)
	truthIPC, err := encodeFixtureTruth(fixture, adscore.AllAnomalyKinds[0], memory.NewGoAllocator())
	require.NoError(t, err)

	series, err := svc.Publish(adhocdata.PublishInput{
		Alias: fixtureSeriesAlias, Publisher: fixturePublisher, ArrowIPCStream: seriesIPC,
	})
	require.NoError(t, err)
	truth, err := svc.Publish(adhocdata.PublishInput{
		Alias: fixtureTruthAlias, Publisher: fixturePublisher, ArrowIPCStream: truthIPC,
	})
	require.NoError(t, err)

	cl := NewClient(ClientConfig{URL: queryURL}, nil)
	cl.bindDataset(fixtureSeriesAlias, series.Handle)
	cl.bindDataset(fixtureTruthAlias, truth.Handle)

	t.Run("an ordinary introspection table", func(t *testing.T) {
		// The control. It shares every part of the path except the sealed
		// dataset and its decryption, so a failure here and a failure below
		// mean different things.
		rows, rErr := readThroughClient(t, cl, queryURL, "SELECT name, category FROM keelson('env') LIMIT 10")
		require.NoError(t, rErr)
		assert.Equal(t, int64(10), rows)
	})

	t.Run("an ad-hoc dataset by alias", func(t *testing.T) {
		// The alias is what a buffer writes; bindDataset rewrites it to the
		// minted handle before the request leaves play (ADR-0134 §SD4). A
		// binding that named the alias on both sides would query a dataset
		// that was never published — the M4 bug this pins against.
		rows, rErr := readThroughClient(t, cl, queryURL,
			"SELECT t, v FROM keelson('"+fixtureSeriesAlias+"') ORDER BY t")
		require.NoError(t, rErr)
		assert.Equal(t, int64(len(fixture.Values)), rows)
	})

	t.Run("the fixture lab's scaffold", func(t *testing.T) {
		// The whole buffer the lab splices, ts* CTEs included — what RunMain
		// ships. It reads two ad-hoc datasets, and the `scores`/`spans` CTEs
		// name functions no server has heard of, which is only safe because
		// ClickHouse does not evaluate an unreferenced CTE. That is a real
		// dependency of the feature, so it is pinned here rather than assumed.
		rows, rErr := readThroughClient(t, cl, queryURL, fixtureScaffold())
		require.NoError(t, rErr)
		assert.Equal(t, int64(len(fixture.Values)), rows)
	})
}
