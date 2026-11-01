// Package servicedemo is a headless app — a service (ADR-0352 (proposed)
// §SD1) — that does one visible thing: it counts ticks in the background
// and keeps the count under a persist key, so a reader can see the
// service host's lifecycle on something: keelson('services') shows it
// selected and running, its lifecycle rows carry its instance key, and
// `runtime.persist` holds a count that grows while it runs and survives
// the process when ClickHouse is up.
//
// It is the reference shape for a service: Mount starts the work on the
// instance's lifetime scope and returns, Cancel() ends the work, and
// Unmount has nothing left to release.
package servicedemo

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

const (
	// TickKey is the persist key the count lives under.
	TickKey = "ticks"
	// DefaultInterval is the tick period.
	DefaultInterval = time.Second
)

// App is one instance of the service.
type App struct {
	interval time.Duration
	log      zerolog.Logger
	// ticks is the count since Mount; read by tests through Ticks.
	ticks   chan uint64
	stopped chan struct{}
}

var _ app.AppI = (*App)(nil)

// New is an instance ticking every interval; zero is DefaultInterval.
func New(interval time.Duration) (inst *App) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	inst = &App{interval: interval, ticks: make(chan uint64, 1), stopped: make(chan struct{})}
	inst.ticks <- 0
	return
}

func (inst *App) Manifest() (m app.Manifest) { return manifest }

// Mount reads the stored count back, so a restart continues where the last
// run stopped, and starts the ticker on the lifetime scope. It returns at
// once: the service's work is the goroutine, not Mount.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.log = ctx.Log()
	start := uint64(0)
	if raw, found, gErr := ctx.Storage().Get(TickKey); gErr == nil && found {
		if n, pErr := strconv.ParseUint(string(raw), 10, 64); pErr == nil {
			start = n
		}
	}
	inst.set(start)
	run := func(c context.Context) {
		defer close(inst.stopped)
		t := time.NewTicker(inst.interval)
		defer t.Stop()
		n := start
		for {
			select {
			case <-c.Done():
				return
			case <-ctx.Cancel():
				return
			case <-t.C:
				n++
				inst.set(n)
				if sErr := ctx.Storage().Set(TickKey, []byte(strconv.FormatUint(n, 10))); sErr != nil {
					inst.log.Debug().Err(sErr).Msg("servicedemo: tick not stored")
				}
			}
		}
	}
	if scope := app.LifetimeOf(ctx); scope != nil && scope.Go(run) {
		return
	}
	// A host without a lifetime scope (a bare test context): plain goroutine.
	go run(context.Background())
	return
}

// Frame is never called on a headless app.
func (inst *App) Frame(ctx app.FrameContextI) (err error) { return }

// Unmount has nothing to release: the ticker ended with Cancel(), and the
// host drains the scope it ran on.
func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *App) set(n uint64) {
	select {
	case <-inst.ticks:
	default:
	}
	inst.ticks <- n
}

// Ticks is the count so far.
func (inst *App) Ticks() (n uint64) {
	n = <-inst.ticks
	inst.ticks <- n
	return
}

// Stopped is closed once the ticker goroutine has ended.
func (inst *App) Stopped() (done <-chan struct{}) { return inst.stopped }
