//go:build llm_generated_opus47

// Package capdemo is the M2 capability-broker showcase app. One window,
// four sections: a fs.dialog.read round-trip (pick a file, get a
// handle, fetch contents), a runtime.persist.{ownAlias}.scratchpad
// round-trip (save / load / delete a TextEdit's contents), an
// fs.dialog.watch round-trip (pick a folder, stream change events on
// fs.handle.{uuid}.event), and a status pane showing what's pending.
// Demonstrates that an app declaring the right Caps gets working Bus()
// and Storage() through MountContextI without any direct syscalls.
//
// Lifecycle: Mount captures the BusI and StorageI from the context.
// The file dialog flow runs in a goroutine because bus.Request blocks
// until the picker resolves; results land in fields guarded by
// `mu` and rendered on the next Frame.
package capdemo

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// ids is the package-level WidgetIdStack. Each frame's render wraps
// the body in c.IdScope(ids.PrepareSeq(inst.seed)) so two open windows
// produce disjoint Go-side widget IDs even though the stack is shared.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds. Every newApp() increments
// and the post-increment value is the App's stable salt for the
// lifetime of that window.
var instanceCounter atomic.Uint64

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

// clipboardDoc is the markdown rendered in the clipboard section. Parsed
// once at package load (markdown.Parse is the retain-once / render-many
// shape) and rendered every frame via Doc.RenderActions, which places a
// small "Copy" button on each fenced block; a click is consumed from the
// returned iter.Seq and routed to clipboard.write.
var clipboardDoc = markdown.Parse([]byte("" +
	"`clipboard.write` copies text to the viewport clipboard through the bus —\n" +
	"the broker accumulates the request off-frame and the host drains it into an\n" +
	"egui `copy_text` op. Click the Copy button above either block below.\n\n" +
	"```go\n" +
	"// the consumer wires the code-block button to any action:\n" +
	"for act := range doc.RenderActions(ids, \"Copy\") {\n" +
	"\tgo func() { _, _ = bus.Request(clipboardbroker.SubjectWrite, []byte(act.Text)) }()\n" +
	"}\n" +
	"```\n\n" +
	"```\n" +
	"plain verbatim block — copies exactly these bytes, no highlighting\n" +
	"```\n"))

// App is the per-window capdemo instance. Mount captures bus +
// storage from the MountContextI; Frame renders the three sections;
// Unmount is a no-op (the goroutine guards against use-after-unmount
// by checking app state before mutating).
type App struct {
	seed   uint64
	logger zerolog.Logger

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	bus     app.BusI
	storage app.StorageI

	mu sync.Mutex

	// fs.dialog.read state.
	pickInFlight     bool
	lastHandlePrefix string
	previewBytes     []byte
	previewTotal     int
	fileErr          string

	// runtime.persist state — the TextEdit binds to scratchpad; the
	// last operation's status (success / error) lands in
	// persistStatus.
	scratchpad    string
	persistStatus string

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
		seed:    instanceCounter.Add(1),
		density: styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount captures the per-app BusI and StorageI handles. The values
// are dormant on M1 hosts (NoopBus / NoopStorage) — every call
// errors with a documented "not available" message; on M2 hosts
// (carousel Phase A+B+C wiring) they are real inprocbus.Client /
// persist.Client. Either way, Mount itself is infallible.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	inst.storage = ctx.Storage()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// Frame renders the three demo sections. Body is wrapped in
// IdScope(seed) so per-instance widget ids stay disjoint across
// multiple open windows.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderApp()
	}
	return
}

