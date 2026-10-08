package example

import (
	"context"
	"errors"
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/dimension/actor"
)

// insertRecorder is an in-memory ExecutorI: it fails the first failFirst
// inserts and records the batch id every insert's context carried.
type insertRecorder struct {
	failFirst int
	batchIds  []recordstore.BatchIdT
}

func (inst *insertRecorder) Exec(context.Context, string) error { return nil }

func (inst *insertRecorder) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *insertRecorder) InsertArrow(ctx context.Context, _ string, _ []arrow.RecordBatch) error {
	id, _ := recordstore.BatchIdFrom(ctx)
	inst.batchIds = append(inst.batchIds, id)
	if len(inst.batchIds) <= inst.failFirst {
		return errors.New("synthetic insert failure")
	}
	return nil
}

type durableBatch struct {
	batch recordstore.BatchIdT
	keys  []recordstore.WrittenKey[uint64, time.Time]
}

// writeLog is a WriteObserverI that keeps everything it is told.
type writeLog struct {
	committed []recordstore.WrittenKey[uint64, time.Time]
	durable   []durableBatch
	discarded [][]recordstore.WrittenKey[uint64, time.Time]
}

func (inst *writeLog) Committed(w recordstore.WrittenKey[uint64, time.Time]) {
	inst.committed = append(inst.committed, w)
}

func (inst *writeLog) Durable(ctx context.Context, batch recordstore.BatchIdT, keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if id, _ := recordstore.BatchIdFrom(ctx); id != batch {
		panic("Durable's context does not carry its batch id")
	}
	inst.durable = append(inst.durable, durableBatch{batch: batch, keys: slices.Collect(keys)})
}

func (inst *writeLog) Discarded(keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	inst.discarded = append(inst.discarded, slices.Collect(keys))
}

// ctxStamper records the call identity each Current found on its context.
type ctxStamper struct{ seen []callident.CallIdentity }

func (inst *ctxStamper) Current(ctx context.Context) iter.Seq2[identifier.TaggedId, error] {
	return func(yield func(identifier.TaggedId, error) bool) {
		ci, _ := callident.CallIdentityFrom(ctx)
		inst.seen = append(inst.seen, ci)
		yield(identifier.TaggedId(7), nil)
	}
}
func (inst *ctxStamper) Flush(context.Context) (int, error) { return 0, nil }

// BeginCtx, and the verbs that open frames through it, hand the stampers the
// caller's context (ADR-0295 §SD7); Begin hands them a bare one.
func TestBeginCtxReachesStamper(t *testing.T) {
	stamper := &ctxStamper{}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{
		Stampers:        []recordstore.ReferenceStamper{stamper},
		TombstoneDetect: func(e *DeviceEntity) bool { return e.Identity.Has && e.Identity.Val.Status == "deleted" },
		TombstoneWrite:  func(b *DeviceEntityBuilder) { b.AddIdentity(Identity{ID: b.key, Status: "deleted"}) },
	})
	defer st.Close()
	ci := callident.CallIdentity{Claims: callident.Claims{Principal: "p:1", Purpose: "test"}}
	ctx := callident.WithCallIdentity(context.Background(), ci)
	t0 := recordstore.SeqTs(1)

	require.NoError(t, st.BeginCtx(ctx, 1, t0).AddBattery(Battery{ID: 1, Charge: 5}).Commit())
	require.NoError(t, st.IngestIdentityCtx(ctx, t0, []Identity{{ID: 2, Status: "live"}}))
	require.NoError(t, st.DeleteCtx(ctx, 3, t0))
	require.NoError(t, st.Begin(4, t0).AddBattery(Battery{ID: 4}).Commit())
	assert.Equal(t, []callident.CallIdentity{ci, ci, ci, {}}, stamper.seen)
}

