package watchbill

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/watchbill/launchcfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// fixture is the window mounted beside a worker over the memory store on
// one in-proc bus, with a consumer client that enqueues what the window
// then manages.
type fixture struct {
	a        *App
	store    *wb.MemStore
	consumer *wb.Client
	release  chan struct{}
}

func newFixture(t *testing.T) (f *fixture) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	f = &fixture{store: wb.NewMemStore(), release: make(chan struct{})}
	reg := wb.NewRegistry()
	require.NoError(t, reg.Register(wb.HandlerFunc{KindName: "mgr.kind", Run: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
		if job.Subject == "fail" {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.release:
			return nil
		}
	}}))
	w, err := wb.New(wb.Config{
		Store: f.store, Handlers: reg, RunId: "run-mgr", Bus: bus.NewClient(wb.WorkerAppId, wb.WorkerCaps()),
		Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, w.Start(context.Background()))
	t.Cleanup(w.Stop)
	f.consumer = wb.NewClient(bus.NewClient("apps/consumer", wb.ClientCaps()))
	// The tables answer empty: the fixture is about the verbs, and a
	// window that reads nothing must still refresh clean.
	stubTables(t, bus, func(table string, _ string) any {
		if table == wb.TableEvent {
			return eventCols{}
		}
		return workerCols{}
	})

	id := app.AppIdT(manifest.Id)
	mc := app.NewStaticMountContext(id, zerolog.Nop(), nil, bus.NewClient(id, manifest.Caps), nil)
	f.a = newApp()
	require.NoError(t, f.a.Mount(mc))
	t.Cleanup(func() { _ = f.a.Unmount(mc) })
	return
}

func (f *fixture) eventuallyState(t *testing.T, id string, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		j, _, _ := f.store.Get(context.Background(), id)
		return j.State == state
	}, 3*time.Second, 10*time.Millisecond, "job %s should reach %s", id, state)
}

func (f *fixture) eventuallyListed(t *testing.T, n int) (s snapshot) {
	t.Helper()
	require.Eventually(t, func() bool {
		s = f.a.snapshot()
		return s.lastError == "" && !s.refreshed.IsZero() && len(s.jobs) == n
	}, 3*time.Second, 10*time.Millisecond, "the window should list %d job(s)", n)
	return
}

func TestManifest(t *testing.T) {
	patterns := make([]string, 0, len(manifest.Caps))
	for _, cp := range manifest.Caps {
		patterns = append(patterns, cp.Pattern)
	}
	assert.Contains(t, patterns, wb.SubjectJobAll)
	assert.Contains(t, patterns, task.PatternAll)
	assert.Contains(t, patterns, task.PatternCancelAll)
	assert.Equal(t, launchcfg.Kind, manifest.LaunchKind)
	assert.Equal(t, launchcfg.AppId, string(manifest.Id))
	require.NoError(t, manifest.Validate())
}

