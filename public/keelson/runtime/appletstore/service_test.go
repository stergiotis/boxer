package appletstore_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// fixtureIdPrefix is the id namespace the fixture minter registers under.
const fixtureIdPrefix = "github.com/example/applets/"

// fixtureMinter stands in for an applet host: a document is an applet when
// it carries a sql fence, a buffer holding INSERT does not parse, and an
// opened window carries the document it was minted from. It counts what the
// store asked of it.
type fixtureMinter struct {
	mu       sync.Mutex
	live     map[string][]byte
	mints    int
	replaces int
}

func newFixtureMinter() (inst *fixtureMinter) {
	inst = &fixtureMinter{live: make(map[string][]byte, 4)}
	return
}

func (inst *fixtureMinter) Parse(slug string, doc []byte) (def appletstore.Definition, applet bool, err error) {
	if !bytes.Contains(doc, []byte("```sql")) {
		return
	}
	if bytes.Contains(doc, []byte("INSERT")) {
		err = eh.Errorf("fixture: the buffer does not parse")
		return
	}
	def = appletstore.Definition{Slug: slug, AppId: app.AppIdT(fixtureIdPrefix + slug), Class: analysis.QuerySecurityRead, Doc: doc}
	applet = true
	return
}

func (inst *fixtureMinter) Mint(reg *app.Registry, def appletstore.Definition) (err error) {
	slug := def.Slug
	m := app.Manifest{
		Id: def.AppId, Display: slug, Summary: "Fixture applet for store tests",
		Topics: []app.TopicT{app.TopicSql}, Kind: app.KindApplet, Surface: app.SurfaceWindowed,
	}
	if err = reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		inst.mu.Lock()
		a = &fixtureApp{m: m, doc: inst.live[slug]}
		inst.mu.Unlock()
		return
	}); err != nil {
		return
	}
	inst.mu.Lock()
	inst.live[slug] = def.Doc
	inst.mints++
	inst.mu.Unlock()
	return
}

func (inst *fixtureMinter) Replace(def appletstore.Definition) (err error) {
	inst.mu.Lock()
	inst.live[def.Slug] = def.Doc
	inst.replaces++
	inst.mu.Unlock()
	return
}

func (inst *fixtureMinter) counts() (mints int, replaces int) {
	inst.mu.Lock()
	mints, replaces = inst.mints, inst.replaces
	inst.mu.Unlock()
	return
}

type fixtureApp struct {
	m   app.Manifest
	doc []byte
}

func (inst *fixtureApp) Manifest() (m app.Manifest)                { return inst.m }
func (inst *fixtureApp) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *fixtureApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *fixtureApp) Unmount(ctx app.MountContextI) (err error) { return }

// storeHarness is one boot: a registry, a fixture minter and the service.
type storeHarness struct {
	reg    *app.Registry
	minter *fixtureMinter
	svc    *appletstore.Service
}

func bootHarness(t *testing.T, bus *inprocbus.Inst) (h storeHarness) {
	t.Helper()
	h.reg = app.NewRegistry()
	h.minter = newFixtureMinter()
	svc, err := appletstore.Start(h.reg, bus, h.minter, zerolog.New(zerolog.NewTestWriter(t)))
	require.NoError(t, err)
	t.Cleanup(svc.Stop)
	h.svc = svc
	return
}

// newMemoryBus is a bus with a memory-backed persist service on it.
func newMemoryBus(t *testing.T) (bus *inprocbus.Inst) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	ps, err := persist.NewService(bus, zerolog.Nop(), persist.NewMemoryBackend())
	require.NoError(t, err)
	t.Cleanup(ps.Close)
	return
}

func newCaller(bus *inprocbus.Inst) (caller *inprocbus.Client) {
	caller = bus.NewClient("test.author", []app.SubjectFilter{
		{Pattern: appletstore.SubjectSave, Direction: app.CapDirectionBoth, Reason: "test author"},
	})
	return
}

