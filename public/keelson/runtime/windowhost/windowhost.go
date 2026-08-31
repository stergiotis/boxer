package windowhost

import (
	"slices"
	"strconv"
	"sync"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
)

// DebugRender, when set to a non-empty value, logs every window-body
// invocation so we can confirm which windows the user actually saw
// painted. Off by default; enable via WINDOWHOST_DEBUG_RENDER=1 for an
// investigative session.
var DebugRender = env.NewString(env.Spec{
	Name:        "WINDOWHOST_DEBUG_RENDER",
	Description: "non-empty enables per-window-body render logging in the windowhost",
	Category:    env.CategoryDev,
})

// WindowKeyT identifies one open window. Stable for the lifetime of
// the window; never reused. Encoded as a uint64 because egui's
// per-widget Memory state keys are u64-hashes — keeping the key
// itself a uint64 means the widget id derived for the window scope
// is stable across frames, so position/size/collapsed state persists
// for as long as the window stays open.
type WindowKeyT uint64

// window holds per-open-window state. One per active Open call.
type window struct {
	key      WindowKeyT
	manifest app.Manifest
	appInst  app.AppI
	mountCtx *app.StaticMountContext
	frameCtx *app.StaticFrameContext
	// frameCtxApp is what Frame() actually receives: frameCtx itself, or a
	// wrapper adding the column-width capability when this host has a facts
	// store. frameCtx is kept alongside because the host calls its setters
	// (focus, egui scope) every frame; the wrapper embeds it, so those
	// still land.
	frameCtxApp app.FrameContextI
	// mount is the per-AppI-instance shared Mount/Unmount lifecycle state.
	// Singleton-registered apps return the same AppI for every Open, so two
	// windows over one instance must share one Mount (on first Frame) and one
	// Unmount (when the last window closes) — otherwise the instance sees
	// double Mount/Unmount, double-acquiring and double-releasing resources.
	// Factory-registered apps get a distinct instance (and thus a distinct
	// instMount with refs==1) per window.
	mount      *instMount
	closeReq   bool // set by the in-body Close button or external Close()
	stopReason string

	// stop is the channel behind this window's MountContextI.Cancel()
	// (ADR-0188 §SD2). The host closes it at the closing edge BEFORE the
	// app's Unmount — the "leave" step — so tasks spawned through
	// task.ForApp cascade-cancel and goroutines watching Cancel() wind
	// down while the app is still mounted. stopOnce keeps a shared
	// singleton's carried window from closing it twice.
	stop     chan struct{}
	stopOnce sync.Once

	// appIds is the per-window WidgetIdStack handed to the app via
	// MountCtx.Ids(). The host pre-pushes an instance-unique salt
	// derived from `key` onto this stack at the start of every Frame
	// pass via c.IdScope, so any widget id the app derives is scoped
	// under that salt and cannot collide with ids from another open
	// app whose own instance counter happens to land on the same
	// value. The stack persists across frames; the IdScope wrapper
	// pops the salt at the end of the pass, so the stack is empty
	// between frames.
	appIds *c.WidgetIdStack

	// openFlag is the Go-side companion to egui::Window's
	// `.open(&mut bool)` close affordance. The Rust interpreter mirrors
	// this bool into its window_open_bindings HashMap (keyed by
	// openBindingId, derived deterministically from window.key) and
	// passes &mut at .show() time. When the user clicks the title-bar
	// X, egui flips it to false; the Rust apply code pushes the
	// transition to r10, which StateManager.Sync writes back here via
	// the r10 databinding registered before each c.Window emit. The
	// Frame loop reads openFlag at the end of each pass and triggers
	// Close(key, "user-close") on the false transition.
	openFlag bool

	// focusHandle is the c.Window block handle of this window's last
	// emit. Frame reads WINDOW_TOPMOST off it — one frame late, like
	// every r7 signal — to decide the shell's active window; the zero
	// handle of a window that has not rendered yet resolves to empty
	// flags. Render-thread only.
	focusHandle widgethandle.WidgetHandle
}

// instMount is the Mount/Unmount lifecycle shared by every window pointing
// at one AppI instance. refs is the number of open windows referencing the
// instance; Mount runs when the first window first Frames (capturing the
// mountCtx it used so the matching Unmount uses the same context), and
// Unmount runs when refs drops to zero. Factory-registered apps yield a
// fresh AppI per Open, so each gets its own instMount with refs==1 — the
// refcount only collapses for singleton-registered apps shown in >1 window.
type instMount struct {
	refs     int
	mounted  bool
	mountErr error // sticky; window body renders an error label when set
	mountCtx *app.StaticMountContext
	// carried holds windows that closed while the instance stayed
	// mounted through another window AND whose mount context is the one
	// the instance was Mounted with (singleton shown twice, the first
	// window closing first). Their stop channel and bus client are what
	// the app captured in Mount, so they are released at the LAST
	// release together with the final window's, not at their own reap
	// (ADR-0188 §SD1/§SD2).
	carried []*window
}

