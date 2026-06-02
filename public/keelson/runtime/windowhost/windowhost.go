//go:build llm_generated_opus47

package windowhost

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
	key        WindowKeyT
	manifest   app.Manifest
	appInst    app.AppI
	mountCtx   *app.StaticMountContext
	frameCtx   *app.StaticFrameContext
	mounted    bool
	mountErr   error // sticky; window body renders an error label when set
	closeReq   bool  // set by the in-body Close button or external Close()
	stopReason string

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
}

// Inst is the window host: the registry plus the list of open windows.
// The zero value is unusable; construct via NewInst.
//
// Goroutine safety: not goroutine-safe. The render loop is single-
// threaded (Go side runs Frame on the main goroutine); Open/Close are
// called from inside Frame's call stack (via the Apps menu or the
// in-body close button) or from CLI argument parsing before the loop
// starts. Tests stick to the single-goroutine contract.
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

	// bus is the M2 in-proc subject router (ADR-0026 §SD3, §SD5).
	// When non-nil, Open mints a per-app inprocbus.Client carrying
	// the app's Manifest.Caps and passes it through MountCtx.Bus().
	// When nil, MountCtx.Bus() falls back to NoopBus — the M1
	// shape every app was bootstrapped against.
	bus *inprocbus.Inst

	mu      sync.Mutex
	nextKey uint64
	windows []*window

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

	// searchText backs the launcher search box (rendered both at the
	// top of the Apps ▾ menu and at the top of the empty-state pane).
	// Single shared field so typing in either surface filters the
	// other on the next frame: the menu and the pane are mutually
	// exclusive at any one moment, but a session that drifts between
	// them (open an app → empty-state hidden; close all → empty-state
	// returns; reopen menu) sees its previous query persist instead of
	// being randomly wiped.
	//
	// Mutated only inside the render loop (TextEdit.SendRespVal writes
	// after StateManager.Sync). Reads outside the render loop need no
	// lock because the host is single-threaded by contract; if that
	// invariant ever loosens, fold this into a per-Inst mu-guarded
	// snapshot.
	searchText string
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
		registry: registry,
		logger:   logger,
		density:  styletokens.DensityFromEnv(),
		fpSaveSvg: filepicker.New("windowhost-save-svg", filepicker.ModeSave,
			filepicker.WithExtensionFilter(".svg"),
			filepicker.WithDefaultFilename("window.svg"),
			filepicker.WithStartAtOsHome()),
	}
	return
}

