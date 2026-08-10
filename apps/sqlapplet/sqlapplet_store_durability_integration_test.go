//go:build integration

package sqlapplet

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// durabilityDb is a scratch database, not the runtime's own boxer.facts.
// The store writes user-authored applet documents; pointing this at the
// live table would mix test documents into whatever the developer's
// running desktop has stored.
const durabilityDb = "sqlapplet_store_durability_test"

// bootStore stands up one complete "process worth" of wiring over an
// already-constructed facts store: a fresh bus, a fresh persist service on
// the facts backend, a fresh registry, and the store service booting from
// whatever it finds in persist. Nothing is shared with a previous boot
// except the ClickHouse rows themselves — which is the whole point, since
// the in-process reload test (TestStoreReloadMintsAtBoot) keeps the persist
// service alive across the "reboot" and therefore passed throughout the
// period when nothing was durable at all.
func bootStore(t *testing.T, cfg chstore.Config) (reg *app.Registry, svc *StoreService, caller *inprocbus.Client) {
	t.Helper()
	store, err := chstore.New(cfg)
	require.NoError(t, err)
	backend, err := persist.NewFactsBackend(store)
	require.NoError(t, err)

	bus := inprocbus.NewInst(zerolog.Nop())
	ps, err := persist.NewService(bus, zerolog.Nop(), backend)
	require.NoError(t, err)
	t.Cleanup(ps.Close)

	reg = app.NewRegistry()
	svc, err = startStore(reg, bus, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)
	t.Cleanup(svc.Stop)

	caller = bus.NewClient("test.author", []app.SubjectFilter{
		{Pattern: appletstore.SubjectSave, Direction: app.CapDirectionBoth, Reason: "test author"},
	})
	return
}

// TestStoreSurvivesProcessRestart_LiveCH is the end-to-end durability check
// for ADR-0132 O4 over the ADR-0026 §SD6 facts backend: an applet saved by
// one boot must be minted by the next boot, with only ClickHouse in between.
//
// It also asserts the negative — the same second boot on a memory backend
// finds nothing — so a regression that silently reverts the wiring fails
// here rather than passing on an assertion with no teeth.
func TestStoreSurvivesProcessRestart_LiveCH(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}

	cfg := chstore.Defaults()
	cfg.Database = durabilityDb
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+durabilityDb))
	t.Cleanup(func() {
		_ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+durabilityDb)
	})
	setup, err := chstore.New(cfg)
	require.NoError(t, err)
	require.NoError(t, setup.SetupTable(ctx, ""))

	const slug = "durability-probe"
	const sql = "SELECT * FROM keelson('workingsets')"

	// Boot 1 — author saves an applet.
	{
		reg, _, caller := bootStore(t, cfg)
		_, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + slug))
		require.False(t, ok, "the scratch database must start empty")

		rep := saveDoc(t, caller, slug, testDoc("Durability probe", sql))
		require.True(t, rep.OK, "refused: %s", rep.Error)
	}

	// Boot 2 — everything above is gone: new chstore client, new bus, new
	// persist service, new registry. Only the ClickHouse rows survive.
	{
		reg, svc, _ := bootStore(t, cfg)
		m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + slug))
		require.True(t, ok, "a saved applet must mint at the next boot")
		assert.Equal(t, "Durability probe", m.Display)

		a, err := reg.Open(m.Id)
		require.NoError(t, err)
		assert.Equal(t, sql, a.(*appletApp).def.SQL,
			"the stored document must round-trip through ClickHouse byte-for-byte")

		// The index key is durable too — without it loadStored has nothing
		// to enumerate and the documents are unreachable even when present.
		idx, found, err := svc.storage.Get(storeIndexKey)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, slug, string(idx))
	}

	// Negative control — the same boot on a memory backend finds nothing.
	// This is the behaviour that shipped before the facts backend existed.
	{
		bus := inprocbus.NewInst(zerolog.Nop())
		ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
		require.NoError(t, err)
		t.Cleanup(ps.Close)
		reg := app.NewRegistry()
		svc, err := startStore(reg, bus, zerolog.Nop())
		require.NoError(t, err)
		t.Cleanup(svc.Stop)

		_, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + slug))
		assert.False(t, ok, "the memory backend must not see another process's applets")
	}
}

// TestStoreWritesFactRows_LiveCH asserts the shape of what durability rests
// on: the store's documents land as KindState rows attributed to the store's
// own durable app id. The id matters — it is a synthetic runtime service
// identity that is never registered as an app, which is why the persist
// service attributes it from the bus envelope rather than resolving the
// subject alias through the registry.
func TestStoreWritesFactRows_LiveCH(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}

	const db = durabilityDb + "_rows"
	cfg := chstore.Defaults()
	cfg.Database = db
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db))
	t.Cleanup(func() {
		_ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
	})
	setup, err := chstore.New(cfg)
	require.NoError(t, err)
	require.NoError(t, setup.SetupTable(ctx, ""))

	_, _, caller := bootStore(t, cfg)
	rep := saveDoc(t, caller, "row-probe", testDoc("Row probe", "SELECT 1"))
	require.True(t, rep.OK, "refused: %s", rep.Error)

	// One row per persist Set: the document and the index. Both carry the
	// store's durable id on the MembRuntimeApp mixed membership, which is
	// what the symbol value column holds.
	rows, err := queryScalar(ctx, cli,
		"SELECT count() FROM "+db+".facts WHERE has(`tv:symbol:value:val:s:m:0:12:0::data`, 'runtime.appletstore')")
	require.NoError(t, err)
	assert.Equal(t, "2", rows,
		"expected one state row for the document and one for the index")
}

// queryScalar runs sql and returns the single value it selects, trimmed.
func queryScalar(ctx context.Context, cli *chclient.Client, sql string) (out string, err error) {
	body, err := cli.Query(ctx, sql)
	if err != nil {
		return
	}
	defer body.Close()
	buf := make([]byte, 4096)
	n, _ := body.Read(buf)
	out = string(buf[:n])
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == '\r') {
		out = out[:len(out)-1]
	}
	return
}