// Inst is the window host: the registry plus the list of open windows.
// The zero value is unusable; construct via NewInst.
//
// Goroutine safety: split. Open / OpenWithConfig / Close / CloseAll and
// the metadata snapshots guard every touch of host state with inst.mu
// and may be called off the render thread — the open service (ADR-0135)
// invokes OpenWithConfig from bus-handler goroutines, and a window
// opened that way is picked up by the next Frame. App factory ctors run
// on the calling goroutine and must not touch render state (the
// lifecycle contract already reserves rendering for Frame). The render
// surfaces themselves (Frame, RenderAppsMenu, the picker/search state)
// remain single-threaded: render-loop only.
type Inst struct {
	registry *app.Registry
	logger   zerolog.Logger

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at NewInst.
	density styletokens.DensityE

	// runId + facts are the audit trail wiring. When facts is non-nil,
	// Open writes a "started" app-lifecycle row and reapClosed writes
	// a "stopped" row carrying the supplied runId. Both are best-
	// effort — write errors are logged but never block host activity.
	runId string
	facts factsstore.FactsStoreI

	// busProvider mints per-app BusI clients over the host's chosen
	// transport (ADR-0026 §SD3/§SD5/§SD4). When non-nil, Open mints a
	// per-app client carrying the app's Manifest.Caps and passes it through
	// MountCtx.Bus(); inprocbus.Inst provides it co-located, natsbus.Provider
	// in a NATS deployment. When nil, MountCtx.Bus() falls back to NoopBus —
	// the M1 shape every app was bootstrapped against.
	busProvider app.BusProvider

	mu      sync.Mutex
	nextKey uint64
	windows []*window

	// pendingRaise queues one window to be raised to the top of egui's
	// stacking on the next Frame — OpenOrRaise's "focus the existing
	// window" half. Written under mu (OpenOrRaise may be called off the
	// render thread like Open); consumed and cleared by Frame on the
	// render thread, where the window's block handle is at hand. A key
	// whose window closed in between matches nothing and the raise is
	// dropped, which is the right answer. Zero = nothing pending.
	pendingRaise WindowKeyT

	// mountState shares Mount/Unmount lifecycle across windows that point at
	// the same AppI instance (singleton-registered apps). Keyed by the AppI
	// interface value; an entry is created on Open and removed when its last
	// window is reaped.
	mountState map[app.AppI]*instMount

	// warnedNoComposer remembers which app ids have already been reported
	// as declaring Manifest.Workingset without implementing
	// app.WorkingsetComposerI (ADR-0148 §SD4) — the check can only run at
	// the first save attempt, and the mismatch is worth saying once, not
	// once per window close.
	warnedNoComposer map[app.AppIdT]struct{}

	// Per-window "Save as SVG" affordance (M2 of the per-window SVG
	// export plan). One singleton picker for all windows — when a
	// window's Save button is clicked, pendingExportKey records which
	// window the picker is collecting a path for; the picker render
	// runs once per Frame at the top level and, on commit, calls
	// `c.ExportSvgWindow(handle, path, true)` against the recorded
	// key. Re-preparing the window id at export time is mandatory
	// because the in-loop `c.Window(...)` call already consumed the
	// original handle this frame.
	pendingExportKey WindowKeyT
	fpSaveSvg        *filepicker.Inst

	// activeKey names the shell's active window — the one whose frame
	// context reads focused (app.WindowFocusI), and therefore the one
	// instance process-global input like the Ctrl+Enter chord belongs
	// to. Derived each Frame by pickActiveWindow from the windows' r7
	// WINDOW_TOPMOST reports; zero while no window is open. Mutated
	// only inside Frame: render-thread only, like searchText below.
	activeKey WindowKeyT

	// launcher renders every launcher surface: the empty-state pane and the
	// Apps ▾ menu (ADR-0214 §SD2). The query, the facet filters and the
	// selection live on it rather than here — they used to be host fields
	// shared by two render paths that had drifted into unequal abilities, and
	// moving them behind one component is what makes that drift unexpressible.
	//
	// nil is tolerated: a host constructed without one renders an empty-state
	// pane that says so, which is what the screenshot-tour path and the
	// windowhost's own tests get.
	launcher launcherI
}

// NewInst constructs a WindowHost backed by registry. logger is used
// for per-window mount/frame errors; per-app loggers (with app_id pre-
// tagged) are derived from it at Open time.
//
// Audit-trail wiring (optional but recommended for production use):
// call SetAudit(runId, facts) after construction; once set, Open and
// reapClosed emit app-lifecycle rows so the persistence layer carries
// a per-window open/close trail correlated with the runtime-start row
// that runId points at.
func NewInst(registry *app.Registry, logger zerolog.Logger) (inst *Inst) {
	inst = &Inst{
		registry:   registry,
		logger:     logger,
		density:    styletokens.ActiveDensity(),
		mountState: make(map[app.AppI]*instMount),
		fpSaveSvg: filepicker.New("windowhost-save-svg", filepicker.ModeSave,
			filepicker.WithExtensionFilter(".svg"),
			filepicker.WithDefaultFilename("window.svg"),
			filepicker.WithStartAtOsHome()),
	}
	return
}

// SetBus attaches a bus provider to the window host. Once set, each Open
// mints a per-app BusI client (gated on the app's Manifest.Caps) and threads
// it through MountCtx.Bus() so apps can publish/subscribe/request (ADR-0026
// §SD3/§SD5). The provider chooses the transport: inprocbus.Inst co-located,
// natsbus.Provider in a NATS deployment (§SD4) — apps never see it. Passing
// nil clears the wiring (subsequent Opens hand out NoopBus). Calling SetBus
// after windows have been opened only affects subsequent Opens — already-
// mounted windows keep the bus they were given.
func (inst *Inst) SetBus(provider app.BusProvider) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.busProvider = provider
}

// SetAudit attaches a runId + FactsStoreI to the window host. Once
// set, every Open emits an "app-lifecycle started" row and every
// reapClosed emits a "stopped" row carrying the supplied StopReason.
// Both writes are best-effort; a failure to persist is logged at warn
// level but never bubbles up to the caller.
//
// Calling SetAudit after windows have been opened is supported but
// won't retroactively emit started rows for windows that are already
// open — audit is forward-only from the point of attachment.
func (inst *Inst) SetAudit(runId string, facts factsstore.FactsStoreI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.runId = runId
	inst.facts = facts
}

// Open allocates a new window for the given AppId. Returns the fresh
// key on success; an error if the registry doesn't know the Id or the
// ctor fails. The window is mounted lazily on first Frame; if Mount
// fails the window stays open with an error label so the user can
// Close it.
//
// For an app that declares Manifest.Workingset this is also the restore
// path: OpenWithConfig looks up the app's stored record and, when one is
// usable, opens the window carrying it (ADR-0148 §SD5).
func (inst *Inst) Open(appId app.AppIdT) (key WindowKeyT, err error) {
	key, err = inst.OpenWithConfig(appId, "", nil)
	return
}

// OpenOrRaise opens appId — unless a window over that app is already
// open, in which case that window is queued to be raised instead of a
// second one stacking. This is the affordance a recurring global
// shortcut wants (F1 → help): the first press opens, every further
// press brings the same window back to the front.
//
// The raise runs on the next Frame, and egui's stacking then reports
// the window topmost — so it also becomes the shell's active window
// (app.WindowFocusI), exactly as a fresh open would be: both halves
// end with the window on top and focused. The oldest window wins when
// several show the app. opened reports which half ran.
func (inst *Inst) OpenOrRaise(appId app.AppIdT) (key WindowKeyT, opened bool, err error) {
	inst.mu.Lock()
	for _, w := range inst.windows {
		if w.manifest.Id == appId && !w.closeReq {
			inst.pendingRaise = w.key
			key = w.key
			inst.mu.Unlock()
			return
		}
	}
	inst.mu.Unlock()
	opened = true
	key, err = inst.Open(appId)
	return
}

// maxLaunchConfigBytes caps the launch-config payload accepted at the
// host boundary (ADR-0135 §SD6 — the request is persisted as an audit
// fact, so the cap also bounds the row). Enforced before any decode.
const maxLaunchConfigBytes = 64 << 10

