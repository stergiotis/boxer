package app

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// LegacyFuncApp adapts a func() error renderer as an AppI: the renderer is
// invoked from Frame, and Mount and Unmount do nothing. It suits a demo or
// a test fixture with no state of its own; an app with state implements
// AppI.
type LegacyFuncApp struct {
	manifest Manifest
	render   func() (err error)
}

var _ AppI = (*LegacyFuncApp)(nil)

// NewLegacyFuncApp constructs a LegacyFuncApp. The renderer must be non-nil
// and the manifest must validate; both checks return an error so init() can
// fail loudly rather than panic.
func NewLegacyFuncApp(m Manifest, render func() (err error)) (inst *LegacyFuncApp, err error) {
	if render == nil {
		err = eh.Errorf("legacy: nil render")
		return
	}
	err = m.Validate()
	if err != nil {
		err = eh.Errorf("legacy: invalid manifest: %w", err)
		return
	}
	inst = &LegacyFuncApp{
		manifest: m,
		render:   render,
	}
	return
}

func (inst *LegacyFuncApp) Manifest() (m Manifest) {
	m = inst.manifest
	return
}

func (inst *LegacyFuncApp) Mount(ctx MountContextI) (err error) {
	return
}

func (inst *LegacyFuncApp) Frame(ctx FrameContextI) (err error) {
	err = inst.render()
	return
}

func (inst *LegacyFuncApp) Unmount(ctx MountContextI) (err error) {
	return
}
