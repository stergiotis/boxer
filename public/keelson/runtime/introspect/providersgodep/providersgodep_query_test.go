package providersgodep

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
)

// The tables are only useful if ClickHouse can read them, so this drives the
// real SQL surface over the fixture manifest — no toolchain run, which keeps
// it in the default lane (the engine_topology_test.go precedent: probe for
// clickhouse-local, skip when absent).
//
// What it pins beyond "rows arrive": that no column name collides with a
// ClickHouse keyword once the provider is snapshotted as a TEMPORARY table,
// and that a recursive walk over go_imports — the shape every graph lens in
// the applet book is built from — runs.
func TestQuery_GodepTables(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	c := readyCache()
	reg := introspect.NewRegistry()
	for _, p := range []introspect.Provider{
		packagesProvider{cache: c}, importsProvider{cache: c},
		collectionProvider{cache: c}, packagePropsProvider{},
	} {
		require.NoError(t, reg.Register(p))
	}

	caller := bus.NewClient("test.godep.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	query := func(sql string) string {
		t.Helper()
		body, _, qErr := e.Query(context.Background(), sql, "TabSeparated")
		require.NoError(t, qErr, "query: %s", sql)
		return strings.TrimSpace(string(body))
	}

	// Every column reads back, including the ones whose names are ordinary
	// English words ClickHouse might have claimed.
	assert.Equal(t, "a\tinternal\t3",
		query("SELECT name, class, num_go_files FROM keelson('go_packages') WHERE import_path LIKE '%/a'"))

	// The join go_imports exists for: an edge resolved to its endpoints
	// without touching the node table's adjacency.
	assert.Equal(t, "github.com/example/mod/b",
		query("SELECT dst_path FROM keelson('go_imports') WHERE src_path LIKE '%/a' AND dst_path != ''"))

	// The header is always one row, whatever the collection state.
	assert.Equal(t, "ready\t2", query("SELECT status, num_packages FROM keelson('go_collection')"))
	assert.Equal(t, "tag_a,tag_b",
		query("SELECT arrayStringConcat(build_tags, ',') FROM keelson('go_collection')"))

	// The shape every graph lens builds on: a bounded reverse walk over the
	// edge table. Two hops from b reaches a (a → b), and no further.
	assert.Equal(t, "github.com/example/mod/a", query(`
WITH RECURSIVE walk AS (
    SELECT src_path AS path, 1 AS depth
    FROM keelson('go_imports')
    WHERE dst_path = 'github.com/example/mod/b'
  UNION ALL
    SELECT i.src_path, w.depth + 1
    FROM keelson('go_imports') AS i
    INNER JOIN walk AS w ON i.dst_path = w.path
    WHERE w.depth < 2
)
SELECT DISTINCT path FROM walk ORDER BY path`))

	// The props table is compiled in, so it answers beside a collected one.
	// The assertion is on the verdict vocabulary rather than on any one
	// package: the survey covers a subset of the repo and which packages it
	// has harvested is not this package's business, but the token set is
	// what an applet's WHERE clause is written against.
	assert.Equal(t, "1", query(`
SELECT count() = countIf(wasm_wasi IN ('unknown', 'compiles', 'blocked'))
   AND countIf(kind IN ('unspecified', 'demo', 'example', 'integration-test')) = count()
   AND count() > 0
FROM keelson('go_package_props')`))
}
