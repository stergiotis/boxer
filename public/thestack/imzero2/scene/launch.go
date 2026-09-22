package scene

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
)

// hostProgram is the imzero2 host a scene launches: this executable for the
// `scene` command, a freshly built one for a Go test. Local, so the caller
// supplies the path and the registry records the role.
var hostProgram = extbin.Declare(extbin.Program{
	Name: "imzero2 host (scene)",
	Kind: extbin.Local,
})

// Options is what a launch needs besides the scene's own spec.
type Options struct {
	// Name labels the host's component, its log file and the driver's roster
	// entry.
	Name string
	// OutDir receives captures; logs go under OutDir/logs.
	OutDir string
	// HostBinary is the imzero2 host. Empty means this executable, which is
	// right for the `scene` command and wrong for a Go test — a test passes
	// the host it built.
	HostBinary string
	// RepoRoot is the checkout holding the Rust client and the fonts. Empty
	// means found by walking up from the working directory.
	RepoRoot string
	// HostDir is the host's working directory. Empty means RepoRoot, which is
	// right when the host is boxer's own; a downstream host whose apps read
	// paths relative to their checkout runs in that checkout instead.
	HostDir string
	// ClientBinary overrides client selection.
	ClientBinary string
	// SQL seeds the variable Spec.SQLEnv names.
	SQL string
	// Timeout bounds the wait for the carrier and each driver request.
	Timeout time.Duration
	// SettleMs is the driver's default pause after a step.
	SettleMs int
	DryRun   bool
	// IgnoreRequires runs a scene whose preconditions do not hold, instead of
	// skipping it: a missing fixture then shows as whatever the app draws
	// without it, which is sometimes the thing to look at.
	IgnoreRequires bool
	// Out receives what `tree` steps print; nil means os.Stdout.
	Out    io.Writer
	Logger zerolog.Logger
}

// Session is a launched host with a connected driver.
type Session struct {
	Client *carrierclient.Client
	// Vars holds what the session's `read` steps have bound, across every
	// Run, so a caller can take readings out and do arithmetic on them.
	Vars *carrierclient.Vars
	// URL is the carrier's WebSocket URL.
	URL string

	opts     Options
	cmd      *exec.Cmd
	exited   chan struct{}
	hostLog  *os.File
	services []*service
}

// freePortPair finds p with p and p+1 both free: the carrier takes the
// WebSocket on one and the viewer page on the next.
func freePortPair() (port int, err error) {
	for attempt := 0; attempt < 32; attempt++ {
		var l net.Listener
		if l, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return 0, eh.Errorf("unable to allocate a port: %w", err)
		}
		port = l.Addr().(*net.TCPAddr).Port
		l2, e2 := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port+1))
		_ = l.Close()
		if e2 == nil {
			_ = l2.Close()
			return port, nil
		}
	}
	return 0, eh.Errorf("unable to find two adjacent free ports")
}

// hostEnv builds the child's environment: this process's, without a display,
// plus the carrier's knobs, the services' exports and the scene's own — in
// that order, so a scene can override anything.
func hostEnv(spec Spec, opts Options, port int, w, h int, exported map[string]string) (env []string) {
	drop := map[string]bool{"DISPLAY": true, "WAYLAND_DISPLAY": true}
	for _, kv := range os.Environ() { //boxer:lint disable=CS011 reason="forwards the ambient process environment into the host it launches, minus the display"
		if name, _, _ := strings.Cut(kv, "="); !drop[name] {
			env = append(env, kv)
		}
	}
	fps := spec.FPS
	if fps <= 0 {
		fps = DefaultFPS
	}
	set := map[string]string{
		"IMZERO2_HEADLESS_LISTEN":     "127.0.0.1:" + strconv.Itoa(port),
		"IMZERO2_HEADLESS_DUMP_DIR":   opts.OutDir,
		"IMZERO2_HEADLESS_DUMP_EVERY": "1000000", // nothing lands but what a trace asks for
		"IMZERO2_HEADLESS_FPS":        strconv.Itoa(fps),
		"IMZERO2_SCREENSHOT_SIZE":     strconv.Itoa(w) + "x" + strconv.Itoa(h),
		"BOXER_COMPONENT":             "scene-" + opts.Name,
		// The software encoder: a scene must not depend on the box's GPU. A
		// scene that wants another lane says so in its own env.
		"IMZERO2_HEADLESS_ENCODER_ARGS": "-c:v libopenh264 -rc_mode off -bf 0 -g 100000",
	}
	for k, v := range exported {
		set[k] = v
	}
	if opts.SQL != "" {
		name := spec.SQLEnv
		if name == "" {
			name = DefaultSQLEnv
		}
		set[name] = opts.SQL
	}
	for k, v := range spec.Env {
		set[k] = v
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+set[k])
	}
	return env
}

