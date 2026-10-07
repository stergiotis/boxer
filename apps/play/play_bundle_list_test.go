package play

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
)

// list_bundles' statement is a string until an engine runs it: this runs it
// over the introspection endpoint against a live bundle catalog and decodes
// what comes back, column summaries included (ADR-0288 §SD5).
func TestBundleListSqlRunsAgainstTheCatalog(t *testing.T) {
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)
	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})
	reg := introspect.NewRegistry()
	svc, err := adhocdata.NewService(adhocdata.Config{Registry: reg, Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{3, 1, 2}, nil)
	rb.Field(1).(*array.StringBuilder).AppendValues([]string{"b", "", "a"}, []bool{true, false, true})
	rec := rb.NewRecordBatch()
	rb.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	rec.Release()
	_, err = svc.PublishBundle(adhocdata.BundlePublishInput{Alias: "sales", Document: []byte("doc"),
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "orders", ArrowIPCStream: buf.Bytes()}}})
	require.NoError(t, err)

	caller := bus.NewClient("test.play.bundlelist", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	runner := introspecthttp.RunnerFunc(func(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
		rep, e := chlocalbroker.ExecOnPool(ctx, caller, "introspect", chlocalbroker.ExecRequest{SQL: sql, Params: params})
		if e != nil {
			return nil, e
		}
		defer func() { _ = rep.Close() }()
		if re := rep.Err(); re != nil {
			return nil, re
		}
		return io.ReadAll(rep)
	})
	s := introspecthttp.New(introspecthttp.Config{Registry: reg, Runner: runner}, logger)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader(bundleListSql+" FORMAT JSONEachRow"))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	got, err := decodeBundleRows(body)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "sales", got[0].Alias)
	assert.Equal(t, []BundleColumn{
		{Dataset: "orders", Name: "id", Type: "int64", Distinct: 3},
		{Dataset: "orders", Name: "note", Type: "utf8", Nulls: 1, Distinct: 2},
	}, got[0].Columns, "the catalog carries statistics, never values")
}
