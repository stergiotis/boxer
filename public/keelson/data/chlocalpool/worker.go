//go:build llm_generated_opus47

package chlocalpool

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Worker wraps one clickhouse-local subprocess. The pool spawns
// workers ahead of time; on Acquire, the caller writes SQL via
// WriteSQL, drains Stdout, then calls Wait and Close. Workers are
// single-use per ADR-0028 §SD3 — once submitted they exit and are
// not returned to the pool.
type Worker struct {
	pool   *Pool
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *capBuffer
	tmpdir string
	bornAt time.Time

	submitOnce sync.Once
	submitErr  error

	waitOnce sync.Once
	waitErr  error
	waitDone chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// newWorker spawns a clickhouse-local subprocess per cfg. The
// returned Worker is registered in pool.live by the caller (the
// pool holds the mutex during spawn ordering).
func newWorker(ctx context.Context, p *Pool) (w *Worker, err error) {
	cfg := p.cfg

	tmpdir, err := os.MkdirTemp(cfg.BaseTmpDir, "chlocal-*")
	if err != nil {
		err = eh.Errorf("chlocalpool: mktemp: %w", err)
		return
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmpdir)
		}
	}()

	args := []string{
		"--path", tmpdir,
		"--max_memory_usage", strconv.FormatUint(cfg.MaxMemoryPerWorker, 10),
		"--logger.console", "0",
	}

	cmd := exec.Command(cfg.BinaryPath, args...)
	stderr := &capBuffer{cap: int(cfg.StderrCapBytes)}
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		err = eh.Errorf("chlocalpool: stdin pipe: %w", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		err = eh.Errorf("chlocalpool: stdout pipe: %w", err)
		return
	}

	// Start() is fast in practice; guard against pathological hangs
	// (slow disk, exhausted PID table) with cfg.SpawnTimeout.
	startDone := make(chan error, 1)
	go func() { startDone <- cmd.Start() }()
	select {
	case startErr := <-startDone:
		if startErr != nil {
			err = eh.Errorf("chlocalpool: start: %w", startErr)
			return
		}
	case <-time.After(cfg.SpawnTimeout):
		// Best effort: reap the process if Start eventually completes.
		go func() {
			if startErr := <-startDone; startErr == nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		}()
		err = eh.Errorf("chlocalpool: spawn timed out after %s", cfg.SpawnTimeout)
		return
	case <-ctx.Done():
		go func() {
			if startErr := <-startDone; startErr == nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		}()
		err = eh.Errorf("chlocalpool: spawn cancelled: %w", ctx.Err())
		return
	}

	w = &Worker{
		pool:     p,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		tmpdir:   tmpdir,
		bornAt:   time.Now(),
		waitDone: make(chan struct{}),
	}
	return
}

// WriteSQL writes "<sql> FORMAT <format>;\n" to the worker's stdin
// and closes stdin so the worker can execute. The format argument
// may be empty if the SQL already contains a FORMAT clause.
// Idempotent: subsequent calls return the first error.
func (inst *Worker) WriteSQL(sql string, format string) (err error) {
	inst.submitOnce.Do(func() {
		var payload string
		if format == "" {
			payload = sql + ";\n"
		} else {
			payload = sql + " FORMAT " + format + ";\n"
		}
		_, writeErr := io.WriteString(inst.stdin, payload)
		if writeErr != nil {
			inst.submitErr = eh.Errorf("chlocalpool: write stdin: %w", writeErr)
			return
		}
		if closeErr := inst.stdin.Close(); closeErr != nil {
			inst.submitErr = eh.Errorf("chlocalpool: close stdin: %w", closeErr)
			return
		}
	})
	err = inst.submitErr
	return
}

// Stdout returns the worker's stdout pipe. Drain to EOF before
// calling Wait; calling Wait early discards remaining bytes.
func (inst *Worker) Stdout() (r io.Reader) {
	r = inst.stdout
	return
}