func (inst *App) renderApp() {
	for range c.PanelTopInside(ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
		c.Label("Capability broker demo — exercises fs.dialog.read + runtime.persist.scratchpad").Send()
	}
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

// renderClipboardSection demonstrates the clipboard.write capability via
// the markdown widget's code-block action buttons (ADR-0026 Update
// 2026-05-30). Doc.RenderActions draws a small "Copy" button on each
// fenced block and yields the clicked blocks; each click fires a
// clipboard.write Request off the frame goroutine — Request blocks until
// the broker acks, and the frame thread must not block (same idiom as
// runPick). The broker enqueues the text; the host's windowed renderer
// drains it into an egui copy_text op.
func (inst *App) renderClipboardSection() {
	for range c.CollapsingHeader(ids.PrepareStr("hdr-clipboard"),
		c.WidgetText().Text("clipboard.write — copy code blocks to the clipboard").Keep()).
		DefaultOpen(true).KeepIter() {
		for act := range clipboardDoc.RenderActions(ids, "Copy") {
			if inst.bus == nil {
				continue
			}
			text := act.Text
			go func() {
				_, _ = inst.bus.Request(clipboardbroker.SubjectWrite, []byte(text))
			}()
		}
	}
}

func (inst *App) renderFsSection() {
	for range c.CollapsingHeader(ids.PrepareStr("hdr-fs"),
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
				c.Label("Picker open… (resolve or cancel in the overlay)").Send()
			} else {
				if c.Button(ids.PrepareStr("pick"),
					c.Atoms().Text("Pick a file…").Keep()).
					SendResp().HasPrimaryClicked() {
					go inst.runPick()
				}
				if handle != "" {
					if c.Button(ids.PrepareStr("close"),
						c.Atoms().Text("Close handle").Keep()).
						SendResp().HasPrimaryClicked() {
						go inst.runClose(handle)
					}
				}
			}
		}
		if handle != "" {
			c.Label("Granted handle subject: " + handle).Send()
		}
		if fileErr != "" {
			c.Label("Error: " + fileErr).Send()
		}
		if preview != nil {
			c.Label(fmt.Sprintf("Bytes read: %d (showing first %d)",
				previewTotal, len(preview))).Send()
			c.Label(string(preview)).Send()
		}
	}
}

func (inst *App) renderPersistSection() {
	for range c.CollapsingHeader(ids.PrepareStr("hdr-persist"),
		c.WidgetText().Text("runtime.persist.scratchpad — Set / Get / Delete").Keep()).
		DefaultOpen(true).KeepIter() {
		inst.mu.Lock()
		// Snapshot status under the lock; the TextEdit binding does
		// its own mutation outside the lock via SendRespVal (single-
		// threaded render is the synchronisation guarantee that lets
		// us share `scratchpad` with the lock-protected goroutine
		// writes; cf. regex_explorer pointer-swap pattern).
		status := inst.persistStatus
		inst.mu.Unlock()

		_ = c.TextEdit(ids.PrepareStr("scratchpad"), inst.scratchpad, true).
			CodeEditor().
			DesiredRows(3).
			HintText("type something to save…").
			SendRespVal(&inst.scratchpad)

		for range c.Horizontal().KeepIter() {
			if c.Button(ids.PrepareStr("save"),
				c.Atoms().Text("Save").Keep()).
				SendResp().HasPrimaryClicked() {
				go inst.runPersistSet(inst.scratchpad)
			}
			if c.Button(ids.PrepareStr("load"),
				c.Atoms().Text("Load").Keep()).
				SendResp().HasPrimaryClicked() {
				go inst.runPersistGet()
			}
			if c.Button(ids.PrepareStr("delete"),
				c.Atoms().Text("Delete").Keep()).
				SendResp().HasPrimaryClicked() {
				go inst.runPersistDelete()
			}
		}
		if status != "" {
			c.Label("Status: " + status).Send()
		}
	}
}

// runPick is the goroutine driving an fs.dialog.read round trip:
// publish → broker queues → picker overlay resolves → reply lands.
// Then a second Request fetches the file contents through the
// granted handle subject. State updates happen under inst.mu so the
// Frame goroutine sees a consistent snapshot.
func (inst *App) runPick() {
	if inst.bus == nil {
		inst.setFileErr("bus unavailable (M1 host?)")
		return
	}
	inst.setBusy(true)
	defer inst.setBusy(false)

	rawReply, rerr := inst.bus.Request(fsbroker.SubjectDialogRead, nil)
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
	body, rerr := inst.bus.Request(readSubj, nil)
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
	_, _ = inst.bus.Request(handlePrefix+".close", nil)
	inst.mu.Lock()
	inst.lastHandlePrefix = ""
	inst.previewBytes = nil
	inst.previewTotal = 0
	inst.fileErr = ""
	inst.mu.Unlock()
}

