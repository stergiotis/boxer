package hello

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// App is one window: the name typed into it and the number of greetings,
// which outlives the window through the app's storage.
type App struct {
	// ioCtx is app.ContextOf(mountCtx): the ctx its persist and bus calls
	// run under when no call of its own carries one.
	ioCtx context.Context
	ids       *c.WidgetIdStack
	log       zerolog.Logger
	storage   app.StorageI
	name      string
	greetings atomic.Uint64
	// scope is the window's lifetime, where storage writes run; ownScope
	// says the host gave none and the window made its own.
	scope    *lifetime.Scope
	ownScope bool
	// storeMu serialises the writes, so the stored count is the latest one
	// even when two writes are in flight.
	storeMu sync.Mutex
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), name: "world"}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount reads the stored count, so a new window continues where the last
// one stopped. Storage is a bus request, so Mount — which may block — is
// where the read belongs, and Frame — which must not — never touches it.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.log = ctx.Log()
	inst.storage = ctx.Storage()
	inst.ioCtx = app.ContextOf(ctx)
	if raw, found, gErr := inst.storage.Get(inst.ioCtx, GreetingsKey); gErr == nil && found {
		if n, pErr := strconv.ParseUint(string(raw), 10, 64); pErr == nil {
			inst.greetings.Store(n)
		}
	} else if gErr != nil {
		inst.log.Debug().Err(gErr).Msg("hello: no stored count; starting at zero")
	}
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		// A host without a lifetime scope (a bare test context).
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	return
}

// Unmount retires a scope of the window's own and waits for the writes in
// flight; the host drains its own.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.ownScope {
		inst.scope.Retire()
		<-inst.scope.Done()
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	c.Label("Your name:").Send()
	c.TextEdit(inst.ids.PrepareStr("name"), inst.name, false).
		AccessibleName("Name").
		SendRespVal(&inst.name)
	if c.Button(inst.ids.PrepareStr("greet"), c.Atoms().Text("Greet").Keep()).SendResp().HasPrimaryClicked() {
		inst.greet()
	}
	c.Label("Hello, " + inst.name + "!").Send()
	c.Label("greeted " + strconv.FormatUint(inst.greetings.Load(), 10) + "×").Send()
	return
}

// greet counts one greeting and stores the count off the frame.
func (inst *App) greet() {
	inst.greetings.Add(1)
	inst.scope.Go(func(context.Context) {
		inst.storeMu.Lock()
		defer inst.storeMu.Unlock()
		n := inst.greetings.Load()
		if sErr := inst.storage.Set(inst.ioCtx, GreetingsKey, []byte(strconv.FormatUint(n, 10))); sErr != nil {
			inst.log.Debug().Err(sErr).Msg("hello: count not stored")
		}
	})
}
