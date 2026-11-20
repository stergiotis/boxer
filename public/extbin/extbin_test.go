package extbin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The override variables the fixtures below honour, declared as every
// program's override is; resolution reads them through the registry.
var (
	fakeOverride = env.NewPath(env.Spec{
		Name:        "BOXER_FAKE_OV",
		Description: "test fixture: path override of the boxer-fake-ov program",
		Category:    env.CategoryTestIntegration,
	})
	resolveOverride = env.NewPath(env.Spec{
		Name:        "BOXER_RESOLVE_OV",
		Description: "test fixture: path override of the boxer-resolve-absent-xyz program",
		Category:    env.CategoryTestIntegration,
	})
	// Two Sensitive fixtures: one no program declares, one a fixture keeps.
	_ = env.NewString(env.Spec{
		Name:        "EXTBIN_TEST_SECRET",
		Description: "test fixture: a secret no child keeps",
		Category:    env.CategoryTestIntegration,
		Sensitive:   true,
	})
	_ = env.NewString(env.Spec{
		Name:        "EXTBIN_TEST_KEPT",
		Description: "test fixture: a secret one fixture program keeps",
		Category:    env.CategoryTestIntegration,
		Sensitive:   true,
	})
)

// fakeBin writes an executable named name into dir that prints marker to stdout
// (ignoring its args), and returns dir. Tests prepend dir to PATH. Skips on
// non-POSIX hosts, where the shell shim doesn't apply.
func fakeBin(t *testing.T, dir, name, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary shim is a POSIX shell script")
	}
	script := "#!/bin/sh\nprintf '%s' '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func prependPATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestHost_Output_ResolvesFromPath(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "boxer-fake-host", "hello-host")
	prependPATH(t, dir)

	p := &Program{Name: "boxer-fake-host", Kind: Host}
	out, err := p.Output(context.Background(), Opts{})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "hello-host" {
		t.Errorf("stdout = %q, want %q", out, "hello-host")
	}
}

func TestHost_NotFound_CarriesInstallHint(t *testing.T) {
	p := &Program{Name: "boxer-definitely-absent-xyz", Kind: Host, InstallHint: "brew install xyz"}
	_, err := p.Output(context.Background(), Opts{})
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if got := ebtest.Fields(t, err)["installHint"]; got != "brew install xyz" {
		t.Errorf("error should carry the install hint; got %v", got)
	}
}

func TestHost_OverrideEnv_WinsOverPath(t *testing.T) {
	// A binary of the program's Name on PATH prints the wrong marker; the
	// OverrideEnv path prints the right one and must win.
	pathDir := t.TempDir()
	fakeBin(t, pathDir, "boxer-fake-ov", "from-path")
	prependPATH(t, pathDir)

	ovDir := t.TempDir()
	fakeBin(t, ovDir, "override-bin", "from-override")
	fakeOverride.SetForTest(t, filepath.Join(ovDir, "override-bin"))

	p := &Program{Name: "boxer-fake-ov", Kind: Host, OverrideEnv: fakeOverride.Spec().Name}
	out, err := p.Output(context.Background(), Opts{})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "from-override" {
		t.Errorf("OverrideEnv should win; got %q", out)
	}
}

func TestExplicitPath_WinsOverEverything(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "explicit-bin", "from-explicit")
	explicit := filepath.Join(dir, "explicit-bin")

	// Host program whose Name isn't on PATH; Opts.Path must still run it.
	p := &Program{Name: "boxer-absent", Kind: Host}
	out, err := p.Output(context.Background(), Opts{Path: explicit})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "from-explicit" {
		t.Errorf("explicit Path should be used; got %q", out)
	}
}

func TestLocal_RequiresPath(t *testing.T) {
	p := &Program{Name: "some-built-artifact", Kind: Local}
	_, err := p.Command(context.Background(), Opts{})
	if err == nil {
		t.Fatal("Local without Opts.Path should error")
	}
	if !strings.Contains(err.Error(), "Path") {
		t.Errorf("error should mention the required Path; got: %v", err)
	}
}