// The list follows the state filter, the kind filter is the window's own,
// and retry and cancel land on the row with the window in the note.
func TestListFilterRetryAndCancel(t *testing.T) {
	f := newFixture(t)
	running, err := f.consumer.Enqueue(wb.Request{Kind: "mgr.kind", Subject: "hold"})
	require.NoError(t, err)
	failed, err := f.consumer.Enqueue(wb.Request{Kind: "mgr.kind", Subject: "fail"})
	require.NoError(t, err)
	f.eventuallyState(t, running.ID, watchbillstore.StateRunning)
	f.eventuallyState(t, failed.ID, watchbillstore.StateDiscarded)
	require.NoError(t, f.store.Enqueue(context.Background(), watchbillstore.Job{ID: "other", Kind: "other.kind", State: watchbillstore.StateQueued, RunAfter: time.Now().Add(time.Hour)}))
	f.a.markDirty()
	s := f.eventuallyListed(t, 3)
	assert.True(t, s.reads, "the window reads the tables through the bus it was mounted with")

	f.a.filters.kind = "MGR"
	assert.Len(t, f.a.visibleJobs(s.jobs), 2)
	f.a.filters.kind = ""

	f.a.toggleState(watchbillstore.StateRunning)
	s = f.eventuallyListed(t, 1)
	assert.Equal(t, running.ID, s.jobs[0].ID)
	f.a.toggleState(watchbillstore.StateRunning)
	f.eventuallyListed(t, 3)

	f.a.select_(failed.ID)
	f.a.retry(failed.ID)
	require.Eventually(t, func() bool { return f.a.snapshot().lastNote == "retried "+short(failed.ID) }, 2*time.Second, 10*time.Millisecond)
	f.eventuallyState(t, failed.ID, watchbillstore.StateDiscarded)
	evs := f.store.Events(failed.ID)
	require.GreaterOrEqual(t, len(evs), 3)
	assert.Equal(t, "asked by "+string(manifest.Id)+": "+noteFrom, evs[2].Event.Note)

	f.a.cancel(running.ID)
	f.eventuallyState(t, running.ID, watchbillstore.StateCancelled)
	f.a.cancel(running.ID)
	require.Eventually(t, func() bool { return f.a.snapshot().lastNote == "cancel did not apply to "+short(running.ID) }, 2*time.Second, 10*time.Millisecond)
}

func TestLaunchConfigSelectsAndFilters(t *testing.T) {
	a := newApp()
	a.applyLaunch(launchcfg.WatchbillLaunch{JobId: "j-9", Kind: "tender", State: watchbillstore.StateDiscarded})
	assert.Equal(t, "j-9", a.selectedID)
	assert.Equal(t, "tender", a.filters.kind)
	assert.Equal(t, []string{watchbillstore.StateDiscarded}, a.filters.stateList())
}

// Clear drops the pills and the kind text together and asks for a fresh
// list; with nothing set it asks for nothing.
func TestClearFilters(t *testing.T) {
	a := newApp()
	a.applyLaunch(launchcfg.WatchbillLaunch{Kind: "tender", State: watchbillstore.StateDiscarded})
	a.kindDraft = "tender"
	a.clearFilters()
	assert.Empty(t, a.filters.stateList())
	assert.Empty(t, a.filters.kind)
	assert.Empty(t, a.kindDraft)
	select {
	case <-a.dirty:
	default:
		t.Fatal("a clear that changed something asks for a refresh")
	}
	a.clearFilters()
	select {
	case <-a.dirty:
		t.Fatal("a clear with nothing to clear asks for nothing")
	default:
	}
}

