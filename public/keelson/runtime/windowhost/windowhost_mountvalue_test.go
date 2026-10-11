package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

type mountValueKey struct{}

// valueApp records what its Mount found under mountValueKey.
type valueApp struct {
	manifest app.Manifest
	got      any
}

var _ app.AppI = (*valueApp)(nil)

func (inst *valueApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *valueApp) Mount(ctx app.MountContextI) (err error) {
	inst.got = app.MountValueOf(ctx, mountValueKey{})
	return
}
func (inst *valueApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *valueApp) Unmount(ctx app.MountContextI) (err error) { return }

// A value the host is handed reaches every app it mounts, the one value
// and not a copy; an unset key is nil.
func TestTheHostHandsItsMountValuesToEveryApp(t *testing.T) {
	reg := app.NewRegistry()
	va := &valueApp{manifest: mkManifest("test.value")}
	require.NoError(t, reg.Register(va))
	h := NewInst(reg, zerolog.Nop())
	v := &struct{ n int }{n: 7}
	h.SetMountValue(mountValueKey{}, v)
	_, err := h.Open("test.value")
	require.NoError(t, err)
	// Drive Mount the way the first Frame would (windowhost_launch_test.go).
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()
	require.NoError(t, w.appInst.Mount(w.mountCtx))
	assert.Same(t, v, va.got)
	assert.Nil(t, app.MountValueOf(app.NewStaticMountContext("x", zerolog.Nop(), nil, nil, nil), mountValueKey{}))
}
