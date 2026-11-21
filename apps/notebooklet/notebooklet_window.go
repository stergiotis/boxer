package notebooklet

import (
	"context"
	"sync"

	nblaunch "github.com/stergiotis/boxer/apps/notebook/launchcfg"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// window is one notebooklet window: the pane over its notebook, and the
// hand-over to the notebook window. It serves reading the document,
// reading the pane's state and starting a run; nothing that changes the
// text (ADR-0380 §SD6).
type window struct {
	def  *Def
	ids  *c.WidgetIdStack
	bus  app.BusI
	pane *notebook.EmbeddedPane
	// paneErr is why the pane could not be built: a document whose records
	// do not classify read, which the corpus test keeps off main.
	paneErr string
	// run stands in for the server in a test; nil is the environment's.
	run mdnotebook.RunFunc

	scope    *lifetime.Scope
	ownScope bool
	// note is what the last hand-over came to, written off the frame under
	// mu; busy says one is in flight.
	mu   sync.Mutex
	note string
	busy bool
}

var _ app.AppI = (*window)(nil)
var _ app.OperationsAppI = (*window)(nil)

func newWindow(def *Def) *window { return &window{def: def, ids: c.NewWidgetIdStack()} }

func (inst *window) Manifest() (m app.Manifest) { return inst.def.Manifest }

// Operations serves the catalog for this window.
func (inst *window) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// build makes the pane, once.
func (inst *window) build() {
	if inst.pane != nil || inst.paneErr != "" {
		return
	}
	pane, err := notebook.NewEmbedded(inst.def.Text, notebook.EmbedConfig{Host: inst.def.Manifest.Id, Key: inst.def.Slug, Files: inst.def.Files,
		RunOnMount: true, OwnWindow: true, Catalog: true, Run: inst.run})
	if err != nil {
		inst.paneErr = err.Error()
		return
	}
	inst.pane = pane
}

// Mount builds the pane and starts its first run: a notebooklet is opened
// to be read live.
func (inst *window) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	inst.build()
	if inst.pane == nil {
		return nil
	}
	if mErr := inst.pane.Mount(ctx); mErr != nil {
		// A run that was refused is shown, not a window that fails to open.
		inst.setNote("not run live: " + mErr.Error())
	}
	return nil
}

func (inst *window) Unmount(ctx app.MountContextI) (err error) {
	if inst.pane != nil {
		err = inst.pane.Unmount(ctx)
	}
	if inst.ownScope && inst.scope != nil {
		inst.scope.Retire()
	}
	return
}

const (
	labelOpenNotebook = "Open in notebook"
	tipOpenNotebook   = "Open the notebook window on this document: its records in play, its stored results and collection. The text is handed over as it stands."
)

func (inst *window) Frame(ctx app.FrameContextI) (err error) {
	inst.mu.Lock()
	note, busy := inst.note, inst.busy
	inst.mu.Unlock()
	for range c.HorizontalTop().KeepIter() {
		for range c.EnabledUi(!busy && inst.bus != nil).KeepIter() {
			for range c.HoverText(tipOpenNotebook).KeepIter() {
				if c.Button(inst.ids.PrepareStr("openNotebook"), c.Atoms().Text(labelOpenNotebook).Keep()).SendResp().HasPrimaryClicked() {
					if _, gErr := appops.Gesture[appops.None, appops.None](ctx, opOpenInNotebook, appops.None{}); gErr != nil {
						if oErr := inst.openInNotebook(); oErr != nil {
							inst.setNote(oErr.Error())
						}
					}
				}
			}
		}
		c.Label(inst.def.Book + " · " + inst.def.Path).Send()
	}
	if note != "" {
		c.Label(note).Wrap().Send()
	}
	c.Separator().Send()
	if inst.pane == nil {
		c.Label("The notebook cannot be read live: " + inst.paneErr).Wrap().Send()
		return nil
	}
	return inst.pane.Frame(ctx)
}

func (inst *window) setNote(note string) {
	inst.mu.Lock()
	inst.note = note
	inst.mu.Unlock()
}

// openInNotebook asks the window host for a notebook window on the
// document. The request runs off the render goroutine; the note says what
// it came to.
func (inst *window) openInNotebook() (err error) {
	if inst.bus == nil {
		return app.RefuseOperation("this host gives the window no bus to open another window through")
	}
	cfg, err := buscodec.Encode(nblaunch.NotebookLaunch{Text: string(inst.def.Text), Name: inst.def.Slug + ".md"})
	if err != nil {
		return err
	}
	inst.mu.Lock()
	if inst.busy {
		inst.mu.Unlock()
		return app.RefuseOperation("the notebook window is being opened")
	}
	inst.busy, inst.note = true, "opening the notebook window…"
	inst.mu.Unlock()
	bus := inst.bus
	started := inst.scope.Go(func(context.Context) {
		_, oErr := windowopen.RequestOpen(bus, nblaunch.AppId, nblaunch.Kind, cfg)
		note := "opened in the notebook window; it has the text and none of the stored results"
		if oErr != nil {
			note = "the notebook window was not opened: " + oErr.Error()
		}
		inst.mu.Lock()
		inst.busy, inst.note = false, note
		inst.mu.Unlock()
	})
	if !started {
		inst.mu.Lock()
		inst.busy, inst.note = false, ""
		inst.mu.Unlock()
		return app.RefuseOperation("the window is closing")
	}
	return nil
}
