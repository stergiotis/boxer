package launchlimit

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func fixtureRegistry(t *testing.T) (reg *app.Registry) {
	t.Helper()
	reg = app.NewRegistry()
	for _, m := range []app.Manifest{
		{Id: "test.shell", Shell: true, Topics: []app.TopicT{app.TopicRuntime}},
		{Id: "test.data", Topics: []app.TopicT{app.TopicData}},
		{Id: "test.applet", Kind: app.KindApplet, Topics: []app.TopicT{app.TopicSql}},
	} {
		m.Version, m.Display, m.Summary, m.Surface = "0.1.0", string(m.Id), "fixture", app.SurfaceWindowed
		require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) { return }))
	}
	return
}

// Unset, Apply neither needs a bus nor reaches chlocal.
func TestApply_UnsetTouchesNothing(t *testing.T) {
	Where.Override("")
	t.Cleanup(Where.ClearOverride)
	reg := fixtureRegistry(t)
	require.NoError(t, Apply(context.Background(), nil, reg, false, zerolog.Nop()))
	assert.True(t, reg.Launchable("test.applet"))
}

// Set without chlocal, the host must not boot without the limit.
func TestApply_SetWithoutChlocalFails(t *testing.T) {
	Where.Override("shell")
	t.Cleanup(Where.ClearOverride)
	err := Apply(context.Background(), nil, fixtureRegistry(t), false, zerolog.Nop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse-local")
}

func newBus(t *testing.T) (bus *inprocbus.Inst) {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus = inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	return
}

func TestEvaluate_SelectsByColumns(t *testing.T) {
	bus := newBus(t)
	reg := fixtureRegistry(t)
	ids, err := Evaluate(context.Background(), bus, reg, "shell OR has(topics, 'data')", zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, []app.AppIdT{"test.data", "test.shell"}, ids)

	ids, err = Evaluate(context.Background(), bus, reg, "kind != 'applet'", zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, []app.AppIdT{"test.data", "test.shell"}, ids)
}

func TestEvaluate_BadPredicateFails(t *testing.T) {
	bus := newBus(t)
	_, err := Evaluate(context.Background(), bus, fixtureRegistry(t), "no_such_column = 1", zerolog.Nop())
	require.Error(t, err)
}

func TestApply_LimitsTheRegistry(t *testing.T) {
	bus := newBus(t)
	Where.Override("NOT shell")
	t.Cleanup(Where.ClearOverride)
	reg := fixtureRegistry(t)
	require.NoError(t, Apply(context.Background(), bus, reg, true, zerolog.Nop()))
	assert.False(t, reg.Launchable("test.shell"))
	assert.True(t, reg.Launchable("test.data"))
	assert.True(t, reg.Launchable("test.applet"))
}