// SetBus attaches an in-proc bus to the window host. Once set, each
// Open mints a per-app inprocbus.Client (gated on the app's
// Manifest.Caps) and threads it through MountCtx.Bus() so apps can
// publish/subscribe/request through the M2 subject router (ADR-0026
// §SD3, §SD5). Passing nil clears the wiring (subsequent Opens
// hand out NoopBus). Calling SetBus after windows have been opened
// is supported but only affects subsequent Opens — already-mounted
// windows keep the bus they were given.
func (inst *Inst) SetBus(bus *inprocbus.Inst) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.bus = bus
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
func (inst *Inst) Open(appId app.AppIdT) (key WindowKeyT, err error) {
	m, ok := inst.registry.LookupManifest(appId)
	if !ok {
		err = eb.Build().Str("id", string(appId)).Errorf("windowhost: app not registered id=%s", string(appId))
		return
	}
	a, err := inst.registry.Open(appId)
	if err != nil {
		err = eh.Errorf("windowhost: open: %w", err)
		return
	}
	inst.mu.Lock()
	inst.nextKey++
	key = WindowKeyT(inst.nextKey)
	// Mint a per-app bus client when the host has an inprocbus.Inst
	// attached. inprocbus.Client implements app.BusI; per-app caps
	// from the manifest are baked in at construction time (additional
	// caps land via capbroker grants in a later phase). When the host
	// has no bus, the nil falls through to NoopBus inside
	// NewStaticMountContext.
	var busC app.BusI
	var storageC app.StorageI
	if inst.bus != nil {
		client := inst.bus.NewClient(m.Id, m.Caps)
		busC = client
		// Phase C: auto-inject the persist cap for any app declaring
		// PersistedKeys. The manifest field exists exactly so apps
		// don't have to repeat the boilerplate cap pattern; the
		// host materialises it when needed.
		if len(m.PersistedKeys) > 0 {
			client.AddCap(app.SubjectFilter{
				Pattern:   persist.SubjectPrefix + m.Id.SubjectAlias() + ".>",
				Direction: app.CapDirectionPub,
				Reason:    "host-injected for declared PersistedKeys",
			})
		}
		// Storage client wraps the same bus client so MountCtx.Storage()
		// shares the per-app permission set with MountCtx.Bus(). Errors
		// from NewClient are impossible here (busC is non-nil) but the
		// signature returns one — if it ever fires, surface as nil so
		// the app sees NoopStorage rather than a half-built client.
		sc, sErr := persist.NewClient(busC, m.Id)
		if sErr != nil {
			inst.logger.Warn().Err(sErr).Str("appId", string(m.Id)).
				Msg("windowhost: persist client construction failed; using NoopStorage")
		} else {
			storageC = sc
		}
	}
	// Per-window logger: app_id + instance_id pre-tagged so any zerolog
	// event the app emits surfaces with the full identity tuple
	// (run_id is added by runinfo.TagLogger on the carousel's global
	// logger; AppLogger adds app_id; we add instance_id here).
	perWindowLogger := app.AppLogger(inst.logger, m.Id).
		With().Uint64("instance_id", uint64(key)).Logger()
	mountCtx := app.NewStaticMountContext(m.Id, perWindowLogger, storageC, busC, nil)
	mountCtx.SetInstanceKey(uint64(key))
	mountCtx.SetRunId(inst.runId)
	appIds := c.NewWidgetIdStack()
	mountCtx.SetIds(appIds)
	frameCtx := app.NewStaticFrameContext(mountCtx, nil)
	inst.windows = append(inst.windows, &window{
		key:      key,
		manifest: m,
		appInst:  a,
		mountCtx: mountCtx,
		frameCtx: frameCtx,
		appIds:   appIds,
		openFlag: true,
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
		if w.mounted {
			umErr := w.appInst.Unmount(w.mountCtx)
			if umErr != nil {
				inst.logger.Warn().Err(umErr).
					Str("id", string(w.manifest.Id)).
					Uint64("windowKey", uint64(w.key)).
					Msg("windowhost: unmount on shutdown error")
			}
		}
		if facts != nil {
			emitStopped(facts, inst.logger, runId, w, reason)
		}
	}
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
		if w.mounted {
			umErr := w.appInst.Unmount(w.mountCtx)
			if umErr != nil {
				inst.logger.Warn().Err(umErr).
					Str("id", string(w.manifest.Id)).
					Uint64("windowKey", uint64(w.key)).
					Msg("windowhost: unmount error")
			}
		}
		if facts != nil {
			emitStopped(facts, inst.logger, runId, w, defaultStopReason(w))
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
	if w.mountErr != nil {
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
	// Snapshot the slice under lock; the iteration runs without the
	// lock held so AppI.Frame can re-enter (e.g., to call Open via the
	// Apps menu, which would otherwise deadlock).
	inst.mu.Lock()
	snapshot := make([]*window, len(inst.windows))
	copy(snapshot, inst.windows)
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
	for _, w := range snapshot {
		w := w // capture
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
		for range c.Window(winId, c.WidgetText().Text(title).Keep()).
			Resizable(true).
			TitleBar(true).
			DefaultOpen(true).
			DefaultSize(ww, hh).
			OpenBound(openBindingId).
			KeepIter() {
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

// preferredCategoryOrder is the display order the launcher renders for
// Manifest.Category buckets. Categories not listed here sort
// alphabetically after the named buckets; uncategorisedBucket always
// sorts last.
var preferredCategoryOrder = []string{
	"Runtime",
	"Tools",
	"Demos",
}

// uncategorisedBucket is the label used for manifests whose Category is
// empty. Always sorts last regardless of what's in preferredCategoryOrder.
const uncategorisedBucket = "Other"

// manifestGroup is one launcher bucket: a category label and the
// manifests that belong to it. Used by both the Apps menu and the
// empty-state pane so the two surfaces stay in sync.
type manifestGroup struct {
	Category  string
	Manifests []app.Manifest
}

// groupByCategory partitions manifests into per-Category buckets and
// returns them in the order defined by preferredCategoryOrder, with any
// extra categories sorted alphabetically in between and
// uncategorisedBucket (catch-all for empty Category) last. Within each
// bucket manifests sort by Display then Id, so two apps that happen to
// share a label still produce stable ordering.
func groupByCategory(manifests []app.Manifest) (groups []manifestGroup) {
	if len(manifests) == 0 {
		return
	}
	byCat := make(map[string][]app.Manifest, len(preferredCategoryOrder)+1)
	for _, m := range manifests {
		cat := m.Category
		if cat == "" {
			cat = uncategorisedBucket
		}
		byCat[cat] = append(byCat[cat], m)
	}
	preferred := make(map[string]int32, len(preferredCategoryOrder))
	for i, c := range preferredCategoryOrder {
		preferred[c] = int32(i)
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.SliceStable(cats, func(i, j int) (less bool) {
		ci, cj := cats[i], cats[j]
		// uncategorisedBucket always last
		if ci == uncategorisedBucket {
			less = false
			return
		}
		if cj == uncategorisedBucket {
			less = true
			return
		}
		pi, oki := preferred[ci]
		pj, okj := preferred[cj]
		switch {
		case oki && okj:
			less = pi < pj
		case oki:
			less = true
		case okj:
			less = false
		default:
			less = ci < cj
		}
		return
	})
	groups = make([]manifestGroup, 0, len(cats))
	for _, c := range cats {
		ms := byCat[c]
		sortManifestsByDisplay(ms)
		groups = append(groups, manifestGroup{Category: c, Manifests: ms})
	}
	return
}

// filterManifests returns the subset of manifests whose Display or
// Category matches the query (case-insensitive substring). Empty
// query returns the input slice unchanged so callers can treat
// "no query" and "query matches everything" identically without an
// extra branch.
//
// Match scope is intentionally narrow: Display first (what the user
// types looking at the visible label), then Category as a secondary
// signal ("demo" should surface every Demos entry even when no app's
// own name starts with the word). Id and Title are deliberately
// excluded — Id is the full import path (would match every entry on
// "github") and Title is usually a longer-form variant of Display.
//
// Ordering follows the input; callers control sort (the launcher's
// flat hit path sorts by Display via the same sort used inside
// groupByCategory's intra-bucket pass).
func filterManifests(manifests []app.Manifest, query string) (hits []app.Manifest) {
	q := strings.TrimSpace(query)
	if q == "" {
		hits = manifests
		return
	}
	qLower := strings.ToLower(q)
	hits = make([]app.Manifest, 0, len(manifests))
	for _, m := range manifests {
		if matchManifestSearch(m, qLower) {
			hits = append(hits, m)
		}
	}
	return
}

// matchManifestSearch returns whether manifest m matches the
// pre-lowercased query qLower. Factored so filterManifests stays a
// straight-line loop and the match rule itself is one line per
// candidate field — easy to extend later without changing the
// filtering control flow.
func matchManifestSearch(m app.Manifest, qLower string) (ok bool) {
	if strings.Contains(strings.ToLower(m.Display), qLower) {
		ok = true
		return
	}
	if strings.Contains(strings.ToLower(m.Category), qLower) {
		ok = true
		return
	}
	return
}

// renderEmptyState draws the "no apps open" pane shown when no
// windows are active. A search box sits at the top of the pane; an
// empty query renders the per-Category sections (matching the Apps
// menu's ordering — Runtime, Tools, Demos, …, Other), and a non-empty
// query flattens the pane into a single list of apps whose Display or
// Category contains the typed substring (case-insensitive). The
// search field is the only launcher input surface; the Apps ▾ menu
// has no in-bar search affordance — see RenderAppsMenu for the
// rationale.
func (inst *Inst) renderEmptyState(ids *c.WidgetIdStack) {
	c.Label("No applications open.").Send()
	c.Label("Pick one below, or use the Apps ▾ menu in the top bar.").Send()
	c.AddSpace(styletokens.GapItems(inst.density))
	manifests := inst.registry.AllManifests()
	if len(manifests) == 0 {
		c.Label("(no apps registered)").Send()
		return
	}
	// Search box with a small top/bottom inner margin so the input
	// doesn't crash into the helper labels above or the section
	// header below. PaddingTight is the IDS token for chrome
	// breathing room.
	pad := styletokens.PaddingTight(inst.density)
	for range c.Frame(ids.PrepareStr("empty-state-search-frame")).
		InnerMarginSides(0, 0, pad, pad).
		KeepIter() {
		searchId := ids.PrepareStr("empty-state-search")
		c.TextEdit(searchId, inst.searchText, false).
			HintText("Search apps…").
			DesiredWidth(360).
			SendRespVal(&inst.searchText)
	}
	query := strings.TrimSpace(inst.searchText)
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		if query == "" {
			groups := groupByCategory(manifests)
			for gi, g := range groups {
				if gi > 0 {
					c.AddSpace(styletokens.GapSections(inst.density))
				}
				c.Label(g.Category).Send()
				c.Separator().Horizontal().Send()
				for _, m := range g.Manifests {
					inst.renderEmptyStateEntry(ids, m, false)
				}
			}
			return
		}
		hits := filterManifests(manifests, query)
		if len(hits) == 0 {
			c.Label("(no matches)").Send()
			return
		}
		sortManifestsByDisplay(hits)
		for _, m := range hits {
			inst.renderEmptyStateEntry(ids, m, true)
		}
	}
}

// renderEmptyStateEntry draws one app row inside the empty-state pane,
// mirroring renderAppsMenuEntry's contract. withCategory appends an
// em-dashed Category suffix — only meaningful in the flattened
// search-hit view, where the section header no longer carries that
// information.
func (inst *Inst) renderEmptyStateEntry(ids *c.WidgetIdStack, m app.Manifest, withCategory bool) {
	label := m.WindowTitle()
	if label == "" {
		label = string(m.Id)
	}
	if withCategory && m.Category != "" {
		label = label + " — " + m.Category
	}
	btnId := ids.PrepareStr("empty-open-" + string(m.Id))
	if !c.Button(btnId, c.Atoms().Text(label).Keep()).
		SendResp().HasPrimaryClicked() {
		return
	}
	inst.logger.Info().
		Str("id", string(m.Id)).
		Msg("windowhost: empty-state click detected; opening window")
	_, oErr := inst.Open(m.Id)
	if oErr != nil {
		inst.logger.Warn().Err(oErr).
			Str("id", string(m.Id)).
			Msg("windowhost: open from empty-state failed")
	}
}

// windowhostDebugRender mirrors DebugRender.Get() != "" at init time.
var windowhostDebugRender = DebugRender.Get() != ""

// renderWindowBody draws one window's body: the app's Frame call,
// gated by lazy Mount + sticky mountErr handling. The close
// affordance is the egui::Window title-bar X (wired via openBound +
// the r10 openFlag databinding registered in Frame); there is no
// in-body close button.
//
// The Frame call is wrapped in c.IdScope on the per-window appIds
// stack with `w.key` as the salt. The window key is monotonic and
// unique across the lifetime of the host, so two open apps that
// happen to share a label string ("btm", "cheat", …) on their
// outermost panel cannot collide on the wire id — each derives its
// id under a different salt. The IdScope wrapper pops the salt on
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
	if !w.mounted && w.mountErr == nil {
		mErr := w.appInst.Mount(w.mountCtx)
		if mErr != nil {
			w.mountErr = mErr
		} else {
			w.mounted = true
		}
	}
	if w.mountErr != nil {
		c.Label("windowhost: mount failed: " + w.mountErr.Error()).Send()
		return
	}
	for range c.IdScope(w.appIds.PrepareSeq(uint64(w.key))) {
		fErr := w.appInst.Frame(w.frameCtx)
		if fErr != nil {
			c.Label("windowhost: frame error: " + fErr.Error()).Send()
		}
	}
}

// RenderAppsMenu draws an "Apps ▾" menu listing every registered app,
// grouped into per-Category submenus (preferredCategoryOrder — Runtime,
// Tools, Demos — then alphabetised extras, then "Other"). Clicking an
// entry calls Open(id) for that app; the new window appears on the
// next frame. Entries within a category sort by Display.
//
// ids is the caller's stack; the menu uses derived ids for the
// per-entry buttons. Place inside a MenuBar (typically the carousel's
// top PanelTop), alongside File / Layout menus.
//
// The menu deliberately has no in-bar search field. egui's
// menu_button closes on any click outside a menu Button (TextEdit
// focus clicks included), and lifting the field into the menu bar
// added chrome clutter for a rarely-used affordance. Search lives in
// the empty-state pane instead (see renderEmptyState), backed by the
// same inst.searchText buffer so future surfaces can hook into the
// same filter state.
func (inst *Inst) RenderAppsMenu(ids *c.WidgetIdStack) {
	for range c.MenuButton(c.Atoms().Text("Apps").Keep()).KeepIter() {
		manifests := inst.registry.AllManifests()
		if len(manifests) == 0 {
			c.Label("(no apps registered)").Send()
			return
		}
		groups := groupByCategory(manifests)
		for _, g := range groups {
			for range c.MenuButton(c.Atoms().Text(g.Category).Keep()).KeepIter() {
				for _, m := range g.Manifests {
					inst.renderAppsMenuEntry(ids, m, false)
				}
			}
		}
	}
}

// renderAppsMenuEntry draws one menu entry for the given manifest.
// Factored out of RenderAppsMenu so the per-entry click dispatch is
// reusable across category submenus and the flat search-hit list.
// When withCategory is true the manifest's Category is appended to the
// button label as an em-dashed suffix — only meaningful in the
// flattened search view, where the submenu chrome no longer carries
// that information.
func (inst *Inst) renderAppsMenuEntry(ids *c.WidgetIdStack, m app.Manifest, withCategory bool) {
	label := m.WindowTitle()
	if label == "" {
		label = string(m.Id)
	}
	if withCategory && m.Category != "" {
		label = label + " — " + m.Category
	}
	btnId := ids.PrepareStr("open-" + string(m.Id))
	if !c.Button(btnId, c.Atoms().Text(label).Keep()).
		SendResp().HasPrimaryClicked() {
		return
	}
	inst.logger.Info().
		Str("id", string(m.Id)).
		Msg("windowhost: apps-menu click detected; opening window")
	_, err := inst.Open(m.Id)
	if err != nil {
		inst.logger.Warn().Err(err).
			Str("id", string(m.Id)).
			Msg("windowhost: open from menu failed")
	}
}

// sortManifestsByDisplay reorders the slice in place by Display
// (then Id for ties) — the same comparator groupByCategory uses
// inside each bucket. Hoisted so the flat search-hit path can apply
// the same ordering without duplicating the closure.
func sortManifestsByDisplay(manifests []app.Manifest) {
	sort.SliceStable(manifests, func(i, j int) (less bool) {
		di, dj := manifests[i].Display, manifests[j].Display
		if di == dj {
			less = manifests[i].Id < manifests[j].Id
			return
		}
		less = di < dj
		return
	})
}
