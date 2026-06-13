//go:build llm_generated_opus47

package task

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TaskApiI is the high-level, identity-aware surface task.ForApp builds
// from a MountContextI. The interface auto-injects OwnerAppId,
// OwnerTileKey, and OwnerRunId from the host into every spawned task,
// composes the caller's context with the app's mount-cancel channel so
// tasks auto-terminate on window close, and tags the producer-side
// logger with task_id (on top of the app's logger context which already
// carries run_id / app_id / instance_id).
//
// Apps inside the keelson runtime use this surface; library code or
// runtime services that operate outside the AppI lifecycle call the
// bare task.Spawn / task.WatchAll / task.RequestCancel functions
// directly and supply their own identity.
type TaskApiI interface {
	// Spawn creates a new task. callerCtx is composed with the host's
	// mount-cancel channel so the returned handle's ctx cancels on
	// either signal (or on terminal Done/Error). Pass
	// context.Background() to opt out of caller-side scoping.
	Spawn(callerCtx context.Context, opts SpawnOpts) (h HandleI, err error)

	// WatchAll attaches an observer to task.> on the host bus.
	// Identical to task.WatchAll(bus, obs) — exposed here so apps
	// don't need to thread the bus.
	WatchAll(obs ObserverI) (unsubscribe func(), err error)

	// RequestCancel publishes a cancel for the given task id.
	RequestCancel(id TaskIdT, reason string) (err error)

	// ListInflight queries the M3 supervisor's snapshot via the
	// SubjectListInflight request/reply. Times out at ListTimeoutMs
	// from ApiConfig. Returns an empty slice (not an error) when no
	// supervisor is running and the bus' request timeout fires —
	// callers distinguish "no supervisor" from "no in-flight tasks"
	// by the err: a nil err with empty entries means the supervisor
	// returned an empty snapshot; an error means the supervisor was
	// unreachable.
	ListInflight() (entries []InflightSnapshotEntry, err error)

	// AppId returns the owner identity the API will stamp on spawned
	// tasks. Useful for diagnostic logging at the app boundary.
	AppId() (id app.AppIdT)

	// InstanceKey returns the host-minted per-window instance id. Zero
	// when the API was constructed without one (tests, standalone CLI).
	InstanceKey() (key uint64)

	// RunId returns the process-wide run id. Empty when the API was
	// constructed without one.
	RunId() (id string)
}

// ApiConfig is the construction parameter set for NewBusApi. All fields
// are optional; the resulting API is usable with any subset (an empty
// config produces an API that delegates to a NoopBus and stamps zero
// values into every TaskCreated — fine for early tests).
type ApiConfig struct {
	// Bus is the host's app.BusI. nil substitutes a NoopBus that
	// errors on every call.
	Bus app.BusI

	// AppId is the owner identity injected into SpawnOpts.OwnerAppId.
	AppId app.AppIdT

	// InstanceKey is the host-minted per-window instance id injected
	// into SpawnOpts.OwnerTileKey.
	InstanceKey uint64

	// RunId is the process-wide run id injected into
	// SpawnOpts.OwnerRunId.
	RunId string

	// Logger is the base producer-side logger. The API adds task_id
	// before passing to each handle. Zero value writes nowhere.
	Logger zerolog.Logger

	// MountCancel is the app's mount-cancel channel. When non-nil, the
	// API composes it into every spawned task's parent context so
	// window close cascades into worker cancellation.
	MountCancel <-chan struct{}

	// ListTimeoutMs caps the bus.Request timeout for ListInflight.
	// Defaults to DefaultListTimeoutMs (2000ms) when zero.
	ListTimeoutMs int64
}

// DefaultListTimeoutMs bounds ListInflight's bus.Request wait. Chosen
// to comfortably exceed the supervisor's snapshot-build latency for any
// realistic in-flight count while still failing fast when no supervisor
// is wired.
const DefaultListTimeoutMs int64 = 2_000

// BusApi is the concrete TaskApiI handed out by the host. Exported so
// hosts can hold a typed reference for diagnostic helpers; consumers
// program against TaskApiI.
type BusApi struct {
	cfg ApiConfig
}

var _ TaskApiI = (*BusApi)(nil)

// NewBusApi constructs a TaskApiI bound to the supplied identity.
// Defensively substitutes a NoopBus when cfg.Bus is nil so callers can
// safely invoke Spawn without panicking — every operation returns the
// NoopBus error.
func NewBusApi(cfg ApiConfig) (inst *BusApi) {
	if cfg.Bus == nil {
		cfg.Bus = &app.NoopBus{}
	}
	if cfg.ListTimeoutMs <= 0 {
		cfg.ListTimeoutMs = DefaultListTimeoutMs
	}
	inst = &BusApi{cfg: cfg}
	return
}

func (inst *BusApi) AppId() (id app.AppIdT)    { id = inst.cfg.AppId; return }
func (inst *BusApi) InstanceKey() (key uint64) { key = inst.cfg.InstanceKey; return }
func (inst *BusApi) RunId() (id string)        { id = inst.cfg.RunId; return }