// OpenWithConfig allocates a new window for appId carrying a launch
// config (ADR-0135): kind names the config's vocabulary kind and cfg is
// the config DTO's facts-CBOR bytes, delivered untouched to the app via
// MountContextI.LaunchConfig at Mount (§SD4). Empty kind + nil cfg is a
// plain open, exactly Open's behaviour.
//
// Boundary validation, in order: target manifest exists → the manifest's
// LaunchKind accepts kind (an argument-carrying open of an app with an
// empty LaunchKind is refused, §SD3) → size cap → the bytes decode as
// the claimed kind (kindcheck). Refusals are returned as named errors —
// the bus-facing open service turns them into LaunchReply refusals,
// never silent drops (§SD1).
//
// A plain open of a workingset participant (ADR-0148 §SD5) is where the
// host supplies a config of its own: the stored record for the app is
// looked up and, when it survives the same boundary rules, the open
// proceeds as a config-carrying one with LaunchReasonRestore. Restore
// therefore has no second delivery channel — the app decodes a launch
// config in Mount either way, and reads MountContextI.LaunchReason to
// tell the two tiers apart.
func (inst *Inst) OpenWithConfig(appId app.AppIdT, kind string, cfg []byte) (key WindowKeyT, err error) {
	if len(cfg) == 0 {
		// The wire cannot distinguish nil from empty; normalise so a
		// plain open always delivers nil through LaunchConfig (§SD4).
		cfg = nil
	}
	m, ok := inst.registry.LookupManifest(appId)
	if !ok {
		err = eb.Build().Str("id", string(appId)).Errorf("windowhost: app not registered id=%s", string(appId))
		return
	}
	if kind == "" && len(cfg) > 0 {
		err = eb.Build().Str("id", string(appId)).Int("len", len(cfg)).
			Errorf("windowhost: launch config bytes without a config kind")
		return
	}
	reason := app.LaunchReasonPlain
	if kind != "" {
		reason = app.LaunchReasonCaller
	} else if restored := inst.restoreWorkingset(m); len(restored) > 0 {
		// From here the open is indistinguishable from a config-carrying
		// one — same validation ladder, same singleton refusal, same
		// delivery at Mount. Only the reason on the mount context, and
		// the caller on the audit row, say where the bytes came from.
		kind = m.LaunchKind
		cfg = restored
		reason = app.LaunchReasonRestore
	}
	if kind != "" {
		if len(cfg) == 0 {
			err = eb.Build().Str("id", string(appId)).Str("kind", kind).
				Errorf("windowhost: config kind claimed but config is empty")
			return
		}
		if m.LaunchKind == "" {
			err = eb.Build().Str("id", string(appId)).Str("kind", kind).
				Errorf("windowhost: app accepts no launch config (manifest LaunchKind is empty)")
			return
		}
		if m.LaunchKind != kind {
			err = eb.Build().Str("id", string(appId)).Str("kind", kind).Str("launchKind", m.LaunchKind).
				Errorf("windowhost: config kind does not match the app's LaunchKind")
			return
		}
		if len(cfg) > maxLaunchConfigBytes {
			err = eb.Build().Str("id", string(appId)).Int("len", len(cfg)).Int("max", maxLaunchConfigBytes).
				Errorf("windowhost: launch config exceeds the size cap")
			return
		}
		if cErr := kindcheck.Check(kind, cfg); cErr != nil {
			err = eb.Build().Str("id", string(appId)).Str("kind", kind).
				Errorf("windowhost: launch config bytes refused: %w", cErr)
			return
		}
		// The config crosses into host-owned state at Mount; copy so a
		// caller recycling its buffer can't mutate the delivered bytes.
		cfg = append([]byte(nil), cfg...)
	}
	a, err := inst.registry.Open(appId)
	if err != nil {
		err = eh.Errorf("windowhost: open: %w", err)
		return
	}
	inst.mu.Lock()
	// Config is delivered at Mount and Mount runs once per AppI instance;
	// a singleton-registered app that already has a window can never
	// consume a fresh config, so refuse rather than drop it silently.
	if kind != "" {
		if ms := inst.mountState[a]; ms != nil && ms.refs > 0 {
			inst.mu.Unlock()
			// A restore is a config delivery too (ADR-0148 §SD5), so it
			// meets the same refusal — which is how the requirement that
			// participants be factory-registered gets enforced. The reason
			// rides the structured data so the two cases are tellable
			// apart in the log.
			err = eb.Build().Str("id", string(appId)).Str("kind", kind).Str("reason", reason.String()).
				Errorf("windowhost: app instance is already open (singleton registration); launch config would never be delivered")
			return
		}
	}
	inst.nextKey++
	key = WindowKeyT(inst.nextKey)
	// Mint a per-app bus client when a provider is attached. The provider
	// chooses the transport (inprocbus co-located, natsbus in deployment);
	// the per-app caps below are baked in at construction. When no provider
	// is set, busC stays nil and falls through to NoopBus inside
	// NewStaticMountContext.
	var busC app.BusI
	var storageC app.StorageI
	if inst.busProvider != nil {
		// Compute the full cap set before minting: manifest caps plus the
		// host-injected persist cap for apps declaring PersistedKeys, so the
		// transport-agnostic provider needs no post-hoc AddCap.
		// Copy the manifest caps before minting so a later AddCap on the client
		// (capbroker grants) can never alias-mutate the manifest's slice.
		caps := append([]app.SubjectFilter(nil), m.Caps...)
		if len(m.PersistedKeys) > 0 {
			caps = append(caps, app.SubjectFilter{
				Pattern:   persist.SubjectPrefix + m.Id.SubjectAlias() + ".>",
				Direction: app.CapDirectionPub,
				Reason:    "host-injected for declared PersistedKeys",
			})
		}
		client, busErr := inst.busProvider.NewBusClient(m.Id, caps)
		if busErr != nil {
			inst.logger.Warn().Err(busErr).Str("appId", string(m.Id)).
				Msg("windowhost: bus client construction failed; using NoopBus")
		} else {
			busC = client
			// Per-instance attribution for the live subscription table
			// (ADR-0188 §SD1); optional capability, so a transport that
			// does not carry it is simply unattributed.
			if bi, ok := client.(app.BusInstanceI); ok {
				bi.SetInstanceKey(uint64(key))
			}
			// Storage client wraps the same bus client so MountCtx.Storage()
			// shares the per-app permission set with MountCtx.Bus().
			sc, sErr := persist.NewClient(busC, m.Id)
			if sErr != nil {
				inst.logger.Warn().Err(sErr).Str("appId", string(m.Id)).
					Msg("windowhost: persist client construction failed; using NoopStorage")
			} else {
				storageC = sc
			}
		}
	}
	// Per-window logger: app_id + instance_id pre-tagged so any zerolog
	// event the app emits surfaces with the full identity tuple
	// (run_id is added by runinfo.TagLogger on the carousel's global
	// logger; AppLogger adds app_id; we add instance_id here).
	perWindowLogger := app.AppLogger(inst.logger, m.Id).
		With().Uint64("instance_id", uint64(key)).Logger()
	// A real stop channel per window (ADR-0188 §SD2): Cancel() fires at
	// the closing edge, before Unmount. Before this the host passed nil,
	// which selects never, so task.ForApp's cascade-cancel was inert.
	stop := make(chan struct{})
	mountCtx := app.NewStaticMountContext(m.Id, perWindowLogger, storageC, busC, stop)
	mountCtx.SetInstanceKey(uint64(key))
	mountCtx.SetRunId(inst.runId)
	mountCtx.SetLaunchConfig(cfg)
	mountCtx.SetLaunchReason(reason)
	appIds := c.NewWidgetIdStack()
	mountCtx.SetIds(appIds)
	frameCtx := app.NewStaticFrameContext(mountCtx, nil)
	// Column-width capability (ADR-0151 M4) in the ADR-0155 §SD1 shape: an
	// optional capability on the frame context, not a contract method. Only
	// wrapped when a facts store exists, because absence of the capability
	// is how an app learns there is nowhere durable to put widths — wrapping
	// unconditionally and handing back nil would move that check to every
	// call site instead.
	var frameCtxApp app.FrameContextI = frameCtx
	if inst.facts != nil {
		frameCtxApp = &frameCtxColWidth{StaticFrameContext: frameCtx, store: inst.facts}
	}
	// Share Mount/Unmount state per AppI instance: a second window over a
	// singleton-registered app reuses the existing instMount (refs++), so the
	// instance is Mounted once and Unmounted only when its last window closes.
	ms := inst.mountState[a]
	if ms == nil {
		ms = &instMount{}
		inst.mountState[a] = ms
	}
	ms.refs++
	inst.windows = append(inst.windows, &window{
		key:         key,
		manifest:    m,
		appInst:     a,
		mountCtx:    mountCtx,
		frameCtx:    frameCtx,
		frameCtxApp: frameCtxApp,
		appIds:      appIds,
		mount:       ms,
		openFlag:    true,
		stop:        stop,
	})
	runId := inst.runId
	facts := inst.facts
	inst.mu.Unlock()

	if facts != nil {
		_, wErr := facts.WriteAppLifecycle(factsstore.AppLifecycleRow{
			RunId:   runId,
			AppId:   m.Id,
			TileKey: uint64(key),
			Phase:   factsstore.AppLifecyclePhaseStarted,
		})
		if wErr != nil {
			inst.logger.Warn().Err(wErr).
				Str("id", string(m.Id)).
				Uint64("windowKey", uint64(key)).
				Msg("windowhost: write app-lifecycle started failed")
		}
		if reason == app.LaunchReasonRestore {
			// Audit the restore as what it is — a launch nobody asked for
			// by name (ADR-0148 §SD6). Caller-delivered opens are attributed
			// by the open service from the bus envelope; this one has no
			// envelope, so the synthetic id stands in. A plain open that
			// arrived over the bus and then restored writes both rows — the
			// caller's plain request and this restore — which is the honest
			// record of what happened. Best-effort, like every audit write.
			_, lErr := facts.WriteLaunch(factsstore.LaunchRow{
				RunId:       runId,
				CallerAppId: WorkingsetCallerAppId,
				TargetAppId: m.Id,
				TileKey:     uint64(key),
				ConfigKind:  kind,
				Config:      cfg,
			})
			if lErr != nil {
				inst.logger.Warn().Err(lErr).
					Str("id", string(m.Id)).
					Uint64("windowKey", uint64(key)).
					Msg("windowhost: write restored-launch fact failed")
			}
		}
	}
	return
}

