package assignments_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The stream half of the union (ADR-0319 §SD2): a tag value claimed for an
// id stream is one id space with every other claim in its space, so within
// that space it must be no other stream's and no vocabulary's.
func TestStreamTagValuesAreDisjointFromEveryOtherClaim(t *testing.T) {
	streams, err := assignments.FindStreamGoldens(repoRoot(t))
	require.NoError(t, err)
	require.NotEmpty(t, streams, "no stream goldens found — the check would prove nothing")
	for path, rows := range streams {
		require.NotEmpty(t, rows, "%s is empty", path)
	}
	for _, c := range assignments.CheckTagValuesDisjoint(allGoldens(t), streams) {
		t.Error(c)
	}
}

// The union keys by (space, value): one value in two named spaces passes, the
// same value twice in one space — the default or a named one — does not.
func TestUnionKeysByIdSpaceAndValue(t *testing.T) {
	vocab := map[string][]assignments.Assignment{
		"vocab/testdata/" + assignments.GoldenBaseName: {{Ordinal: 1, Name: "a", Id: identifier.TagValue(2178400).GetTag().ComposeId(1).Value()}},
	}
	stream := func(name string, tv uint32, sp string) assignments.StreamClaim {
		return assignments.StreamClaim{Name: name, TagValue: tv, Width: 32, MaxExpectedIds: 1, Space: sp}
	}

	ok := map[string][]assignments.StreamClaim{
		"kid/testdata/" + assignments.StreamGoldenBaseName: {stream("kid/cellA", 2178401, "kid")},
		"eid/testdata/" + assignments.StreamGoldenBaseName: {stream("eid/cellA", 2178401, "eid"), stream("eid/vocabValue", 2178400, "eid")},
	}
	assert.Empty(t, assignments.CheckTagValuesDisjoint(vocab, ok))

	defaultDup := map[string][]assignments.StreamClaim{
		"s/testdata/" + assignments.StreamGoldenBaseName: {stream("streamOnVocabValue", 2178400, "")},
	}
	assert.Len(t, assignments.CheckTagValuesDisjoint(vocab, defaultDup), 1, "a stream on a vocabulary's value in the default space collides")

	streamDup := map[string][]assignments.StreamClaim{
		"s/testdata/" + assignments.StreamGoldenBaseName: {stream("one", 2178402, ""), stream("two", 2178402, "")},
	}
	assert.Len(t, assignments.CheckTagValuesDisjoint(nil, streamDup), 1, "two streams on one value in the default space collide")

	namedDup := map[string][]assignments.StreamClaim{
		"a/testdata/" + assignments.StreamGoldenBaseName: {stream("eid/cellA", 2178401, "eid")},
		"b/testdata/" + assignments.StreamGoldenBaseName: {stream("eid/cellB", 2178401, "eid")},
	}
	assert.Len(t, assignments.CheckTagValuesDisjoint(nil, namedDup), 1, "two claims of one value in one named space collide")
}

// A golden names a claim's space as a fifth field, and omits it for the
// default space, so goldens without named spaces read and render as before.
func TestStreamGoldenCarriesANamedSpaceOnly(t *testing.T) {
	plain := []assignments.StreamClaim{{Name: "a", TagValue: 46368, Width: 24, MaxExpectedIds: 10}}
	text := assignments.RenderStreams(plain)
	assert.Contains(t, text, "# name\ttagValue\twidth\tmaxExpectedIds\n")
	assert.Contains(t, text, "\na\t46368\t24\t10\n")

	mixed := append(plain, assignments.StreamClaim{Name: "b", TagValue: 46368, Width: 24, MaxExpectedIds: 10, Space: "eid"})
	text = assignments.RenderStreams(mixed)
	assert.Contains(t, text, "\tmaxExpectedIds\tspace\n")
	assert.Contains(t, text, "\na\t46368\t24\t10\n")
	assert.Contains(t, text, "\nb\t46368\t24\t10\teid\n")
	back, err := assignments.ParseStreams(text)
	require.NoError(t, err)
	assert.Equal(t, mixed, back)

	moved := []assignments.StreamClaim{{Name: "b", TagValue: 46368, Width: 24, MaxExpectedIds: 10, Space: "kid"}}
	diff := assignments.DiffStreams(mixed[1:], moved)
	require.Len(t, diff, 1)
	assert.Contains(t, diff[0], "id space")
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
	claimCall := regexp.MustCompile(`tagmint\.MustClaim\("|tagmint\.MustClaimIn\(\w+(\.\w+)?,\s*"`)
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
