package opsdemo

import (
	"context"
	"strconv"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// App is one window: the note, the level, and the handles of the widgets
// bound to them, kept so the catalog can tell when the person is editing.
type App struct {
	ids    *c.WidgetIdStack
	bus    app.BusI
	note   string
	level  float64
	noteH  widgethandle.WidgetHandle
	levelH widgethandle.WidgetHandle
	// cleared counts the Clear button's gestures, shown so a scene can see
	// the person's path ran.
	cleared int
	// copied counts copies to the clipboard.
	copied int
	// scope is the window's lifetime, where the copy's round trip runs;
	// ownScope says the host gave none and the window made its own.
	scope    *lifetime.Scope
	ownScope bool
	// copyLine is what the broker did with the last copy, written off the
	// frame under mu and shown in the window.
	mu       sync.Mutex
	copyLine string
}

var _ app.AppI = (*App)(nil)
var _ app.OperationsAppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), note: "A note both of you can edit.", level: 50}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		// A host without a lifetime scope (a bare test context).
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	return
}

// copySource names the widget the note is copied from, on the broker's
// record and in the journal's copy entry (ADR-0317 §SD1).
const copySource = string(AppId) + ":note"

// copyNote hands the note to the clipboard broker as a labelled payload,
// off the render goroutine, and shows what the broker did with it once it
// answers: the note is the person's own text, so its label is the zero
// label, plain text.
func (inst *App) copyNote() {
	inst.copied++
	bus, text := inst.bus, inst.note
	if bus == nil {
		inst.setCopyLine("not copied: this host gives the app no bus")
		return
	}
	inst.scope.Go(func(context.Context) {
		rec, err := clipboardbroker.Write(bus, clipboardbroker.Payload{Text: text, Source: copySource})
		switch {
		case err != nil && rec.Refused:
			inst.setCopyLine("refused: " + rec.Reason)
		case err != nil:
			inst.setCopyLine("not copied: " + err.Error())
		case rec.Handle != "":
			inst.setCopyLine("copied by reference (" + rec.Handle + "): paste it inside boxer")
		default:
			inst.setCopyLine("copied as " + rec.Verdict() + " · " + strconv.Itoa(rec.Bytes) + " bytes")
		}
	})
}

func (inst *App) setCopyLine(line string) {
	inst.mu.Lock()
	inst.copyLine = line
	inst.mu.Unlock()
}

// Unmount retires a scope of the window's own; the host drains its own.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.ownScope {
		inst.scope.Retire()
	}
	return
}

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

const hintNote = "note"

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	c.Label("An agent holding a grant for this window changes the note and the level through the app's operations. " +
		"Typing in the note while a change is queued makes the change a conflict: the person wins a tie.").Wrap().Send()
	inst.noteH = widgethandle.Make(inst.ids.PrepareStr("note").Derive())
	c.TextEdit(inst.ids.PrepareStr("note"), inst.note, true).
		DesiredRows(4).
		HintText(hintNote).
		SendRespVal(&inst.note)
	inst.levelH = widgethandle.Make(inst.ids.PrepareStr("level").Derive())
	c.SliderF64(inst.ids.PrepareStr("level"), inst.level, 0, 100).Text("level").SendRespVal(&inst.level)
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("clear"), c.Atoms().Text("Clear").Keep()).SendResp().HasPrimaryClicked() {
			// One path (ADR-0269 §SD8): the button and an agent call the
			// same handler. Where no host serves the catalog, clear here.
			if _, gErr := appops.Gesture[appops.None, appops.None](ctx, opClearNote, appops.None{}); gErr != nil {
				inst.note = ""
			}
			inst.cleared++
		}
		c.Label("cleared " + strconv.Itoa(inst.cleared) + "×").Send()
		if c.Button(inst.ids.PrepareStr("copy"), c.Atoms().Text("Copy").Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
			if _, gErr := appops.Gesture[appops.None, appops.None](ctx, opCopyNote, appops.None{}); gErr != nil {
				inst.copyNote()
			}
		}
		c.Label("copied " + strconv.Itoa(inst.copied) + "×").Send()
	}
	inst.mu.Lock()
	copyLine := inst.copyLine
	inst.mu.Unlock()
	if copyLine != "" {
		c.Label(copyLine).Wrap().Send()
	}
	c.Label("note: " + strconv.Itoa(len([]rune(inst.note))) + " characters · level " + strconv.FormatFloat(inst.level, 'f', 1, 64)).Send()
	return
}
