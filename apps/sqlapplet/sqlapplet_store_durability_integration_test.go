//go:build integration

package sqlapplet

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// durabilityDb is a scratch database, not the runtime's own boxer. The
// store writes user-authored applet documents; pointing this at the live
// `boxer.persiststate` would mix test documents into whatever the
// developer's running desktop has stored. The persist store's runtime table
// override (ADR-0100, 2026-08-15) is what lets the generated store — whose
// baked table is `boxer.persiststate` — be aimed here.
const durabilityDb = "sqlapplet_store_durability_test"

// bootStore stands up one complete "process worth" of wiring over the
// production persist backend, aimed at table: a fresh executor over the
// HTTP client, a fresh persist service on the store backend, a fresh
// registry, and the store service booting from whatever it finds in
// persist. Nothing is shared with a previous boot except the ClickHouse
// rows themselves — which is the whole point, since the in-process reload
// test (TestStoreReloadMintsAtBoot) keeps the persist service alive across
// the "reboot" and therefore passed throughout the period when nothing was
// durable at all.
//
// This is the wiring the carousel performs (storeexec over chclient, then
// OpenStoreBackend), so it also proves that a generated store provisions
// itself through the HTTP transport — EnsureTable issues its statements one
// per Exec, because the HTTP interface rejects a multi-statement body.
func bootStore(t *testing.T, cli *chclient.Client, table string) (reg *app.Registry, svc *StoreService, caller *inprocbus.Client) {
	t.Helper()
	exec, err := storeexec.New(cli, nil)
	require.NoError(t, err)
	backend, err := persist.OpenStoreBackendAt(context.Background(), exec, nil, table)
	require.NoError(t, err)
	t.Cleanup(backend.Close)

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
// for ADR-0132 O4 over the ADR-0105 D3a store backend: an applet saved by
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

	table := durabilityDb + "." + persiststore.TableName
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+durabilityDb))
	t.Cleanup(func() {
		_ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+durabilityDb)
	})

	const slug = "durability-probe"
	const sql = "SELECT * FROM keelson('workingsets')"

	// Boot 1 — author saves an applet. The first boot also provisions the
	// scratch database and table (OpenStoreBackendAt runs EnsureTable).
	{
		reg, _, caller := bootStore(t, cli, table)
		_, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + slug))
		require.False(t, ok, "the scratch database must start empty")

		rep := saveDoc(t, caller, slug, testDoc("Durability probe", sql))
		require.True(t, rep.OK, "refused: %s", rep.Error)
	}

	// Boot 2 — everything above is gone: new executor, new bus, new
	// persist service, new registry. Only the ClickHouse rows survive.
	{
		reg, svc, _ := bootStore(t, cli, table)
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

// TestStoreWritesStateRows_LiveCH asserts the shape of what durability
// rests on: the store's documents land as persist-state rows attributed to
// the store's own durable app id. The id matters — it is a synthetic
// runtime service identity that is never registered as an app, which is
// why the persist service attributes it from the bus envelope rather than
// resolving the subject alias through the registry. Read back through the
// generated store's own scan, so the assertion follows the schema rather
// than a hand-written physical column name.
func TestStoreWritesStateRows_LiveCH(t *testing.T) {
	ctx := context.Background()
	cli := chclient.New(chclient.Defaults(), nil)
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", chclient.Defaults().URL, err)
	}

	const db = durabilityDb + "_rows"
	table := db + "." + persiststore.TableName
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db))
	t.Cleanup(func() {
		_ = cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
	})

	_, _, caller := bootStore(t, cli, table)
	rep := saveDoc(t, caller, "row-probe", testDoc("Row probe", "SELECT 1"))
	require.True(t, rep.OK, "refused: %s", rep.Error)

	// One row per persist Set: the document and the index. Both carry the
	// store's durable id in the state component's AppId.
	exec, err := storeexec.New(cli, nil)
	require.NoError(t, err)
	reader := persiststore.NewPersistStore(exec, nil, persiststore.PersistStoreConfig{Table: table})
	defer reader.Close()
	rows := 0
	for ent, serr := range reader.ScanState(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		if ent.State.Has && ent.State.Val.AppId == "runtime.appletstore" {
			rows++
		}
	}
	assert.Equal(t, 2, rows,
		"expected one state row for the document and one for the index")
}
