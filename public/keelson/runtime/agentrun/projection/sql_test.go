package projection

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

const testPool = "projection-test"

// workerRunner is a broker over one local worker pool, skipped where the
// engine is absent.
func workerRunner(t *testing.T) RunnerI {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
		MaxMemoryPerWorker: 256 << 20, SpawnTimeout: 10 * time.Second, WatchdogMaxLifetime: 120 * time.Second, KillGrace: 250 * time.Millisecond, StderrCapBytes: 4096}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	caller := bus.NewClient("test.caller", []app.SubjectFilter{{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"}})
	return BrokerRunner{Bus: caller, Pool: testPool}
}

// Law L4 on the corpus: the SQL projection in the worker and the Go fold
// return the same batch and the same digest, and the worker's rows match
// the golden the fold is held to (ADR-0309 §SD7).
func TestTheWorkerAndTheFoldAgree(t *testing.T) {
	runner := workerRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	entries := corpus()
	sql, err := Project(ctx, runner, entries, order)
	require.NoError(t, err)
	gofold := Fold(entries, order)
	assert.True(t, gofold.Equal(sql), "worker:\n%+v\nfold:\n%+v", sql, gofold)
	d1, _ := sql.Digest("fp")
	d2, _ := gofold.Digest("fp")
	assert.Equal(t, d2, d1)
	assert.Equal(t, readGolden(t), goldenOf(sql))
}

// L4 over generated journals: random journals through the worker agree
// with the fold. The worker round trip is slow, so the run is capped;
// AGENTRUN_RAPID_WORKER raises the cap.
func TestTheWorkerAndTheFoldAgreeOnGeneratedJournals(t *testing.T) {
	runner := workerRunner(t)
	limit := 12
	if v, err := strconv.Atoi(os.Getenv("AGENTRUN_RAPID_WORKER")); err == nil && v > 0 {
		limit = v
	}
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		if n >= limit {
			return
		}
		n++
		entries := genEntries(rt)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		sql, err := Project(ctx, runner, entries, order)
		if err != nil {
			rt.Fatalf("project: %v", err)
		}
		if !Fold(entries, order).Equal(sql) {
			rt.Fatalf("worker and fold differ on %d entries:\n%+v\n%+v", len(entries), sql, Fold(entries, order))
		}
	})
}

// A duplicated section order and an upper-case closing tag project the
// same through the worker as through the fold (law L4).
func TestTheWorkerAgreesOnDuplicateOrderAndCase(t *testing.T) {
	runner := workerRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	es := []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindSection, Name: "a", Body: "A"},
		{Ordinal: 2, Kind: agentrun.KindSection, Name: "b", Body: "B"},
		{Ordinal: 3, Kind: agentrun.KindNote, Run: "r", Body: "x\n</RECALLED>\ny", WriterRun: "r", WriterOrdinal: 1},
	}
	dup := Order{"b", "a", "b"}
	sql, err := Project(ctx, runner, es, dup)
	require.NoError(t, err)
	assert.True(t, Fold(es, dup).Equal(sql), "%+v\n%+v", sql, Fold(es, dup))
}

func TestAnEmptyJournalProjectsNothing(t *testing.T) {
	runner := workerRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := Project(ctx, runner, nil, order)
	require.NoError(t, err)
	assert.Empty(t, b.Messages)
	assert.Empty(t, b.Tools)
	assert.True(t, Fold(nil, order).Equal(b))
}
