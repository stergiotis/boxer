//go:build llm_generated_opus47

package app

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// NoopBus is a BusI that errors on every call. Used by M1 hosts before the
// cap broker arrives in M2. Apps that exercise Bus() in M1 fail loudly with
// a structured error naming the subject.
type NoopBus struct{}

var _ BusI = (*NoopBus)(nil)

func (inst *NoopBus) Publish(subject string, payload []byte) (err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: broker not available in M1 (subject=%s)", subject)
	return
}

func (inst *NoopBus) Subscribe(subject string, handler MsgHandlerFunc) (unsubscribe func(), err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: broker not available in M1 (subject=%s)", subject)
	return
}

func (inst *NoopBus) Request(subject string, payload []byte) (reply []byte, err error) {
	err = eb.Build().Str("subject", subject).Errorf("noopbus: broker not available in M1 (subject=%s)", subject)
	return
}

// NoopStorage is a StorageI that errors on every call. Used by M1 hosts
// before the CH+leeway state layer arrives in M2.
type NoopStorage struct{}

var _ StorageI = (*NoopStorage)(nil)

func (inst *NoopStorage) Get(key string) (value []byte, found bool, err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: storage not available in M1 (key=%s)", key)
	return
}

func (inst *NoopStorage) Set(key string, value []byte) (err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: storage not available in M1 (key=%s)", key)
	return
}

func (inst *NoopStorage) Delete(key string) (err error) {
	err = eb.Build().Str("key", key).Errorf("noopstorage: storage not available in M1 (key=%s)", key)
	return
}

// StaticMountContext is a minimal MountContextI for tests and bootstrap
// scenarios that don't yet have a real bus or storage. Hosts construct one
// per app at Mount.
type StaticMountContext struct {
	id          AppIdT
	logger      zerolog.Logger
	storage     StorageI
	bus         BusI
	ids         *c.WidgetIdStack
	stop        <-chan struct{}
	instanceKey uint64
	runId       string
}

var _ MountContextI = (*StaticMountContext)(nil)

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

// StaticFrameContext extends StaticMountContext with a host-supplied egui
// scope. M1 hosts pass nil; M3 hosts pass the real *egui2.Context. The
// scope is typed as any here so the runtime package does not pull egui2
// bindings transitively — the bridge to the typed scope lands when the
// DockHost ships in M3.
type StaticFrameContext struct {
	*StaticMountContext
	scope any
}

var _ FrameContextI = (*StaticFrameContext)(nil)

// NewStaticFrameContext wraps a MountContext with a host-supplied egui scope.
// scope may be nil in M1; consumers that need it should error at the per-app
// boundary, not panic.
func NewStaticFrameContext(mc *StaticMountContext, scope any) (inst *StaticFrameContext) {
	inst = &StaticFrameContext{
		StaticMountContext: mc,
		scope:              scope,
	}
	return
}

func (inst *StaticFrameContext) EguiScope() (scope any) {
	scope = inst.scope
	return
}