// Launch starts the services and the host a spec asks for, waits for the
// carrier, and connects a driver. The caller owns the Session and must Close
// it; on error everything started so far has already been torn down.
func Launch(spec Spec, opts Options) (s *Session, err error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.Name == "" {
		opts.Name = spec.Launch
	}
	if opts.RepoRoot == "" {
		if opts.RepoRoot, err = FindRepoRoot("."); err != nil {
			return nil, err
		}
	}
	if opts.HostDir == "" {
		opts.HostDir = opts.RepoRoot
	}
	if opts.HostBinary == "" {
		if opts.HostBinary, err = os.Executable(); err != nil {
			return nil, eh.Errorf("unable to find this executable: %w", err)
		}
	}
	if opts.OutDir, err = filepath.Abs(opts.OutDir); err != nil {
		return nil, eh.Errorf("unable to resolve the output directory: %w", err)
	}
	if err = os.MkdirAll(filepath.Join(opts.OutDir, "logs"), 0o755); err != nil {
		return nil, eh.Errorf("unable to create the output directory: %w", err)
	}
	w, h, err := spec.Dimensions()
	if err != nil {
		return nil, err
	}
	client, err := chooseClient(opts.RepoRoot, opts.ClientBinary, spec.Needs)
	if err != nil {
		return nil, err
	}

	s = &Session{opts: opts, Vars: carrierclient.NewVars(), exited: make(chan struct{})}
	defer func() {
		if err != nil {
			_ = s.Close()
			s = nil
		}
	}()

	exported := map[string]string{}
	for _, name := range spec.Services {
		var svc *service
		if svc, err = startService(name, opts); err != nil {
			return s, err
		}
		s.services = append(s.services, svc)
		for k, v := range svc.export {
			exported[k] = v
		}
	}

	port, err := freePortPair()
	if err != nil {
		return s, err
	}
	s.URL = "ws://127.0.0.1:" + strconv.Itoa(port) + "/"

	args := []string{"--logFormat=console", "--logLevel=warn", "imzero2", "demo",
		"--clientBinary", client,
		"--clientInitialMainWindowWidth", strconv.Itoa(w),
		"--clientInitialMainWindowHeight", strconv.Itoa(h),
	}
	for _, f := range []struct{ flag, path string }{
		{"--mainFontTTF", resolveFont("Noto Sans", "Noto Sans")},
		{"--monoFontTTF", resolveFont("DejaVu Sans Mono", "DejaVu Sans Mono")},
		{"--phosphorFontTTF", filepath.Join(opts.RepoRoot, "rust", "imzero2", "assets", "fonts", "phosphor", "Phosphor.ttf")},
		// Without a fallback a codepoint the main font lacks renders as a
		// different glyph than on a desktop launch, at a different size.
		{"--fallbackFontTTF", resolveFont("Noto Sans Mono CJK JP", "CJK")},
	} {
		if _, e := os.Stat(f.path); f.path != "" && e == nil {
			args = append(args, f.flag, f.path)
		}
	}
	args = append(args, "--launch", spec.Launch)

	if s.hostLog, err = os.Create(filepath.Join(opts.OutDir, "logs", opts.Name+".host.log")); err != nil {
		return s, eh.Errorf("unable to create the host log: %w", err)
	}
	s.cmd, err = hostProgram.Command(context.Background(), extbin.Opts{
		Path: opts.HostBinary,
		Dir:  opts.HostDir,
		Env:  hostEnv(spec, opts, port, w, h, exported),
	}, args...)
	if err != nil {
		s.cmd = nil
		return s, err
	}
	s.cmd.Stdout, s.cmd.Stderr = s.hostLog, s.hostLog
	ownGroup(s.cmd)
	if err = s.cmd.Start(); err != nil {
		s.cmd = nil
		return s, eb.Build().Str("host", opts.HostBinary).Errorf("unable to start the host: %w", err)
	}
	go func() { _ = s.cmd.Wait(); close(s.exited) }()

	if err = s.awaitCarrier(port); err != nil {
		return s, err
	}
	s.Client, err = carrierclient.Connect(carrierclient.Config{
		URL: s.URL, Label: "scene-" + opts.Name, DialTimeout: opts.Timeout, Logger: opts.Logger,
	})
	if err != nil {
		return s, err
	}
	if spec.SettleMs > 0 {
		if err = s.Client.Idle(time.Duration(spec.SettleMs) * time.Millisecond); err != nil {
			return s, err
		}
	}
	return s, nil
}

