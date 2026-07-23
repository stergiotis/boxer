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

// TestServiceE2E_LeewayShapedDataset publishes a leeway-shaped dataset —
// colon-laden physical names, Array-typed repeated sections, a nested
// Struct, and a Nullable scalar — through the capability service, then
// queries keelson('<handle>') through the in-process engine (publish → fifo
// read → served back → client SELECT). SELECT * returns every column
// intact, and the colon-named columns are addressable by quoted identifier
// through the keelson macro rewrite (ADR-0134 SD1/SD3).
func TestServiceE2E_LeewayShapedDataset(t *testing.T) {
	svc, eng := setupE2E(t)

	res, err := svc.Publish(adhocdata.PublishInput{Alias: "records", ArrowIPCStream: leewayStream(t)})
	require.NoError(t, err)
	require.Equal(t, uint64(2), res.Rows)

	// SELECT * carries the colon-named Array/Struct/Nullable columns back.
	body, _, err := eng.Query(context.Background(),
		"SELECT * FROM keelson('"+res.Handle+"') ORDER BY `id:kid:u64`", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t,
		"7\t[]\t(-3,'bye')\t\\N\n42\t['a','b']\t(10,'hi')\tok",
		strings.TrimSpace(string(body)))

	// The colon-named columns are queryable by quoted identifier through the
	// macro rewrite: nested Tuple access and a Nullable projection included.
	body, _, err = eng.Query(context.Background(),
		"SELECT `id:kid:u64`, length(`sec:tags`), `s:pair`.`y:sub`, `note` "+
			"FROM keelson('"+res.Handle+"') ORDER BY `id:kid:u64`", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "7\t0\tbye\t\\N\n42\t2\thi\tok", strings.TrimSpace(string(body)))
}

// TestServiceE2E_NaiveTimestamp publishes a timezone-naive Timestamp(ns)
// backbone column and reads it back through the engine. The stored epoch
// value must survive publish → fifo read → SELECT (a naive zone affects only
// display, not the value), checked tz-independently via toUnixTimestamp64Nano.
func TestServiceE2E_NaiveTimestamp(t *testing.T) {
	svc, eng := setupE2E(t)

	res, err := svc.Publish(adhocdata.PublishInput{Alias: "events", ArrowIPCStream: naiveTsStream(t)})
	require.NoError(t, err)

	body, _, err := eng.Query(context.Background(),
		"SELECT `id`, toUnixTimestamp64Nano(`evt:ns`) FROM keelson('"+res.Handle+"') ORDER BY `id`", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1\t1000000000000000000\n2\t0", strings.TrimSpace(string(body)))
}

// naiveTsStream builds a 2-row Arrow stream whose event-time column is a
// timezone-naive Timestamp(ns) (empty zone), the leeway backbone shape.
func naiveTsStream(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "evt:ns", Type: &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: ""}},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{1, 2}, nil)
	rb.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{1_000_000_000_000_000_000, 0}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// leewayStream builds a 2-row Arrow stream with a leeway-shaped schema:
// colon-laden names, an Array(String) repeated section, a nested Struct, and
// a Nullable scalar.
func leewayStream(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id:kid:u64", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "sec:tags", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String)},
		{Name: "s:pair", Type: arrow.StructOf(
			arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64},
			arrow.Field{Name: "y:sub", Type: arrow.BinaryTypes.String},
		)},
		{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()

	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{42, 7}, nil)

	tags := rb.Field(1).(*array.ListBuilder)
	tagsV := tags.ValueBuilder().(*array.StringBuilder)
	tags.Append(true)
	tagsV.AppendValues([]string{"a", "b"}, nil)
	tags.Append(true) // empty list

	pair := rb.Field(2).(*array.StructBuilder)
	px := pair.FieldBuilder(0).(*array.Int64Builder)
	py := pair.FieldBuilder(1).(*array.StringBuilder)
	pair.Append(true)
	px.Append(10)
	py.Append("hi")
	pair.Append(true)
	px.Append(-3)
	py.Append("bye")

	note := rb.Field(3).(*array.StringBuilder)
	note.Append("ok")
	note.AppendNull()

	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
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
