package windowhost

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// windowhost_workstatelet.go is the host half of ADR-0369 (proposed):
// opening the windows a workstatelet document lists, in its order and
// layout, and writing a document from the windows that are open.

// SetWorkstatelets attaches the catalog a workstatelet's id is opened from.
// Without one, an open of such an id is refused with that reason.
func (inst *Inst) SetWorkstatelets(cat *workstatelet.Catalog) {
	inst.mu.Lock()
	inst.workstatelets = cat
	inst.mu.Unlock()
}

// WorkstateletSkip is one window of a document the host did not open, and
// why (ADR-0369 §SD4).
type WorkstateletSkip struct {
	// Index is the window's position in the document, from 0.
	Index  int
	Window workstatelet.Window
	Err    error
}

// OpenWorkstatelet opens the windows of the document minted under id
// (ADR-0369 §SD4). It closes nothing. The document's windows join the
// desktop in document order with the first as the main and in front; a
// layout the document names is set, with the parameters it gives.
//
// Each window passes OpenWithConfig like any open. One that is refused is
// returned in skipped, logged, and its siblings still open. err is set when
// the id is not a workstatelet this process may open, when the layout is not
// one the host has, and when no window opened.
//
// Call it on the render goroutine or before the first Frame: the order and
// the float set it writes are synced against the windows of the frame in
// progress.
func (inst *Inst) OpenWorkstatelet(id app.AppIdT) (opened []WindowKeyT, skipped []WorkstateletSkip, err error) {
	m, ok := inst.registry.LookupManifest(id)
	if !ok {
		err = eb.Build().Str("id", string(id)).Errorf("workstatelet is not registered")
		return
	}
	if m.Kind != app.KindWorkstatelet {
		err = eb.Build().Str("id", string(id)).Str("kind", m.Kind.String()).Errorf("app is not a workstatelet")
		return
	}
	// The launch limit names workstatelets like any entry (ADR-0272); each
	// window then meets it again at its own open.
	if !inst.registry.Launchable(id) {
		err = eb.Build().Str("id", string(id)).
			Errorf("workstatelet is not launchable in this process (KEELSON_LAUNCHABLE_APPS_WHERE)")
		return
	}
	inst.mu.Lock()
	cat := inst.workstatelets
	inst.mu.Unlock()
	if cat == nil {
		err = eb.Build().Str("id", string(id)).Errorf("no workstatelet catalog is attached to this host")
		return
	}
	doc, ok := cat.Lookup(id)
	if !ok {
		err = eb.Build().Str("id", string(id)).Errorf("no document is minted under this id")
		return
	}
	layout := LayoutFloating
	if doc.Desktop.Layout != "" {
		layout, ok = ParseLayout(doc.Desktop.Layout)
		if !ok {
			err = eb.Build().Str("id", string(id)).Str("layout", doc.Desktop.Layout).
				Strs("layouts", LayoutIdents()).Errorf("document names a layout this host does not have")
			return
		}
	}

	// Last window first: on a floating desktop a later open stacks in
	// front, so the document's first window ends up on top.
	keys := make([]WindowKeyT, len(doc.Windows))
	for i := len(doc.Windows) - 1; i >= 0; i-- {
		w := doc.Windows[i]
		key, target, resolved, oerr := inst.openWorkstateletWindow(w)
		if oerr != nil {
			inst.logger.Warn().Err(oerr).Str("workstatelet", string(id)).Int("window", i+1).
				Str("app", w.App).Str("kind", w.Kind).
				Msg("windowhost: a workstatelet's window did not open; skipped")
			skipped = append(skipped, WorkstateletSkip{Index: i, Window: w, Err: oerr})
			continue
		}
		keys[i] = key
		inst.writeWorkstateletLaunch(id, target, key, w, resolved)
	}
	slices.Reverse(skipped)
	for _, k := range keys {
		if k != 0 {
			opened = append(opened, k)
		}
	}
	if len(opened) == 0 {
		err = eb.Build().Str("id", string(id)).Int("windows", len(doc.Windows)).
			Errorf("no window of the workstatelet opened: %w", skipped[0].Err)
		return
	}

	inst.mu.Lock()
	rest := make([]WindowKeyT, 0, len(inst.tileOrder))
	for _, k := range inst.tileOrder {
		if !slices.Contains(opened, k) {
			rest = append(rest, k)
		}
	}
	inst.tileOrder = append(slices.Clone(opened), rest...)
	inst.holdPinnedLocked()
	for i, k := range keys {
		if k != 0 && doc.Windows[i].Floating {
			inst.setFloatingLocked(k, true)
		}
	}
	if doc.Desktop.Layout != "" {
		inst.setLayoutLocked(layout)
		p := inst.layoutParams
		if doc.Desktop.MainRatio != 0 {
			p.MainRatio = doc.Desktop.MainRatio
		}
		if doc.Desktop.MainCount != 0 {
			p.MainCount = int(doc.Desktop.MainCount)
		}
		inst.layoutParams = p.clamped()
	}
	inst.tileDirty = true
	inst.pendingRaise = opened[0]
	inst.mu.Unlock()
	return
}

