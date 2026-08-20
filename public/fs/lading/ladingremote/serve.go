package ladingremote

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/pkg/sftp"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Remote is an [FS] over a spawned `rclone serve sftp --stdio`, and the
// process behind it.
//
// Close is not optional: it ends the session and reaps the process. A Remote
// that is dropped leaves an rclone running until its stdin closes, which for a
// leaked pipe is "eventually".
type Remote struct {
	*FS
	cmd    *exec.Cmd
	client *sftp.Client
	stdin  io.WriteCloser
	stderr *ringBuffer
	once   sync.Once
	closed error
}

// Option configures [Serve].
type Option func(*serveConfig)

type serveConfig struct {
	args   []string
	opts   extbin.Opts
	subdir string
}

// WithFilters passes rclone's own filter flags to the serving side —
// `--include`, `--exclude`, `--filter-from`, `--max-size` and the rest.
//
// The filter runs at the source, which is the point: a mount's content policy
// for a remote is rclone's filter language rather than anything this store
// invents, and what is filtered out is never transferred, let alone stored.
func WithFilters(args ...string) Option {
	return func(inst *serveConfig) { inst.args = append(inst.args, args...) }
}

// WithArgs passes any other flags to `rclone serve sftp`.
func WithArgs(args ...string) Option {
	return func(inst *serveConfig) { inst.args = append(inst.args, args...) }
}

// WithExtbinOpts overrides how the rclone binary is resolved — a pinned path,
// a working directory, an environment.
func WithExtbinOpts(o extbin.Opts) Option {
	return func(inst *serveConfig) { inst.opts = o }
}

// WithSubdir roots the returned FS at a path inside what rclone serves,
// rather than at its top.
func WithSubdir(dir string) Option {
	return func(inst *serveConfig) { inst.subdir = dir }
}

// Serve spawns `rclone serve sftp --stdio <remote>` and returns its tree as an
// `fs.FS`.
//
// remote is rclone's own spelling — `s3:bucket/prefix`, `gdrive:docs`,
// `/a/local/path`, a `crypt:` remote, a connection string. Nothing about it is
// parsed here: this end knows only that rclone will speak SFTP on the pipe.
//
// There is no port, no listener and no credential in this path. rclone holds
// whatever the remote needs, and the only channel between the two processes is
// the pipe — the same shape as the egress head, in the other direction.
func Serve(ctx context.Context, remote string, opts ...Option) (inst *Remote, err error) {
	if strings.TrimSpace(remote) == "" {
		err = eh.Errorf("ladingremote: no remote given")
		return
	}
	cfg := serveConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	args := append([]string{"serve", "sftp", "--stdio"}, cfg.args...)
	args = append(args, remote)
	cmd, err := extbin.Rclone.Command(ctx, cfg.opts, args...)
	if err != nil {
		err = eh.Errorf("ladingremote: resolve rclone: %w", err)
		return
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		err = eh.Errorf("ladingremote: stdin pipe: %w", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		err = eh.Errorf("ladingremote: stdout pipe: %w", err)
		return
	}
	// rclone logs to stderr, and a failed spawn says why there. Kept bounded
	// and folded into the error rather than inherited: a serve that dies mid
	// walk would otherwise leave the caller with an EOF and no reason.
	ring := newRingBuffer(8 << 10)
	cmd.Stderr = ring

	err = cmd.Start()
	if err != nil {
		err = eh.Errorf("ladingremote: start rclone: %w", err)
		return
	}

	client, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		err = eb.Build().Str("remote", remote).Str("stderr", ring.String()).
			Errorf("ladingremote: rclone did not speak SFTP: %w", err)
		return
	}

	root := "/"
	if cfg.subdir != "" {
		root = "/" + strings.Trim(cfg.subdir, "/")
	}
	fsys, err := NewFS(client, root)
	if err != nil {
		_ = client.Close()
		_ = cmd.Wait()
		return
	}
	inst = &Remote{FS: fsys, cmd: cmd, client: client, stdin: stdin, stderr: ring}
	return
}

// Close ends the session and waits for rclone to exit.
//
// Closing the client closes the pipe, which is how `serve --stdio` learns the
// session is over; the Wait after it is what stops a walk from leaving a
// process behind.
func (inst *Remote) Close() error {
	inst.once.Do(func() {
		cerr := inst.client.Close()
		_ = inst.stdin.Close()
		werr := inst.cmd.Wait()
		switch {
		case cerr != nil:
			inst.closed = eh.Errorf("ladingremote: close sftp client: %w", cerr)
		case werr != nil:
			// An rclone that exits non-zero after a clean close is worth
			// reporting with what it said, not swallowing.
			inst.closed = eb.Build().Str("stderr", inst.stderr.String()).
				Errorf("ladingremote: rclone exited: %w", werr)
		}
	})
	return inst.closed
}

// Stderr is what rclone has said so far, bounded to the ring's size. Useful
// when a walk over a remote is empty and the reason is a filter or a
// credential rather than an empty remote.
func (inst *Remote) Stderr() string { return inst.stderr.String() }

// ringBuffer keeps the last n bytes written to it. rclone can be chatty, and a
// spawn that fails after megabytes of progress lines should still report the
// part that says why.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer { return &ringBuffer{size: size} }

func (inst *ringBuffer) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.buf = append(inst.buf, p...)
	if over := len(inst.buf) - inst.size; over > 0 {
		inst.buf = inst.buf[over:]
	}
	return len(p), nil
}

func (inst *ringBuffer) String() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return strings.TrimSpace(string(inst.buf))
}

var _ io.Writer = (*ringBuffer)(nil)
