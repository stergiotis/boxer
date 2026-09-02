package fsbroker_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// newWatchSetup spins up an Inst + fsbroker.Service + an app client with
// caps for fs.dialog.watch. The broker augments the client's caps to
// include fs.handle.{uuid}.> (CapDirectionBoth) on Resolve so the app can
// publish requests AND subscribe to the .event stream.
func newWatchSetup(t *testing.T) (inst *inprocbus.Inst, svc *fsbroker.Service, appBus *inprocbus.Client, cleanup func()) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(2 * time.Second)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	appBus = inst.NewClient("test.watchapp", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogWatch, Direction: app.CapDirectionPub, Reason: "test"},
	})
	cleanup = func() {
		svc.Close()
	}
	return
}

// eventCollector subscribes to a fs.handle.{uuid}.event subject and feeds
// matching kinds onto a buffered channel. Used by tests to assert events
// land without races. The collector preserves arrival order; callers may
// time out via the channel's read deadline.
type eventCollector struct {
	mu     sync.Mutex
	events []fsbroker.WatchEvent
	ch     chan fsbroker.WatchEvent
	stop   func()
}

func newEventCollector(t *testing.T, bus *inprocbus.Client, subject string) (c *eventCollector) {
	t.Helper()
	c = &eventCollector{
		ch: make(chan fsbroker.WatchEvent, 64),
	}
	unsub, err := bus.Subscribe(subject, func(msg *app.Msg) {
		ev, evErr := fsbroker.UnmarshalWatchEvent(msg.Payload)
		if evErr != nil {
			return
		}
		c.mu.Lock()
		c.events = append(c.events, ev)
		c.mu.Unlock()
		select {
		case c.ch <- ev:
		default:
		}
	})
	require.NoError(t, err)
	c.stop = unsub
	return
}

func (c *eventCollector) waitFor(t *testing.T, kind fsbroker.WatchEventKindE, name string, within time.Duration) (ev fsbroker.WatchEvent) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		select {
		case got := <-c.ch:
			if got.Kind == kind && (name == "" || got.Name == name) {
				ev = got
				return
			}
			// Not the one we wanted; keep draining.
		case <-time.After(50 * time.Millisecond):
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("no %s event for %q within %s; saw %d events: %+v",
		kind, name, within, len(c.events), c.events)
	return
}

func pendingWatchOnce(t *testing.T, svc *fsbroker.Service) (req fsbroker.PendingRequest) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all := svc.Pending()
		if len(all) == 1 && all[0].Op == "watch" {
			req = all[0]
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no watch dialog pending after 2s")
	return
}

// resolveWatchPath drives a fs.dialog.watch request, resolves it against
// dir, and returns the handle subject prefix the app received.
func resolveWatchPath(t *testing.T, svc *fsbroker.Service, appBus *inprocbus.Client, dir string) (prefix string) {
	t.Helper()
	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(fsbroker.SubjectDialogWatch, nil)
		resultCh <- res{reply, err}
	}()
	req := pendingWatchOnce(t, svc)
	_, err := svc.Resolve(req.Id, dir)
	require.NoError(t, err)
	r := <-resultCh
	require.NoError(t, r.err)
	dr, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	require.True(t, dr.Granted)
	prefix = dr.HandleSubjectPrefix
	return
}

func TestService_Watch_InotifyCreateEvent(t *testing.T) {
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	prefix := resolveWatchPath(t, svc, appBus, tmp)

	// Subscribe before starting the watch so we don't miss the early
	// inotify events that fire on file creation.
	col := newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	watchReply, err := appBus.Request(prefix+".watch", nil)
	require.NoError(t, err)
	wr, err := fsbroker.UnmarshalWatchReply(watchReply)
	require.NoError(t, err)
	require.True(t, wr.Started, "watch should start: %s", wr.Reason)
	assert.Equal(t, "inotify", wr.Backend)
	assert.Equal(t, prefix+"."+fsbroker.HandleEventOp, wr.EventSubject)

	// Give inotify a moment to actually register the watch fd before
	// touching the directory; AddWatch returned synchronously but the
	// kernel's queue plumbing is asynchronous on some kernels.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "hello.txt"), []byte("hi"), 0o644))

	ev := col.waitFor(t, fsbroker.WatchEventCreate, "hello.txt", 2*time.Second)
	assert.Equal(t, "hello.txt", ev.Name)
}