// openWorkstateletWindow opens one window of a document. resolved is true
// when the window named a kind and no app.
func (inst *Inst) openWorkstateletWindow(w workstatelet.Window) (key WindowKeyT, target app.AppIdT, resolved bool, err error) {
	if w.App != "" {
		target, err = inst.resolveAppName(w.App)
	} else {
		resolved = true
		target, err = inst.ResolveLaunchKind(w.Kind)
	}
	if err != nil {
		return
	}
	if len(w.Config) == 0 {
		// As a click on the app would: OpenWithConfig restores the app's
		// default workingset where it keeps one.
		key, err = inst.OpenWithConfig(target, "", nil)
		return
	}
	key, err = inst.OpenWithConfig(target, w.Kind, w.Config)
	return
}

// resolveAppName is the app a document's `app` names: an id, or a subject
// alias, which the registry keeps unique.
func (inst *Inst) resolveAppName(name string) (id app.AppIdT, err error) {
	if _, ok := inst.registry.LookupManifest(app.AppIdT(name)); ok {
		id = app.AppIdT(name)
		return
	}
	for _, m := range inst.registry.Manifests() {
		if m.Id.SubjectAlias() == name {
			id = m.Id
			return
		}
	}
	err = eb.Build().Str("app", name).Errorf("no registered app has this id or alias")
	return
}

// writeWorkstateletLaunch records one window a workstatelet opened as a
// launch whose caller is the workstatelet's id (ADR-0369 §SD6). Best-effort,
// like every audit write.
func (inst *Inst) writeWorkstateletLaunch(caller app.AppIdT, target app.AppIdT, key WindowKeyT, w workstatelet.Window, resolved bool) {
	inst.mu.Lock()
	runId, facts := inst.runId, inst.facts
	inst.mu.Unlock()
	if facts == nil {
		return
	}
	row := factsstore.LaunchRow{RunId: runId, CallerAppId: caller, TargetAppId: target, TileKey: uint64(key), ResolvedFromKind: resolved}
	if len(w.Config) > 0 {
		row.ConfigKind, row.Config = w.Kind, w.Config
	}
	if _, wErr := facts.WriteLaunch(row); wErr != nil {
		inst.logger.Warn().Err(wErr).Str("workstatelet", string(caller)).Uint64("windowKey", uint64(key)).
			Msg("windowhost: write workstatelet launch fact failed")
	}
}

// RequestWorkstateletCapture asks the next Frame for a document of the open
// windows (CaptureWorkstatelet) and hands it to done on the render
// goroutine. done must not block. Safe off the render thread.
func (inst *Inst) RequestWorkstateletCapture(done func(doc workstatelet.Doc, err error)) {
	if done == nil {
		return
	}
	inst.mu.Lock()
	inst.pendingCaptures = append(inst.pendingCaptures, done)
	inst.mu.Unlock()
}

// stepCaptures answers the capture requests queued since the last Frame.
// Render goroutine only; it runs before the frame's reap so a window closing
// this frame is still composed.
func (inst *Inst) stepCaptures() {
	inst.mu.Lock()
	pending := inst.pendingCaptures
	inst.pendingCaptures = nil
	inst.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	doc, err := inst.CaptureWorkstatelet()
	for _, done := range pending {
		done(doc, err)
	}
}

