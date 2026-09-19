//go:build unix

package scene

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHost writes a shell script standing in for the imzero2 host. It records
// its pid and the pid of a child it spawns — the Rust client's stand-in — so a
// test can ask afterwards whether either survived.
func fakeHost(t *testing.T, body string) (host, pids string, opts Options) {
	t.Helper()
	dir := t.TempDir()
	pids = filepath.Join(dir, "pids")
	host = filepath.Join(dir, "host.sh")
	script := "#!/bin/sh\nsleep 300 &\necho $$ $! > " + pids + "\n" + body + "\n"
	require.NoError(t, os.WriteFile(host, []byte(script), 0o755))
	client := filepath.Join(dir, "client")
	require.NoError(t, os.WriteFile(client, []byte("not mesh-only"), 0o755))
	return host, pids, Options{
		Name: "fake", OutDir: filepath.Join(dir, "out"), HostBinary: host,
		RepoRoot: dir, ClientBinary: client, Timeout: 2 * time.Second,
	}
}

func alive(t *testing.T, pidsFile string) (n int) {
	t.Helper()
	b, err := os.ReadFile(pidsFile)
	require.NoError(t, err)
	for _, f := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(f)
		require.NoError(t, err)
		if syscall.Kill(pid, 0) == nil {
			n++
		}
	}
	return n
}

func TestLaunchReapsAHostThatNeverListens(t *testing.T) {
	// The failure the copied harnesses leaked on: the host is up, the carrier
	// never answers, the run gives up — and the host and its client stay.
	_, pids, opts := fakeHost(t, "sleep 300")
	start := time.Now()
	s, err := Launch(Spec{Launch: "x"}, opts)
	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "did not come up")
	assert.Less(t, time.Since(start), 20*time.Second)
	assert.Eventually(t, func() bool { return alive(t, pids) == 0 }, 5*time.Second, 50*time.Millisecond,
		"the host and the child it spawned are both gone")
}

func TestLaunchGivesUpAtOnceWhenTheHostDies(t *testing.T) {
	_, pids, opts := fakeHost(t, "exit 3")
	opts.Timeout = 30 * time.Second
	start := time.Now()
	_, err := Launch(Spec{Launch: "x"}, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited before its carrier came up")
	assert.Less(t, time.Since(start), 10*time.Second, "does not wait out the timeout")
	assert.Eventually(t, func() bool { return alive(t, pids) == 0 }, 5*time.Second, 50*time.Millisecond,
		"the orphaned child is reaped through the process group")
}

func TestHostEnvOrderLetsASceneOverride(t *testing.T) {
	t.Setenv("DISPLAY", ":0")
	env := hostEnv(Spec{Env: map[string]string{"IMZERO2_HEADLESS_FPS": "60", "X": "1"}, SQLEnv: "MY_SQL"},
		Options{OutDir: "/o", Name: "n", SQL: "SELECT 1"}, 4000, 800, 600, map[string]string{"TILES": "u"})
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	assert.NotContains(t, got, "DISPLAY", "a scene never reaches for a display")
	assert.Equal(t, "60", got["IMZERO2_HEADLESS_FPS"], "the scene's env wins over the runner's")
	assert.Equal(t, "127.0.0.1:4000", got["IMZERO2_HEADLESS_LISTEN"])
	assert.Equal(t, "800x600", got["IMZERO2_SCREENSHOT_SIZE"])
	assert.Equal(t, "SELECT 1", got["MY_SQL"])
	assert.Equal(t, "u", got["TILES"])
}

func TestChooseClient(t *testing.T) {
	root := t.TempDir()
	mesh := filepath.Join(root, "mesh")
	require.NoError(t, os.WriteFile(mesh, []byte("… "+meshOnlyMarker+" …"), 0o755))
	_, err := chooseClient(root, mesh, []string{NeedRaster})
	require.Error(t, err, "a mesh-only client cannot capture")
	got, err := chooseClient(root, mesh, nil)
	require.NoError(t, err)
	assert.Equal(t, mesh, got)
	_, err = chooseClient(root, mesh, []string{"gpu"})
	require.Error(t, err, "an unknown capability is a typo, not a pass")

	// Always fatal: a client older than the generated interpreter sources.
	gen := filepath.Join(root, "rust", "imzero2", "src", "imzero2")
	require.NoError(t, os.MkdirAll(gen, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gen, "interpreter.rs"), nil, 0o644))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(mesh, old, old))
	_, err = chooseClient(root, mesh, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "older than the generated")
}

func TestCheckRequireRejectsWhatItDoesNotKnow(t *testing.T) {
	_, err := CheckRequire("clickhose")
	require.Error(t, err, "a typo must not read as a precondition that holds")
	_, err = CheckRequire(RequireTablePrefix + "x; DROP TABLE y")
	require.Error(t, err)
	_, err = CheckRequire(RequireExePrefix + "surely-not-a-declared-program")
	require.Error(t, err, "only programs the extbin registry declares can be required")
	unmet, err := CheckRequire(RequireExePrefix + "go")
	require.NoError(t, err)
	assert.Empty(t, unmet, "the go toolchain is what runs this test")
}
