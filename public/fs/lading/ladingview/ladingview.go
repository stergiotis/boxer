// Package ladingview is what an interactive host needs around an adapter
// view (ADR-0200 §SD4): the locking wrapper that lets one render thread and
// several lanes share the single-goroutine stores (ADR-0100), and the bounded
// read a preview wants — a file's head, its size and its kind in one call.
//
// It lives beside the adapter rather than inside the app so the app's own
// code never handles an [fs.File] or an [fs.FileInfo] directly: the capability
// gate (ADR-0026 §SD10) reads an interface call on those as potential OS file
// access, and a browser over a store should not carry that finding — every
// byte it reads comes out of ClickHouse.
package ladingview

import (
	"errors"
	"io"
	"io/fs"
	"sync"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Guard is the one lock every view of a store shares. The lading adapter's
// reads go through generated record stores, which are single-goroutine; a
// host that reads from the render thread (directory listings) and from lanes
// (previews) at once takes this before every call — the SFTP head's
// arrangement. Embed or hold it by value in the store owner.
type Guard struct {
	mu sync.Mutex
}

// Lock and Unlock are the guard's own; a caller that reaches the store
// outside a Locked view (the mount list, a SQL lane) takes them itself.
func (g *Guard) Lock()   { g.mu.Lock() }
func (g *Guard) Unlock() { g.mu.Unlock() }

// Locked serialises every call into an fs.FS against a [Guard].
type Locked struct {
	g    *Guard
	fsys fs.FS
}

// NewLocked wraps fsys; every view of one store should share g.
func NewLocked(g *Guard, fsys fs.FS) *Locked {
	return &Locked{g: g, fsys: fsys}
}

var _ fs.FS = (*Locked)(nil)
var _ fs.ReadDirFS = (*Locked)(nil)
var _ fs.StatFS = (*Locked)(nil)
var _ fs.ReadFileFS = (*Locked)(nil)
var _ fsmatch.FS = (*Locked)(nil)

func (l *Locked) Open(name string) (f fs.File, err error) {
	l.g.Lock()
	defer l.g.Unlock()
	f, err = l.fsys.Open(name)
	if err != nil {
		return
	}
	return &lockedFile{g: l.g, f: f}, nil
}

func (l *Locked) ReadDir(name string) ([]fs.DirEntry, error) {
	l.g.Lock()
	defer l.g.Unlock()
	return fs.ReadDir(l.fsys, name)
}

func (l *Locked) Stat(name string) (fs.FileInfo, error) {
	l.g.Lock()
	defer l.g.Unlock()
	return fs.Stat(l.fsys, name)
}

func (l *Locked) ReadFile(name string) ([]byte, error) {
	l.g.Lock()
	defer l.g.Unlock()
	return fs.ReadFile(l.fsys, name)
}

// MatchPaths forwards the push-down seam under the lock. A view whose file
// system has none answers [errors.ErrUnsupported], which is a consumer's cue
// to walk; the assertion has to be answered at call time because Locked
// wraps any fs.FS.
func (l *Locked) MatchPaths(dir, pattern string, hidden bool, limit int) ([]fsmatch.Match, bool, error) {
	m, ok := l.fsys.(fsmatch.FS)
	if !ok {
		return nil, false, errors.ErrUnsupported
	}
	l.g.Lock()
	defer l.g.Unlock()
	return m.MatchPaths(dir, pattern, hidden, limit)
}

// lockedFile serialises the reads a caller makes through a handle after Open
// returned — the same reason the SFTP head wraps its readers.
type lockedFile struct {
	g *Guard
	f fs.File
}

func (l *lockedFile) Stat() (fs.FileInfo, error) {
	l.g.Lock()
	defer l.g.Unlock()
	return l.f.Stat()
}

func (l *lockedFile) Read(p []byte) (int, error) {
	l.g.Lock()
	defer l.g.Unlock()
	return l.f.Read(p)
}

func (l *lockedFile) Close() error {
	l.g.Lock()
	defer l.g.Unlock()
	return l.f.Close()
}

func (l *lockedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	l.g.Lock()
	defer l.g.Unlock()
	if rd, ok := l.f.(fs.ReadDirFile); ok {
		return rd.ReadDir(n)
	}
	return nil, eh.Errorf("ladingview: not a directory")
}

// Head is what a preview needs to know about one path: its size and kind,
// and its bytes — all of them when the file fits the budget, the first
// headBytes otherwise.
type Head struct {
	Size      int64
	IsDir     bool
	Data      []byte
	Truncated bool
}

// ReadHead stats name and reads it: whole when Size <= maxWhole, else the
// first headBytes with Truncated set. A directory returns IsDir and no bytes.
func ReadHead(fsys fs.FS, name string, maxWhole int64, headBytes int) (h Head, err error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return
	}
	h.Size = info.Size()
	h.IsDir = info.IsDir()
	if h.IsDir {
		return
	}
	if h.Size <= maxWhole {
		h.Data, err = fs.ReadFile(fsys, name)
		return
	}
	f, err := fsys.Open(name)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, headBytes)
	n, rerr := io.ReadFull(f, buf)
	if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) && !errors.Is(rerr, io.EOF) {
		err = rerr
		return
	}
	h.Data, h.Truncated = buf[:n], true
	return
}
