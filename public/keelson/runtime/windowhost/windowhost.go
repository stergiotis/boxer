package windowhost

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
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

	// searchHl is the search box's regexedit highlight-job cache
	// (ADR-0164 §SD4). One per box: two boxes sharing an Edit would
	// evict each other's job every frame. Render-thread confined, like
	// the searchText buffer it colours.
	searchHl regexedit.Edit

	// kindShown backs the launcher's provenance toggles (ADR-0158
	// §SD5/§SD6): the controls that answer "show me the demos" now that
	// provenance is no longer a browse section. One entry per app.AllKinds
	// member, positionally — not indexed by the enum value, so the toggles
	// do not silently depend on KindE staying contiguous from zero.
	//
	// It is a []bool rather than the kindFilterT mask the filter actually
	// wants because Checkbox.SendRespVal needs a stable address to write
	// into, and that write is *deferred* to StateManager.Sync — after the
	// frame body has run. Comparing a local before/after inside the body
	// would therefore never observe a change, and the toggle would be a
	// silent no-op. Binding straight to a persistent field is the working
	// pattern; kindFilter() derives the mask each frame.
	//
	// Shared by the Apps ▾ menu and the empty-state pane for the same
	// reason searchText is, and mutated on the render thread only.
	kindShown []bool

	// topicFilter restricts the browse view to a chosen set of topics
	// (ADR-0158 §SD6). Held as the mask itself rather than as a []bool
	// mirror because its chips are SelectableLabels, whose click response
	// is read within the frame — the deferred-write problem that shapes
	// kindShown does not arise here.
	topicFilter topicFilterT
}

// topicFilterT is the set of topics the launcher restricts the view to, as
// a bitmask over app.AllTopics *positions* (TopicT is a string, so position
// is what a mask can address; it also means the mask never depends on the
// tokens themselves).
//
// The zero value selects nothing and means **no restriction** — the
// opposite polarity to [kindFilterT], which stores what is hidden. That is
// deliberate rather than an inconsistency: the two axes are used with
// different gestures. There are three kinds, you normally want all of them,
// and the useful action is "hide the demos" — so a hidden-set is the
// natural store. There are nine topics, you normally want one, and the
// useful action is "show me only code" — so a selected-set is. Both
// conventions make the zero value inert, which is the property that
// matters for an untouched host.
type topicFilterT uint32

// topicIndex resolves a topic to its position in app.AllTopics.
func topicIndex(t app.TopicT) (idx int, ok bool) {
	for i, x := range app.AllTopics {
		if x == t {
			idx = i
			ok = true
			return
		}
	}
	return
}

// isInert reports whether the filter restricts nothing.
func (inst topicFilterT) isInert() (ok bool) {
	ok = inst == 0
	return
}

// selectedAt reports whether the topic at position idx is selected.
func (inst topicFilterT) selectedAt(idx int) (ok bool) {
	ok = inst&(1<<idx) != 0
	return
}

// toggledAt returns the filter with position idx flipped.
func (inst topicFilterT) toggledAt(idx int) (out topicFilterT) {
	out = inst ^ (1 << idx)
	return
}

// shows reports whether topic t passes. An inert filter passes everything,
// which is what makes "nothing selected" mean "no restriction".
func (inst topicFilterT) shows(t app.TopicT) (ok bool) {
	if inst.isInert() {
		ok = true
		return
	}
	idx, known := topicIndex(t)
	if !known {
		return
	}
	ok = inst.selectedAt(idx)
	return
}

// showsAny reports whether a manifest carries at least one passing topic —
// the manifest-level question, since a manifest may carry several.
func (inst topicFilterT) showsAny(topics []app.TopicT) (ok bool) {
	if inst.isInert() {
		ok = true
		return
	}
	if slices.ContainsFunc(topics, inst.shows) {
		ok = true
		return
	}
	return
}

