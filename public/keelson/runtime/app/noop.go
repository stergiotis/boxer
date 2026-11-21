package app

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// NoopBus is a BusI that errors on every call: what a context without a
// transport hands out (a test's, a CLI bootstrap's), so an app that uses
// Bus() there fails loudly with an error naming the subject.
type NoopBus struct{}

var _ BusI = (*NoopBus)(nil)

func (inst *NoopBus) Publish(subject string, payload []byte) (err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: this context has no bus")
	return
}

func (inst *NoopBus) Subscribe(subject string, handler MsgHandlerFunc) (unsubscribe func(), err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: this context has no bus")
	return
}

func (inst *NoopBus) Request(ctx context.Context, subject string, payload []byte) (reply []byte, err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: this context has no bus")
	return
}

// NoopStorage is a StorageI that errors on every call: what a context
// without a persist service hands out.
type NoopStorage struct{}

var _ StorageI = (*NoopStorage)(nil)

func (inst *NoopStorage) Get(ctx context.Context, key string) (value []byte, found bool, err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: this context has no storage")
	return
}

func (inst *NoopStorage) Set(ctx context.Context, key string, value []byte) (err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: this context has no storage")
	return
}

func (inst *NoopStorage) Delete(ctx context.Context, key string) (err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: this context has no storage")
	return
}

// StaticMountContext is the MountContextI every host in this module hands
// its apps, and the base a fake embeds: hosts construct one per instance and
// fill it with the Set methods before Mount (ADR-0396 §4). A wrapper that
// changes one capability — a pane's own storage — embeds the MountContextI
// it wraps instead, so it forwards every other method, including ones added
// later.
type StaticMountContext struct {
	id           AppIdT
	logger       zerolog.Logger
	storage      StorageI
	bus          BusI
	ids          *c.WidgetIdStack
	stop         <-chan struct{}
	instanceKey  uint64
	runId        string
	launchConfig []byte
	launchReason LaunchReasonE
	lifetime     *lifetime.Scope
	values       map[any]any
}

var _ MountContextI = (*StaticMountContext)(nil)

// SetMountValue records v under key (MountContextI.MountValue); hosts call
// it before Mount.
func (inst *StaticMountContext) SetMountValue(key any, v any) {
	if inst.values == nil {
		inst.values = make(map[any]any)
	}
	inst.values[key] = v
}

func (inst *StaticMountContext) MountValue(key any) (v any) {
	v = inst.values[key]
	return
}

// SetLifetime records the instance's lifetime scope (ADR-0322, proposed),
// which the host retires at the closing edge and drains before it closes
// the bus. Unset, Lifetime returns nil: the host does not drain.
func (inst *StaticMountContext) SetLifetime(s *lifetime.Scope) {
	inst.lifetime = s
}

func (inst *StaticMountContext) Lifetime() (s *lifetime.Scope) {
	s = inst.lifetime
	return
}

// NewStaticMountContext wires the provided deps. Nil storage / bus are
// replaced with the corresponding Noop type so the context is usable
// immediately and callers don't need to defensively guard against nil.
// The ids stack is left as a fresh empty WidgetIdStack; production hosts
// override it via SetIds with a per-instance stack so widget ids are
// scoped under an instance-unique salt the host pre-pushes before each
// Frame() call. Tests that never render leave the default in place.
func NewStaticMountContext(id AppIdT, logger zerolog.Logger, storage StorageI, bus BusI, stop <-chan struct{}) (inst *StaticMountContext) {
	if storage == nil {
		storage = &NoopStorage{}
	}
	if bus == nil {
		bus = &NoopBus{}
	}
	inst = &StaticMountContext{
		id:      id,
		logger:  logger,
		storage: storage,
		bus:     bus,
		ids:     c.NewWidgetIdStack(),
		stop:    stop,
	}
	return
}

// SetIds replaces the per-instance WidgetIdStack returned by Ids().
// Hosts call this once after construction with a per-window stack they
// own; ownership of the pointer stays with the host, so the host is the
// one that pushes and pops the instance-unique salt around each Frame.
// Passing nil is a no-op (the default fresh stack stays in place).
func (inst *StaticMountContext) SetIds(ids *c.WidgetIdStack) {
	if ids == nil {
		return
	}
	inst.ids = ids
}

func (inst *StaticMountContext) AppId() (id AppIdT) {
	id = inst.id
	return
}

func (inst *StaticMountContext) Log() (logger zerolog.Logger) {
	logger = inst.logger
	return
}

func (inst *StaticMountContext) Storage() (s StorageI) {
	s = inst.storage
	return
}

func (inst *StaticMountContext) Bus() (b BusI) {
	b = inst.bus
	return
}

