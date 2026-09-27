package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// appScene mounts one registered keelson app (ADR-0026 AppI) the way the
// window host does, minus the window: a static mount context over an
// in-process bus with the app's own declared caps, the instance salt on
// the id stack, and Frame once per tick. Nothing else of the host runtime
// is booted — no persist, no facts, no services — so an app that needs a
// bus peer beyond itself gets the bus's timeout; taskdemo needs none, its
// producer and observer are both the app.
type appScene struct {
	id    app.AppIdT
	inst  app.AppI
	mount *app.StaticMountContext
	frame *app.StaticFrameContext
	stop  chan struct{}
	err   error
}

// appSceneSalt scopes the app's widget ids as windowhost's instance salt
// does; one instance, one salt.
const appSceneSalt = "wasmspike-app"

func (inst *appScene) setup(ids *c.WidgetIdStack) {
	m, ok := app.DefaultRegistry.LookupManifest(inst.id)
	if !ok {
		inst.err = errNoSuchApp
		return
	}
	a, err := app.DefaultRegistry.Open(inst.id)
	if err != nil {
		inst.err = err
		return
	}
	inst.inst = a
	inst.stop = make(chan struct{})
	logger := log.Logger.With().Str("app", string(m.Id)).Logger()
	bus := inprocbus.NewInst(zerolog.Nop())
	inst.mount = app.NewStaticMountContext(m.Id, logger, nil, bus.NewClient(m.Id, m.Caps), inst.stop)
	inst.mount.SetIds(ids)
	inst.frame = app.NewStaticFrameContext(inst.mount, nil)
	if err = a.Mount(inst.mount); err != nil {
		inst.err = err
	}
}

func (inst *appScene) render(ids *c.WidgetIdStack, _, _ float32) {
	if inst.err != nil {
		c.Label("wasmspike: app: " + inst.err.Error()).Send()
		return
	}
	for range c.IdScope(ids.PrepareStr(appSceneSalt)) {
		if err := inst.inst.Frame(inst.frame); err != nil {
			c.Label("wasmspike: frame error: " + err.Error()).Send()
		}
	}
}

type appSceneError string

func (e appSceneError) Error() string { return string(e) }

const errNoSuchApp appSceneError = "no app registered under that id (is its package linked into the spike?)"