// launcherFilter is the launcher's whole filter state (ADR-0158 §SD6) in
// one value: the query string, the provenance toggles, and the topic chips.
// Both surfaces resolve through it, which is what keeps the Apps ▾ menu and
// the empty-state pane from drifting apart.
type launcherFilter struct {
	query  string
	kinds  kindFilterT
	topics topicFilterT
}

// isInert reports whether the filter would remove nothing.
func (inst launcherFilter) isInert() (ok bool) {
	ok = strings.TrimSpace(inst.query) == "" &&
		!inst.kinds.hidesAnything() &&
		inst.topics.isInert()
	return
}

// admits applies the two facet axes — kind and topic — to one manifest.
// Split out because the query axis is scored rather than boolean, so the
// two halves of the filter no longer read as one condition.
func (inst launcherFilter) admits(m app.Manifest) (ok bool) {
	ok = inst.kinds.shows(m.Kind) && inst.topics.showsAny(m.Topics)
	return
}

// kindFilterT is the set of app.KindE values the launcher **hides**.
//
// Storing the hidden set rather than the shown one is deliberate on two
// counts: the zero value hides nothing, so an untouched host shows
// everything and needs no initialisation; and "every kind hidden" stays
// distinguishable from "nothing configured yet", which a shown-set mask
// with a zero-means-all convention could not express.
type kindFilterT uint8

// shows reports whether a manifest of kind k survives the filter.
func (inst kindFilterT) shows(k app.KindE) (ok bool) {
	ok = inst&(1<<k) == 0
	return
}

// toggled returns the filter with k's visibility flipped.
func (inst kindFilterT) toggled(k app.KindE) (out kindFilterT) {
	out = inst ^ (1 << k)
	return
}

// hidesAnything reports whether the filter is doing something — used to
// tell "your query matched nothing" apart from "your toggles hid it".
func (inst kindFilterT) hidesAnything() (ok bool) {
	ok = inst != 0
	return
}

// kindFilter derives the filter value from the per-kind toggle state. A
// host whose toggles were never initialised — the zero Inst, which tests
// construct — yields the inert filter, so "uninitialised" shows everything
// rather than hiding everything.
func (inst *Inst) kindFilter() (f kindFilterT) {
	if len(inst.kindShown) != len(app.AllKinds) {
		return
	}
	for i, k := range app.AllKinds {
		if !inst.kindShown[i] {
			f = f.toggled(k)
		}
	}
	return
}