// One Flush reports its keys once, under the batch id its insert carried —
// the same id the executor received.
func TestWriteObserverOneFlushOneBatch(t *testing.T) {
	exec := &insertRecorder{}
	log := &writeLog{}
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{WriteObserver: log})
	defer st.Close()
	t1, t2 := recordstore.SeqTs(1), recordstore.SeqTs(2)
	require.NoError(t, st.Begin(1, t1).AddBattery(Battery{ID: 1, Charge: 9}).Commit())
	require.NoError(t, st.Begin(2, t1).AddIdentity(Identity{ID: 2, Status: "live"}).Commit())
	require.NoError(t, st.Delete(1, t2))
	require.Error(t, st.Begin(5, t1).AddBattery(Battery{ID: 5}).AddBattery(Battery{ID: 5}).Commit(), "a refused commit is not reported")

	want := []recordstore.WrittenKey[uint64, time.Time]{
		{Key: 1, Order: t1, Lifecycle: recordstore.LifecycleLive},
		{Key: 2, Order: t1, Lifecycle: recordstore.LifecycleLive},
		{Key: 1, Order: t2, Lifecycle: recordstore.LifecycleTombstone, Tombstone: true},
	}
	assert.Equal(t, want, log.committed)
	assert.Empty(t, log.durable, "nothing is durable before Flush")

	n, err := st.Flush(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	require.Len(t, log.durable, 1)
	require.Len(t, exec.batchIds, 1)
	assert.Equal(t, exec.batchIds[0], log.durable[0].batch)
	assert.NotEmpty(t, log.durable[0].batch)
	assert.Equal(t, want, log.durable[0].keys)

	_, err = st.Flush(context.Background())
	require.NoError(t, err)
	assert.Len(t, log.durable, 1, "an empty Flush reports nothing")
}

// A failed Flush reports nothing; the retry that lands reports every key
// once, under its own batch id — including a row committed in between.
func TestWriteObserverFailedFlushRetryReportsOnce(t *testing.T) {
	exec := &insertRecorder{failFirst: 1}
	log := &writeLog{}
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{WriteObserver: log})
	defer st.Close()
	t1 := recordstore.SeqTs(1)
	require.NoError(t, st.Begin(1, t1).AddBattery(Battery{ID: 1}).Commit())
	require.NoError(t, st.Begin(2, t1).AddBattery(Battery{ID: 2}).Commit())
	_, err := st.Flush(context.Background())
	require.Error(t, err)
	assert.Empty(t, log.durable)

	require.NoError(t, st.Begin(3, t1).AddBattery(Battery{ID: 3}).Commit())
	_, err = st.Flush(context.Background())
	require.NoError(t, err)

	require.Len(t, exec.batchIds, 2)
	assert.NotEqual(t, exec.batchIds[0], exec.batchIds[1], "each insert attempt is its own batch")
	require.Len(t, log.durable, 1)
	assert.Equal(t, exec.batchIds[1], log.durable[0].batch)
	keys := make([]uint64, 0, 3)
	for _, w := range log.durable[0].keys {
		keys = append(keys, w.Key)
	}
	assert.Equal(t, []uint64{1, 2, 3}, keys)
}

// DiscardPending tells the observer which keys will never be durable, and a
// later Flush does not report them.
func TestWriteObserverDiscard(t *testing.T) {
	exec := &insertRecorder{failFirst: 1}
	log := &writeLog{}
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{WriteObserver: log})
	defer st.Close()
	require.NoError(t, st.Begin(1, recordstore.SeqTs(1)).AddBattery(Battery{ID: 1}).Commit())
	_, err := st.Flush(context.Background())
	require.Error(t, err)
	st.DiscardPending()
	require.Len(t, log.discarded, 1)
	assert.Equal(t, uint64(1), log.discarded[0][0].Key)

	require.NoError(t, st.Begin(2, recordstore.SeqTs(1)).AddBattery(Battery{ID: 2}).Commit())
	_, err = st.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, log.durable, 1)
	require.Len(t, log.durable[0].keys, 1)
	assert.Equal(t, uint64(2), log.durable[0].keys[0].Key)
}

// Under a configured tombstone pair the marker row reads LifecycleLive; the
// Tombstone flag is what says Delete wrote it.
func TestWriteObserverTombstonePair(t *testing.T) {
	log := &writeLog{}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{
		WriteObserver:   log,
		TombstoneDetect: func(e *DeviceEntity) bool { return e.Identity.Has && e.Identity.Val.Status == "deleted" },
		TombstoneWrite:  func(b *DeviceEntityBuilder) { b.AddIdentity(Identity{ID: b.key, Status: "deleted"}) },
	})
	defer st.Close()
	require.NoError(t, st.Delete(9, recordstore.SeqTs(3)))
	require.Len(t, log.committed, 1)
	assert.Equal(t, recordstore.WrittenKey[uint64, time.Time]{Key: 9, Order: recordstore.SeqTs(3), Lifecycle: recordstore.LifecycleLive, Tombstone: true}, log.committed[0])
}

