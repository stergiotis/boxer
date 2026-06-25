//go:build !unix

package filestore

import "os"

// acquireLock is a no-op on platforms without advisory file locking: the
// store opens unlocked, so single-writer discipline is the caller's
// responsibility there. Unix builds get real flock-based mutual exclusion
// (see lock_unix.go).
func acquireLock(_ string) (*os.File, error) {
	return nil, nil
}

func releaseLock(_ *os.File) error {
	return nil
}
