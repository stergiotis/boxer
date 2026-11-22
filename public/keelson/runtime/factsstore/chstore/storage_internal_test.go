package chstore

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// sentRequest is one request the fake server received: the statement, and
// for an insert the columns of the Arrow body.
type sentRequest struct {
	query string
	cols  []string
}

// fakeServer answers every statement with an empty body and records what it
// was sent. It stands in for ClickHouse so that a store's statements can be
// read without one.
type fakeServer struct {
	mu   sync.Mutex
	sent []sentRequest
}

func (inst *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s := sentRequest{query: r.URL.Query().Get("query")}
	if s.query == "" {
		s.query = string(body)
	} else if rdr, err := ipc.NewFileReader(bytes.NewReader(body)); err == nil {
		for _, f := range rdr.Schema().Fields() {
			s.cols = append(s.cols, f.Name)
		}
		_ = rdr.Close()
	}
	inst.mu.Lock()
	inst.sent = append(inst.sent, s)
	inst.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (inst *fakeServer) requests() (out []sentRequest) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = append(out, inst.sent...)
	inst.sent = nil
	return
}

// newFakeStore is a store over a fake server, minting ids in process so
// that no lease statement is sent.
func newFakeStore(t *testing.T, cfg Config) (st *Store, srv *fakeServer) {
	t.Helper()
	srv = &fakeServer{}
	hs := httptest.NewServer(http.HandlerFunc(srv.handle))
	t.Cleanup(hs.Close)
	cfg.URL = hs.URL + "/"
	cfg.RowIds.Allocator = memalloc.NewAllocator()
	st, err := New(cfg)
	require.NoError(t, err)
	return
}

// A write of the hand-written store reaches the server through the
// executor, so a transform registered for its table rewrites it (ADR-0402
// §SD1): the insert names the transform's table and carries its columns.
func TestWrites_GoThroughTheRegisteredTransform(t *testing.T) {
	st, srv := newFakeStore(t, Config{Database: "scratch_chstore_tr", Table: "facts"})
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var seenCols int
	unregister := recordstore.RegisterInsertTransform("scratch_chstore_tr.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			seenCols = int(records[0].NumCols())
			b := array.NewUint64Builder(alloc)
			defer b.Release()
			b.Append(uint64(records[0].NumRows()))
			arr := b.NewArray()
			defer arr.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "rows", Type: arrow.PrimitiveTypes.Uint64}}, nil)
			return "scratch_chstore_tr.carrier", []arrow.RecordBatch{array.NewRecordBatch(schema, []arrow.Array{arr}, 1)}, nil
		}))
	defer unregister()

	ctx := context.Background()
	_, err := st.WriteGrant(ctx, factsstore.GrantRow{AppId: "example.app", Pattern: "a.>", Direction: app.CapDirectionPub})
	require.NoError(t, err)
	_, err = st.WriteLogs(ctx, []factsstore.LogRow{{Level: "info", Message: "one"}, {Level: "info", Message: "two"}})
	require.NoError(t, err)

	got := srv.requests()
	require.Len(t, got, 2)
	for _, s := range got {
		assert.Equal(t, "INSERT INTO scratch_chstore_tr.carrier (`rows`) FORMAT Arrow", s.query)
		assert.Equal(t, []string{"rows"}, s.cols)
	}
	assert.Greater(t, seenCols, 20, "the transform receives the batches in the facts columns")

	unregister()
	_, err = st.WriteGrant(ctx, factsstore.GrantRow{AppId: "example.app", Pattern: "a.>", Direction: app.CapDirectionPub})
	require.NoError(t, err)
	got = srv.requests()
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0].query, "INSERT INTO scratch_chstore_tr.facts (`"), "with nothing registered the facts table is written, its columns named: %s", got[0].query)
}

// storeMethodsThatDoNotRead are the exported methods of Store that are not
// reads of facts: the writes, setup, and the statements that change or empty
// stored rows. Every other exported method is a reader, and the guard below
// calls it.
var storeMethodsThatDoNotRead = map[string]bool{
	"Ping": true, "SetupTable": true, "EnsureReadSurface": true,
	"WriteGrant": true, "WriteLog": true, "WriteLogs": true, "WriteRuntimeStart": true,
	"WriteRuntimeHeartbeat": true, "WriteAppLifecycle": true, "WriteLaunch": true,
	"Truncate": true, "DropTable": true, "RowIds": true,
}

// With a read table set, every reader of the store composes against it and
// none against the table written to (ADR-0402 §SD3). The readers are found
// by reflection, so one added later is held to the same rule without being
// listed here; a method that is not a read joins storeMethodsThatDoNotRead.
func TestReaders_NameTheReadTableAndNeverTheWrittenOne(t *testing.T) {
	const written, read = "wr_guard.facts", "rd_guard.facts_view"
	st, srv := newFakeStore(t, Config{Database: "wr_guard", Table: "facts", ReadTable: read})
	ctxType := reflect.TypeFor[context.Context]()
	v := reflect.ValueOf(st)
	readers := 0
	for i := range v.NumMethod() {
		m := v.Type().Method(i)
		if storeMethodsThatDoNotRead[m.Name] {
			continue
		}
		readers++
		args := make([]reflect.Value, 0, m.Type.NumIn()-1)
		for j := 1; j < m.Type.NumIn(); j++ {
			in := m.Type.In(j)
			if in == ctxType {
				args = append(args, reflect.ValueOf(context.Background()))
				continue
			}
			// A positive number for a numeric argument (a half-life, a
			// limit) and a non-empty string field in a filter (a run id),
			// which a reader may refuse empty before it sends.
			arg := reflect.New(in).Elem()
			switch {
			case arg.CanInt():
				arg.SetInt(int64(time.Hour))
			case arg.CanUint():
				arg.SetUint(10)
			case arg.Kind() == reflect.Struct:
				for f := range arg.NumField() {
					if fv := arg.Field(f); fv.Kind() == reflect.String && fv.CanSet() {
						fv.SetString("x")
					}
				}
			}
			args = append(args, arg)
		}
		v.Method(i).Call(args)
		got := srv.requests()
		require.NotEmpty(t, got, "%s sent nothing; if it is not a read of facts, list it in storeMethodsThatDoNotRead", m.Name)
		for _, s := range got {
			assert.Contains(t, s.query, read, "%s", m.Name)
			assert.NotContains(t, s.query, written, "%s composes against the table written to", m.Name)
		}
	}
	assert.GreaterOrEqual(t, readers, 7)

	_, err := st.WriteGrant(context.Background(), factsstore.GrantRow{AppId: "example.app", Pattern: "a.>", Direction: app.CapDirectionPub})
	require.NoError(t, err)
	got := srv.requests()
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0].query, "INSERT INTO "+written+" (`"), "writes still name the table written to: %s", got[0].query)
}