// CloseAll marks every open window for reaping with the supplied
// StopReason. Used by the carousel on shutdown to leave a clean audit
// trail — without it, windows still mounted at process exit would
// have "started" rows in the facts table but no matching "stopped"
// rows. Call Frame once after CloseAll to drive reap; or call ReapAll
// for out-of-render-loop teardown.
func (inst *Inst) CloseAll(reason string) {
	inst.mu.Lock()
	for _, w := range inst.windows {
		w.closeReq = true
		w.stopReason = reason
	}
	inst.mu.Unlock()
}

// ReapAll runs Unmount and writes "stopped" lifecycle rows for every
// currently-open window, then empties the slice. Unlike reapClosed
// (which fires after the render pass), this is the shutdown path —
// call it from a defer in the carousel main after the render loop
// has exited so closing-window audit rows still get written.
func (inst *Inst) ReapAll(reason string) {
	inst.mu.Lock()
	wins := inst.windows
	inst.windows = nil
	runId := inst.runId
	facts := inst.facts
	inst.mu.Unlock()

	for _, w := range wins {
		inst.reapWindow(w, reason, "windowhost: unmount on shutdown error")
		if facts != nil {
			emitStopped(facts, inst.logger, runId, w, reason)
		}
	}
}

// releaseMount decrements the shared per-AppI-instance mount refcount for w
// and returns the context to Unmount with when w was the last window
// referencing that instance (nil otherwise — another window still holds it,
// or the instance was never mounted). Removes the mountState entry on last
// release. Takes the host lock; the returned Unmount runs outside it so an
// Unmount that re-enters the host cannot deadlock.
//
// stillShared distinguishes the two nil cases: true means another window
// kept the instance alive (only possible for a singleton-registered app
// shown more than once), false with a nil context means the instance was
// never mounted. The workingset save (ADR-0148) needs the distinction —
// there is nothing to save from an unmounted instance, and nothing this
// window may save from a shared one.
func (inst *Inst) releaseMount(w *window) (unmountCtx *app.StaticMountContext, stillShared bool, carried []*window) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ms := w.mount
	if ms == nil {
		return
	}
	ms.refs--
	if ms.refs > 0 {
		stillShared = true
		if ms.mounted && ms.mountCtx == w.mountCtx {
			// The instance was Mounted with THIS window's context; its
			// stop channel and bus client are what the app holds. Carry
			// them to the last release (ADR-0188 §SD1/§SD2).
			ms.carried = append(ms.carried, w)
			carried = ms.carried
		}
		return
	}
	delete(inst.mountState, w.appInst)
	carried = ms.carried
	if ms.mounted {
		unmountCtx = ms.mountCtx
	}
	return
}

