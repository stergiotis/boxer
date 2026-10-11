//go:build integration

package agentrun

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// A run's life on the record under clickhouse local: the row is created
// and rewritten latest-wins, entries append and read back in order with
// their chain intact, a reopened writer continues, and a tombstone keeps
// digests and drops bodies.
func TestTheSqlStoreUnderClickhouseLocal(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "artest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout), "idempotent")
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, exec, layout))
	// The host's shape (ADR-0320 §SD1): the SQL store is the ledger, the
	// vault holds the content, the vaulted store is what a writer sees.
	ledger := NewSqlStore(exec, layout)
	defer ledger.Close()
	vault := NewSqlBodyStore(exec, layout)
	defer vault.Close()
	st := NewVaulted(ledger, vault)

	w, err := Open(ctx, st, Run{Cell: "cell", Conversation: "c1", OwnerApp: "chat", Coordinator: "w1", Label: Label{Tier: TierGreen}})
	require.NoError(t, err)
	// The run id is minted from the persisted stream, leased through the
	// layout's lease table (ADR-0325 §SD5).
	id := w.Run().Id
	assert.Equal(t, vocab.IdStreamAgentRun.Value(), identifier.TaggedId(id).GetTag().GetValue())
	_, err = w.Append(ctx,
		Entry{Kind: KindUser, Role: "user", Body: "hello", Turn: "t1", Label: Label{Level: LevelPerson}},
		Entry{Kind: KindRequest, Key: "llm-1", Body: `{"digest":"x"}`, Turn: "t1"},
		Entry{Kind: KindReply, Key: "llm-1", Role: "assistant", Body: "hi", Calls: `[{"id":"c1"}]`, Turn: "t1", Label: Label{Level: LevelSystem, Origin: []string{"openai"}}})
	require.NoError(t, err)
	require.NoError(t, w.Update(ctx, func(r *Run) { r.Fingerprint = "fp"; r.Label = r.Label.Join(Label{Tier: TierAmber}) }))

	got, found, err := st.GetRun(ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "fp", got.Fingerprint)
	assert.Equal(t, TierAmber, got.Label.Tier)
	assert.Equal(t, uint32(3), got.LastOrdinal)
	assert.Equal(t, StateOpen, got.State)
	runs, err := st.ListRuns(ctx, "chat", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "latest-wins: one row per run")

	entries, err := st.Entries(ctx, id, 0)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, []string{"openai"}, entries[2].Label.Origin)
	assert.Equal(t, `[{"id":"c1"}]`, entries[2].Calls)
	broken, err := Verify(got.Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain reads back intact")

	again, err := Reopen(ctx, st, id)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), again.Len())
	_, err = again.Append(ctx, Entry{Kind: KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	from2, err := st.Entries(ctx, id, 2)
	require.NoError(t, err)
	assert.Len(t, from2, 3)

	require.NoError(t, again.Close(ctx, StateClosed))
	require.NoError(t, again.Resume(ctx))
	require.NoError(t, again.Delete(ctx, "person"))
	after, err := st.Entries(ctx, id, 0)
	require.NoError(t, err)
	require.Len(t, after, 5)
	assert.Empty(t, after[0].Body)
	assert.Empty(t, after[2].Calls)
	assert.True(t, after[2].Tombstoned())
	assert.Equal(t, entries[0].Digest, after[0].Digest)
	assert.Equal(t, KindDeleted, after[4].Kind)
	assert.Equal(t, "person", after[4].Name)
	got, _, err = st.GetRun(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StateDeleted, got.State)
	assert.Equal(t, "person", got.DeletedBy)
	broken, err = VerifyRun(got, after)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain verifies after the tombstone (L31)")

	// A fresh store over the same tables reads the record, not a cache:
	// the tombstone's revisions win, the row is the latest version, the
	// chain holds, and the deleted run reopens read-only.
	freshLedger := NewSqlStore(exec, layout)
	defer freshLedger.Close()
	fresh := NewVaulted(freshLedger, vault)
	reopened, err := Reopen(ctx, fresh, id)
	require.NoError(t, err)
	assert.Equal(t, StateDeleted, reopened.Run().State)
	assert.Equal(t, got.Version, reopened.Run().Version)
	assert.Len(t, reopened.Entries(), 5)
	assert.True(t, reopened.Entries()[0].Tombstoned())
	all, err := fresh.AllEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 5, "one entry per ordinal across the record")
	assert.EqualValues(t, 5, rowCount(t, ctx, exec, layout, id), "the ledger never held content, so a deletion adds no row to it")
	assert.EqualValues(t, 0, bodyCount(t, ctx, exec, layout, id), "the vault rows are destroyed")
	of, err := fresh.EntriesOf(ctx, []uint64{id, 1})
	require.NoError(t, err)
	assert.Len(t, of, 5)
}