func TestLocal_RunsExplicitPath(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "artifact", "built-output")
	p := &Program{Name: "some-built-artifact", Kind: Local}
	out, err := p.Output(context.Background(), Opts{Path: filepath.Join(dir, "artifact")})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "built-output" {
		t.Errorf("stdout = %q, want %q", out, "built-output")
	}
}

func TestGoTool_FallsBackToPathBinary(t *testing.T) {
	// A GoTool whose module tool isn't declared: `go tool <name>` fails fast
	// (in a non-module dir), so the PATH binary of the same name is used.
	dir := t.TempDir()
	name := "boxer-fake-gotool"
	fakeBin(t, dir, name, "from-path-fallback")
	prependPATH(t, dir)

	p := &Program{Name: name, Kind: GoTool}
	// t.TempDir is outside any Go module, so `go tool <name>` returns
	// "no such tool" immediately.
	out, err := p.Output(context.Background(), Opts{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Output with GoTool fallback: %v", err)
	}
	if string(out) != "from-path-fallback" {
		t.Errorf("stdout = %q, want fallback output", out)
	}
}

func TestGoTool_NoFallback_ErrorNamesPath(t *testing.T) {
	p := &Program{Name: "boxer-absent-gotool-xyz", Kind: GoTool}
	_, err := p.Output(context.Background(), Opts{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when neither go tool nor PATH binary resolves")
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("error should note the missing PATH fallback; got: %v", err)
	}
}

func TestCommand_AppliesDirAndEnv(t *testing.T) {
	p := &Program{Name: "boxer-fake", Kind: Host}
	cmd, err := p.Command(context.Background(), Opts{Path: "/bin/true", Dir: "/tmp", Env: []string{"A=B"}})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if cmd.Dir != "/tmp" {
		t.Errorf("cmd.Dir = %q, want /tmp", cmd.Dir)
	}
	if len(cmd.Env) != 1 || cmd.Env[0] != "A=B" {
		t.Errorf("cmd.Env = %v, want [A=B]", cmd.Env)
	}
}

func TestDeclare_RejectsDuplicateAndEmpty(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		fn()
	}
	mustPanic("empty name", func() { Declare(Program{Name: ""}) })

	Declare(Program{Name: "boxer-test-unique-decl", Kind: Host})
	mustPanic("duplicate", func() { Declare(Program{Name: "boxer-test-unique-decl", Kind: Host}) })
}

func TestRegistry_IncludesDeclaredProgramsSorted(t *testing.T) {
	progs := Registry()
	if len(progs) == 0 {
		t.Fatal("registry is empty")
	}
	var names []string
	for _, p := range progs {
		names = append(names, p.Name)
	}
	// Sorted.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("registry not sorted at %d: %q > %q", i, names[i-1], names[i])
		}
	}
	// The manifest's cornerstone programs are present.
	for _, want := range []string{"git", "go", "scc", "clickhouse local"} {
		if !slices.Contains(names, want) {
			t.Errorf("registry missing declared program %q", want)
		}
	}
}

func TestKindString(t *testing.T) {
	cases := map[Kind]string{Host: "host", GoTool: "gotool", GoToolchain: "gotoolchain", Local: "local", Kind(99): "unknown"}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestResolve_HostAndLocal(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "boxer-resolve-host", "x")
	prependPATH(t, dir)

	// Host present on PATH → resolved path + available.
	host := &Program{Name: "boxer-resolve-host", Kind: Host}
	path, ok := host.Resolve()
	if !ok || path != filepath.Join(dir, "boxer-resolve-host") {
		t.Errorf("Host resolve = (%q, %v), want the PATH binary + true", path, ok)
	}

	// Host absent → ("", false).
	absent := &Program{Name: "boxer-resolve-absent-xyz", Kind: Host}
	if path, ok := absent.Resolve(); ok || path != "" {
		t.Errorf("absent Host resolve = (%q, %v), want ('', false)", path, ok)
	}

	// Local needs a caller path → ("", false).
	local := &Program{Name: "some-artifact", Kind: Local}
	if path, ok := local.Resolve(); ok || path != "" {
		t.Errorf("Local resolve = (%q, %v), want ('', false)", path, ok)
	}

	// OverrideEnv wins.
	resolveOverride.SetForTest(t, "/opt/custom/tool")
	ov := &Program{Name: "boxer-resolve-absent-xyz", Kind: Host, OverrideEnv: resolveOverride.Spec().Name}
	if path, ok := ov.Resolve(); !ok || path != "/opt/custom/tool" {
		t.Errorf("override resolve = (%q, %v), want ('/opt/custom/tool', true)", path, ok)
	}
}

