package chlocalbroker

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
)

// --- unit tests (no clickhouse) ---

type fakeKeyStore map[string][]byte

func (f fakeKeyStore) LookupDatasetKey(name string) (key []byte, ok bool) {
	key, ok = f[name]
	return
}

func TestFoldEncryptedInputsKeyDiscipline(t *testing.T) {
	base := computeCacheKey("SELECT * FROM ds", "TabSeparated", nil)

	// No encrypted inputs: base passes through.
	assert.Equal(t, base, foldEncryptedInputs(base, nil))

	// Deterministic across map order.
	a := map[string]EncryptedInputRef{"x": {Path: "/p/x", Revision: 1}, "y": {Path: "/p/y", Revision: 2}}
	b := map[string]EncryptedInputRef{"y": {Path: "/p/y", Revision: 2}, "x": {Path: "/p/x", Revision: 1}}
	assert.Equal(t, foldEncryptedInputs(base, a), foldEncryptedInputs(base, b))
	assert.NotEqual(t, base, foldEncryptedInputs(base, a))

	// Only (name, revision) folds: a different path or structure at the
	// same revision keys identically (a legitimate cache hit).
	same := map[string]EncryptedInputRef{"x": {Path: "/other", Structure: "z String", Revision: 1}}
	assert.Equal(t,
		foldEncryptedInputs(base, map[string]EncryptedInputRef{"x": {Path: "/p/x", Structure: "a Int64", Revision: 1}}),
		foldEncryptedInputs(base, same),
		"path and structure must not perturb the key; only (name, revision) does")

	// A bumped revision is a different key (republish invalidates).
	assert.NotEqual(t,
		foldEncryptedInputs(base, map[string]EncryptedInputRef{"x": {Revision: 1}}),
		foldEncryptedInputs(base, map[string]EncryptedInputRef{"x": {Revision: 2}}))

	// Domain separation from the params and input-table folds: the same
	// name must not alias across the three.
	ek := foldEncryptedInputs(base, map[string]EncryptedInputRef{"a": {Revision: 0}})
	pk := foldParams(base, map[string]string{"a": ""})
	tk := foldInputTables(base, map[string][]byte{"a": nil})
	assert.NotEqual(t, ek, pk)
	assert.NotEqual(t, ek, tk)
	assert.NotEqual(t, pk, tk)
}

func TestMaterializeEncryptedInputs_Validation(t *testing.T) {
	dir := t.TempDir()
	keys := fakeKeyStore{"good": bytes.Repeat([]byte{1}, adhocdata.KeySize)}

	// Bad name: rejected before any pipe or goroutine.
	_, _, cleanup, err := materializeEncryptedInputs(context.Background(), dir,
		map[string]EncryptedInputRef{"bad-name": {Structure: "a Int64"}}, keys)
	cleanup()
	require.Error(t, err)

	// No registered key: rejected.
	_, _, cleanup, err = materializeEncryptedInputs(context.Background(), dir,
		map[string]EncryptedInputRef{"nokey": {Structure: "a Int64"}}, keys)
	cleanup()
	require.Error(t, err)
}

func TestWireRequestEncryptedInputsRoundTrip(t *testing.T) {
	in := map[string]EncryptedInputRef{"ds": {Path: "/a/b.bxad", Structure: "id Int64", Revision: 7}}
	raw, err := encodeRequest(wireRequest{SQL: "SELECT * FROM ds", EncryptedInputs: in})
	require.NoError(t, err)
	req, err := decodeRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, in, req.EncryptedInputs)
}

// --- live tests (gated on clickhouse-local) ---

// arrowStream renders schema/build to Arrow IPC stream bytes.
func arrowStream(t *testing.T, schema *arrow.Schema, build func(rb *array.RecordBuilder)) []byte {
	t.Helper()
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	build(rb)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// encFile encrypts plaintext to a fresh .bxad file under a temp dir and
// returns its path.
func encFile(t *testing.T, key, plaintext []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ds.bxad")
	f, err := os.Create(path)
	require.NoError(t, err)
	w, err := adhocdata.NewWriter(f, key)
	require.NoError(t, err)
	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path
}

func testAEADKey() []byte { return bytes.Repeat([]byte{0xAB}, adhocdata.KeySize) }

func idNameSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
}

