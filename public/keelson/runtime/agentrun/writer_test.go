package agentrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedClock(t *testing.T) {
	t.Helper()
	was := Now
	n := int64(0)
	Now = func() time.Time { n++; return time.Date(2026, 10, 11, 12, 0, 0, int(n), time.UTC) }
	t.Cleanup(func() { Now = was })
}

// The writer assigns contiguous ordinals, chains every entry to the one
// before from the run row's digest, and the chain verifies; a reopened
// writer continues it.
func TestTheWriterChainsEntries(t *testing.T) {
	fixedClock(t)
	ctx := context.Background()
	st := NewMemStore()
	w, err := Open(ctx, st, Run{Id: "cell/run-1", Cell: "cell", Conversation: "c1", OwnerApp: "chat", Coordinator: "w7"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Role: "user", Body: "hello"}, Entry{Kind: KindRequest, Key: "llm-1", Body: "{}"})
	require.NoError(t, err)
	require.Len(t, written, 2)
	assert.Equal(t, uint32(1), written[0].Ordinal)
	assert.Equal(t, uint32(0), written[0].Prev)
	assert.Equal(t, uint32(2), written[1].Ordinal)
	assert.Equal(t, uint32(1), written[1].Prev)
	assert.NotEqual(t, written[0].Digest, written[1].Digest)
	assert.Equal(t, "cell/run-1", written[1].Run)

	entries := w.Entries()
	broken, err := Verify(w.Run().Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
	entries[0].Body = "tampered"
	broken, _ = Verify(w.Run().Digest, entries)
	assert.Equal(t, uint32(1), broken, "a changed body breaks the chain at its entry")

	again, err := Reopen(ctx, st, "cell/run-1")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), again.Len())
	more, err := again.Append(ctx, Entry{Kind: KindReply, Key: "llm-1", Role: "assistant", Body: "hi"})
	require.NoError(t, err)
	assert.Equal(t, uint32(3), more[0].Ordinal)
	assert.Equal(t, written[1].Digest, ChainDigest(written[0].Digest, mustForm(t, written[1])))
	assert.Equal(t, uint32(3), again.Run().LastOrdinal)
}

func mustForm(t *testing.T, e Entry) []byte {
	t.Helper()
	b, err := e.CanonicalForm()
	require.NoError(t, err)
	return b
}

type failingStore struct {
	StoreI
	fail bool
}

func (inst *failingStore) Append(ctx context.Context, entries []Entry) error {
	if inst.fail {
		return errors.New("disk gone")
	}
	return inst.StoreI.Append(ctx, entries)
}

// A failed write refuses the act and every later one until the run is
// reopened from the record (ADR-0308 §SD2).
func TestAFailedWriteRefusesTheAct(t *testing.T) {
	ctx := context.Background()
	st := &failingStore{StoreI: NewMemStore()}
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	st.fail = true
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "x"})
	require.Error(t, err)
	st.fail = false
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "y"})
	require.Error(t, err, "refused until reopened")
	assert.Zero(t, w.Len())
	again, err := Reopen(ctx, st, "r")
	require.NoError(t, err)
	_, err = again.Append(ctx, Entry{Kind: KindUser, Body: "y"})
	require.NoError(t, err)
	_, err = again.Append(ctx, Entry{Kind: KindE(200)})
	assert.Error(t, err, "no such kind")
}

// Deletion is a tombstone: bodies go, the chain and the metadata stay and
// still verify, since the chain commits to the bodies' digests; who
// deleted is named outside the bodies; a deleted run reopens read-only
// (ADR-0308 §SD8, ADR-0314 §SD5).
func TestDeletionKeepsTheChain(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	w, err := Open(ctx, st, Run{Id: "r", OwnerApp: "chat"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "secret"}, Entry{Kind: KindReply, Body: "answer", Calls: "[]"})
	require.NoError(t, err)
	require.NoError(t, w.Close(ctx, StateClosed))
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "late"})
	assert.Error(t, err, "a closed run takes no entry")
	require.NoError(t, w.Delete(ctx, "person"), "but it can be deleted")
	assert.Error(t, w.Delete(ctx, "person"), "once")
	r, found, err := st.GetRun(ctx, "r")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, StateDeleted, r.State)
	assert.Equal(t, "person", r.DeletedBy)
	entries, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Empty(t, entries[0].Body)
	assert.True(t, entries[0].Tombstoned())
	assert.Equal(t, written[0].BodyDigest, entries[0].BodyDigest, "the commitment stays")
	assert.Empty(t, entries[0].Opening, "the opening goes with the body")
	assert.Equal(t, KindDeleted, entries[2].Kind)
	assert.Equal(t, "person", entries[2].Name, "who deleted is outside the bodies")
	assert.NotEmpty(t, entries[0].Digest, "digests stay")
	broken, err := VerifyRun(r, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain verifies after the tombstone (L31)")
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "more"})
	assert.Error(t, err, "a deleted run is not open")
	again, err := Reopen(ctx, st, "r")
	require.NoError(t, err, "a deleted run reopens")
	assert.Equal(t, StateDeleted, again.Run().State)
	_, err = again.Append(ctx, Entry{Kind: KindUser, Body: "more"})
	assert.Error(t, err)
	assert.Error(t, again.Resume(ctx), "and does not resume")
	runs, err := st.ListRuns(ctx, "chat", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Empty(t, must(st.ListRuns(ctx, "other", 0)))
}

