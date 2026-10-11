//go:build integration

package agentrun

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
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

	w, err := Open(ctx, st, Run{Id: "cell/r1", Cell: "cell", Conversation: "c1", OwnerApp: "chat", Coordinator: "w1", Label: Label{Tier: TierGreen}})
	require.NoError(t, err)
	_, err = w.Append(ctx,
		Entry{Kind: KindUser, Role: "user", Body: "hello", Turn: "t1", Label: Label{Level: LevelPerson}},
		Entry{Kind: KindRequest, Key: "llm-1", Body: `{"digest":"x"}`, Turn: "t1"},
		Entry{Kind: KindReply, Key: "llm-1", Role: "assistant", Body: "hi", Calls: `[{"id":"c1"}]`, Turn: "t1", Label: Label{Level: LevelSystem, Origin: []string{"openai"}}})
	require.NoError(t, err)
	require.NoError(t, w.Update(ctx, func(r *Run) { r.Fingerprint = "fp"; r.Label = r.Label.Join(Label{Tier: TierAmber}) }))

	got, found, err := st.GetRun(ctx, "cell/r1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "fp", got.Fingerprint)
	assert.Equal(t, TierAmber, got.Label.Tier)
	assert.Equal(t, uint32(3), got.LastOrdinal)
	assert.Equal(t, StateOpen, got.State)
	runs, err := st.ListRuns(ctx, "chat", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 1, "latest-wins: one row per run")

	entries, err := st.Entries(ctx, "cell/r1", 0)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, []string{"openai"}, entries[2].Label.Origin)
	assert.Equal(t, `[{"id":"c1"}]`, entries[2].Calls)
	broken, err := Verify(got.Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain reads back intact")

	again, err := Reopen(ctx, st, "cell/r1")
	require.NoError(t, err)
	assert.Equal(t, uint32(3), again.Len())
	_, err = again.Append(ctx, Entry{Kind: KindTurn, Turn: "t1", Body: "answered"})
	require.NoError(t, err)
	from2, err := st.Entries(ctx, "cell/r1", 2)
	require.NoError(t, err)
	assert.Len(t, from2, 3)

	require.NoError(t, again.Close(ctx, StateClosed))
	require.NoError(t, again.Resume(ctx))
	require.NoError(t, again.Delete(ctx, "person"))
	after, err := st.Entries(ctx, "cell/r1", 0)
	require.NoError(t, err)
	require.Len(t, after, 5)
	assert.Empty(t, after[0].Body)
	assert.Empty(t, after[2].Calls)
	assert.True(t, after[2].Tombstoned())
	assert.Equal(t, entries[0].Digest, after[0].Digest)
	assert.Equal(t, KindDeleted, after[4].Kind)
	assert.Equal(t, "person", after[4].Name)
	got, _, err = st.GetRun(ctx, "cell/r1")
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
	reopened, err := Reopen(ctx, fresh, "cell/r1")
	require.NoError(t, err)
	assert.Equal(t, StateDeleted, reopened.Run().State)
	assert.Equal(t, got.Version, reopened.Run().Version)
	assert.Len(t, reopened.Entries(), 5)
	assert.True(t, reopened.Entries()[0].Tombstoned())
	all, err := fresh.AllEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 5, "one entry per ordinal across the record")
	assert.EqualValues(t, 5, rowCount(t, ctx, exec, layout, "cell/r1"), "the ledger never held content, so a deletion adds no row to it")
	assert.EqualValues(t, 0, bodyCount(t, ctx, exec, layout, "cell/r1"), "the vault rows are destroyed")
	of, err := fresh.EntriesOf(ctx, []string{"cell/r1", "none"})
	require.NoError(t, err)
	assert.Len(t, of, 5)
}

// rowCount is the raw number of rows the entry table holds for a run,
// every revision alike.
func rowCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run string) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.EntryTable()+" WHERE "+agentrunstore.EntryColKey+" = "+marshalling.EscapeString(run)) {
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
	w, err := Open(ctx, st, Run{Id: "r1", OwnerApp: "chat"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "one", Label: Label{Tier: TierAmber}})
	require.NoError(t, err)
	exec.fail = true
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "two"})
	require.Error(t, err, "the entries landed and the row did not")
	r, found, err := st.GetRun(ctx, "r1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint32(2), r.LastOrdinal, "the row is rebuilt from the entry tail")
	assert.Equal(t, TierAmber, r.Label.Tier)
	again, err := Reopen(ctx, st, "r1")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), again.Len())
	more, err := again.Append(ctx, Entry{Kind: KindUser, Body: "three"})
	require.NoError(t, err)
	assert.Equal(t, uint32(3), more[0].Ordinal)
	freshLedger := NewSqlStore(local, layout)
	defer freshLedger.Close()
	fresh := NewVaulted(freshLedger, vault)
	r, _, err = fresh.GetRun(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, uint32(3), r.LastOrdinal, "the repaired row was written")
	assert.Equal(t, more[0].Digest, r.Head)
}

// Two stores over one table, as two processes would be: both read the
// run row, the first rewrites it, and the second's rewrite — guarded on
// the version it read — loses instead of overwriting (ADR-0308 §SD9).
// The loser's cache is dropped, so its next read sees the winner's row.
func TestASecondStoreLosesTheRunRow(t *testing.T) {
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
	require.NoError(t, a.CreateRun(ctx, Run{Id: "r1", OwnerApp: "chat", State: StateOpen, Version: 1}))
	ra, found, err := a.GetRun(ctx, "r1")
	require.NoError(t, err)
	require.True(t, found)
	rb, found, err := b.GetRun(ctx, "r1")
	require.NoError(t, err)
	require.True(t, found)

	ra.Fingerprint, ra.Version = "from-a", 2
	require.NoError(t, a.UpdateRun(ctx, ra))
	rb.Fingerprint, rb.Version = "from-b", 2
	require.ErrorContains(t, b.UpdateRun(ctx, rb), "another writer holds the run")

	got, _, err := b.GetRun(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "from-a", got.Fingerprint, "the loser reads the winner's row")
	assert.Equal(t, uint32(2), got.Version)
	assert.EqualValues(t, 1, runRowCount(t, ctx, exec, layout, "r1"), "one row per run, rewritten in place")
}

// runRowCount is the raw number of rows the run table holds for a run.
func runRowCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run string) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.RunTable()+" WHERE "+agentrunstore.RunColKey+" = "+marshalling.EscapeString(run)) {
		require.NoError(t, err)
		if rec.NumRows() > 0 {
			n = rec.Column(0).(*array.Uint64).Value(0)
		}
		rec.Release()
	}
	return
}

// bodyCount is the raw number of vault rows for a run.
func bodyCount(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, layout agentrunstore.Layout, run string) (n uint64) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT count() FROM "+layout.BodyTable()+" WHERE "+agentrunstore.BodyRowColKey+" = "+marshalling.EscapeString(run)) {
		require.NoError(t, err)
		if rec.NumRows() > 0 {
			n = rec.Column(0).(*array.Uint64).Value(0)
		}
		rec.Release()
	}
	return
}
