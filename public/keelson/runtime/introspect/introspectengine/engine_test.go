package introspectengine

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

func testRegistry(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	return r
}

func fieldNames(rec arrow.RecordBatch) (names []string) {
	for _, f := range rec.Schema().Fields() {
		names = append(names, f.Name)
	}
	return
}

// --- plan() white-box analysis tests (no broker needed) ---

func TestPlan_SingleTablePrunes(t *testing.T) {
	e := &Engine{reg: testRegistry(t)}
	p := e.plan("SELECT name, category FROM env WHERE sensitive")
	require.Equal(t, []string{"env"}, p.tables)
	require.True(t, p.pruned)
	require.False(t, p.proj["env"].IsAll())

	prov, _ := e.reg.Lookup("env")
	rec, err := prov.Snapshot(p.proj["env"])
	require.NoError(t, err)
	defer rec.Release()
	assert.ElementsMatch(t, []string{"name", "category", "sensitive"}, fieldNames(rec))
}

func TestPlan_StarForcesAllColumns(t *testing.T) {
	e := &Engine{reg: testRegistry(t)}
	p := e.plan("SELECT * FROM env WHERE category = 'system'")
	require.Equal(t, []string{"env"}, p.tables)
	assert.False(t, p.pruned)
	assert.True(t, p.proj["env"].IsAll())
}

func TestPlan_NoKnownTable(t *testing.T) {
	e := &Engine{reg: testRegistry(t)}
	assert.Empty(t, e.plan("SELECT 1").tables)
}

func TestPlan_JoinUsesAllColumns(t *testing.T) {
	e := &Engine{reg: testRegistry(t)}
	p := e.plan("SELECT * FROM env, apps")
	assert.ElementsMatch(t, []string{"apps", "env"}, p.tables)
	assert.False(t, p.pruned)
	assert.True(t, p.proj["env"].IsAll())
	assert.True(t, p.proj["apps"].IsAll())
}

// --- Query() integration tests (broker + clickhouse-local) ---

func newEngineWithBroker(t *testing.T) *Engine {
	t.Helper()
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
	caller := bus.NewClient("test.introspect.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := New(Config{Registry: testRegistry(t), Bus: caller}, logger)
	require.NoError(t, err)
	return e
}

func TestQuery_CountEnv(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(), "SELECT count() FROM env", "TabSeparated")
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	require.NoError(t, err, "body: %q", string(body))
	assert.Positive(t, n)
}

func TestQuery_PrunedProjectionReturnsRows(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(), "SELECT name FROM env ORDER BY name LIMIT 1", "TabSeparated")
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(body)))
}

func TestQuery_StarReturnsAllColumns(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(), "SELECT * FROM env ORDER BY name LIMIT 1", "JSONEachRow")
	require.NoError(t, err)
	s := string(body)
	// The star must materialise the full column set, not a pruned subset.
	assert.Contains(t, s, `"name"`)
	assert.Contains(t, s, `"category"`)
	assert.Contains(t, s, `"description"`)
}

func TestQuery_NoTableSelectLiteral(t *testing.T) {
	e := newEngineWithBroker(t)
	body, _, err := e.Query(context.Background(), "SELECT 1", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(body)))
}

func TestQuery_KeelsonMacro(t *testing.T) {
	e := newEngineWithBroker(t)
	// keelson('env') must resolve exactly like FROM env.
	body, _, err := e.Query(context.Background(), "SELECT count() FROM keelson('env')", "TabSeparated")
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	require.NoError(t, err, "body: %q", string(body))
	assert.Positive(t, n)
}

func TestQuery_KeelsonMacroUnknownFailsFast(t *testing.T) {
	// The macro rewrite runs before any broker call, so this needs no
	// clickhouse-local — an unknown keelson table errors immediately.
	e := &Engine{reg: testRegistry(t)}
	_, _, err := e.Query(context.Background(), "SELECT * FROM keelson('bogus')", "TabSeparated")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown keelson table")
}
