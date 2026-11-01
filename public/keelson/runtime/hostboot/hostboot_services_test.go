package hostboot

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"
	"github.com/stergiotis/boxer/public/keelson/runtime/servicehost"
)

// tickService is a headless fixture: Mount records the context and returns;
// Unmount counts.
type tickService struct {
	manifest     app.Manifest
	mounted      chan struct{}
	unmountCalls int
	cfgSeen      []byte
}

var _ app.AppI = (*tickService)(nil)

func (inst *tickService) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *tickService) Mount(ctx app.MountContextI) (err error) {
	inst.cfgSeen = ctx.LaunchConfig()
	close(inst.mounted)
	return
}
func (inst *tickService) Frame(ctx app.FrameContextI) (err error)   { panic("Frame on a service") }
func (inst *tickService) Unmount(ctx app.MountContextI) (err error) { inst.unmountCalls++; return }

const headlessId app.AppIdT = "hostboot.test.service"

func registryWithService(t *testing.T) (reg *app.Registry, svc *tickService) {
	t.Helper()
	reg = newTestRegistry(t)
	m := app.Manifest{
		Id: headlessId, Version: "0.1.0", Display: "service", Summary: "fixture service",
		Topics: []app.TopicT{app.TopicRuntime}, Surface: app.SurfaceHeadless, Kind: app.KindDemo, LaunchKind: testCfgKind,
	}
	svc = &tickService{manifest: m, mounted: make(chan struct{})}
	require.NoError(t, reg.RegisterFactory(m, func() (app.AppI, error) { return svc, nil }))
	return
}

func servicesRow(t *testing.T, rt *Runtime, id app.AppIdT) (row servicehost.Row) {
	t.Helper()
	require.NotNil(t, rt.Services)
	for _, r := range rt.Services.Rows() {
		if r.Id == id {
			return r
		}
	}
	t.Fatalf("no services row for %s", id)
	return
}

// Unset, the service host still exists and knows the headless app, and a
// window-less boot has no window host.
func TestBoot_NoWindowHost_UnsetRunsNoService(t *testing.T) {
	servicehost.Where.Override("")
	t.Cleanup(servicehost.Where.ClearOverride)
	reg, svc := registryWithService(t)
	opts := minimalOptions(t, reg)
	opts.NoWindowHost = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	assert.Nil(t, rt.Host)
	row := servicesRow(t, rt, headlessId)
	assert.False(t, row.Selected)
	assert.Equal(t, servicehost.StateNotSelected, row.State)
	select {
	case <-svc.mounted:
		t.Fatal("an unselected service was mounted")
	default:
	}
	_, ok := rt.Introspect.Lookup(servicehost.TableName)
	assert.True(t, ok, "keelson('services') is registered")
}

// A windowed host also has the service host, and Wait refuses it.
func TestBoot_WindowedHostHasServicesAndRefusesWait(t *testing.T) {
	servicehost.Where.Override("")
	t.Cleanup(servicehost.Where.ClearOverride)
	reg, _ := registryWithService(t)
	rt, err := Boot(context.Background(), minimalOptions(t, reg))
	require.NoError(t, err)
	defer rt.Close()
	require.NotNil(t, rt.Host)
	require.NotNil(t, rt.Services)
	require.Error(t, rt.Wait(context.Background()))
}

