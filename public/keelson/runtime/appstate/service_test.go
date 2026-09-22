package appstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

const managerId app.AppIdT = "test.appstate.manager"

// serve starts a service over store and returns a manager client holding
// ClientCaps, plus the bus's audit sink.
func serve(t *testing.T, store StoreI) (bus *inprocbus.Inst, client *Client, sink *audit.InMemoryAuditSink) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	sink = audit.NewInMemoryAuditSink()
	bus.SetAuditSink(sink)
	svc, err := NewService(bus, zerolog.Nop(), store)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	client = NewClient(bus.NewClient(managerId, ClientCaps()))
	client.Timeout = 5 * time.Second
	return
}

// openBackend is the runtime's own state backend over clickhouse-local.
func openBackend(t *testing.T) *persist.StoreBackend {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	b, err := persist.OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(b.Close)
	return b
}

const playId app.AppIdT = "github.com/example/play"

func seed(t *testing.T, b *persist.StoreBackend) {
	t.Helper()
	ref := persist.StorageRef{Alias: "play", AppId: playId}
	require.NoError(t, b.Set(ref, "tabs", []byte("x")))
	require.NoError(t, b.Set(ref, "font", []byte("y")))
	past := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, b.WriteWorkingset(statestore.WorkingsetRow{AppId: playId, Name: "default", Kind: "playLaunch", Config: []byte("c"), Ts: past}))
	require.NoError(t, b.WriteColumnWidth(statestore.ColumnWidthRow{AppId: playId, Tier: "instance", Scope: "master/table", ColumnKey: "ab12", Points: 90, FontSize: 12, Ts: past}))
	require.NoError(t, b.WriteColumnWidth(statestore.ColumnWidthRow{AppId: "github.com/example/tally", Tier: "column", ColumnKey: "cd34", Points: 50, FontSize: 12, Ts: past}))
}

// TestDeleteClearsOneEntryAndTheOwnerSeesItGone: a delete named as
// keelson('app_state') names it — a column width whose scope contains "/" —
// is cleared, and the owning app's own read path agrees at once.
func TestDeleteClearsOneEntryAndTheOwnerSeesItGone(t *testing.T) {
	b := openBackend(t)
	seed(t, b)
	_, client, _ := serve(t, b)

	res, err := client.Delete(playId, persiststore.KindColumnWidth, "instance/master/table/ab12", "")
	require.NoError(t, err)
	require.True(t, res.Ok, res.Reason)
	assert.Equal(t, []Outcome{{Kind: persiststore.KindColumnWidth, Cleared: 1}}, res.Outcomes)

	rows, err := b.ListColumnWidths(playId)
	require.NoError(t, err)
	assert.Empty(t, rows, "the resolver's read must see the clear")

	res, err = client.Delete(playId, persiststore.KindPersist, "tabs", "")
	require.NoError(t, err)
	require.True(t, res.Ok, res.Reason)
	_, found, err := b.Get(persist.StorageRef{Alias: "play", AppId: playId}, "tabs")
	require.NoError(t, err)
	assert.False(t, found, "the app's own Get must read the key as absent")
}

// TestDeleteRefusesWhatIsNotThere: a delete that addresses nothing live, or
// another app's entry, is refused — no tombstone lands on a key that was
// never there or that belongs to someone else.
func TestDeleteRefusesWhatIsNotThere(t *testing.T) {
	b := openBackend(t)
	seed(t, b)
	_, client, _ := serve(t, b)

	for _, c := range []struct{ name, app, kind, key, entity string }{
		{"never written", string(playId), persiststore.KindPersist, "no-such-key", ""},
		{"another app's width", string(playId), persiststore.KindColumnWidth, "column//cd34", ""},
		{"a malformed width key", string(playId), persiststore.KindColumnWidth, "ab12", ""},
		{"an unknown kind without a store key", string(playId), persiststore.KindUnknown, "", ""},
		{"a label no kind carries", string(playId), "no-such-kind", "x", ""},
	} {
		res, err := client.Delete(app.AppIdT(c.app), c.kind, c.key, c.entity)
		require.NoError(t, err, c.name)
		assert.False(t, res.Ok, c.name)
		assert.NotEmpty(t, res.Reason, c.name)
		assert.Empty(t, res.Outcomes, c.name)
	}
	entries, err := b.LiveEntries("github.com/example/tally")
	require.NoError(t, err)
	assert.Len(t, entries, 1, "tally's width must survive a delete that named play")
}