// Wait blocks until the worker process exits, returning nil on
// clean exit or an error wrapping the exit code and stderr tail.
// Idempotent. Callers must have drained Stdout to EOF first; the
// exec package documents that Wait closes the stdout pipe.
func (inst *Worker) Wait() (err error) {
	inst.waitOnce.Do(func() {
		waitErr := inst.cmd.Wait()
		if waitErr != nil {
			tail := inst.stderr.Bytes()
			if len(tail) > 0 {
				inst.waitErr = eh.Errorf("chlocalpool: worker exit: %w (stderr: %q)", waitErr, string(tail))
			} else {
				inst.waitErr = eh.Errorf("chlocalpool: worker exit: %w", waitErr)
			}
		}
		close(inst.waitDone)
	})
	err = inst.waitErr
	return
}

// StderrTail returns up to Config.StderrCapBytes of captured stderr.
// Stable only after Wait returns or the process has otherwise exited.
func (inst *Worker) StderrTail() (b []byte) {
	b = inst.stderr.Bytes()
	return
}

// Age returns time since the worker process spawned. Used by the
// pool watchdog.
func (inst *Worker) Age() (d time.Duration) {
	d = time.Since(inst.bornAt)
	return
}

// Done returns a channel closed once the worker's subprocess has
// been waited on (either via Wait or as part of Close after a reap).
// Tests poll this to observe watchdog reaps; production callers
// should not need it directly.
func (inst *Worker) Done() (ch <-chan struct{}) {
	ch = inst.waitDone
	return
}

// Close releases the worker's resources. If the process is still
// running, it is SIGTERMed and then SIGKILLed after KillGrace.
// Always removes the tmpdir and notifies the pool. Idempotent.
func (inst *Worker) Close() (err error) {
	if inst == nil {
		return
	}
	inst.closeOnce.Do(func() {
		// If Wait hasn't been called and the process is alive, terminate.
		select {
		case <-inst.waitDone:
			// Already reaped by a prior Wait call.
		default:
			if inst.cmd.Process != nil {
				_ = inst.cmd.Process.Signal(syscall.SIGTERM)
				inst.reapWithGrace()
			} else {
				close(inst.waitDone) // never started; mark reaped
			}
		}
		// Close stdin/stdout pipes (idempotent — exec may have closed them).
		if inst.stdin != nil {
			_ = inst.stdin.Close()
		}
		if inst.stdout != nil {
			_ = inst.stdout.Close()
		}
		// Cleanup tmpdir.
		if inst.tmpdir != "" {
			if rmErr := os.RemoveAll(inst.tmpdir); rmErr != nil {
				inst.closeErr = eh.Errorf("chlocalpool: rm tmpdir %s: %w", inst.tmpdir, rmErr)
			}
		}
		// Notify pool last so the live count drops after cleanup.
		if inst.pool != nil {
			inst.pool.workerClosed(inst)
		}
	})
	err = inst.closeErr
	return
}

// reapWithGrace assumes the caller has already signalled the
// process and waits for it to exit, escalating to SIGKILL after
// KillGrace. Always leaves waitDone closed.
func (inst *Worker) reapWithGrace() {
	reapDone := make(chan struct{})
	go func() {
		_ = inst.Wait() // marks waitDone closed
		close(reapDone)
	}()
	select {
	case <-reapDone:
	case <-time.After(inst.pool.cfg.KillGrace):
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Kill()
		}
		<-reapDone
	}
}

// capBuffer is a thread-safe head-capped byte sink for stderr. Once
// the cap is reached, further writes are silently discarded. The
// head (not the tail) is preserved because ClickHouse error messages
// arrive as a single multi-line payload on first write.
type capBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

func (inst *capBuffer) Write(p []byte) (n int, err error) {
	n = len(p)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	room := inst.cap - len(inst.buf)
	if room <= 0 {
		return
	}
	if len(p) > room {
		inst.buf = append(inst.buf, p[:room]...)
		return
	}
	inst.buf = append(inst.buf, p...)
	return
}

func (inst *capBuffer) Bytes() (b []byte) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	b = make([]byte, len(inst.buf))
	copy(b, inst.buf)
	return
}
