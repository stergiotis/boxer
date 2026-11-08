package application

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostwake"
)

// wakeFifoEnv is the variable the client reads the FIFO's path from
// (rust/imzero2/src/imzero2/wake.rs). Mirrored by imzero2env.WakeFifo.
const wakeFifoEnv = "IMZERO2_WAKE_FIFO"

// waker is the Go end of the client's wake channel (ADR-0363 §M4, proposed):
// a FIFO the client reads on a thread of its own, turning each byte into a
// repaint request, so code off the render goroutine can ask for a pass
// between heartbeats (hostwake.Wake).
type waker struct {
	dir  string
	path string
	// kick holds at most one wake not yet written: wakes that arrive before
	// the writer gets to them are served by one byte, and so by one pass.
	kick  chan struct{}
	done  chan struct{}
	close sync.Once
}

// newWaker makes the FIFO and names it in cmd's environment. It returns nil
// where FIFOs are not available; the client then keeps its cadence.
func newWaker(cmd *exec.Cmd) (w *waker, err error) {
	if !fifoSupported {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "imzero2-wake-")
	if err != nil {
		return nil, eh.Errorf("unable to make the wake fifo's directory: %w", err)
	}
	path := filepath.Join(dir, "wake")
	if err = makeFifo(path); err != nil {
		_ = os.RemoveAll(dir)
		return nil, eh.Errorf("unable to make the wake fifo: %w", err)
	}
	cmd.Env = append(cmd.Environ(), wakeFifoEnv+"="+path)
	w = &waker{dir: dir, path: path, kick: make(chan struct{}, 1), done: make(chan struct{})}
	return w, nil
}

// run writes one byte per kick until Close. Opening the FIFO for writing
// waits for the client to open it for reading; a client that never does
// (one built before the wake existed) leaves the kicks to drop.
func (inst *waker) run() {
	f, err := os.OpenFile(inst.path, os.O_WRONLY, 0)
	if err != nil {
		log.Warn().Err(err).Msg("wake fifo: unable to open for writing; the client keeps its cadence")
		return
	}
	defer func() { _ = f.Close() }()
	b := []byte{1}
	for {
		select {
		case <-inst.done:
			return
		case <-inst.kick:
			if _, err = f.Write(b); err != nil {
				// The client has exited or closed its end.
				return
			}
		}
	}
}

// wake asks for a pass without blocking.
func (inst *waker) wake() {
	select {
	case inst.kick <- struct{}{}:
	default:
	}
}

// Close stops the writer and removes the FIFO. A writer still waiting for
// the client to open the FIFO is released by a reader opened here.
func (inst *waker) Close() (err error) {
	inst.close.Do(func() {
		hostwake.Set(nil)
		close(inst.done)
		if r, e := os.OpenFile(inst.path, os.O_RDONLY|nonBlockFlag, 0); e == nil {
			_ = r.Close()
		}
		err = os.RemoveAll(inst.dir)
	})
	return err
}