func TestService_Watch_PollFallbackCreateEvent(t *testing.T) {
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	prefix := resolveWatchPath(t, svc, appBus, tmp)

	col := newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	reqPayload, err := fsbroker.MarshalWatchRequest(fsbroker.WatchRequest{
		PollFallback:   true,
		PollIntervalMs: 100,
	})
	require.NoError(t, err)
	watchReply, err := appBus.Request(prefix+".watch", reqPayload)
	require.NoError(t, err)
	wr, err := fsbroker.UnmarshalWatchReply(watchReply)
	require.NoError(t, err)
	require.True(t, wr.Started)
	assert.Equal(t, "poller", wr.Backend)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p.txt"), []byte("p"), 0o644))

	// Poller ticks at 100ms; allow a few ticks before failing.
	ev := col.waitFor(t, fsbroker.WatchEventCreate, "p.txt", 2*time.Second)
	assert.Equal(t, "p.txt", ev.Name)
}

// resolveDialog drives one fs.dialog.{op} request over the given subject,
// resolves it against path, and returns the granted reply.
func resolveDialog(t *testing.T, svc *fsbroker.Service, appBus *inprocbus.Client, subject string, op string, path string) (dr fsbroker.DialogReply) {
	t.Helper()
	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(subject, nil)
		resultCh <- res{reply, err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if all := svc.Pending(); len(all) == 1 && all[0].Op == op {
			_, err := svc.Resolve(all[0].Id, path)
			require.NoError(t, err)
			r := <-resultCh
			require.NoError(t, r.err)
			dr, err = fsbroker.UnmarshalDialogReply(r.reply)
			require.NoError(t, err)
			require.True(t, dr.Granted)
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no %s dialog pending after 2s", op)
	return
}

// TestService_Watch_OnReadHandleFollowsTheFile is the read-handle watch seam:
// a file granted via fs.dialog.read may be watched, the stream carries only
// THAT file's events (Name scrubbed to "" — the watched object is the file),
// sibling churn leaks nothing, and a rename-replace save — the common editor
// write — arrives as RenameTo rather than orphaning the watch. Runs on the
// app's own dialog cap only, so it also proves the narrow event-Sub cap
// Resolve grants on read handles is sufficient to subscribe.
func TestService_Watch_OnReadHandleFollowsTheFile(t *testing.T) {
	inst := inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(time.Second)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()

	appBus := inst.NewClient("test.readwatch", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})

	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	dr := resolveDialog(t, svc, appBus, fsbroker.SubjectDialogRead, "read", path)

	// Subscribe BEFORE starting the watch (the capdemo ordering) — this is
	// the subscribe the narrow Sub cap has to cover.
	col := newEventCollector(t, appBus, dr.HandleSubjectPrefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	watchReply, err := appBus.Request(dr.HandleSubjectPrefix+".watch", nil)
	require.NoError(t, err)
	wr, err := fsbroker.UnmarshalWatchReply(watchReply)
	require.NoError(t, err)
	require.True(t, wr.Started, "read-handle watch should start: %s", wr.Reason)
	assert.Equal(t, dr.HandleSubjectPrefix+"."+fsbroker.HandleEventOp, wr.EventSubject)

	time.Sleep(50 * time.Millisecond)

	// Sibling churn first: none of it may reach the stream — the grant is
	// one file, not its directory.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "sibling.txt"), []byte("s"), 0o644))

	// An in-place write to the watched file: Modify, addressing the watched
	// object itself (empty Name).
	require.NoError(t, os.WriteFile(path, []byte("xy"), 0o644))
	ev := col.waitFor(t, fsbroker.WatchEventModify, "", 2*time.Second)
	assert.Empty(t, ev.Name, "the event addresses the watched file itself")

	// A rename-replace save: write a temp sibling, move it over the target.
	// The parent-directory watch sees it as RenameTo carrying the file's
	// name; an inode-bound watch would have been orphaned here.
	stage := filepath.Join(tmp, "f.txt.tmp")
	require.NoError(t, os.WriteFile(stage, []byte("xyz"), 0o644))
	require.NoError(t, os.Rename(stage, path))
	col.waitFor(t, fsbroker.WatchEventRenameTo, "", 2*time.Second)

	// Nothing named a sibling ever surfaced.
	col.mu.Lock()
	defer col.mu.Unlock()
	for _, got := range col.events {
		assert.Empty(t, got.Name, "a file watch must not leak sibling names: %+v", got)
	}
}

// TestService_Watch_RejectedOnWriteHandle keeps the refusal coverage the
// read-handle seam narrowed: write and bundle handles still cannot watch.
func TestService_Watch_RejectedOnWriteHandle(t *testing.T) {
	inst := inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(time.Second)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()

	appBus := inst.NewClient("test.writewatch", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogWrite, Direction: app.CapDirectionPub},
	})

	path := filepath.Join(t.TempDir(), "out.txt")
	dr := resolveDialog(t, svc, appBus, fsbroker.SubjectDialogWrite, "write", path)

	reply, err := appBus.Request(dr.HandleSubjectPrefix+".watch", nil)
	require.NoError(t, err)
	rejected, err := fsbroker.UnmarshalDialogReply(reply)
	require.NoError(t, err)
	assert.False(t, rejected.Granted)
	assert.Contains(t, rejected.Reason, "not opened for watch")
}

