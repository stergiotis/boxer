package browserhost

import (
	"errors"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ErrNoSuchApp is Mount's answer for an id no linked package registered.
var ErrNoSuchApp = errors.New("browserhost: no app registered under that id (is its package linked in?)")

// instanceSalt scopes the app's widget ids as the window host's per-window
// salt does; one instance, one salt.
const instanceSalt = "browserhost"

// Mounted is one registered app running in the tab: mounted once, framed
// per tick, unmounted on close.
type Mounted struct {
	inst  app.AppI
	mount *app.StaticMountContext
	frame *app.StaticFrameContext
	stop  chan struct{}
}

// Mount looks id up in the default registry, mints an in-process bus client
// with the app's declared caps, and mounts the app over a static mount
// context on ids. Nothing else of the host runtime is booted: an app that
// needs a bus peer beyond itself (persist, fsbroker, adhocdata, ...) gets the
// bus's timeout on those requests.
func Mount(id app.AppIdT, ids *c.WidgetIdStack, logger zerolog.Logger) (m *Mounted, err error) {
	manifest, ok := app.DefaultRegistry.LookupManifest(id)
	if !ok {
		return nil, ErrNoSuchApp
	}
	inst, err := app.DefaultRegistry.Open(id)
	if err != nil {
		return nil, eb.Build().Str("app", string(id)).Errorf("browserhost: open: %w", err)
	}
	stop := make(chan struct{})
	bus := inprocbus.NewInst(zerolog.Nop())
	mc := app.NewStaticMountContext(manifest.Id, logger.With().Str("app", string(manifest.Id)).Logger(), nil, bus.NewClient(manifest.Id, manifest.Caps), stop)
	mc.SetIds(ids)
	m = &Mounted{inst: inst, mount: mc, frame: app.NewStaticFrameContext(mc, nil), stop: stop}
	if err = inst.Mount(mc); err != nil {
		close(stop)
		return nil, eb.Build().Str("app", string(id)).Errorf("browserhost: mount: %w", err)
	}
	return
}

// Frame renders one frame of the app under its instance salt on ids; an
// error the app returns is drawn in place, as the window host does.
func (inst *Mounted) Frame(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr(instanceSalt)) {
		if err := inst.inst.Frame(inst.frame); err != nil {
			c.Label("browserhost: frame error: " + err.Error()).Send()
		}
	}
}

// Unmount closes the mount context's stop channel and unmounts the app.
func (inst *Mounted) Unmount() (err error) {
	close(inst.stop)
	return inst.inst.Unmount(inst.mount)
}
