// Package capdemo is the M2 capability-broker showcase app. One window,
// four sections: a fs.dialog.read round-trip (pick a file, get a
// handle, fetch contents), a runtime.persist.{ownAlias}.scratchpad
// round-trip (save / load / delete a TextEdit's contents), an
// fs.dialog.watch round-trip (pick a folder, stream change events on
// fs.handle.{uuid}.event), and a clipboard section (a labelled copy
// through the broker). Demonstrates that an app declaring the right Caps
// gets working Bus() and Storage() through MountContextI without any
// direct syscalls.
//
// Lifecycle: Mount captures the BusI and StorageI from the context and
// the instance's lifetime scope. Every verb runs as one piece of work on
// that scope because bus.Request blocks until the broker (or the picker)
// resolves; results land in fields guarded by `mu` and are drawn on the
// next Frame. Unmount releases the watch subscription; the host's drain
// covers the goroutines (ADR-0322).
package capdemo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// scratchpadKey is the persist key the demo writes / reads on every
// Save / Load click. Single NATS token (no dots) so the wire subject
// stays valid (the service rejects dotted keys).
const scratchpadKey = "scratchpad"

// previewLimit caps the file-preview rendering so a multi-MB pick
// doesn't drown the UI. Bytes past this are summarised.
const previewLimit = 1024

// watchEventLimit caps the rolling event buffer that renderWatchSection
// displays. Older events fall off the front as new ones arrive.
const watchEventLimit = 50

// watchEventDisplay is the number of trailing events the UI renders per
// frame. Keeps the section vertically bounded regardless of cadence.
const watchEventDisplay = 12

// clipboardSource names the surface a copy leaves from, on the payload's
// Source so the broker's record and the journal's `copy` entry say where
// the text came from (ADR-0317 §SD1).
const clipboardSource = "capdemo.clipboard-doc"

// noBusRemedy is the one sentence every verb shows when the mount context
// wired no bus: the demo needs the app runtime, which the windowed host
// provides and a bare test context does not.
const noBusRemedy = "no bus wired — the broker needs the app runtime; open the demo from the app center under the windowed host"

// clipboardDoc is the markdown rendered in the clipboard section. Parsed
// once at package load (markdown.Parse is the retain-once / render-many
// shape) and rendered every frame via Doc.RenderActions, which places a
// small "Copy" button on each fenced block; a click is consumed from the
// returned iter.Seq and routed to a labelled clipboardbroker.Write. The
// Go block teaches that shape, so what the person copies is the idiom.
var clipboardDoc = markdown.Parse([]byte("" +
	"`clipboard.write` copies text to the viewport clipboard through the broker: a\n" +
	"labelled `Payload` names what left and from where, and the record that comes\n" +
	"back says how it left — as plaintext or by handle. Click the Copy button above\n" +
	"either block below.\n\n" +
	"```go\n" +
	"// the consumer wires the code-block button to a labelled copy, off the frame:\n" +
	"res := markdown.Render(markdown.Input{Ids: ids, Doc: doc, ActionLabels: []string{\"Copy\"}})\n" +
	"for _, act := range res.Actions {\n" +
	"\ttext := act.Text\n" +
	"\tscope.Go(func(context.Context) {\n" +
	"\t\trec, err := clipboardbroker.Write(bus, clipboardbroker.Payload{Text: text, Source: \"myapp.doc\"})\n" +
	"\t\t// hand rec.Verdict() and rec.Handle to the next frame\n" +
	"\t})\n" +
	"}\n" +
	"```\n\n" +
	"```\n" +
	"plain verbatim block — copies exactly these bytes, no highlighting\n" +
	"```\n"))

