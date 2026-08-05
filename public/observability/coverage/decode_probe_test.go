package coverage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The probe is a tiny self-contained module (two packages, functions with
// known covered and never-executed paths) built with the live toolchain to
// produce real meta/counter blobs. It backs both the committed fixtures
// (TestRegenFixtures) and the integration-lane drift guard.
const probeGoMod = `module covprobe

go 1.26
`

const probeMain = `package main

import "covprobe/inner"

func alpha(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

func beta(b bool) int {
	if b {
		return 1
	}
	return 2
}

func neverCalled() int {
	return 42
}

func main() {
	v := alpha(10) + beta(false) + inner.Used(3)
	if v < 0 {
		_ = neverCalled()
		_ = inner.Unused()
	}
}
`

const probeInner = `package inner

func Used(x int) int {
	return x * 2
}

func Unused() int {
	return -1
}
`

// buildAndRunProbe builds the probe with -cover -covermode=atomic using the
// toolchain on PATH, runs it with GOCOVERDIR, and returns the emitted meta
// and counter blobs plus the emission directory (for covdata oracles).
// GOWORK/GOFLAGS are neutralized so the ambient workspace and tag pins do
// not leak into the throwaway module.
func buildAndRunProbe(t *testing.T) (metaData []byte, counterData []byte, coverDir string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(probeGoMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(probeMain), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "inner"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inner", "inner.go"), []byte(probeInner), 0o644))

	probeBin := filepath.Join(dir, "probe")
	build := exec.Command("go", "build", "-cover", "-covermode=atomic", "-o", probeBin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build -cover failed: %s", out)

	coverDir = filepath.Join(dir, "covdata")
	require.NoError(t, os.MkdirAll(coverDir, 0o755))
	run := exec.Command(probeBin)
	run.Dir = dir
	run.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)
	out, err = run.CombinedOutput()
	require.NoError(t, err, "probe run failed: %s", out)

	metaData = readSoleGlob(t, coverDir, "covmeta.*")
	counterData = readSoleGlob(t, coverDir, "covcounters.*")
	return
}

func readSoleGlob(t *testing.T, dir string, pattern string) (data []byte) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	require.NoError(t, err)
	require.Len(t, matches, 1, "expected exactly one %s file", pattern)
	data, err = os.ReadFile(matches[0])
	require.NoError(t, err)
	return
}

// TestRegenFixtures rewrites the committed fixture blobs with the current
// toolchain. Gated on BOXER_COVDECODE_REGEN=1 (the pprofarrow precedent);
// run it after a toolchain bump and commit the result.
func TestRegenFixtures(t *testing.T) {
	if os.Getenv("BOXER_COVDECODE_REGEN") == "" {
		t.Skip("set BOXER_COVDECODE_REGEN=1 to regenerate the committed fixtures with the current toolchain")
	}
	metaData, counterData, _ := buildAndRunProbe(t)
	require.NoError(t, os.MkdirAll("testdata", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("testdata", "covmeta.bin"), metaData, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("testdata", "covcounters.bin"), counterData, 0o644))
	t.Logf("regenerated fixtures: covmeta.bin %d bytes, covcounters.bin %d bytes", len(metaData), len(counterData))
}