// TestService_ReadAndWriteHandlesOnSamePathAreDistinct pins the uuid-mint
// disambiguation: the op is in the hash, so a read grant and a write grant
// on the SAME file coexist — before it, the second Resolve overwrote the
// first handle, flipping its mode underneath the earlier grant.
func TestService_ReadAndWriteHandlesOnSamePathAreDistinct(t *testing.T) {
	inst := inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(time.Second)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()

	appBus := inst.NewClient("test.samepath", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
		{Pattern: fsbroker.SubjectDialogWrite, Direction: app.CapDirectionPub},
	})

	path := filepath.Join(t.TempDir(), "doc.md")
	require.NoError(t, os.WriteFile(path, []byte("body"), 0o644))

	readGrant := resolveDialog(t, svc, appBus, fsbroker.SubjectDialogRead, "read", path)
	writeGrant := resolveDialog(t, svc, appBus, fsbroker.SubjectDialogWrite, "write", path)
	require.NotEqual(t, readGrant.HandleSubjectPrefix, writeGrant.HandleSubjectPrefix,
		"two dialog kinds on one path must not share a handle")

	// Both handles live: the read still answers after the write grant, and
	// closing the write leaves the read untouched.
	body, err := appBus.Request(readGrant.HandleSubjectPrefix+".read", nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("body"), body)

	_, err = appBus.Request(writeGrant.HandleSubjectPrefix+".close", nil)
	require.NoError(t, err)

	body, err = appBus.Request(readGrant.HandleSubjectPrefix+".read", nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("body"), body, "closing the write handle must not destroy the read handle")
}

func TestService_Watch_UnwatchStopsStream(t *testing.T) {
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	prefix := resolveWatchPath(t, svc, appBus, tmp)

	col := newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	_, err := appBus.Request(prefix+".watch", nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("a"), 0o644))
	col.waitFor(t, fsbroker.WatchEventCreate, "a.txt", 2*time.Second)

	// Stop the watch.
	_, err = appBus.Request(prefix+".unwatch", nil)
	require.NoError(t, err)

	// Drain anything currently queued, then assert no further events.
	drainDeadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(drainDeadline) {
		select {
		case <-col.ch:
		case <-time.After(20 * time.Millisecond):
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("b"), 0o644))
	select {
	case ev := <-col.ch:
		// Closed events from teardown are legitimate; anything else is a leak.
		if ev.Kind != fsbroker.WatchEventClosed {
			t.Fatalf("unexpected event after unwatch: %+v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		// No event — pass.
	}
}

func TestService_Watch_HandleCloseTearsDownWatch(t *testing.T) {
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	prefix := resolveWatchPath(t, svc, appBus, tmp)

	col := newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	_, err := appBus.Request(prefix+".watch", nil)
	require.NoError(t, err)

	// Close the handle — the watch should be torn down implicitly.
	_, err = appBus.Request(prefix+".close", nil)
	require.NoError(t, err)

	// A subsequent watch op is denied at the bus layer: closing the handle
	// revokes the fs.handle.{uuid}.> cap, so the app can no longer publish to
	// the handle subject at all.
	_, err = appBus.Request(prefix+".watch", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation)
}

// startRecursiveWatch is the common prefix every recursive test uses:
// build a watch on dir with Recursive=true, subscribe to the event
// stream, and return the collector ready for assertions.
func startRecursiveWatch(t *testing.T, svc *fsbroker.Service, appBus *inprocbus.Client, dir string, pollFallback bool) (col *eventCollector, prefix string) {
	t.Helper()
	prefix = resolveWatchPath(t, svc, appBus, dir)
	col = newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	req := fsbroker.WatchRequest{Recursive: true, PollFallback: pollFallback}
	if pollFallback {
		req.PollIntervalMs = 100
	}
	reqPayload, err := fsbroker.MarshalWatchRequest(req)
	require.NoError(t, err)
	watchReply, err := appBus.Request(prefix+".watch", reqPayload)
	require.NoError(t, err)
	wr, err := fsbroker.UnmarshalWatchReply(watchReply)
	require.NoError(t, err)
	require.True(t, wr.Started, "watch should start: %s", wr.Reason)
	return
}

func TestService_Watch_Recursive_Inotify_PreExistingSubdir(t *testing.T) {
	// A subdirectory created BEFORE the watch starts must be picked up
	// by the initial walk-and-AddWatch; events from inside it then fire
	// with a forward-slash relative path.
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "sub")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	col, _ := startRecursiveWatch(t, svc, appBus, tmp, false)
	defer col.stop()

	// Inotify add+register has a small async window on some kernels.
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "inner.txt"), []byte("x"), 0o644))

	ev := col.waitFor(t, fsbroker.WatchEventCreate, "sub/inner.txt", 2*time.Second)
	assert.Equal(t, "sub/inner.txt", ev.Name,
		"recursive event must carry the forward-slash relpath from the watch root")
}

