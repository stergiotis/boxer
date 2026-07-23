package introspecthttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// TestServer_AdhocQueryEndpoint is the ADR-0134 §SD3 (revised) end-to-end:
// publish an encrypted dataset, then query keelson('<handle>') over the
// introspection /query endpoint. The query is rewritten to
// url('.../table/<handle>','ArrowStream','<structure>'); clickhouse-local
// fetches /table, which streams the in-process decryption; the rows match.
// A regular introspection table (env) resolves in the same query, proving
// the two coexist.
func TestServer_AdhocQueryEndpoint(t *testing.T) {
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
	require.NoError(t, providers.RegisterStatic(reg))
	adhoc, err := adhocdata.NewService(adhocdata.Config{
		Registry: reg, Keys: broker.KeyStore(), Dir: t.TempDir(), Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adhoc.Close(context.Background()) })

	res, err := adhoc.Publish(adhocdata.PublishInput{Alias: "items", ArrowIPCStream: adhocInt64Stream(t, 10, 20, 30)})
	require.NoError(t, err)

	caller := bus.NewClient("test.introspect.adhoc.query", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	runner := RunnerFunc(func(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
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
	s := New(Config{Registry: reg, Runner: runner, Decryptor: broker}, logger)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	post := func(sql string) string {
		resp, perr := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader(sql))
		require.NoError(t, perr)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		require.Equalf(t, http.StatusOK, resp.StatusCode, "body: %s", body)
		return strings.TrimSpace(string(body))
	}

	assert.Equal(t, "60", post("SELECT sum(v) FROM keelson('"+res.Handle+"') FORMAT TabSeparated"))

	// Ad-hoc and a regular provider in one query.
	assert.Equal(t, "3\t1",
		post("SELECT (SELECT count() FROM keelson('"+res.Handle+"')) AS a, (SELECT count()>0 FROM keelson('env')) AS b FORMAT TabSeparated"))

	// A leeway-shaped dataset — colon-laden names, an Array-typed section, a
	// nested Struct, a Nullable scalar — resolves over the same url() path:
	// its explicit structure rides the third url() argument, so clickhouse
	// applies the bounded mapping rather than its own inference (ADR-0134
	// §SD3 revised). SELECT * and quoted-identifier access both survive.
	lw, err := adhoc.Publish(adhocdata.PublishInput{Alias: "records", ArrowIPCStream: adhocLeewayStream(t)})
	require.NoError(t, err)
	assert.Equal(t, "7\t[]\t(-3,'bye')\t\\N\n42\t['a','b']\t(10,'hi')\tok",
		post("SELECT * FROM keelson('"+lw.Handle+"') ORDER BY `id:kid:u64` FORMAT TabSeparated"))
	assert.Equal(t, "7\t0\tbye\t\\N\n42\t2\thi\tok",
		post("SELECT `id:kid:u64`, length(`sec:tags`), `s:pair`.`y:sub`, `note` "+
			"FROM keelson('"+lw.Handle+"') ORDER BY `id:kid:u64` FORMAT TabSeparated"))

	// A republish is seen immediately (no /query result caching, single-use workers).
	_, err = adhoc.Publish(adhocdata.PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: adhocInt64Stream(t, 100)})
	require.NoError(t, err)
	assert.Equal(t, "100", post("SELECT sum(v) FROM keelson('"+res.Handle+"') FORMAT TabSeparated"))
}

// adhocLeewayStream builds a 2-row Arrow stream with a leeway-shaped schema
// (colon-laden names, an Array(String) section, a nested Struct, a Nullable
// scalar) for the url()-path round-trip.
func adhocLeewayStream(t *testing.T) []byte {
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

func adhocInt64Stream(t *testing.T, vals ...int64) []byte {
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
