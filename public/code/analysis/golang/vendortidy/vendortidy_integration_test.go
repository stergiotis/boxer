//go:build integration

package vendortidy

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stretchr/testify/require"
)

// TestBoxerIsAFixpoint stages boxer's committed tree as the only module of a
// vendored workspace and requires the computed go.mod and go.sum to equal the
// committed ones, which CI keeps tidy (ADR-0384 verification plan). Needs git
// and a module cache or network for `go work vendor`.
func TestBoxerIsAFixpoint(t *testing.T) {
	top, err := extbin.Git.Output(t.Context(), extbin.Opts{}, "rev-parse", "--show-toplevel")
	require.NoError(t, err)
	repo := strings.TrimSpace(string(top))

	root := t.TempDir()
	mod := filepath.Join(root, "boxer")
	extractHead(t, repo, mod)

	goVersion := readGoLine(t, filepath.Join(mod, "go.mod"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte("go "+goVersion+"\n\nuse ./boxer\n"), 0o644))
	t.Setenv("GOWORK", filepath.Join(root, "go.work"))
	t.Setenv("GOFLAGS", "")
	out, err := extbin.Go.CombinedOutput(t.Context(), extbin.Opts{Dir: root}, "work", "vendor", "-e")
	require.NoError(t, err, string(out))

	ws, err := LoadWorkspace(filepath.Join(root, "go.work"), testGoRoot(t))
	require.NoError(t, err)
	res, err := ws.Tidy(mod)
	require.NoError(t, err)
	require.False(t, res.HasUnresolved(), "%v", res.Problems)

	oldMod, err := os.ReadFile(filepath.Join(mod, "go.mod"))
	require.NoError(t, err)
	oldSum, err := os.ReadFile(filepath.Join(mod, "go.sum"))
	require.NoError(t, err)
	newMod, err := FormatGoMod(oldMod, res)
	require.NoError(t, err)
	require.Equal(t, string(oldMod), string(newMod))
	idx, err := ws.LoadSums()
	require.NoError(t, err)
	newSum, _, err := MergeGoSum(oldSum, oldMod, res, idx)
	require.NoError(t, err)
	require.Equal(t, string(oldSum), string(newSum))
}

func extractHead(t *testing.T, repo string, dest string) {
	t.Helper()
	archive, err := extbin.Git.Output(t.Context(), extbin.Opts{}, "-C", repo, "archive", "--format=tar", "HEAD")
	require.NoError(t, err)
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		require.NoError(t, err)
		p := filepath.Join(dest, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			require.NoError(t, os.MkdirAll(p, 0o755))
		case tar.TypeReg:
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			f, ferr := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777)
			require.NoError(t, ferr)
			_, err = io.Copy(f, tr)
			require.NoError(t, err)
			require.NoError(t, f.Close())
		case tar.TypeSymlink:
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.Symlink(h.Linkname, p))
		}
	}
}

func readGoLine(t *testing.T, goMod string) string {
	t.Helper()
	b, err := os.ReadFile(goMod)
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "go "); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatal("no go line")
	return ""
}
