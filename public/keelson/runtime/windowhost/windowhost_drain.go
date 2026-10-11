package windowhost

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// The drain step of the closing edge (ADR-0322, proposed): between an
// instance's Unmount and the close of its bus client, the host waits for the
// instance's lifetime scope (app.LifetimeI) to be done, so work the app
// counted in writes its tail over a live bus. A window close drains off the
// render goroutine — the window is gone from the desktop at once, and its
// client closes when the drain ends; a shutdown drains every instance in
// parallel and waits, inside a budget cut from the shutdown grace.

const (
	// DefaultCloseDrain bounds the drain after a window close. Nothing
	// waits on it but the instance's own resources, so it is sized for a
	// slow store write rather than for a person.
	DefaultCloseDrain = 30 * time.Second
	// DefaultShutdownDrain bounds the drain at shutdown, when the process
	// waits on it. It must stay below the host's shutdown grace, past which
	// the process exits regardless.
	DefaultShutdownDrain = 5 * time.Second
)

// drainState is the host's account of instances between Unmount and unload.
type drainState struct {
	mu       sync.Mutex
	close    time.Duration
	shutdown time.Duration
	// pending counts window-close drains still running, so a shutdown
	// waits for them as for its own.
	pending sync.WaitGroup
}

// SetDrainDeadlines sets how long the host waits for an instance's lifetime
// scope after a window close and at shutdown; zero keeps the default.
func (inst *Inst) SetDrainDeadlines(onClose time.Duration, onShutdown time.Duration) {
	inst.drains.mu.Lock()
	inst.drains.close, inst.drains.shutdown = onClose, onShutdown
	inst.drains.mu.Unlock()
}

func (inst *Inst) drainDeadlines() (onClose time.Duration, onShutdown time.Duration) {
	inst.drains.mu.Lock()
	onClose, onShutdown = inst.drains.close, inst.drains.shutdown
	inst.drains.mu.Unlock()
	if onClose <= 0 {
		onClose = DefaultCloseDrain
	}
	if onShutdown <= 0 {
		onShutdown = DefaultShutdownDrain
	}
	return
}

// closing is one reaped window past its Unmount: the windows whose
// resources are released once the scopes drained, and the stop row still to
// write. A window whose instance stays mounted through another, or that was
// never mounted, releases its own resources at reap and carries none.
type closing struct {
	w      *window
	reason string
	unload []*window
}

// isDrained says no scope of the closing has work left: an instance that
// counted nothing into its scope is done once its window left.
func (inst closing) isDrained() (drained bool) {
	for _, uw := range inst.unload {
		if uw.lifetime == nil {
			continue
		}
		select {
		case <-uw.lifetime.Done():
		default:
			return false
		}
	}
	return true
}

// drainAndRelease waits for the lifetime scopes of the windows to unload,
// bounded by ctx, then closes their bus clients and writes the stop row.
func (inst *Inst) drainAndRelease(ctx context.Context, cl closing, facts factsstore.FactsStoreI, runId string) {
	for _, uw := range cl.unload {
		if uw.lifetime == nil {
			continue
		}
		start := time.Now()
		if err := uw.lifetime.Drain(ctx); err != nil {
			inst.logger.Warn().Err(err).
				Str("id", string(uw.manifest.Id)).
				Uint64("windowKey", uint64(uw.key)).
				Dur("waited", time.Since(start)).
				Msg("windowhost: the instance did not drain before its bus closed")
		}
	}
	for _, uw := range cl.unload {
		uw.unload(inst.logger)
	}
	if facts != nil {
		emitStopped(facts, inst.logger, runId, cl.w, cl.reason)
	}
}

// releaseLater drains a window-close off the render goroutine.
func (inst *Inst) releaseLater(cl closing, facts factsstore.FactsStoreI, runId string) {
	onClose, _ := inst.drainDeadlines()
	inst.drains.pending.Add(1)
	go func() {
		defer inst.drains.pending.Done()
		ctx, cancel := context.WithTimeout(context.Background(), onClose)
		defer cancel()
		inst.drainAndRelease(ctx, cl, facts, runId)
	}()
}

// releaseAll drains every closing in parallel and waits, together with the
// window-close drains already running, within the shutdown budget.
func (inst *Inst) releaseAll(cls []closing, facts factsstore.FactsStoreI, runId string) {
	_, onShutdown := inst.drainDeadlines()
	ctx, cancel := context.WithTimeout(context.Background(), onShutdown)
	defer cancel()
	var wg sync.WaitGroup
	for _, cl := range cls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst.drainAndRelease(ctx, cl, facts, runId)
		}()
	}
	wg.Wait()
	earlier := make(chan struct{})
	go func() {
		inst.drains.pending.Wait()
		close(earlier)
	}()
	select {
	case <-earlier:
	case <-ctx.Done():
		inst.logger.Warn().Msg("windowhost: closed windows were still draining at the end of the shutdown budget")
	}
}
