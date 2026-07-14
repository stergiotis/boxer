package scctree

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover RunScc's scc resolution (the `go tool scc` → PATH `scc`
// fallback). They lean on a shell shim, so they skip on non-POSIX hosts, and
// they run scc against a temp dir that sits outside any Go module — there,
// `go tool scc` fails fast ("no such tool"), which is exactly the condition
// that hands off to the PATH fallback.

// writeFakeScc drops an executable named scc into dir that ignores its args
// and prints a fixed scc-style JSON document.
func writeFakeScc(t *testing.T, dir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		`printf '%s' '[{"Name":"Go","Files":[{"Language":"Go","Filename":"fake.go","Location":"fake.go","Code":42,"Complexity":7}]}]'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "scc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake scc: %v", err)
	}
}

func TestRunScc_FallsBackToPathScc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake scc shim is a POSIX shell script")
	}
	binDir := t.TempDir()
	writeFakeScc(t, binDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// t.TempDir sits under the OS temp root, outside the repo's module, so the
	// primary `go tool scc` misses and the fallback runs our shim.
	groups, err := RunScc(t.TempDir())
	if err != nil {
		t.Fatalf("RunScc with PATH fallback: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != 1 {
		t.Fatalf("unexpected groups shape: %+v", groups)
	}
	if got := groups[0].Files[0].Code; got != 42 {
		t.Errorf("fallback scc output not parsed: Code=%d want 42", got)
	}
}

func TestRunScc_NoSccResolvable_ErrorNamesPathFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX PATH semantics")
	}
	// Resolve `go` before narrowing PATH so the primary attempt can still run
	// (and fail with "no such tool"); with no scc anywhere, the error should
	// call out the absent PATH fallback.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go not on PATH: %v", err)
	}
	t.Setenv("PATH", filepath.Dir(goBin))

	_, err = RunScc(t.TempDir())
	if err == nil {
		t.Fatal("expected an error when neither go tool scc nor PATH scc is available")
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("error should mention the missing PATH fallback; got: %v", err)
	}
}
