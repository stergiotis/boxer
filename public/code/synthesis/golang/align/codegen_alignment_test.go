package align_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stergiotis/boxer/public/code/synthesis/golang/align"
	"github.com/stretchr/testify/require"
)

// TestAlignAndFormatHeadOutGoIsAligned asserts that every committed
// .out.go file produced by leeway-style generators is in
// betteralign-optimal layout at git HEAD. Running AlignAndFormat on the
// HEAD bytes is expected to be a no-op; if it isn't, the most recent
// commit drifted from `./scripts/dev/betteralign.sh` output.
//
// Reads HEAD content rather than the working tree so the check is robust
// to in-progress changes (between a generator-test run and a
// `betteralign.sh` run).
func TestAlignAndFormatHeadOutGoIsAligned(t *testing.T) {
	moduleRoot := findRepoRoot(t)
	tags, err := align.FindModuleBuildTags(moduleRoot)
	require.NoError(t, err)

	targets := []string{
		"public/semistructured/leeway/dml/example/dml_testtable.out.go",
		"public/semistructured/leeway/dml/example/dml_json.out.go",
		"public/semistructured/leeway/readaccess/example/readaccess_testtable_ra.out.go",
		"public/semistructured/leeway/readaccess/example/readaccess_testtable_dml.out.go",
		"public/semistructured/leeway/common/lw_system_table_columns_dml.out.go",
	}
	for _, rel := range targets {
		t.Run(rel, func(t *testing.T) {
			cmd := exec.Command("git", "show", "HEAD:"+rel)
			cmd.Dir = moduleRoot
			src, err := cmd.Output()
			if err != nil {
				t.Skipf("git show failed (file not at HEAD?): %v", err)
			}
			abs := filepath.Join(moduleRoot, rel)
			out, err := align.AlignAndFormat(src, abs, tags)
			require.NoError(t, err)
			require.Equal(t, string(src), string(out),
				"%s at HEAD is not in betteralign-optimal order", rel)
		})
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("no go.mod found above %s", d)
		}
		d = parent
	}
}

// WriteAligned regenerates sources that live in PACKAGE directories — several
// callers write into a sibling package — so it runs while the rest of the
// suite may be compiling that package. The replacement must therefore be
// atomic: a concurrent reader sees the old file or the new one, never a
// missing or half-written one.
//
// It was neither. The write truncated in place, and one caller unlinked the
// target first, leaving it absent for the whole align-and-format pass; a
// concurrent `go build` of the affected package failed 6 times in 12 attempts,
// surfacing as an unrelated-looking capslock failure under `go test ./...`.
func TestWriteAlignedReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	// FindModuleBuildTags walks up for a go.mod; without one WriteAligned
	// cannot resolve build tags. No `tags` file means the empty tag set.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module scratch\n\ngo 1.24\n"), 0o644))
	path := filepath.Join(dir, "gen.go")
	src := []byte("package scratch\n\nconst Sentinel = 1\n")
	require.NoError(t, align.WriteAligned(path, src))

	stop := make(chan struct{})
	var missing, partial atomic.Int64
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, rerr := os.ReadFile(path)
			switch {
			case rerr != nil:
				missing.Add(1)
			case !bytes.Contains(b, []byte("Sentinel")):
				partial.Add(1)
			}
		}
	})
	for range 60 {
		require.NoError(t, align.WriteAligned(path, src))
	}
	close(stop)
	wg.Wait()

	require.Zero(t, missing.Load(), "the target was observed absent mid-replacement")
	require.Zero(t, partial.Load(), "the target was observed half-written")

	// And nothing is left behind beside it.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp", "a temp file survived the replacement")
	}
}

// The mode of an existing target survives replacement: os.WriteFile applied its
// perm only when creating, so regeneration never changed it, and some
// checked-in generated sources are 0755. A fresh temp file would silently
// normalise them and show up as repo-wide drift.
func TestWriteAlignedPreservesMode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module scratch\n\ngo 1.24\n"), 0o644))
	path := filepath.Join(dir, "gen.go")
	src := []byte("package scratch\n\nconst Sentinel = 1\n")

	require.NoError(t, align.WriteAligned(path, src))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), fi.Mode().Perm(), "a new file gets the default mode")

	require.NoError(t, os.Chmod(path, 0o755))
	require.NoError(t, align.WriteAligned(path, src))
	fi, err = os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "an existing file keeps its mode")
}