// rowCount is the raw number of rows the entry table holds for a run,
// every revision alike.
func rowCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run uint64) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.EntryTable()+" WHERE "+agentrunstore.EntryColKey+" = "+strconv.FormatUint(run, 10)) {
		require.NoError(t, err)
		if rec.NumRows() > 0 {
			n = rec.Column(0).(*array.Uint64).Value(0)
		}
		rec.Release()
	}
	return
}

// laggingExecutor fails the next write to one table, once: an insert, or
// the in-place update a run row is rewritten by.
type laggingExecutor struct {
	recordstore.ExecutorI
	table string
	fail  bool
}

func (inst *laggingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if inst.fail && table == inst.table {
		inst.fail = false
		return errors.New("the run table is away")
	}
	return inst.ExecutorI.InsertArrow(ctx, table, records)
}

func (inst *laggingExecutor) Exec(ctx context.Context, sql string) (err error) {
	if inst.fail && strings.HasPrefix(sql, "UPDATE "+inst.table+" ") {
		inst.fail = false
		return errors.New("the run table is away")
	}
	return inst.ExecutorI.Exec(ctx, sql)
}

// A run row write that fails after its entries landed does not wedge the
// run: the append reports it, the row is rebuilt from the entry tail at
// the next read, and the reopened writer continues at the ordinal the
// record holds.
func TestARunRowThatLagsItsEntriesIsRepaired(t *testing.T) {
	ctx := context.Background()
	local, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "artest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, local, layout))
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, local, layout))
	exec := &laggingExecutor{ExecutorI: local, table: layout.RunTable()}
	ledger, vault := NewSqlStore(exec, layout), NewSqlBodyStore(exec, layout)
	defer ledger.Close()
	defer vault.Close()
	st := NewVaulted(ledger, vault)
	w, err := Open(ctx, st, Run{Id: 11, OwnerApp: "chat"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "one", Label: Label{Tier: TierAmber}})
	require.NoError(t, err)
	exec.fail = true
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "two"})
	require.Error(t, err, "the entries landed and the row did not")
	r, found, err := st.GetRun(ctx, 11)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint32(2), r.LastOrdinal, "the row is rebuilt from the entry tail")
	assert.Equal(t, TierAmber, r.Label.Tier)
	again, err := Reopen(ctx, st, 11)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), again.Len())
	more, err := again.Append(ctx, Entry{Kind: KindUser, Body: "three"})
	require.NoError(t, err)
	assert.Equal(t, uint32(3), more[0].Ordinal)
	freshLedger := NewSqlStore(local, layout)
	defer freshLedger.Close()
	fresh := NewVaulted(freshLedger, vault)
	r, _, err = fresh.GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), r.LastOrdinal, "the repaired row was written")
	assert.Equal(t, more[0].Digest, r.Head)
}

// deadLiveness reports every holder but one's own as gone.
type deadLiveness struct{}

func (deadLiveness) Alive(context.Context, Holder) (bool, error) { return false, nil }

func leaseStores(t *testing.T) (exec *chexec.LocalExecutor, layout agentrunstore.Layout, a *SqlStore, b *SqlStore) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout = agentrunstore.Layout{Database: "artest"}
	require.NoError(t, agentrunstore.ProvisionIn(context.Background(), exec, layout))
	a, b = NewSqlStore(exec, layout), NewSqlStore(exec, layout)
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	require.NotEqual(t, a.Holder.Run, b.Holder.Run, "two stores write as two holders")
	return
}