// App is the per-window capdemo instance. Mount captures bus, storage and
// the lifetime scope from the MountContextI; Frame applies what the verbs
// handed over and renders the sections; Unmount releases the watch
// subscription.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026
	// §SD9, windowhost.renderWindowBody), so every widget id the app
	// derives is unique across all concurrently open instances — even
	// when two apps share a label string ("topbar", …). Captured from
	// ctx.Ids() in Mount; the app must NOT Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	bus     app.BusI
	storage app.StorageI

	// scope is the instance's lifetime scope (W5): every verb goroutine
	// runs on it so the host's drain waits for it. nil on a host without
	// one (a bare test context), where spawn falls back to a plain
	// goroutine.
	scope *lifetime.Scope

	mu sync.Mutex

	// fs.dialog.read state.
	pickInFlight     bool
	lastHandlePrefix string
	previewBytes     []byte
	previewTotal     int
	fileErr          string

	// runtime.persist state — the TextEdit binds scratchpad on the render
	// goroutine and nothing else writes it: a Load lands its value in
	// pendingScratchpad under mu, and Frame moves it across before the
	// widget binds (T5). The last operation's status and its tone land
	// in persistStatus / persistTone.
	scratchpad        string
	pendingScratchpad *string
	persistStatus     string
	persistTone       badge.ToneE

	// clipboard state: the broker's answer to the last copy.
	clipboardNote string
	clipboardTone badge.ToneE

	// fs.dialog.watch state. watchUsePoller and watchRecursive are
	// written directly by the render goroutine (via Checkbox
	// SendRespVal); the picker goroutine reads them once on pick. All
	// other fields are mutated under inst.mu from the picker / event
	// handler goroutines.
	watchUsePoller    bool
	watchRecursive    bool
	watchHandlePrefix string
	watchBackend      string
	watchActive       bool
	watchEvents       []fsbroker.WatchEvent
	watchUnsubscribe  func()
	watchErr          string
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:     c.NewWidgetIdStack(),
		density: styletokens.ActiveDensity(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount captures the per-app BusI and StorageI handles and the lifetime
// scope. The values are dormant on M1 hosts (NoopBus / NoopStorage) —
// every call errors with a documented "not available" message; on M2
// hosts (carousel Phase A+B+C wiring) they are real inprocbus.Client /
// persist.Client. Either way, Mount itself is infallible.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	inst.storage = ctx.Storage()
	inst.scope = app.LifetimeOf(ctx)
	return
}

// Unmount releases the watch subscription so no event lands after the
// window is gone. The handle itself is the broker's to revoke: it goes
// with the bus client, which the host closes after the drain
// (ADR-0322), and the verb goroutines are on the scope that drain waits
// for.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.mu.Lock()
	unsub := inst.watchUnsubscribe
	inst.watchUnsubscribe = nil
	inst.watchActive = false
	inst.mu.Unlock()
	if unsub != nil {
		unsub()
	}
	return
}

// spawn runs f as one piece of the instance's work (W5): on the lifetime
// scope when the host gave one, so the drain waits for it and the Running
// tab lists it; a plain goroutine on a bare test context. A scope that
// already retired starts nothing — the window is closing.
func (inst *App) spawn(f func()) {
	if inst.scope != nil {
		inst.scope.Go(func(context.Context) { f() })
		return
	}
	go f()
}

// Frame renders the demo sections. The host has already pre-pushed a
// window-unique salt onto inst.ids via c.IdScope (windowhost.
// renderWindowBody, ADR-0026 §SD9), so widget ids derived from inst.ids
// are unique across all concurrently open instances — the app must not
// Reset() the stack or wrap the body in its own instance salt (doing so
// discards the host salt and collides with sibling apps that share a
// label string).
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.takePending()
	inst.renderApp()
	return
}

// takePending moves what a verb handed over into the fields the widgets
// bind, before any widget runs this frame.
func (inst *App) takePending() {
	inst.mu.Lock()
	if p := inst.pendingScratchpad; p != nil {
		inst.scratchpad = *p
		inst.pendingScratchpad = nil
	}
	inst.mu.Unlock()
}

func (inst *App) renderApp() {
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			inst.renderFsSection()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			inst.renderPersistSection()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			inst.renderWatchSection()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			inst.renderClipboardSection()
		}
	}
}

// renderNote draws one outcome as a toned badge (L2): empty draws nothing.
func (inst *App) renderNote(key string, text string, tone badge.ToneE) {
	if text == "" {
		return
	}
	badge.New(inst.ids.PrepareStr(key), text).Tone(tone).Variant(badge.VariantSoft).Send()
}