// A malformed read table is refused when the store is built, not spliced
// into a statement.
func TestNew_RefusesAMalformedReadTable(t *testing.T) {
	_, err := New(Config{URL: "http://127.0.0.1:1/", Database: "boxer", Table: "facts", ReadTable: "a.b; DROP TABLE c"})
	require.Error(t, err)
}

// The read table setting reaches a config for the facts table and not one
// pointed at a scratch table.
func TestConfigWithEnvDefaults_TheReadTableFollowsTheFactsTableOnly(t *testing.T) {
	envtest.Set(t, factsschema.ReadTableSetting, "carrier.facts_view")
	assert.Equal(t, "carrier.facts_view", ConfigWithEnvDefaults(Config{}).ReadTable)
	assert.Empty(t, ConfigWithEnvDefaults(Config{Database: "scratch", Table: "facts"}).ReadTable)
	assert.Equal(t, "carrier.facts_view", ConfigFromEnv().ReadTable)
}

// passThrough is a transform that sends the batches as they are to target.
func passThrough(target string) recordstore.InsertTransformFunc {
	return func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		for _, r := range records {
			r.Retain()
		}
		return target, records, nil
	}
}

// Under a transform setup creates no facts table — only its database — and
// the statements that change or empty stored rows by its name are refused
// before anything is sent (ADR-0402 §SD5).
func TestUnderATransform_NoFactsDDLAndTheRefusals(t *testing.T) {
	cfg := Config{Database: "scratch_sd5", Table: "facts"}
	st, srv := newFakeStore(t, cfg)
	unregister := recordstore.RegisterInsertTransform("scratch_sd5.facts", passThrough("scratch_sd5.carrier"))
	defer unregister()

	_ = st.SetupTable(context.Background(), "")
	sent := srv.requests()
	require.NotEmpty(t, sent)
	assert.Equal(t, "CREATE DATABASE IF NOT EXISTS scratch_sd5", sent[0].query)
	for _, s := range sent {
		assert.NotContains(t, s.query, "CREATE TABLE IF NOT EXISTS scratch_sd5.facts")
		assert.NotContains(t, s.query, "idlease", "an allocator was given, so no lease table")
	}

	stmts, err := SetupStatements(Config{Database: "scratch_sd5", Table: "facts"}, "")
	require.NoError(t, err)
	assert.Equal(t, "CREATE DATABASE IF NOT EXISTS scratch_sd5", stmts[0])
	for _, s := range stmts {
		assert.NotContains(t, s, "scratch_sd5.facts")
	}

	require.ErrorIs(t, st.Truncate(context.Background()), recordstore.ErrTransformedTable)
	require.ErrorIs(t, st.DropTable(context.Background()), recordstore.ErrTransformedTable)
	assert.Empty(t, srv.requests(), "a refused statement sends nothing")

	unregister()
	stmts, err = SetupStatements(Config{Database: "scratch_sd5", Table: "facts"}, "")
	require.NoError(t, err)
	assert.Contains(t, stmts[1], "CREATE TABLE IF NOT EXISTS scratch_sd5.facts", "without a transform the table is created")
}

// The schema check judges the table written to in full, and a read relation
// other than it by its columns; under a transform only the read relation
// (ADR-0402 §SD4).
func TestSchemaContracts(t *testing.T) {
	base := Config{Database: "scratch_sc", Table: "facts"}
	cs, err := SchemaContracts(base)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "scratch_sc.facts", cs[0].Table)
	assert.False(t, cs[0].ColumnsOnly)

	withRead := base
	withRead.ReadTable = "carrier.facts_view"
	cs, err = SchemaContracts(withRead)
	require.NoError(t, err)
	require.Len(t, cs, 2)
	assert.Equal(t, "scratch_sc.facts", cs[0].Table)
	assert.False(t, cs[0].ColumnsOnly)
	assert.Equal(t, "carrier.facts_view", cs[1].Table)
	assert.True(t, cs[1].ColumnsOnly)
	assert.Empty(t, cs[1].Stamp)
	assert.True(t, cs[1].Layout.IsZero())
	assert.Equal(t, cs[0].Columns, cs[1].Columns)

	unregister := recordstore.RegisterInsertTransform("scratch_sc.facts", passThrough("scratch_sc.carrier"))
	defer unregister()
	cs, err = SchemaContracts(base)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "scratch_sc.facts", cs[0].Table, "the name itself, which the deployment may have made its view")
	assert.True(t, cs[0].ColumnsOnly)
	cs, err = SchemaContracts(withRead)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "carrier.facts_view", cs[0].Table)
	assert.True(t, cs[0].ColumnsOnly)
}
