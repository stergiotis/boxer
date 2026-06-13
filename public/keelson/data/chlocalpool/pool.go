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
// M1 of ADR-0028 (per §SD9): standalone package, no bus or broker
// integration. Consumable directly via Pool.Acquire for testing
// and direct Go callers. M2 wraps this in chlocalbroker.
package chlocalpool

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Pool manages a set of clickhouse-local worker processes per the
// supplied Config. New launches refill and watchdog goroutines; the
// caller MUST invoke Stop on shutdown to drain workers and join the
// goroutines.
type Pool struct {
	cfg    Config
	logger zerolog.Logger

	idle          chan *Worker
	spawnSem      chan struct{}
	refillTrigger chan struct{}
	stopCh        chan struct{}
	bg            sync.WaitGroup

	mu            sync.Mutex
	stopped       bool
	live          int
	pendingSpawns int
	tracked       map[*Worker]struct{}
	acquired      map[*Worker]time.Time
}

// New constructs a Pool, validates cfg, probes the binary, and
// kicks off the refill goroutine to fill MinIdle workers.
func New(cfg Config, logger zerolog.Logger) (p *Pool, err error) {
	cfg = cfg.withDefaults()
	if err = cfg.validate(); err != nil {
		return
	}
	if _, statErr := os.Stat(cfg.BinaryPath); statErr != nil {
		err = eh.Errorf("chlocalpool: binary %s: %w", cfg.BinaryPath, statErr)
		return
	}
	p = &Pool{
		cfg:           cfg,
		logger:        logger,
		idle:          make(chan *Worker, cfg.MaxConcurrent),
		spawnSem:      make(chan struct{}, cfg.SpawnConcurrency),
		refillTrigger: make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		tracked:       make(map[*Worker]struct{}),
		acquired:      make(map[*Worker]time.Time),
	}
	p.bg.Add(2)
	go p.refillLoop()
	go p.watchdogLoop()
	p.signalRefill() // kick the initial fill
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
	// Fast path: pop an idle worker.
	select {
	case w = <-inst.idle:
		inst.afterAcquire(w)
		return
	default:
	}

	// Idle empty: see if we can spawn on demand.
	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		err = eh.Errorf("chlocalpool: pool stopped")
		return
	}
	canSpawn := inst.live+inst.pendingSpawns < int(inst.cfg.MaxConcurrent)
	if canSpawn {
		inst.pendingSpawns++
	}
	inst.mu.Unlock()

	if canSpawn {
		w, err = inst.spawnAndCount(ctx)
		if err != nil {
			return
		}
		inst.afterAcquire(w)
		return
	}

	// Pool maxed; block.
	select {
	case w = <-inst.idle:
		inst.afterAcquire(w)
		return
	case <-ctx.Done():
		err = eh.Errorf("chlocalpool: acquire cancelled: %w", ctx.Err())
		return
	case <-inst.stopCh:
		err = eh.Errorf("chlocalpool: pool stopped")
		return
	}
}

// Stop drains the pool: closes every tracked worker, joins the
// refill / watchdog / in-flight spawn goroutines. Aggressive — any
// worker the caller still holds will be terminated under it.
// Idempotent; honours ctx for an upper-bound deadline.
func (inst *Pool) Stop(ctx context.Context) (err error) {
	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		return
	}
	inst.stopped = true
	close(inst.stopCh)
	workers := make([]*Worker, 0, len(inst.tracked))
	for w := range inst.tracked {
		workers = append(workers, w)
	}
	inst.mu.Unlock()

	closeDone := make(chan struct{})
	go func() {
		defer close(closeDone)
		var wg sync.WaitGroup
		for _, w := range workers {
			wg.Add(1)
			go func(w *Worker) {
				defer wg.Done()
				_ = w.Close()
			}(w)
		}
		wg.Wait()
		inst.bg.Wait()
	}()

	select {
	case <-closeDone:
	case <-ctx.Done():
		err = eh.Errorf("chlocalpool: stop timed out: %w", ctx.Err())
	}
	return
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
	inst.mu.Lock()
	defer inst.mu.Unlock()
	s.Live = inst.live
	s.Idle = len(inst.idle)
	s.Acquired = len(inst.acquired)
	s.PendingSpawns = inst.pendingSpawns
	s.Stopped = inst.stopped
	return
}

// afterAcquire records the moment the worker was handed to a caller,
// for the watchdog to age out forgotten Closes.
func (inst *Pool) afterAcquire(w *Worker) {
	inst.mu.Lock()
	inst.acquired[w] = time.Now()
	inst.mu.Unlock()
	inst.signalRefill()
}

