package capmapcorpus_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
)

// roundTripVault carries every shape the renderer has to put back: a
// directory-backed parent, a leaf with the full frontmatter surface, a
// multi-parent building block, a qualified link, a citation and a dangling one.
func roundTripVault() map[string]string {
	return map[string]string{
		"boxer/analytics/capability.md": "---\n" +
			"name: Analytics\nabbrev: Ana\nsynopsis: \"counting things: carefully\"\n" +
			"level: 1\nparent_ids: []\ndomain: boxer-toolbelt\ncatalog: boxer\nmaturity: 255\npain: 255\n" +
			"---\n\n# Vision and Scope\n\nroot prose linking [[robustness]]\n",
		"boxer/analytics/robustness.md": "---\n" +
			"name: Robustness\nabbrev: Rob\nlevel: 2\nparent_ids:\n  - \"[[analytics/capability]]\"\n" +
			"domain: boxer-toolbelt\ncatalog: boxer\nowner: Platform Lead\nmaturity: 3\npain: 0\n" +
			"tags: [needs-owner, workflow/triage]\n" +
			"similar:\n  - ref: \"[[shared-block]]\"\n    ncd: 0.4548\n" +
			"lifecycle_defined_by: J. Smith\nlifecycle_defined_at: 2026-03-15\n" +
			"lifecycle_assessed_by: R. Jones\nlifecycle_assessed_at: 2026-04-01 09:30:00\n" +
			"---\n\n# Activities\n\ncites [[nowhere]] and [[Jouppi-1990]]\n\n# Standards\n\n- one\n- two\n",
		"boxer/shared-block.md": "---\n" +
			"name: Shared Block\nlevel: 4\nparent_ids:\n  - \"[[analytics]]\"\n  - \"[[robustness]]\"\n" +
			"domain: boxer-toolbelt\ncatalog: boxer\nmaturity: 255\npain: 255\n" +
			"---\n\n# Vision and Scope\n\nserves two parents\n",
	}
}

// The round trip the ADR's verification plan asks for: what [ParseDir] read,
// [WriteVault] writes, and reading that back gives the same corpus.
//
// It compares the model rather than the bytes. Byte equality would pin YAML
// emission style — quoting, key order, list indentation — which is not what the
// vault's authority rests on; a competence that comes back with the same
// metadata, prose, tags, lifecycle and links is the same competence however the
// stanza was spelled.
func TestWriteVaultRoundTripsThroughParse(t *testing.T) {
	first, err := capmapcorpus.ParseDir(writeVault(t, roundTripVault()))
	require.NoError(t, err)
	require.Len(t, first.Competences, 3)

	out := t.TempDir()
	stats, err := capmapcorpus.WriteVault(first, out)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.Files)
	assert.Equal(t, 1, stats.Directories, "only the competence with children is directory-backed")

	second, err := capmapcorpus.ParseDir(out)
	require.NoError(t, err)

	capmapcorpus.SortCorpus(&first)
	capmapcorpus.SortCorpus(&second)
	assert.Equal(t, first.Competences, second.Competences)
	assert.Equal(t, first.Relations, second.Relations)
}

// Files land where they were read from, which is what makes a dump of an
// unedited vault a no-op rather than a reorganisation.
func TestWriteVaultKeepsStoredPaths(t *testing.T) {
	corpus, err := capmapcorpus.ParseDir(writeVault(t, roundTripVault()))
	require.NoError(t, err)
	out := t.TempDir()
	_, err = capmapcorpus.WriteVault(corpus, out)
	require.NoError(t, err)

	for _, rel := range []string{
		filepath.Join("boxer", "analytics", "capability.md"),
		filepath.Join("boxer", "analytics", "robustness.md"),
		filepath.Join("boxer", "shared-block.md"),
	} {
		_, statErr := os.Stat(filepath.Join(out, rel))
		assert.NoErrorf(t, statErr, "expected %s", rel)
	}
}