// storedKeys reads the store's persisted keys from outside the service,
// under its identity: the key layout is what lets a later boot find stored
// applets, so the tests pin it rather than reach into the service.
func storedKeys(t *testing.T, bus *inprocbus.Inst) (storage app.StorageI) {
	t.Helper()
	c := bus.NewClient(appletstore.AppId, []app.SubjectFilter{
		{Pattern: persist.SubjectPrefix + appletstore.AppId.SubjectAlias() + ".>", Direction: app.CapDirectionPub, Reason: "test reads the store's keys"},
	})
	storage, err := persist.NewClient(c, appletstore.AppId)
	require.NoError(t, err)
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
	return []byte("---\ntype: reference\nstatus: draft\ntitle: \"" + title + "\"\n" +
		"summary: \"Fixture applet for store tests\"\n" +
		"---\n\n# " + title + "\n\nProse.\n\n```sql\n" + sql + "\n```\n")
}

func openDoc(t *testing.T, reg *app.Registry, slug string) (doc []byte) {
	t.Helper()
	a, err := reg.Open(app.AppIdT(fixtureIdPrefix + slug))
	require.NoError(t, err)
	doc = a.(*fixtureApp).doc
	return
}

func TestStartRefusesMissingParts(t *testing.T) {
	bus := newMemoryBus(t)
	_, err := appletstore.Start(nil, bus, newFixtureMinter(), zerolog.Nop())
	assert.Error(t, err)
	_, err = appletstore.Start(app.NewRegistry(), nil, newFixtureMinter(), zerolog.Nop())
	assert.Error(t, err)
	_, err = appletstore.Start(app.NewRegistry(), bus, nil, zerolog.Nop())
	assert.Error(t, err)
}

func TestSaveMintsAndPersists(t *testing.T) {
	bus := newMemoryBus(t)
	h := bootHarness(t, bus)
	doc := testDoc("My tables", "SELECT * FROM keelson('apps')")

	rep := saveDoc(t, newCaller(bus), "my-tables", doc)
	require.True(t, rep.OK, "refused: %s", rep.Error)
	assert.Equal(t, "read", rep.Class)
	_, ok := h.reg.LookupManifest(fixtureIdPrefix + "my-tables")
	require.True(t, ok, "a saved applet is minted live")
	assert.Equal(t, doc, openDoc(t, h.reg, "my-tables"))

	// The persisted layout: one document per slug and the index, under the
	// store's alias. A later boot reads exactly these keys.
	keys := storedKeys(t, bus)
	got, found, err := keys.Get("applet_my-tables")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, doc, got)
	idx, found, err := keys.Get("index")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "my-tables", string(idx))

	require.True(t, saveDoc(t, newCaller(bus), "second", testDoc("Second", "SELECT 2")).OK)
	idx, _, err = keys.Get("index")
	require.NoError(t, err)
	assert.Equal(t, "my-tables\nsecond", string(idx))
}

func TestReloadMintsAtBoot(t *testing.T) {
	bus := newMemoryBus(t)
	h := bootHarness(t, bus)
	doc := testDoc("Reloaded", "SELECT 1")
	require.True(t, saveDoc(t, newCaller(bus), "reloaded", doc).OK)

	// A fresh boot over the same bus and persist backend (O4-D3).
	h.svc.Stop()
	h2 := bootHarness(t, bus)
	assert.Equal(t, doc, openDoc(t, h2.reg, "reloaded"), "a stored applet mints at boot from the index")
	mints, _ := h2.minter.counts()
	assert.Equal(t, 1, mints)
}

