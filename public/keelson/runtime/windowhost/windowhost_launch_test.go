package windowhost

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// testCfgKind is a test-local launch-config kind. The probe accepts
// exactly testCfgBytes, standing in for a generated codec's decoder so
// these tests exercise the host boundary without depending on any real
// codec module's wire format.
const testCfgKind = "windowhostTestCfg"

var testCfgBytes = []byte("windowhost-test-cfg-ok")

func init() {
	kindcheck.Register(testCfgKind, func(b []byte) (err error) {
		if !bytes.Equal(b, testCfgBytes) {
			err = eh.Errorf("test probe: bytes are not a %s payload", testCfgKind)
		}
		return
	})
}

// launchApp records the LaunchConfig its Mount observed.
type launchApp struct {
	manifest app.Manifest
	gotCfg   []byte
	mounted  bool
}

var _ app.AppI = (*launchApp)(nil)

func (inst *launchApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *launchApp) Mount(ctx app.MountContextI) (err error) {
	inst.gotCfg = ctx.LaunchConfig()
	inst.mounted = true
	return
}
func (inst *launchApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *launchApp) Unmount(ctx app.MountContextI) (err error) { return }

func mkLaunchManifest(id app.AppIdT, launchKind string) (m app.Manifest) {
	m = mkManifest(id)
	m.LaunchKind = launchKind
	return
}

func TestOpenWithConfig_UnknownApp(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, "test.a")
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.absent", testCfgKind, testCfgBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestOpenWithConfig_RefusesArgsForNoArgsApp(t *testing.T) {
	// mkRegistryWithSingleton registers manifests with an empty LaunchKind.
	reg, apps := mkRegistryWithSingleton(t, "test.noargs")
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.noargs", testCfgKind, testCfgBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts no launch config")
	assert.Equal(t, 0, h.Len(), "no window on refusal")
	assert.Zero(t, apps["test.noargs"].mountCalls)
}

func TestOpenWithConfig_RefusesKindMismatch(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", "someOtherKind")}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.launch", testCfgKind, testCfgBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
	assert.Equal(t, 0, h.Len())
}

func TestOpenWithConfig_RefusesOversize(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	big := make([]byte, maxLaunchConfigBytes+1)
	_, err := h.OpenWithConfig("test.launch", testCfgKind, big)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size cap")
	assert.Equal(t, 0, h.Len())
}

func TestOpenWithConfig_RefusesGarbageEnvelope(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.launch", testCfgKind, []byte("not a valid payload"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Equal(t, 0, h.Len())
}

func TestOpenWithConfig_RefusesInconsistentKindConfigPair(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.launch", "", testCfgBytes)
	require.Error(t, err, "config bytes without a kind must refuse")

	_, err = h.OpenWithConfig("test.launch", testCfgKind, nil)
	require.Error(t, err, "a claimed kind with empty config must refuse")

	assert.Equal(t, 0, h.Len())
}

func TestOpenWithConfig_DeliversBytesToMount(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	// Hand in a mutable copy so the defensive-copy contract is testable.
	caller := append([]byte(nil), testCfgBytes...)
	key, err := h.OpenWithConfig("test.launch", testCfgKind, caller)
	require.NoError(t, err)
	require.NotZero(t, key)
	caller[0] ^= 0xff

	// Drive Mount the way the first Frame would (Frame itself needs the
	// Rust runtime; see the lifecycle tests above).
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()
	require.NoError(t, w.appInst.Mount(w.mountCtx))
	assert.True(t, la.mounted)
	assert.Equal(t, testCfgBytes, la.gotCfg, "Mount sees the validated bytes, unaffected by caller mutation")
}

func TestOpen_PlainOpenOfLaunchKindAppUnchanged(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.Open("test.launch")
	require.NoError(t, err, "declaring LaunchKind must not affect plain opens")

	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()
	require.NoError(t, w.appInst.Mount(w.mountCtx))
	assert.Nil(t, la.gotCfg, "plain open delivers nil LaunchConfig")
}

func TestOpenWithConfig_SingletonSecondConfigRefused(t *testing.T) {
	reg := app.NewRegistry()
	la := &launchApp{manifest: mkLaunchManifest("test.launch", testCfgKind)}
	require.NoError(t, reg.Register(la))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.launch", testCfgKind, testCfgBytes)
	require.NoError(t, err)

	// The singleton instance already holds a window; a second config-
	// carrying open could never deliver its config at Mount.
	_, err = h.OpenWithConfig("test.launch", testCfgKind, testCfgBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "never be delivered")

	// A plain second window over the same singleton stays allowed.
	_, err = h.Open("test.launch")
	require.NoError(t, err)
	assert.Equal(t, 2, h.Len())
}

func TestOpenWithConfig_FactorySecondWindowEachGetsOwnConfig(t *testing.T) {
	// SD5: always a new window. Factory-registered apps mint one AppI per
	// Open, so every config-carrying open delivers to its own Mount.
	m := mkLaunchManifest("test.factory", testCfgKind)
	var instances []*launchApp
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		la := &launchApp{manifest: m}
		instances = append(instances, la)
		a = la
		return
	}))
	h := NewInst(reg, zerolog.Nop())

	_, err := h.OpenWithConfig("test.factory", testCfgKind, testCfgBytes)
	require.NoError(t, err)
	_, err = h.OpenWithConfig("test.factory", testCfgKind, testCfgBytes)
	require.NoError(t, err)
	require.Len(t, instances, 2)

	h.mu.Lock()
	wins := append([]*window(nil), h.windows...)
	h.mu.Unlock()
	require.Len(t, wins, 2)
	for i, w := range wins {
		require.NoError(t, w.appInst.Mount(w.mountCtx))
		assert.Equal(t, testCfgBytes, instances[i].gotCfg, "window %d delivers its own config", i)
	}
}
