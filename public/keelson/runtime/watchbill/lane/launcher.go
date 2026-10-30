package lane

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ChildI is one job child the coordinator watches: it ends on its own,
// or the coordinator ends it.
type ChildI interface {
	// Done is closed when the child has exited and been reaped.
	Done() (done <-chan struct{})
	// Err is the exit error, valid after Done: nil for exit status zero.
	Err() (err error)
	// Terminate asks the child to stop (SIGTERM to its group).
	Terminate()
	// Kill ends the child now (SIGKILL to its group).
	Kill()
	// Describe names the child for a log line: a pid, a label.
	Describe() (s string)
}

// LauncherI starts the child that runs one claimed-by-id job. The
// coordinator owns what happens after Start: it watches Done, reads the
// row, and ends the child on stop.
type LauncherI interface {
	Start(ctx context.Context, job watchbillstore.Job) (child ChildI, err error)
}

// childProgram is the executable a job child runs: this one, for the host,
// so a cover-built host spawns a cover-built child (ADR-0344 §SD4). Local,
// so the caller supplies the path and the registry records the role.
var childProgram = extbin.Declare(extbin.Program{
	Name: "watchbill job child",
	Kind: extbin.Local,
})

// DefaultChildArgs is the verb a job child runs: `watchbill run --job <id>`
// on a binary that carries the watchbill CLI command.
func DefaultChildArgs(job watchbillstore.Job) (args []string) {
	return []string{"watchbill", "run", "--job", job.ID}
}

// ExecLauncher starts a process per job: Path with Args(job), in Dir, with
// this process's environment (the coverage opt-ins travel with it), in a
// process group of its own, its stdout and stderr piped into Log line by
// line (ADR-0344 §SD4, F1).
type ExecLauncher struct {
	// Path is the executable; os.Executable() for the host.
	Path string
	// Args builds the argument list for a job; nil is [DefaultChildArgs].
	Args func(job watchbillstore.Job) []string
	// Dir is the working directory; empty inherits.
	Dir string
	// Log receives the child's lines. A zerolog JSON line is re-emitted
	// at its own level with its fields; anything else is one line of text.
	Log zerolog.Logger
	// MaxLineBytes bounds one line read from the child; longer lines are
	// cut and marked. Zero is 64 KiB.
	MaxLineBytes int
}

// Start launches the child and starts reading its output; it returns once
// the process started. The reader goroutine ends at the pipe's end of
// file, which is when the child and everything that inherited the pipe
// have exited.
func (inst ExecLauncher) Start(ctx context.Context, job watchbillstore.Job) (child ChildI, err error) {
	argsOf := inst.Args
	if argsOf == nil {
		argsOf = DefaultChildArgs
	}
	// Background, not ctx: the child outlives the call that started it
	// and is ended by the coordinator through the group, not by a cancel
	// of the spawn.
	cmd, err := childProgram.Command(context.Background(), extbin.Opts{Path: inst.Path, Dir: inst.Dir}, argsOf(job)...)
	if err != nil {
		return nil, eh.Errorf("lane: resolve the child: %w", err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, eh.Errorf("lane: pipe: %w", err)
	}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.Stdin = nil
	ownGroup(cmd)
	if err = cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		return nil, eb.Build().Str("path", inst.Path).Str("job", job.ID).Errorf("lane: start the child: %w", err)
	}
	// The parent's copy of the write end closes now, so the read end sees
	// end of file once the child's side is gone.
	_ = w.Close()
	c := &execChild{cmd: cmd, done: make(chan struct{}), jobId: job.ID}
	go c.wait()
	go c.relay(r, inst.Log.With().Str("job", job.ID).Int("pid", cmd.Process.Pid).Logger(), inst.MaxLineBytes)
	child = c
	return
}

type execChild struct {
	cmd   *exec.Cmd
	jobId string
	done  chan struct{}
	err   error
	ended sync.Once
}

func (inst *execChild) wait() {
	inst.err = inst.cmd.Wait()
	close(inst.done)
}

func (inst *execChild) Done() (done <-chan struct{}) { return inst.done }
func (inst *execChild) Err() (err error)             { return inst.err }
func (inst *execChild) Terminate()                   { terminateGroup(inst.cmd) }
func (inst *execChild) Kill()                        { killGroup(inst.cmd) }
func (inst *execChild) Describe() (s string) {
	if inst.cmd.Process == nil {
		return "job " + inst.jobId
	}
	return "job " + inst.jobId + " pid " + strconv.Itoa(inst.cmd.Process.Pid)
}

// defaultMaxLineBytes is where a child's line is cut: a panic trace fits,
// a runaway writer does not take the host's log with it.
const defaultMaxLineBytes = 64 << 10

// relay reads the child's output line by line into log until end of file.
// It must never stop early: a reader that stalls fills the pipe and
// blocks the child on its next write (ADR-0344 §SD4, F1).
func (inst *execChild) relay(r io.ReadCloser, log zerolog.Logger, maxLine int) {
	defer func() { _ = r.Close() }()
	if maxLine <= 0 {
		maxLine = defaultMaxLineBytes
	}
	br := bufio.NewReaderSize(r, 32<<10)
	for {
		line, err := readLine(br, maxLine)
		if len(line) > 0 {
			emit(log, line)
		}
		if err != nil {
			return
		}
	}
}

// readLine returns one line without its terminator, cut at maxLine with
// a marker, and the read error that ended it (io.EOF included).
func readLine(br *bufio.Reader, maxLine int) (line []byte, err error) {
	var cut bool
	for {
		var part []byte
		var more bool
		part, more, err = br.ReadLine()
		if err != nil {
			return line, err
		}
		if !cut {
			room := maxLine - len(line)
			if len(part) > room {
				line = append(line, part[:max(room, 0)]...)
				line = append(line, " …[line cut]"...)
				cut = true
			} else {
				line = append(line, part...)
			}
		}
		if !more {
			return line, nil
		}
	}
}

// emit writes one child line into the host's log: a zerolog JSON object is
// re-emitted at its level with its own fields, so the child's run id and
// everything it said travel through; any other line is text.
func emit(log zerolog.Logger, line []byte) {
	s := strings.TrimRight(string(line), "\r")
	if strings.HasPrefix(s, "{") {
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil {
			lvl := zerolog.InfoLevel
			if l, ok := m["level"].(string); ok {
				if parsed, perr := zerolog.ParseLevel(l); perr == nil && parsed != zerolog.NoLevel {
					lvl = parsed
				}
			}
			msg, _ := m["message"].(string)
			delete(m, "level")
			delete(m, "message")
			delete(m, "time")
			log.WithLevel(lvl).Fields(m).Msg(msg)
			return
		}
	}
	log.Info().Str("line", s).Msg("watchbill lane: child output")
}

// waitOrKill waits for the child up to grace after a Terminate, then
// kills it and waits for the reap.
func waitOrKill(child ChildI, grace time.Duration) {
	select {
	case <-child.Done():
	case <-time.After(grace):
		child.Kill()
		<-child.Done()
	}
}
