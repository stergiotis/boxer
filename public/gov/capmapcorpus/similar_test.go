package capmapcorpus_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
)

const similarNote = `---
name: "Access Control"
abbrev: AC
level: 3
parent_ids:
  - "[[identity]]"
maturity: 255
pain: 255
---

# Vision and Scope

Who may do what, and [[gdpr]] says so.

# Standards
`

// The stanza is added under the existing keys, in the vault's own spelling, and
// the body — which is the part a person wrote — comes back byte for byte.
func TestUpsertSimilarAddsTheStanzaAndKeepsTheBody(t *testing.T) {
	out, changed, err := capmapcorpus.UpsertSimilar([]byte(similarNote), []capmapcorpus.SimilarEntry{
		{Target: "authorization", Ncd: 0.31234, Qualified: true},
		{Target: "audit-logging", Ncd: 0.4},
	})
	require.NoError(t, err)
	assert.True(t, changed)
	text := string(out)
	assert.Contains(t, text, "similar:\n  - ref: \"[[authorization/capability]]\"\n    ncd: 0.3123\n  - ref: \"[[audit-logging]]\"\n    ncd: 0.4000\n---")
	_, body, _ := strings.Cut(text, "\n---\n")
	_, wantBody, _ := strings.Cut(similarNote, "\n---\n")
	assert.Equal(t, wantBody, body, "the body is not the writer's to touch")
	assert.Less(t, strings.Index(text, "name:"), strings.Index(text, "parent_ids:"), "key order is preserved")
	assert.Less(t, strings.Index(text, "pain:"), strings.Index(text, "similar:"), "the new key goes last")
}

// A second run replaces the stanza rather than stacking a second one, and an
// unchanged result is reported as such so a caller can skip the write.
func TestUpsertSimilarReplacesAndIsIdempotent(t *testing.T) {
	entries := []capmapcorpus.SimilarEntry{{Target: "authorization", Ncd: 0.3}}
	once, _, err := capmapcorpus.UpsertSimilar([]byte(similarNote), entries)
	require.NoError(t, err)
	twice, changed, err := capmapcorpus.UpsertSimilar(once, entries)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, string(once), string(twice))
	assert.Equal(t, 1, strings.Count(string(twice), "similar:"))

	replaced, changed, err := capmapcorpus.UpsertSimilar(once, []capmapcorpus.SimilarEntry{{Target: "other", Ncd: 0.2}})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotContains(t, string(replaced), "authorization")
	assert.Contains(t, string(replaced), "[[other]]")
}

// No neighbours means no stanza: a note whose resemblances all fell under the
// threshold on a re-run must not keep last run's list.
func TestUpsertSimilarRemovesAnEmptyStanza(t *testing.T) {
	once, _, err := capmapcorpus.UpsertSimilar([]byte(similarNote), []capmapcorpus.SimilarEntry{{Target: "x", Ncd: 0.1}})
	require.NoError(t, err)
	out, changed, err := capmapcorpus.UpsertSimilar(once, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotContains(t, string(out), "similar")

	same, changed, err := capmapcorpus.UpsertSimilar([]byte(similarNote), nil)
	require.NoError(t, err)
	assert.False(t, changed, "nothing to remove, nothing to write")
	assert.Equal(t, similarNote, string(same))
}

// What the parser reads out of an edited note is the original competence plus
// the similar relations — which is the property the writer exists to keep, and
// the reason it edits the tree rather than re-rendering the stanza.
func TestUpsertSimilarRoundTripsThroughTheParser(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write("identity/capability.md", "---\nname: Identity\nlevel: 2\nparent_ids: []\n---\n\n# Vision and Scope\n\nWho.\n")
	write("identity/access-control.md", similarNote)
	write("identity/authorization/capability.md", "---\nname: Authorization\nlevel: 3\nparent_ids:\n  - \"[[identity]]\"\n---\n\n# Vision and Scope\n\nMay.\n")
	before, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)

	path := filepath.Join(root, "identity/access-control.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	out, _, err := capmapcorpus.UpsertSimilar(content, []capmapcorpus.SimilarEntry{{Target: "authorization", Ncd: 0.25, Qualified: true}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0o644))

	after, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)
	require.Equal(t, len(before.Competences), len(after.Competences))
	for i := range before.Competences {
		assert.Equal(t, before.Competences[i], after.Competences[i], "competence %s changed", before.Competences[i].Slug)
	}
	var similar []capmapcorpus.Relation
	for _, r := range after.Relations {
		if r.Kind == capmapcorpus.RelationKindSimilar {
			similar = append(similar, r)
		}
	}
	require.Len(t, similar, 1)
	assert.Equal(t, "access-control", similar[0].SourceSlug)
	assert.Equal(t, "authorization", similar[0].Target)
	assert.Equal(t, capmapcorpus.ResolutionDirect, similar[0].Resolution, "the qualified spelling resolves to the competence")
	assert.Equal(t, 0.25, similar[0].Ncd)
	assert.Equal(t, len(before.Relations)+1, len(after.Relations))
}

func TestUpsertSimilarRefusesANoteWithoutFrontmatter(t *testing.T) {
	_, _, err := capmapcorpus.UpsertSimilar([]byte("# Just a heading\n"), nil)
	assert.Error(t, err)
	_, _, err = capmapcorpus.UpsertSimilar([]byte("---\nname: x\n"), nil)
	assert.Error(t, err, "no closing delimiter")
}

func TestDirectoryBacked(t *testing.T) {
	assert.True(t, capmapcorpus.Competence{VaultPath: "identity/capability.md"}.DirectoryBacked())
	assert.True(t, capmapcorpus.Competence{VaultPath: "capability.md"}.DirectoryBacked())
	assert.False(t, capmapcorpus.Competence{VaultPath: "identity/access-control.md"}.DirectoryBacked())
	assert.False(t, capmapcorpus.Competence{VaultPath: "identity/notcapability.md"}.DirectoryBacked())
}
