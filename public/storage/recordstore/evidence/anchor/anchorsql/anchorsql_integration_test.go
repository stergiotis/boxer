//go:build integration

package anchorsql_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor/anchorsql"
)

// The table store keeps the contract the memory store does — keyed by
// (cell, scope, ordinal, scheme), the first receipt counts, another digest
// is refused — and its receipts are durable: a second store over the same
// table, another host, reads them and is held to them.
func TestTheTableStoreKeepsTheContract(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	st := anchorsql.New(exec, "", "host-a")
	require.NoError(t, st.Provision(ctx))
	require.NoError(t, st.Provision(ctx), "idempotent")

	h := anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v1", Digest: "abc"}
	r, err := st.Append(ctx, h)
	require.NoError(t, err)
	assert.True(t, r.Durable)
	assert.Equal(t, "host-a", r.By)

	other := anchorsql.New(exec, "", "host-b")
	same, err := other.Append(ctx, h)
	require.NoError(t, err)
	assert.Equal(t, r.Id, same.Id, "the first receipt counts, whoever sends it again")
	assert.Equal(t, "host-a", same.By)
	assert.True(t, r.At.Equal(same.At))
	_, err = other.Append(ctx, anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v1", Digest: "xyz"})
	assert.Error(t, err, "another digest at the ordinal")
	_, err = st.Append(ctx, anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v2", Digest: "xyz"})
	require.NoError(t, err, "another scheme is another anchor")
	_, err = st.Append(ctx, anchor.Head{Cell: "c", Scope: 11, Ordinal: 4, Scheme: "s/v1", Digest: "def"})
	require.NoError(t, err)

	rows, err := other.Read(ctx, "c", 11)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	heads := anchor.HeadsOf(rows, "s/v1")
	require.Len(t, heads, 2)
	assert.Equal(t, uint64(3), heads[0].Ordinal)
	assert.Equal(t, r.Id, heads[0].ReceiptId)
	rows, err = st.Read(ctx, "c", 12)
	require.NoError(t, err)
	assert.Empty(t, rows)
	all := st.All()
	assert.Len(t, all, 3)
	for _, a := range all {
		assert.True(t, a.Receipt.Durable)
	}
}

// A receipt another writer's store holds binds this one: the same digest
// answers with that receipt, another digest is refused.
func TestAnotherWritersReceiptBinds(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	st := anchorsql.New(exec, "org.heads", "host-a")
	require.NoError(t, st.Provision(ctx))
	// Another writer's row, by another client of the organisation's table.
	require.NoError(t, exec.Exec(ctx, "INSERT INTO org.heads VALUES ('c', 1, 1, 's', 'other', 'anchor-early', fromUnixTimestamp64Nano(1, 'UTC'), 'host-b')"))
	_, err = st.Append(ctx, anchor.Head{Cell: "c", Scope: 1, Ordinal: 1, Scheme: "s", Digest: "mine"})
	require.Error(t, err)
	r, err := st.Append(ctx, anchor.Head{Cell: "c", Scope: 1, Ordinal: 1, Scheme: "s", Digest: "other"})
	require.NoError(t, err)
	assert.Equal(t, "anchor-early", r.Id)
}
