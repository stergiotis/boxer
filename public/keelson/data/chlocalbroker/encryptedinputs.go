package chlocalbroker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// fifoOpenPollInterval bounds the wait between attempts to open a pipe's
// write end while clickhouse-local has not yet opened the read end.
const fifoOpenPollInterval = time.Millisecond

// encWriter streams one encrypted dataset's decrypt into its named pipe.
type encWriter struct {
	name   string
	fifo   string
	ctPath string
	key    []byte

	mu     sync.Mutex
	fd     *os.File
	closed bool
	err    error
}

// materializeEncryptedInputs creates one named pipe per encrypted input
// under a fresh directory and returns a prelude binding each as a
// TEMPORARY table read from its pipe as ArrowStream, a wait() that joins
// the streaming goroutines and returns the first real writer error, and
// a cleanup() that removes the pipes. Each dataset's key is resolved
// from keys by table name; a missing key errors before any goroutine
// starts (ADR-0134 SD3). The caller MUST run cleanup after wait.
//
// FIFO discipline (verified against clickhouse-local 26.6, ADR-0134 M2):
// the reader blocks until pipe EOF — it does not stop at the Arrow
// end-of-stream marker — so a writer must close its pipe to end the
// read. Each writer opens O_WRONLY only once a reader is present (polled
// non-blocking, ctx-bounded), which guarantees the reader-opened-first
// ordering that a bare O_RDWR-then-close would race — on small data
// losing the payload and hanging the read with no writer present. It
// then streams the streaming-decrypt and closes to deliver EOF. A
// decryption error (authentication failure, truncation) is recorded and
// surfaced by wait(), which fails the request even if the worker
// consumed the verified prefix and exited 0.
//
// ctx bounds the writers so the stuck-writer path terminates: when the
// worker errors before opening a later pipe, wait() cancels ctx to
// release any writer still polling to open.
func materializeEncryptedInputs(ctx context.Context, baseTmpDir string, refs map[string]EncryptedInputRef, keys KeyStoreI) (prelude string, wait func() error, cleanup func(), err error) {
	wait = func() error { return nil }
	cleanup = func() {}
	if len(refs) == 0 {
		return
	}

	names := make([]string, 0, len(refs))
	for name := range refs {
		if !validInputTableName(name) {
			err = eh.Errorf("chlocalbroker: invalid encrypted input name %q", name)
			return
		}
		names = append(names, name)
	}
	sort.Strings(names) // deterministic prelude + stable cache-key fold order

	dir, mkErr := os.MkdirTemp(baseTmpDir, "chlocal-enc-*")
	if mkErr != nil {
		err = eh.Errorf("chlocalbroker: mktemp encrypted dir: %w", mkErr)
		return
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	// Pass 1: resolve keys, make pipes, build the prelude. Nothing is
	// spawned yet, so a failure here leaks no goroutine.
	writers := make([]*encWriter, 0, len(names))
	var b strings.Builder
	for _, name := range names {
		ref := refs[name]
		key, ok := keys.LookupDatasetKey(name)
		if !ok {
			err = eh.Errorf("chlocalbroker: no key registered for encrypted input %q", name)
			return
		}
		fifo := filepath.Join(dir, name+".fifo")
		if mfErr := unix.Mkfifo(fifo, 0o600); mfErr != nil {
			err = eh.Errorf("chlocalbroker: mkfifo %q: %w", name, mfErr)
			return
		}
		b.WriteString("CREATE TEMPORARY TABLE ")
		b.WriteString(name)
		b.WriteString(" AS SELECT * FROM file(")
		b.WriteString(sqlQuoteString(fifo))
		b.WriteString(", 'ArrowStream', ")
		b.WriteString(sqlQuoteString(ref.Structure))
		b.WriteString(");\n")
		writers = append(writers, &encWriter{name: name, fifo: fifo, ctPath: ref.Path, key: key})
	}
	prelude = b.String()

	// Pass 2: spawn the streaming goroutines under a cancelable ctx.
	wctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, w := range writers {
		wg.Add(1)
		go w.run(wctx, &wg)
	}

	wait = func() (werr error) {
		// Release any writer still waiting to open its pipe (worker
		// errored before reaching that CREATE), close remaining fds to
		// nudge any writer blocked mid-stream, then join.
		cancel()
		for _, w := range writers {
			_ = w.closeFd()
		}
		wg.Wait()
		for _, w := range writers {
			if e := w.takeErr(); e != nil && !errors.Is(e, context.Canceled) {
				if werr == nil {
					werr = e
				}
			}
		}
		return
	}
	return
}

// run streams the decrypted ciphertext into the pipe, closing it to
// deliver EOF. All exits record an error (or nil) retrievable by wait().
func (inst *encWriter) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	src, err := os.Open(inst.ctPath)
	if err != nil {
		inst.setErr(eh.Errorf("chlocalbroker: open ciphertext %q: %w", inst.name, err))
		return
	}
	defer func() { _ = src.Close() }()

	ar, err := adhocdata.NewReader(src, inst.key)
	if err != nil {
		inst.setErr(eh.Errorf("chlocalbroker: decrypt reader %q: %w", inst.name, err))
		return
	}

	f, err := openFifoWrite(ctx, inst.fifo)
	if err != nil {
		inst.setErr(err) // ctx.Canceled on abort; otherwise a real open error
		return
	}
	if !inst.adoptFd(f) {
		// wait() fired closeFd before we adopted; abandon quietly.
		_ = f.Close()
		inst.setErr(context.Canceled)
		return
	}

	_, copyErr := io.Copy(f, ar)
	closeErr := inst.closeFd()
	switch {
	case copyErr != nil:
		inst.setErr(eh.Errorf("chlocalbroker: stream encrypted input %q: %w", inst.name, copyErr))
	case closeErr != nil:
		inst.setErr(eh.Errorf("chlocalbroker: close pipe %q: %w", inst.name, closeErr))
	}
}

// adoptFd stores f so wait() can close it, unless wait() has already
// asked writers to stop.
func (inst *encWriter) adoptFd(f *os.File) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return false
	}
	inst.fd = f
	return true
}

// closeFd closes the pipe fd once. Safe to call from both run (normal
// EOF) and wait (interrupt); the second call is a no-op.
func (inst *encWriter) closeFd() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil
	}
	inst.closed = true
	if inst.fd != nil {
		err = inst.fd.Close()
		inst.fd = nil
	}
	return
}

func (inst *encWriter) setErr(err error) {
	inst.mu.Lock()
	if inst.err == nil {
		inst.err = err
	}
	inst.mu.Unlock()
}

func (inst *encWriter) takeErr() (err error) {
	inst.mu.Lock()
	err = inst.err
	inst.mu.Unlock()
	return
}

// openFifoWrite opens path for writing once a reader is present. It
// polls O_WRONLY|O_NONBLOCK, which returns ENXIO while no reader has
// opened the read end, until it succeeds or ctx is done — so the write
// side is opened strictly after the read side, and a worker that never
// reaches the file() read cannot hang the writer past ctx. The returned
// *os.File stays under the Go runtime poller (a fifo is pollable), so a
// write larger than the pipe buffer parks the goroutine rather than
// failing with EAGAIN, and closeFd interrupts a parked write; a
// still-blocked write also returns EPIPE if the worker exits (which is
// when wait runs).
func openFifoWrite(ctx context.Context, path string) (f *os.File, err error) {
	for {
		f, err = os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.ENXIO) {
			return nil, eh.Errorf("chlocalbroker: open pipe for write: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(fifoOpenPollInterval):
		}
	}
}
