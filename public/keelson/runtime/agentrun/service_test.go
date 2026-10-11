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

	w, err := Open(ctx, chat, Run{Id: "r1", Conversation: "c1", OwnerApp: "lies"})
	require.NoError(t, err)
	stored, _, _ := store.GetRun(ctx, "r1")
	assert.Equal(t, "app.chat", stored.OwnerApp, "the owner is the sender, whatever the request said")
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	require.Len(t, written, 1)

	entries, err := chat.Entries(ctx, "r1", 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, written[0].Digest, entries[0].Digest)
	_, found, err := chat.GetRun(ctx, "r1")
	require.NoError(t, err)
	assert.True(t, found)

	_, found, err = other.GetRun(ctx, "r1")
	require.NoError(t, err)
	assert.False(t, found, "another app's run is not found")
	_, err = other.Entries(ctx, "r1", 0)
	assert.Error(t, err)
	assert.Error(t, other.Append(ctx, []Entry{{Run: "r1", Ordinal: 2, Kind: KindUser}}))
	assert.Error(t, other.Tombstone(ctx, "r1"))
	runs, err := other.ListRuns(ctx, "", 0)
	require.NoError(t, err)
	assert.Empty(t, runs)
	runs, err = chat.ListRuns(ctx, "", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)

	again, err := Reopen(ctx, chat, "r1")
	require.NoError(t, err)
	assert.Equal(t, RunDigestSeed(again.Run()), again.Run().Digest, "the seed is over the row as stored")
	require.NoError(t, again.Delete(ctx, "person"))
	r, _, _ := store.GetRun(ctx, "r1")
	assert.Equal(t, StateDeleted, r.State)
	deleted, err := Reopen(ctx, chat, "r1")
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
	var _ app.BusI = bus.NewClient("x", nil)
}