// TestDeleteAnUnknownKindByItsStoreKey: a row of a kind this build does not
// know is cleared by the store key keelson('app_state') shows for it — the
// promise ADR-0185 §SD1 makes about `unknown` rows.
func TestDeleteAnUnknownKindByItsStoreKey(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	b, err := persist.OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(b.Close)
	// An Owner with no kind, written the way a newer build's kind would
	// look to this one.
	raw := persiststore.NewPersistStore(exec, nil, persiststore.PersistStoreConfig{})
	defer raw.Close()
	const id = "zz/future-kind/1"
	require.NoError(t, raw.Begin(id, time.Now().UTC().Add(-time.Minute)).AddOwner(persiststore.Owner{ID: id, AppId: string(playId)}).Commit())
	_, err = raw.Flush(context.Background())
	require.NoError(t, err)
	_, client, _ := serve(t, b)

	res, err := client.Delete(playId, persiststore.KindUnknown, "", id)
	require.NoError(t, err)
	require.True(t, res.Ok, res.Reason)
	_, found, err := b.EntryAt(id)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestForgetClearsEveryKindOfOneApp: forget reaches every kind the app
// keeps and nothing another app keeps.
func TestForgetClearsEveryKindOfOneApp(t *testing.T) {
	b := openBackend(t)
	seed(t, b)
	_, client, _ := serve(t, b)

	res, err := client.Forget(playId)
	require.NoError(t, err)
	require.True(t, res.Ok, res.Reason)
	assert.Equal(t, []Outcome{
		{Kind: persiststore.KindColumnWidth, Cleared: 1},
		{Kind: persiststore.KindPersist, Cleared: 2},
		{Kind: persiststore.KindWorkingset, Cleared: 1},
	}, res.Outcomes)

	left, err := b.LiveEntries(playId)
	require.NoError(t, err)
	assert.Empty(t, left)
	other, err := b.LiveEntries("github.com/example/tally")
	require.NoError(t, err)
	assert.Len(t, other, 1, "forget must not reach another app")
}

// failingStore fails the delete of one entity; everything else succeeds.
type failingStore struct {
	entries []persist.StateEntry
	failOn  string
	deleted []string
}

func (inst *failingStore) LiveEntries(appId app.AppIdT) ([]persist.StateEntry, error) {
	return inst.entries, nil
}
func (inst *failingStore) EntryAt(entityId string) (persist.StateEntry, bool, error) {
	return persist.StateEntry{}, false, nil
}
func (inst *failingStore) DeleteEntity(entityId string) error {
	if entityId == inst.failOn {
		return errors.New("synthetic delete failure")
	}
	inst.deleted = append(inst.deleted, entityId)
	return nil
}

// TestForgetNeverStopsEarly is ADR-0185 §SD4's rule, which nothing else
// checks: one failed delete neither stops the rest nor hides itself.
func TestForgetNeverStopsEarly(t *testing.T) {
	store := &failingStore{
		entries: []persist.StateEntry{
			{Kind: persiststore.KindPersist, EntityId: "state/a/1"},
			{Kind: persiststore.KindPersist, EntityId: "state/a/2"},
			{Kind: persiststore.KindWorkingset, EntityId: "ws/a/default"},
		},
		failOn: "state/a/1",
	}
	_, client, _ := serve(t, store)
	res, err := client.Forget("a")
	require.NoError(t, err)
	assert.False(t, res.Ok)
	assert.Contains(t, res.Reason, "synthetic delete failure")
	assert.Equal(t, []string{"state/a/2", "ws/a/default"}, store.deleted, "the entries after the failure were still attempted")
	assert.Equal(t, []Outcome{
		{Kind: persiststore.KindPersist, Cleared: 1, Failed: 1},
		{Kind: persiststore.KindWorkingset, Cleared: 1},
	}, res.Outcomes)
}

// TestRefusedWithoutTheCapabilityAndAudited: an app that did not declare
// ClientCaps cannot clear anything, and the denial is itself an audit row —
// the reason the verbs are requests, not publishes (ADR-0185 §SD3).
func TestRefusedWithoutTheCapabilityAndAudited(t *testing.T) {
	store := &failingStore{entries: []persist.StateEntry{{Kind: persiststore.KindPersist, EntityId: "state/a/1"}}}
	bus, _, sink := serve(t, store)
	const intruder app.AppIdT = "test.appstate.intruder"
	rogue := NewClient(bus.NewClient(intruder, nil))
	rogue.Timeout = time.Second

	_, err := rogue.Forget("a")
	require.Error(t, err)
	assert.Empty(t, store.deleted, "nothing may be cleared")

	var denied bool
	for _, rec := range sink.Records() {
		if rec.AppId == intruder && rec.Subject == SubjectForget && rec.Result == audit.AuditResultDenied {
			denied = true
		}
	}
	assert.True(t, denied, "the refusal must be audited")
}

// TestAllowedRequestsAreAudited: the grant is visible afterwards too — every
// clear lands an audit row naming the manager.
func TestAllowedRequestsAreAudited(t *testing.T) {
	store := &failingStore{}
	_, client, sink := serve(t, store)
	_, err := client.Forget("a")
	require.NoError(t, err)
	var ok bool
	for _, rec := range sink.Records() {
		if rec.AppId == managerId && rec.Subject == SubjectForget && rec.Result == audit.AuditResultOk {
			ok = true
		}
	}
	assert.True(t, ok)
}

// TestWithoutADurableStoreTheServiceSaysSo: with no store the service still
// answers, so a manager learns why instead of waiting out a timeout.
func TestWithoutADurableStoreTheServiceSaysSo(t *testing.T) {
	_, client, _ := serve(t, nil)
	res, err := client.Forget("a")
	require.NoError(t, err)
	assert.False(t, res.Ok)
	assert.Contains(t, res.Reason, "no durable state store")
}
