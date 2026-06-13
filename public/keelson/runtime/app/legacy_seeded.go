package app

import (
	"sync/atomic"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SeededFuncApp is the multi-instance-safe twin of LegacyFuncApp. The
// renderer receives a unique, atomic-counter-derived seed at every
// Open(); inside the renderer, that seed wraps the body in a
// c.IdScope so two open windows of the same app push disjoint Go-side
// widget IDs through the duplicate-id checker, even though both share
// a package-level WidgetIdStack and key strings.
//
// Apps register via RegisterFactory so each Open allocates a fresh
// SeededFuncApp value (with a fresh seed). State held in the
// renderer's package globals (filter selections, query caches, …)
// remains shared across instances — a known caveat. Apps that need
// per-instance state migrate to a bespoke AppI struct (see logviewer
// for the pattern); apps that only need ID isolation use
// SeededFuncApp.
type SeededFuncApp struct {
	manifest Manifest
	seed     uint64
	render   func(seed uint64) (err error)
}

var _ AppI = (*SeededFuncApp)(nil)

// seededInstanceCounter feeds per-instance seeds. Starts at 0; every
// new SeededFuncApp increments and feeds the post-increment value
// into the renderer via Frame. Splitmix64 mixing happens inside the
// renderer (via WidgetIdStack.PrepareSeq), so the counter itself can
// stay a plain monotonic uint64.
var seededInstanceCounter atomic.Uint64

// NewSeededFuncApp constructs a SeededFuncApp with a fresh seed.
// Intended call site: a factory ctor passed to
// Registry.RegisterFactory. Singleton-registered SeededFuncApps would
// hand the same seed to every window and defeat the isolation, so
// the factory shape is mandatory in practice — Register(a) on this
// type still compiles, but loses the multi-instance guarantee.
func NewSeededFuncApp(m Manifest, render func(seed uint64) (err error)) (inst *SeededFuncApp, err error) {
	if render == nil {
		err = eh.Errorf("seeded: nil render")
		return
	}
	err = m.Validate()
	if err != nil {
		err = eh.Errorf("seeded: invalid manifest: %w", err)
		return
	}
	inst = &SeededFuncApp{
		manifest: m,
		seed:     seededInstanceCounter.Add(1),
		render:   render,
	}
	return
}

func (inst *SeededFuncApp) Manifest() (m Manifest)                  { m = inst.manifest; return }
func (inst *SeededFuncApp) Mount(ctx MountContextI) (err error)     { return }
func (inst *SeededFuncApp) Unmount(ctx MountContextI) (err error)   { return }
func (inst *SeededFuncApp) Frame(ctx FrameContextI) (err error)     { err = inst.render(inst.seed); return }

// Seed exposes the per-instance seed. Tests assert that two Open()
// calls produce SeededFuncApps with different seeds; production
// callers never need it.
func (inst *SeededFuncApp) Seed() (seed uint64) { seed = inst.seed; return }
