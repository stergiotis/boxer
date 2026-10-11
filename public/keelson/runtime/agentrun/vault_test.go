package agentrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vaulted is the store a host runs, over memory.
func vaulted() (st *VaultedStore, ledger *MemStore, vault *MemBodyStore) {
	ledger, vault = NewMemStore(), NewMemBodyStore()
	return NewVaulted(ledger, vault), ledger, vault
}

// Content goes to the vault and never to the ledger; a read joins the
// two and the chain verifies over the join (ADR-0320 §SD1, §SD5).
func TestTheVaultHoldsTheContentAndTheLedgerNone(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, ledger, vault := vaulted()
	w, err := Open(ctx, st, Run{Id: "r", OwnerApp: "chat"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "a name"}, Entry{Kind: KindReply, Body: "hi", Calls: "[]"}, Entry{Kind: KindTurn})
	require.NoError(t, err)

	bare, err := ledger.Entries(ctx, "r", 0)
	require.NoError(t, err)
	require.Len(t, bare, 3)
	for _, e := range bare {
		assert.Empty(t, e.Body, "the ledger holds no body")
		assert.Empty(t, e.Calls)
		assert.Nil(t, e.Opening, "nor an opening")
	}
	assert.NotEmpty(t, bare[0].BodyDigest, "it holds the commitment")
	rows, err := vault.Get(ctx, "r", 0)
	require.NoError(t, err)
	assert.Len(t, rows, 2, "one vault row per entry with content")

	joined, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	assert.Equal(t, written, joined, "the join is the entry as written")
	broken, err := VerifyRun(w.Run(), joined)
	require.NoError(t, err)
	assert.Zero(t, broken)

	r, err := Reopen(ctx, st, "r")
	require.NoError(t, err)
	assert.Equal(t, "a name", r.Entries()[0].Body)
}

// A vault row that does not open its entry's commitments is ignored, and
// the entry reads as erased, which law L31 reports since no erasure names
// it (ADR-0320 §SD5).
func TestAVaultRowThatDoesNotOpenReadsAsErased(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, _, vault := vaulted()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	written, err := w.Append(ctx, Entry{Kind: KindUser, Body: "a name"})
	require.NoError(t, err)
	require.NoError(t, vault.Put(ctx, []Body{{Run: "r", Ordinal: 1, Opening: written[0].Opening, Body: "another name"}}))
	entries, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	assert.Empty(t, entries[0].Body)
	assert.True(t, entries[0].Tombstoned())
	broken, err := VerifyRun(w.Run(), entries)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), broken, "content gone without an erasure on record breaks the chain")
	_, err = Reopen(ctx, st, "r")
	assert.Error(t, err)
}

// Erasing one entry: an erased entry names it in the chain, the vault row
// is destroyed, the chain still verifies, the run goes on, and the erasure
// is refused where it must be (ADR-0320 §SD4).
func TestErasingOneEntryKeepsTheChain(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, _, vault := vaulted()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	written, err := w.Append(ctx,
		Entry{Kind: KindUser, Body: "ask"},
		Entry{Kind: KindOutcome, Key: "c1", Role: "tool", Body: `{"customer":"a name"}`},
		Entry{Kind: KindReply, Body: "the customer is a name"})
	require.NoError(t, err)
	require.NoError(t, w.Close(ctx, StateClosed), "a closed run erases")

	assert.Error(t, w.Erase(ctx, "person", 9), "no such entry")
	assert.Error(t, w.Erase(ctx, "person", 0), "no such entry")
	require.NoError(t, w.Erase(ctx, "person", 2, 3))

	entries, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	require.Len(t, entries, 5, "one erased entry per ordinal")
	assert.Equal(t, "ask", entries[0].Body, "the rest stays")
	assert.Empty(t, entries[1].Body)
	assert.Empty(t, entries[2].Body)
	assert.Nil(t, entries[1].Opening)
	assert.Equal(t, written[1].BodyDigest, entries[1].BodyDigest, "the commitment stays")
	assert.Equal(t, KindErased, entries[3].Kind)
	assert.Equal(t, uint32(2), entries[3].Target)
	assert.Equal(t, "person", entries[3].Name)
	assert.Equal(t, uint32(3), entries[4].Target)
	rows, err := vault.Get(ctx, "r", 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "the vault rows are destroyed")

	broken, err := VerifyRun(w.Run(), entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the erasure is in the chain")
	assert.Equal(t, w.Entries(), entries, "the writer's copies are emptied too")

	assert.Error(t, w.Erase(ctx, "person", 4), "an erasure is not content")
	assert.Error(t, w.Erase(ctx, "person", 2), "erasing again finds no content")

	r, err := Reopen(ctx, st, "r")
	require.NoError(t, err)
	require.NoError(t, r.Resume(ctx))
	_, err = r.Append(ctx, Entry{Kind: KindUser, Body: "on we go"})
	require.NoError(t, err, "the run continues after an erasure")
}

// A head entry's body is a digest the anchor law reads: it is not erased,
// and a deletion keeps its vault row (ADR-0314 §SD5).
func TestAHeadEntryIsNotErasedAndSurvivesDeletion(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, _, vault := vaulted()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a name"}, Entry{Kind: KindHead, Body: `{"ordinal":1}`})
	require.NoError(t, err)
	assert.Error(t, w.Erase(ctx, "person", 2), "a head entry is not content")
	require.NoError(t, w.Delete(ctx, "person"))
	entries, err := st.Entries(ctx, "r", 0)
	require.NoError(t, err)
	assert.Empty(t, entries[0].Body)
	assert.Equal(t, `{"ordinal":1}`, entries[1].Body, "the head's body stays")
	rows, err := vault.Get(ctx, "r", 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	broken, err := VerifyRun(w.Run(), entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
}

// An erasure the process did not finish, the erased entry chained and the
// vault row still there, is finished by Reopen.
func TestReopenFinishesAnErasure(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, _, vault := vaulted()
	w, err := Open(ctx, st, Run{Id: "r"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a name"})
	require.NoError(t, err)
	_, err = w.append(ctx, true, Entry{Kind: KindErased, Name: "person", Target: 1})
	require.NoError(t, err, "the erased entry alone, as a crash leaves it")
	rows, err := vault.Get(ctx, "r", 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the content is still there")

	r, err := Reopen(ctx, st, "r")
	require.NoError(t, err)
	assert.Empty(t, r.Entries()[0].Body)
	rows, err = vault.Get(ctx, "r", 0)
	require.NoError(t, err)
	assert.Empty(t, rows, "Reopen finished the erasure")
}

// The service erases only what the chain names, through the bus.
func TestTheServiceErasesWhatTheChainNames(t *testing.T) {
	fixedClock(t)
	fixedOpenings(t)
	ctx := context.Background()
	st, _, _ := vaulted()
	w, err := Open(ctx, st, Run{Id: "r", OwnerApp: "chat"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a name"})
	require.NoError(t, err)
	svc := &Service{store: st}
	assert.Equal(t, "nothing to erase", svc.erasable(ctx, w.Run(), nil))
	assert.Contains(t, svc.erasable(ctx, w.Run(), []uint32{1}), "follows the erased entry")
	_, err = w.append(ctx, true, Entry{Kind: KindErased, Name: "person", Target: 1})
	require.NoError(t, err)
	assert.Empty(t, svc.erasable(ctx, w.Run(), []uint32{1}))
	assert.Contains(t, svc.erasable(ctx, w.Run(), []uint32{3}), "no such entry")
}