func TestService_Watch_Recursive_Inotify_DynamicSubdir(t *testing.T) {
	// A subdirectory created AFTER the watch starts must be AddWatched
	// dynamically on the IN_CREATE+IN_ISDIR event so subsequent events
	// inside it fire — exercising the addSubdirWatch path in parseBuf.
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	col, _ := startRecursiveWatch(t, svc, appBus, tmp, false)
	defer col.stop()
	time.Sleep(60 * time.Millisecond)

	// First, the new subdir itself fires a Create with IS_DIR.
	dynamicSub := filepath.Join(tmp, "later")
	require.NoError(t, os.Mkdir(dynamicSub, 0o755))
	col.waitFor(t, fsbroker.WatchEventCreate, "later", 2*time.Second)

	// Give the broker a moment to register the dynamic AddWatch before
	// dropping a file inside the new subdir.
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(dynamicSub, "child.txt"), []byte("c"), 0o644))

	ev := col.waitFor(t, fsbroker.WatchEventCreate, "later/child.txt", 2*time.Second)
	assert.Equal(t, "later/child.txt", ev.Name)
}

func TestService_Watch_Recursive_Poller(t *testing.T) {
	// Poller-backed recursive watch — WalkDir-based snapshot diffs
	// produce the same relpath-keyed Create event as inotify.
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "deep", "nested")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	col, _ := startRecursiveWatch(t, svc, appBus, tmp, true)
	defer col.stop()

	require.NoError(t, os.WriteFile(filepath.Join(subdir, "leaf.txt"), []byte("l"), 0o644))

	// Poll tick is 100ms; allow a few ticks plus a margin.
	ev := col.waitFor(t, fsbroker.WatchEventCreate, "deep/nested/leaf.txt", 3*time.Second)
	assert.Equal(t, "deep/nested/leaf.txt", ev.Name)
}

func TestService_Watch_NonRecursive_SubdirEventNotReported(t *testing.T) {
	// Drift guard against the default. Without Recursive=true, an event
	// inside a subdirectory must NOT surface — only events at the
	// watched root's first level do. This pins the contract so a future
	// "always recursive" refactor would loudly break this test.
	inst, svc, appBus, cleanup := newWatchSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "sub")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	prefix := resolveWatchPath(t, svc, appBus, tmp)
	col := newEventCollector(t, appBus, prefix+"."+fsbroker.HandleEventOp)
	defer col.stop()

	// Default (non-recursive) watch.
	_, err := appBus.Request(prefix+".watch", nil)
	require.NoError(t, err)
	time.Sleep(60 * time.Millisecond)

	require.NoError(t, os.WriteFile(filepath.Join(subdir, "ignored.txt"), []byte("x"), 0o644))

	// No event for sub/ignored.txt should arrive — drain the channel
	// for a short window to be sure.
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case ev := <-col.ch:
			if ev.Name == "sub/ignored.txt" || ev.Name == "ignored.txt" {
				t.Fatalf("non-recursive watch must not surface subdir-internal events; got %+v", ev)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}
