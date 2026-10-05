package fsbroker

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// WatchEventKindE classifies a single filesystem change reported by a
// backend. Inotify masks flatten to one kind per event; rename pairs are
// emitted as RenameFrom + RenameTo with a shared Cookie. The poller cannot
// distinguish renames so it emits Delete+Create with Cookie=0.
type WatchEventKindE uint8

const (
	WatchEventUnspecified WatchEventKindE = 0
	WatchEventCreate      WatchEventKindE = 1
	WatchEventDelete      WatchEventKindE = 2
	WatchEventModify      WatchEventKindE = 3
	WatchEventAttrib      WatchEventKindE = 4
	WatchEventRenameFrom  WatchEventKindE = 5
	WatchEventRenameTo    WatchEventKindE = 6
	// WatchEventOverflow signals that events were lost — either inotify's
	// IN_Q_OVERFLOW fired or the broker's per-watch channel filled. The
	// app must rescan the watched directory to recover state.
	WatchEventOverflow WatchEventKindE = 7
	// WatchEventClosed signals that the watched root has gone away
	// (deleted, moved, or remounted) or that the backend stopped of its
	// own accord. The event stream closes immediately after this event.
	WatchEventClosed WatchEventKindE = 8
)

var AllWatchEventKinds = []WatchEventKindE{
	WatchEventCreate,
	WatchEventDelete,
	WatchEventModify,
	WatchEventAttrib,
	WatchEventRenameFrom,
	WatchEventRenameTo,
	WatchEventOverflow,
	WatchEventClosed,
}

func (inst WatchEventKindE) String() (s string) {
	switch inst {
	case WatchEventCreate:
		s = "create"
	case WatchEventDelete:
		s = "delete"
	case WatchEventModify:
		s = "modify"
	case WatchEventAttrib:
		s = "attrib"
	case WatchEventRenameFrom:
		s = "renameFrom"
	case WatchEventRenameTo:
		s = "renameTo"
	case WatchEventOverflow:
		s = "overflow"
	case WatchEventClosed:
		s = "closed"
	default:
		s = "unspecified"
	}
	return
}

// ParseWatchEventKind is the inverse of WatchEventKindE.String.
// Unknown inputs map to WatchEventUnspecified — the wire is
// forward-compatible with future event kinds a receiver did not
// anticipate.
func ParseWatchEventKind(s string) (k WatchEventKindE) {
	switch s {
	case "create":
		k = WatchEventCreate
	case "delete":
		k = WatchEventDelete
	case "modify":
		k = WatchEventModify
	case "attrib":
		k = WatchEventAttrib
	case "renameFrom":
		k = WatchEventRenameFrom
	case "renameTo":
		k = WatchEventRenameTo
	case "overflow":
		k = WatchEventOverflow
	case "closed":
		k = WatchEventClosed
	default:
		k = WatchEventUnspecified
	}
	return
}

// WatchEvent is the payload published on fs.handle.{uuid}.event,
// wire-encoded via the canonical bus codec. Name is the basename of
// the affected entry within the watched directory in single-level
// mode, or a forward-slash relative path (e.g. "sub/file.txt") in
// recursive mode. Empty when the event addresses the watched root
// itself. Cookie pairs inotify RenameFrom/RenameTo events; zero on
// poller-backed watches.
type WatchEvent struct {
	Kind   WatchEventKindE `json:"kind"`
	Name   string          `json:"name,omitempty"`
	Cookie uint32          `json:"cookie,omitempty"`
	Ts     int64           `json:"ts"`
}

// WatchRequest is the optional payload of an fs.handle.{uuid}.watch
// request. All zero values select defaults — empty payload is valid.
type WatchRequest struct {
	// PollFallback forces the poller backend regardless of the underlying
	// filesystem. Defaults to false; statfs auto-routes to the poller on
	// proc, sysfs, NFS, FUSE, CIFS.
	PollFallback bool `json:"pollFallback,omitempty"`
	// PollIntervalMs is the poller's tick interval. Defaults to 500ms.
	// Values below 100ms clamp to 100ms.
	PollIntervalMs int32 `json:"pollIntervalMs,omitempty"`
	// Recursive enables watching the whole subtree rooted at the handle's
	// path. inotify-backed watches walk-and-AddWatch every existing
	// subdirectory at start and dynamically AddWatch new directories on
	// IN_CREATE+IN_ISDIR. Poller-backed watches WalkDir the subtree on
	// every tick. Event Name carries the relative path under the root.
	Recursive bool `json:"recursive,omitempty"`
}

