//go:build llm_generated_opus47

package task

import (
	"context"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/task/estimator"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// nanoidLen is the number of characters in an auto-generated TaskIdT.
// 21 characters from the default URL-safe alphabet (64 symbols) gives
// ~126 bits of entropy — overkill for a per-process registry, comfortable
// for a future M4 NATS cluster.
const nanoidLen = 21

// SpawnOpts configures a new task. All fields are optional except Kind,
// which is the schema-key observers dispatch on. Title defaults to Kind.
// Id is auto-generated as a nanoid when empty.
type SpawnOpts struct {
	// Id sets a deterministic task id. Empty means "generate one via
	// nanoid".
	Id TaskIdT

	// Kind groups tasks for observer dispatch ("ch.export", "fs.scan",
	// "kafka.catchup"). Required. Conventional strings, not a registered
	// enum.
	Kind string

	// Title is the human-readable label observers display. Defaults to
	// Kind when empty.
	Title string

	// OwnerAppId attributes the task to the spawning app. Carried in the
	// TaskCreated payload for audit and display; not validated. Empty is
	// allowed for runtime-side tasks that don't correspond to a user app.
	// Auto-filled by task.ForApp(ctx) — direct callers of Spawn
	// supply it themselves.
	OwnerAppId app.AppIdT

	// OwnerTileKey is the host-minted per-window instance id. Auto-filled
	// by task.ForApp(ctx) so audit rows can join back to
	// AppLifecycleRow.TileKey. Direct callers may leave it zero.
	OwnerTileKey uint64

	// OwnerRunId is the process-wide run id. Auto-filled by
	// task.ForApp(ctx) so audit rows can join back to
	// RuntimeStartRow.RunId. Direct callers may leave it empty.
	OwnerRunId string

	// Cancellable surfaces a hint to consumers (UI: should we show a
	// cancel button?). The handle's cancel subscription is always
	// active; this field controls observer affordances, not engine
	// behaviour.
	Cancellable bool

	// EstimatedMs gives an initial duration guess for observers (e.g.,
	// to show a progress bar before the first Report). Zero means
	// "unknown".
	EstimatedMs int64

	// HeartbeatMs overrides DefaultIndeterminateHeartbeatMs for tasks
	// whose total is unknown. Zero ⇒ default.
	HeartbeatMs int64

	// Logger is the producer-side diagnostic logger. task.ForApp(ctx)
	// pre-contextualises it with run_id / app_id / instance_id; the
	// handle adds task_id internally. nil ⇒ handle uses a no-op logger
	// (zero-value zerolog.Logger writes nowhere). Pointer because
	// zerolog.Logger contains a []byte and cannot be compared with ==.
	Logger *zerolog.Logger
}

// Spawn registers a new task and returns a HandleI for the producer side.
// The supplied bus is used to:
//
//   - publish task.<id>.created once now;
//   - publish task.<id>.progress as the handle reports;
//   - publish task.<id>.done or task.<id>.error on terminal;
//   - subscribe to task.<id>.cancel for the lifetime of the handle.
//
// The handle's Ctx() is derived from parent and cancels on parent-cancel,
// bus-cancel, or Done/Error.
func Spawn(parent context.Context, bus app.BusI, opts SpawnOpts) (h HandleI, err error) {
	h, err = spawnWithCancel(parent, bus, opts, time.Now, nil)
	return
}

// SpawnWithClock is Spawn with an injected clock. Tests use this to make
// AtMs values deterministic; production code calls Spawn.
func SpawnWithClock(parent context.Context, bus app.BusI, opts SpawnOpts, nowFn func() time.Time) (h HandleI, err error) {
	h, err = spawnWithCancel(parent, bus, opts, nowFn, nil)
	return
}

// spawnWithCancel is the internal Spawn that additionally observes a
// host-supplied mount-cancel channel. BusApi.Spawn passes its
// ApiConfig.MountCancel here so the single monitor goroutine cascades a
// window close into the task — replacing the previous composed-context +
// watcher-goroutine pair, which leaked one goroutine and one context per
// task until the mount channel fired. A nil cancelCh selects never.
func spawnWithCancel(parent context.Context, bus app.BusI, opts SpawnOpts, nowFn func() time.Time, cancelCh <-chan struct{}) (h HandleI, err error) {
	if bus == nil {
		err = eh.Errorf("task: spawn: nil bus")
		return
	}
	if opts.Kind == "" {
		err = eh.Errorf("task: spawn: empty Kind")
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	if nowFn == nil {
		nowFn = time.Now
	}

	id := opts.Id
	if id == "" {
		var raw string
		raw, err = gonanoid.New(nanoidLen)
		if err != nil {
			err = eh.Errorf("task: spawn: generate id: %w", err)
			return
		}
		id = TaskIdT(raw)
	}

	heartbeatMs := opts.HeartbeatMs
	if heartbeatMs <= 0 {
		heartbeatMs = DefaultIndeterminateHeartbeatMs
	}

	ctx, cancel := context.WithCancel(parent)
	var base zerolog.Logger
	if opts.Logger != nil {
		base = *opts.Logger
	}
	logger := base.With().Str("task_id", string(id)).Logger()
	handle := &Handle{
		id:          id,
		kind:        opts.Kind,
		ownerAppId:  opts.OwnerAppId,
		bus:         bus,
		now:         nowFn,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		est:         estimator.New(),
		heartbeatMs: heartbeatMs,
		logger:      logger,
	}

	// Subscribe before publishing TaskCreated so a peer that observes
	// created and immediately publishes cancel reaches our handler. In
	// the in-proc bus this is order-significant: Publish dispatches
	// synchronously inside Publish().
	var unsubscribe func()
	unsubscribe, err = bus.Subscribe(SubjectCancel(id), func(_ *app.Msg) {
		cancel()
	})
	if err != nil {
		cancel()
		err = eh.Errorf("task: spawn: subscribe cancel: %w", err)
		return
	}
	handle.unsubscribeCancel = unsubscribe

	title := opts.Title
	if title == "" {
		title = opts.Kind
	}

	createdAt := nowFn().UTC()
	created := taskcreated.TaskCreated{
		TaskId:       string(id),
		Kind:         opts.Kind,
		Title:        title,
		OwnerAppId:   string(opts.OwnerAppId),
		OwnerTileKey: opts.OwnerTileKey,
		OwnerRunId:   opts.OwnerRunId,
		CancellableB: opts.Cancellable,
		EstimatedMs:  opts.EstimatedMs,
		At:           createdAt,
	}
	var b []byte
	b, err = MarshalTaskCreated(created)
	if err != nil {
		unsubscribe()
		cancel()
		err = eh.Errorf("task: spawn: marshal created: %w", err)
		return
	}
	err = bus.Publish(SubjectCreated(id), b)
	if err != nil {
		unsubscribe()
		cancel()
		err = eh.Errorf("task: spawn: publish created: %w", err)
		return
	}

	// Monitor goroutine: cascade external cancellation (parent context or the
	// host mount-cancel channel) into the handle, and — crucially — exit as
	// soon as the handle reaches a terminal state via handle.done. The prior
	// implementation blocked solely on parent.Done(), so a task completed via
	// Done/Error under a long-lived (or Background) parent leaked this
	// goroutine for the process lifetime.
	go func() {
		select {
		case <-parent.Done():
			handle.terminateExternal()
		case <-cancelCh:
			handle.terminateExternal()
		case <-handle.done:
			// Terminal reached via Done/Error; finishLocked already tore down
			// the cancel subscription and cancelled Ctx. Nothing to do.
		}
	}()

	h = handle
	return
}
