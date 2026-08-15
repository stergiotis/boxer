package assignments_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The repo-wide half of the assignment goldens (ADR-0183 D1). A registry can
// only see the vocabularies its own binary links, and no binary links them
// all; the committed tables can be read together by anything, which is what
// the checks here do.

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "walked past the filesystem root without finding go.mod")
		dir = parent
	}
}

// A golden nobody wrote is a check nobody runs. Every package that builds a
// version-controlled vocabulary must carry one, so a sixth vocabulary cannot
// join the shared table without joining the union above.
func TestEveryVcsManagedVocabularyHasAGolden(t *testing.T) {
	root := repoRoot(t)
	vcsContract := regexp.MustCompile(`contract\.NewVcsManagedContract\(\)`)

	var missing []string
	var found int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if !vcsContract.Match(raw) {
			return nil
		}
		found++
		dir := filepath.Dir(path)
		if _, serr := os.Stat(assignments.GoldenPath(dir)); serr != nil {
			missing = append(missing, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, found, "found no vcs-managed vocabulary at all — the check would prove nothing")
	assert.Empty(t, missing,
		"these packages mint version-controlled ids without a committed assignment table (%s)", assignments.GoldenBaseName)
}