// leave closes a window's stop channel — the first step of the closing
// edge (ADR-0188 §SD2). Idempotent.
func (w *window) leave() {
	w.stopOnce.Do(func() {
		if w.stop != nil {
			close(w.stop)
		}
	})
}

// unload closes a window's bus client — the last step of the closing edge
// (ADR-0188 §SD1): every subscription the instance created and every
// runtime grant it received are released with it. Hosts without a
// transport hold a NoopBus, which has no closer, and skip. Idempotent
// through the client.
func (w *window) unload(logger zerolog.Logger) {
	bc, ok := w.mountCtx.Bus().(app.BusCloserI)
	if !ok {
		return
	}
	if cErr := bc.Close(); cErr != nil {
		logger.Warn().Err(cErr).
			Str("id", string(w.manifest.Id)).
			Uint64("windowKey", uint64(w.key)).
			Msg("windowhost: bus client close")
	}
}

// reapWindow runs one reaped window's closing-edge work, outside the host
// lock so an Unmount that re-enters the host cannot deadlock: pull the
// workingset (ADR-0148 §SD4 — before Unmount, which tears the app down),
// then Unmount. reason is the stop reason the caller settled on; it also
// lands on the workingset row as save provenance. unmountMsg names the
// caller's context in the failure log (reap vs shutdown).
//
// The closing edge runs in a fixed order (ADR-0188 §SD2, documented on
// AppI): leave — close the stop channel, so Cancel() fires and tasks
// cascade-cancel while the app is still mounted; unmount — the workingset
// pull and the app's own Unmount, with its bus still usable; unload — close
// the bus client, releasing every subscription and runtime grant the
// instance still held. A window that closes while its singleton instance
// stays mounted through another window releases nothing the app may still
// be holding: if it is the mounting window its resources are carried to
// the last release, otherwise its own (unobserved) stop channel and client
// close now.
func (inst *Inst) reapWindow(w *window, reason string, unmountMsg string) {
	uc, shared, carried := inst.releaseMount(w)
	if shared {
		inst.warnWorkingsetSharedInstance(w)
		if !containsWindow(carried, w) {
			w.leave()
			w.unload(inst.logger)
		}
		return
	}
	if uc == nil {
		// Never mounted: no state was ever built, so there is nothing to
		// compose and nothing to unmount; only this window's own
		// resources go.
		w.leave()
		w.unload(inst.logger)
		return
	}
	// leave
	for _, cw := range carried {
		cw.leave()
	}
	w.leave()
	// unmount
	inst.saveWorkingset(w, reason)
	umErr := w.appInst.Unmount(uc)
	if umErr != nil {
		inst.logger.Warn().Err(umErr).
			Str("id", string(w.manifest.Id)).
			Uint64("windowKey", uint64(w.key)).
			Msg(unmountMsg)
	}
	// unload
	for _, cw := range carried {
		cw.unload(inst.logger)
	}
	w.unload(inst.logger)
}

func containsWindow(ws []*window, w *window) (found bool) {
	if slices.Contains(ws, w) {
		found = true
		return
	}
	return
}

// Close requests removal of the window with the given key, attaching
// an optional reason that lands in the "stopped" app-lifecycle row.
// The actual Unmount + slice removal happens at the end of the
// current frame (Frame()) so we don't pull state out from under an
// in-flight body. Closing an unknown key is a no-op.
func (inst *Inst) Close(windowKey WindowKeyT, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range inst.windows {
		if w.key == windowKey {
			w.closeReq = true
			if reason != "" {
				w.stopReason = reason
			}
			return
		}
	}
}

// OpenWindows returns the keys of currently open windows in
// declaration order (== the order in which they were Open()'d).
// Primarily a test helper.
func (inst *Inst) OpenWindows() (keys []WindowKeyT) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	keys = make([]WindowKeyT, 0, len(inst.windows))
	for _, w := range inst.windows {
		keys = append(keys, w.key)
	}
	return
}

// Len returns the number of open windows.
func (inst *Inst) Len() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = len(inst.windows)
	return
}

// WindowInfo is a public, read-only snapshot of one open window's
// metadata, returned by WindowInfos for runtime introspection (ADR-0094
// §SD8) without exposing the private window state.
type WindowInfo struct {
	Key     WindowKeyT
	AppId   app.AppIdT
	Display string
	Title   string
	Surface app.SurfaceE
	// Topics is the app's subject classification (ADR-0158 §SD2), copied
	// from the manifest so a window row is readable without joining the
	// app table. Multi-valued: a window has no single category.
	Topics []app.TopicT
	// Kind is the app's provenance (ADR-0158 §SD5) — a filter dimension,
	// not a section.
	Kind       app.KindE
	StopReason string

	// LaunchReason says where this window's content came from: nobody
	// delivered a config, a caller did, or the host restored the app's
	// stored workingset (ADR-0148 §SD5). The distinction is invisible in
	// the window itself, and it is what "which of my windows came back
	// from stored state" asks about.
	LaunchReason app.LaunchReasonE
	// ConfigKind is the vocabulary kind of the delivered launch config,
	// empty for a plain open. It is the manifest's LaunchKind — the host
	// refuses any other at the boundary — repeated here so a row is
	// readable without joining the app table.
	ConfigKind string
	// ConfigBytes is the delivered config's size, 0 for a plain open. The
	// bytes themselves stay inside the window: they are the app's own DTO,
	// may carry a user's query text, and the audit trail already records
	// them where that is intended (ADR-0135 §SD6).
	ConfigBytes int
	// SharesInstance reports that another open window points at the same
	// AppI instance — only possible for a singleton-registered app shown
	// more than once. Load-bearing rather than trivia: such a window can
	// neither be handed a config nor have its workingset saved, because
	// the state is not this window's alone.
	SharesInstance bool
}

// WindowInfos returns a metadata snapshot of the currently open windows
// in declaration order.
//
// Every field it reads is either immutable for the window's lifetime
// (the manifest, the key, the mount context's delivered config and
// reason — both set at Open before the window is published) or mutated
// only under inst.mu (stop reason, the shared-instance refcount), so the
// snapshot is safe to take off the render thread — which the
// introspection provider does, serving a query from an HTTP handler.
// Deliberately absent for that reason: the lazy Mount flags, which the
// render thread writes without the lock.
func (inst *Inst) WindowInfos() (out []WindowInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = make([]WindowInfo, 0, len(inst.windows))
	for _, w := range inst.windows {
		cfg := w.mountCtx.LaunchConfig()
		kind := ""
		if len(cfg) > 0 {
			kind = w.manifest.LaunchKind
		}
		out = append(out, WindowInfo{
			Key:            w.key,
			AppId:          w.manifest.Id,
			Display:        w.manifest.Display,
			Title:          w.manifest.WindowTitle(),
			Surface:        w.manifest.Surface,
			Topics:         w.manifest.Topics,
			Kind:           w.manifest.Kind,
			StopReason:     w.stopReason,
			LaunchReason:   w.mountCtx.LaunchReason(),
			ConfigKind:     kind,
			ConfigBytes:    len(cfg),
			SharesInstance: w.mount != nil && w.mount.refs > 1,
		})
	}
	return
}

