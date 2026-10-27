package agentrun

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The service scopes every call to the sender: an app writes runs it owns
// and reads only those; another app's run is not found.
func TestTheServiceScopesRunsToTheSender(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	store := NewMemStore()
	svc, err := NewService(bus, zerolog.Nop(), store)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	other := NewClient(bus.NewClient("app.other", ClientCaps("test")))

	w, err := Open(ctx, chat, Run{Id: 11, Conversation: "c1", OwnerApp: "lies"})
	require.NoError(t, err)
	stored, _, _ := store.GetRun(ctx, 11)
	assert.Equal(t, "app.chat", stored.OwnerApp, "the owner is the sender, whatever the request said")
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	require.Len(t, written, 1)

	entries, err := chat.Entries(ctx, 11, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, written[0].Digest, entries[0].Digest)
	_, found, err := chat.GetRun(ctx, 11)
	require.NoError(t, err)
	assert.True(t, found)

	_, found, err = other.GetRun(ctx, 11)
	require.NoError(t, err)
	assert.False(t, found, "another app's run is not found")
	_, err = other.Entries(ctx, 11, 0)
	assert.Error(t, err)
	assert.Error(t, other.Append(ctx, []Entry{{Run: 11, Ordinal: 2, Kind: KindUser}}))
	assert.Error(t, other.Tombstone(ctx, 11))
	runs, err := other.ListRuns(ctx, "", 0)
	require.NoError(t, err)
	assert.Empty(t, runs)
	runs, err = chat.ListRuns(ctx, "", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)

	again, err := Reopen(ctx, chat, 11)
	require.NoError(t, err)
	seed, err := RunDigestSeed(again.Run())
	require.NoError(t, err)
	assert.Equal(t, seed, again.Run().Digest, "the seed is over the row as stored")
	require.NoError(t, again.Delete(ctx, "person"))
	r, _, _ := store.GetRun(ctx, 11)
	assert.Equal(t, StateDeleted, r.State)
	deleted, err := Reopen(ctx, chat, 11)
	require.NoError(t, err, "a deleted run reopens over the bus")
	assert.True(t, deleted.Entries()[0].Tombstoned())

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, store))
	p, ok := reg.Lookup(TableEntries)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 2, rec.NumRows())
	v, ok := reg.Lookup(TableVerdicts)
	require.True(t, ok, "keelson('agent_verdicts') is registered beside the entries")
	vrec, err := v.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer vrec.Release()
	assert.EqualValues(t, 0, vrec.NumRows())
	var _ app.BusI = bus.NewClient("x", nil)
}

