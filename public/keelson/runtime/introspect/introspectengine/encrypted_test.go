package introspectengine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// newEncEngine builds an engine over a broker and a registry the test
// controls, so an EncryptedEntry and its key can be registered.
func newEncEngine(t *testing.T) (*Engine, *chlocalbroker.Service, *introspect.Registry) {
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
	caller := bus.NewClient("test.introspect.enc", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	e, err := New(Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)
	return e, svc, reg
}

// publishEnc encrypts a one-Int64-column dataset, registers its key with
// the broker, and registers it as an EncryptedEntry in the registry.
func publishEnc(t *testing.T, svc *chlocalbroker.Service, reg *introspect.Registry, name string, vals []int64) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)

	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var streamBuf bytes.Buffer
	w := ipc.NewWriter(&streamBuf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())

	key := bytes.Repeat([]byte{0x5A}, adhocdata.KeySize)
	path := filepath.Join(t.TempDir(), name+".bxad")
	f, err := os.Create(path)
	require.NoError(t, err)
	ew, err := adhocdata.NewWriter(f, key)
	require.NoError(t, err)
	_, err = ew.Write(streamBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, ew.Close())
	require.NoError(t, f.Close())

	svc.KeyStore().RegisterDatasetKey(name, key)
	require.NoError(t, reg.Register(introspect.NewEncryptedEntry(name, schema, structure, path, 1)))
}

func TestEngine_EncryptedDataset(t *testing.T) {
	e, svc, reg := newEncEngine(t)
	publishEnc(t, svc, reg, "adhoc_ds", []int64{5, 7, 9})

	body, _, err := e.Query(context.Background(), "SELECT sum(v) FROM keelson('adhoc_ds')", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "21", strings.TrimSpace(string(body)))
}

// TestEngine_MixedAdhocAndProvider joins an ad-hoc (streamed) table and a
// snapshot provider in one query; the engine routes each by its kind.
func TestEngine_MixedAdhocAndProvider(t *testing.T) {
	e, svc, reg := newEncEngine(t)
	publishEnc(t, svc, reg, "adhoc_ds", []int64{1, 2, 3})

	body, _, err := e.Query(context.Background(),
		"SELECT (SELECT count() FROM keelson('adhoc_ds')) AS a, (SELECT count() > 0 FROM keelson('env')) AS b",
		"TabSeparated")
	require.NoError(t, err)
	// a = ad-hoc row count (streamed); b = 1 because env (a snapshot
	// provider) has rows. Both tables resolved in one query.
	assert.Equal(t, "3\t1", strings.TrimSpace(string(body)))
}
