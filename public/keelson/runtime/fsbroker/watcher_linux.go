//go:build linux

package fsbroker

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// pickNativeBackend is the Linux choice: statfs picks inotify by default
// and falls back to the poller on inotify-blind filesystems.
func pickNativeBackend(path string, interval time.Duration, recursive bool) (b watcherBackendI, name string, err error) {
	var st unix.Statfs_t
	err = unix.Statfs(path, &st)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("fsbroker: statfs: %w", err)
		return
	}
	if _, blind := inotifyBlindFsMagic[int64(st.Type)]; blind {
		b, err = newPollerWatcher(path, interval, recursive)
		name = "poller"
		return
	}
	b, err = newInotifyWatcher(path, recursive)
	name = "inotify"
	return
}

// inotifyBlindFsMagic lists statfs Type values for filesystems where
// inotify is unreliable or unsupported on Linux. pickBackend auto-routes
// to the poller for paths residing on these. Hexadecimal source values
// come from linux/include/uapi/linux/magic.h.
var inotifyBlindFsMagic = map[int64]struct{}{
	0x9fa0:     {}, // PROC_SUPER_MAGIC
	0x62656572: {}, // SYSFS_MAGIC
	0x6969:     {}, // NFS_SUPER_MAGIC
	0x65735546: {}, // FUSE_SUPER_MAGIC
	0xff534d42: {}, // CIFS_MAGIC_NUMBER
}

// inotifyWatcher uses inotify(7) on Linux to surface events on path.
// In single-level mode (recursive=false) a single InotifyAddWatch
// covers the path; events on its immediate children are reported. For
// regular files the file itself is watched (Modify, Attrib, DeleteSelf,
// MoveSelf).
//
// In recursive mode (recursive=true) every existing subdirectory is
// AddWatched at construction and new directories are AddWatched
// dynamically on IN_CREATE+IN_ISDIR. The wdToRelDir map translates
// inotify event wds back into watch-root-relative directory paths so
// event Name fields carry the full relpath ("sub/file.txt") rather
// than just the basename.
//
// The wdToRelDir map is single-writer (the parser goroutine + the
// constructor before Start); no lock is needed — the Go memory model's
// happens-before for go statements covers the handoff.
type inotifyWatcher struct {
	path      string
	recursive bool
	fd        int
	mask      uint32
	rootWd    int32
	events    chan WatchEvent
	stopped   atomic.Bool

	wdToRelDir map[int32]string
}

// inotifyWatchMask is the set of mask bits AddWatch installs on every
// directory (root and dynamically-added subdirs in recursive mode). Held
// on the struct so dynamic adds reuse it without re-deriving.
const inotifyWatchMask uint32 = unix.IN_CREATE |
	unix.IN_DELETE |
	unix.IN_MODIFY |
	unix.IN_ATTRIB |
	unix.IN_MOVED_FROM |
	unix.IN_MOVED_TO |
	unix.IN_DELETE_SELF |
	unix.IN_MOVE_SELF

func newInotifyWatcher(path string, recursive bool) (w *inotifyWatcher, err error) {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		err = eh.Errorf("fsbroker: inotify init: %w", err)
		return
	}
	rootWd, err := unix.InotifyAddWatch(fd, path, inotifyWatchMask)
	if err != nil {
		_ = unix.Close(fd)
		err = eb.Build().Str("path", path).Errorf("fsbroker: inotify add: %w", err)
		return
	}
	w = &inotifyWatcher{
		path:       path,
		recursive:  recursive,
		fd:         fd,
		mask:       inotifyWatchMask,
		rootWd:     int32(rootWd),
		events:     make(chan WatchEvent, 256),
		wdToRelDir: map[int32]string{int32(rootWd): ""},
	}
	if recursive {
		w.walkAndAddSubdirs()
	}
	return
}

// walkAndAddSubdirs traverses the watch root and AddWatches every
// subdirectory it finds. Best-effort — directories that can't be
// AddWatched (e.g. permission denied, fs.inotify.max_user_watches
// exhausted) are skipped silently. Symlinks aren't followed.
func (inst *inotifyWatcher) walkAndAddSubdirs() {
	_ = filepath.WalkDir(inst.path, func(p string, d fs.DirEntry, walkErr error) (err error) {
		if walkErr != nil {
			// Permission denied / vanished mid-walk — skip subtree if
			// it's a directory.
			if d != nil && d.IsDir() {
				err = fs.SkipDir
			}
			return
		}
		if !d.IsDir() || p == inst.path {
			return
		}
		// Don't follow symlinks into other subtrees.
		fi, lerr := os.Lstat(p)
		if lerr != nil || fi.Mode()&os.ModeSymlink != 0 {
			err = fs.SkipDir
			return
		}
		inst.addSubdirWatch(p)
		return
	})
}