// renderClipboardSection demonstrates the clipboard.write capability via
// the markdown widget's code-block action buttons (ADR-0026 Update
// 2026-05-30). Doc.RenderActions draws a small "Copy" button on each
// fenced block and yields the clicked blocks; each click hands a labelled
// payload to clipboardbroker.Write off the frame goroutine — Write blocks
// until the broker acks, and the frame thread must not block (same idiom
// as runPick). The record the broker returns is shown on the next frame.
func (inst *App) renderClipboardSection() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-clipboard"),
		c.WidgetText().Text("clipboard.write — copy code blocks to the clipboard").Keep()).
		DefaultOpen(true).KeepIter() {
		res := markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "clipboard-doc", Doc: clipboardDoc, ActionLabels: []string{"Copy"}})
		for _, act := range res.Actions {
			text := act.Text
			inst.spawn(func() { inst.runCopy(text) })
		}
		inst.mu.Lock()
		note, tone := inst.clipboardNote, inst.clipboardTone
		inst.mu.Unlock()
		inst.renderNote("clipboard-note", note, tone)
	}
}

// runCopy is one labelled copy (D5): the payload names its source, and the
// broker's record says how the text left.
func (inst *App) runCopy(text string) {
	if inst.bus == nil {
		inst.setClipboardNote(noBusRemedy, badge.ToneWarning)
		return
	}
	rec, err := clipboardbroker.Write(inst.bus, clipboardbroker.Payload{Text: text, Source: clipboardSource})
	if err != nil {
		inst.setClipboardNote("copy: "+err.Error(), badge.ToneError)
		return
	}
	note := fmt.Sprintf("copied %d bytes as %s", rec.Bytes, rec.Verdict())
	if rec.Handle != "" {
		note += " (" + rec.Handle + "): paste it inside boxer"
	}
	inst.setClipboardNote(note, badge.ToneSuccess)
}

func (inst *App) renderFsSection() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-fs"),
		c.WidgetText().Text("fs.dialog.read — Powerbox file pick").Keep()).
		DefaultOpen(true).KeepIter() {
		inst.mu.Lock()
		busy := inst.pickInFlight
		handle := inst.lastHandlePrefix
		preview := inst.previewBytes
		previewTotal := inst.previewTotal
		fileErr := inst.fileErr
		inst.mu.Unlock()

		for range c.Horizontal().KeepIter() {
			if busy {
				inst.renderNote("pick-busy", "Picker open — resolve or cancel it in the overlay", badge.ToneInfo)
			} else {
				if c.Button(inst.ids.PrepareStr("pick"),
					c.Atoms().Text("Pick a file…").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.spawn(inst.runPick)
				}
				if handle != "" {
					if c.Button(inst.ids.PrepareStr("close"),
						c.Atoms().Text("Close handle").Keep()).
						SendResp().HasPrimaryClicked() {
						inst.spawn(func() { inst.runClose(handle) })
					}
				}
			}
		}
		if handle != "" {
			c.Label("Granted handle subject: " + handle).Send()
		}
		inst.renderNote("file-err", fileErr, badge.ToneError)
		if preview != nil {
			c.Label(fmt.Sprintf("Bytes read: %d (showing first %d)",
				previewTotal, len(preview))).Send()
			c.Label(string(preview)).Send()
		}
	}
}

func (inst *App) renderPersistSection() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-persist"),
		c.WidgetText().Text("runtime.persist.scratchpad — Set / Get / Delete").Keep()).
		DefaultOpen(true).KeepIter() {
		inst.mu.Lock()
		status, tone := inst.persistStatus, inst.persistTone
		inst.mu.Unlock()

		// scratchpad is the render goroutine's: a Load lands in
		// pendingScratchpad and Frame moved it across before this widget.
		_ = c.TextEdit(inst.ids.PrepareStr("scratchpad"), inst.scratchpad, true).
			CodeEditor().
			DesiredRows(3).
			HintText("type something to save…").
			SendRespVal(&inst.scratchpad)

		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("save"),
				c.Atoms().Text("Save").Keep()).
				SendResp().HasPrimaryClicked() {
				value := inst.scratchpad
				inst.spawn(func() { inst.runPersistSet(value) })
			}
			if c.Button(inst.ids.PrepareStr("load"),
				c.Atoms().Text("Load").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.spawn(inst.runPersistGet)
			}
			if c.Button(inst.ids.PrepareStr("delete"),
				c.Atoms().Text("Delete").Keep()).
				Kind(c.ButtonKindDanger).
				SendResp().HasPrimaryClicked() {
				inst.spawn(inst.runPersistDelete)
			}
		}
		inst.renderNote("persist-status", status, tone)
	}
}

