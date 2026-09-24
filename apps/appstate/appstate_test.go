package appstate

import (
	"bytes"
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	as "github.com/stergiotis/boxer/public/keelson/runtime/appstate"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

func TestManifest(t *testing.T) {
	require.NoError(t, manifest.Validate())
	require.Len(t, manifest.Caps, 2)
	assert.Equal(t, as.SubjectAll, manifest.Caps[0].Pattern)
	assert.Equal(t, app.CapDirectionPub, manifest.Caps[0].Direction)
	assert.False(t, manifest.Caps[0].Sticky, "a remembered grant to clear every app's state must not exist (ADR-0185 §SD3)")
	assert.Equal(t, keelsonquery.Subject(providers.TableAppState), manifest.Caps[1].Pattern)
	assert.True(t, manifest.Caps[1].Sticky, "reading the table is the low end of what an app asks for (ADR-0253 §SD1)")
	assert.LessOrEqual(t, utf8.RuneCountInString(manifest.Summary), 72)
}

func TestSummarizeAndEntriesOf(t *testing.T) {
	var rows entryCols
	rows.Add(entryRow{AppId: "x/play", Kind: "persist", Key: "a", PayloadBytes: 10})
	rows.Add(entryRow{AppId: "x/tally", Kind: "workingset", Key: "default", PayloadBytes: 5})
	rows.Add(entryRow{AppId: "x/play", Kind: "column_width", Key: "column//k"})
	assert.Equal(t, []appSummary{{appId: "x/play", entries: 2, bytes: 10}, {appId: "x/tally", entries: 1, bytes: 5}}, summarize(&rows))
	assert.Equal(t, []int{0, 2}, entriesOf(&rows, "x/play"))
	assert.Empty(t, entriesOf(&rows, "x/none"))
	assert.Equal(t, "play", shortApp("github.com/stergiotis/boxer/apps/play"))
	assert.Equal(t, "flat", shortApp("flat"))
}

func TestDescribe(t *testing.T) {
	assert.Equal(t, "forgot play: column_width ×1, persist ×2",
		describe(as.Result{Ok: true, Outcomes: []as.Outcome{{Kind: "column_width", Cleared: 1}, {Kind: "persist", Cleared: 2}}}, "forgot play"))
	assert.Equal(t, "forgot play (nothing was stored)", describe(as.Result{Ok: true}, "forgot play"))
	assert.Equal(t, "not cleared: no live entry", describe(as.Result{Reason: "no live entry"}, "x"))
}

// The read travels as a keelson.query request on the table's subject and
// decodes ArrowStream from the reply into columns.
func TestTableReader(t *testing.T) {
	var want entryCols
	want.Add(entryRow{Kind: "persist", AppId: "x/play", Key: "tabs", EntityId: "state/x%2Fplay/tabs", PayloadBytes: 5,
		WrittenAt: "2026-09-22T10:00:00Z", RunId: "r1", InstanceKey: 3})
	bus := inprocbus.NewInst(zerolog.Nop())
	var gotSQL, gotTable string
	svc := bus.NewClient(keelsonquery.ServiceAppId, keelsonquery.ServiceCaps("introspect"))
	unsub, err := svc.Subscribe(keelsonquery.SubjectAll, func(msg *app.Msg) {
		req, derr := buscodec.Decode[keelsonqueryrequest.KeelsonQueryRequest](msg.Payload)
		require.NoError(t, derr)
		gotSQL, gotTable = req.Sql, strings.TrimPrefix(msg.Subject, keelsonquery.SubjectPrefix)
		require.Equal(t, keelsonquery.FormatArrowStream, req.Format)
		var body bytes.Buffer
		require.NoError(t, chrows.EncodeStream(&body, &want))
		payload, eerr := buscodec.Encode(keelsonqueryreply.KeelsonQueryReply{Ok: true, Body: body.Bytes()})
		require.NoError(t, eerr)
		require.NoError(t, svc.Publish(msg.Reply, payload))
	})
	require.NoError(t, err)
	t.Cleanup(unsub)

	reader := newTableReader(bus.NewClient(AppId, manifest.Caps))
	require.NotNil(t, reader)
	rows, err := reader.entries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, providers.TableAppState, gotTable)
	assert.Contains(t, gotSQL, "keelson('app_state')")
	assert.Equal(t, want, rows)
	assert.Nil(t, newTableReader(nil), "no bus is nil, not a reader that fails")
}

// storeReader lists the live entries straight from the store, standing in
// for the endpoint the window reads in a host.
type storeReader struct{ exec recordstore.ExecutorI }

func (inst storeReader) entries(ctx context.Context) (rows entryCols, err error) {
	st := persiststore.NewPersistStore(inst.exec, nil, persiststore.PersistStoreConfig{})
	defer st.Close()
	for ent, serr := range st.ScanLiveOwner(ctx, recordstore.ScanOpts{}) {
		if serr != nil {
			return entryCols{}, serr
		}
		rows.Add(entryRow{Kind: persiststore.KindOf(ent), AppId: ent.Owner.Val.AppId, Key: persiststore.EntryKeyOf(ent), EntityId: ent.ID})
	}
	return
}

