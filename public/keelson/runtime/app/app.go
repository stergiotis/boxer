package app

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
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
//   - Frame runs once per render pass of the host, for every open window,
//     focused or not (Manifest.BackgroundTickHz is read by no host,
//     ADR-0322). A pass happens on input, when an app asks for a repaint,
//     and otherwise on the render loop's idle heartbeat, so work that
//     finishes off the render goroutine lands on a later Frame within that
//     heartbeat at the latest. For SurfaceHeadless
//     apps, Frame is a no-op invoked once before Unmount. **For
//     SurfaceWindowed apps, Frame is invoked inside a runtime-owned
//     window scope (title from Manifest.Title, icon from Manifest.Icon).
//     Apps must NOT call c.Window(...) or c.PanelCentral() themselves.**
//   - Unmount runs when the host releases the app. App-private cleanup and
//     persistence flush happen here. Runtime-mediated effects — bus
//     subscriptions, runtime capability grants, tasks spawned through
//     task.ForApp — are released by the host at the closing edge whether or
//     not Unmount does so (ADR-0188): the host closes MountContextI.Cancel()
//     first, then runs Unmount, then drains the instance's lifetime scope
//     (see [MountContextI.Lifetime]), then closes the instance's bus client. An app may
//     therefore still use its bus inside Unmount, and work it counted into
//     its scope may write its tail until the drain ends; a goroutine that
//     outlives the drain and keeps the client sees a closed-client error
//     rather than a silently live one. Releasing an effect early stays a
//     no-op at close.
//
// Goroutine: Mount, Frame and Unmount run on the host's render goroutine,
// the one running the imzero2 render loop, and every open app shares it —
// each window's Frame runs after the previous one returns (ADR-0261). The
// one exception is a shutdown whose render loop is wedged: Unmount then runs
// from the signal handler while the loop is blocked rather than drawing. Every
// imzero2 call must be made on that goroutine; a call from another one
// interleaves into the frame being written. Work that blocks or takes long
// (queries, file walks, the bus) runs on a goroutine of the app's own or a
// task, and hands its result to a later Frame. The time a Frame takes is
// time every other window waits: the host measures it per window, in
// keelson('frame_times') and the app center. IMZERO2_RENDER_GOROUTINE_CHECK
// turns an off-goroutine call into a panic that names the caller.
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

// PausableAppI is implemented by an app that stops its own background work
// while the person has paused it (ADR-0354 §SD2): its pollers, timers,
// subscriptions — what it stops is its choice. The host calls SetPaused on
// the render goroutine, with true once every window over the instance is
// paused and with false at the first resume. A paused window's Frame is not
// called either way; an app without this only stops drawing.
type PausableAppI interface {
	SetPaused(paused bool)
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
	// Lifetime is the instance's lifetime scope (ADR-0322), which the host
	// retires with Cancel(), before Unmount, and drains after Unmount — off
	// the render goroutine, up to a deadline — before it closes the bus
	// client. Work an app counts into the scope (Enter, Go, a child scope,
	// or a task spawned through task.ForApp) therefore keeps a live bus for
	// its tail; past the deadline the scope is abandoned, tails see their
	// context end, and the bus closes regardless. Tails and drain hooks run
	// beside the app after Unmount has returned: they make no imzero2 call.
	// A goroutine the app starts without counting it in is not waited for.
	//
	// Absent: nil, on a host that does not drain (a test's context, a CLI
	// bootstrap); a child of nil is a root scope the app owns.
	Lifetime() (s *lifetime.Scope)
	// MountValue is the value the host hands every app it mounts under key,
	// keyed as context.Context's values are, so a package this one cannot
	// import — the cell declaration (package cell), the column-width store
	// (package colwidth) — reaches an app as the host's one copy. The
	// package that owns a value owns its key and a typed accessor over this
	// method.
	//
	// Absent: nil.
	MountValue(key any) (v any)
}

// FrameContextI extends MountContextI with what a host offers during a
// Frame. The host pre-prepares the WidgetIdStack before each Frame() call
// (ADR-0026 §SD9), so apps must not call Prepare() themselves.
//
// Every capability is a method with a documented absent result (ADR-0396
// §4), so a wrapper that embeds the interface forwards capabilities added
// later, and a fake embeds *StaticFrameContext.
type FrameContextI interface {
	MountContextI
	// WindowFocused says whether the window this instance draws into is the
	// shell's active one this frame. Process-global input — a keyboard
	// chord drained once from egui's shared queue — is visible to every
	// open instance's Frame alike, so an app acting on such input gates on
	// this or one press fans out into every window hosting that app.
	//
	// Absent: true, on a single-surface host, where the only instance is
	// the active one.
	WindowFocused() (focused bool)
	// CloseWindow asks the host to close the window the app is drawn in
	// (ADR-0370 §SD9). The host closes it at the end of the frame along the
	// path its own close takes — leave, unmount, drain, unload — with
	// reason on the lifecycle row, so an app does nothing after the call
	// that it would not do before any other close. Call it from Frame. It
	// is not a capability subject: the call reaches nothing but the
	// caller's own window, which the person can open again.
	//
	// Absent: asked is false when the host has no window of the app's to
	// close — a single-surface host, a test fake, a pane drawn in another
	// app's window; the app falls back to what it does without it.
	CloseWindow(reason string) (asked bool)
	// OperationGesture applies one of the app's catalog commands as the
	// person, through the host (ADR-0269 §SD8), so it is logged and bumps
	// revisions like any other writer's. Call it from Frame, usually
	// through appops.Gesture.
	//
	// Absent: a refusal (IsOperationRefusal) where the host serves no
	// catalog for the window; the app then applies the change itself.
	OperationGesture(op string, args []byte) (result []byte, err error)
}

