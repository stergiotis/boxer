package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/dimension/actor"
)

// TestActorStampingEndToEnd wires the actor dimension in as a device-store
// stamper (ADR-0295 §SD8): a write without a principal on its context is
// refused, a write with one carries the interned actor id in-row, and the
// ordered flush has made the descriptor durable by the time the row is.
func TestActorStampingEndToEnd(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	actorStore := actor.NewActorStore(exec, nil, actor.ActorStoreConfig{})
	require.NoError(t, actorStore.EnsureTable(ctx))
	idgen, err := mem.NewIdInternalizer(3, 1024)
	require.NoError(t, err)
	rec := actor.NewRecorder(idgen, actor.NewStoreSink(actorStore))

	dev := NewDeviceStore(exec, nil, DeviceStoreConfig{
		Stampers: []recordstore.ReferenceStamper{rec.Stamper()},
	})
	require.NoError(t, dev.EnsureTable(ctx))

	err = dev.Begin(1, recordstore.SeqTs(1)).AddIdentity(Identity{ID: 1, Status: "live"}).Commit()
	require.ErrorIs(t, err, actor.ErrNoPrincipal, "no identity, no write")
	assert.Zero(t, dev.Buffered())

	who := callident.WithCallIdentity(ctx, callident.CallIdentity{
		Origin: callident.Origin{Run: "r", App: "example.ledger"},
		Claims: callident.Claims{Principal: "p:9", Purpose: "rectification"},
	})
	require.NoError(t, dev.BeginCtx(who, 1, recordstore.SeqTs(1)).AddIdentity(Identity{ID: 1, Status: "live"}).Commit())
	_, err = dev.Flush(ctx)
	require.NoError(t, err)

	high := storedSymbolHighCardRef(t, ctx, exec, 1)
	require.Len(t, high, 1)
	// A fresh store over the same table, no cache: the descriptor is durable.
	fresh := actor.NewActorStore(exec, nil, actor.ActorStoreConfig{})
	ent, found, err := fresh.Latest(ctx, high[0])
	require.NoError(t, err)
	require.True(t, found, "the ordered flush made the descriptor durable with the row")
	assert.Equal(t, actor.Actor{ID: high[0], Principal: "p:9", Purpose: "rectification", App: "example.ledger"}, ent.Actor.Val)

	got, found, err := rec.Resolve(ctx, identifier.TaggedId(high[0]))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "p:9", got.Principal)
}
