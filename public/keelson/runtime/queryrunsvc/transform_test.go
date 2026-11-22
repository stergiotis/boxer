package queryrunsvc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// A transform the capture cannot use is refused before any statement is
// sent: one that fails on a query-run batch, names no database, or returns
// no batch. The client points at a closed port, so the refusal does not
// depend on the server, and says it could not settle whether an earlier
// binary left a view behind.
func TestReconcileRefusesATransformTheCaptureCannotUse(t *testing.T) {
	for name, fn := range map[string]recordstore.InsertTransformFunc{
		"fails": func(_ string, _ []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "", nil, assert.AnError
		},
		"bare target": func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			records[0].Retain()
			return "carrier", records, nil
		},
		"no batch": func(_ string, _ []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "scratch_qr.carrier", nil, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := New(Config{Database: "scratch_qr", Table: "facts", ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
			require.NoError(t, err)
			unregister := recordstore.RegisterInsertTransform("scratch_qr.facts", fn)
			defer unregister()
			err = s.Reconcile(context.Background())
			require.ErrorIs(t, err, recordstore.ErrTransformedTable)
			require.ErrorIs(t, err, errNoCapture)
			assert.Contains(t, err.Error(), "could not be settled")
			assert.Nil(t, s.route.Load(), "a refused reconcile serves no transformed batch")
		})
	}
}

// The read relation defaults to BOXER_FACTS_READ_TABLE for boxer.facts and to
// the destination for any other table; a malformed one fails New, as it
// fails a host at boot.
func TestNewResolvesTheReadRelation(t *testing.T) {
	envtest.Set(t, factsschema.ReadTableSetting, "carrier.facts_view")
	s, err := New(Config{ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, "carrier.facts_view", s.cfg.ReadTable)

	s, err = New(Config{Database: "scratch_qr", ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	assert.Equal(t, "scratch_qr.facts", s.cfg.ReadTable, "a scratch destination reads itself")

	_, err = New(Config{Database: "scratch_qr", ReadTable: "no database", ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.Error(t, err)
}

// transformedService is a service whose route was settled by probing the
// reference carrier's transform, as a reconcile does before its first
// statement; servePull is reached through a test server.
func transformedService(t *testing.T, fn recordstore.InsertTransformI) (s *Service, srv *httptest.Server, rows *atomic.Pointer[[]queryrunfacts.Row]) {
	t.Helper()
	s, err := New(Config{Database: "scratch_qr", Table: "facts", ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	unregister := recordstore.RegisterInsertTransform("scratch_qr.facts", fn)
	t.Cleanup(unregister)
	route, err := s.probeRoute()
	require.NoError(t, err)
	s.route.Store(route)
	rows = &atomic.Pointer[[]queryrunfacts.Row]{}
	rows.Store(&[]queryrunfacts.Row{})
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.servePull(w, r, *rows.Load())
	}))
	t.Cleanup(srv.Close)
	return
}

func referenceCarrier(t *testing.T) (c *chstoretest.Carrier) {
	t.Helper()
	contract, err := chstore.SchemaContract(chstore.Config{Database: "scratch_qr", Table: "facts"})
	require.NoError(t, err)
	return chstoretest.NewCarrier(contract.Columns, "scratch_qr.carrier", "scratch_qr.facts_view", "boxer")
}

func probeRows(n int) (rows []queryrunfacts.Row) {
	for i := range n {
		r := queryrunfacts.ProbeRow()
		r.EventUs += int64(i)
		rows = append(rows, r)
	}
	return
}

// readPull reads one /pull answer as an Arrow stream.
func readPull(t *testing.T, url string) (schema *arrow.Schema, n int64) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rd, err := ipc.NewReader(resp.Body)
	require.NoError(t, err)
	defer rd.Release()
	schema = rd.Schema()
	for rd.Next() {
		n += rd.RecordBatch().NumRows()
	}
	require.NoError(t, rd.Err())
	return
}

// Under a transform /pull serves the transform's output: the carrier's
// columns, every row, and a schema-only stream in the carrier's schema when
// there is nothing to serve. The ETag guards a ranged continuation as it
// does for the facts columns.
func TestPullUnderATransformServesTheTargetsShape(t *testing.T) {
	c := referenceCarrier(t)
	_, srv, rows := transformedService(t, c.Transform())

	schema, n := readPull(t, srv.URL)
	assert.Zero(t, n)
	require.Equal(t, "tenant", schema.Field(0).Name, "an empty pull is in the target's schema")

	three := probeRows(3)
	rows.Store(&three)
	schema, n = readPull(t, srv.URL)
	assert.EqualValues(t, 3, n)
	require.Equal(t, "tenant", schema.Field(0).Name)
	assert.Equal(t, "c0", schema.Field(1).Name)

	get := func(hdr map[string]string) (resp *http.Response) {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return
	}
	etag := get(nil).Header.Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, etag, get(nil).Header.Get("ETag"), "a pure transform keeps an unchanged answer's ETag")
	require.Equal(t, http.StatusPartialContent, get(map[string]string{"Range": "bytes=8-", "If-Range": etag}).StatusCode)
	require.Equal(t, http.StatusOK, get(map[string]string{"Range": "bytes=8-", "If-Range": `"stale"`}).StatusCode)
}

// A pull whose batch the transform sends to another table, or returns in
// another schema, than the reconcile settled fails: the view would append
// it to the reconciled table.
func TestPullRefusesABatchTheViewWasNotComposedFor(t *testing.T) {
	carrier := referenceCarrier(t).Transform()
	routeByRows := recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		_, out, err := carrier.TransformInsert(table, records)
		if records[0].NumRows() > 1 {
			return "scratch_qr.carrier_many", out, err
		}
		return "scratch_qr.carrier", out, err
	})
	addColumn := recordstore.InsertTransformFunc(func(table string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		target, out, err := carrier.TransformInsert(table, records)
		if err != nil || records[0].NumRows() == 1 {
			return target, out, err
		}
		b := array.NewUint8Builder(memory.NewGoAllocator())
		defer b.Release()
		for range out[0].NumRows() {
			b.Append(1)
		}
		extra := b.NewArray()
		defer extra.Release()
		fields := append(out[0].Schema().Fields(), arrow.Field{Name: "extra", Type: arrow.PrimitiveTypes.Uint8})
		widened := array.NewRecordBatch(arrow.NewSchema(fields, nil), append(out[0].Columns(), extra), out[0].NumRows())
		out[0].Release()
		return target, []arrow.RecordBatch{widened}, nil
	})
	for name, fn := range map[string]recordstore.InsertTransformI{"another table": routeByRows, "another schema": addColumn} {
		t.Run(name, func(t *testing.T) {
			_, srv, rows := transformedService(t, fn)
			two := probeRows(2)
			rows.Store(&two)
			resp, err := http.Get(srv.URL)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		})
	}
}