// workerClosed is the Worker→Pool callback invoked from Worker.Close
// after the subprocess has been reaped and the tmpdir removed.
func (inst *Pool) workerClosed(w *Worker) {
	inst.mu.Lock()
	delete(inst.acquired, w)
	if _, ok := inst.tracked[w]; ok {
		delete(inst.tracked, w)
		inst.live--
	}
	needSignal := !inst.stopped && len(inst.idle)+inst.pendingSpawns < int(inst.cfg.MinIdle) &&
		inst.live+inst.pendingSpawns < int(inst.cfg.MaxConcurrent)
	inst.mu.Unlock()
	if needSignal {
		inst.signalRefill()
	}
}

// signalRefill nudges the refill goroutine to re-check depth.
// Non-blocking: a single buffered slot collapses repeated nudges.
func (inst *Pool) signalRefill() {
	select {
	case inst.refillTrigger <- struct{}{}:
	default:
	}
}

// spawnAndCount spawns one worker, registering it in the pool's
// live/tracked bookkeeping on success. Caller must have already
// incremented pendingSpawns under the lock. On any return path,
// pendingSpawns is decremented (success or failure).
func (inst *Pool) spawnAndCount(ctx context.Context) (w *Worker, err error) {
	// Bounded parallelism on spawns.
	select {
	case inst.spawnSem <- struct{}{}:
	case <-ctx.Done():
		inst.decPendingSpawns()
		err = eh.Errorf("chlocalpool: spawn cancelled: %w", ctx.Err())
		return
	case <-inst.stopCh:
		inst.decPendingSpawns()
		err = eh.Errorf("chlocalpool: pool stopped during spawn wait")
		return
	}
	defer func() { <-inst.spawnSem }()

	spawnCtx, cancel := context.WithTimeout(ctx, inst.cfg.SpawnTimeout*4)
	defer cancel()
	w, err = newWorker(spawnCtx, inst)
	if err != nil {
		inst.decPendingSpawns()
		return
	}

	inst.mu.Lock()
	if inst.stopped {
		inst.pendingSpawns--
		inst.mu.Unlock()
		_ = w.Close()
		w = nil
		err = eh.Errorf("chlocalpool: pool stopped during spawn")
		return
	}
	inst.live++
	inst.pendingSpawns--
	inst.tracked[w] = struct{}{}
	inst.mu.Unlock()
	return
}

func (inst *Pool) decPendingSpawns() {
	inst.mu.Lock()
	inst.pendingSpawns--
	inst.mu.Unlock()
}

// refillLoop consumes refillTrigger nudges and keeps len(idle) +
// pendingSpawns >= MinIdle, bounded by MaxConcurrent.
func (inst *Pool) refillLoop() {
	defer inst.bg.Done()
	for {
		select {
		case <-inst.stopCh:
			return
		case <-inst.refillTrigger:
		}
		for inst.tryStartRefillSpawn() {
		}
	}
}

func (inst *Pool) tryStartRefillSpawn() (started bool) {
	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		return
	}
	if len(inst.idle)+inst.pendingSpawns >= int(inst.cfg.MinIdle) {
		inst.mu.Unlock()
		return
	}
	if inst.live+inst.pendingSpawns >= int(inst.cfg.MaxConcurrent) {
		inst.mu.Unlock()
		return
	}
	inst.pendingSpawns++
	inst.mu.Unlock()

	inst.bg.Add(1)
	go inst.refillSpawnOnce()
	return true
}

func (inst *Pool) refillSpawnOnce() {
	defer inst.bg.Done()

	// Use a parent context that observes stopCh so a slow spawn unblocks
	// on shutdown.
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-inst.stopCh:
			cancel()
		case <-parent.Done():
		}
	}()

	w, err := inst.spawnAndCount(parent)
	if err != nil {
		inst.logger.Warn().Err(err).Msg("chlocalpool: refill spawn failed")
		return
	}
	select {
	case inst.idle <- w:
	case <-inst.stopCh:
		_ = w.Close()
	}
}

// watchdogLoop reaps acquired workers older than WatchdogMaxLifetime.
// Idle workers are left alone — they sit on stdin without consuming
// CPU and have nothing to leak. This is the belt-and-suspenders for
// forgotten Worker.Close calls (ADR-0028 §SD3).
func (inst *Pool) watchdogLoop() {
	defer inst.bg.Done()
	tick := inst.cfg.WatchdogMaxLifetime / 4
	if tick < 50*time.Millisecond {
		tick = 50 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-inst.stopCh:
			return
		case <-t.C:
			inst.watchdogSweep()
		}
	}
}

func (inst *Pool) watchdogSweep() {
	deadline := inst.cfg.WatchdogMaxLifetime
	now := time.Now()
	var candidates []*Worker
	inst.mu.Lock()
	for w, acquiredAt := range inst.acquired {
		if now.Sub(acquiredAt) > deadline {
			candidates = append(candidates, w)
		}
	}
	inst.mu.Unlock()
	for _, w := range candidates {
		inst.logger.Warn().Dur("acquired_age", time.Since(w.bornAt)).
			Msg("chlocalpool: watchdog reaping forgotten worker")
		_ = w.Close()
	}
}
