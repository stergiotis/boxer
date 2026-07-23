package adhocdata

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

type fakeKeys struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func newFakeKeys() *fakeKeys { return &fakeKeys{keys: make(map[string][]byte)} }

func (f *fakeKeys) RegisterDatasetKey(name string, key []byte) {
	f.mu.Lock()
	f.keys[name] = append([]byte(nil), key...)
	f.mu.Unlock()
}

func (f *fakeKeys) DeregisterDatasetKey(name string) {
	f.mu.Lock()
	delete(f.keys, name)
	f.mu.Unlock()
}

func (f *fakeKeys) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.keys[name]
	return ok
}

func testLogger(t *testing.T) zerolog.Logger { return zerolog.New(zerolog.NewTestWriter(t)) }

func int64Stream(t *testing.T, nullable bool, vals ...int64) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64, Nullable: nullable}}, nil)
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

// unsupportedStream builds a one-column Arrow stream whose type (LargeString)
// stays outside the publish gate's supported set, so Publish must reject it.
func unsupportedStream(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.BinaryTypes.LargeString}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.LargeStringBuilder).Append("x")
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(Config{
		Registry: introspect.NewRegistry(),
		Keys:     newFakeKeys(),
		Dir:      t.TempDir(),
		Log:      testLogger(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	return svc
}

func TestServiceLifecycle(t *testing.T) {
	dir := t.TempDir()
	keys := newFakeKeys()
	reg := introspect.NewRegistry()
	svc, err := NewService(Config{Registry: reg, Keys: keys, Dir: dir, Log: testLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1, 2, 3)})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(res.Handle, "adhoc_"), res.Handle)
	assert.Equal(t, uint64(1), res.Revision)
	assert.Equal(t, uint64(3), res.Rows)
	assert.Positive(t, res.Bytes)

	_, ok := reg.Lookup(res.Handle)
	assert.True(t, ok, "handle registered as a provider")
	assert.True(t, keys.has(res.Handle), "key registered with the broker")
	assert.FileExists(t, filepath.Join(dir, res.Handle+".bxad"))

	g, err := svc.Grant(res.Handle)
	require.NoError(t, err)
	assert.Equal(t, "items", g.Alias)
	assert.Equal(t, uint64(1), g.Revision)
	assert.Equal(t, "`v` Int64", g.Structure)

	// Republish: same handle, bumped revision, entry updated in place.
	res2, err := svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 4, 5)})
	require.NoError(t, err)
	assert.Equal(t, res.Handle, res2.Handle)
	assert.Equal(t, uint64(2), res2.Revision)
	assert.Equal(t, uint64(2), res2.Rows)
	p, _ := reg.Lookup(res.Handle)
	assert.Equal(t, uint64(2), p.(introspect.EncryptedDatasetI).Revision())

	// Retract: provider, key, and file all gone.
	require.NoError(t, svc.Retract(res.Handle))
	_, ok = reg.Lookup(res.Handle)
	assert.False(t, ok)
	assert.False(t, keys.has(res.Handle))
	assert.NoFileExists(t, filepath.Join(dir, res.Handle+".bxad"))

	require.Error(t, svc.Retract("adhoc_missing"))
	_, err = svc.Grant("adhoc_missing")
	require.Error(t, err)
}

func TestServiceRejections(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.Publish(PublishInput{Alias: "bad-alias", ArrowIPCStream: int64Stream(t, false, 1)})
	require.Error(t, err, "invalid alias")

	_, err = svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: unsupportedStream(t)})
	require.Error(t, err, "a type outside the supported set is rejected")

	_, err = svc.Publish(PublishInput{Alias: "items", Handle: "adhoc_nope", ArrowIPCStream: int64Stream(t, false, 1)})
	require.Error(t, err, "republish of an unknown handle")

	_, err = svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: []byte("not an arrow stream")})
	require.Error(t, err, "undecodable arrow bytes")
}

func TestServiceCountQuota(t *testing.T) {
	svc := newTestService(t)
	for i := range MaxDatasets {
		_, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, int64(i))})
		require.NoErrorf(t, err, "publish %d", i)
	}
	_, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 999)})
	require.Error(t, err, "the dataset past MaxDatasets must be refused")
}

func TestCheckQuotaLocked(t *testing.T) {
	svc := &Service{datasets: make(map[string]*dataset)}

	// Byte budget.
	svc.totalBytes = StoreMaxBytes - 10
	require.NoError(t, svc.checkQuotaLocked(nil, 10))
	require.Error(t, svc.checkQuotaLocked(nil, 11))

	// Count budget; a republish (existing != nil) does not add to the count.
	svc.totalBytes = 0
	for i := range MaxDatasets {
		svc.datasets[fmt.Sprintf("d%d", i)] = &dataset{}
	}
	require.Error(t, svc.checkQuotaLocked(nil, 1), "a new dataset past the count exceeds")
	require.NoError(t, svc.checkQuotaLocked(svc.datasets["d0"], 1), "republish keeps the count")
}

func TestSweepOnStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "adhoc_stale.bxad"), []byte("crash residue"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover.tmp"), []byte("x"), 0o600))

	svc, err := NewService(Config{Registry: introspect.NewRegistry(), Keys: newFakeKeys(), Dir: dir, Log: testLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "sweep removes crash residue on start")
}

func TestNewHandleShape(t *testing.T) {
	h, err := newHandle()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "adhoc_"))
	assert.Len(t, h, len("adhoc_")+16)
	assert.True(t, introspect.NewRegistry().Register(introspect.NewEncryptedEntry(h, arrow.NewSchema(nil, nil), "", "", 1)) == nil,
		"a minted handle is a valid table name")
}