// The writer lease (ADR-0319 §SD7): while a live process holds an open
// run, another is refused before it writes; closing releases the lease,
// the next act takes the run over at the next epoch, and the first holder
// is then the one refused. A holder that no longer runs is taken over.
func TestTheWriterLease(t *testing.T) {
	ctx := context.Background()
	exec, layout, a, b := leaseStores(t)
	require.NoError(t, a.CreateRun(ctx, Run{Id: 11, OwnerApp: "chat", State: StateOpen, Version: 1}))
	r, _, err := b.GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Equal(t, a.Holder.Run, r.Holder)
	assert.EqualValues(t, 1, r.Epoch)

	r.Fingerprint, r.Version = "from-b", r.Version+1
	assert.ErrorIs(t, b.UpdateRun(ctx, r), ErrHeldElsewhere, "a live holder keeps an open run")

	ra, _, err := a.GetRun(ctx, 11)
	require.NoError(t, err)
	ra.State, ra.Version = StateClosed, ra.Version+1
	require.NoError(t, a.UpdateRun(ctx, ra))
	got, _, err := NewSqlStore(exec, layout).GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Empty(t, got.Holder, "a closed run has no writer")

	rb, _, err := b.GetRun(ctx, 11)
	require.NoError(t, err)
	rb.State, rb.Version = StateOpen, rb.Version+1
	require.NoError(t, b.UpdateRun(ctx, rb), "a released run is taken over")
	got, _, err = NewSqlStore(exec, layout).GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Equal(t, b.Holder.Run, got.Holder)
	assert.EqualValues(t, 2, got.Epoch)

	ra, _, err = a.GetRun(ctx, 11)
	require.NoError(t, err)
	ra.Fingerprint, ra.Version = "from-a", ra.Version+1
	assert.ErrorIs(t, a.UpdateRun(ctx, ra), ErrHeldElsewhere, "the first holder is now the one refused")

	a.Liveness = deadLiveness{}
	ra, _, err = a.GetRun(ctx, 11)
	require.NoError(t, err)
	ra.Fingerprint, ra.Version = "from-a", ra.Version+1
	require.NoError(t, a.UpdateRun(ctx, ra), "a holder that no longer runs is taken over")
	got, _, err = NewSqlStore(exec, layout).GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Equal(t, a.Holder.Run, got.Holder)
	assert.EqualValues(t, 3, got.Epoch)
	assert.Equal(t, "from-a", got.Fingerprint)
	assert.EqualValues(t, 1, runRowCount(t, ctx, exec, layout, 11), "one row per run, rewritten in place")
}

// takeoverBeforeInsert runs a takeover once, just before the next insert
// into the entry table: the window a holder's in-flight batch is in when
// the holder is declared gone.
type takeoverBeforeInsert struct {
	recordstore.ExecutorI
	table string
	run   func()
}

func (inst *takeoverBeforeInsert) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if inst.run != nil && table == inst.table {
		f := inst.run
		inst.run = nil
		f()
	}
	return inst.ExecutorI.InsertArrow(ctx, table, records)
}

// The epoch fence: a batch that had passed its claim lands after another
// process took the run over and wrote the same ordinal. The new holder's
// entry is at a higher revision and wins the ordinal; the old holder's
// confirm fails; a fresh reader sees the new holder's chain, and it holds.
func TestABatchInFlightAtATakeoverLosesItsOrdinals(t *testing.T) {
	fixedOpenings(t)
	ctx := context.Background()
	local, layout, _, ledgerB := leaseStores(t)
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, local, layout))
	exec := &takeoverBeforeInsert{ExecutorI: local, table: layout.EntryTable()}
	ledgerA := NewSqlStore(exec, layout)
	defer ledgerA.Close()
	ledgerB.Liveness = deadLiveness{}
	vault := func(e recordstore.ExecutorI) *SqlBodyStore {
		v := NewSqlBodyStore(e, layout)
		t.Cleanup(v.Close)
		return v
	}
	// The host's shape (ADR-0320 §SD1): a ledger under a vault.
	a, b := NewVaulted(ledgerA, vault(exec)), NewVaulted(ledgerB, vault(local))

	wa, err := Open(ctx, a, Run{Id: 11, OwnerApp: "chat"})
	require.NoError(t, err)
	_, err = wa.Append(ctx, Entry{Kind: KindUser, Role: "user", Body: "one"})
	require.NoError(t, err)

	exec.run = func() {
		wb, rerr := Reopen(ctx, b, 11)
		require.NoError(t, rerr)
		_, aerr := wb.Append(ctx, Entry{Kind: KindUser, Role: "user", Body: "two, from b"})
		require.NoError(t, aerr, "the dead holder's run is taken over and written")
	}
	_, err = wa.Append(ctx, Entry{Kind: KindUser, Role: "user", Body: "two, from a"})
	require.Error(t, err, "the old holder's confirm fails after the takeover")

	freshLedger := NewSqlStore(local, layout)
	defer freshLedger.Close()
	fresh := NewVaulted(freshLedger, vault(local))
	r, _, err := fresh.GetRun(ctx, 11)
	require.NoError(t, err)
	assert.Equal(t, ledgerB.Holder.Run, r.Holder)
	entries, err := fresh.Entries(ctx, 11, 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "two, from b", entries[1].Body, "the new holder's revision wins the ordinal")
	broken, err := VerifyRun(r, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain holds from the head")
}

