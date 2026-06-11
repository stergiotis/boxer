//go:build llm_generated_opus47

package pijul

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// RunnerI is the lower-level CLI-verb seam used by the text
// backend. External consumers normally go through [BackendI]/[RepoI],
// which deal in domain objects rather than pijul subcommand arguments;
// the interface is exported so that test fakes (and out-of-package
// alternative runners) can satisfy it.
//
// Each method returns an `audit` string — a single formatted line
// describing what was executed and how it terminated — which the
// caller appends to the corresponding actor's CLI log without further
// formatting. Real errors come back via err. The Pull method
// additionally returns hadConflict==true when Pijul exited non-zero
// because it injected conflict markers; this is *not* a fatal error
// and err is nil in that case.
type RunnerI interface {
	Init(ctx context.Context, repoDir string) (audit string, err error)
	Clone(ctx context.Context, srcRepo string, parentDir string, name string) (audit string, err error)
	Add(ctx context.Context, repoDir string, file string) (audit string, err error)
	Record(ctx context.Context, repoDir string, author string, message string) (audit string, err error)
	Push(ctx context.Context, repoDir string, remoteRepo string) (audit string, err error)
	Pull(ctx context.Context, repoDir string, remoteRepo string) (audit string, hadConflict bool, err error)
	ApplyPatch(ctx context.Context, repoDir string, patchPath string) (audit string, err error)
	Log(ctx context.Context, repoDir string) (entries []LogEntry, audit string, err error)
	LatestHash(ctx context.Context, repoDir string) (hash string, audit string, err error)
	Credit(ctx context.Context, repoDir string, file string) (raw string, audit string, err error)
	LatestChangeFile(ctx context.Context, repoDir string) (patchPath string, err error)
}

const (
	defaultRunTimeout = 15 * time.Second
	defaultLogTimeout = 5 * time.Second
)

type cliRunner struct {
	runTimeout time.Duration
	logTimeout time.Duration
}

var _ RunnerI = (*cliRunner)(nil)

// NewCliRunner returns a runner that drives the system `pijul` binary
// via os/exec with conservative timeouts. The zero-valued [cliRunner]
// is also usable. Callers seed the [pijulTextBackend] with it.
func NewCliRunner() (inst *cliRunner) {
	inst = &cliRunner{
		runTimeout: defaultRunTimeout,
		logTimeout: defaultLogTimeout,
	}
	return
}

func (inst *cliRunner) timeoutForRun() (d time.Duration) {
	if inst.runTimeout == 0 {
		d = defaultRunTimeout
		return
	}
	d = inst.runTimeout
	return
}

func (inst *cliRunner) timeoutForLog() (d time.Duration) {
	if inst.logTimeout == 0 {
		d = defaultLogTimeout
		return
	}
	d = inst.logTimeout
	return
}

// runCmd invokes a single shell command with a timeout, captures its
// output, and returns a one-line audit summary. The audit string is
// shaped for direct append to the per-actor CliLog buffer.
func (inst *cliRunner) runCmd(ctx context.Context, dir string, name string, args ...string) (stdout string, audit string, exitErr *exec.ExitError, err error) {
	timeout := inst.timeoutForRun()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdStr := fmt.Sprintf("$ %s %s", name, strings.Join(args, " "))

	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	rerr := cmd.Run()
	stdout = outBuf.String()

	switch {
	case rerr == nil:
		out := strings.TrimSpace(stdout)
		if out != "" {
			audit = cmdStr + "\n" + out
		} else {
			audit = cmdStr + "\n[OK]"
		}
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		audit = fmt.Sprintf("%s\n[TIMEOUT %s]", cmdStr, timeout)
		err = eh.Errorf("command timed out after %s: %s", timeout, cmdStr)
	default:
		audit = fmt.Sprintf("%s\n[ERROR] %v\n%s", cmdStr, rerr, errBuf.String())
		var ee *exec.ExitError
		if errors.As(rerr, &ee) {
			exitErr = ee
		}
		err = eh.Errorf("command failed: %s\nstderr: %s: %w", cmdStr, errBuf.String(), rerr)
	}
	return
}

func (inst *cliRunner) Init(ctx context.Context, repoDir string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "init")
	return
}

// Clone runs in parentDir so that pijul resolves the relative dest
// name; this matches the original demo behaviour.
func (inst *cliRunner) Clone(ctx context.Context, srcRepo string, parentDir string, name string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, parentDir, "pijul", "clone", srcRepo, name)
	return
}