// A multi-call program (`clickhouse local`) must prepend its subcommand to
// every invocation — including when the caller supplies an explicit path, which
// names the multi-call binary and not the subcommand. Getting that wrong is
// silent: the process starts and misreads the first real argument.
func TestArgvPrefixIsPrepended(t *testing.T) {
	p := &Program{Name: "multicall probe", Argv: []string{"probe", "sub"}, Kind: Host}
	cmd, err := p.Command(context.Background(), Opts{Path: "/opt/probe"}, "--query", "SELECT 1")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{"/opt/probe", "sub", "--query", "SELECT 1"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("argv = %q, want %q", cmd.Args, want)
	}
	// The declaration must not be reachable through the returned argv.
	if !slices.Equal(p.Argv, []string{"probe", "sub"}) {
		t.Errorf("Argv mutated to %q", p.Argv)
	}
}

func TestCheckMinVersion(t *testing.T) {
	p := &Program{Name: "probe-test", MinVersion: "26.8", MinVersionNeeds: "a feature"}
	v, err := checkMinVersion(p, "ClickHouse local version 26.9.13.15 (official build).\n")
	if err != nil || v != "26.9.13.15" {
		t.Fatalf("got %q, %v", v, err)
	}
	if _, err = checkMinVersion(p, "26.8"); err != nil {
		t.Fatalf("the floor itself passes: %v", err)
	}
	if _, err = checkMinVersion(p, "version 26.7.9"); err == nil || !strings.Contains(err.Error(), "below the floor") {
		t.Fatalf("below the floor must be refused: %v", err)
	}
	if _, err = checkMinVersion(p, "no version here"); err == nil {
		t.Fatal("an output without a version must be refused")
	}
}

func TestCheckVersionWithoutProbePasses(t *testing.T) {
	p := &Program{Name: "no-probe", Kind: Local}
	if _, err := p.CheckVersion(t.Context(), Opts{}); err != nil {
		t.Fatalf("a program without a probe is not checked: %v", err)
	}
}

func TestClickHouseLocalMeetsTheFloor(t *testing.T) {
	if _, ok := ClickHouseLocal.Resolve(); !ok {
		t.Skip("clickhouse not on PATH")
	}
	v, err := ClickHouseLocal.CheckVersion(t.Context(), Opts{})
	if err != nil {
		t.Fatalf("clickhouse local on PATH: %v", err)
	}
	if v == "" {
		t.Fatal("the probe reported no version")
	}
}

func TestChildEnvDropsUndeclaredSecrets(t *testing.T) {
	base := []string{"PATH=/bin", "EXTBIN_TEST_SECRET=s3cret", "EXTBIN_TEST_KEPT=k3pt", "EXTBIN_TEST_PLAIN=x"}
	plain := &Program{Name: "plain"}
	out := plain.childEnv(base)
	assert.NotContains(t, out, "EXTBIN_TEST_SECRET=s3cret")
	assert.NotContains(t, out, "EXTBIN_TEST_KEPT=k3pt")
	assert.Contains(t, out, "PATH=/bin")
	assert.Contains(t, out, "EXTBIN_TEST_PLAIN=x")

	keeps := &Program{Name: "keeps", InheritSensitive: []string{"EXTBIN_TEST_KEPT"}}
	out = keeps.childEnv(base)
	assert.NotContains(t, out, "EXTBIN_TEST_SECRET=s3cret")
	assert.Contains(t, out, "EXTBIN_TEST_KEPT=k3pt")

	host := &Program{Name: "host", InheritAllSensitive: true}
	assert.Equal(t, base, host.childEnv(base))
}

func TestChildEnvStripsTheInheritedEnvironment(t *testing.T) {
	t.Setenv("EXTBIN_TEST_SECRET", "s3cret")
	out := (&Program{Name: "plain"}).childEnv(nil)
	require.NotNil(t, out)
	assert.NotContains(t, out, "EXTBIN_TEST_SECRET=s3cret")
}