// kindLabel is a toggle's user-facing label: the plural of the kind, since
// the toggle governs a set. Kept here rather than on app.KindE because it is
// presentation — the introspection column wants the singular wire form that
// KindE.String gives, and the two should not drift into one another.
func kindLabel(k app.KindE) (s string) {
	switch k {
	case app.KindApp:
		s = "Apps"
	case app.KindApplet:
		s = "Applets"
	case app.KindDemo:
		s = "Demos"
	default:
		s = k.String()
	}
	return
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
		density:    styletokens.DensityFromEnv(),
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

// manifestGroup is one launcher section: a topic and the manifests that
// carry it. Used by both the Apps menu and the empty-state pane so the two
// surfaces stay in sync.
//
// Sections are views, not homes (ADR-0158 §SD3): a manifest appears in every
// section whose topic it declares, so the groups' Manifests slices
// deliberately overlap. There is no catch-all bucket — Manifest.Validate
// refuses a windowed app with no topics, so the only manifests that fall out
// of every section are headless ones, which have no window to open anyway.
type manifestGroup struct {
	Topic     app.TopicT
	Manifests []app.Manifest
}

// groupByTopic sections manifests by topic, in app.AllTopics order, keeping
// only the sections `only` admits. Empty sections are omitted rather than
// rendered blank, so the browse view shows only topics the process actually
// has apps for. Within a section manifests
// sort by Display then Id, so two apps sharing a label still order stably.
//
// Ordering comes from the vocabulary rather than from a launcher-local list:
// the old preferredCategoryOrder had to cope with categories it had never
// heard of, which a closed vocabulary makes impossible.
func groupByTopic(manifests []app.Manifest, only topicFilterT) (groups []manifestGroup) {
	if len(manifests) == 0 {
		return
	}
	byTopic := make(map[app.TopicT][]app.Manifest, len(app.AllTopics))
	for _, m := range manifests {
		for _, t := range m.Topics {
			byTopic[t] = append(byTopic[t], m)
		}
	}
	groups = make([]manifestGroup, 0, len(app.AllTopics))
	for _, t := range app.AllTopics {
		// The chips drop whole sections, not just manifests. Filtering
		// only at the manifest level would leave a two-topic app visible
		// under its *unselected* topic as well, since a manifest that
		// passes the filter is still sectioned under everything it carries.
		if !only.shows(t) {
			continue
		}
		ms := byTopic[t]
		if len(ms) == 0 {
			continue
		}
		sortManifestsByDisplay(ms)
		groups = append(groups, manifestGroup{Topic: t, Manifests: ms})
	}
	return
}

// filterManifests returns the subset of manifests passing the launcher's
// whole filter state (ADR-0158 §SD6): provenance toggles, topic chips, and
// the query string. It is the one function both surfaces resolve through,
// so the Apps ▾ menu and the empty-state pane cannot disagree.
//
// An inert filter returns the input slice unchanged, so callers can treat
// "no filter" and "filter matches everything" identically. Kind and topic
// are applied even when the query is empty: the chips and toggles govern
// the sectioned browse view too, not just search hits.
//
// The query is a pattern battery (ADR-0164 §SD2, see windowhost_search.go),
// scored per manifest. Ordering therefore depends on whether one was typed:
// with a query the result is **ranked** (score descending, then Display,
// then Id); without one it follows the input, and the caller sections and
// sorts it. Both paths return a fresh slice unless the filter is inert.
func filterManifests(manifests []app.Manifest, f launcherFilter) (hits []app.Manifest) {
	if f.isInert() {
		hits = manifests
		return
	}
	b := launcherBattery(f.query)
	if b.IsZero() {
		hits = make([]app.Manifest, 0, len(manifests))
		for _, m := range manifests {
			if !f.admits(m) {
				continue
			}
			hits = append(hits, m)
		}
		return
	}
	scored := make([]manifestHit, 0, len(manifests))
	for _, m := range manifests {
		if !f.admits(m) {
			continue
		}
		score, ok := scoreManifest(m, &b)
		if !ok {
			continue
		}
		scored = append(scored, manifestHit{m: m, score: score})
	}
	sortManifestHits(scored)
	hits = make([]app.Manifest, len(scored))
	for i := range scored {
		hits[i] = scored[i].m
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
	c.Label("Pick one below, or use the Apps " + icons.PhCaretDown + " menu in the top bar.").Send()
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
		for range c.Horizontal().KeepIter() {
			// regexedit is the same box the help nav and play's snippet
			// filter use (ADR-0164 §SD4), in the token mode that lexes
			// each whitespace-separated pattern independently — so an
			// unclosed group in one token cannot mis-colour the next.
			searchId := ids.PrepareStr("empty-state-search")
			inst.searchHl.Prepare(searchId, inst.searchText, false, regexedit.ModeTokens).
				HintText("Search apps (regex, space = AND)").
				DesiredWidth(360).
				SendRespVal(&inst.searchText)
			if inst.searchText != "" {
				if c.Button(ids.PrepareStr("empty-state-search-clear"), c.Atoms().Text("×").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.searchText = ""
				}
			}
		}
	}
	inst.renderKindToggles(ids, "empty-state")
	inst.renderTopicChips(ids)
	query := strings.TrimSpace(inst.searchText)
	// The chips and toggles apply to the sectioned browse too, so filter
	// before sectioning rather than only on the search path (ADR-0158 §SD6).
	browse := launcherFilter{kinds: inst.kindFilter(), topics: inst.topicFilter}
	visible := filterManifests(manifests, browse)
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		if query == "" {
			groups := groupByTopic(visible, inst.topicFilter)
			if len(groups) == 0 {
				c.Label(inst.emptyResultHint()).Send()
				return
			}
			for gi, g := range groups {
				if gi > 0 {
					c.AddSpace(styletokens.GapSections(inst.density))
				}
				c.Label(g.Topic.String()).Send()
				c.Separator().Horizontal().Send()
				for _, m := range g.Manifests {
					inst.renderEmptyStateEntry(ids, g.Topic.String(), m, false)
				}
			}
			return
		}
		hits := filterManifests(visible, launcherFilter{query: query})
		renderSearchNotes(launcherBattery(query), len(hits), len(visible))
		if len(hits) == 0 {
			c.Label(inst.emptyResultHint()).Send()
			return
		}
		// Already ranked by filterManifests — score first, Display for
		// ties — so no re-sort here; sorting by Display would discard the
		// ranking the battery just produced.
		for _, m := range hits {
			inst.renderEmptyStateEntry(ids, "hits", m, true)
		}
	}
}