func TestExecOnPool_EncryptedRoundTrip(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := idNameSchema()
	stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
		rb.Field(1).(*array.StringBuilder).AppendValues([]string{"alice", "bob"}, nil)
	})
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	path := encFile(t, key, stream)
	svc.KeyStore().RegisterDatasetKey("ds", key)

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:    "SELECT id, name FROM ds ORDER BY id",
		Format: "TabSeparated",
		EncryptedInputs: map[string]EncryptedInputRef{
			"ds": {Path: path, Structure: structure, Revision: 1},
		},
	})
	require.NoError(t, err)
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	assert.Equal(t, "1\talice\n2\tbob\n", string(body))
}

// TestExecOnPool_EncryptedLarge exercises the blocking-write path: a
// dataset larger than the pipe buffer, so the writer parks and resumes
// as clickhouse-local drains.
func TestExecOnPool_EncryptedLarge(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	const n = 50000 // ~400 KiB of Arrow, well past the 64 KiB pipe buffer
	ids := make([]int64, n)
	var want int64
	for i := range ids {
		ids[i] = int64(i)
		want += int64(i)
	}
	stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	})
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	path := encFile(t, key, stream)
	svc.KeyStore().RegisterDatasetKey("big", key)

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:             "SELECT count(), sum(id) FROM big",
		Format:          "TabSeparated",
		EncryptedInputs: map[string]EncryptedInputRef{"big": {Path: path, Structure: structure, Revision: 1}},
	})
	require.NoError(t, err)
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	assert.Equal(t, "50000\t"+strconv.FormatInt(want, 10)+"\n", string(body))
}

func TestExecOnPool_EncryptedWrongKeyFails(t *testing.T) {
	svc, caller := newTestBroker(t)
	schema := idNameSchema()
	stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1}, nil)
		rb.Field(1).(*array.StringBuilder).AppendValues([]string{"x"}, nil)
	})
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	path := encFile(t, testAEADKey(), stream)
	// Register a DIFFERENT key than the file was sealed with.
	svc.KeyStore().RegisterDatasetKey("ds", bytes.Repeat([]byte{0x01}, adhocdata.KeySize))

	before := runtime.NumGoroutine()
	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:             "SELECT * FROM ds",
		Format:          "TabSeparated",
		EncryptedInputs: map[string]EncryptedInputRef{"ds": {Path: path, Structure: structure, Revision: 1}},
	})
	require.NoError(t, err, "transport succeeds; the broker replies with a structured error")
	_, _ = io.ReadAll(rep)
	require.Error(t, rep.Err(), "a wrong key must fail the request even if the worker exited 0")

	// The streaming goroutine must not leak.
	assertGoroutinesSettle(t, before)
}

func TestExecOnPool_EncryptedTruncatedFails(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := idNameSchema()
	stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
		rb.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b", "c"}, nil)
	})
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	path := encFile(t, key, stream)
	// Chop bytes off the end: the decrypt reader must detect truncation.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(path, fi.Size()-20))
	svc.KeyStore().RegisterDatasetKey("ds", key)

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:             "SELECT * FROM ds",
		Format:          "TabSeparated",
		EncryptedInputs: map[string]EncryptedInputRef{"ds": {Path: path, Structure: structure, Revision: 1}},
	})
	require.NoError(t, err)
	_, _ = io.ReadAll(rep)
	require.Error(t, rep.Err(), "a truncated ciphertext must fail the request")
}

