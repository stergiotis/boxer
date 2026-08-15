package app

import (
	"time"

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
//   - Unmount runs when the host releases the app. App-private cleanup and
//     persistence flush happen here. Runtime-mediated effects — bus
//     subscriptions, runtime capability grants, tasks spawned through
//     task.ForApp — are released by the host at the closing edge whether or
//     not Unmount does so (ADR-0188): the host closes MountContextI.Cancel()
//     first, then runs Unmount, then closes the instance's bus client. An
//     app may therefore still use its bus inside Unmount; a goroutine that
//     outlives Unmount and keeps the client sees a closed-client error rather
//     than a silently live one. Releasing an effect early stays a no-op at
//     close.
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
	// LaunchConfig returns the raw facts-CBOR launch-config bytes the
	// window carrying this context was opened with (ADR-0135 §SD4);
	// nil when the window was opened plainly. The host has already
	// validated the size cap and that the bytes claim the manifest's
	// LaunchKind, but has not decoded them — the app decodes with its
	// generated Unmarshal in Mount, and a decode failure belongs in
	// Mount's error return (it surfaces as the host's failed-mount
	// label), never in a silent fallback. Frozen per window: post-mount
	// parameter changes stay per-app ops.
	LaunchConfig() (cfg []byte)
	// LaunchReason says why LaunchConfig holds what it holds (ADR-0148
	// §SD5): a caller asked for this window and supplied the config, the
	// host restored the app's own stored workingset onto an otherwise
	// plain open, or nothing was delivered at all. Adopters read it to
	// place their environment overrides between the two config tiers —
	// caller config > env override > restored config > default — and to
	// apply restored optional fields by different rules than caller ones
	// (an empty field in a restored record is a value the user arrived
	// at, not an omission). LaunchReasonPlain whenever LaunchConfig is
	// nil.
	LaunchReason() (reason LaunchReasonE)
}

// FrameContextI extends MountContextI with frame-scoped resources. The host
// pre-prepares the WidgetIdStack before each Frame() call (ADR-0026 §SD9),
// so apps must not call Prepare() themselves.
//
// Hosts that can tell which of their windows is active additionally
// implement [WindowFocusI] on the same context — an optional capability,
// not part of this interface, so single-surface hosts and test fakes owe
// nothing.
type FrameContextI interface {
	MountContextI
	// EguiScope returns the host-provided egui rendering scope. The concrete
	// type is the egui2 Context wrapper introduced by M3 (DockHost); until
	// then, M1 hosts return nil and apps fall back to the legacy global
	// bindings via a LegacyFuncApp wrapper.
	EguiScope() (scope any)
}

// WindowFocusI is the optional frame-context capability a multi-window
// host provides so an app instance can tell whether ITS window is the
// shell's active one this frame. Process-global input — a keyboard chord
// drained once from egui's shared queue — is visible to every open
// instance's Frame alike, so an app acting on such input must gate on
// this or one press fans out into every window hosting that app.
//
// Type-assert from the FrameContextI; a context without it belongs to a
// single-surface host, where the only instance is the active one — treat
// absence as focused:
//
//	focused := true
//	if f, ok := ctx.(app.WindowFocusI); ok {
//		focused = f.WindowFocused()
//	}
type WindowFocusI interface {
	WindowFocused() (focused bool)
}

// BusI is the cap-broker / inter-app message bus described by ADR-0026 §SD3
// and §SD5. M1 hosts hand back a NoopBus; M2 introduces the in-proc client
// (package runtime/inprocbus); M4 swaps the in-proc transport for a NATS
// connection. The interface is stable across the swap.
type BusI interface {
	Publish(subject string, payload []byte) (err error)
	Subscribe(subject string, handler MsgHandlerFunc) (unsubscribe func(), err error)
	Request(subject string, payload []byte) (reply []byte, err error)
	// RequestWithTimeout is Request with the caller naming the wait instead
	// of the transport.
	//
	// It exists because one class of request is bounded by a HUMAN rather
	// than by a service: an fs.dialog.{read,write} is answered when someone
	// finishes choosing in a file picker, which is not a five-second
	// operation. Under the transport default those flows fail while the
	// picker is still open, and the app is told "timeout" for a dialog the
	// user is looking at (found by driving mdedit's Save; the same latency
	// bound applies to every fsbroker dialog consumer).
	//
	// The alternative — raising the transport's default — makes every
	// request in the process wait as long as the slowest human, which is the
	// wrong trade for the many requests answered by a service in
	// milliseconds. A caller that knows what it is waiting for is the only
	// one positioned to say.
	//
	// d <= 0 means "use the transport default", so it degrades to Request.
	RequestWithTimeout(subject string, payload []byte, d time.Duration) (reply []byte, err error)
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

// BusCloserI is the optional close capability of a per-instance bus client
// (ADR-0188 §SD1). A host type-asserts it on the BusI it minted at Open and
// calls Close once the instance's Unmount has run: the in-proc client drops
// every subscription it created and leaves the router's live registry
// (releasing runtime grants with it); the NATS client closes its
// connection, which has the same meaning. NoopBus does not implement it, so
// hosts without a transport skip the step. Optional rather than a BusI
// method so that BusI stays exactly the surface an app programs against.
type BusCloserI interface {
	Close() (err error)
}

// BusInstanceI is the optional per-instance identity capability of a bus
// client (ADR-0188 §SD1): the host stamps the window or embed key it minted
// the client for, so subscriptions can be attributed to one instance where
// several windows share an app id. Optional for the same reason as
// BusCloserI.
type BusInstanceI interface {
	SetInstanceKey(key uint64)
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