func (inst *App) runPersistSet(value string) {
	if inst.storage == nil {
		inst.setPersistStatus("storage unavailable (M1 host?)")
		return
	}
	err := inst.storage.Set(scratchpadKey, []byte(value))
	if err != nil {
		inst.setPersistStatus("set failed: " + err.Error())
		return
	}
	inst.setPersistStatus(fmt.Sprintf("saved %d bytes at %s",
		len(value), time.Now().Format("15:04:05")))
}

func (inst *App) runPersistGet() {
	if inst.storage == nil {
		inst.setPersistStatus("storage unavailable (M1 host?)")
		return
	}
	value, found, err := inst.storage.Get(scratchpadKey)
	if err != nil {
		inst.setPersistStatus("get failed: " + err.Error())
		return
	}
	if !found {
		inst.setPersistStatus("get: key absent")
		return
	}
	inst.mu.Lock()
	inst.scratchpad = string(value)
	inst.persistStatus = fmt.Sprintf("loaded %d bytes", len(value))
	inst.mu.Unlock()
}

func (inst *App) runPersistDelete() {
	if inst.storage == nil {
		inst.setPersistStatus("storage unavailable (M1 host?)")
		return
	}
	err := inst.storage.Delete(scratchpadKey)
	if err != nil {
		inst.setPersistStatus("delete failed: " + err.Error())
		return
	}
	inst.setPersistStatus("deleted")
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

func (inst *App) setPersistStatus(s string) {
	inst.mu.Lock()
	inst.persistStatus = s
	inst.mu.Unlock()
}

func (inst *App) renderWatchSection() {
	for range c.CollapsingHeader(ids.PrepareStr("hdr-watch"),
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
			_ = c.Checkbox(ids.PrepareStr("usepoller"), usePoller, "Force poller backend").
				SendRespVal(&inst.watchUsePoller)
			_ = c.Checkbox(ids.PrepareStr("recursive"), recursive, "Recursive (subtree)").
				SendRespVal(&inst.watchRecursive)
		}

		for range c.Horizontal().KeepIter() {
			if !active {
				if c.Button(ids.PrepareStr("watchpick"),
					c.Atoms().Text("Pick folder to watch…").Keep()).
					SendResp().HasPrimaryClicked() {
					go inst.runWatchPick()
				}
				if prefix != "" {
					if c.Button(ids.PrepareStr("watchclose"),
						c.Atoms().Text("Close handle").Keep()).
						SendResp().HasPrimaryClicked() {
						go inst.runWatchClose()
					}
				}
			} else {
				if c.Button(ids.PrepareStr("watchstop"),
					c.Atoms().Text("Stop watching").Keep()).
					SendResp().HasPrimaryClicked() {
					go inst.runWatchStop()
				}
				if c.Button(ids.PrepareStr("watchclose-active"),
					c.Atoms().Text("Close handle").Keep()).
					SendResp().HasPrimaryClicked() {
					go inst.runWatchClose()
				}
			}
		}

		if prefix != "" {
			c.Label("Handle: " + prefix).Send()
		}
		if backend != "" {
			c.Label("Backend: " + backend).Send()
		}
		if watchErr != "" {
			c.Label("Error: " + watchErr).Send()
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
		inst.setWatchErr("bus unavailable (M1 host?)")
		return
	}

	rawReply, rerr := inst.bus.Request(fsbroker.SubjectDialogWatch, nil)
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

	watchReply, werr := inst.bus.Request(dr.HandleSubjectPrefix+".watch", reqPayload)
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
		_, _ = inst.bus.Request(prefix+".unwatch", nil)
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
		_, _ = inst.bus.Request(prefix+".close", nil)
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