// serialExec runs one statement at a time. clickhouse-local refuses a
// second process on a data directory in use (exit 76), and the window's
// poller and its verbs reach the one directory concurrently; in a host the
// executor is a server that serves both at once, so this is the fixture's
// constraint, not the window's.
type serialExec struct {
	mu    sync.Mutex
	inner recordstore.ExecutorI
}

func (inst *serialExec) Exec(ctx context.Context, sql string) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.inner.Exec(ctx, sql)
}

func (inst *serialExec) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.inner.InsertArrow(ctx, table, records)
}

// QueryArrow holds the lock for the whole iteration: the records are the
// query's output, and the next statement must not start before it ends.
func (inst *serialExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		for rec, err := range inst.inner.QueryArrow(ctx, sql) {
			if !yield(rec, err) {
				return
			}
		}
	}
}

const (
	playId  = "github.com/example/play"
	tallyId = "github.com/example/tally"
)

// mounted is the window beside the host's service over the runtime's own
// state backend, on one bus — the arrangement hostboot makes.
func mounted(t *testing.T) (a *App, b *persist.StoreBackend) {
	t.Helper()
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	exec := &serialExec{inner: local}
	b, err = persist.OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(b.Close)
	ref := persist.StorageRef{Alias: "play", AppId: playId}
	require.NoError(t, b.Set(ref, "tabs", []byte("x")))
	past := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, b.WriteColumnWidth(statestore.ColumnWidthRow{AppId: playId, Tier: "column", ColumnKey: "k", Points: 90, FontSize: 12, Ts: past}))
	require.NoError(t, b.WriteWorkingset(statestore.WorkingsetRow{AppId: tallyId, Name: "default", Kind: "tallyLaunch", Config: []byte("c"), Ts: past}))

	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := as.NewService(bus, zerolog.Nop(), b)
	require.NoError(t, err)
	t.Cleanup(svc.Close)

	mc := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, bus.NewClient(AppId, manifest.Caps), nil)
	a = newApp()
	a.reader = storeReader{exec: exec}
	require.NoError(t, a.Mount(mc))
	t.Cleanup(func() { _ = a.Unmount(mc) })
	return
}

func (inst *App) eventuallyListed(t *testing.T, n int) (s snapshot) {
	t.Helper()
	inst.markDirty()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s = inst.snapshot()
		if s.inflight == 0 && !s.refreshed.IsZero() && s.rows.Len() == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the window should list %d entries; it lists %d (err=%q note=%q inflight=%d)", n, s.rows.Len(), s.lastError, s.lastNote, s.inflight)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestForgetWaitsForItsConfirmation is the confirmation step ADR-0185 M3
// names: arming clears nothing, cancelling disarms, and only the confirmed
// forget clears — every entry of that app, and nothing of another's.
func TestForgetWaitsForItsConfirmation(t *testing.T) {
	a, b := mounted(t)
	s := a.eventuallyListed(t, 3)
	apps := summarize(&s.rows)
	require.Len(t, apps, 2)
	play := apps[0]
	require.Equal(t, playId, play.appId)

	a.armForget(play)
	require.NotNil(t, a.armed)
	assert.Equal(t, 2, a.armed.entries, "the banner names what was there when armed")
	a.eventuallyListed(t, 3)
	a.cancelForget()
	assert.Nil(t, a.armed)
	a.confirmForget()
	a.eventuallyListed(t, 3)

	a.armForget(play)
	a.confirmForget()
	assert.Nil(t, a.armed, "a confirmed forget disarms")
	s = a.eventuallyListed(t, 1)
	assert.Equal(t, tallyId, s.rows.AppId[0], "forget reaches one app only")
	assert.Contains(t, s.lastNote, "forgot play")
	left, err := b.LiveEntries(playId)
	require.NoError(t, err)
	assert.Empty(t, left)
}

// TestDeleteOneEntry: a row's Delete clears that entry as the table names
// it, and the window re-reads.
func TestDeleteOneEntry(t *testing.T) {
	a, b := mounted(t)
	s := a.eventuallyListed(t, 3)
	var width entryRow
	for _, r := range s.rows.All() {
		if r.Kind == persiststore.KindColumnWidth {
			width = r
		}
	}
	require.NotEmpty(t, width.EntityId)
	a.deleteEntry(width)
	s = a.eventuallyListed(t, 2)
	assert.Contains(t, s.lastNote, "cleared column_width")
	rows, err := b.ListColumnWidths(playId)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// TestARefusalIsShownNotSwallowed: a delete the service refuses surfaces as
// the window's error, with the service's reason.
func TestARefusalIsShownNotSwallowed(t *testing.T) {
	a, _ := mounted(t)
	a.eventuallyListed(t, 3)
	a.deleteEntry(entryRow{AppId: playId, Kind: persiststore.KindPersist, Key: "never-written"})
	require.Eventually(t, func() bool {
		return strings.Contains(a.snapshot().lastError, "no live entry")
	}, 5*time.Second, 10*time.Millisecond)
}