// renderSearchNotes draws the two lines that describe what the battery
// did: how selective it was, and whether any token silently stopped
// being a regex.
//
// The selectivity readout is a bare count, not the byte-share progress
// bar the help and snippet boxes carry. There the sections a battery
// selects vary hugely in size, so a count would misreport how much
// corpus a query actually admits; here every hit is one app-sized thing
// and the count *is* the honest number.
func renderSearchNotes(b search.Battery, nHits int, nTotal int) {
	for rt := range c.RichTextLabel(strconv.Itoa(nHits) + " of " + strconv.Itoa(nTotal) + " apps") {
		rt.Small().Weak()
	}
	for pi := range b.Patterns {
		if b.Patterns[pi].Literal {
			// Surfaced rather than silent (ADR-0164 §SD2): a half-typed
			// `quantile(` keeps matching as text, and the user is told
			// that is what happened.
			for rt := range c.RichTextLabel("some tokens are not valid regexps and match literally") {
				rt.Small().Weak()
			}
			break
		}
	}
}

// emptyResultHint distinguishes "nothing matched what you typed" from
// "your own toggles hid it". Without the second wording a user who has
// switched Demos off sees a bare "(no matches)" and reasonably concludes
// the app is gone rather than filtered — the failure mode that makes
// hide-toggles annoying elsewhere.
func (inst *Inst) emptyResultHint() (s string) {
	switch {
	case !inst.topicFilter.isInert() && inst.kindFilter().hidesAnything():
		s = "(no matches — a topic filter and hidden kinds are both active)"
	case !inst.topicFilter.isInert():
		s = "(no matches — filtered to selected topics)"
	case inst.kindFilter().hidesAnything():
		s = "(no matches — some kinds are hidden)"
	default:
		s = "(no matches)"
	}
	return
}

// renderTopicChips draws the topic filter (ADR-0158 §SD6): one selectable
// chip per vocabulary member, wrapping, with none selected meaning no
// restriction. Selecting a chip narrows the browse view to that section;
// selecting several unions them.
//
// Only the vocabulary members some registered app actually carries get a
// chip — a chip that can only ever yield an empty view is a dead control.
// SelectableLabel reports its click within the frame, so unlike the pane's
// kind checkboxes this needs no persistent mirror to write into.
func (inst *Inst) renderTopicChips(ids *c.WidgetIdStack) {
	manifests := inst.registry.AllManifests()
	present := make(map[app.TopicT]struct{}, len(app.AllTopics))
	for _, m := range manifests {
		for _, t := range m.Topics {
			present[t] = struct{}{}
		}
	}
	for range c.HorizontalWrapped().KeepIter() {
		c.Label("Topics:").Send()
		for i, t := range app.AllTopics {
			if _, has := present[t]; !has {
				continue
			}
			if c.SelectableLabel(ids.PrepareStr("topic-chip-"+t.String()),
				inst.topicFilter.selectedAt(i), t.String()).
				SendResp().HasPrimaryClicked() {
				inst.topicFilter = inst.topicFilter.toggledAt(i)
			}
		}
		if !inst.topicFilter.isInert() {
			// An explicit reset: with several chips on, clicking each one
			// off again is tedious, and "no chips" is not otherwise
			// reachable in one gesture.
			if c.Button(ids.PrepareStr("topic-chip-clear"), c.Atoms().Text(icons.PhX+" Clear").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.topicFilter = 0
			}
		}
	}
}