func (inst *StaticMountContext) Cancel() (ch <-chan struct{}) {
	ch = inst.stop
	return
}

func (inst *StaticMountContext) Ids() (ids *c.WidgetIdStack) {
	ids = inst.ids
	return
}

// SetInstanceKey records the host-minted per-window instance id so
// MountContextI.InstanceKey returns it to the app. Defaults to zero
// when not set (tests and CLI bootstrap).
func (inst *StaticMountContext) SetInstanceKey(key uint64) {
	inst.instanceKey = key
}

// SetRunId records the process-wide run id so MountContextI.RunId
// returns it to the app. Defaults to "" when not set.
func (inst *StaticMountContext) SetRunId(runId string) {
	inst.runId = runId
}

func (inst *StaticMountContext) InstanceKey() (key uint64) {
	key = inst.instanceKey
	return
}

func (inst *StaticMountContext) RunId() (id string) {
	id = inst.runId
	return
}

// SetLaunchConfig records the validated launch-config bytes the host
// delivers through MountContextI.LaunchConfig (ADR-0135 §SD4). Defaults
// to nil when not set — the plain-open shape. Ownership of the slice
// passes to the context; hosts copy caller-supplied bytes before setting.
func (inst *StaticMountContext) SetLaunchConfig(cfg []byte) {
	inst.launchConfig = cfg
}

func (inst *StaticMountContext) LaunchConfig() (cfg []byte) {
	cfg = inst.launchConfig
	return
}

// SetLaunchReason records why the launch config is there (ADR-0148 §SD5):
// a caller supplied it, the host restored a stored workingset, or neither.
// Defaults to LaunchReasonPlain when not set — the shape every context
// without a config has, so tests and CLI bootstrap need no wiring.
func (inst *StaticMountContext) SetLaunchReason(reason LaunchReasonE) {
	inst.launchReason = reason
}

func (inst *StaticMountContext) LaunchReason() (reason LaunchReasonE) {
	reason = inst.launchReason
	return
}

// StaticFrameContext is the FrameContextI every host in this module hands
// its apps, and the base a fake embeds. Its zero capabilities are the
// absent results FrameContextI documents: focused, no close, no gesture.
type StaticFrameContext struct {
	*StaticMountContext
	// windowFocused backs WindowFocused. Constructed true:
	// a host that never says otherwise is a single-surface host, whose
	// sole instance is the active one by definition. A multi-window host
	// (windowhost) overwrites it before every Frame with whether this
	// context's window is the shell's topmost.
	windowFocused bool
	// gesture backs OperationGesture; nil when the host serves no catalog
	// for this window.
	gesture func(op string, args []byte) (result []byte, err error)
	// closeWindow backs CloseWindow; nil when the host has no window of the
	// app's to close.
	closeWindow func(reason string)
}

var _ FrameContextI = (*StaticFrameContext)(nil)

// NewStaticFrameContext wraps a mount context for Frame.
func NewStaticFrameContext(mc *StaticMountContext) (inst *StaticFrameContext) {
	inst = &StaticFrameContext{
		StaticMountContext: mc,
		windowFocused:      true,
	}
	return
}

// SetOperationsGesture installs the host's gesture path for this window
// (ADR-0269 §SD8); nil removes it.
func (inst *StaticFrameContext) SetOperationsGesture(fn func(op string, args []byte) (result []byte, err error)) {
	inst.gesture = fn
}

// OperationGesture applies a command of the app's catalog as the person,
// through the host, so it is logged and bumps revisions like any other
// writer's.
func (inst *StaticFrameContext) OperationGesture(op string, args []byte) (result []byte, err error) {
	if inst.gesture == nil {
		err = RefuseOperation("the host serves no catalog for this window")
		return
	}
	result, err = inst.gesture(op, args)
	return
}

// SetWindowClose installs the host's close for the window carrying this
// context (ADR-0370 §SD9); nil removes it.
func (inst *StaticFrameContext) SetWindowClose(fn func(reason string)) {
	inst.closeWindow = fn
}

// CloseWindow asks the host to close the window; asked is false when the
// host installed no close.
func (inst *StaticFrameContext) CloseWindow(reason string) (asked bool) {
	if inst.closeWindow == nil {
		return
	}
	inst.closeWindow(reason)
	return true
}

// SetWindowFocused records whether the window carrying this context is the
// shell's active window. Multi-window hosts call it before every Frame;
// nothing else should.
func (inst *StaticFrameContext) SetWindowFocused(focused bool) {
	inst.windowFocused = focused
}

func (inst *StaticFrameContext) WindowFocused() (focused bool) {
	focused = inst.windowFocused
	return
}