// runPick is the goroutine driving an fs.dialog.read round trip:
// publish → broker queues → picker overlay resolves → reply lands.
// Then a second Request fetches the file contents through the
// granted handle subject. State updates happen under inst.mu so the
// Frame goroutine sees a consistent snapshot.
func (inst *App) runPick() {
	if inst.bus == nil {
		inst.setFileErr(noBusRemedy)
		return
	}
	inst.setBusy(true)
	defer inst.setBusy(false)

	rawReply, rerr := inst.bus.RequestWithTimeout(fsbroker.SubjectDialogRead, nil, fsbroker.DialogTimeout)
	if rerr != nil {
		inst.setFileErr("fs.dialog.read: " + rerr.Error())
		return
	}
	dr, jerr := fsbroker.UnmarshalDialogReply(rawReply)
	if jerr != nil {
		inst.setFileErr("dialog reply parse: " + jerr.Error())
		return
	}
	if !dr.Granted {
		inst.setFileErr("dialog denied: " + dr.Reason)
		return
	}

	readSubj := dr.HandleSubjectPrefix + ".read"
	body, rerr := inst.bus.RequestWithTimeout(readSubj, nil, fsbroker.HandleOpTimeout)
	if rerr != nil {
		inst.mu.Lock()
		inst.lastHandlePrefix = dr.HandleSubjectPrefix
		inst.fileErr = "handle read: " + rerr.Error()
		inst.previewBytes = nil
		inst.previewTotal = 0
		inst.mu.Unlock()
		return
	}
	inst.mu.Lock()
	inst.lastHandlePrefix = dr.HandleSubjectPrefix
	inst.previewTotal = len(body)
	if len(body) > previewLimit {
		inst.previewBytes = append([]byte(nil), body[:previewLimit]...)
	} else {
		inst.previewBytes = append([]byte(nil), body...)
	}
	inst.fileErr = ""
	inst.mu.Unlock()
}

// runClose releases the granted handle on the broker side and clears
// the local preview state. The handle subject becomes invalid on
// reply (further reads error).
func (inst *App) runClose(handlePrefix string) {
	if inst.bus == nil {
		return
	}
	_, _ = inst.bus.RequestWithTimeout(handlePrefix+".close", nil, fsbroker.HandleOpTimeout)
	inst.mu.Lock()
	inst.lastHandlePrefix = ""
	inst.previewBytes = nil
	inst.previewTotal = 0
	inst.fileErr = ""
	inst.mu.Unlock()
}

func (inst *App) runPersistSet(value string) {
	if inst.storage == nil {
		inst.setPersistStatus("no storage wired — persist needs the app runtime", badge.ToneWarning)
		return
	}
	err := inst.storage.Set(scratchpadKey, []byte(value))
	if err != nil {
		inst.setPersistStatus("set failed: "+err.Error(), badge.ToneError)
		return
	}
	inst.setPersistStatus(fmt.Sprintf("saved %d bytes at %s",
		len(value), time.Now().Format("15:04:05")), badge.ToneSuccess)
}

// runPersistGet reads the key and hands the value to the next frame: the
// TextEdit binds scratchpad on the render goroutine, so this goroutine
// never writes it.
func (inst *App) runPersistGet() {
	if inst.storage == nil {
		inst.setPersistStatus("no storage wired — persist needs the app runtime", badge.ToneWarning)
		return
	}
	value, found, err := inst.storage.Get(scratchpadKey)
	if err != nil {
		inst.setPersistStatus("get failed: "+err.Error(), badge.ToneError)
		return
	}
	if !found {
		inst.setPersistStatus("get: key absent", badge.ToneWarning)
		return
	}
	loaded := string(value)
	inst.mu.Lock()
	inst.pendingScratchpad = &loaded
	inst.persistStatus = fmt.Sprintf("loaded %d bytes", len(value))
	inst.persistTone = badge.ToneSuccess
	inst.mu.Unlock()
}

func (inst *App) runPersistDelete() {
	if inst.storage == nil {
		inst.setPersistStatus("no storage wired — persist needs the app runtime", badge.ToneWarning)
		return
	}
	err := inst.storage.Delete(scratchpadKey)
	if err != nil {
		inst.setPersistStatus("delete failed: "+err.Error(), badge.ToneError)
		return
	}
	inst.setPersistStatus("deleted", badge.ToneSuccess)
}

