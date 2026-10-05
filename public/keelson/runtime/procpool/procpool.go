package procpool

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// WorkerI is one pooled worker. Close terminates it and frees what it holds,
// and must be idempotent: the pool calls it on Stop and from the watchdog,
// possibly while the caller holding the worker calls it too.
type WorkerI interface {
	Close() error
}

// ReleaseFunc tells the pool that a worker is gone. A worker calls it exactly
// once, after everything the worker holds is freed — so a slot the pool counts
// as free is free, which matters where the slot stands for a licence seat or a
// sandbox. A worker that notices its process died on its own calls it as well;
// that is how the pool learns an idle worker is dead.
type ReleaseFunc func()

// SpawnerI makes workers. SpawnE returns once the worker can serve, not merely
// once its process started; a worker that takes seconds to become ready is
// what the pool exists to hide. On error SpawnE must have freed everything
// and must not call release.
type SpawnerI[W WorkerI] interface {
	SpawnE(ctx context.Context, release ReleaseFunc) (w W, err error)
}

// Config parameterises a Pool. Every zero value is meaningful rather than a
// request for a default: MinIdle 0 keeps no spares, SpawnTimeout 0 sets no
// deadline beyond the caller's, WatchdogMaxLifetime 0 lets a caller hold a
// worker indefinitely. MaxConcurrent must be at least 1; SpawnConcurrency 0
// means one spawn at a time.
type Config struct {
	MinIdle             uint8
	MaxConcurrent       uint8
	SpawnConcurrency    uint8
	SpawnTimeout        time.Duration
	WatchdogMaxLifetime time.Duration
}

func (inst Config) validate() (err error) {
	if inst.MaxConcurrent == 0 {
		err = eh.Errorf("MaxConcurrent must be at least 1")
		return
	}
	if inst.MinIdle > inst.MaxConcurrent {
		err = eb.Build().Uint8("minIdle", inst.MinIdle).Uint8("maxConcurrent", inst.MaxConcurrent).Errorf("MinIdle exceeds MaxConcurrent")
		return
	}
	if inst.SpawnConcurrency > inst.MaxConcurrent {
		err = eb.Build().Uint8("spawnConcurrency", inst.SpawnConcurrency).Uint8("maxConcurrent", inst.MaxConcurrent).Errorf("SpawnConcurrency exceeds MaxConcurrent")
		return
	}
	return
}

// slot is the pool's record of one worker. Identity is the slot, not W, so W
// need not be comparable and a release that arrives before the spawn returns
// still finds its record.
type slot[W WorkerI] struct {
	w        W
	bornAt   time.Time
	released bool
}

// Pool keeps pre-spawned workers per ADR-0285 §SD1. New starts its background
// goroutines; the caller must call StopE.
type Pool[W WorkerI] struct {
	cfg     Config
	spawner SpawnerI[W]
	logger  zerolog.Logger

	spawnSem      chan struct{}
	refillTrigger chan struct{}
	stopCh        chan struct{}
	bg            sync.WaitGroup

	stopOnce sync.Once
	stopDone chan struct{}

	mu sync.Mutex
	// changed is closed and replaced whenever a waiting Acquire could now
	// succeed: a worker turned idle, or a release freed capacity.
	changed       chan struct{}
	stopped       bool
	live          int
	pendingSpawns int
	idle          []*slot[W]
	tracked       map[*slot[W]]struct{}
	acquired      map[*slot[W]]time.Time
}

// New validates cfg and starts filling MinIdle spares.
func New[W WorkerI](cfg Config, spawner SpawnerI[W], logger zerolog.Logger) (p *Pool[W], err error) {
	if err = cfg.validate(); err != nil {
		return
	}
	if cfg.SpawnConcurrency == 0 {
		cfg.SpawnConcurrency = 1
	}
	p = &Pool[W]{
		cfg:           cfg,
		spawner:       spawner,
		logger:        logger,
		spawnSem:      make(chan struct{}, cfg.SpawnConcurrency),
		refillTrigger: make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		stopDone:      make(chan struct{}),
		changed:       make(chan struct{}),
		idle:          make([]*slot[W], 0, cfg.MaxConcurrent),
		tracked:       make(map[*slot[W]]struct{}, cfg.MaxConcurrent),
		acquired:      make(map[*slot[W]]time.Time, cfg.MaxConcurrent),
	}
	p.bg.Add(1)
	go p.refillLoop()
	if cfg.WatchdogMaxLifetime > 0 {
		p.bg.Add(1)
		go p.watchdogLoop()
	}
	p.signalRefill()
	return
}