func (inst *cliRunner) Add(ctx context.Context, repoDir string, file string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "add", file)
	return
}

func (inst *cliRunner) Record(ctx context.Context, repoDir string, author string, message string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "record", "--author", author, "-am", message)
	return
}

func (inst *cliRunner) Push(ctx context.Context, repoDir string, remoteRepo string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "push", "--all", remoteRepo)
	return
}

// Pull treats exit code 1 as a non-fatal "applied with conflicts"
// signal. The caller distinguishes via hadConflict; err is nil in that
// case so the demo doesn't surface the conflict as a fatal error.
func (inst *cliRunner) Pull(ctx context.Context, repoDir string, remoteRepo string) (audit string, hadConflict bool, err error) {
	var exitErr *exec.ExitError
	_, audit, exitErr, err = inst.runCmd(ctx, repoDir, "pijul", "pull", "--all", remoteRepo)
	if err != nil && exitErr != nil && exitErr.ExitCode() == 1 {
		hadConflict = true
		err = nil
	}
	return
}

func (inst *cliRunner) ApplyPatch(ctx context.Context, repoDir string, patchPath string) (audit string, err error) {
	_, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "apply", patchPath)
	return
}

// Log uses a tighter timeout than the mutating commands because it is
// invoked from the reload path on every task tick.
func (inst *cliRunner) Log(ctx context.Context, repoDir string) (entries []LogEntry, audit string, err error) {
	timeout := inst.timeoutForLog()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdStr := "$ pijul log --output-format json"
	cmd := exec.CommandContext(cctx, "pijul", "log", "--output-format", "json")
	cmd.Dir = repoDir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	rerr := cmd.Run()
	switch {
	case rerr == nil:
		audit = cmdStr + "\n[OK]"
		entries, err = ParseLogJSON(outBuf.Bytes())
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		audit = fmt.Sprintf("%s\n[TIMEOUT %s]", cmdStr, timeout)
		err = eh.Errorf("pijul log timed out after %s", timeout)
	default:
		audit = fmt.Sprintf("%s\n[ERROR] %v\n%s", cmdStr, rerr, errBuf.String())
		err = eh.Errorf("pijul log error: %s: %w", errBuf.String(), rerr)
	}
	return
}

func (inst *cliRunner) LatestHash(ctx context.Context, repoDir string) (hash string, audit string, err error) {
	var raw string
	raw, audit, _, err = inst.runCmd(ctx, repoDir, "pijul", "log", "--hash-only")
	if err != nil {
		return
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			hash = line
			return
		}
	}
	hash = "unknown"
	return
}

func (inst *cliRunner) Credit(ctx context.Context, repoDir string, file string) (raw string, audit string, err error) {
	timeout := inst.timeoutForLog()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdStr := "$ pijul credit " + file
	cmd := exec.CommandContext(cctx, "pijul", "credit", file)
	cmd.Dir = repoDir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	rerr := cmd.Run()
	switch {
	case rerr == nil:
		audit = cmdStr + "\n[OK]"
		raw = outBuf.String()
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		audit = fmt.Sprintf("%s\n[TIMEOUT %s]", cmdStr, timeout)
		err = eh.Errorf("pijul credit timed out after %s", timeout)
	default:
		audit = fmt.Sprintf("%s\n[ERROR] %v\n%s", cmdStr, rerr, errBuf.String())
		err = eh.Errorf("pijul credit error: %s: %w", errBuf.String(), rerr)
	}
	return
}

// LatestChangeFile returns the most recently modified file under
// <repoDir>/.pijul/changes/ — used by the demo's "Email Patch" feature
// to extract a binary patch for peer-to-peer distribution. Pijul splits
// hashes into nested directories, so a mod-time scan is the most
// portable way to find "the patch I just recorded".
func (inst *cliRunner) LatestChangeFile(ctx context.Context, repoDir string) (patchPath string, err error) {
	changeDir := filepath.Join(repoDir, ".pijul", "changes")
	var maxTime time.Time
	werr := filepath.WalkDir(changeDir, func(p string, d os.DirEntry, e error) (rerr error) {
		if e != nil || d.IsDir() {
			return
		}
		info, ierr := d.Info()
		if ierr != nil {
			return
		}
		if info.ModTime().After(maxTime) {
			maxTime = info.ModTime()
			patchPath = p
		}
		return
	})
	if werr != nil {
		err = eh.Errorf("walk changes dir %s: %w", changeDir, werr)
		return
	}
	if patchPath == "" {
		err = eh.Errorf("no binary patch file found in %s", changeDir)
	}
	return
}