func (inst *App) setBusy(b bool) {
	inst.mu.Lock()
	inst.pickInFlight = b
	inst.mu.Unlock()
}

func (inst *App) setFileErr(s string) {
	inst.mu.Lock()
	inst.fileErr = s
	inst.previewBytes = nil
	inst.previewTotal = 0
	inst.mu.Unlock()
}

func (inst *App) setPersistStatus(s string, tone badge.ToneE) {
	inst.mu.Lock()
	inst.persistStatus = s
	inst.persistTone = tone
	inst.mu.Unlock()
}

func (inst *App) setClipboardNote(s string, tone badge.ToneE) {
	inst.mu.Lock()
	inst.clipboardNote = s
	inst.clipboardTone = tone
	inst.mu.Unlock()
}

func (inst *App) renderWatchSection() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-watch"),
		c.WidgetText().Text("fs.dialog.watch — folder change notifications").Keep()).
		DefaultOpen(true).KeepIter() {
		inst.mu.Lock()
		prefix := inst.watchHandlePrefix
		backend := inst.watchBackend
		active := inst.watchActive
		evCount := len(inst.watchEvents)
		// Snapshot the trailing window for stable rendering.
		start := 0
		if evCount > watchEventDisplay {
			start = evCount - watchEventDisplay
		}
		display := append([]fsbroker.WatchEvent(nil), inst.watchEvents[start:]...)
		watchErr := inst.watchErr
		usePoller := inst.watchUsePoller
		recursive := inst.watchRecursive
		inst.mu.Unlock()

		for range c.Horizontal().KeepIter() {
			_ = c.Checkbox(inst.ids.PrepareStr("usepoller"), usePoller, "Force poller backend").
				SendRespVal(&inst.watchUsePoller)
			_ = c.Checkbox(inst.ids.PrepareStr("recursive"), recursive, "Recursive (subtree)").
				SendRespVal(&inst.watchRecursive)
		}

		for range c.Horizontal().KeepIter() {
			if !active {
				if c.Button(inst.ids.PrepareStr("watchpick"),
					c.Atoms().Text("Pick folder to watch…").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.spawn(inst.runWatchPick)
				}
				if prefix != "" {
					if c.Button(inst.ids.PrepareStr("watchclose"),
						c.Atoms().Text("Close handle").Keep()).
						SendResp().HasPrimaryClicked() {
						inst.spawn(inst.runWatchClose)
					}
				}
			} else {
				if c.Button(inst.ids.PrepareStr("watchstop"),
					c.Atoms().Text("Stop watching").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.spawn(inst.runWatchStop)
				}
				if c.Button(inst.ids.PrepareStr("watchclose-active"),
					c.Atoms().Text("Close handle").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.spawn(inst.runWatchClose)
				}
			}
		}

		if prefix != "" {
			c.Label("Handle: " + prefix).Send()
		}
		if backend != "" {
			c.Label("Backend: " + backend).Send()
		}
		inst.renderNote("watch-err", watchErr, badge.ToneError)
		if active {
			inst.renderNote("watch-active", "watching", badge.ToneSuccess)
		}
		c.Label(fmt.Sprintf("Events received: %d (showing last %d)",
			evCount, len(display))).Send()
		for _, ev := range display {
			c.Label(fmt.Sprintf("  %s  %s  %s",
				ev.Kind,
				ev.Name,
				time.Unix(0, ev.Ts).Format("15:04:05.000"))).Send()
		}
	}
}

