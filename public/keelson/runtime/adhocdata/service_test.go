package adhocdata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	captureAudits(svc)
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
	require.NoError(t, svc.checkQuotaLocked(nil, 10, Identity{}))
	require.Error(t, svc.checkQuotaLocked(nil, 11, Identity{}))

	// Count budget; a republish (existing != nil) does not add to the count.
	svc.totalBytes = 0
	for i := range MaxDatasets {
		svc.live[fmt.Sprintf("d%d", i)] = &record{}
	}
	require.Error(t, svc.checkQuotaLocked(nil, 1, Identity{}), "a new dataset past the count exceeds")
	require.NoError(t, svc.checkQuotaLocked(svc.live["d0"], 1, Identity{}), "republish keeps the count")
}

// One owner cannot take the process's whole count (ADR-0288 (proposed)
// §SD9): its datasets stop at MaxDatasetsPerOwner while another owner's
// still go in, and a republish never counts.
func TestCheckQuotaLockedPerOwner(t *testing.T) {
	svc := &Service{live: make(map[string]*record)}
	owner := Identity{App: "test.app", Instance: 1}
	for i := range MaxDatasetsPerOwner {
		svc.live[fmt.Sprintf("d%d", i)] = &record{owner: owner}
	}
	require.Error(t, svc.checkQuotaLocked(nil, 1, owner))
	require.NoError(t, svc.checkQuotaLocked(svc.live["d0"], 1, owner), "republish keeps the count")
	require.NoError(t, svc.checkQuotaLocked(nil, 1, Identity{App: "test.app", Instance: 2}), "another window still publishes")
	require.NoError(t, svc.checkQuotaLocked(nil, 1, Identity{}), "the runtime has no per-owner count")
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

// A read racing a republish gets one revision or the other, never a file
// the republish already retired.
func TestOpenRacingRepublish(t *testing.T) {
	svc := newTestService(t)
	res, err := svc.Publish(PublishInput{Alias: "items", ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	p, ok := svc.reg.Lookup(res.Handle)
	require.True(t, ok)
	enc := p.(introspect.EncryptedDatasetI)
	stream := int64Stream(t, false, 2)
	done := make(chan struct{})
	var readers sync.WaitGroup
	var failures atomic.Int64
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				rc, _, oErr := enc.Open()
				if oErr != nil {
					failures.Add(1)
					continue
				}
				_ = rc.Close()
			}
		}()
	}
	for range 2000 {
		_, err = svc.Publish(PublishInput{Alias: "items", Handle: res.Handle, ArrowIPCStream: stream})
		require.NoError(t, err)
	}
	close(done)
	readers.Wait()
	assert.Zero(t, failures.Load(), "opens that found the file already retired")
}

// A window-scoped publish goes under the window's own alias, so two
// windows of one app each hold theirs; a plain alias held by one window is
// refused to the other (ADR-0288 (proposed) §SD3).
func TestWindowScopedAliasesAndOwnership(t *testing.T) {
	svc := newTestService(t)
	w1, w2 := Identity{App: "test.app", Instance: 3}, Identity{App: "test.app", Instance: 4}
	a, err := svc.Publish(PublishInput{Alias: "stats", By: w1, WindowScoped: true, ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	assert.Equal(t, "stats_w3", a.Alias)
	b, err := svc.Publish(PublishInput{Alias: "stats", By: w2, WindowScoped: true, ArrowIPCStream: int64Stream(t, false, 2)})
	require.NoError(t, err)
	assert.Equal(t, "stats_w4", b.Alias)
	got, err := svc.Resolve("stats_w3")
	require.NoError(t, err)
	assert.Equal(t, a.Handle, got.Handle, "each window resolves its own")

	re, err := svc.Publish(PublishInput{Alias: "stats", Handle: a.Handle, By: w1, WindowScoped: true, ArrowIPCStream: int64Stream(t, false, 5)})
	require.NoError(t, err)
	assert.Equal(t, "stats_w3", re.Alias, "a republish keeps the window's alias")

	_, err = svc.Publish(PublishInput{Alias: "plain", By: w1, ArrowIPCStream: int64Stream(t, false, 1)})
	require.NoError(t, err)
	_, err = svc.Publish(PublishInput{Alias: "plain", By: w2, ArrowIPCStream: int64Stream(t, false, 1)})
	assert.ErrorIs(t, err, ErrAliasHeld)
	_, err = svc.Publish(PublishInput{Alias: "plain", By: w1, ArrowIPCStream: int64Stream(t, false, 2)})
	assert.NoError(t, err, "the owner may publish another dataset under its alias")
	_, err = svc.Publish(PublishInput{Alias: "plain", ArrowIPCStream: int64Stream(t, false, 3)})
	assert.NoError(t, err, "the runtime may")
	assert.Equal(t, "plain", WindowAlias("plain", 0))
}