// A corpus that carries no paths — one assembled by hand, or read back from a
// store that lost them — still lays out as a vault: children under parents,
// a competence with children as a directory.
func TestWriteVaultDerivesLayoutWithoutStoredPaths(t *testing.T) {
	corpus := capmapcorpus.Corpus{
		Competences: []capmapcorpus.Competence{
			{Slug: "analytics", Name: "Analytics", Catalog: "boxer", Level: 1,
				Maturity: capmapcorpus.NotAssessed, Pain: capmapcorpus.NotAssessed},
			{Slug: "robustness", Name: "Robustness", Catalog: "boxer", Level: 2,
				Maturity: capmapcorpus.NotAssessed, Pain: capmapcorpus.NotAssessed},
		},
		Relations: []capmapcorpus.Relation{
			{SourceSlug: "robustness", Target: "analytics", Kind: capmapcorpus.RelationKindParent,
				Resolution: capmapcorpus.ResolutionDirect},
		},
	}
	out := t.TempDir()
	_, err := capmapcorpus.WriteVault(corpus, out)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(out, "boxer", "analytics", "capability.md"))
	assert.NoError(t, err, "a competence with children is directory-backed")
	_, err = os.Stat(filepath.Join(out, "boxer", "analytics", "robustness.md"))
	assert.NoError(t, err, "a leaf sits beside its parent's marker file")
}

// A path column is data, and data read out of a store can say anything. A
// traversal must not become a write outside the output directory.
func TestWriteVaultIgnoresUnsafeStoredPaths(t *testing.T) {
	for _, unsafe := range []string{"../escape.md", "/etc/passwd", "boxer/notes.txt"} {
		corpus := capmapcorpus.Corpus{
			Competences: []capmapcorpus.Competence{
				{Slug: "escapee", Name: "Escapee", VaultPath: unsafe, Level: 1,
					Maturity: capmapcorpus.NotAssessed, Pain: capmapcorpus.NotAssessed},
			},
		}
		out := t.TempDir()
		_, err := capmapcorpus.WriteVault(corpus, out)
		require.NoErrorf(t, err, "%q", unsafe)

		_, statErr := os.Stat(filepath.Join(out, "escapee.md"))
		assert.NoErrorf(t, statErr, "%q must fall back to the derived path", unsafe)
		_, escaped := os.Stat(filepath.Join(filepath.Dir(out), "escape.md"))
		assert.Errorf(t, escaped, "%q must not have written outside the output directory", unsafe)
	}
}

// Two rows for one file means the corpus is inconsistent. Writing both and
// keeping the last would make the dump quietly lossy, so it is an error.
func TestWriteVaultRefusesTwoCompetencesOnOnePath(t *testing.T) {
	corpus := capmapcorpus.Corpus{
		Competences: []capmapcorpus.Competence{
			{Slug: "a", Name: "A", VaultPath: "same.md", Level: 1},
			{Slug: "b", Name: "B", VaultPath: "same.md", Level: 1},
		},
	}
	_, err := capmapcorpus.WriteVault(corpus, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, ebtest.Text(t, err), "same.md")
}

// The emitted stanza is the vault's dialect, not boxer's: the marker file keeps
// the industry's word and links keep the spelling they were written in.
func TestRenderCompetenceEmitsTheVaultsDialect(t *testing.T) {
	corpus, err := capmapcorpus.ParseDir(writeVault(t, roundTripVault()))
	require.NoError(t, err)
	var robustness capmapcorpus.Competence
	for _, c := range corpus.Competences {
		if c.Slug == "robustness" {
			robustness = c
		}
	}
	require.Equal(t, "robustness", robustness.Slug)

	out, err := capmapcorpus.RenderCompetence(robustness, corpus.Relations)
	require.NoError(t, err)
	rendered := string(out)

	assert.Contains(t, rendered, `- '[[analytics/capability]]'`, "the qualified spelling survives")
	assert.Contains(t, rendered, "maturity: 3")
	assert.Contains(t, rendered, "pain: 0", "a zero score is a judgement and is written")
	assert.Contains(t, rendered, "- needs-owner")
	assert.Contains(t, rendered, "lifecycle_defined_at: \"2026-03-15\"", "a date with no clock stays a date")
	assert.Contains(t, rendered, "lifecycle_assessed_at: \"2026-04-01 09:30:00\"")
	assert.Contains(t, rendered, "\n# Activities\n\ncites [[nowhere]] and [[Jouppi-1990]]\n",
		"the body is written back verbatim, links included")
	assert.NotContains(t, rendered, "\nid:", "ids are derived from the slug, never stored in the vault")
}