// reapClosed unmounts and removes windows whose closeReq flag is set.
// Called at the end of Frame() so we never mutate the slice mid-
// render. Emits app-lifecycle "stopped" rows for each reaped window
// when the audit wiring is attached (SetAudit).
func (inst *Inst) reapClosed() {
	inst.mu.Lock()
	if len(inst.windows) == 0 {
		inst.mu.Unlock()
		return
	}
	// Build the reap set + the kept set under the lock; do the actual
	// Unmount / facts writes outside the lock so an Unmount that calls
	// back into the host (rare, but possible via the bus) doesn't
	// deadlock.
	kept := make([]*window, 0, len(inst.windows))
	var reaped []*window
	for _, w := range inst.windows {
		if w.closeReq {
			reaped = append(reaped, w)
			continue
		}
		kept = append(kept, w)
	}
	inst.windows = kept
	runId := inst.runId
	facts := inst.facts
	inst.mu.Unlock()

	for _, w := range reaped {
		reason := defaultStopReason(w)
		inst.reapWindow(w, reason, "windowhost: unmount error")
		if facts != nil {
			emitStopped(facts, inst.logger, runId, w, reason)
		}
	}
}

// defaultStopReason picks a reason for a window being reaped when the
// caller didn't supply one. Windows that failed Mount get
// "mount-error"; everything else defaults to "user-close" (the most
// likely path: user clicked the in-body × Close button).
func defaultStopReason(w *window) (reason string) {
	if w.stopReason != "" {
		reason = w.stopReason
		return
	}
	if w.mount != nil && w.mount.mountErr != nil {
		reason = "mount-error"
		return
	}
	reason = "user-close"
	return
}

// emitStopped writes one app-lifecycle "stopped" row and logs on failure.
func emitStopped(facts factsstore.FactsStoreI, logger zerolog.Logger, runId string, w *window, reason string) {
	_, err := facts.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId:      runId,
		AppId:      w.manifest.Id,
		TileKey:    uint64(w.key),
		Phase:      factsstore.AppLifecyclePhaseStopped,
		StopReason: reason,
	})
	if err != nil {
		logger.Warn().Err(err).
			Str("id", string(w.manifest.Id)).
			Uint64("windowKey", uint64(w.key)).
			Str("reason", reason).
			Msg("windowhost: write app-lifecycle stopped failed")
	}
}

// Frame renders every open window as a top-level c.Window
// (egui::Window — floating, movable, resizable). Each window's body
// is a small × Close header followed by the app's Frame call. Mount
// runs lazily on the first pass per window; sticky mountErr displays
// an error label and skips Frame so the host stays responsive.
//
// ids must be a stable WidgetIdStack supplied by the caller (usually
// the carousel renderer's bodyIds). Per-window egui Memory (position,
// size, collapsed flag) is keyed by the window's widget id, which is
// derived from `ids.PrepareStr("window-<key>")` — stable for the
// window's lifetime because window keys are monotonic and never
// reused.
//
// When zero windows are open, an empty-state pane is rendered
// instead. The empty-state pane lists every registered app with an
// "open" button per app and runs inside a c.PanelCentral so the user
// can at least see something on the desktop after launch.
func (inst *Inst) Frame(ids *c.WidgetIdStack) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	// Snapshot the slice under lock; the iteration runs without the
	// lock held so AppI.Frame can re-enter (e.g., to call Open via the
	// Apps menu, which would otherwise deadlock).
	inst.mu.Lock()
	snapshot := make([]*window, len(inst.windows))
	copy(snapshot, inst.windows)
	raiseKey := inst.pendingRaise
	inst.pendingRaise = 0
	inst.mu.Unlock()

	if len(snapshot) == 0 {
		// renderEmptyState needs a ui scope — egui's interpret_outer
		// starts each frame with `u = &mut None`; after the carousel's
		// PanelTop / PanelBottom close, we're back at the egui Context
		// root with u=None and c.Label / c.Button would silently no-op.
		// PanelCentral establishes the scope.
		for range c.PanelCentral().KeepIter() {
			inst.renderEmptyState(ids)
		}
		inst.reapClosed()
		return
	}

	// c.Window is a top-level egui::Window; it does not need a parent
	// ui scope (it uses egui::Context directly), so no PanelCentral
	// wrap here.
	sm := c.CurrentApplicationState.StateManager
	// Decide the shell's active window from last frame's stacking
	// reports, then stamp every window's frame context below so each
	// app's Frame can gate process-global input (app.WindowFocusI) on
	// whether ITS window is the active one — the seam that keeps one
	// Ctrl+Enter from running a query in every open playground.
	{
		facts := make([]windowFocusFact, 0, len(snapshot))
		for _, w := range snapshot {
			facts = append(facts, windowFocusFact{
				key:     w.key,
				topmost: sm.GetResponse(w.focusHandle).HasWindowTopmost(),
			})
		}
		inst.activeKey = pickActiveWindow(inst.activeKey, facts)
	}
	for _, w := range snapshot {
		title := w.manifest.WindowTitle()
		if title == "" {
			title = string(w.manifest.Id)
		}
		winId := ids.PrepareStr("window-" + strconv.FormatUint(uint64(w.key), 10))
		// Register the r10 databinding for the title-bar X. Re-registers
		// every frame because applyDataBindingsConst2 resets the
		// databindings map after each Sync; the bindingId is derived
		// deterministically from the window key so the Rust side keys
		// the bool in its window_open_bindings HashMap stably across
		// frames. After this frame's Sync, w.openFlag reflects the
		// post-egui state; we check for the false transition below.
		openBindingId := openBindingIdFor(w.key)
		sm.AddR10Databinding(openBindingId, &w.openFlag)
		ww, hh := windowDefaultSize(w.manifest.SurfaceHints)
		wf := c.Window(winId, c.WidgetText().Text(title).Keep()).
			Resizable(true).
			TitleBar(true).
			DefaultOpen(true).
			DefaultSize(ww, hh).
			OpenBound(openBindingId)
		// The handle feeds next frame's active-window decision; the
		// focus stamp is this frame's answer, set before the app's
		// Frame runs inside the block body.
		w.focusHandle = wf.Handle()
		w.frameCtx.SetWindowFocused(w.key == inst.activeKey)
		if w.key == raiseKey {
			// OpenOrRaise's queued raise: bring the existing window to
			// the front of egui's stacking. The topmost report follows a
			// frame later and moves the active window here with it.
			c.MoveWindowToTop(w.focusHandle)
		}
		for range wf.KeepIter() {
			// Top-of-body chrome: a small "Save as SVG…" affordance
			// rendered above the app's Frame. egui::Window has no
			// custom-title-bar-button API in this IDL (the open(&mut
			// bool) hook is the only title-bar slot), so the action
			// lives in the body — kept compact (one icon + 3-letter
			// label) so the visual cost is bounded. Per-window keying
			// avoids id collisions across windows on the shared ids
			// stack.
			saveBtnId := ids.PrepareStr("windowhost-save-svg-" +
				strconv.FormatUint(uint64(w.key), 10))
			if c.Button(saveBtnId,
				c.Atoms().Text(icons.IconSaveAs+" SVG").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.pendingExportKey = w.key
				inst.fpSaveSvg.Show()
			}
			renderWindowBody(w, inst.logger)
		}
	}
	// Render the SVG-save picker once per Frame. It draws its own
	// egui::Window so it sits at top level; Render returns
	// ActionNone for non-commit frames. On Save, re-prepare the
	// originating window's widget id (the in-loop c.Window already
	// consumed the handle this pass — see SKILLS:fffi2) and queue
	// the ExportSvgWindow opcode. The SvgExportPlugin drains it in
	// on_end_pass this same frame, so the captured shapes match what
	// the user just saw.
	switch act, paths := inst.fpSaveSvg.Render(ids); act {
	case filepicker.ActionSave:
		if inst.pendingExportKey != 0 && len(paths) > 0 {
			key := inst.pendingExportKey
			inst.pendingExportKey = 0
			p := paths[0]
			// Re-prepare the window's widget id and wrap the derived
			// value in a WidgetHandle. The in-loop `c.Window(...)`
			// already consumed the prepared id and pushed/popped its
			// stacked scope this pass, so the WidgetIdStack is back at
			// the same outer state — derive here gives the same wire
			// id the window painted under (XOR-with-stack-top is
			// deterministic for the same input + stack state).
			ids.PrepareStr("window-" +
				strconv.FormatUint(uint64(key), 10))
			h := widgethandle.Make(ids.Derive())
			// Faithful mode (0) + dark VIEWPORT_BG so the saved SVG
			// reads as a screenshot of the window as the user sees
			// it. M3 introduces a `ContentOnly` mode (1) + transparent
			// bg for HTML-embedding reports; expose it on the UI with
			// a second affordance when there's user demand.
			c.ExportSvgWindow(h, p, true, 0, 0x1e1e1eff)
			inst.logger.Info().
				Uint64("windowKey", uint64(key)).
				Str("path", p).
				Msg("windowhost: queued ExportSvgWindow")
		}
	case filepicker.ActionCancel:
		inst.pendingExportKey = 0
	}
	// Detect title-bar X clicks: any window whose openFlag flipped to
	// false since last frame gets queued for reap with reason
	// "user-close". Reads after Frame because StateManager.Sync runs
	// after this Frame returns, so openFlag won't be updated this
	// frame — instead we read whatever Sync set on the previous frame.
	// The one-frame lag is invisible at interactive cadence; the
	// canonical close-button latency is identical to every other
	// r7/r10-mediated widget response.
	inst.mu.Lock()
	for _, w := range inst.windows {
		if !w.openFlag && !w.closeReq {
			w.closeReq = true
			w.stopReason = "user-close"
		}
	}
	inst.mu.Unlock()
	inst.reapClosed()
	return
}

