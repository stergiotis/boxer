package example

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/dimension/provenance"
	"github.com/stergiotis/boxer/public/storage/recordstore/example/internal/lowlevel"
	"github.com/stretchr/testify/require"
)

// TestProvenanceStampingEndToEnd wires a provenance DimensionStore in as a
// device-store stamper (ADR-0112 S2). A write stamps its attributes with the
// interned surrogate id (via the M1 ambient primitive); reading the stored
// row's HighCardRef membership back from ClickHouse and resolving it returns
// this test's host and call stack.
func TestProvenanceStampingEndToEnd(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}

	// Provenance descriptor store — NO stampers (the recursion guard) — over the
	// same executor, plus a recorder that stamps through it.
	provStore := provenance.NewProvenanceStore(exec, nil, provenance.ProvenanceStoreConfig{})
	require.NoError(t, provStore.EnsureTable(ctx))
	idgen, err := mem.NewIdInternalizer(1, 1024)
	require.NoError(t, err)
	rec, err := provenance.NewRecorder(idgen, provenance.NewStoreSink(provStore))
	require.NoError(t, err)

	// Device store WITH the provenance stamper registered.
	dev := NewDeviceStore(exec, nil, DeviceStoreConfig{
		Stampers: []recordstore.ReferenceStamper{rec.Stamper()},
	})
	require.NoError(t, dev.EnsureTable(ctx))

	// Write one entity; Begin consults the stamper and stamps its attributes.
	require.NoError(t, dev.Begin(1, recordstore.SeqTs(1)).
		AddIdentity(Identity{ID: 1, Status: "live"}).Commit())
	n, err := dev.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	// No manual provenance flush: dev.Flush ordered-flushed the bound provenance
	// store before its own insert (ADR-0112 SD5), so the descriptor fact is
	// already durable and the Resolve below finds it.

	// The typed decode is unaffected by the stamp: the Identity component
	// round-trips intact, because the decode never reads the stamp's lane.
	ent, found, err := dev.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, ent.Identity.Has)
	require.Equal(t, "live", ent.Identity.Val.Status)

	// The stored symbol attribute carries exactly one HighCardRef membership: the stamp.
	high := storedSymbolHighCardRef(t, ctx, exec, 1)
	require.Len(t, high, 1)

	// And it resolves to this test's provenance.
	prov, found, err := rec.Resolve(ctx, identifier.TaggedId(high[0]))
	require.NoError(t, err)
	require.True(t, found)
	host, _ := os.Hostname()
	require.Equal(t, host, prov.Host)
	require.Contains(t, strings.Join(prov.Stack, "\n"), "TestProvenanceStampingEndToEnd")
}

// storedSymbolHighCardRef reads the HighCardRef memberships of the first symbol
// attribute of the key row straight from ClickHouse via the read-access classes
// (the typed store decode ignores them).
func storedSymbolHighCardRef(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, key uint64) (out []uint64) {
	sql := "SELECT * FROM " + DeviceTableName + " WHERE " + DeviceColKey + " = " + deviceKeyLiteral(key) + deviceArrowOutputSettings
	for rec, err := range exec.QueryArrow(ctx, sql) {
		require.NoError(t, err)
		symR := lowlevel.NewReadAccessDeviceTableTaggedSymbol()
		require.NoError(t, symR.LoadFromRecord(rec))
		out = slices.Collect(symR.GetMemberships().GetMembValueHighCardRef(raruntime.EntityIdx(0), raruntime.AttributeIdx(0)))
		symR.Release()
		rec.Release()
	}
	return
}
