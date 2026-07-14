// Package extbin is boxer's single sanctioned entry point for invoking
// external programs. Every host binary boxer can spawn is declared here as a
// [Program] and resolved through one policy, so "which external programs does
// this toolkit execute, and where are they found" is one auditable list — a
// [Registry] — rather than a grep across the tree. For a toolkit that ships
// airgapped and pins its own toolchain, that chokepoint is a supply-chain
// asset, not merely deduplication.
//
// extbin resolves executables; it deliberately does not own process lifecycle.
// [Program.Command] returns a configured *exec.Cmd that the caller still drives
// — wiring stdio, stdin pipes, process groups (SysProcAttr), signals, and
// Start/Wait/Kill as it sees fit. [Program.Output], [Program.CombinedOutput]
// and [Program.Run] are conveniences for the common "spawn and collect" case.
//
// The codelint rule CS012 ("no direct os/exec outside extbin") enforces that
// every external-process resolution flows through this package; extbin itself
// is the one exempt caller of os/exec.
package extbin

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Kind selects a [Program]'s resolution policy.
type Kind uint8

const (
	// Host is an ambient binary looked up on PATH — the bulk of boxer's
	// external dependencies (git, clickhouse-local, tinygo, rustfmt, …). Such
	// binaries are, by nature, not pinned; declaring them here at least makes
	// the set enumerable and gives each a uniform override + install hint.
	Host Kind = iota
	// GoTool is a module tool pinned in go.mod's tool block. It is invoked as
	// `go tool <Name>` (reproducible, version-matched) and falls back to a
	// `<Name>` binary on PATH when the module tool cache is unavailable.
	GoTool
	// GoToolchain is the `go` binary itself (`go env`, `go build`). Its
	// reproducibility comes from the go.mod toolchain directive + GOTOOLCHAIN.
	GoToolchain
	// Local is a caller-supplied executable path — a freshly built artifact or
	// a configured client binary. [Opts].Path is required; there is no PATH
	// lookup. The registry entry records the role, not a fixed path.
	Local
)

// String returns the lowercase kind name (host, gotool, gotoolchain, local),
// suitable for a table column or a log field.
func (k Kind) String() string {
	switch k {
	case Host:
		return "host"
	case GoTool:
		return "gotool"
	case GoToolchain:
		return "gotoolchain"
	case Local:
		return "local"
	default:
		return "unknown"
	}
}

// Program is a declared external-program dependency of boxer. Declare programs
// as package-level vars via [Declare]; the set of declarations is the audit
// surface.
type Program struct {
	// Name is the invocation/lookup name for Host and GoTool programs (e.g.
	// "git", "scc"). For Local programs it is a role label used only to key the
	// audit registry. Must be unique across all declarations.
	Name string
	// Kind is the resolution policy.
	Kind Kind
	// Module is the go.mod tool-block module path for a GoTool (e.g.
	// "github.com/boyter/scc/v3"), recorded to cross-reference the SBOM.
	// Ignored for other kinds.
	Module string
	// OverrideEnv, when non-empty, names an environment variable whose value —
	// an absolute path — takes precedence over PATH lookup. Host and
	// GoToolchain only. It is the hook a future hermetic/airgap mode can
	// require so no external binary is resolved from an ambient PATH.
	OverrideEnv string
	// InstallHint is appended to the not-found error, telling an operator how
	// to satisfy the dependency.
	InstallHint string
}

// Opts configures a single invocation.
type Opts struct {
	// Dir is the working directory; empty inherits the current process's.
	Dir string
	// Path overrides binary resolution with an explicit executable path. It is
	// the highest-priority source for any kind, and is required for Local
	// programs.
	Path string
	// Env sets the child environment, exactly like exec.Cmd.Env: nil inherits
	// the parent's, non-nil replaces it wholesale. Callers wanting
	// os.Environ()+X or key-overrides compute that slice here.
	Env []string
}