// AcquireE hands out a worker: an idle one if there is one, else one spawned
// for this caller while MaxConcurrent allows, else the next one to turn idle.
// The caller must Close the worker.
func (inst *Pool[W]) AcquireE(ctx context.Context) (w W, err error) {
	for {
		inst.mu.Lock()
		if inst.stopped {
			inst.mu.Unlock()
			err = eh.Errorf("pool stopped")
			return
		}
		if len(inst.idle) > 0 {
			s := inst.idle[0]
			inst.idle = slices.Delete(inst.idle, 0, 1)
			inst.acquired[s] = time.Now()
			inst.mu.Unlock()
			inst.signalRefill()
			w = s.w
			return
		}
		if inst.live+inst.pendingSpawns < int(inst.cfg.MaxConcurrent) {
			inst.pendingSpawns++
			inst.mu.Unlock()
			var s *slot[W]
			s, err = inst.spawnAndRegister(ctx, true)
			if err != nil {
				return
			}
			w = s.w
			return
		}
		wait := inst.changed
		inst.mu.Unlock()

		select {
		case <-wait:
		case <-ctx.Done():
			err = eh.Errorf("acquire cancelled: %w", ctx.Err())
			return
		case <-inst.stopCh:
			err = eh.Errorf("pool stopped")
			return
		}
	}
}

// StopE closes every worker, including those callers still hold, and joins
// the pool's goroutines. The teardown runs once; every call waits for it
// under its own ctx, so a caller whose deadline expired can call again.
func (inst *Pool[W]) StopE(ctx context.Context) (err error) {
	inst.stopOnce.Do(inst.beginStop)
	select {
	case <-inst.stopDone:
		return
	case <-ctx.Done():
		err = eh.Errorf("stop timed out: %w", ctx.Err())
		return
	}
}

func (inst *Pool[W]) beginStop() {
	inst.mu.Lock()
	inst.stopped = true
	close(inst.stopCh)
	workers := make([]W, 0, len(inst.tracked))
	for s := range inst.tracked {
		workers = append(workers, s.w)
	}
	inst.idle = inst.idle[:0]
	inst.notifyLocked()
	inst.mu.Unlock()

	go func() {
		defer close(inst.stopDone)
		var wg sync.WaitGroup
		for _, w := range workers {
			wg.Go(func() { _ = w.Close() })
		}
		wg.Wait()
		inst.bg.Wait()
	}()
}

// Stats is a snapshot of the pool's cardinality.
type Stats struct {
	Live          int
	Idle          int
	Acquired      int
	PendingSpawns int
	Stopped       bool
}

// Stats snapshots the pool under its lock.
func (inst *Pool[W]) Stats() (s Stats) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	s.Live = inst.live
	s.Idle = len(inst.idle)
	s.Acquired = len(inst.acquired)
	s.PendingSpawns = inst.pendingSpawns
	s.Stopped = inst.stopped
	return
}

func (inst *Pool[W]) notifyLocked() {
	close(inst.changed)
	inst.changed = make(chan struct{})
}

// release is the ReleaseFunc behind one slot. A slot released while idle
// leaves the idle list here, so a dead spare is never handed out.
func (inst *Pool[W]) release(s *slot[W]) {
	inst.mu.Lock()
	s.released = true
	delete(inst.acquired, s)
	if _, ok := inst.tracked[s]; ok {
		delete(inst.tracked, s)
		inst.live--
		inst.idle = slices.DeleteFunc(inst.idle, func(x *slot[W]) bool { return x == s })
	}
	inst.notifyLocked()
	needSignal := !inst.stopped && len(inst.idle)+inst.pendingSpawns < int(inst.cfg.MinIdle) &&
		inst.live+inst.pendingSpawns < int(inst.cfg.MaxConcurrent)
	inst.mu.Unlock()
	if needSignal {
		inst.signalRefill()
	}
}

func (inst *Pool[W]) signalRefill() {
	select {
	case inst.refillTrigger <- struct{}{}:
	default:
	}
}