// windowFocusFact is one open window's contribution to the active-window
// decision, in Frame order (oldest open first).
type windowFocusFact struct {
	key     WindowKeyT
	topmost bool
}

// pickActiveWindow decides which open window is the shell's active one.
//
// The primary signal is egui's own stacking: the window whose Area is the
// top layer of the Middle order (r7 WINDOW_TOPMOST, one frame late like
// every response signal) — egui raises a window on any press inside it, so
// topmost tracks where the user last clicked. When NO host window is
// topmost — an app's tethered child window, the SVG-save picker, or the
// help host holds the top layer — the previous answer stands: a child
// surface belongs to the interaction that opened it, and "the window I am
// working in" does not change because something popped up over it. A
// previous answer that is gone (window closed, or nothing decided yet)
// falls to the newest window, which is also the one egui opens on top.
func pickActiveWindow(prev WindowKeyT, facts []windowFocusFact) (active WindowKeyT) {
	if len(facts) == 0 {
		return 0
	}
	for _, f := range facts {
		if f.topmost {
			return f.key
		}
	}
	for _, f := range facts {
		if f.key == prev {
			return prev
		}
	}
	return facts[len(facts)-1].key
}

// openBindingIdFor derives the r10 binding id for a window key. The
// derivation is deterministic and never collides with other r10
// binding ids in the same process because window keys are monotonic
// uint64s allocated by Inst.nextKey, and PrepareStr-derived widget
// ids share the same 64-bit namespace via different hash seeds —
// XOR'ing the key with a high-entropy magic ensures no accidental
// hash collision while keeping the value stable for the window's
// lifetime.
func openBindingIdFor(key WindowKeyT) (id uint64) {
	const magic uint64 = 0xC4B7_E0B1_0B7E_D9E5
	id = uint64(key) ^ magic
	return
}

// windowDefaultSize returns the initial size for a new window.
// Honours SurfaceHints when set, otherwise falls back to the medium
// SurfaceApp archetype (ADR-0065) — a sensible pair that fits most laptop
// screens without occupying the whole viewport. No registered app currently
// hits this fallback; every windowed app sets hints.
func windowDefaultSize(h app.SurfaceHints) (w, height float32) {
	w = float32(h.PreferredWidth)
	if w == 0 {
		w = float32(styletokens.SurfaceApp.W)
	}
	height = float32(h.PreferredHeight)
	if height == 0 {
		height = float32(styletokens.SurfaceApp.H)
	}
	return
}

// OpenOrRaiseApp opens appId or raises its existing window, discarding the
// window key and the opened flag. It exists so *Inst satisfies the launcher's
// host interface (ADR-0214 §SD3) structurally, without either package naming
// the other's types: the launcher declares what it needs of a host in
// app-level terms, and this is the host answering in those terms.
func (inst *Inst) OpenOrRaiseApp(appId app.AppIdT) (err error) {
	_, _, err = inst.OpenOrRaise(appId)
	return
}

