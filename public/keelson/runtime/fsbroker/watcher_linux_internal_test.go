//go:build linux

package fsbroker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectInotify gathers the watcher's events for d.
func collectInotify(w *inotifyWatcher, d time.Duration) (names []string) {
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			names = append(names, ev.Kind.String()+" "+ev.Name)
		case <-deadline:
			return
		}
	}
}

// settle gives the watcher's poll loop time to read what the kernel queued.
func settle() { time.Sleep(150 * time.Millisecond) }

// TestInotify_Recursive_SubtreeMovedOutStopsReporting: a directory moved out
// of the granted tree takes its descendants' watches with it, so files made
// there afterwards are never named to the app.
func TestInotify_Recursive_SubtreeMovedOutStopsReporting(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755))
	w, err := newInotifyWatcher(root, true)
	require.NoError(t, err)
	require.NoError(t, w.Start())
	defer w.Stop()

	require.NoError(t, os.Rename(filepath.Join(root, "sub"), filepath.Join(base, "private")))
	settle()
	require.NoError(t, os.WriteFile(filepath.Join(base, "private", "deep", "secret-name.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "private", "outside.txt"), []byte("x"), 0o644))
	names := collectInotify(w, 300*time.Millisecond)
	assert.Contains(t, names, "renameFrom sub")
	for _, n := range names {
		assert.NotContains(t, n, "secret-name", "a file outside the grant was named: %v", names)
		assert.NotContains(t, n, "outside", "a file outside the grant was named: %v", names)
	}
}

// TestInotify_Recursive_RenamedSubdirReportsUnderNewName: a subdirectory
// renamed inside the tree keeps reporting, and its descendants report under
// the new path rather than the old one.
func TestInotify_Recursive_RenamedSubdirReportsUnderNewName(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755))
	w, err := newInotifyWatcher(root, true)
	require.NoError(t, err)
	require.NoError(t, w.Start())
	defer w.Stop()

	require.NoError(t, os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "renamed")))
	settle()
	require.NoError(t, os.WriteFile(filepath.Join(root, "renamed", "a.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "renamed", "deep", "b.txt"), []byte("x"), 0o644))
	names := collectInotify(w, 300*time.Millisecond)
	assert.Contains(t, names, "create renamed/a.txt")
	assert.Contains(t, names, "create renamed/deep/b.txt")
	for _, n := range names {
		assert.False(t, strings.HasPrefix(n, "create sub/"), "stale path reported: %v", names)
	}
}