// A body changed after the fact breaks the chain at its entry even
// though the chain commits to digests: the digest no longer matches.
func TestAChangedBodyBreaksTheChain(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a"}, Entry{Kind: KindReply, Body: "b", Calls: "[]"})
	require.NoError(t, err)
	entries := w.Entries()
	entries[1].Calls = "[1]"
	broken, _ := Verify(w.Run().Digest, entries)
	assert.Equal(t, uint32(2), broken)
	entries = w.Entries()
	entries[0].BodyDigest = ContentDigest("z")
	broken, _ = Verify(w.Run().Digest, entries)
	assert.Equal(t, uint32(1), broken, "a changed digest breaks the chain digest")
}

// A closed run resumes; the row's label is the join of its entries'
// labels, raised as they append; the run row moves by version.
func TestAClosedRunResumesAndTheLabelFloats(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	v := w.Run().Version
	_, err = w.Append(ctx, Entry{Kind: KindOutcome, Body: "x", Label: Label{Level: LevelExternal, Tier: TierAmber, Exposure: []string{"p"}}})
	require.NoError(t, err)
	r, _, _ := st.GetRun(ctx, "r")
	assert.Equal(t, Label{Level: LevelExternal, Tier: TierAmber, Exposure: []string{"p"}}, r.Label, "the store raised the row's label")
	assert.Equal(t, w.Run().Label, r.Label, "and the writer mirrors it")
	assert.Equal(t, w.Run().Head, w.Entries()[0].Digest, "the head is the last digest")
	assert.Greater(t, w.Run().Version, v)
	require.NoError(t, w.Update(ctx, func(r *Run) { r.Label = Label{} }))
	assert.Equal(t, TierAmber, w.Run().Label.Tier, "an update cannot lower the label")
	require.NoError(t, w.Close(ctx, StateClosed))
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "y"})
	require.Error(t, err)
	require.NoError(t, w.Resume(ctx))
	assert.Equal(t, StateOpen, w.Run().State)
	assert.True(t, w.Run().Closed.IsZero())
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "y"})
	require.NoError(t, err)
	require.NoError(t, w.Close(ctx, StateStranded))
	assert.Error(t, w.Resume(ctx), "a stranded run does not resume")
	stale := w.Run()
	stale.Version--
	assert.Error(t, st.UpdateRun(ctx, stale), "an older version is refused")
}

// A context done before anything was sent refuses the act alone: nothing
// reached the record, so the writer does not mark itself failed.
func TestADoneContextDoesNotFailTheWriter(t *testing.T) {
	ctx := context.Background()
	w, err := Open(ctx, NewMemStore(), Run{Id: "r"})
	require.NoError(t, err)
	done, cancel := context.WithCancel(ctx)
	cancel()
	_, err = w.Append(done, Entry{Kind: KindUser, Body: "x"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "x"})
	require.NoError(t, err, "the next append stands")
}

// A compaction or edit aims at an earlier entry: target zero or at or
// past its own ordinal is refused at append.
func TestACompactionAimsAtAnEarlierEntry(t *testing.T) {
	ctx := context.Background()
	w, err := Open(ctx, NewMemStore(), Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "x"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindCompact, Target: 0})
	assert.Error(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindEdit, Target: 2})
	assert.Error(t, err, "its own ordinal")
	_, err = w.Append(ctx, Entry{Kind: KindCompact, Target: 1})
	assert.NoError(t, err)
	assert.Equal(t, uint32(2), w.Len())
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// tombstoneFailingStore refuses the tombstone once.
type tombstoneFailingStore struct {
	StoreI
	fail bool
}

