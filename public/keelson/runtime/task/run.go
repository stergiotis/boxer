package task

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Run runs work on the calling goroutine as a task of api, for the work
// bgjob's runners do not fit. Call it from the goroutine the work runs on,
// never from the render goroutine: creating the task publishes on the bus,
// and the supervisor writes the task's audit row to the facts store before
// that publish returns.
//
// work gets the task's context, which ends on a cancel from the task
// monitor, on the window's close (for an api from ForApp) and with parent.
// Run publishes Done when work returns nil, and also when it returns after
// that context ended, whatever the error: a cancelled run is not a failed
// one, and a cancelled stream does not always fail with context.Canceled.
// Otherwise it publishes Error. A panic in work is published as Error and
// re-raised. Until work returns, a task from ForApp holds the instance's
// lifetime, so the window's drain waits for it (ADR-0322). Cancel parent
// only after Run returned: a cancel before it is announced on the task.
//
// A nil api, or a spawn the bus or the lifetime refuses, runs work untracked
// under parent.
func Run(api TaskApiI, parent context.Context, opts SpawnOpts, work func(ctx context.Context) error) (err error) {
	h := spawnFor(api, parent, opts)
	if h == nil {
		return work(parent)
	}
	ctx := h.Ctx()
	defer endOnPanic(h, opts)
	err = work(ctx)
	if err != nil && ctx.Err() == nil {
		_ = h.Error(err, opts.Kind+" failed")
		return
	}
	_ = h.Done(nil)
	return
}

// RunToCompletion runs work, which takes no context, as a task kept for the
// record: neither the task monitor nor the window's close stops it, so the
// task is not cancellable, and its error is always a failure. Like Run, it
// is called from the goroutine the work runs on, and a task from ForApp holds
// the instance's lifetime until work returns. A window closed meanwhile still
// announces a cancel on the task before its Done or Error, since the window
// asked; the audit trail carries both.
func RunToCompletion(api TaskApiI, opts SpawnOpts, work func() error) (err error) {
	opts.Cancellable = false
	h := spawnFor(api, context.Background(), opts)
	if h == nil {
		return work()
	}
	defer endOnPanic(h, opts)
	err = work()
	if err != nil {
		_ = h.Error(err, opts.Kind+" failed")
		return
	}
	_ = h.Done(nil)
	return
}

// spawnFor is api.Spawn, or nil when there is no api or it refused.
func spawnFor(api TaskApiI, parent context.Context, opts SpawnOpts) (h HandleI) {
	if api == nil {
		return nil
	}
	h, err := api.Spawn(parent, opts)
	if err != nil {
		// Most often a manifest without ProducerCaps, or a window already
		// draining.
		log.Debug().Err(err).Str("kind", opts.Kind).Msg("task: spawn refused; the work runs untracked")
		return nil
	}
	return h
}

// endOnPanic publishes a panicking work's Error and re-raises the panic.
func endOnPanic(h HandleI, opts SpawnOpts) {
	if r := recover(); r != nil {
		_ = h.Error(eh.Errorf("task: work panicked: %v", r), opts.Kind+" panicked")
		panic(r)
	}
}