// The service holds the journal's invariants against a client that has
// them wrong (ADR-0308 §SD9): an append must continue the run's chain at
// the next ordinal from the head the writer expects, into an open run,
// and the service prepares it — ids, commitments, digests, the sender as
// who wrote it (ADR-0327 §SD9); a run row keeps its identity, seed and
// label; a deleted run is never written again; a tombstone follows the
// deleted entry.
func TestTheServiceHoldsTheInvariants(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	store := NewMemStore()
	svc, err := NewService(bus, zerolog.Nop(), store)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	w, err := Open(ctx, chat, Run{Id: 11, Conversation: "c1"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello", Label: Label{Tier: TierAmber}})
	require.NoError(t, err)
	head := written[0]
	require.NotNil(t, head.WrittenBy)
	assert.Equal(t, "app.chat", head.WrittenBy.App, "the service recorded the sender as who wrote it")
	assert.Equal(t, "envelope", head.WrittenBy.OriginBy, "vouched by the envelope")

	// next is an entry as the writer sends it: the next ordinal after the
	// head and the head it expects, nothing prepared.
	next := func(e Entry) Entry {
		e.Run, e.Prev = 11, head.Ordinal
		return e
	}
	at := expecting(ctx, head.Digest)
	assert.Error(t, chat.Append(at, []Entry{next(Entry{Ordinal: 3, Kind: KindUser, Body: "skips"})}), "not the next ordinal")
	assert.Error(t, chat.Append(at, []Entry{next(Entry{Ordinal: 1, Prev: 0, Kind: KindUser, Body: "repeats"})}), "a duplicate ordinal")
	assert.Error(t, chat.Append(expecting(ctx, "stale"), []Entry{next(Entry{Ordinal: 2, Kind: KindUser, Body: "x"})}), "a head that moved")
	assert.Error(t, chat.Append(at, []Entry{next(Entry{Ordinal: 2, Kind: KindE(200)})}), "no such kind")
	assert.Error(t, chat.Append(at, []Entry{next(Entry{Ordinal: 2, Kind: KindCompact, Target: 2})}), "a compaction aimed at itself")
	assert.Error(t, chat.Tombstone(ctx, 11), "no deleted entry at the head")
	good := []Entry{next(Entry{Ordinal: 2, Kind: KindUser, Body: "x"})}
	require.NoError(t, chat.Append(at, good), "the next entry")
	assert.NotEmpty(t, good[0].Digest, "the service prepared it")
	assert.NotEmpty(t, good[0].Commitments)
	assert.NotEmpty(t, good[0].Openings, "and handed the openings back")
	r, _, _ := store.GetRun(ctx, 11)
	assert.Equal(t, uint32(2), r.LastOrdinal)
	assert.Equal(t, good[0].Digest, r.Head)
	assert.Equal(t, TierAmber, r.Label.Tier, "the row's label rose with the entries")

	rewritten := r
	rewritten.Id, rewritten.Digest, rewritten.Created, rewritten.LastOrdinal, rewritten.Label = 11, "forged", Now(), 9, Label{}
	rewritten.Fingerprint = "fp"
	assert.Error(t, chat.UpdateRun(ctx, rewritten), "a row at the stored version is a stale writer's")
	rewritten.Version = r.Version + 1
	require.NoError(t, chat.UpdateRun(ctx, rewritten))
	r, _, _ = store.GetRun(ctx, 11)
	assert.Equal(t, "fp", r.Fingerprint, "the mutable field moved")
	assert.NotEqual(t, "forged", r.Digest, "the seed did not")
	assert.Equal(t, uint32(2), r.LastOrdinal)
	assert.Equal(t, TierAmber, r.Label.Tier, "the label did not lower")
	stranded := r
	stranded.State, stranded.Version = StateStranded, r.Version+1
	require.NoError(t, chat.UpdateRun(ctx, stranded))
	reopened := stranded
	reopened.State, reopened.Version = StateOpen, stranded.Version+1
	assert.Error(t, chat.UpdateRun(ctx, reopened), "stranded does not move to open")
	assert.Error(t, chat.Append(expecting(ctx, r.Head), []Entry{{Run: 11, Ordinal: 3, Prev: 2, Kind: KindUser, Body: "z"}}), "not open")
	del := stranded
	del.State, del.Version = StateDeleted, stranded.Version+1
	assert.Error(t, chat.UpdateRun(ctx, del), "deleted needs the deleted entry at the head")

	again, err := Reopen(ctx, chat, 11)
	require.NoError(t, err)
	require.NoError(t, again.Delete(ctx, "person"))
	r, _, _ = store.GetRun(ctx, 11)
	assert.Equal(t, StateDeleted, r.State)
	revived := r
	revived.State = StateOpen
	assert.Error(t, chat.UpdateRun(ctx, revived), "a deleted run is never written again")
	revived.State = StateDeleted
	assert.Error(t, chat.UpdateRun(ctx, revived))
	assert.Error(t, chat.Append(ctx, []Entry{{Run: 11, Ordinal: 4, Prev: 3, Kind: KindUser}}))

	// A deleted entry at the head, the row still open (a deletion the
	// process did not finish): nothing follows it through the service.
	w2, err := Open(ctx, chat, Run{Id: 12})
	require.NoError(t, err)
	first, err := w2.Append(ctx, Entry{Kind: KindUser, Body: "x"})
	require.NoError(t, err)
	deleted := []Entry{{Run: 12, Ordinal: 2, Prev: 1, Kind: KindDeleted, Name: "person"}}
	require.NoError(t, chat.Append(expecting(ctx, first[0].Digest), deleted))
	after := []Entry{{Run: 12, Ordinal: 3, Prev: 2, Kind: KindUser, Body: "y"}}
	assert.Error(t, chat.Append(expecting(ctx, deleted[0].Digest), after), "nothing follows the deleted entry")
}

// delayedStore holds every append for a while: what lets two writers race
// through the service.
type delayedStore struct {
	StoreI
	delay time.Duration
}

func (inst *delayedStore) Append(ctx context.Context, entries []Entry) error {
	time.Sleep(inst.delay)
	return inst.StoreI.Append(ctx, entries)
}

// Two writers on one run through the service: the check and the write
// are one critical section per run, so of two appends at the same
// ordinal one lands and the other is refused, and the run reopens.
func TestTwoWritersOnOneRunAreSerialised(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	store := NewMemStore()
	svc, err := NewService(bus, zerolog.Nop(), &delayedStore{StoreI: store, delay: 50 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	w, err := Open(ctx, chat, Run{Id: 11})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	head := written[0]
	second := func(body string) Entry {
		return Entry{Run: 11, Ordinal: 2, Prev: head.Ordinal, Kind: KindUser, Body: body}
	}
	errs := make(chan error, 2)
	for _, body := range []string{"a", "b"} {
		go func() { errs <- chat.Append(expecting(ctx, head.Digest), []Entry{second(body)}) }()
	}
	var failed int
	for range 2 {
		if <-errs != nil {
			failed++
		}
	}
	assert.Equal(t, 1, failed, "one lands, one is refused")
	entries, err := store.Entries(ctx, 11, 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	again, err := Reopen(ctx, chat, 11)
	require.NoError(t, err, "the chain verifies")
	assert.Equal(t, uint32(2), again.Len())
}

// A stale writer's late close does not take a run another writer
// reopened: the service writes a row only at the next version over the
// stored one, and the writer whose close is refused adopts the row as
// stored instead of failing.
func TestAStaleCloseDoesNotTakeAReopenedRun(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), NewMemStore())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	first, err := Open(ctx, chat, Run{Id: 11})
	require.NoError(t, err)
	_, err = first.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, first.Close(ctx, StateClosed))
	second, err := Reopen(ctx, chat, 11)
	require.NoError(t, err)
	require.NoError(t, second.Resume(ctx))
	require.NoError(t, first.Close(ctx, StateClosed), "the late close stands down")
	r, found, err := chat.GetRun(ctx, 11)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, StateOpen, r.State, "the run stays open for the writer that resumed it")
	assert.Equal(t, StateOpen, first.Run().State, "the stale writer adopted the row")
	_, err = second.Append(ctx, Entry{Kind: KindUser, Body: "on"})
	require.NoError(t, err)
}
