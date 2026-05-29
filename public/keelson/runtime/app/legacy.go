//go:build llm_generated_opus47

package app

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// LegacyFuncApp adapts an existing func() error renderer — the shape used
// by the appCode → render-closure switch at
// public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go
// — as an AppI. The renderer is invoked from Frame; Mount and Unmount are
// no-ops.
//
// Intended as a migration bridge during M1: lets the foundation package ship
// without rewriting the seven graphical apps. Each migrated app deletes its
// LegacyFuncApp wrapper once it implements AppI directly with a proper
// Frame body that consumes the FrameContextI.
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
