// Package chlocalpool implements ADR-0028's pool of pre-spawned
// `clickhouse-local` worker processes.
//
// The pool maintains MinIdle warm workers blocked on stdin; each
// Acquire either pops a warm worker (the common case, ~8 ms latency
// for SELECT 1 per the M0 spike) or — if idle is empty and
// MaxConcurrent allows — spawns one on demand at the cost of
// cold-spawn latency (~40 ms).
//
// Workers are single-use: after Acquire, the caller writes SQL,
// drains stdout, and Closes. The pool does not reuse workers,
// avoiding the engine=Memory leakage and format-framing problems
// that motivated ADR-0028's O2 rejection.
//
// The pool mechanics — refill, watchdog, stop — are
// [github.com/stergiotis/boxer/public/keelson/runtime/procpool]
// (ADR-0285); this package supplies the clickhouse-local
// worker and ADR-0028 §SD3's defaults.
package chlocalpool

import (
	"context"
	"os"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/procpool"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Pool manages a set of clickhouse-local worker processes per the
// supplied Config. The caller MUST invoke Stop on shutdown to drain
// workers and join the pool's goroutines.
type Pool struct {
	cfg   Config
	inner *procpool.Pool[*Worker]
}

// New constructs a Pool, validates cfg, probes the binary, and
// starts filling MinIdle workers.
func New(cfg Config, logger zerolog.Logger) (p *Pool, err error) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = resolveBinaryPath()
	}
	cfg = cfg.withDefaults()
	if err = cfg.validate(); err != nil {
		return
	}
	if _, statErr := os.Stat(cfg.BinaryPath); statErr != nil {
		err = eb.Build().Str("binaryPath", cfg.BinaryPath).Errorf("chlocalpool: binary: %w", statErr)
		return
	}
	inner, err := procpool.New(procpool.Config{
		MinIdle:          cfg.MinIdle,
		MaxConcurrent:    cfg.MaxConcurrent,
		SpawnConcurrency: cfg.SpawnConcurrency,
		// newWorker bounds Start by SpawnTimeout itself; the pool's
		// deadline covers the wait for a spawn slot as well.
		SpawnTimeout:        cfg.SpawnTimeout * 4,
		WatchdogMaxLifetime: cfg.WatchdogMaxLifetime,
	}, procpool.SpawnerI[*Worker](spawner{cfg: cfg}), logger)
	if err != nil {
		return
	}
	p = &Pool{cfg: cfg, inner: inner}
	return
}

// Acquire returns the next available worker. If the idle pool is
// empty and the MaxConcurrent ceiling allows, a worker is spawned
// on demand for this caller (paying cold-spawn latency). If the
// ceiling is reached, blocks until a worker is released, ctx is
// done, or the pool is stopped.
//
// The caller MUST eventually Close the returned worker to release
// its slot in the pool and free OS resources.
func (inst *Pool) Acquire(ctx context.Context) (w *Worker, err error) {
	return inst.inner.AcquireE(ctx)
}

// Stop drains the pool: closes every worker, joins the refill and
// watchdog goroutines. Aggressive — any worker the caller still holds
// is terminated under it. Every call waits for the one teardown under
// its own ctx, so a caller whose deadline expired can call again.
func (inst *Pool) Stop(ctx context.Context) (err error) {
	return inst.inner.StopE(ctx)
}

// Stats snapshots the pool's current cardinality. Useful for tests
// and operators.
type Stats struct {
	Live          int
	Idle          int
	Acquired      int
	PendingSpawns int
	Stopped       bool
}

func (inst *Pool) Stats() (s Stats) {
	ps := inst.inner.Stats()
	s = Stats{
		Live:          ps.Live,
		Idle:          ps.Idle,
		Acquired:      ps.Acquired,
		PendingSpawns: ps.PendingSpawns,
		Stopped:       ps.Stopped,
	}
	return
}

type spawner struct {
	cfg Config
}

func (inst spawner) SpawnE(ctx context.Context, release procpool.ReleaseFunc) (w *Worker, err error) {
	return newWorker(ctx, inst.cfg, release)
}

var _ procpool.SpawnerI[*Worker] = spawner{}
