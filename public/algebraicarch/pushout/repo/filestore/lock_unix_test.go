//go:build unix

package filestore

import (
	"errors"
	"testing"
)

// A second Open of a live store's directory fails with ErrLocked; after
// Close (or process exit, which the OS models for us) the lock is free to
// reacquire. This is the single-writer guarantee the engine's recovery
// model assumes — two processes on one root would corrupt acknowledged
// state.
func TestLock_SecondOpenFailsThenSucceedsAfterClose(tt *testing.T) {
	dir := tt.TempDir()
	st1, err := Open(dir)
	if err != nil {
		tt.Fatalf("first open: %v", err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrLocked) {
		tt.Fatalf("second open while first is live: want ErrLocked, got %v", err)
	}
	if err := st1.Close(); err != nil {
		tt.Fatalf("close: %v", err)
	}
	st2, err := Open(dir)
	if err != nil {
		tt.Fatalf("reopen after close must succeed, got %v", err)
	}
	if err := st2.Close(); err != nil {
		tt.Fatalf("close reopened: %v", err)
	}
}