// CaptureWorkstatelet writes a document of the open windows (ADR-0369
// §SD7): the windows in the desktop's order, each with the config its app
// composes as a workingset, else the launch config it was opened with, else
// none; the float set; and the layout and its parameters when the desktop is
// tiled. Windows of shell apps are left out.
//
// The document has its windows, desktop, topics and a summary; the caller
// names it (Title, Slug) before it is composed or stored. err is set when no
// window is left to write.
//
// Render goroutine only: it calls into the apps, as a close does.
func (inst *Inst) CaptureWorkstatelet() (doc workstatelet.Doc, err error) {
	inst.mu.Lock()
	windows := make([]*window, 0, len(inst.windows))
	for _, w := range inst.windows {
		if !w.closeReq && !w.manifest.Shell {
			windows = append(windows, w)
		}
	}
	order := slices.Clone(inst.tileOrder)
	floating := make(map[WindowKeyT]bool, len(inst.floating))
	for k, v := range inst.floating {
		floating[k] = v
	}
	layout, params := inst.layout, inst.layoutParams
	inst.mu.Unlock()
	if len(windows) == 0 {
		err = eb.Build().Errorf("no window is open to capture")
		return
	}
	// The desktop's order, main first. A window opened since the order was
	// last synced is not in it yet; it sorts in front, newest first, which
	// is where the sync will put it.
	rank := func(w *window) (r int) {
		if i := slices.Index(order, w.key); i >= 0 {
			return i
		}
		return -1
	}
	slices.SortStableFunc(windows, func(a, b *window) int {
		ra, rb := rank(a), rank(b)
		if ra != rb {
			return ra - rb
		}
		return int(b.key) - int(a.key)
	})

	var displays []string
	topics := make(map[app.TopicT]bool, 4)
	for _, w := range windows {
		out := workstatelet.Window{App: w.manifest.Id.SubjectAlias(), Floating: floating[w.key]}
		if cfg := inst.captureConfig(w); len(cfg) > 0 {
			out.Kind, out.Config = w.manifest.LaunchKind, cfg
		}
		doc.Windows = append(doc.Windows, out)
		if !slices.Contains(displays, w.manifest.Display) {
			displays = append(displays, w.manifest.Display)
		}
		for _, t := range w.manifest.Topics {
			topics[t] = true
		}
	}
	for _, t := range app.AllTopics {
		if topics[t] {
			doc.Topics = append(doc.Topics, t)
		}
	}
	doc.Summary = "Opens " + strings.Join(displays, ", ")
	if layout.Tiled() {
		doc.Desktop = workstatelet.Desktop{Layout: layout.Ident()}
		if layout == LayoutMainStack {
			doc.Desktop.MainRatio, doc.Desktop.MainCount = params.MainRatio, int32(params.MainCount)
		}
	}
	return
}

// captureConfig is the launch config a capture writes for w: what the app
// composes as its workingset, whether or not the window is dirty — the
// person asked for this capture, which is the intent a close has to infer —
// else the config the window was opened with. Nil when the window has
// neither, or when what it has would not pass the open boundary.
func (inst *Inst) captureConfig(w *window) (cfg []byte) {
	kind := w.manifest.LaunchKind
	if kind == "" {
		return
	}
	usable := func(b []byte, what string) bool {
		if len(b) == 0 {
			return false
		}
		if len(b) > maxLaunchConfigBytes {
			inst.logger.Warn().Str("id", string(w.manifest.Id)).Str("source", what).
				Int("len", len(b)).Int("max", maxLaunchConfigBytes).
				Msg("windowhost: capture: config exceeds the size cap; left out")
			return false
		}
		if cErr := kindcheck.Check(kind, b); cErr != nil {
			inst.logger.Warn().Err(cErr).Str("id", string(w.manifest.Id)).Str("source", what).
				Msg("windowhost: capture: config refused by kindcheck; left out")
			return false
		}
		return true
	}
	// A composer is asked only where a close would ask it: a mounted
	// instance that this window alone holds.
	if w.manifest.Workingset && w.mount != nil && w.mount.mounted && w.mount.mountErr == nil && w.mount.refs == 1 {
		if composer, ok := w.appInst.(app.WorkingsetComposerI); ok {
			composed, _, cErr := composer.ComposeWorkingset()
			if cErr != nil {
				inst.logger.Warn().Err(cErr).Str("id", string(w.manifest.Id)).
					Msg("windowhost: capture: compose workingset failed; falling back to the launch config")
			} else if usable(composed, "workingset") {
				cfg = append([]byte(nil), composed...)
				return
			}
		}
	}
	if launched := w.mountCtx.LaunchConfig(); usable(launched, "launch") {
		cfg = append([]byte(nil), launched...)
	}
	return
}
