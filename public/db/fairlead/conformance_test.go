package fairlead

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// unledFiles are the files that match the exit-point shapes below and send no
// statement. Each entry says why; an entry without a reason does not belong.
var unledFiles = map[string]string{
	"public/keelson/runtime/queryrunsvc/reconcile.go": "asks a pull service's /healthz, which takes no statement",
}

var (
	reLocalProcess = regexp.MustCompile(`ClickHouseLocal\.Command\(|exec\.Command(Context)?\([^)]*"clickhouse"`)
	reHttpRequest  = regexp.MustCompile(`http\.NewRequest(WithContext)?\(`)
	reServerMarker = regexp.MustCompile(`(?i)clickhouse|default_format|query_id`)
)

// TestEveryExitPointLeads holds ADR-0367 §SD1 mechanically: a file that
// starts a query process, or builds an HTTP request in a file that talks to
// the database server, leads its statement through this package.
//
// The check is textual and per file. It finds an exit point added without the
// call; it does not prove that every request in a matching file is led, and a
// statement sent through a transport it has no pattern for is invisible to
// it.
func TestEveryExitPointLeads(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	seenAllowed := make(map[string]struct{}, len(unledFiles))
	for _, top := range []string{"public", "apps"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(src)
			if strings.HasPrefix(text, "//go:build ignore") {
				return nil
			}
			exits := reLocalProcess.MatchString(text) ||
				(reHttpRequest.MatchString(text) && reServerMarker.MatchString(text))
			if !exits {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.Contains(text, "fairlead.Lead(") {
				return nil
			}
			if _, allowed := unledFiles[rel]; allowed {
				seenAllowed[rel] = struct{}{}
				return nil
			}
			offenders = append(offenders, rel)
			return nil
		})
		require.NoError(t, err)
	}
	require.Empty(t, offenders, "these files send statements without fairlead.Lead (ADR-0367 §SD1)")
	for rel, reason := range unledFiles {
		require.NotEmpty(t, reason, rel)
		_, still := seenAllowed[rel]
		require.True(t, still, "%s no longer matches an exit-point shape; drop it from unledFiles", rel)
	}
}

func repoRoot(t *testing.T) (root string) {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			root = dir
			return
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above the test's directory")
		dir = parent
	}
}