// spawnAndRegister spawns one worker and records it as acquired or idle. The
// caller has already counted it in pendingSpawns; every return path uncounts
// it.
func (inst *Pool[W]) spawnAndRegister(ctx context.Context, acquire bool) (s *slot[W], err error) {
	select {
	case inst.spawnSem <- struct{}{}:
	case <-ctx.Done():
		inst.decPendingSpawns()
		err = eh.Errorf("spawn cancelled: %w", ctx.Err())
		return
	case <-inst.stopCh:
		inst.decPendingSpawns()
		err = eh.Errorf("pool stopped during spawn wait")
		return
	}
	defer func() { <-inst.spawnSem }()

	spawnCtx := ctx
	if inst.cfg.SpawnTimeout > 0 {
		var cancel context.CancelFunc
		spawnCtx, cancel = context.WithTimeout(ctx, inst.cfg.SpawnTimeout)
		defer cancel()
	}
	s = &slot[W]{}
	w, err := inst.spawner.SpawnE(spawnCtx, func() { inst.release(s) })
	if err != nil {
		inst.decPendingSpawns()
		s = nil
		return
	}
	s.w = w
	s.bornAt = time.Now()

	inst.mu.Lock()
	inst.pendingSpawns--
	if inst.stopped {
		inst.mu.Unlock()
		_ = w.Close()
		s = nil
		err = eh.Errorf("pool stopped during spawn")
		return
	}
	if s.released {
		inst.mu.Unlock()
		s = nil
		err = eh.Errorf("worker exited before it was handed over")
		return
	}
	inst.live++
	inst.tracked[s] = struct{}{}
	if acquire {
		inst.acquired[s] = time.Now()
	} else {
		inst.idle = append(inst.idle, s)
		inst.notifyLocked()
	}
	inst.mu.Unlock()
	return
}

func (inst *Pool[W]) decPendingSpawns() {
	inst.mu.Lock()
	inst.pendingSpawns--
	inst.notifyLocked()
	inst.mu.Unlock()
}

// refillLoop keeps idle + pendingSpawns at MinIdle, bounded by
// MaxConcurrent. A failed spawn is not retried until the next nudge — an
// AcquireE, a release — so a worker that cannot start costs one attempt per
// demand rather than a loop.
func (inst *Pool[W]) refillLoop() {
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

func (inst *Pool[W]) tryStartRefillSpawn() (started bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.stopped {
		return
	}
	if len(inst.idle)+inst.pendingSpawns >= int(inst.cfg.MinIdle) {
		return
	}
	if inst.live+inst.pendingSpawns >= int(inst.cfg.MaxConcurrent) {
		return
	}
	inst.pendingSpawns++
	inst.bg.Add(1)
	go inst.refillSpawnOnce()
	started = true
	return
}

func (inst *Pool[W]) refillSpawnOnce() {
	defer inst.bg.Done()
	// The spawn outlives any caller, so its parent observes only Stop.
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-inst.stopCh:
			cancel()
		case <-parent.Done():
		}
	}()
	if _, err := inst.spawnAndRegister(parent, false); err != nil {
		inst.logger.Warn().Err(err).Msg("refill spawn failed")
	}
}

// watchdogLoop reaps acquired workers held longer than WatchdogMaxLifetime —
// the backstop for a caller that never Closes. Idle workers are not judged.
func (inst *Pool[W]) watchdogLoop() {
	defer inst.bg.Done()
	tick := max(inst.cfg.WatchdogMaxLifetime/4, 50*time.Millisecond)
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

type reapCandidate[W WorkerI] struct {
	s          *slot[W]
	acquiredAt time.Time
}

func (inst *Pool[W]) watchdogSweep() {
	deadline := inst.cfg.WatchdogMaxLifetime
	now := time.Now()
	var candidates []reapCandidate[W]
	inst.mu.Lock()
	for s, acquiredAt := range inst.acquired {
		if now.Sub(acquiredAt) > deadline {
			candidates = append(candidates, reapCandidate[W]{s: s, acquiredAt: acquiredAt})
		}
	}
	inst.mu.Unlock()
	for _, c := range candidates {
		inst.logger.Warn().
			Dur("acquired_age", now.Sub(c.acquiredAt)).
			Dur("worker_age", now.Sub(c.s.bornAt)).
			Msg("watchdog reaping forgotten worker")
		_ = c.s.w.Close()
	}
}
