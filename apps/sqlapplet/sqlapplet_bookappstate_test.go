package sqlapplet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// appStateDefsBySlug parses the embedded app-state book and indexes it.
func appStateDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("appstate", help.MustSub(bookappstateFS, "bookappstate"))
	require.Empty(t, errs)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 4, "one chapter per kind plus a per-app rollup (ADR-0185 §SD6)")
	return bySlug
}

// TestAppStateBookCorpus is the ADR-0132 §SD6 gate over the app-state book.
func TestAppStateBookCorpus(t *testing.T) {
	for slug, d := range appStateDefsBySlug(t) {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: nothing to fill in — the tables are live", slug)
		assert.NotContains(t, d.SQL, "payload ", "%s: payloads are described, not served (ADR-0185 §SD7)", slug)
	}
}

// TestAppStateBookQueriesExecute writes one entry of each kind for two apps
// through the runtime's own state backend, then runs every chapter through
// the introspection engine and checks what each one reports.
func TestAppStateBookQueriesExecute(t *testing.T) {
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	stateExec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	require.NoError(t, err)
	backend, err := persist.OpenStoreBackend(context.Background(), stateExec, nil)
	require.NoError(t, err)
	t.Cleanup(backend.Close)

	past := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, backend.Set(persist.StorageRef{Alias: "play", AppId: "example/play", InstanceKey: 2}, "tabs", []byte("hello")))
	require.NoError(t, backend.WriteWorkingset(statestore.WorkingsetRow{
		AppId: "example/tally", Name: "default", Kind: "tallyLaunch", Config: []byte("cfg"), Reason: "user-close", Ts: past,
	}))
	require.NoError(t, backend.WriteColumnWidth(statestore.ColumnWidthRow{
		AppId: "example/play", Tier: statestore.ColWidthTierInstance, Scope: "master/table",
		ColumnKey: "ab12", Points: 90, FontSize: 12, Ts: past,
	}))

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(120 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterAppState(reg, stateExec))
	require.NoError(t, providers.RegisterWorkingsets(reg, backend))
	caller := bus.NewClient("test.bookappstate.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	run := func(slug string) []string {
		t.Helper()
		d, ok := appStateDefsBySlug(t)[slug]
		require.True(t, ok, slug)
		body, _, qErr := e.Query(context.Background(), d.SQL, "TabSeparated")
		require.NoError(t, qErr, "%s: buffer failed", slug)
		return strings.Split(strings.TrimSpace(string(body)), "\n")
	}

	byApp := run("appstate-by-app")
	require.Len(t, byApp, 2, "one row per app")
	assert.True(t, strings.HasPrefix(byApp[0], "example/play\t1\t0\t1\t0\t5\t"), "play: one persisted value of 5 bytes and one width; got %q", byApp[0])
	assert.True(t, strings.HasPrefix(byApp[1], "example/tally\t0\t1\t0\t0\t3\t"), "tally: one workingset of 3 bytes; got %q", byApp[1])

	persisted := run("appstate-persist")
	require.Len(t, persisted, 1)
	assert.True(t, strings.HasPrefix(persisted[0], "example/play\ttabs\t5\t"), "got %q", persisted[0])

	ws := run("appstate-workingsets")
	require.Len(t, ws, 1)
	assert.True(t, strings.HasPrefix(ws[0], "example/tally\tdefault\ttallyLaunch\t3\tuser-close\t"), "got %q", ws[0])

	widths := run("appstate-column-widths")
	require.Len(t, widths, 1)
	assert.True(t, strings.HasPrefix(widths[0], "example/play\tinstance\tmaster/table\tab12\t90 pt @ 12\t"),
		"a scope containing / survives the split; got %q", widths[0])
}