var (
	registryMu sync.RWMutex
	registry   = map[string]*Program{}
)

// Declare registers p and returns a stable handle to it. It panics on an empty
// Name or a duplicate — declarations are package-init constants, so a clash is
// a programming error worth failing loudly at startup rather than resolving
// ambiguously at run time.
func Declare(p Program) (handle *Program) {
	if p.Name == "" {
		panic("extbin: Program.Name must be set")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[p.Name]; dup {
		panic("extbin: duplicate program declaration: " + p.Name)
	}
	handle = &p
	registry[p.Name] = handle
	return
}

// Registry returns every declared program, sorted by Name. This is the
// machine-readable audit surface: everything boxer is wired to be able to
// spawn.
func Registry() (programs []*Program) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	programs = make([]*Program, 0, len(registry))
	for _, p := range registry {
		programs = append(programs, p)
	}
	sort.Slice(programs, func(i, j int) bool { return programs[i].Name < programs[j].Name })
	return
}

// Command resolves p under o and returns an *exec.Cmd with the context, working
// directory, environment and args applied — ready for the caller to wire stdio
// / SysProcAttr and then Start or Run. Use this whenever the invocation needs
// streaming, stdin, process-group control, or custom signal handling.
//
// For a GoTool this returns the pinned `go tool <Name>` form only. The PATH
// fallback is a run-time behaviour, so it applies to [Program.Output],
// [Program.CombinedOutput] and [Program.Run], which can observe a failed run;
// a caller that streams a GoTool gets the pinned form (pass Opts.Path to force
// a specific binary).
func (p *Program) Command(ctx context.Context, o Opts, args ...string) (cmd *exec.Cmd, err error) {
	var name string
	var pre []string
	name, pre, err = p.resolve(o)
	if err != nil {
		return
	}
	cmd = exec.CommandContext(ctx, name, append(pre, args...)...)
	cmd.Dir = o.Dir
	cmd.Env = o.Env
	return
}

// Output runs p and returns its stdout. Stderr is captured and folded into the
// error on failure. For a GoTool, a failed `go tool` invocation falls back to a
// PATH binary of the same Name.
func (p *Program) Output(ctx context.Context, o Opts, args ...string) (stdout []byte, err error) {
	return p.capture(ctx, o, false, args)
}

// CombinedOutput runs p and returns stdout and stderr interleaved, with the
// same GoTool fallback as [Program.Output].
func (p *Program) CombinedOutput(ctx context.Context, o Opts, args ...string) (out []byte, err error) {
	return p.capture(ctx, o, true, args)
}

// Run runs p and discards stdout; stderr is folded into the error on failure.
// It is a convenience for fire-and-forget invocations whose exit status is the
// only thing of interest.
func (p *Program) Run(ctx context.Context, o Opts, args ...string) (err error) {
	_, err = p.capture(ctx, o, false, args)
	return
}

func (p *Program) capture(ctx context.Context, o Opts, combined bool, args []string) (out []byte, err error) {
	var cmd *exec.Cmd
	cmd, err = p.Command(ctx, o, args...)
	if err != nil {
		return
	}
	out, err = runCapture(cmd, combined)
	if err != nil && p.Kind == GoTool && o.Path == "" {
		// The pinned `go tool <Name>` was unavailable or failed. Fall back to a
		// PATH binary of the same name when one exists; the primary attempt
		// fails fast (`go: no such tool`) so this adds no redundant work.
		var fb *exec.Cmd
		var ok bool
		fb, ok = p.goToolFallback(ctx, o, args)
		if ok {
			return runCapture(fb, combined)
		}
		err = eh.Errorf("%w (and no %q on PATH to fall back to)", err, p.Name)
	}
	return
}

