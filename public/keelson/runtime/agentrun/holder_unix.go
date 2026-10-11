//go:build unix

package agentrun

import (
	"errors"
	"syscall"
)

// processExists sends signal 0: no signal, only the check that the pid
// names a process (EPERM: one this user may not signal, which exists).
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
