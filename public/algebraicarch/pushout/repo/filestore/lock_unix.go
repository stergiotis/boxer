//go:build unix

package filestore

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// acquireLock takes an advisory exclusive lock on dir via flock(2) on a
// "lock" file. LOCK_NB makes contention fail fast (ErrLocked) instead of
// blocking; the lock is held by the returned open fd and released when it
// is closed — including on process death, so a crash leaves no stale lock
// to clean up by hand.
func acquireLock(dir string) (lockFile *os.File, err error) {
	path := filepath.Join(dir, "lock")
	f, oerr := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if oerr != nil {
		err = eh.Errorf("open lock file: %w", oerr)
		return
	}
	if ferr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); ferr != nil {
		_ = f.Close()
		err = eh.Errorf("%s: %w", path, errors.Join(ErrLocked, ferr))
		return
	}
	lockFile = f
	return
}

// releaseLock drops the advisory lock by closing its fd (close releases
// the flock). Safe on a nil file.
func releaseLock(lockFile *os.File) (err error) {
	if lockFile == nil {
		return
	}
	if cerr := lockFile.Close(); cerr != nil {
		err = eh.Errorf("close lock file: %w", cerr)
	}
	return
}
