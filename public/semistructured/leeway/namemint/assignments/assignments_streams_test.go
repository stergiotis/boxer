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

// The stream half of the union (ADR-0319 §SD2): a tag value claimed for an
// id stream is one id space, like a vocabulary's, so it must be no other
// stream's and no vocabulary's.
func TestStreamTagValuesAreDisjointFromEveryOtherClaim(t *testing.T) {
	streams, err := assignments.FindStreamGoldens(repoRoot(t))
	require.NoError(t, err)
	require.NotEmpty(t, streams, "no stream goldens found — the check would prove nothing")

	owner := map[uint32]string{}
	for path, rows := range allGoldens(t) {
		owner[assignments.TagValueOf(rows[0].Id).Value()] = path
	}
	for path, rows := range streams {
		require.NotEmpty(t, rows, "%s is empty", path)
		for _, c := range rows {
			if prev, taken := owner[c.TagValue]; taken {
				t.Errorf("tag value %d is claimed twice: %s and %s/%s", c.TagValue, prev, path, c.Name)
				continue
			}
			owner[c.TagValue] = path + "/" + c.Name
		}
	}
}

// Each committed stream sits in the class the planner advises for what it
// declared, and the width recorded is the value's own.
func TestStreamClaimsSitInTheirAdvisedClass(t *testing.T) {
	streams, err := assignments.FindStreamGoldens(repoRoot(t))
	require.NoError(t, err)
	for path, rows := range streams {
		for _, c := range rows {
			assert.NoError(t, assignments.CheckStreamClass(c), "%s/%s", path, c.Name)
		}
	}
}

// A claim nobody committed is a check nobody runs. Every non-test call of
// tagmint.MustClaim is either a vocabulary's, pinned by its package's
// membership golden, or a stream's, pinned by a row of its package's stream
// golden — so a package's call count is exactly what its goldens account
// for. A claim site names its claim as a literal, which is what the pattern
// keys on, so prose that mentions the call does not count. tagmint itself is
// exempt: its one claim reserves a value rather than minting under it.
func TestEveryTagClaimIsInAGolden(t *testing.T) {
	root := repoRoot(t)
	claimCall := regexp.MustCompile(`tagmint\.MustClaim\("`)
	calls := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && (d.Name() == "testdata" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
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
		if n := len(claimCall.FindAll(raw, -1)); n > 0 {
			calls[filepath.Dir(path)] += n
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, calls)

	for dir, n := range calls {
		if strings.HasSuffix(dir, filepath.Join("identity", "tagmint")) {
			continue
		}
		accounted := 0
		if _, serr := os.Stat(assignments.GoldenPath(dir)); serr == nil {
			accounted++
		}
		if raw, rerr := os.ReadFile(assignments.StreamGoldenPath(dir)); rerr == nil {
			rows, perr := assignments.ParseStreams(string(raw))
			require.NoError(t, perr, dir)
			accounted += len(rows)
		}
		rel, _ := filepath.Rel(root, dir)
		assert.Equal(t, n, accounted, "%s makes %d tag claims and its goldens account for %d (%s, %s)", rel, n, accounted, assignments.GoldenBaseName, assignments.StreamGoldenBaseName)
	}
}
