//go:build unix

package application

import "syscall"

const fifoSupported = true

const nonBlockFlag = syscall.O_NONBLOCK

// errNoReader is what opening a FIFO for writing without waiting fails with
// while nobody has it open for reading.
var errNoReader error = syscall.ENXIO

func makeFifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