// runWatchPick drives a fs.dialog.watch round-trip: publish → broker
// queues → picker overlay resolves with a folder → fs.handle.{uuid}.>
// is auto-granted (CapDirectionBoth — Sub on .event included) →
// subscribe to .event → start the watch. State updates land under
// inst.mu so Frame sees consistent snapshots.
//
// The Subscribe call happens BEFORE the watch start request so the
// pump can't publish an event the subscription has yet to register.
func (inst *App) runWatchPick() {
	if inst.bus == nil {
		inst.setWatchErr(noBusRemedy)
		return
	}

	rawReply, rerr := inst.bus.RequestWithTimeout(fsbroker.SubjectDialogWatch, nil, fsbroker.DialogTimeout)
	if rerr != nil {
		inst.setWatchErr("fs.dialog.watch: " + rerr.Error())
		return
	}
	dr, jerr := fsbroker.UnmarshalDialogReply(rawReply)
	if jerr != nil {
		inst.setWatchErr("dialog reply parse: " + jerr.Error())
		return
	}
	if !dr.Granted {
		inst.setWatchErr("dialog denied: " + dr.Reason)
		return
	}

	eventSubject := dr.HandleSubjectPrefix + "." + fsbroker.HandleEventOp
	unsubscribe, suberr := inst.bus.Subscribe(eventSubject, inst.handleWatchEvent)
	if suberr != nil {
		inst.setWatchErr("subscribe: " + suberr.Error())
		return
	}

	inst.mu.Lock()
	usePoller := inst.watchUsePoller
	recursive := inst.watchRecursive
	inst.mu.Unlock()

	req := fsbroker.WatchRequest{Recursive: recursive}
	if usePoller {
		req.PollFallback = true
		req.PollIntervalMs = 250
	}
	var reqPayload []byte
	if usePoller || recursive {
		payload, perr := fsbroker.MarshalWatchRequest(req)
		if perr != nil {
			unsubscribe()
			inst.setWatchErr("marshal request: " + perr.Error())
			return
		}
		reqPayload = payload
	}

	watchReply, werr := inst.bus.RequestWithTimeout(dr.HandleSubjectPrefix+".watch", reqPayload, fsbroker.HandleOpTimeout)
	if werr != nil {
		unsubscribe()
		inst.setWatchErr("watch start: " + werr.Error())
		return
	}
	wr, jerr := fsbroker.UnmarshalWatchReply(watchReply)
	if jerr != nil {
		unsubscribe()
		inst.setWatchErr("watch reply parse: " + jerr.Error())
		return
	}
	if !wr.Started {
		unsubscribe()
		inst.setWatchErr("watch not started: " + wr.Reason)
		return
	}

	inst.mu.Lock()
	inst.watchHandlePrefix = dr.HandleSubjectPrefix
	inst.watchBackend = wr.Backend
	inst.watchActive = true
	inst.watchUnsubscribe = unsubscribe
	inst.watchEvents = nil
	inst.watchErr = ""
	inst.mu.Unlock()
}

// runWatchStop publishes .unwatch and tears down the local subscription
// but keeps the handle alive so the user can restart the watch with a
// fresh subscription. Idempotent — calling stop on an inactive watch is
// a no-op.
func (inst *App) runWatchStop() {
	if inst.bus == nil {
		return
	}
	inst.mu.Lock()
	prefix := inst.watchHandlePrefix
	unsub := inst.watchUnsubscribe
	inst.watchActive = false
	inst.watchUnsubscribe = nil
	inst.mu.Unlock()
	if prefix != "" {
		_, _ = inst.bus.RequestWithTimeout(prefix+".unwatch", nil, fsbroker.HandleOpTimeout)
	}
	if unsub != nil {
		unsub()
	}
}

// runWatchClose releases the handle entirely (broker evicts it; the
// subject becomes invalid). Tears down any active watch implicitly via
// the broker's handleClose path.
func (inst *App) runWatchClose() {
	if inst.bus == nil {
		return
	}
	inst.mu.Lock()
	prefix := inst.watchHandlePrefix
	unsub := inst.watchUnsubscribe
	inst.watchActive = false
	inst.watchUnsubscribe = nil
	inst.watchHandlePrefix = ""
	inst.watchBackend = ""
	inst.watchEvents = nil
	inst.mu.Unlock()
	if prefix != "" {
		_, _ = inst.bus.RequestWithTimeout(prefix+".close", nil, fsbroker.HandleOpTimeout)
	}
	if unsub != nil {
		unsub()
	}
}

// handleWatchEvent is the per-event callback. Appends to the rolling
// buffer under inst.mu. The watchActive gate guards against late events
// arriving between Stop's publish and the unsubscribe call.
func (inst *App) handleWatchEvent(msg *app.Msg) {
	ev, jerr := fsbroker.UnmarshalWatchEvent(msg.Payload)
	if jerr != nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.watchActive {
		return
	}
	inst.watchEvents = append(inst.watchEvents, ev)
	if len(inst.watchEvents) > watchEventLimit {
		inst.watchEvents = inst.watchEvents[len(inst.watchEvents)-watchEventLimit:]
	}
}

func (inst *App) setWatchErr(s string) {
	inst.mu.Lock()
	inst.watchErr = s
	inst.mu.Unlock()
}
