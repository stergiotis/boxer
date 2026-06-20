package app

import (
	"github.com/rs/zerolog"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// AppI is the runtime's contract for a launchable program. Implementations
// register themselves into the package-level Registry, typically from init().
//
// Lifecycle: Mount once → Frame N times → Unmount once.
//
//   - Mount runs when the host activates the app (dock tile opened, CLI
//     command resolved). It is the place to acquire bus subscriptions and
//     load persisted state.
//   - Frame runs once per frame the app holds focus (or per
//     BackgroundTickHz when unfocused, if non-zero). For SurfaceHeadless
//     apps, Frame is a no-op invoked once before Unmount. **For
//     SurfaceWindowed apps, Frame is invoked inside a runtime-owned
//     window scope (title from Manifest.Title, icon from Manifest.Icon).
//     Apps must NOT call c.Window(...) or c.PanelCentral() themselves.**
//   - Unmount runs when the host releases the app. Cleanup of bus
//     subscriptions and persistence flush happens here.
//
// All three methods may return an error; the host logs and propagates per
// host policy (DockHost surfaces the error in the tile chrome; CliHost
// returns it from main).
type AppI interface {
	Manifest() (m Manifest)
	Mount(ctx MountContextI) (err error)
	Frame(ctx FrameContextI) (err error)
	Unmount(ctx MountContextI) (err error)
}

// MountContextI is the per-lifetime context handed to Mount and Unmount.
// Frame-scoped resources (the egui scope) live on FrameContextI.
type MountContextI interface {
	AppId() (id AppIdT)
	Log() (logger zerolog.Logger)
	Storage() (s StorageI)
	Bus() (b BusI)
	Cancel() (ch <-chan struct{})
	// Ids returns the per-app-instance WidgetIdStack. The host owns the
	// stack pointer for the lifetime of the app instance and pre-pushes
	// an instance-unique salt onto it before every Frame() call, so any
	// widget id the app derives from this stack is unique across all
	// concurrently open instances — even when two apps use the same
	// label string. Apps capture the pointer in Mount, then call
	// PrepareStr/PrepareSeq/IdScope against it during Frame. The stack
	// must NOT be Reset()-ed by the app: the host manages stack lifetime.
	Ids() (ids *c.WidgetIdStack)
	// InstanceKey returns the host-minted per-window instance id. Same
	// value the host writes to factsstore.AppLifecycleRow.TileKey; used
	// by task.ForApp to stamp every spawned task's OwnerTileKey so
	// audit rows join back to the lifecycle row. Zero on hosts that
	// have not allocated an instance key (StaticMountContext defaults,
	// CLI / one-shot bootstrap).
	InstanceKey() (key uint64)
	// RunId returns the process-wide run id (runinfo.RunId) the host
	// has tagged this context with. Same string that lands on
	// AppLifecycleRow.RunId / RuntimeStartRow.RunId / HeartbeatRow.RunId
	// — used by task.ForApp to stamp OwnerRunId so task audit rows
	// join the runtime-start row of the same process. Empty when no
	// runinfo was wired.
	RunId() (id string)
}

// FrameContextI extends MountContextI with frame-scoped resources. The host
// pre-prepares the WidgetIdStack before each Frame() call (ADR-0026 §SD9),
// so apps must not call Prepare() themselves.
type FrameContextI interface {
	MountContextI
	// EguiScope returns the host-provided egui rendering scope. The concrete
	// type is the egui2 Context wrapper introduced by M3 (DockHost); until
	// then, M1 hosts return nil and apps fall back to the legacy global
	// bindings via a LegacyFuncApp wrapper.
	EguiScope() (scope any)
}

// BusI is the cap-broker / inter-app message bus described by ADR-0026 §SD3
// and §SD5. M1 hosts hand back a NoopBus; M2 introduces the in-proc client
// (package runtime/inprocbus); M4 swaps the in-proc transport for a NATS
// connection. The interface is stable across the swap.
type BusI interface {
	Publish(subject string, payload []byte) (err error)
	Subscribe(subject string, handler MsgHandlerFunc) (unsubscribe func(), err error)
	Request(subject string, payload []byte) (reply []byte, err error)
}

// BusProvider mints per-app BusI clients over a concrete transport. The host
// (windowhost) holds one and calls NewBusClient at Open, so the transport —
// the in-proc bus co-located, a NATS connection in deployment (ADR-0026 §SD4,
// ADR-0090 P3) — is a host decision apps never see. inprocbus.Inst and
// natsbus.Provider implement it. caps carries the app's declared
// SubjectFilters: an in-proc client enforces them locally; a NATS client
// treats them as advisory (the server enforces via NKey/JWT).
type BusProvider interface {
	NewBusClient(appId AppIdT, caps []SubjectFilter) (bus BusI, err error)
}

// MsgHandlerFunc is the per-message callback handed to Subscribe. The Msg
// pointer is owned by the bus and must not be retained past the handler
// return.
type MsgHandlerFunc func(msg *Msg)

// Msg is the bus envelope. Subject names the destination; Reply, when
// non-empty, names the inbox a handler should publish a response to (set
// automatically by Request). Sender is the AppId of the publisher, set by
// the bus when the message is dispatched — handlers and reply receivers
// inspect it to know which app spoke. Payload is the body. Handlers that
// wish to reply call bus.Publish(msg.Reply, replyPayload) explicitly — no
// Respond helper, keeping the Msg type a plain value.
type Msg struct {
	Subject string
	Reply   string
	Sender  AppIdT
	Payload []byte
}

// StorageI is the forward declaration of the CH+leeway-backed cold-state
// store described by ADR-0026 §SD6. The implementation lands in M2; M1
// hosts hand back a NoopStorage.
type StorageI interface {
	Get(key string) (value []byte, found bool, err error)
	Set(key string, value []byte) (err error)
	Delete(key string) (err error)
}
