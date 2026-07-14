package extbin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
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
	if !strings.Contains(err.Error(), "brew install xyz") {
		t.Errorf("error should carry the install hint; got: %v", err)
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
	t.Setenv("BOXER_FAKE_OV", filepath.Join(ovDir, "override-bin"))

	p := &Program{Name: "boxer-fake-ov", Kind: Host, OverrideEnv: "BOXER_FAKE_OV"}
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
	for _, want := range []string{"git", "go", "scc", "clickhouse-local"} {
		if !slices.Contains(names, want) {
			t.Errorf("registry missing declared program %q", want)
		}
	}
}