// WatchReply is the reply payload to fs.handle.{uuid}.watch. Started is
// false either on broker-side error (with Reason populated) or when the
// handle already has an active watch.
type WatchReply struct {
	Started      bool   `json:"started"`
	EventSubject string `json:"eventSubject,omitempty"`
	Backend      string `json:"backend,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// watcherBackendI is the broker-internal contract for one source of
// filesystem events on a single path. Backends decouple inotify and the
// poller behind a uniform channel surface.
type watcherBackendI interface {
	// Start spawns the backend's internal goroutine. After Start returns
	// nil the channel from Events() yields events until Stop is called
	// or the watched root vanishes.
	Start() (err error)
	// Stop signals termination. Idempotent. The events channel closes
	// shortly after.
	Stop()
	// Events is the read-only event stream. Closed when the backend's
	// goroutine exits.
	Events() (ch <-chan WatchEvent)
}

// activeWatch couples a backend with the handle uuid it serves. Kept under
// Service.mu inside the watches map.
type activeWatch struct {
	uuid    string
	backend watcherBackendI
}

// pickBackend returns the appropriate backend for path. PollFallback in
// the request forces the poller; otherwise the platform picks — on Linux
// inotify unless the filesystem is inotify-blind, elsewhere the poller
// (pickNativeBackend, per platform file).
func pickBackend(path string, req WatchRequest) (b watcherBackendI, name string, err error) {
	interval := time.Duration(req.PollIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 500 * time.Millisecond
	} else if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	if req.PollFallback {
		b, err = newPollerWatcher(path, interval, req.Recursive)
		name = "poller"
		return
	}
	return pickNativeBackend(path, interval, req.Recursive)
}

// fileWatchBackend adapts a directory backend to a single-FILE watch: it
// watches filepath.Dir(path) through the normal pickBackend routing and
// forwards only the events addressing filepath.Base(path), with Name
// scrubbed to "" (the documented "event addresses the watched root"
// semantics — the watched object IS the file). The scrub-and-filter is
// mandatory, not an optimisation: the app holds no path, and a read grant on
// one file must not stream sibling names past the Powerbox.
//
// Watching the parent rather than the file's own inode is load-bearing too:
// the common editor save is a rename-replace (write a temp file, move it
// over the target), which orphans an inode-bound watch while the path lives
// on. From the parent the same save is a RenameTo carrying the file's name.
type fileWatchBackend struct {
	inner watcherBackendI
	base  string
	out   chan WatchEvent
}

// newFileWatchBackend builds the wrapper. Recursive is forced off — a file
// has no subtree, and honouring it would only widen what the filter must
// then suppress.
func newFileWatchBackend(path string, req WatchRequest) (b watcherBackendI, name string, err error) {
	req.Recursive = false
	inner, name, err := pickBackend(filepath.Dir(path), req)
	if err != nil {
		return
	}
	b = &fileWatchBackend{
		inner: inner,
		base:  filepath.Base(path),
		out:   make(chan WatchEvent, 256),
	}
	return
}

var _ watcherBackendI = (*fileWatchBackend)(nil)

func (inst *fileWatchBackend) Start() (err error) {
	err = inst.inner.Start()
	if err != nil {
		return
	}
	go inst.forward()
	return
}

func (inst *fileWatchBackend) Stop() {
	inst.inner.Stop()
}

func (inst *fileWatchBackend) Events() (ch <-chan WatchEvent) {
	ch = inst.out
	return
}

// forward filters the parent-directory stream down to the one file. Overflow
// and Closed always pass — losing events, or losing the parent directory, is
// losing the file's stream too. The send blocks rather than dropping: the
// inner channel is the bounded stage, and its emit already substitutes an
// Overflow when the consumer stalls.
func (inst *fileWatchBackend) forward() {
	defer close(inst.out)
	for ev := range inst.inner.Events() {
		switch ev.Kind {
		case WatchEventOverflow, WatchEventClosed:
			ev.Name = ""
		default:
			if ev.Name != inst.base {
				continue
			}
			ev.Name = ""
		}
		inst.out <- ev
	}
}

// pollerWatcher polls path on a fixed interval and diffs the result
// against a snapshot to synthesise Create/Delete/Modify. Auto-selected
// for inotify-blind filesystems (proc/sysfs/NFS/FUSE/CIFS); forced via
// WatchRequest.PollFallback. Rename appears as Delete+Create without a
// paired Cookie. In recursive mode the snapshot keys are forward-
// slash relative paths from the watch root, populated via WalkDir.
type pollerWatcher struct {
	path      string
	interval  time.Duration
	isDir     bool
	recursive bool
	events    chan WatchEvent
	stop      chan struct{}
	stopped   atomic.Bool

	snapshot map[string]fileMeta
}

// fileMeta captures the minimal per-entry state the poller diffs on.
// For single-file watches the snapshot is keyed by "" with the file's own
// mtime/size.
type fileMeta struct {
	mtimeNs int64
	size    int64
	isDir   bool
}

func newPollerWatcher(path string, interval time.Duration, recursive bool) (w *pollerWatcher, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("fsbroker: poller stat: %w", err)
		return
	}
	w = &pollerWatcher{
		path:      path,
		interval:  interval,
		isDir:     fi.IsDir(),
		recursive: recursive && fi.IsDir(),
		events:    make(chan WatchEvent, 256),
		stop:      make(chan struct{}),
	}
	w.snapshot, err = w.scan()
	if err != nil {
		err = eh.Errorf("fsbroker: poller initial scan: %w", err)
		return
	}
	return
}

var _ watcherBackendI = (*pollerWatcher)(nil)

func (inst *pollerWatcher) Start() (err error) {
	go inst.loop()
	return
}

func (inst *pollerWatcher) Stop() {
	if !inst.stopped.CompareAndSwap(false, true) {
		return
	}
	close(inst.stop)
}

func (inst *pollerWatcher) Events() (ch <-chan WatchEvent) {
	ch = inst.events
	return
}

func (inst *pollerWatcher) loop() {
	defer close(inst.events)
	t := time.NewTicker(inst.interval)
	defer t.Stop()
	for {
		select {
		case <-inst.stop:
			return
		case <-t.C:
			next, scanErr := inst.scan()
			if scanErr != nil {
				inst.emit(WatchEvent{Kind: WatchEventClosed, Ts: time.Now().UnixNano()})
				return
			}
			inst.diff(inst.snapshot, next)
			inst.snapshot = next
		}
	}
}

func (inst *pollerWatcher) scan() (snap map[string]fileMeta, err error) {
	snap = make(map[string]fileMeta)
	if !inst.isDir {
		fi, statErr := os.Stat(inst.path)
		if statErr != nil {
			err = statErr
			return
		}
		snap[""] = fileMeta{
			mtimeNs: fi.ModTime().UnixNano(),
			size:    fi.Size(),
			isDir:   fi.IsDir(),
		}
		return
	}
	if inst.recursive {
		err = inst.scanRecursive(snap)
		return
	}
	entries, readErr := os.ReadDir(inst.path)
	if readErr != nil {
		err = readErr
		return
	}
	for _, e := range entries {
		fi, infoErr := e.Info()
		if infoErr != nil {
			// Race with concurrent delete — skip; next tick picks
			// up the steady state.
			continue
		}
		snap[e.Name()] = fileMeta{
			mtimeNs: fi.ModTime().UnixNano(),
			size:    fi.Size(),
			isDir:   fi.IsDir(),
		}
	}
	return
}

// scanRecursive populates snap with every entry under inst.path,
// keyed by forward-slash relative path. Symlinks aren't followed
// (filepath.WalkDir uses Lstat). Unreadable entries are skipped so a
// permission-denied subtree doesn't fail the whole tick.
func (inst *pollerWatcher) scanRecursive(snap map[string]fileMeta) (err error) {
	walkErr := filepath.WalkDir(inst.path, func(p string, d fs.DirEntry, perEntryErr error) (cbErr error) {
		if perEntryErr != nil {
			if d != nil && d.IsDir() {
				cbErr = fs.SkipDir
			}
			return
		}
		if p == inst.path {
			return
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return
		}
		rel, relErr := filepath.Rel(inst.path, p)
		if relErr != nil {
			return
		}
		snap[filepath.ToSlash(rel)] = fileMeta{
			mtimeNs: fi.ModTime().UnixNano(),
			size:    fi.Size(),
			isDir:   fi.IsDir(),
		}
		return
	})
	// Root vanishing surfaces as walkErr — callers will see the same
	// "scan failed" path that triggers WatchEventClosed.
	if walkErr != nil {
		err = walkErr
	}
	return
}

func (inst *pollerWatcher) diff(prev, next map[string]fileMeta) {
	now := time.Now().UnixNano()
	for name, p := range prev {
		n, ok := next[name]
		if !ok {
			inst.emit(WatchEvent{Kind: WatchEventDelete, Name: name, Ts: now})
			continue
		}
		if n.mtimeNs != p.mtimeNs || n.size != p.size {
			inst.emit(WatchEvent{Kind: WatchEventModify, Name: name, Ts: now})
		}
	}
	for name := range next {
		if _, ok := prev[name]; !ok {
			inst.emit(WatchEvent{Kind: WatchEventCreate, Name: name, Ts: now})
		}
	}
}

func (inst *pollerWatcher) emit(ev WatchEvent) {
	select {
	case inst.events <- ev:
	default:
		select {
		case inst.events <- WatchEvent{Kind: WatchEventOverflow, Ts: time.Now().UnixNano()}:
		default:
		}
	}
}
