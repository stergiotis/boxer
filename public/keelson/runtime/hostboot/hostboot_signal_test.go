package hostboot

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// A loop that stops in time is left to Close, which reaps on the render
// goroutine (ADR-0261): the signal handler itself does not reap.
func TestShutdownOnSignalLeavesReapToTheLoop(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	loopDone := make(chan struct{})
	var stopped, reaped, exited atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		shutdownOnSignal(sigCh, loopDone, time.Second, zerolog.Nop(),
			func() { stopped.Store(true); close(loopDone) },
			func() { reaped.Store(true) },
			func() { exited.Store(true) })
	}()
	sigCh <- syscall.SIGTERM
	<-done
	assert.True(t, stopped.Load(), "the loop is asked to stop")
	assert.False(t, reaped.Load(), "the handler leaves reaping to Close")
	assert.False(t, exited.Load(), "no forced exit before the grace period")
}

// A wedged loop never returns: the handler reaps itself, then exits a grace
// period later.
func TestShutdownOnSignalReapsAWedgedLoop(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	var reaped atomic.Bool
	exited := make(chan struct{})
	go shutdownOnSignal(sigCh, make(chan struct{}), 20*time.Millisecond, zerolog.Nop(),
		func() {},
		func() { reaped.Store(true) },
		func() { close(exited) })
	sigCh <- syscall.SIGINT
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("no forced exit")
	}
	assert.True(t, reaped.Load(), "reaped before the forced exit")
}
