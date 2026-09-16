package adhocdata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

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
		Dir:      t.TempDir(),
		Log:      testLogger(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	return svc
}

// readAll opens the registered sealed provider and reads it back.
func readAll(t *testing.T, reg *introspect.Registry, handle string) (plain []byte, revision uint64) {
	t.Helper()
	p, ok := reg.Lookup(handle)
	require.True(t, ok)
	rc, rev, err := p.(introspect.EncryptedDatasetI).Open()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	plain, err = io.ReadAll(rc)
	require.NoError(t, err)
	return plain, rev
}

func TestServiceLifecycle(t *testing.T) {
	dir := t.TempDir()
	reg := introspect.NewRegistry()
	svc, err := NewService(Config{Registry: reg, Dir: dir, Log: testLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1, 2, 3)})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(res.Handle, "adhoc_"), res.Handle)
	assert.Equal(t, uint64(1), res.Revision)
	assert.Equal(t, uint64(3), res.Rows)
	assert.Positive(t, res.Bytes)

	p, ok := reg.Lookup(res.Handle)
	require.True(t, ok, "handle registered as a provider")
	enc := p.(introspect.EncryptedDatasetI)
	assert.Equal(t, "`v` Int64", enc.Structure())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a sealed dataset has no name in the store directory")
	plain, rev := readAll(t, reg, res.Handle)
	assert.Equal(t, uint64(1), rev)
	assert.Equal(t, int64Stream(t, false, 1, 2, 3), plain, "the sealed bytes are the publisher's stream")

	// Republish: same handle, bumped revision, record updated in place, the
	// previous file retired.
	res2, err := svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 4, 5)})
	require.NoError(t, err)
	assert.Equal(t, res.Handle, res2.Handle)
	assert.Equal(t, uint64(2), res2.Revision)
	assert.Equal(t, uint64(2), res2.Rows)
	assert.Equal(t, uint64(2), enc.Revision())
	plain, rev = readAll(t, reg, res.Handle)
	assert.Equal(t, uint64(2), rev)
	assert.Equal(t, int64Stream(t, false, 4, 5), plain)

	// Retract is two-phase (ADR-0188 §SD3, ADR-0240 §SD4). LEAVE: the
	// record stops resolving at once, but the provider stays for the grace
	// so an already-resolved query completes.
	require.NoError(t, svc.Retract(res.Handle, Identity{}))
	_, rErr := svc.Resolve("items")
	require.Error(t, rErr, "a retracted dataset no longer resolves")
	assert.False(t, svc.IsLive(res.Handle))
	_, ok = reg.Lookup(res.Handle)
	assert.True(t, ok, "the provider stays queryable during the grace")
	_, _ = readAll(t, reg, res.Handle)
	// UNLOAD (run early here): provider gone, file closed.
	svc.FlushRetracts()
	_, ok = reg.Lookup(res.Handle)
	assert.False(t, ok)
	_, _, err = enc.Open()
	require.Error(t, err, "the sealed file is closed")

	require.Error(t, svc.Retract("adhoc_missing", Identity{}))
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

	_, err = svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: make([]byte, PerDatasetMaxBytes+1)})
	require.Error(t, err, "an oversize stream is refused before it is decoded")
	assert.Equal(t, 0, svc.LiveCount())
}