// addSubdirWatch records a new wd→relDir mapping. Caller has verified
// p is a directory under the watch root. Errors (e.g. max watches
// exhausted) are swallowed — the subdir simply won't fire events.
// IN_ONLYDIR|IN_DONT_FOLLOW close the gap between that check and the add: a
// directory swapped for a symlink in between is refused rather than followed
// out of the tree.
func (inst *inotifyWatcher) addSubdirWatch(p string) {
	wd, addErr := unix.InotifyAddWatch(inst.fd, p, inst.mask|unix.IN_ONLYDIR|unix.IN_DONT_FOLLOW)
	if addErr != nil {
		return
	}
	rel, relErr := filepath.Rel(inst.path, p)
	if relErr != nil {
		return
	}
	inst.wdToRelDir[int32(wd)] = filepath.ToSlash(rel)
}

// dropSubtreeWatches removes the watch on relDir and on every directory
// below it, from the kernel and from the map. A directory moved out of the
// tree keeps its inode, so its watches — and its descendants' — would go on
// reporting files the grant no longer covers, under the old relative names.
func (inst *inotifyWatcher) dropSubtreeWatches(relDir string) {
	prefix := relDir + "/"
	for wd, rd := range inst.wdToRelDir {
		if wd == inst.rootWd || (rd != relDir && !strings.HasPrefix(rd, prefix)) {
			continue
		}
		_, _ = unix.InotifyRmWatch(inst.fd, uint32(wd))
		delete(inst.wdToRelDir, wd)
	}
}

var _ watcherBackendI = (*inotifyWatcher)(nil)

func (inst *inotifyWatcher) Start() (err error) {
	go inst.loop()
	return
}

func (inst *inotifyWatcher) Stop() {
	// Only signal termination. The loop goroutine owns inst.fd and closes
	// it on exit (see loop). Closing here would race the concurrent
	// unix.Read in loop(), and — because the fd is non-blocking and polled —
	// the fd number could be reused by an unrelated open() in the window
	// between our Close and loop()'s next Read, making loop() read a
	// different file's descriptor. loop() observes inst.stopped within one
	// poll interval (~20 ms) and tears down cleanly.
	inst.stopped.CompareAndSwap(false, true)
}

func (inst *inotifyWatcher) Events() (ch <-chan WatchEvent) {
	ch = inst.events
	return
}

// loop pumps the inotify FD until Stop is called or the watched root
// vanishes. inotify event header layout is fixed: wd(int32) mask(uint32)
// cookie(uint32) len(uint32) followed by len bytes of NUL-padded name.
func (inst *inotifyWatcher) loop() {
	defer close(inst.events)
	// Single-owner close: the loop goroutine is the only place inst.fd is
	// closed, so no other goroutine can close it out from under an in-flight
	// Read (see Stop). Ordered after the events-close defer so the fd is
	// released first.
	defer func() { _ = unix.Close(inst.fd) }()
	buf := make([]byte, 4096)
	for {
		if inst.stopped.Load() {
			// Emit a final Closed so consumers see the same terminal event
			// the previous fd-close-from-Stop path produced before the
			// events channel closes.
			inst.emit(WatchEvent{Kind: WatchEventClosed, Ts: time.Now().UnixNano()})
			return
		}
		n, err := unix.Read(inst.fd, buf)
		if err != nil {
			switch err {
			case unix.EAGAIN:
				time.Sleep(20 * time.Millisecond)
				continue
			case unix.EINTR:
				continue
			}
			// FD closed by Stop, or another terminal error. The
			// Closed event is best-effort — if the channel is full
			// at shutdown we drop it.
			inst.emit(WatchEvent{Kind: WatchEventClosed, Ts: time.Now().UnixNano()})
			return
		}
		if n < unix.SizeofInotifyEvent {
			continue
		}
		rootGone := inst.parseBuf(buf[:n])
		if rootGone {
			return
		}
	}
}

