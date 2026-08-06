package providersgodep

import (
	"cmp"
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Collection status values, reported in go_collection.status.
const (
	statusCollecting = "collecting" // started, no result yet
	statusReady      = "ready"      // rows are the tables' answer
	statusFailed     = "failed"     // collection failed; error carries why
)

// firstWaitBudget is how long the first query waits for collection before
// answering with zero rows. Sized against the measured warm cost on this
// repository (~1.4 s) with room to spare, and well under any plausible
// query timeout — a cold toolchain run is what the budget exists to bound,
// and that case degrades to an empty table plus status='collecting' rather
// than a slow query.
const firstWaitBudget = 5 * time.Second

// collectTimeout bounds the collection itself, so a wedged toolchain leaves
// a failed status behind rather than a goroutine that never finishes.
const collectTimeout = 5 * time.Minute

// edge is one row of go_imports: an import relation with both endpoints'
// paths denormalised, so the common query never joins back to go_packages
// twice (the counterpart to ADR-0064 §SD2's adjacency-in-node fact shape,
// which is the wrong shape for a recursive walk).
type edge struct {
	srcID   uint64
	dstID   uint64
	srcPath string
	dstPath string
}

// snapshot is one completed collection attempt — the value every provider
// reads. It is written once and never mutated, so providers share it without
// copying.
type snapshot struct {
	man      godep.Manifest
	edges    []edge
	status   string
	errText  string
	finished time.Time
	duration time.Duration
}

// cache holds the process-lifetime collection shared by the four providers.
// Collection runs at most once: a failure is sticky and reported through
// go_collection rather than retried per query, so a missing toolchain costs
// one attempt, not one per query.
type cache struct {
	cfg    Config
	budget time.Duration
	// load performs the collection. It is a field so tests can drive the
	// ready / failed / still-collecting paths without running the toolchain;
	// production always gets the live collector below.
	load func(context.Context) (godep.Manifest, error)

	// vol backs the go_packages volume columns. Separate from the graph
	// collection on purpose (ADR-0173 §SD3): it costs seconds and only a
	// query that selects a volume column should pay for it.
	vol *volumeCache

	mu      sync.Mutex
	started bool
	startAt time.Time
	done    chan struct{}
	snap    *snapshot
}

func newCache(cfg Config) (inst *cache) {
	inst = &cache{cfg: cfg, budget: firstWaitBudget, vol: newVolumeCache(cfg)}
	inst.load = func(ctx context.Context) (godep.Manifest, error) {
		return godepcollect.New(godepcollect.Config{Dir: cfg.Root, Tags: cfg.Tags}).Load(ctx)
	}
	return
}

// get returns the collection result, starting collection on first call and
// waiting up to the budget for it. A collection still running past the
// budget yields a status-only snapshot (no rows), which is what makes an
// expensive first collect degrade to an empty table instead of a stalled
// query.
func (inst *cache) get() (s *snapshot) {
	inst.mu.Lock()
	if inst.snap != nil {
		s = inst.snap
		inst.mu.Unlock()
		return
	}
	if !inst.started {
		inst.started = true
		inst.startAt = time.Now()
		inst.done = make(chan struct{})
		go inst.collect(inst.done)
	}
	done, startAt := inst.done, inst.startAt
	inst.mu.Unlock()

	timer := time.NewTimer(inst.budget)
	defer timer.Stop()
	select {
	case <-done:
		inst.mu.Lock()
		s = inst.snap
		inst.mu.Unlock()
	case <-timer.C:
		s = &snapshot{status: statusCollecting, duration: time.Since(startAt)}
	}
	return
}

// collect runs the live collector and publishes the result. It owns the
// only write to inst.snap, which happens before done is closed.
func (inst *cache) collect(done chan struct{}) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	s := &snapshot{status: statusReady}
	man, err := inst.load(ctx)
	if err == nil && man.Run.RootModulePath == "" {
		// Off-repo, `go list ./...` does not fail — it reports one synthetic
		// package for the unresolved pattern. Without a main module there is
		// no root to classify against, so that lone row is noise, not data:
		// fail the collection instead and say where to point it. (Found by
		// launching the applet from a directory with no go.mod.)
		err = eh.Errorf("no main Go module at %q — set BOXER_GODEP_ROOT to the module to collect",
			cmp.Or(inst.cfg.Root, "the working directory"))
	}
	if err != nil {
		s.status = statusFailed
		s.errText = err.Error()
		inst.cfg.Log.Warn().Err(err).Str("root", inst.cfg.Root).
			Msg("providersgodep: collection failed; keelson go_* tables stay empty")
	} else {
		s.man = man
		s.edges = buildEdges(man)
		inst.cfg.Log.Info().Str("root", inst.cfg.Root).
			Int("packages", len(man.Packages)).Int("edges", len(s.edges)).
			Dur("took", time.Since(started)).Msg("providersgodep: collected the Go package graph")
	}
	s.finished = time.Now()
	s.duration = s.finished.Sub(started)

	inst.mu.Lock()
	inst.snap = s
	inst.mu.Unlock()
	close(done)
}

// buildEdges flattens the manifest's adjacency into the go_imports row set,
// in (source path, target id) order — the manifest's packages are already
// path-sorted and each import set is id-sorted, so repeated collections of
// the same module state produce identical row order.
//
// An import whose target is absent from the manifest keeps its edge and
// carries an empty dst_path: go/packages walks the closure, so this is not
// expected, and dropping such an edge silently would hide the anomaly from
// the query that could report it.
func buildEdges(man godep.Manifest) (out []edge) {
	byID := make(map[uint64]string, len(man.Packages))
	for i := range man.Packages {
		byID[man.Packages[i].Id] = man.Packages[i].ImportPath
	}
	n := 0
	for i := range man.Packages {
		n += len(man.Packages[i].Imports)
	}
	out = make([]edge, 0, n)
	for i := range man.Packages {
		p := &man.Packages[i]
		for _, dst := range p.Imports {
			out = append(out, edge{srcID: p.Id, dstID: dst, srcPath: p.ImportPath, dstPath: byID[dst]})
		}
	}
	return
}
