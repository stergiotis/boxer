package tabhost

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The flags bundle builds with are the ones go-build-env.sh gives every other
// shipped binary (ADR-0215); this test is what keeps the two definitions one.
func TestBuildFlagsMatchTheShellEnv(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "scripts", "dev", "go-build-env.sh"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^BOXER_GO_FLAGS="([^"]*)"`).FindSubmatch(b)
	require.NotNil(t, m, "go-build-env.sh sets BOXER_GO_FLAGS")
	require.Equal(t, strings.Fields(string(m[1])), goBuildFlags)
	for _, kv := range goBuildEnv {
		require.Regexp(t, regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(kv)+`$`), string(b))
	}
}

// The module's tags and toolchain pin are read as the shell env reads them.
func TestModuleTagsAndToolchain(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, "", readTags(dir))
	require.Equal(t, "", goModVersion(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tags"), []byte("a,b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme\n\ngo 1.27.0\n\nrequire x v1\n"), 0o644))
	require.Equal(t, "a,b", readTags(dir))
	require.Equal(t, "1.27.0", goModVersion(dir))
}

// copyFile onto the same file — `bundle --host` naming the host already in the
// output directory, directly or through a link — keeps the file; it used to
// truncate it before reading it, and the tab failed on an empty module.
func TestCopyFileOntoItselfKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "imzero2_browser.wasm")
	want := []byte("\x00asm host bytes")
	require.NoError(t, os.WriteFile(host, want, 0o644))
	link := filepath.Join(dir, "linked.wasm")
	require.NoError(t, os.Symlink(host, link))

	require.NoError(t, copyFile(host, host))
	require.NoError(t, copyFile(link, host))
	got, err := os.ReadFile(host)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// An ordinary copy replaces dst whole, leaves no temporary file behind, and
// gives the copy the mode the bundle's other files have.
func TestCopyFileReplacesTheDestination(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("an older, longer file"), 0o644))
	require.NoError(t, copyFile(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	info, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "no temporary file is left")

	require.Error(t, copyFile(filepath.Join(dir, "missing"), dst))
	got, err = os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "new", string(got), "a failed copy leaves dst as it was")
}
