package adhocdata_test

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
)

// setupE2E wires a broker, capability service, and in-process engine over
// one shared registry, exactly as the runtime does.
func setupE2E(t *testing.T) (*adhocdata.Service, *introspectengine.Engine) {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)

	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	svc, err := adhocdata.NewService(adhocdata.Config{
		Registry: reg, Keys: broker.KeyStore(), Dir: t.TempDir(), Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	caller := bus.NewClient("test.adhoc.e2e", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	eng, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)
	return svc, eng
}

// TestServiceE2E_QueryViaEngine is the milestone e2e: publish a dataset
// through the capability service, then query it by handle through the
// in-process engine over the same registry and broker (ADR-0134).
func TestServiceE2E_QueryViaEngine(t *testing.T) {
	svc, eng := setupE2E(t)

	res, err := svc.Publish(adhocdata.PublishInput{
		Alias: "items", ArrowIPCStream: int64Stream(t, 10, 20, 30),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), res.Rows)

	body, _, err := eng.Query(context.Background(), "SELECT sum(v) FROM keelson('"+res.Handle+"') ORDER BY 1", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "60", strings.TrimSpace(string(body)))

	// Republish new data; a fresh query sees it (revision bump invalidates
	// the broker's revision-keyed cache).
	_, err = svc.Publish(adhocdata.PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, 100)})
	require.NoError(t, err)
	body, _, err = eng.Query(context.Background(), "SELECT sum(v) FROM keelson('"+res.Handle+"')", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "100", strings.TrimSpace(string(body)))
}

// TestCatalogE2E queries keelson('adhoc'): the catalog lists live
// datasets and shrinks on retract (ADR-0134 SD6).
func TestCatalogE2E(t *testing.T) {
	svc, eng := setupE2E(t)

	a, err := svc.Publish(adhocdata.PublishInput{Alias: "alpha", Publisher: "app.one", ArrowIPCStream: int64Stream(t, 1, 2)})
	require.NoError(t, err)
	_, err = svc.Publish(adhocdata.PublishInput{Alias: "beta", Publisher: "app.two", ArrowIPCStream: int64Stream(t, 3)})
	require.NoError(t, err)

	body, _, err := eng.Query(context.Background(), "SELECT count(*) FROM keelson('adhoc')", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "2", strings.TrimSpace(string(body)), "two datasets in the catalog")

	// Columns are readable and attributed.
	body, _, err = eng.Query(context.Background(),
		"SELECT alias, publisher, rows FROM keelson('adhoc') ORDER BY alias", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "alpha\tapp.one\t2\nbeta\tapp.two\t1", strings.TrimSpace(string(body)))

	require.NoError(t, svc.Retract(a.Handle))
	body, _, err = eng.Query(context.Background(), "SELECT count(*) FROM keelson('adhoc')", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(body)), "retract removes the catalog row")
}

func int64Stream(t *testing.T, vals ...int64) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}