// The service holds the journal's invariants against a client that has
// them wrong (ADR-0308 §SD9): an append must continue the run's chain at
// the next ordinal, with matching digests, into an open run; a run row
// keeps its identity, seed and label; a deleted run is never written
// again; a tombstone follows the deleted entry.
func TestTheServiceHoldsTheInvariants(t *testing.T) {
	ctx := context.Background()
	bus := inprocbus.NewInst(zerolog.Nop())
	store := NewMemStore()
	svc, err := NewService(bus, zerolog.Nop(), store)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chat := NewClient(bus.NewClient("app.chat", ClientCaps("test")))
	w, err := Open(ctx, chat, Run{Id: "r1", Conversation: "c1"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello", Label: Label{Tier: TierAmber}})
	require.NoError(t, err)
	head := written[0]

	chained := func(e Entry) Entry {
		e.Run = "r1"
		e, err := e.chained(head.Ordinal, head.Digest)
		require.NoError(t, err)
		return e
	}
	assert.Error(t, chat.Append(ctx, []Entry{chained(Entry{Ordinal: 3, Kind: KindUser, Body: "skips"})}), "not the next ordinal")
	assert.Error(t, chat.Append(ctx, []Entry{chained(Entry{Ordinal: 1, Kind: KindUser, Body: "repeats"})}), "a duplicate ordinal")
	bad := chained(Entry{Ordinal: 2, Kind: KindUser, Body: "x"})
	bad.Digest = "forged"
	assert.Error(t, chat.Append(ctx, []Entry{bad}), "not chained to the head")
	lying := chained(Entry{Ordinal: 2, Kind: KindUser, Body: "x"})
	lying.Body = "y"
	assert.Error(t, chat.Append(ctx, []Entry{lying}), "a body its digest does not commit to")
	assert.Error(t, chat.Append(ctx, []Entry{chained(Entry{Ordinal: 2, Kind: KindE(200)})}), "no such kind")
	assert.Error(t, chat.Append(ctx, []Entry{chained(Entry{Ordinal: 2, Kind: KindCompact, Target: 2})}), "a compaction aimed at itself")
	assert.Error(t, chat.Tombstone(ctx, "r1"), "no deleted entry at the head")
	good := chained(Entry{Ordinal: 2, Kind: KindUser, Body: "x"})
	require.NoError(t, chat.Append(ctx, []Entry{good}), "the next entry, chained")
	r, _, _ := store.GetRun(ctx, "r1")
	assert.Equal(t, uint32(2), r.LastOrdinal)
	assert.Equal(t, good.Digest, r.Head)
	assert.Equal(t, TierAmber, r.Label.Tier, "the row's label rose with the entries")

	rewritten := r
	rewritten.Id, rewritten.Digest, rewritten.Created, rewritten.LastOrdinal, rewritten.Label = "r1", "forged", Now(), 9, Label{}
	rewritten.Fingerprint = "fp"
	assert.Error(t, chat.UpdateRun(ctx, rewritten), "a row at the stored version is a stale writer's")
	rewritten.Version = r.Version + 1
	require.NoError(t, chat.UpdateRun(ctx, rewritten))
	r, _, _ = store.GetRun(ctx, "r1")
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
	assert.Error(t, chat.Append(ctx, []Entry{chained(Entry{Ordinal: 3, Kind: KindUser, Body: "z"})}), "not open")
	del := stranded
	del.State, del.Version = StateDeleted, stranded.Version+1
	assert.Error(t, chat.UpdateRun(ctx, del), "deleted needs the deleted entry at the head")

	again, err := Reopen(ctx, chat, "r1")
	require.NoError(t, err)
	require.NoError(t, again.Delete(ctx, "person"))
	r, _, _ = store.GetRun(ctx, "r1")
	assert.Equal(t, StateDeleted, r.State)
	revived := r
	revived.State = StateOpen
	assert.Error(t, chat.UpdateRun(ctx, revived), "a deleted run is never written again")
	revived.State = StateDeleted
	assert.Error(t, chat.UpdateRun(ctx, revived))
	assert.Error(t, chat.Append(ctx, []Entry{{Run: "r1", Ordinal: 4, Prev: 3, Kind: KindUser}}))

	// A deleted entry at the head, the row still open (a deletion the
	// process did not finish): nothing follows it through the service.
	w2, err := Open(ctx, chat, Run{Id: "r2"})
	require.NoError(t, err)
	first, err := w2.Append(ctx, Entry{Kind: KindUser, Body: "x"})
	require.NoError(t, err)
	deleted, err := Entry{Run: "r2", Kind: KindDeleted, Name: "person"}.chained(first[0].Ordinal, first[0].Digest)
	require.NoError(t, err)
	deleted.Ordinal = 2
	deleted, err = deleted.chained(first[0].Ordinal, first[0].Digest)
	require.NoError(t, err)
	require.NoError(t, chat.Append(ctx, []Entry{deleted}))
	after, err := Entry{Run: "r2", Ordinal: 3, Kind: KindUser, Body: "y"}.chained(deleted.Ordinal, deleted.Digest)
	require.NoError(t, err)
	assert.Error(t, chat.Append(ctx, []Entry{after}), "nothing follows the deleted entry")
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
	w, err := Open(ctx, chat, Run{Id: "r1"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	head := written[0]
	second := func(body string) Entry {
		e := Entry{Run: "r1", Ordinal: 2, Kind: KindUser, Body: body}
		e, cerr := e.chained(head.Ordinal, head.Digest)
		require.NoError(t, cerr)
		return e
	}
	errs := make(chan error, 2)
	for _, body := range []string{"a", "b"} {
		go func() { errs <- chat.Append(ctx, []Entry{second(body)}) }()
	}
	var failed int
	for range 2 {
		if <-errs != nil {
			failed++
		}
	}
	assert.Equal(t, 1, failed, "one lands, one is refused")
	entries, err := store.Entries(ctx, "r1", 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	again, err := Reopen(ctx, chat, "r1")
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
	first, err := Open(ctx, chat, Run{Id: "r1"})
	require.NoError(t, err)
	_, err = first.Append(ctx, Entry{Kind: KindUser, Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, first.Close(ctx, StateClosed))
	second, err := Reopen(ctx, chat, "r1")
	require.NoError(t, err)
	require.NoError(t, second.Resume(ctx))
	require.NoError(t, first.Close(ctx, StateClosed), "the late close stands down")
	r, found, err := chat.GetRun(ctx, "r1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, StateOpen, r.State, "the run stays open for the writer that resumed it")
	assert.Equal(t, StateOpen, first.Run().State, "the stale writer adopted the row")
	_, err = second.Append(ctx, Entry{Kind: KindUser, Body: "on"})
	require.NoError(t, err)
}