// parseBuf walks one inotify read buffer, emitting one WatchEvent per
// matched mask bit. Returns true when the watched root vanished
// (caller should exit the loop). In recursive mode, event Name is
// joined with the wd's relative directory so cross-subtree events
// carry their full relpath; new subdirectories AddWatched on the fly.
func (inst *inotifyWatcher) parseBuf(buf []byte) (rootGone bool) {
	offset := 0
	for offset+unix.SizeofInotifyEvent <= len(buf) {
		wd := int32(binary.LittleEndian.Uint32(buf[offset:]))
		mask := binary.LittleEndian.Uint32(buf[offset+4:])
		cookie := binary.LittleEndian.Uint32(buf[offset+8:])
		nameLen := binary.LittleEndian.Uint32(buf[offset+12:])
		nameStart := offset + unix.SizeofInotifyEvent
		nameEnd := nameStart + int(nameLen)
		if nameEnd > len(buf) {
			return
		}
		name := ""
		if nameLen > 0 {
			raw := buf[nameStart:nameEnd]
			end := 0
			for end < len(raw) && raw[end] != 0 {
				end++
			}
			name = string(raw[:end])
		}
		offset = nameEnd
		now := time.Now().UnixNano()

		// IN_Q_OVERFLOW carries wd=-1 — no path lookup, no per-name
		// resolution; surface as Overflow and move on. The app's job
		// to rescan.
		if mask&unix.IN_Q_OVERFLOW != 0 {
			inst.emit(WatchEvent{Kind: WatchEventOverflow, Ts: now})
			continue
		}
		// IN_IGNORED fires when a watch is removed (either by us
		// implicitly via fd close, or by the kernel when the watched
		// inode is deleted). Clean up the wd→relDir mapping; the
		// IN_DELETE on the parent already announced the deletion.
		if mask&unix.IN_IGNORED != 0 {
			if wd != inst.rootWd {
				delete(inst.wdToRelDir, wd)
			}
			continue
		}

		relDir, known := inst.wdToRelDir[wd]
		if !known {
			// Event for a wd we already removed (race between the
			// kernel still emitting and our cleanup). Drop.
			continue
		}

		// Self-events have empty name and address the watched item
		// itself. IN_DELETE_SELF on the root is the only signal that
		// terminates the entire watch; on a subdir it just means that
		// subdir vanished — the parent's IN_DELETE already reported it
		// from the parent's perspective.
		if mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
			if wd == inst.rootWd {
				inst.emit(WatchEvent{Kind: WatchEventClosed, Ts: now})
				rootGone = true
				return
			}
			delete(inst.wdToRelDir, wd)
			continue
		}

		// Compose the event's Name: in single-level mode relDir is
		// always "" and we report bare names. In recursive mode the
		// relDir is the subdir path; joining gives "sub/file.txt".
		fullName := name
		if relDir != "" && name != "" {
			fullName = relDir + "/" + name
		} else if relDir != "" && name == "" {
			fullName = relDir
		}

		if mask&unix.IN_CREATE != 0 {
			inst.emit(WatchEvent{Kind: WatchEventCreate, Name: fullName, Cookie: cookie, Ts: now})
			// Dynamic add: if recursive watching is on and the newly
			// created entry is itself a directory, install a watch on
			// it before any of its own contents fire events. Best-
			// effort: max_user_watches / EACCES errors silently skip.
			if inst.recursive && mask&unix.IN_ISDIR != 0 && name != "" {
				inst.addSubdirWatch(filepath.Join(inst.path, fullName))
			}
		}
		if mask&unix.IN_DELETE != 0 {
			inst.emit(WatchEvent{Kind: WatchEventDelete, Name: fullName, Cookie: cookie, Ts: now})
		}
		if mask&unix.IN_MODIFY != 0 {
			inst.emit(WatchEvent{Kind: WatchEventModify, Name: fullName, Cookie: cookie, Ts: now})
		}
		if mask&unix.IN_ATTRIB != 0 {
			inst.emit(WatchEvent{Kind: WatchEventAttrib, Name: fullName, Cookie: cookie, Ts: now})
		}
		if mask&unix.IN_MOVED_FROM != 0 {
			inst.emit(WatchEvent{Kind: WatchEventRenameFrom, Name: fullName, Cookie: cookie, Ts: now})
			// The directory leaves its old name: whether it lands outside
			// the tree or under a new name (IN_MOVED_TO re-adds it), the
			// watches under the old name must go.
			if inst.recursive && mask&unix.IN_ISDIR != 0 && name != "" {
				inst.dropSubtreeWatches(fullName)
			}
		}
		if mask&unix.IN_MOVED_TO != 0 {
			inst.emit(WatchEvent{Kind: WatchEventRenameTo, Name: fullName, Cookie: cookie, Ts: now})
			// Dynamic add: directories moved into the watch tree need
			// a watch too, mirroring the IN_CREATE+IN_ISDIR path.
			if inst.recursive && mask&unix.IN_ISDIR != 0 && name != "" {
				inst.addSubdirWatch(filepath.Join(inst.path, fullName))
			}
		}
	}
	return
}

// emit non-blocking-publishes one event. Channel-full drops the event and
// substitutes a synthetic Overflow so the consumer knows state was lost.
// In extremis (Overflow also dropped) the consumer eventually sees a
// kernel-side IN_Q_OVERFLOW for the same effect.
func (inst *inotifyWatcher) emit(ev WatchEvent) {
	select {
	case inst.events <- ev:
	default:
		select {
		case inst.events <- WatchEvent{Kind: WatchEventOverflow, Ts: time.Now().UnixNano()}:
		default:
		}
	}
}