// Set without clickhouse-local, the host does not boot.
func TestBoot_ServicesPredicateWithoutChlocalFails(t *testing.T) {
	servicehost.Where.Override("true")
	t.Cleanup(servicehost.Where.ClearOverride)
	reg, _ := registryWithService(t)
	opts := minimalOptions(t, reg)
	opts.NoWindowHost = true
	rt, err := Boot(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse-local")
	if rt != nil {
		rt.Close()
	}
}

func chlocalOptions(t *testing.T, reg *app.Registry) (opts Options) {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	opts = minimalOptions(t, reg)
	opts.Services.ChLocal = true
	opts.NoWindowHost = true
	opts.ShutdownGrace = 30 * time.Second
	return
}

// The predicate composes with the launch limit by intersection: a service
// the limit refuses does not run under a predicate that selects it.
func TestBoot_ServicesPredicateComposesWithLaunchLimit(t *testing.T) {
	servicehost.Where.Override("kind = 'demo'")
	t.Cleanup(servicehost.Where.ClearOverride)
	launchlimit.Where.Override("kind = 'app'")
	t.Cleanup(launchlimit.Where.ClearOverride)
	reg, svc := registryWithService(t)
	rt, err := Boot(context.Background(), chlocalOptions(t, reg))
	require.NoError(t, err)
	defer rt.Close()
	assert.False(t, reg.Launchable(headlessId), "the launch limit refused the service")
	row := servicesRow(t, rt, headlessId)
	assert.False(t, row.Selected, "selected by the predicate alone, refused by the limit: not run")
	select {
	case <-svc.mounted:
		t.Fatal("a service the launch limit refused was mounted")
	default:
	}
}

// Within the limit, the predicate selects; the service mounts with its
// seed, writes its lifecycle rows, and Close reaps it through Unmount.
func TestBoot_ServicesPredicateSelectsAndCloseReaps(t *testing.T) {
	servicehost.Where.Override("kind = 'demo'")
	t.Cleanup(servicehost.Where.ClearOverride)
	launchlimit.Where.Override("")
	t.Cleanup(launchlimit.Where.ClearOverride)
	reg, svc := registryWithService(t)
	opts := chlocalOptions(t, reg)
	opts.SeedServices = []SeedService{{AppId: headlessId, Kind: testCfgKind, Config: testCfgBytes}}
	facts := factsstore.NewInMemoryFactsStore()
	opts.Facts = facts
	ctx, cancel := context.WithCancel(context.Background())
	rt, err := Boot(ctx, opts)
	require.NoError(t, err)
	require.NoError(t, rt.Services.Wait(context.Background()))
	<-svc.mounted
	assert.Equal(t, testCfgBytes, svc.cfgSeen)
	row := servicesRow(t, rt, headlessId)
	assert.True(t, row.Selected)
	assert.Equal(t, servicehost.StateRunning, row.State)
	assert.NotZero(t, row.InstanceKey)
	started := 0
	for _, lc := range facts.Lifecycles() {
		if lc.AppId == headlessId && lc.Phase == factsstore.AppLifecyclePhaseStarted {
			started++
			assert.Equal(t, row.InstanceKey, lc.TileKey)
		}
	}
	assert.Equal(t, 1, started)

	// The windowed fixture apps were never mounted: no window host here.
	assert.Len(t, rt.Services.Rows(), 1)

	cancel()
	require.NoError(t, rt.Wait(ctx), "Wait returns when the context ends and closes the runtime")
	assert.Equal(t, 1, svc.unmountCalls)
	row = servicesRow(t, rt, headlessId)
	assert.Equal(t, servicehost.StateStopped, row.State)
	assert.Equal(t, servicehost.StopReasonShutdown, row.StopReason)
	stopped := 0
	for _, lc := range facts.Lifecycles() {
		if lc.AppId == headlessId && lc.Phase == factsstore.AppLifecyclePhaseStopped {
			stopped++
			assert.Equal(t, servicehost.StopReasonShutdown, lc.StopReason)
		}
	}
	assert.Equal(t, 1, stopped)
}

// A seed that does not fit its service is a boot error, as a seed window's is.
func TestBoot_BadServiceSeedIsAnError(t *testing.T) {
	servicehost.Where.Override("kind = 'demo'")
	t.Cleanup(servicehost.Where.ClearOverride)
	launchlimit.Where.Override("")
	t.Cleanup(launchlimit.Where.ClearOverride)
	reg, _ := registryWithService(t)
	opts := chlocalOptions(t, reg)
	opts.SeedServices = []SeedService{{AppId: headlessId, Kind: testCfgKind, Config: []byte("not the payload")}}
	rt, err := Boot(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "services")
	if rt != nil {
		rt.Close()
	}
}

var _ = zerolog.Nop