// OpenAppIds reports which apps currently hold a window, for the launcher's
// "open" badge. Duplicates are possible and meaningful to nobody here — two
// windows of one app are still one "open" — so callers build a set.
func (inst *Inst) OpenAppIds() (ids []app.AppIdT) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ids = make([]app.AppIdT, 0, len(inst.windows))
	for _, w := range inst.windows {
		ids = append(ids, w.manifest.Id)
	}
	return
}

// launcherI is the launcher surface the host renders when no window is open,
// and delegates its Apps ▾ menu to (ADR-0214 §SD2). Declared here rather than
// taken as the concrete type so this package keeps compiling against a fake
// in its own tests, and so the seam names exactly what the host asks of it.
//
// The launcher package is the implementing side: it must not import
// windowhost, because windowhost imports it (§SD3).
type launcherI interface {
	Render(ids *c.WidgetIdStack)
	RenderMenu(ids *c.WidgetIdStack)
}

// SetLauncher installs the launcher surface. Optional — a host without one
// renders an empty-state pane that says the launcher is unavailable, which is
// what the screenshot-tour path gets and what the host's own tests run with.
func (inst *Inst) SetLauncher(l launcherI) {
	inst.launcher = l
}

// renderEmptyState draws the pane shown when no window is open. Since
// ADR-0214 §SD2 that is the launcher component itself rather than a second,
// differently-abled browse surface: the pane and the menu had drifted into
// "the one that can search" and "the one that is reachable", and the fix is
// that there is now one component with one state value.
func (inst *Inst) renderEmptyState(ids *c.WidgetIdStack) {
	if inst.launcher == nil {
		c.Label("No applications open.").Send()
		c.Label("(launcher unavailable — no launcher was wired into this host)").Send()
		return
	}
	inst.launcher.Render(ids)
}

// windowhostDebugRender mirrors DebugRender.Get() != "" at init time.
var windowhostDebugRender = DebugRender.Get() != ""

// windowhostInstanceSalt derives the per-window widget-id salt that
// renderWindowBody pushes around each app's Frame. The salt MUST live
// outside the sequence-id space that PrepareSeq(index) draws from
// (makeHighEntropy of small integers): the widget-id stack combines a
// pushed salt and a child's PrepareSeq(index) by XOR, so a salt of
// makeHighEntropy(w.key) — what an earlier appIds.PrepareSeq(w.key)
// produced — aliased option ids across windows whenever a window key
// equalled a child index. Two open windows then shared the tab whose
// index matched the other window's key (e.g. window 1 tab 2 == window 2
// tab 1), colliding in the global seenIds registry and sharing egui
// widget state. Multiplying the key by the golden-ratio odd constant and
// XOR-ing a domain tag moves the salt clear of that space, mirroring the
// play app's playInstanceSalt.
func windowhostInstanceSalt(key WindowKeyT) uint64 {
	const saltTag = 0x57696e486f737421 // "WinHost!"
	return (uint64(key) * 0x9e3779b97f4a7c15) ^ saltTag
}

// renderWindowBody draws one window's body: the app's Frame call,
// gated by lazy Mount + sticky mountErr handling. The close
// affordance is the egui::Window title-bar X (wired via openBound +
// the r10 openFlag databinding registered in Frame); there is no
// in-body close button.
//
// The Frame call is wrapped in c.IdScope on the per-window appIds
// stack with a per-window salt from windowhostInstanceSalt(w.key), so
// two open apps that happen to share a label string ("btm", "cheat", …)
// on their outermost panel cannot collide on the wire id — each derives
// its id under a different salt. The IdScope wrapper pops the salt on
// return so the stack is empty between frames.
func renderWindowBody(w *window, logger zerolog.Logger) {
	if windowhostDebugRender {
		logger.Info().
			Uint64("windowKey", uint64(w.key)).
			Str("id", string(w.manifest.Id)).
			Msg("windowhost: rendering window body")
	}
	if w.closeReq {
		// closeReq was set this frame (external Close or shutdown
		// reap). Skip Frame to avoid drawing content the next reap is
		// about to tear down anyway.
		return
	}
	// Mount runs once per AppI instance (shared via w.mount), capturing the
	// first window's mountCtx so the eventual Unmount uses the same context.
	if !w.mount.mounted && w.mount.mountErr == nil {
		mErr := w.appInst.Mount(w.mountCtx)
		if mErr != nil {
			w.mount.mountErr = mErr
		} else {
			w.mount.mounted = true
			w.mount.mountCtx = w.mountCtx
		}
	}
	if w.mount.mountErr != nil {
		c.Label("windowhost: mount failed: " + w.mount.mountErr.Error()).Send()
		return
	}
	for range c.IdScope(w.appIds.PrepareHighEntropy(windowhostInstanceSalt(w.key))) {
		fErr := w.appInst.Frame(w.frameCtxApp)
		if fErr != nil {
			c.Label("windowhost: frame error: " + fErr.Error()).Send()
		}
	}
}

// RenderAppsMenu draws the shell's top-bar "Apps ▾" menu.
//
// A delegate since ADR-0214 §SD2: the launcher owns every launcher surface,
// and this method exists so the shell chrome keeps one call site. What the
// menu holds also changed — recents plus a door to the launcher, instead of a
// submenu per topic over every registered app. The reasoning is in the
// launcher package's menu.go; the short version is that a menu cannot hold a
// search box, and at this corpus size a cascade without one is unusable.
//
// ids is the caller's stack. Place inside a MenuBar (typically the shell's
// top PanelTop), alongside the File / Layout menus.
func (inst *Inst) RenderAppsMenu(ids *c.WidgetIdStack) {
	if inst.launcher == nil {
		for range c.MenuButton(c.Atoms().Text("Apps").Keep()).KeepIter() {
			c.Label("(launcher unavailable)").Send()
		}
		return
	}
	inst.launcher.RenderMenu(ids)
}

// frameCtxColWidth adds the ADR-0151 column-width capability to a window's
// frame context. It embeds the static context rather than reimplementing
// it, so every other capability the host sets each frame — window focus,
// the egui scope — passes through untouched.
type frameCtxColWidth struct {
	*app.StaticFrameContext
	store colwidth.StoreI
}

var _ colwidth.HostI = (*frameCtxColWidth)(nil)

// ColumnWidthStore hands back the host's facts store, whose column-width
// verbs are exactly colwidth.StoreI. Scoping is the app's: the resolver
// keys every read and write by the mount context's AppId, so one app's
// overrides cannot reach another's through this.
func (inst *frameCtxColWidth) ColumnWidthStore() (store colwidth.StoreI) {
	store = inst.store
	return
}
