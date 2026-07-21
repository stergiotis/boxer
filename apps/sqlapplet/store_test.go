package sqlapplet

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// newStoreHarness stands up a bus, a memory-backed persist service, a
// caller client holding the play-shaped save cap, and the store service
// against a fresh registry.
func newStoreHarness(t *testing.T) (reg *app.Registry, bus *inprocbus.Inst, svc *StoreService, caller *inprocbus.Client) {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus = inprocbus.NewInst(logger)
	ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)
	t.Cleanup(func() { ps.Close() })
	reg = app.NewRegistry()
	svc, err = startStore(reg, bus, logger)
	require.NoError(t, err)
	t.Cleanup(svc.Stop)
	caller = bus.NewClient("test.author", []app.SubjectFilter{
		{Pattern: appletstore.SubjectSave, Direction: app.CapDirectionBoth, Reason: "test author"},
	})
	return
}

func saveDoc(t *testing.T, caller *inprocbus.Client, slug string, doc []byte) (rep appletstore.SaveReply) {
	t.Helper()
	payload, err := appletstore.EncodeSaveRequest(appletstore.SaveRequest{Slug: slug, Doc: doc})
	require.NoError(t, err)
	replyBytes, err := caller.Request(appletstore.SubjectSave, payload)
	require.NoError(t, err)
	rep, err = appletstore.DecodeSaveReply(replyBytes)
	require.NoError(t, err)
	return
}

func testDoc(title string, sql string) []byte {
	return []byte("---\ntype: reference\nstatus: draft\ntitle: \"" + title + "\"\n---\n\n# " + title + "\n\nProse.\n\n```sql\n" + sql + "\n```\n")
}

func TestStoreSaveMintsAndPersists(t *testing.T) {
	reg, _, svc, caller := newStoreHarness(t)

	rep := saveDoc(t, caller, "my-tables", testDoc("My tables", "SELECT * FROM keelson('apps')"))
	require.True(t, rep.OK, "refused: %s", rep.Error)
	assert.Equal(t, "read", rep.Class)

	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "my-tables"))
	require.True(t, ok, "saved applet must be minted live")
	assert.Equal(t, "My tables", m.Display)
	assert.Equal(t, "Applets", m.Category)

	a, err := reg.Open(m.Id)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM keelson('apps')", a.(*appletApp).def.SQL)

	// Persisted: document + index reachable through the service's storage.
	doc, found, err := svc.storage.Get(storeKeyPrefix + "my-tables")
	require.NoError(t, err)
	require.True(t, found)
	assert.Contains(t, string(doc), "SELECT * FROM keelson('apps')")
	idx, found, err := svc.storage.Get(storeIndexKey)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "my-tables", string(idx))
}

func TestStoreReloadMintsAtBoot(t *testing.T) {
	_, bus, svc, caller := newStoreHarness(t)
	rep := saveDoc(t, caller, "reloaded", testDoc("Reloaded", "SELECT 1"))
	require.True(t, rep.OK, "refused: %s", rep.Error)

	// A fresh boot: stop the first service, start another registry over the
	// same bus + persist backend (O4-D3).
	svc.Stop()
	reg2 := app.NewRegistry()
	svc2, err := startStore(reg2, bus, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(svc2.Stop)
	_, ok := reg2.LookupManifest(app.AppIdT(appletIdPrefix + "reloaded"))
	assert.True(t, ok, "stored applet must mint at boot from the index")
}

func TestStoreOverwriteSwapsLiveDefinition(t *testing.T) {
	reg, _, _, caller := newStoreHarness(t)
	require.True(t, saveDoc(t, caller, "evolving", testDoc("Evolving", "SELECT 1")).OK)
	rep := saveDoc(t, caller, "evolving", testDoc("Evolving", "SELECT 2"))
	require.True(t, rep.OK, "overwrite refused: %s", rep.Error)

	a, err := reg.Open(app.AppIdT(appletIdPrefix + "evolving"))
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", a.(*appletApp).def.SQL, "opens after an overwrite see the new definition (O4-D4)")
}

func TestStoreRefusals(t *testing.T) {
	reg, _, _, caller := newStoreHarness(t)

	// The committed corpus wins slug collisions (O4-D3).
	committed := &AppletDef{Slug: "taken", Title: "Taken", SQL: "SELECT 1", Class: analysis.QuerySecurityRead}
	require.NoError(t, reg.RegisterFactory(manifestFor(committed, nil), func() (app.AppI, error) {
		return &appletApp{def: committed}, nil
	}))
	rep := saveDoc(t, caller, "taken", testDoc("Taken again", "SELECT 1"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "collides")

	// The store is the same gate as the corpus test: an unparseable buffer
	// is refused with the classifier's reasoning (ADR-0132 §SD5/§SD6).
	rep = saveDoc(t, caller, "bad-sql", testDoc("Bad", "INSERT INTO t VALUES (1)"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "does not parse")

	// A document without a fence is prose, not an applet.
	rep = saveDoc(t, caller, "prose", []byte("---\ntitle: \"P\"\n---\n\njust words\n"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "no sql fence")

	// Slug discipline matches the committed books.
	rep = saveDoc(t, caller, "Bad_Slug", testDoc("X", "SELECT 1"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "slug")
}

// TestComposeRoundTrip pins the two halves of O4 against each other: the
// document the creator composes is accepted and parsed back to the same
// buffer by the store's gate (O4-D5 / §SD1's pasteable-complete invariant).
func TestComposeRoundTrip(t *testing.T) {
	doc, err := appletstore.ComposeAppletDoc("Röund \"trip\"", "🧪", "introspection", "SET param_x = 1;\nSELECT {x:UInt64}")
	require.NoError(t, err)
	def, err := ParseDocSource("store", "round-trip.md", doc)
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, `Röund "trip"`, def.Title)
	assert.Equal(t, "🧪", def.Icon)
	assert.Equal(t, "SET param_x = 1;\nSELECT {x:UInt64}", def.SQL)
	assert.Equal(t, analysis.QuerySecurityRead, def.Class)
	assert.Equal(t, EndpointIntrospection, def.Endpoint, "the authoring endpoint travels with the document")
}