// stubTables stands in for the host's keelson.query service: it answers
// every table read with the ArrowStream body of the columns answer returns for the
// subject's table and the statement, so the reader is exercised without a
// clickhouse-local.
func stubTables(t *testing.T, bus *inprocbus.Inst, answer func(table string, sql string) any) (lastSQL func() string) {
	t.Helper()
	var last string
	svc := bus.NewClient(keelsonquery.ServiceAppId, keelsonquery.ServiceCaps("introspect"))
	unsub, err := svc.Subscribe(keelsonquery.SubjectAll, func(msg *app.Msg) {
		req, derr := buscodec.Decode[keelsonqueryrequest.KeelsonQueryRequest](msg.Payload)
		require.NoError(t, derr)
		last = req.Sql
		table := strings.TrimPrefix(msg.Subject, keelsonquery.SubjectPrefix)
		require.Equal(t, table, req.Table)
		require.Equal(t, keelsonquery.FormatArrowStream, req.Format)
		var body bytes.Buffer
		require.NoError(t, chrows.EncodeStream(&body, answer(table, req.Sql)))
		payload, eerr := buscodec.Encode(keelsonqueryreply.KeelsonQueryReply{Ok: true, Body: body.Bytes()})
		require.NoError(t, eerr)
		require.NoError(t, svc.Publish(msg.Reply, payload))
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	return func() string { return last }
}

// The two introspection reads travel as keelson.query requests on the
// table's own subject, decode ArrowStream from the reply, and the trail
// query carries the escaped job id.
func TestTableReads(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	lastSQL := stubTables(t, bus, func(table string, _ string) any {
		switch table {
		case wb.TableEvent:
			return eventCols{JobId: []string{"j1", "j1"}, At: []string{"2026-09-15T10:00:00Z", "2026-09-15T10:00:03Z"},
				State: []string{"running", "discarded"}, Attempt: []int64{1, 1}, WorkerRun: []string{"r1", "r1"},
				Note: []string{"", "attempts exhausted"}, Error: []string{"", "boom\nat x"}}
		default:
			return workerCols{RunId: []string{"r1"}, Host: []string{"box"}, Kinds: [][]string{{"a", "b"}}, Queues: [][]string{{}},
				MaxWorkers: []int64{2}, StartedAt: []string{"2026-09-15T09:00:00Z"}, Alive: []bool{true}, Local: []bool{true},
				Running: [][]string{{"j1"}}, LastTick: []string{"2026-09-15T10:00:00Z"}, PollMs: []int64{5000},
				Serving: []bool{true}, Sweeping: []bool{false}}
		}
	})
	reader := newTableReader(bus.NewClient("apps/reader", keelsonquery.ClientCaps(wb.TableEvent, wb.TableWorker)))
	require.NotNil(t, reader)
	evs, err := reader.events(context.Background(), "j'1")
	require.NoError(t, err)
	assert.Contains(t, lastSQL(), `'j\'1'`)
	assert.NotContains(t, lastSQL(), "FORMAT", "the format is the request's, not the statement's")
	require.Equal(t, 2, evs.Len())
	assert.Equal(t, "discarded", evs.State[1])
	assert.Equal(t, "boom", firstLine(evs.Error[1]))
	ws, err := reader.workers(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, ws.Len())
	w := ws.Row(0)
	assert.Equal(t, []string{"a", "b"}, w.Kinds)
	assert.Empty(t, w.Queues)
	assert.True(t, w.Serving)
	assert.False(t, w.Sweeping)

	// Without the grant the bus refuses the publish, and the window shows
	// that rather than rows.
	ungranted := newTableReader(bus.NewClient("apps/ungranted", nil))
	_, err = ungranted.workers(context.Background())
	require.Error(t, err)
	assert.Nil(t, newTableReader(nil), "no bus is nil, not a reader that fails")
}

func TestSortJobs(t *testing.T) {
	jobs := []watchbillstore.Job{{ID: "b", Attempt: 2, Kind: "x"}, {ID: "a", Attempt: 1, Kind: "y"}, {ID: "c", Attempt: 2, Kind: "x"}}
	var s tableSort
	ids := func(js []watchbillstore.Job) (out []string) {
		for _, j := range js {
			out = append(out, j.ID)
		}
		return
	}
	assert.Equal(t, []string{"b", "a", "c"}, ids(sortJobs(jobs, s)), "no sort keeps the listed order")
	s.clicked(0)
	assert.Equal(t, []string{"a", "b", "c"}, ids(sortJobs(jobs, s)))
	assert.Equal(t, " ▲", s.glyph(0))
	s.clicked(0)
	assert.Equal(t, []string{"c", "b", "a"}, ids(sortJobs(jobs, s)))
	assert.Equal(t, " ▼", s.glyph(0))
	s.clicked(0)
	assert.Equal(t, sortNone, s.dir)
	s.clicked(5)
	assert.Equal(t, []string{"a", "b", "c"}, ids(sortJobs(jobs, s)), "numeric ascending, stable among equals")
	s.clicked(1)
	assert.Equal(t, []string{"b", "c", "a"}, ids(sortJobs(jobs, s)), "another column starts ascending")
}

// A dragged width survives the app: observed after the settle window, it
// is flushed to the facts store and a fresh resolver over the same store
// resolves it in place of the default (ADR-0151).
func TestColumnWidthsPersist(t *testing.T) {
	store := statestore.NewMemory()
	appId := app.AppIdT(manifest.Id)
	res, err := colwidth.New(store, colwidth.Opts{AppId: appId, MinPoints: colMinWidth, MaxPoints: colMaxWidth, Debounce: time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, res.Load())
	cols := jobColumnKeys()
	defaults := []float64{rowNumColWidth}
	for _, col := range jobColumns {
		defaults = append(defaults, float64(col.width))
	}
	got := res.Resolve(jobsTableTag, cols, 0, defaults)
	assert.Equal(t, defaults, got, "nothing stored resolves to the defaults")

	// The first report is the fit and is not a drag; a later, different
	// report is one.
	now := time.Now()
	res.Observe(jobsTableTag, cols, defaults, 0, true, now)
	for i := 0; i < 4; i++ {
		res.Resolve(jobsTableTag, cols, 0, defaults)
		res.Observe(jobsTableTag, cols, defaults, 0, false, now.Add(time.Duration(i+1)*time.Second))
	}
	dragged := append([]float64(nil), defaults...)
	dragged[3] = 260
	res.Observe(jobsTableTag, cols, dragged, 0, false, now.Add(10*time.Second))
	_, err = res.Flush(now.Add(20 * time.Second))
	require.NoError(t, err)

	again, err := colwidth.New(store, colwidth.Opts{AppId: appId, MinPoints: colMinWidth, MaxPoints: colMaxWidth})
	require.NoError(t, err)
	require.NoError(t, again.Load())
	got = again.Resolve(jobsTableTag, cols, 0, defaults)
	assert.Equal(t, 260.0, got[3], "the dragged subject column is stored")
	assert.Equal(t, defaults[1], got[1], "an untouched column keeps its default")
}

// The split follows a drag, ignores a window resize, re-asserts itself for
// two frames after one, and clamps to keep both panes reachable.
func TestSplitState(t *testing.T) {
	s := newSplitState()
	exact, w := s.frame(1200, true, 0, false)
	assert.False(t, exact)
	assert.Equal(t, float32(defaultSplit), w)
	// The panel reports; nothing moved.
	_, w = s.frame(1200, true, 630, true)
	assert.Equal(t, float32(defaultSplit), w)
	// A drag: the window stood still, the panel moved.
	exact, w = s.frame(1200, true, 530, true)
	assert.False(t, exact)
	assert.Equal(t, float32(540), w)
	assert.True(t, s.takeDirty())
	assert.False(t, s.takeDirty())
	// A window resize shrinks the panel: not a drag, and the kept width
	// is re-asserted for two frames.
	exact, w = s.frame(700, true, 300, true)
	assert.True(t, exact)
	assert.Equal(t, float32(380), w, "drawn clamped so the detail keeps its minimum")
	exact, _ = s.frame(700, true, 370, true)
	assert.True(t, exact)
	exact, _ = s.frame(700, true, 370, true)
	assert.False(t, exact)
	assert.False(t, s.takeDirty(), "a resize is not a drag")
	// Growing back re-asserts the width the user left, not the clamped one.
	exact, w = s.frame(1200, true, 370, true)
	assert.True(t, exact)
	assert.Equal(t, float32(540), w)
	assert.Equal(t, float32(540), s.Width)

	assert.Equal(t, "540.0", string(encodeSplit(540)))
	got, ok := decodeSplit([]byte(" 612.5\n"))
	assert.True(t, ok)
	assert.Equal(t, float32(612.5), got)
	_, ok = decodeSplit([]byte("nope"))
	assert.False(t, ok)
	assert.Contains(t, manifest.PersistedKeys, splitKey)
}