// runRowCount is the raw number of rows the run table holds for a run.
func runRowCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run uint64) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.RunTable()+" WHERE "+agentrunstore.RunColKey+" = "+strconv.FormatUint(run, 10)) {
		require.NoError(t, err)
		if rec.NumRows() > 0 {
			n = rec.Column(0).(*array.Uint64).Value(0)
		}
		rec.Release()
	}
	return
}

// bodyCount is the raw number of vault rows for a run.
func bodyCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run uint64) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.BodyTable()+" WHERE "+agentrunstore.BodyRowColKey+" = "+strconv.FormatUint(run, 10)) {
		require.NoError(t, err)
		if rec.NumRows() > 0 {
			n = rec.Column(0).(*array.Uint64).Value(0)
		}
		rec.Release()
	}
	return
}

// Two stores over one database, as two processes would hold them, mint
// run and entry ids that never collide: each leases its own blocks
// through the one lease table (ADR-0319 §SD3, ADR-0325 §SD5).
func TestTwoStoresMintDistinctIds(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "artest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	a, b := NewSqlStore(exec, layout), NewSqlStore(exec, layout)
	defer a.Close()
	defer b.Close()
	runs := map[uint64]bool{}
	entries := map[uint64]bool{}
	var first uint64
	for i := 0; i < 3; i++ {
		for _, st := range []*SqlStore{a, b} {
			w, oerr := Open(ctx, st, Run{OwnerApp: "chat", Conversation: "c" + strconv.Itoa(i)})
			require.NoError(t, oerr)
			require.False(t, runs[w.Run().Id], "run id %d minted twice", w.Run().Id)
			runs[w.Run().Id] = true
			if first == 0 {
				first = w.Run().Id
			}
			written, aerr := w.Append(ctx, Entry{Kind: KindUser, Role: "user", Body: "x"}, Entry{Kind: KindTurn, TurnEnd: &TurnState{State: "answered"}})
			require.NoError(t, aerr)
			for _, e := range written {
				require.NotZero(t, e.EntryId)
				require.False(t, entries[e.EntryId], "entry id %d minted twice", e.EntryId)
				entries[e.EntryId] = true
			}
		}
	}
	back, err := a.Entries(ctx, first, 0)
	require.NoError(t, err)
	require.Len(t, back, 2)
	for _, e := range back {
		assert.True(t, entries[e.EntryId], "an entry reads back with the id it was written under")
	}
}

// The run list reads newest first and stops at the bound in the statement,
// scoped to an owner when one is named.
func TestTheRunListIsNewestFirstAndBounded(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "artest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	st := NewSqlStore(exec, layout)
	defer st.Close()
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for i, owner := range []string{"chat", "other", "chat", "chat"} {
		require.NoError(t, st.CreateRun(ctx, Run{Id: uint64(10 + i), OwnerApp: owner, State: StateOpen, Created: base.Add(time.Duration(i) * time.Minute)}))
	}
	fresh := NewSqlStore(exec, layout)
	defer fresh.Close()
	ids := func(owner string, limit int) (out []uint64) {
		runs, lerr := fresh.ListRuns(ctx, owner, limit)
		require.NoError(t, lerr)
		for _, r := range runs {
			out = append(out, r.Id)
		}
		return
	}
	assert.Equal(t, []uint64{13, 12, 11, 10}, ids("", 0))
	assert.Equal(t, []uint64{13, 12}, ids("chat", 2))
	assert.Equal(t, []uint64{11}, ids("other", 0))
}

// A journal table an earlier schema created is not accepted silently:
// CREATE TABLE IF NOT EXISTS succeeds against any shape, so provisioning
// checks the shape and names the drift, and the host falls back to the
// in-process store instead of failing at the first insert.
func TestProvisioningRefusesAnOldTableShape(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "artest"}
	require.NoError(t, exec.Exec(ctx, "CREATE DATABASE IF NOT EXISTS artest"))
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE artest.agentrunentry (`id:id:s:4::0:` String, `ts:ts:z64:47::0:` DateTime64(9)) ENGINE = MergeTree ORDER BY tuple()"))
	err = agentrunstore.ProvisionIn(ctx, exec, layout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry table")
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, exec, layout), "a fresh vault table provisions")
}