// ensureKindShown lazily initialises the toggle state to "everything
// shown". Lazy rather than done in NewInst so a zero Inst — and any future
// constructor — cannot start out hiding every app.
func (inst *Inst) ensureKindShown() {
	if len(inst.kindShown) == len(app.AllKinds) {
		return
	}
	inst.kindShown = make([]bool, len(app.AllKinds))
	for i := range inst.kindShown {
		inst.kindShown[i] = true
	}
}

// renderKindToggles draws the provenance filter (ADR-0158 §SD5): one
// checkbox per kind, all on by default. These exist because §SD3 retired
// "Applets" and "Demos" as browse sections — provenance is a filter over a
// subject-organised list, not a place an app lives — and without them that
// retirement would simply delete two views people use.
//
// scope keys the widget ids so the pane and the menu can both draw the same
// toggles without deriving the same ids.
func (inst *Inst) renderKindToggles(ids *c.WidgetIdStack, scope string) {
	inst.ensureKindShown()
	for range c.Horizontal().KeepIter() {
		c.Label("Show:").Send()
		for i, k := range app.AllKinds {
			c.Checkbox(ids.PrepareStr("kind-"+scope+"-"+k.String()), inst.kindShown[i], kindLabel(k)).
				SendRespVal(&inst.kindShown[i])
		}
	}
}

