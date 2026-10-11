//go:build integration

package agentrun

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// The vault under clickhouse local (ADR-0320 §SD4, §SD5): rows round
// trip with their openings, an erasure rewrites the named rows out of
// the table and the chain still verifies, a deletion keeps the head
// entries' rows, and a row that does not open is read as erased.
func TestTheVaultUnderClickhouseLocal(t *testing.T) {
	fixedOpenings(t)
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := agentrunstore.Layout{Database: "vtest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, exec, layout))
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, exec, layout), "idempotent")
	ledger := NewSqlStore(exec, layout)
	defer ledger.Close()
	vault := NewSqlBodyStore(exec, layout)
	defer vault.Close()
	st := NewVaulted(ledger, vault)

	w, err := Open(ctx, st, Run{Id: "cell/v1", Cell: "cell", OwnerApp: "chat"})
	require.NoError(t, err)
	written, err := w.Append(ctx,
		Entry{Kind: KindUser, Role: "user", Body: "a name", Turn: "t1"},
		Entry{Kind: KindOutcome, Key: "c1", Role: "tool", Body: `{"customer":"a name"}`, Turn: "t1"},
		Entry{Kind: KindReply, Key: "llm-1", Role: "assistant", Body: "hi", Calls: `[{"id":"c1"}]`, Turn: "t1"},
		Entry{Kind: KindHead, Body: `{"ordinal":3}`})
	require.NoError(t, err)

	entries, err := st.Entries(ctx, "cell/v1", 0)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	assert.Equal(t, written, entries, "the join reads the entries as written, openings included")
	bare, err := ledger.Entries(ctx, "cell/v1", 0)
	require.NoError(t, err)
	assert.Empty(t, bare[0].Body, "the ledger holds no content")
	assert.Nil(t, bare[0].Opening)
	assert.EqualValues(t, 4, bodyCount(t, ctx, exec, layout, "cell/v1"))

	require.NoError(t, w.Erase(ctx, "person", 2))
	assert.EqualValues(t, 3, bodyCount(t, ctx, exec, layout, "cell/v1"), "the mutation rewrote the row out of the table")
	entries, err = st.Entries(ctx, "cell/v1", 0)
	require.NoError(t, err)
	require.Len(t, entries, 5)
	assert.Empty(t, entries[1].Body)
	assert.Equal(t, written[1].BodyDigest, entries[1].BodyDigest)
	assert.Equal(t, "hi", entries[2].Body, "the rest stays")
	assert.Equal(t, KindErased, entries[4].Kind)
	r, _, err := st.GetRun(ctx, "cell/v1")
	require.NoError(t, err)
	broken, err := VerifyRun(r, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain verifies over the erasure (L31)")

	from3, err := st.Entries(ctx, "cell/v1", 3)
	require.NoError(t, err)
	require.Len(t, from3, 3)
	assert.Equal(t, "hi", from3[0].Body, "a bounded read joins its tail")

	require.NoError(t, w.Delete(ctx, "person"))
	assert.EqualValues(t, 1, bodyCount(t, ctx, exec, layout, "cell/v1"), "a deletion keeps the head entry's row")
	entries, err = st.Entries(ctx, "cell/v1", 0)
	require.NoError(t, err)
	assert.Equal(t, `{"ordinal":3}`, entries[3].Body)
	assert.Empty(t, entries[0].Body)
	r, _, err = st.GetRun(ctx, "cell/v1")
	require.NoError(t, err)
	broken, err = VerifyRun(r, entries)
	require.NoError(t, err)
	assert.Zero(t, broken)

	// A fresh store over the same tables reads the record: the deleted
	// run reopens read-only with its content gone.
	freshLedger := NewSqlStore(exec, layout)
	defer freshLedger.Close()
	reopened, err := Reopen(ctx, NewVaulted(freshLedger, vault), "cell/v1")
	require.NoError(t, err)
	assert.Equal(t, StateDeleted, reopened.Run().State)
	assert.Empty(t, reopened.Entries()[0].Body)
	assert.True(t, reopened.Entries()[0].Tombstoned())
}
