//go:build !(js || wasip1)

package goccyengine_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const envTmpDirChild = "GOCCYENGINE_TMPDIR_CHILD"

// TestGraphvizCacheIsPerUser re-executes the test binary with TMPDIR unset and
// a private cache home: go-graphviz's init must have put its compilation cache
// under that cache home rather than the machine-wide /tmp, and TMPDIR must be
// unset again once initialization is over.
func TestGraphvizCacheIsPerUser(t *testing.T) {
	if os.Getenv(envTmpDirChild) != "" {
		_, set := os.LookupEnv("TMPDIR")
		require.False(t, set, "TMPDIR leaked past initialization")
		return
	}
	cacheHome := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestGraphvizCacheIsPerUser$")
	env := []string{envTmpDirChild + "=1", "XDG_CACHE_HOME=" + cacheHome}
	for _, kv := range os.Environ() {
		if len(kv) >= 7 && kv[:7] == "TMPDIR=" {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.DirExists(t, filepath.Join(cacheHome, "boxer", "goccyengine", "go-graphviz"))
}