func TestBootSkipsWhatNoLongerMints(t *testing.T) {
	bus := newMemoryBus(t)
	keys := storedKeys(t, bus)
	require.NoError(t, keys.Set("applet_good", testDoc("Good", "SELECT 1")))
	require.NoError(t, keys.Set("applet_broken", testDoc("Broken", "INSERT INTO t VALUES (1)")))
	require.NoError(t, keys.Set("applet_taken", testDoc("Taken", "SELECT 1")))
	require.NoError(t, keys.Set("index", []byte("good\nbroken\nmissing\ntaken")))

	reg := app.NewRegistry()
	committed := app.Manifest{Id: fixtureIdPrefix + "taken", Display: "Committed", Summary: "Fixture applet for store tests",
		Topics: []app.TopicT{app.TopicSql}, Kind: app.KindApplet, Surface: app.SurfaceWindowed}
	require.NoError(t, reg.RegisterFactory(committed, func() (app.AppI, error) { return &fixtureApp{m: committed}, nil }))
	minter := newFixtureMinter()
	svc, err := appletstore.Start(reg, bus, minter, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(svc.Stop)

	_, ok := reg.LookupManifest(fixtureIdPrefix + "good")
	assert.True(t, ok)
	_, ok = reg.LookupManifest(fixtureIdPrefix + "broken")
	assert.False(t, ok, "a stored document that no longer parses is skipped")
	m, ok := reg.LookupManifest(fixtureIdPrefix + "taken")
	require.True(t, ok)
	assert.Equal(t, "Committed", m.Display, "curation outranks runtime state")
	mints, _ := minter.counts()
	assert.Equal(t, 1, mints)
}

func TestOverwriteReplacesTheLiveDefinition(t *testing.T) {
	bus := newMemoryBus(t)
	h := bootHarness(t, bus)
	caller := newCaller(bus)
	require.True(t, saveDoc(t, caller, "evolving", testDoc("Evolving", "SELECT 1")).OK)
	second := testDoc("Evolving", "SELECT 2")
	rep := saveDoc(t, caller, "evolving", second)
	require.True(t, rep.OK, "overwrite refused: %s", rep.Error)

	mints, replaces := h.minter.counts()
	assert.Equal(t, 1, mints, "an overwrite does not register again")
	assert.Equal(t, 1, replaces)
	assert.Equal(t, second, openDoc(t, h.reg, "evolving"), "opens after an overwrite see the new definition (O4-D4)")
}

func TestSaveRefusals(t *testing.T) {
	bus := newMemoryBus(t)
	h := bootHarness(t, bus)
	caller := newCaller(bus)

	// A committed applet holds the id (O4-D3).
	committed := app.Manifest{Id: fixtureIdPrefix + "taken", Display: "Taken", Summary: "Fixture applet for store tests",
		Topics: []app.TopicT{app.TopicSql}, Kind: app.KindApplet, Surface: app.SurfaceWindowed}
	require.NoError(t, h.reg.RegisterFactory(committed, func() (app.AppI, error) { return &fixtureApp{m: committed}, nil }))
	rep := saveDoc(t, caller, "taken", testDoc("Taken again", "SELECT 1"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "collides")

	// The minter's parse error is the reply.
	rep = saveDoc(t, caller, "bad-sql", testDoc("Bad", "INSERT INTO t VALUES (1)"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "does not parse")

	// A document without a fence is prose, not an applet.
	rep = saveDoc(t, caller, "prose", []byte("---\ntitle: \"P\"\n---\n\njust words\n"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "no sql fence")

	// The slug rule is checked before anything is parsed.
	rep = saveDoc(t, caller, "Bad_Slug", testDoc("X", "SELECT 1"))
	require.False(t, rep.OK)
	assert.Contains(t, rep.Error, "slug")

	mints, _ := h.minter.counts()
	assert.Zero(t, mints)
	_, found, err := storedKeys(t, bus).Get("index")
	require.NoError(t, err)
	assert.False(t, found, "a refused document is never half-saved")
}

func TestAdmitUnreviewed(t *testing.T) {
	reg := app.NewRegistry()
	def := appletstore.Definition{Slug: "fresh", AppId: fixtureIdPrefix + "fresh", Class: analysis.QuerySecurityRead}
	require.NoError(t, appletstore.AdmitUnreviewed(reg, def))

	for _, class := range []analysis.QuerySecurityClassE{analysis.QuerySecurityMutating, analysis.QuerySecurityReadEgress} {
		d := def
		d.Class = class
		assert.ErrorContains(t, appletstore.AdmitUnreviewed(reg, d), "must be read-class", "class=%s", class)
	}

	m := app.Manifest{Id: def.AppId, Display: "Fresh", Summary: "Fixture applet for store tests",
		Topics: []app.TopicT{app.TopicSql}, Kind: app.KindApplet, Surface: app.SurfaceWindowed}
	require.NoError(t, reg.RegisterFactory(m, func() (app.AppI, error) { return &fixtureApp{m: m}, nil }))
	assert.ErrorContains(t, appletstore.AdmitUnreviewed(reg, def), "committed applet")
}

func TestSlugPattern(t *testing.T) {
	for _, ok := range []string{"a", "my-tables", "x9-y"} {
		assert.True(t, appletstore.SlugPattern.MatchString(ok), ok)
	}
	for _, bad := range []string{"", "-lead", "Upper", "dot.ted", "under_score", "new\nline"} {
		assert.False(t, appletstore.SlugPattern.MatchString(bad), bad)
	}
}