// Resolve reports where p currently resolves on this host and whether it is
// available, without running it — the read-only counterpart to [Program.Command],
// for introspection and supply-chain attestation.
//
// path is the concrete executable file to attest, when there is one: the PATH
// lookup for a Host program, the `go` binary for GoToolchain, and — for a
// GoTool — the PATH binary of the same name if present. A GoTool that resolves
// only via the pinned `go tool <Name>` form reports (path="", available=true):
// its artifact lives in the go build cache and is attested by [Program.Module]
// + go.sum rather than a file hash. A Local program needs a caller-supplied
// path and reports ("", false).
func (p *Program) Resolve() (path string, available bool) {
	if bin, ok := p.override(); ok {
		return bin, true
	}
	switch p.Kind {
	case Host:
		if bin, err := exec.LookPath(p.Name); err == nil {
			return bin, true
		}
	case GoToolchain:
		if bin, err := exec.LookPath("go"); err == nil {
			return bin, true
		}
	case GoTool:
		if bin, err := exec.LookPath(p.Name); err == nil {
			return bin, true // a concrete PATH binary to attest
		}
		if _, err := exec.LookPath("go"); err == nil {
			return "", true // runnable via `go tool`, but no single binary file
		}
	case Local:
		// Needs a caller-supplied Opts.Path; not resolvable from the registry.
	}
	return "", false
}

// resolve computes the primary executable name and any leading args for p under
// o. For a GoTool the returned name is the `go` binary and pre is {"tool",
// Name}; callers of the capture path additionally consult goToolFallback.
func (p *Program) resolve(o Opts) (name string, pre []string, err error) {
	if o.Path != "" {
		name = o.Path
		return
	}
	switch p.Kind {
	case Local:
		err = eh.Errorf("extbin: program %q is Local and requires an explicit Opts.Path", p.Name)
	case Host:
		name, err = p.lookHost()
	case GoToolchain:
		if bin, ok := p.override(); ok {
			name = bin
			return
		}
		name = goBinary()
	case GoTool:
		name = goBinary()
		pre = []string{"tool", p.Name}
	default:
		err = eh.Errorf("extbin: program %q has unknown kind %d", p.Name, p.Kind)
	}
	return
}

func (p *Program) lookHost() (name string, err error) {
	if bin, ok := p.override(); ok {
		name = bin
		return
	}
	name, err = exec.LookPath(p.Name)
	if err != nil {
		err = p.notFound(err)
	}
	return
}

func (p *Program) override() (path string, ok bool) {
	if p.OverrideEnv == "" {
		return
	}
	v := strings.TrimSpace(os.Getenv(p.OverrideEnv))
	if v == "" {
		return
	}
	return v, true
}

func (p *Program) goToolFallback(ctx context.Context, o Opts, args []string) (cmd *exec.Cmd, ok bool) {
	bin, err := exec.LookPath(p.Name)
	if err != nil {
		return
	}
	cmd = exec.CommandContext(ctx, bin, args...)
	cmd.Dir = o.Dir
	cmd.Env = o.Env
	return cmd, true
}

func (p *Program) notFound(cause error) (err error) {
	if p.InstallHint != "" {
		return eh.Errorf("extbin: program %q not found (%s): %w", p.Name, p.InstallHint, cause)
	}
	return eh.Errorf("extbin: program %q not found: %w", p.Name, cause)
}

// goBinary resolves the `go` executable, preferring PATH and falling back to
// the bare name so exec can surface its own not-found error.
func goBinary() (bin string) {
	bin, err := exec.LookPath("go")
	if err != nil {
		bin = "go"
	}
	return
}

func runCapture(cmd *exec.Cmd, combined bool) (out []byte, err error) {
	if combined {
		out, err = cmd.CombinedOutput()
		if err != nil {
			err = eh.Errorf("%s: %w (output: %s)", cmd.Path, err, strings.TrimSpace(string(out)))
		}
		return
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		err = eh.Errorf("%s: %w (stderr: %s)", cmd.Path, err, strings.TrimSpace(stderr.String()))
		return
	}
	out = stdout.Bytes()
	return
}
