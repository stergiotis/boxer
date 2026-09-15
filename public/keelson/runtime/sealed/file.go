package sealed

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// BaseDir names the directory unnamed sealed files are allocated under.
// Nothing in it is ever listable: it only decides which filesystem holds
// the inodes, so it must be one with O_TMPFILE support — ext4 and tmpfs
// both are. Empty resolves to <user cache dir>/boxer/adhoc.
var BaseDir = env.NewString(env.Spec{
	Name:        "BOXER_ADHOC_DIR",
	Default:     "",
	Description: "directory whose filesystem holds the unnamed sealed files of ad-hoc datasets and staged recordings (ADR-0240 §SD1); empty resolves to <user cache dir>/boxer/adhoc; must support O_TMPFILE (ext4, tmpfs)",
	Category:    env.CategorySystem,
})

// ErrUnsupported is wrapped into Create's error when the base directory's
// filesystem cannot allocate an unnamed inode. There is no fallback to a
// named file by design: point BaseDir at ext4 or tmpfs instead.
var ErrUnsupported = errors.New("filesystem does not support unnamed files (O_TMPFILE)")

// File is one unnamed, sealed file: a descriptor with no directory entry
// and the only copy of the key that opens it. It is written exactly once
// through [File.Writer] and then read any number of times through
// [File.Open]; readers are counted so [File.Retire] can close the inode
// the moment the last one leaves. Closing the descriptor is what frees the
// bytes — there is no name to remove.
type File struct {
	f    *os.File
	key  []byte
	aead aeadOpener

	mu        sync.Mutex
	state     fileState
	written   bool // Writer handed out
	ct, plain int64
	readers   int
	retiring  bool
	timer     *time.Timer
}

type fileState uint8

const (
	stateCreated fileState = iota // Writer not yet closed
	stateSealed                   // readable
	stateClosed                   // descriptor closed, key zeroed
)

// Create allocates an unnamed file under [BaseDir] and mints its key.
func Create() (inst *File, err error) {
	return CreateIn(BaseDirPath())
}

// CreateIn is Create under an explicit directory. The directory is
// created if missing; the file never appears in it.
func CreateIn(dir string) (inst *File, err error) {
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, eb.Build().Str("dir", dir).Errorf("sealed: prepare base directory: %w", err)
	}
	fd, err := unix.Open(dir, unix.O_TMPFILE|unix.O_RDWR|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
	if err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EISDIR) || errors.Is(err, unix.ENOTSUP) {
			return nil, eb.Build().Str("dir", dir).Errorf("sealed: %w: %w", ErrUnsupported, err)
		}
		return nil, eb.Build().Str("dir", dir).Errorf("sealed: allocate unnamed file: %w", err)
	}
	key := make([]byte, KeySize)
	if _, err = rand.Read(key); err != nil {
		_ = unix.Close(fd)
		return nil, eh.Errorf("sealed: mint key: %w", err)
	}
	aead, err := newGCM(key)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	inst = &File{f: os.NewFile(uintptr(fd), "sealed"), key: key, aead: aead}
	return
}

// Writer returns the one writer that fills the file. Its Close seals the
// stream and turns the file readable. A second call is an error.
func (inst *File) Writer() (w *Writer, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.state != stateCreated || inst.written {
		return nil, eh.Errorf("sealed: file already written")
	}
	w, err = newWriter(inst.f, inst.key, ChunkSize)
	if err != nil {
		return nil, err
	}
	inst.written = true
	w.onClose = func(ct, plain int64) {
		inst.mu.Lock()
		inst.ct, inst.plain = ct, plain
		if inst.state == stateCreated {
			inst.state = stateSealed
		}
		inst.mu.Unlock()
	}
	return
}

// Open returns an independent reader over the plaintext. The reader
// counts as open until its Close; a file being retired closes its
// descriptor when the count reaches zero.
func (inst *File) Open() (r *Reader, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch inst.state {
	case stateCreated:
		return nil, eh.Errorf("sealed: file not sealed yet")
	case stateClosed:
		return nil, eh.Errorf("sealed: file is closed")
	}
	r, err = newReader(inst.f, inst.ct, inst.key)
	if err != nil {
		return nil, err
	}
	inst.readers++
	r.onClose = inst.readerClosed
	return
}

func (inst *File) readerClosed() {
	inst.mu.Lock()
	inst.readers--
	closeNow := inst.retiring && inst.readers == 0 && inst.state != stateClosed
	inst.mu.Unlock()
	if closeNow {
		_ = inst.Close()
	}
}

// Retire closes the file once no reader is open: now if none is, else at
// the last reader's Close or after ceiling, whichever comes first — a
// reader that never leaves must not pin a key forever. Idempotent; a
// ceiling of zero or less closes at once regardless of readers.
func (inst *File) Retire(ceiling time.Duration) {
	inst.mu.Lock()
	if inst.state == stateClosed || inst.retiring {
		inst.mu.Unlock()
		return
	}
	if inst.readers == 0 || ceiling <= 0 {
		inst.mu.Unlock()
		_ = inst.Close()
		return
	}
	inst.retiring = true
	inst.timer = time.AfterFunc(ceiling, func() { _ = inst.Close() })
	inst.mu.Unlock()
}

// Close closes the descriptor — freeing the inode once no other process
// holds it, which none can — and zeroes the key. Open readers fail on
// their next read. Idempotent. Prefer [File.Retire] when readers may be
// mid-flight.
func (inst *File) Close() (err error) {
	inst.mu.Lock()
	if inst.state == stateClosed {
		inst.mu.Unlock()
		return
	}
	inst.state = stateClosed
	if inst.timer != nil {
		inst.timer.Stop()
		inst.timer = nil
	}
	clear(inst.key)
	inst.aead = nil
	f := inst.f
	inst.mu.Unlock()
	return f.Close()
}

// Size reports the ciphertext size in bytes; zero until sealed.
func (inst *File) Size() (n int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.ct
}

// PlaintextSize reports the plaintext size in bytes; zero until sealed.
func (inst *File) PlaintextSize() (n int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.plain
}

// Readers reports how many readers are open.
func (inst *File) Readers() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.readers
}

// Closed reports whether the descriptor has been closed.
func (inst *File) Closed() (closed bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state == stateClosed
}

// ciphertextAt exposes the raw sealed bytes to tests, which assert that
// nothing recognisable is in them.
func (inst *File) ciphertextAt(p []byte, off int64) (n int, err error) {
	return inst.f.ReadAt(p, off)
}

// BaseDirPath resolves [BaseDir]: the configured directory, else
// <user cache dir>/boxer/adhoc, else <temp dir>/boxer-adhoc.
func BaseDirPath() (dir string) {
	if d := BaseDir.Get(); d != "" {
		return d
	}
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "boxer", "adhoc")
	}
	return filepath.Join(os.TempDir(), "boxer-adhoc")
}

var _ io.ReadSeekCloser = (*Reader)(nil)
var _ io.ReaderAt = (*Reader)(nil)
var _ io.WriteCloser = (*Writer)(nil)