// renderEmptyStateEntry draws one app row inside the empty-state pane,
// mirroring renderAppsMenuEntry's contract. withCategory appends an
// em-dashed topic suffix — only meaningful in the flattened search-hit
// view, where the section header no longer carries that information.
//
// section keys the widget id alongside the app id. Under ADR-0158 §SD3 one
// manifest renders in every section whose topic it declares, so the app id
// alone no longer identifies a row: two sections would derive the same
// button id, and a duplicate id resolves to one shared response — clicking
// either row would open whichever the id stack landed on.
func (inst *Inst) renderEmptyStateEntry(ids *c.WidgetIdStack, section string, m app.Manifest, withTopics bool) {
	label := m.WindowTitle()
	if label == "" {
		label = string(m.Id)
	}
	if withTopics {
		if suffix := topicSuffix(m); suffix != "" {
			label = label + " — " + suffix
		}
	}
	btnId := ids.PrepareStr("empty-open-" + section + "-" + string(m.Id))
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

// RenderAppsMenu draws an "Apps ▾" menu listing every registered app,
// grouped into per-topic submenus in app.AllTopics order (ADR-0158 §SD3;
// an app carrying two topics appears under both). Clicking an entry calls
// Open(id) for that app; the new window appears on the next frame. Entries
// within a topic sort by Display.
//
// ids is the caller's stack; the menu uses derived ids for the per-entry
// buttons. Place inside a MenuBar (typically the carousel's top PanelTop),
// alongside File / Layout menus.
//
// The menu deliberately has no in-bar search field. egui's menu_button
// closes on any click outside a menu Button (TextEdit focus clicks
// included), and lifting the field into the menu bar added chrome clutter
// for a rarely-used affordance. Search lives in the empty-state pane
// instead (see renderEmptyState), backed by the same inst.searchText
// buffer so future surfaces hook into the same filter state.
//
// The kind toggles are a different matter and do appear here, as a "Show"
// submenu of plain Buttons rather than the pane's checkboxes — same
// constraint, different resolution. The menu is the *only* launcher
// surface once a window is open, so a filter reachable solely from the
// pane could not be undone without closing everything; and a Button is
// exactly the widget the menu tolerates. The cost is that the menu closes
// on the click, so changing two toggles means opening it twice. That is
// acceptable for a mode switch, and it is why the label reports state
// ("✔ Demos") rather than relying on the user remembering it.
func (inst *Inst) RenderAppsMenu(ids *c.WidgetIdStack) {
	for range c.MenuButton(c.Atoms().Text("Apps").Keep()).KeepIter() {
		manifests := inst.registry.AllManifests()
		if len(manifests) == 0 {
			c.Label("(no apps registered)").Send()
			return
		}
		inst.renderKindMenu(ids)
		c.Separator().Horizontal().Send()
		// The menu carries no topic chips: its per-topic submenus already
		// *are* the topic axis, and a chip row that hid submenus would be
		// two controls for one thing. It still honours a selection made in
		// the pane, so the two surfaces agree about what is on screen.
		visible := filterManifests(manifests, launcherFilter{kinds: inst.kindFilter(), topics: inst.topicFilter})
		groups := groupByTopic(visible, inst.topicFilter)
		if len(groups) == 0 {
			c.Label(inst.emptyResultHint()).Send()
			return
		}
		for _, g := range groups {
			for range c.MenuButton(c.Atoms().Text(g.Topic.String()).Keep()).KeepIter() {
				for _, m := range g.Manifests {
					inst.renderAppsMenuEntry(ids, g.Topic.String(), m, false)
				}
			}
		}
	}
}

// renderKindMenu draws the provenance filter as a "Show" submenu of
// state-reporting Buttons — the menu-side counterpart of the pane's
// renderKindToggles, sharing inst.kindFilter so the two never disagree.
// See RenderAppsMenu for why this is Buttons and not checkboxes.
func (inst *Inst) renderKindMenu(ids *c.WidgetIdStack) {
	inst.ensureKindShown()
	for range c.MenuButton(c.Atoms().Text("Show").Keep()).KeepIter() {
		for i, k := range app.AllKinds {
			mark := icons.PhCheck + " "
			if !inst.kindShown[i] {
				// An en-space, not a plain space: it advances the same
				// width as the check mark, so the labels stay aligned
				// whether or not their kind is shown.
				mark = "\u2002 "
			}
			btnId := ids.PrepareStr("kindmenu-" + k.String())
			if c.Button(btnId, c.Atoms().Text(mark+kindLabel(k)).Keep()).
				SendResp().HasPrimaryClicked() {
				// A Button response is read within the frame, unlike the
				// pane checkbox's deferred write, so this flip is direct.
				inst.kindShown[i] = !inst.kindShown[i]
			}
		}
	}
}

// renderAppsMenuEntry draws one menu entry for the given manifest.
// Factored out of RenderAppsMenu so the per-entry click dispatch is
// reusable across topic submenus and the flat search-hit list. When
// withTopics is true the manifest's topics are appended to the button
// label as an em-dashed suffix — only meaningful in the flattened search
// view, where the submenu chrome no longer carries that information.
//
// section keys the widget id alongside the app id, for the ADR-0158 §SD3
// reason spelled out on renderEmptyStateEntry: one manifest renders under
// every topic it declares, so the app id alone no longer identifies a row.
func (inst *Inst) renderAppsMenuEntry(ids *c.WidgetIdStack, section string, m app.Manifest, withTopics bool) {
	label := m.WindowTitle()
	if label == "" {
		label = string(m.Id)
	}
	if withTopics {
		if suffix := topicSuffix(m); suffix != "" {
			label = label + " — " + suffix
		}
	}
	btnId := ids.PrepareStr("open-" + section + "-" + string(m.Id))
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

// topicSuffix renders a manifest's topics as a compact label suffix for the
// flattened search views, where no section header says what an entry is
// about. Joined with a middot rather than a comma: the list is short, and a
// comma reads as part of the app name when it follows one.
func topicSuffix(m app.Manifest) (s string) {
	if len(m.Topics) == 0 {
		return
	}
	parts := make([]string, 0, len(m.Topics))
	for _, t := range m.Topics {
		parts = append(parts, t.String())
	}
	s = strings.Join(parts, " · ")
	return
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