// TestServiceOwnership pins ADR-0240 §SD2/§SD5: a republish or retract by
// any identity but the publisher's is refused; the runtime may do either;
// a dataset kept after close belongs to the app, so any of its instances
// may.
func TestServiceOwnership(t *testing.T) {
	svc := newTestService(t)
	owner := Identity{App: "app.a", Instance: 1}
	sibling := Identity{App: "app.a", Instance: 2}
	other := Identity{App: "app.b", Instance: 1}

	res, err := svc.Publish(PublishInput{Alias: "items", By: owner, ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	_, err = svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, By: other, ArrowIPCStream: int64Stream(t, false, 2)})
	require.ErrorIs(t, err, ErrNotOwner, "another app cannot republish")
	_, err = svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, By: sibling, ArrowIPCStream: int64Stream(t, false, 2)})
	require.ErrorIs(t, err, ErrNotOwner, "another instance of the same app cannot either")
	require.ErrorIs(t, svc.Retract(res.Handle, other), ErrNotOwner)
	_, err = svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, By: owner, ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err, "the publisher republishes")
	require.NoError(t, svc.Retract(res.Handle, Identity{}), "the runtime retracts anything")

	kept, err := svc.Publish(PublishInput{Alias: "prof", By: owner, KeepAfterClose: true, ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	_, err = svc.Publish(PublishInput{Alias: "prof", Handle: kept.Handle, By: sibling, ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err, "kept after close: the app's, so a sibling instance republishes")
	require.ErrorIs(t, svc.Retract(kept.Handle, other), ErrNotOwner)
	require.NoError(t, svc.Retract(kept.Handle, sibling))
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
	svc := &Service{live: make(map[string]*record)}

	// Byte budget.
	svc.totalBytes = StoreMaxBytes - 10
	require.NoError(t, svc.checkQuotaLocked(nil, 10))
	require.Error(t, svc.checkQuotaLocked(nil, 11))

	// Count budget; a republish (existing != nil) does not add to the count.
	svc.totalBytes = 0
	for i := range MaxDatasets {
		svc.live[fmt.Sprintf("d%d", i)] = &record{}
	}
	require.Error(t, svc.checkQuotaLocked(nil, 1), "a new dataset past the count exceeds")
	require.NoError(t, svc.checkQuotaLocked(svc.live["d0"], 1), "republish keeps the count")
}

// TestQuotaAccountingSurvivesRetractAndRepublish pins the invariant the v1
// service broke under a republish racing a retract: the byte total equals
// the sum of live datasets after any interleaving.
func TestQuotaAccountingSurvivesRetractAndRepublish(t *testing.T) {
	svc := newTestService(t)
	var handles []string
	for i := range 8 {
		res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, int64(i))})
		require.NoError(t, err)
		handles = append(handles, res.Handle)
	}
	var wg sync.WaitGroup
	for _, h := range handles {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = svc.Publish(PublishInput{Alias: "items", Handle: h, ArrowIPCStream: int64Stream(t, false, 1, 2, 3, 4)})
		}()
		go func() {
			defer wg.Done()
			_ = svc.Retract(h, Identity{})
		}()
	}
	wg.Wait()
	svc.FlushRetracts()
	svc.mu.RLock()
	var sum uint64
	for _, r := range svc.live {
		sum += r.bytes
	}
	assert.Equal(t, sum, svc.totalBytes, "the byte total is the sum of the live datasets")
	svc.mu.RUnlock()
}

// TestConcurrentRepublishesKeepOneRevisionPerFile: two republishes of one
// handle land as two revisions, each readable as a whole stream.
func TestConcurrentRepublishesKeepOneRevisionPerFile(t *testing.T) {
	reg := introspect.NewRegistry()
	svc, err := NewService(Config{Registry: reg, Dir: t.TempDir(), Log: testLogger(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 0)})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, int64(i), int64(i))})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	p, _ := reg.Lookup(res.Handle)
	assert.Equal(t, uint64(9), p.(introspect.EncryptedDatasetI).Revision())
	plain, rev := readAll(t, reg, res.Handle)
	assert.Equal(t, uint64(9), rev)
	rdr, err := ipc.NewReader(bytes.NewReader(plain))
	require.NoError(t, err, "the final file is one whole stream")
	rdr.Release()
}

func TestPublishAfterCloseRefused(t *testing.T) {
	svc := newTestService(t)
	require.NoError(t, svc.Close(context.Background()))
	_, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.ErrorIs(t, err, ErrClosed)
	_, err = svc.Resolve("items")
	require.ErrorIs(t, err, ErrClosed)
	require.ErrorIs(t, svc.Retract("adhoc_x", Identity{}), ErrClosed)
	require.NoError(t, svc.Close(context.Background()), "close is idempotent")
}

// TestRepublishKeepsOldRevisionForOpenReaders: a reader opened before a
// republish reads the revision it opened, to the end.
func TestRepublishKeepsOldRevisionForOpenReaders(t *testing.T) {
	reg := introspect.NewRegistry()
	svc, err := NewService(Config{Registry: reg, Dir: t.TempDir(), Log: testLogger(t), RetractGrace: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	p, _ := reg.Lookup(res.Handle)
	rc, rev, err := p.(introspect.EncryptedDatasetI).Open()
	require.NoError(t, err)
	assert.Equal(t, uint64(1), rev)
	_, err = svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: int64Stream(t, false, 2, 3)})
	require.NoError(t, err)
	old, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, int64Stream(t, false, 1), old, "the open reader still sees revision 1")
	require.NoError(t, rc.Close())
}

func TestNewHandleShape(t *testing.T) {
	h, err := newHandle()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "adhoc_"))
	assert.Len(t, h, len("adhoc_")+16)
	assert.True(t, introspect.ValidTableName(h), "a minted handle is a valid table name")
}
