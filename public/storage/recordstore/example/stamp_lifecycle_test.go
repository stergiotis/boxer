package example

import (
	"context"
	"iter"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/example/internal/lowlevel"
	"github.com/stretchr/testify/require"
)

// fixedStamper yields one constant id — the minimal ReferenceStamper for
// exercising the stamp lifecycle without a dimension store behind it.
type fixedStamper struct{ id uint64 }

func (f fixedStamper) Current(_ context.Context) iter.Seq2[identifier.TaggedId, error] {
	return func(yield func(identifier.TaggedId, error) bool) {
		yield(identifier.TaggedId(f.id), nil)
	}
}
func (f fixedStamper) Flush(_ context.Context) (int, error) { return 0, nil }

// TestStampDiscardPendingClearsAmbient pins the ambient-stamp lifecycle:
// stamps pushed by a Begin die with the frame. A builder abandoned without
// Commit/Rollback leaves its stamps on the entity's ambient stack; the
// documented recovery verb DiscardPending must clear them, or every later
// entity carries the stale id on all its attributes in addition to its own.
func TestStampDiscardPendingClearsAmbient(t *testing.T) {
	dev := NewDeviceStore(nil, nil, DeviceStoreConfig{
		Stampers: []recordstore.ReferenceStamper{fixedStamper{42}},
	})

	// Abandon a stamped Begin (no Commit, no Rollback), then recover.
	_ = dev.Begin(1, recordstore.SeqTs(1))
	dev.DiscardPending()

	// A clean write afterwards must carry exactly its own stamp.
	require.NoError(t, dev.Begin(2, recordstore.SeqTs(2)).
		AddIdentity(Identity{ID: 2, Status: "live"}).Commit())

	recs, err := lowlevel.InEntityDeviceTableTransferRecords(dev.dml, nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	rec := recs[len(recs)-1]
	symR := lowlevel.NewReadAccessDeviceTableTaggedSymbol()
	defer symR.Release()
	require.NoError(t, symR.LoadFromRecord(rec))
	last := raruntime.EntityIdx(int(rec.NumRows()) - 1)
	got := slices.Collect(symR.GetMemberships().GetMembValueHighCardRef(last, raruntime.AttributeIdx(0)))
	require.Equal(t, []uint64{42}, got,
		"the abandoned Begin's stamp leaked past DiscardPending onto a later entity")
}

// countingStamper records how many times Flush reached it.
type countingStamper struct {
	id      uint64
	flushes int
}

func (s *countingStamper) Current(_ context.Context) iter.Seq2[identifier.TaggedId, error] {
	return func(yield func(identifier.TaggedId, error) bool) {
		yield(identifier.TaggedId(s.id), nil)
	}
}
func (s *countingStamper) Flush(_ context.Context) (int, error) {
	s.flushes++
	return 0, nil
}

// The ordered flush (ADR-0112 SD5) must not be skipped by the store's
// nothing-to-do return. A Begin that stamped and then rolled back leaves the
// dimension rows those stamps reference buffered in the stamper while the
// store itself has nothing to ship — and the store's Flush is the only thing
// obliged to ship them in order. Flushing an empty stamper is free, so the
// ordered flush runs unconditionally.
func TestStampFlushRunsWithNothingBuffered(t *testing.T) {
	ctx := context.Background()
	stamper := &countingStamper{id: 42}
	dev := NewDeviceStore(nil, nil, DeviceStoreConfig{
		Stampers: []recordstore.ReferenceStamper{stamper},
	})
	defer dev.Close()

	// Nothing buffered at all.
	n, err := dev.Flush(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, 1, stamper.flushes, "the ordered stamp flush must run even with no rows to insert")

	// A stamped frame that never becomes a row: same obligation.
	require.NoError(t, dev.Begin(1, recordstore.SeqTs(1)).Rollback())
	n, err = dev.Flush(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, 2, stamper.flushes)
}

// BestEffortStampFlush opts out of the ordering, so the stampers are not
// flushed by the store at all — the caller owns their durability.
func TestStampFlushBestEffortSkipsOrdering(t *testing.T) {
	ctx := context.Background()
	stamper := &countingStamper{id: 42}
	dev := NewDeviceStore(nil, nil, DeviceStoreConfig{
		Stampers:             []recordstore.ReferenceStamper{stamper},
		BestEffortStampFlush: true,
	})
	defer dev.Close()

	_, err := dev.Flush(ctx)
	require.NoError(t, err)
	require.Zero(t, stamper.flushes)
}