// The default tombstone pair's Delete consults the stampers like Begin does:
// an actor-stamped store refuses a deletion with no principal on its context,
// and the observer is told who deleted.
func TestDefaultDeleteHonoursStampers(t *testing.T) {
	gen, err := mem.NewIdInternalizer(5, 64)
	require.NoError(t, err)
	rec := actor.NewRecorder(gen, actor.NewStoreSink(actor.NewActorStore(nil, nil, actor.ActorStoreConfig{})))
	log := &writeLog{}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{
		Stampers:      []recordstore.ReferenceStamper{rec.Stamper()},
		WriteObserver: log,
	})
	defer st.Close()

	require.ErrorIs(t, st.Delete(1, recordstore.SeqTs(1)), actor.ErrNoPrincipal)
	require.ErrorIs(t, st.DeleteCtx(context.Background(), 1, recordstore.SeqTs(1)), actor.ErrNoPrincipal)
	assert.Zero(t, st.Buffered())
	assert.Empty(t, log.committed)

	ci := callident.CallIdentity{Claims: callident.Claims{Principal: "p:del", Purpose: "erasure"}}
	require.NoError(t, st.DeleteCtx(callident.WithCallIdentity(context.Background(), ci), 1, recordstore.SeqTs(1)))
	assert.Equal(t, 1, st.Buffered())
	require.Len(t, log.committed, 1)
	assert.Equal(t, ci, log.committed[0].Identity)
	assert.True(t, log.committed[0].Tombstone)
}

// Each row carries the identity it was committed under, whoever flushes it.
func TestWriteObserverIdentityIsTheCommitters(t *testing.T) {
	log := &writeLog{}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{WriteObserver: log})
	defer st.Close()
	who := func(p string) context.Context {
		return callident.WithCallIdentity(context.Background(), callident.CallIdentity{Claims: callident.Claims{Principal: p}})
	}
	require.NoError(t, st.BeginCtx(who("p:a"), 1, recordstore.SeqTs(1)).AddBattery(Battery{ID: 1}).Commit())
	require.NoError(t, st.IngestBatteryCtx(who("p:b"), recordstore.SeqTs(1), []Battery{{ID: 2}}))
	require.NoError(t, st.Begin(3, recordstore.SeqTs(1)).AddBattery(Battery{ID: 3}).Commit())
	_, err := st.Flush(who("p:flusher"))
	require.NoError(t, err)
	require.Len(t, log.durable, 1)
	got := make([]string, 0, 3)
	for _, w := range log.durable[0].keys {
		got = append(got, w.Identity.Claims.Principal)
	}
	assert.Equal(t, []string{"p:a", "p:b", ""}, got)
}

// reentrantLog commits one more row into its store from Durable, once.
type reentrantLog struct {
	writeLog
	st   *DeviceStore
	done bool
}

func (inst *reentrantLog) Durable(ctx context.Context, batch recordstore.BatchIdT, keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	inst.writeLog.Durable(ctx, batch, keys)
	if !inst.done {
		inst.done = true
		_ = inst.st.Begin(999, recordstore.SeqTs(9)).AddBattery(Battery{ID: 999}).Commit()
	}
}

// A row the observer commits from its own Durable callback is reported with
// the flush that ships it, not cleared with the batch being reported.
func TestWriteObserverReentrantCommit(t *testing.T) {
	log := &reentrantLog{}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{WriteObserver: log})
	log.st = st
	defer st.Close()
	require.NoError(t, st.Begin(1, recordstore.SeqTs(1)).AddBattery(Battery{ID: 1}).Commit())
	_, err := st.Flush(context.Background())
	require.NoError(t, err)
	_, err = st.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, log.durable, 2)
	assert.Equal(t, uint64(1), log.durable[0].keys[0].Key)
	require.Len(t, log.durable[1].keys, 1)
	assert.Equal(t, uint64(999), log.durable[1].keys[0].Key)
}

type panickingLog struct {
	writeLog
	armed bool
}

func (inst *panickingLog) Durable(ctx context.Context, batch recordstore.BatchIdT, keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	if inst.armed {
		inst.armed = false
		panic("observer failure")
	}
	inst.writeLog.Durable(ctx, batch, keys)
}

// A panicking observer loses that one report; the next flush does not repeat
// the rows under a second batch id.
func TestWriteObserverPanicDoesNotRepeat(t *testing.T) {
	log := &panickingLog{armed: true}
	st := NewDeviceStore(&insertRecorder{}, nil, DeviceStoreConfig{WriteObserver: log})
	defer st.Close()
	require.NoError(t, st.Begin(1, recordstore.SeqTs(1)).AddBattery(Battery{ID: 1}).Commit())
	require.Panics(t, func() { _, _ = st.Flush(context.Background()) })
	require.NoError(t, st.Begin(2, recordstore.SeqTs(1)).AddBattery(Battery{ID: 2}).Commit())
	_, err := st.Flush(context.Background())
	require.NoError(t, err)
	require.Len(t, log.durable, 1)
	require.Len(t, log.durable[0].keys, 1)
	assert.Equal(t, uint64(2), log.durable[0].keys[0].Key)
}