func TestExecOnPool_TwoEncryptedTables(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	mk := func(name string, vals []int64) EncryptedInputRef {
		stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
			rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
		})
		p := encFile(t, key, stream)
		svc.KeyStore().RegisterDatasetKey(name, key)
		return EncryptedInputRef{Path: p, Structure: structure, Revision: 1}
	}

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:    "SELECT (SELECT sum(v) FROM a) + (SELECT sum(v) FROM b)",
		Format: "TabSeparated",
		EncryptedInputs: map[string]EncryptedInputRef{
			"a": mk("a", []int64{1, 2, 3}),
			"b": mk("b", []int64{10, 20}),
		},
	})
	require.NoError(t, err)
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	assert.Equal(t, "36", strings.TrimSpace(string(body)))
}

func TestExecOnPool_EncryptedCacheKeying(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)
	stream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
	})
	path := encFile(t, key, stream)
	svc.KeyStore().RegisterDatasetKey("ds", key)

	run := func(rev uint64) (body string, hit bool) {
		rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
			SQL:             "SELECT count() FROM ds",
			Format:          "TabSeparated",
			Cacheable:       true,
			EncryptedInputs: map[string]EncryptedInputRef{"ds": {Path: path, Structure: structure, Revision: rev}},
		})
		require.NoError(t, err)
		b, err := io.ReadAll(rep)
		require.NoError(t, err)
		require.NoError(t, rep.Err())
		return strings.TrimSpace(string(b)), rep.CacheHit
	}

	body, hit := run(1)
	assert.Equal(t, "4", body)
	assert.False(t, hit, "first run misses")

	body, hit = run(1)
	assert.Equal(t, "4", body)
	assert.True(t, hit, "same (name, revision) serves from cache")

	body, hit = run(2)
	assert.Equal(t, "4", body)
	assert.False(t, hit, "a bumped revision must miss")
}

// TestExecOnPool_EncryptedStuckWriter is the plan's riskiest concurrency
// case: the worker errors before draining a second encrypted input, so
// its writer is stuck waiting to open the pipe. wait() must cancel and
// return promptly, not hang until the bus timeout.
func TestExecOnPool_EncryptedStuckWriter(t *testing.T) {
	svc, caller := newTestBroker(t)
	key := testAEADKey()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	structure, err := adhocdata.StructureFor(schema)
	require.NoError(t, err)

	// Table `a` is deliberately garbage: sealed correctly (so it decrypts)
	// but not a valid Arrow stream, so clickhouse-local errors reading it
	// and never reaches the CREATE for `b`, stranding b's writer.
	garbage := encFile(t, key, []byte("this is not an arrow stream, at all"))
	svc.KeyStore().RegisterDatasetKey("a", key)

	goodStream := arrowStream(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendValues([]int64{1}, nil)
	})
	good := encFile(t, key, goodStream)
	svc.KeyStore().RegisterDatasetKey("b", key)

	before := runtime.NumGoroutine()
	done := make(chan struct{})
	var repErr error
	go func() {
		defer close(done)
		rep, e := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
			SQL:    "SELECT (SELECT sum(v) FROM a) + (SELECT sum(v) FROM b)",
			Format: "TabSeparated",
			EncryptedInputs: map[string]EncryptedInputRef{
				"a": {Path: garbage, Structure: structure, Revision: 1},
				"b": {Path: good, Structure: structure, Revision: 1},
			},
		})
		if e != nil {
			repErr = e
			return
		}
		_, _ = io.ReadAll(rep)
		repErr = rep.Err()
	}()

	select {
	case <-done:
		// Must be a failure, and it must have arrived well before the
		// 15s bus timeout — proving wait() did not hang.
		require.Error(t, repErr)
	case <-time.After(10 * time.Second):
		t.Fatal("request did not return promptly: wait() likely hung on the stranded writer")
	}
	assertGoroutinesSettle(t, before)
}

// assertGoroutinesSettle polls until the goroutine count returns near a
// baseline, proving streaming goroutines terminated rather than leaked.
func assertGoroutinesSettle(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+3 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutines did not settle: baseline %d, now %d", baseline, runtime.NumGoroutine())
}