func (inst *tombstoneFailingStore) Tombstone(ctx context.Context, run string) error {
	if inst.fail {
		inst.fail = false
		return errors.New("disk gone")
	}
	return inst.StoreI.Tombstone(ctx, run)
}

// A deletion that fails after its entry leaves the writer failed and the
// run taking nothing: no entry follows a deleted entry. Reopen finishes
// the deletion: bodies emptied, the row deleted, the chain verifying.
func TestNothingFollowsTheDeletedEntry(t *testing.T) {
	ctx := context.Background()
	st := &tombstoneFailingStore{StoreI: NewMemStore(), fail: true}
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "secret"})
	require.NoError(t, err)
	require.Error(t, w.Delete(ctx, "person"), "the tombstone failed")
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "after deleted"})
	require.Error(t, err, "the writer is failed and the run has its deleted entry")
	entries, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "secret", entries[0].Body, "the bodies are still there")
	r, _, _ := st.GetRun(ctx, "r")
	assert.Equal(t, StateOpen, r.State, "and the row is not deleted")

	again, err := Reopen(ctx, st, "r")
	require.NoError(t, err, "reopen finishes the deletion")
	assert.Equal(t, StateDeleted, again.Run().State)
	assert.Equal(t, "person", again.Run().DeletedBy)
	entries, err = st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	assert.Empty(t, entries[0].Body)
	broken, err := VerifyRun(again.Run(), entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
	_, err = again.Append(ctx, Entry{Kind: KindUser, Body: "more"})
	assert.Error(t, err)
}

// A body blanked in a live run is not a tombstone: the chain commits to
// digests, so Verify must refuse an emptied body unless the chain ends
// with the deleted entry (law L31).
func TestABlankedBodyInALiveRunBreaksTheChain(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a"}, Entry{Kind: KindReply, Body: "b"})
	require.NoError(t, err)
	entries := w.Entries()
	entries[0].Body = ""
	broken, err := VerifyRun(w.Run(), entries)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), broken, "an emptied body in an open run breaks the chain at its entry")
	require.NoError(t, st.Tombstone(ctx, "r"), "a store-level tombstone without the deleted entry")
	entries, _ = st.Entries(ctx, "r", 0)
	broken, _ = VerifyRun(w.Run(), entries)
	assert.Equal(t, uint32(1), broken)
	_, err = Reopen(ctx, st, "r")
	assert.Error(t, err, "and the run does not reopen")
}

// A mark with a comma cannot be stored, so the writer refuses it, and a
// label joined with one drops it; an entry with valid marks survives the
// column round trip with its chain intact.
func TestAMarkWithACommaIsRefused(t *testing.T) {
	ctx := context.Background()
	w, err := Open(ctx, NewMemStore(), Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "x", Label: Label{Origin: []string{"a,b"}}})
	require.Error(t, err)
	assert.Nil(t, Label{}.Join(Label{Exposure: []string{"a,b"}}).Exposure)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "x", Label: Label{Origin: []string{"b", "a"}}})
	require.NoError(t, err)
	back := entryOf(entryRow(written[0]), "r")
	// The ledger row carries neither content nor opening; the vault
	// rejoins them (ADR-0320 §SD5).
	back.Body, back.Opening = written[0].Body, written[0].Opening
	assert.Equal(t, []string{"a", "b"}, back.Label.Origin)
	broken, err := Verify(w.Run().Digest, []Entry{back})
	require.NoError(t, err)
	assert.Zero(t, broken, "the row round trip keeps the chain")
}

// blockingStore holds an append until released.
type blockingStore struct {
	StoreI
	release chan struct{}
}

func (inst *blockingStore) Append(ctx context.Context, entries []Entry) error {
	<-inst.release
	return inst.StoreI.Append(ctx, entries)
}

// A reader never waits behind an append in flight: the store call is a
// bus round trip, and the state lock is not held across it.
func TestReadersDoNotWaitBehindAnAppend(t *testing.T) {
	ctx := context.Background()
	st := &blockingStore{StoreI: NewMemStore(), release: make(chan struct{})}
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, aerr := w.Append(ctx, Entry{Kind: KindUser, Body: "x"})
		done <- aerr
	}()
	read := make(chan int, 1)
	go func() { read <- len(w.Entries()) + int(w.Run().LastOrdinal) + int(w.Len()) }()
	select {
	case n := <-read:
		assert.Zero(t, n, "the entries before the append")
	case <-time.After(2 * time.Second):
		t.Fatal("a reader waited behind the append")
	}
	close(st.release)
	require.NoError(t, <-done)
	assert.Equal(t, uint32(1), w.Len())
}
