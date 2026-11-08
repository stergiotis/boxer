//go:build unix

package application

import "syscall"

const fifoSupported = true

const nonBlockFlag = syscall.O_NONBLOCK

func makeFifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
