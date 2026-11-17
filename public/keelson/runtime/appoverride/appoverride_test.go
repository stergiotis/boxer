package appoverride

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"
)

func fixtureRegistry(t *testing.T) (reg *app.Registry) {
	t.Helper()
	reg = app.NewRegistry()
	for _, m := range []app.Manifest{
		{Id: "test.shell", Shell: true, Topics: []app.TopicT{app.TopicRuntime}, Maturity: app.MaturityCore},
		{Id: "test.data", Topics: []app.TopicT{app.TopicData}, Maturity: app.MaturityIncubating},
		{Id: "test.applet", Kind: app.KindApplet, Topics: []app.TopicT{app.TopicSql}},
	} {
		m.Version, m.Display, m.Summary, m.Surface = "0.1.0", string(m.Id), "fixture", app.SurfaceWindowed
		require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) { return }))
	}
	return
}

func row(t *testing.T, kv map[string]any) (r Row) {
	t.Helper()
	r = make(Row, len(kv))
	for k, v := range kv {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		r[k] = b
	}
	return
}

func TestDiff(t *testing.T) {
	in := []Row{
		row(t, map[string]any{"id": "a", "display": "A", "title": "A", "maturity": "incubating", "maturity_rank": 1, "kind": "app", "topics": []string{"data"}}),
		row(t, map[string]any{"id": "b", "display": "B", "title": "B", "maturity": "unstated", "maturity_rank": 0, "kind": "app", "topics": []string{"data"}}),
	}
	t.Run("restated columns become fields, unchanged ones do not", func(t *testing.T) {
		out := []Row{row(t, map[string]any{"id": "a", "display": "Renamed", "title": "A", "maturity": "supported", "maturity_rank": 1, "kind": "app", "topics": []string{"data"}})}
		o, err := Diff(in, out)
		require.NoError(t, err)
		require.Contains(t, o, app.AppIdT("a"))
		assert.Equal(t, "Renamed", *o["a"].Display)
		assert.Equal(t, app.MaturitySupported, *o["a"].Maturity)
		assert.Nil(t, o["a"].Topics)
		assert.NotContains(t, o, app.AppIdT("b"), "an app the statement omits keeps its values")
	})
	t.Run("a rank that follows the grade is accepted", func(t *testing.T) {
		_, err := Diff(in, []Row{row(t, map[string]any{"id": "a", "maturity": "supported", "maturity_rank": app.MaturitySupported.Rank()})})
		require.NoError(t, err)
	})
	t.Run("refusals", func(t *testing.T) {
		for name, out := range map[string][]Row{
			"no id":              {row(t, map[string]any{"display": "X"})},
			"unregistered id":    {row(t, map[string]any{"id": "z", "display": "X"})},
			"repeated id":        {row(t, map[string]any{"id": "a"}), row(t, map[string]any{"id": "a"})},
			"unknown column":     {row(t, map[string]any{"id": "a", "maturty": "supported"})},
			"not writable":       {row(t, map[string]any{"id": "a", "kind": "demo"})},
			"rank against grade": {row(t, map[string]any{"id": "a", "maturity_rank": 3})},
			"title":              {row(t, map[string]any{"id": "a", "title": "Other"})},
			"unknown grade":      {row(t, map[string]any{"id": "a", "maturity": "shiny"})},
			"unknown topic":      {row(t, map[string]any{"id": "a", "topics": []string{"no-such-topic"}})},
		} {
			_, err := Diff(in, out)
			assert.Error(t, err, name)
		}
	})
}

// Unset, Apply neither needs a bus nor reaches chlocal.
func TestApply_UnsetTouchesNothing(t *testing.T) {
	Sql.Override("")
	t.Cleanup(Sql.ClearOverride)
	require.NoError(t, Apply(context.Background(), nil, fixtureRegistry(t), false, "  ", zerolog.Nop()))
}

func TestApply_SetWithoutChlocalFails(t *testing.T) {
	Sql.Override("")
	t.Cleanup(Sql.ClearOverride)
	err := Apply(context.Background(), nil, fixtureRegistry(t), false, "SELECT id FROM apps", zerolog.Nop())
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

// A statement may return every app, restating a column by expression; only
// the rows whose values change are overrides, and the predicates read the
// result. (The grammar the engine parses with has no `* REPLACE (...)`, so
// the columns are listed.)
func TestTransform_EveryRowFeedsThePredicates(t *testing.T) {
	bus := newBus(t)
	reg := fixtureRegistry(t)
	n, err := Transform(context.Background(), bus, reg,
		"SELECT id, if(id = 'test.applet', 'supported', maturity) AS maturity, if(id = 'test.applet', arrayPushBack(keywords, 'sql'), keywords) AS keywords, maturity_rank FROM apps",
		zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	m, _ := reg.LookupManifest("test.applet")
	assert.Equal(t, app.MaturitySupported, m.Maturity)
	assert.Equal(t, []string{"sql"}, m.Keywords)
	assert.Equal(t, []string{"keywords", "maturity"}, reg.Overridden("test.applet"))

	ids, err := launchlimit.Evaluate(context.Background(), bus, reg, "maturity_rank >= 2 AND NOT shell", zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, []app.AppIdT{"test.applet"}, ids)
	ids, err = launchlimit.Evaluate(context.Background(), bus, reg, "has(overridden, 'maturity')", zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, []app.AppIdT{"test.applet"}, ids)
}

// The narrow form names only the apps and columns it changes.
func TestTransform_NarrowForm(t *testing.T) {
	bus := newBus(t)
	reg := fixtureRegistry(t)
	_, err := Transform(context.Background(), bus, reg,
		"SELECT id, 'Data browser' AS display, ['data', 'sql'] AS topics FROM apps WHERE id = 'test.data' -- trailing comment",
		zerolog.Nop())
	require.NoError(t, err)
	m, _ := reg.LookupManifest("test.data")
	assert.Equal(t, "Data browser", m.Display)
	assert.Equal(t, []app.TopicT{app.TopicData, app.TopicSql}, m.Topics)
}

func TestTransform_Refusals(t *testing.T) {
	bus := newBus(t)
	for name, sql := range map[string]string{
		"host-decided column": "SELECT id, false AS launchable FROM apps",
		"derived column":      "SELECT id, 3 AS maturity_rank FROM apps",
		"provenance column":   "SELECT id, 'demo' AS kind FROM apps",
		"invalid manifest":    "SELECT id, '' AS display FROM apps",
		"not a statement":     "SELECT FROM",
	} {
		reg := fixtureRegistry(t)
		_, err := Transform(context.Background(), bus, reg, sql, zerolog.Nop())
		assert.Error(t, err, name)
		assert.Nil(t, reg.Overridden("test.data"), name)
	}
}

// The operator's statement reads the table as the wiring's left it.
func TestApply_ComposesWiringThenOperator(t *testing.T) {
	bus := newBus(t)
	reg := fixtureRegistry(t)
	Sql.Override("SELECT id, concat(display, ' (ops)') AS display FROM apps WHERE has(overridden, 'display')")
	t.Cleanup(Sql.ClearOverride)
	require.NoError(t, Apply(context.Background(), bus, reg, true,
		"SELECT id, 'Data' AS display FROM apps WHERE id = 'test.data'", zerolog.Nop()))
	m, _ := reg.LookupManifest("test.data")
	assert.Equal(t, "Data (ops)", m.Display)
	m, _ = reg.LookupManifest("test.shell")
	assert.Equal(t, "test.shell", m.Display)
}
