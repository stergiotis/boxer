package hostboot

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

const testCfgKind = "hostbootTestCfg"

var testCfgBytes = []byte("hostboot-test-cfg-ok")

func init() {
	kindcheck.Register(testCfgKind, func(b []byte) (err error) {
		if !bytes.Equal(b, testCfgBytes) {
			err = eh.Errorf("test probe: bytes are not a %s payload", testCfgKind)
		}
		return
	})
}

// seedApp records what its lifecycle observed; mounting happens in the
// host's Frame, which needs a client, so a boot test sees Open and Reap.
type seedApp struct {
	manifest app.Manifest
}

var _ app.AppI = (*seedApp)(nil)

func (inst *seedApp) Manifest() (m app.Manifest)                { return inst.manifest }
func (inst *seedApp) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *seedApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *seedApp) Unmount(ctx app.MountContextI) (err error) { return }

func newTestRegistry(t *testing.T) (reg *app.Registry) {
	t.Helper()
	reg = app.NewRegistry()
	m := app.Manifest{
		Id:         "hostboot.test.seeded",
		Version:    "0.1.0",
		Display:    "seeded",
		Title:      "seeded",
		Topics:     []app.TopicT{app.TopicRuntime},
		Surface:    app.SurfaceWindowed,
		LaunchKind: testCfgKind,
	}
	require.NoError(t, reg.RegisterFactory(m, func() (app.AppI, error) { return &seedApp{manifest: m}, nil }))
	plain := app.Manifest{
		Id:      "hostboot.test.plain",
		Version: "0.1.0",
		Display: "plain",
		Title:   "plain",
		Topics:  []app.TopicT{app.TopicRuntime},
		Surface: app.SurfaceWindowed,
	}
	require.NoError(t, reg.Register(&seedApp{manifest: plain}))
	return
}

func minimalOptions(t *testing.T, reg *app.Registry) Options {
	t.Helper()
	return Options{
		Log:           zerolog.Nop(),
		Registry:      reg,
		Facts:         factsstore.NewInMemoryFactsStore(),
		KeepCoreDumps: true,
	}
}

func TestBoot_MinimalHostSeedsConfiguredWindow(t *testing.T) {
	reg := newTestRegistry(t)
	opts := minimalOptions(t, reg)
	plain, ok := reg.Lookup("hostboot.test.plain")
	require.True(t, ok)
	opts.LaunchApps = []app.AppI{plain}
	opts.SeedWindows = []SeedWindow{{AppId: "hostboot.test.seeded", Kind: testCfgKind, Config: testCfgBytes}}
	hookSeen := false
	opts.AfterHost = func(rt *Runtime) error {
		hookSeen = rt.Host != nil && rt.Bus != nil && rt.Introspect != nil
		return nil
	}

	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()

	assert.True(t, hookSeen, "AfterHost runs with host, bus and registry present")
	assert.False(t, rt.ScreenshotMode)
	assert.False(t, rt.IsChStore)
	assert.Equal(t, "mem", rt.Status.FactsBackend)
	assert.True(t, rt.Status.BusActive)
	assert.False(t, rt.Status.FsBrokerActive, "fs service is off in the minimal boot")
	assert.Nil(t, rt.Fs)
	assert.Nil(t, rt.Persist)
	assert.Nil(t, rt.ChLocal)
	assert.Nil(t, rt.Adhoc)
	assert.Nil(t, rt.Clipboard)
	assert.NotNil(t, rt.Tasks, "the task supervisor is part of the minimal boot")
	require.NotNil(t, rt.Host)
	assert.Equal(t, 2, rt.Host.Len(), "one plain seed and one configured seed")
	assert.Len(t, rt.Renderers, 1)

	rt.Reap()
	assert.Equal(t, 0, rt.Host.Len(), "Reap leaves no window")
	rt.Reap() // idempotent
}

func TestBoot_SeedWindowFailureIsAnError(t *testing.T) {
	reg := newTestRegistry(t)
	opts := minimalOptions(t, reg)
	opts.SeedWindows = []SeedWindow{{AppId: "hostboot.test.seeded", Kind: testCfgKind, Config: []byte("not the payload")}}

	rt, err := Boot(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seed window")
	if rt != nil {
		rt.Close()
	}
}

func TestBoot_ScreenshotModeNeedsLaunchApps(t *testing.T) {
	reg := newTestRegistry(t)
	opts := minimalOptions(t, reg)
	opts.ScreenshotDir = t.TempDir()

	rt, err := Boot(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "screenshot mode")
	if rt != nil {
		rt.Close()
	}
}

func TestBoot_ScreenshotModeBuildsTourRenderers(t *testing.T) {
	reg := newTestRegistry(t)
	opts := minimalOptions(t, reg)
	opts.ScreenshotDir = t.TempDir()
	plain, ok := reg.Lookup("hostboot.test.plain")
	require.True(t, ok)
	opts.LaunchApps = []app.AppI{plain}

	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	assert.True(t, rt.ScreenshotMode)
	assert.Nil(t, rt.Host, "screenshot mode has no window host")
	assert.Len(t, rt.Renderers, 1)
}

func TestRuntime_CloseRunsCleanupsInReverseOrder(t *testing.T) {
	reg := newTestRegistry(t)
	rt, err := Boot(context.Background(), minimalOptions(t, reg))
	require.NoError(t, err)
	var order []string
	rt.OnClose(func() { order = append(order, "first") })
	rt.OnClose(func() { order = append(order, "second") })
	rt.Close()
	rt.Close() // idempotent
	assert.Equal(t, []string{"second", "first"}, order)
}