// awaitCarrier waits for the carrier's port, and gives up at once when the
// host dies first — the log says why, and waiting out the timeout hides that.
func (inst *Session) awaitCarrier(port int) (err error) {
	deadline := time.Now().Add(inst.opts.Timeout)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	for {
		conn, e := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if e == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-inst.exited:
			return eb.Build().Str("log", inst.hostLog.Name()).Errorf("the host exited before its carrier came up")
		default:
		}
		if time.Now().After(deadline) {
			return eb.Build().Str("log", inst.hostLog.Name()).Stringer("timeout", inst.opts.Timeout).
				Errorf("the carrier did not come up")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Name is the label the session was launched under; its host log is
// logs/<Name>.host.log in the output directory.
func (inst *Session) Name() string { return inst.opts.Name }

// OutDir is where the session's captures land.
func (inst *Session) OutDir() string { return inst.opts.OutDir }

// Run executes steps against the session. Bindings persist across calls.
func (inst *Session) Run(steps []carrierclient.Step) (err error) {
	return carrierclient.RunTrace(inst.Client, steps, carrierclient.RunOptions{
		Timeout:  inst.opts.Timeout,
		SettleMs: inst.opts.SettleMs,
		DryRun:   inst.opts.DryRun,
		Out:      inst.opts.Out,
		Vars:     inst.Vars,
		Logger:   inst.opts.Logger,
	})
}

// RunJSONL is Run for steps written as a trace: one JSON object per line.
func (inst *Session) RunJSONL(trace string) (err error) {
	steps, err := carrierclient.ParseTrace(strings.NewReader(trace))
	if err != nil {
		return err
	}
	return inst.Run(steps)
}

// Close tears everything down: the driver, the host with its client, the
// services. It is safe on a half-built session and safe to call twice.
func (inst *Session) Close() (err error) {
	if inst == nil {
		return nil
	}
	if inst.Client != nil {
		_ = inst.Client.Close()
		inst.Client = nil
	}
	if inst.cmd != nil {
		terminateGroup(inst.cmd)
		select {
		case <-inst.exited:
		case <-time.After(10 * time.Second):
			killGroup(inst.cmd)
			<-inst.exited
		}
		// The Rust client owns the carrier socket and can outlive the Go host
		// by a moment; make sure the group is gone before the port is reused.
		killGroup(inst.cmd)
		inst.cmd = nil
	}
	if inst.hostLog != nil {
		_ = inst.hostLog.Close()
		inst.hostLog = nil
	}
	for _, svc := range inst.services {
		svc.stop()
	}
	inst.services = nil
	return nil
}