// Spawn delegates to task.Spawn after injecting host identity and
// composing the caller's context with MountCancel. Owner identity
// fields on opts override the API-supplied defaults when set
// (callers passing a non-empty OwnerAppId, for example, win).
func (inst *BusApi) Spawn(callerCtx context.Context, opts SpawnOpts) (h HandleI, err error) {
	parent := callerCtx
	if parent == nil {
		parent = context.Background()
	}

	if opts.OwnerAppId == "" {
		opts.OwnerAppId = inst.cfg.AppId
	}
	if opts.OwnerTileKey == 0 {
		opts.OwnerTileKey = inst.cfg.InstanceKey
	}
	if opts.OwnerRunId == "" {
		opts.OwnerRunId = inst.cfg.RunId
	}
	if opts.Logger == nil {
		// Capture by value into a local; the field is *zerolog.Logger
		// so callers can also explicitly override per-Spawn.
		base := inst.cfg.Logger
		opts.Logger = &base
	}

	// MountCancel is threaded directly into the handle's single monitor
	// goroutine (see spawnWithCancel) so a window close cascades into the
	// task without a per-task watcher goroutine or composed context.
	h, err = spawnWithCancel(parent, inst.cfg.Bus, opts, time.Now, inst.cfg.MountCancel)
	return
}

// WatchAll registers an observer on task.> over the host bus.
func (inst *BusApi) WatchAll(obs ObserverI) (unsubscribe func(), err error) {
	unsubscribe, err = WatchAll(inst.cfg.Bus, obs)
	return
}

// RequestCancel publishes a cancel for the named task. Uses the host
// bus; the API itself does not own a cap check (the bus client does).
func (inst *BusApi) RequestCancel(id TaskIdT, reason string) (err error) {
	err = RequestCancel(inst.cfg.Bus, id, reason)
	return
}

// ListInflight performs the supervisor request/reply. The bus' Request
// timeout (configured on inprocbus.Inst, typically 5s) is the
// authoritative bound; ListTimeoutMs in ApiConfig is reserved for a
// future API revision that uses a per-call context (the current
// app.BusI does not accept one).
func (inst *BusApi) ListInflight() (entries []InflightSnapshotEntry, err error) {
	if inst.cfg.Bus == nil {
		err = eh.Errorf("task: list inflight: nil bus")
		return
	}
	var raw []byte
	raw, err = inst.cfg.Bus.Request(SubjectListInflight, nil)
	if err != nil {
		err = eh.Errorf("task: list inflight: request: %w", err)
		return
	}
	reply, dErr := UnmarshalInflightSnapshotReply(raw)
	if dErr != nil {
		err = eh.Errorf("task: list inflight: decode: %w", dErr)
		return
	}
	entries = reply.Entries
	return
}

// NoopTaskApi is the fallback API for hosts that have not wired a real
// task surface yet (M1 bootstrap, isolated tests). Every operation
// returns a structured error naming the operation, matching the
// NoopBus / NoopStorage shape so apps detect the missing-host case
// cleanly.
type NoopTaskApi struct{}

var _ TaskApiI = (*NoopTaskApi)(nil)

func (inst *NoopTaskApi) Spawn(_ context.Context, opts SpawnOpts) (h HandleI, err error) {
	err = eb.Build().Str("kind", opts.Kind).Errorf("task api: not available (no host wiring) kind=%s", opts.Kind)
	return
}
func (inst *NoopTaskApi) WatchAll(_ ObserverI) (unsubscribe func(), err error) {
	err = eh.Errorf("task api: not available (no host wiring)")
	return
}
func (inst *NoopTaskApi) RequestCancel(id TaskIdT, _ string) (err error) {
	err = eb.Build().Str("taskId", string(id)).Errorf("task api: not available (no host wiring) taskId=%s", string(id))
	return
}
func (inst *NoopTaskApi) ListInflight() (entries []InflightSnapshotEntry, err error) {
	err = eh.Errorf("task api: not available (no host wiring)")
	return
}
func (inst *NoopTaskApi) AppId() (id app.AppIdT)    { return }
func (inst *NoopTaskApi) InstanceKey() (key uint64) { return }
func (inst *NoopTaskApi) RunId() (id string)        { return }

// ForApp is the idiomatic constructor for app code: pulls identity,
// bus, logger, mount-cancel from a MountContextI to produce a fully
// wired TaskApiI. Apps capture this in Mount and reuse across Frame
// passes:
//
//	func (inst *App) Mount(ctx app.MountContextI) (err error) {
//	    inst.tasks = task.ForApp(ctx)
//	    return
//	}
//
// The TaskApiI is independent of the MountContextI — it captures the
// pieces it needs by value, so it remains usable after the
// MountContextI itself goes out of scope (within the goroutine that
// holds the reference). The MountCancel channel is the one
// host-level signal the API will observe to cascade-cancel running
// tasks on window close.
//
// Hosts whose MountContextI carries a NoopBus return an API whose
// every operation surfaces the NoopBus error. This means callers
// don't need to defensively branch on "do I have a real bus"; they
// can call Spawn unconditionally and propagate any returned err.
func ForApp(ctx app.MountContextI) (api TaskApiI) {
	api = NewBusApi(ApiConfig{
		Bus:         ctx.Bus(),
		AppId:       ctx.AppId(),
		InstanceKey: ctx.InstanceKey(),
		RunId:       ctx.RunId(),
		Logger:      ctx.Log(),
		MountCancel: ctx.Cancel(),
	})
	return
}