// ContextOf is the context an app instance does its I/O under — persist,
// bus requests, store reads — when no call of its own carries one: the
// tail context of its lifetime scope, which outlives the work context's
// Retire and ends when the host abandons the instance at the end of its
// drain. So a save in Unmount, after the instance has left, still runs, and
// nothing outlives the instance. Work that should stop the moment the
// instance leaves uses mc.Lifetime().Ctx() instead.
//
// A mount context without a lifetime scope — a test's, a CLI bootstrap's —
// has no end, and neither has the context returned for it. Call it once,
// in Mount, and keep the result: each call wraps a new context.
func ContextOf(mc MountContextI) (ctx context.Context) {
	if s := mc.Lifetime(); s != nil {
		return s.TailContext()
	}
	return context.Background() //boxer:lint disable=CS020 reason="a mount context without a lifetime scope has no end; this is its root"
}

// BusI is the cap-broker / inter-app message bus described by ADR-0026 §SD3
// and §SD5. M1 hosts hand back a NoopBus; M2 introduces the in-proc client
// (package runtime/inprocbus); M4 swaps the in-proc transport for a NATS
// connection. The interface is stable across the swap.
type BusI interface {
	Publish(subject string, payload []byte) (err error)
	Subscribe(subject string, handler MsgHandlerFunc) (unsubscribe func(), err error)
	// Request publishes payload to subject and waits for one reply, under
	// ctx: the wait ends when ctx does, and ctx's deadline, when it has one,
	// is the wait; a ctx without a deadline waits the transport's default.
	// The handlers of the request see ctx as their message's Context.
	Request(ctx context.Context, subject string, payload []byte) (reply []byte, err error)
}

// RequestWithin is bus.Request bounded by d as well as by ctx, for the
// caller that names its wait instead of the transport.
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
// d <= 0 means "the transport default", so it degrades to Request; a d of
// zero is not a deadline already passed.
func RequestWithin(ctx context.Context, bus BusI, d time.Duration, subject string, payload []byte) (reply []byte, err error) {
	if d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	return bus.Request(ctx, subject, payload)
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

// BusInboxI is the optional private-inbox capability of a bus client
// (ADR-0286 §SD3): a subject unique to this client that the caller can name
// in a request, so a service can publish more than the one reply to it — a
// streamed answer's deltas. It is subscribed without a capability check,
// as a request's reply inbox is: no other client can subscribe to it by
// name. The subscription lives until unsubscribe or the client's Close.
// Optional for the same reason as BusCloserI; a caller whose bus lacks it
// asks for nothing that needs one.
type BusInboxI interface {
	SubscribeInbox(handler MsgHandlerFunc) (inbox string, unsubscribe func(), err error)
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
//
// SenderInstance is the publisher's window (ADR-0191 §SD2): the same
// host-minted key [MountContextI.InstanceKey] returns, stamped by the bus
// from the client the host opened for that window. It is what lets a
// service attribute a request to a window rather than to an app — with two
// windows of one app open, Sender alone cannot. Zero means unattributed: a
// service's own client, a CLI bootstrap, or a transport that does not carry
// it (natsbus sets no Sender either).
//
// It is deliberately not part of any request payload. The bus stamps it, so
// a caller cannot claim to be a window it is not — the same reason ADR-0135
// attributes a launch's caller from the envelope rather than the request.
//
// RequesterGoroutine is, on the in-process bus, the id of the goroutine a
// Request was made on. The handlers of a request run on a goroutine of the
// request's own (ADR-0386), so a handler that must refuse a caller's
// goroutine — the render goroutine, which a handler waiting on the next
// frame would block — reads it here. Zero for a publish, whose handlers run
// on the publisher's goroutine, and on transports that do not carry it.
//
// The message's context, read through Context, is what a handler does the
// message's I/O under.
type Msg struct {
	Subject            string
	Reply              string
	Sender             AppIdT
	SenderInstance     uint64
	Payload            []byte
	RequesterGoroutine uint64
	ctx                context.Context
}

// Context is the context a handler does this message's I/O under. For a
// request on the in-process bus it is the requester's ctx bounded by the
// request's wait, and it ends when the requester stops waiting — on the
// reply, at the deadline, or when the requester's ctx ends — so a handler's
// round trip does not outlive the caller it answers. It carries the
// requester's values, the call identity among them. Work a handler goes on
// with after it has replied, and work that must land whether or not the
// requester still waits (an audit row), runs under context.WithoutCancel of
// it or under the handler's own lifetime ctx.
//
// A publish, and a message from a transport that carries no requester,
// has no context of its own: Context then returns a context that never
// ends, as net/http's Request.Context does for a request built without one.
func (inst *Msg) Context() (ctx context.Context) {
	if inst.ctx == nil {
		return context.Background() //boxer:lint disable=CS020 reason="the root for a message no requester waits on, as http.Request.Context"
	}
	return inst.ctx
}

// WithContext returns a shallow copy of msg whose Context is ctx. A bus
// implementation uses it to hand a request's context to its handlers.
func (inst *Msg) WithContext(ctx context.Context) (out *Msg) {
	cp := *inst
	cp.ctx = ctx
	return &cp
}

// StorageI is the forward declaration of the CH+leeway-backed cold-state
// store described by ADR-0026 §SD6. The implementation lands in M2; M1
// hosts hand back a NoopStorage.
//
// Every call is a round trip and takes the ctx it runs under; an app with
// none of its own passes ContextOf(mountCtx).
type StorageI interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte) (err error)
	Delete(ctx context.Context, key string) (err error)
}
