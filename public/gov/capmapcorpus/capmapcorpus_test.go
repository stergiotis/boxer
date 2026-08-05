package capmapcorpus_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
)

// writeVault materialises a small vault and returns its root. Files are given
// as vault-relative paths so a test reads as the tree it describes.
func writeVault(t *testing.T, files map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

// findRelation returns the single relation matching source/target/kind.
func findRelation(t *testing.T, rels []capmapcorpus.Relation, source, target string, kind capmapcorpus.RelationKindE) (rel capmapcorpus.Relation) {
	t.Helper()
	var hits []capmapcorpus.Relation
	for _, r := range rels {
		if r.SourceSlug == source && r.Target == target && r.Kind == kind {
			hits = append(hits, r)
		}
	}
	require.Len(t, hits, 1, "expected exactly one %s relation %s -> %s", kind, source, target)
	return hits[0]
}

func TestParseDirReadsMetadataAndSections(t *testing.T) {
	root := writeVault(t, map[string]string{
		"boxer/similarity.md": `---
name: "Similarity"
abbrev: Sim
synopsis: compression-based distance
level: 2
domain: BoxerToolbelt
catalog: boxer
owner: Platform Lead
maturity: 3
pain: 0
lifecycle_defined_by: J. Smith
lifecycle_defined_at: 2026-03-15
---

# Vision and Scope

Prose about scope.

# Activities

- One thing
`,
	})
	corpus, err := capmapcorpus.ParseDir(root)
	caps := corpus.Capabilities
	require.NoError(t, err)
	require.Len(t, caps, 1)
	c := caps[0]

	assert.Equal(t, "similarity", c.Slug)
	assert.Equal(t, "Similarity", c.Name)
	assert.Equal(t, uint8(2), c.Level)
	assert.Equal(t, "boxer", c.Catalog)
	assert.Equal(t, filepath.Join("boxer", "similarity.md"), c.VaultPath)
	// The domain was written in a different style and must converge.
	assert.Equal(t, "boxer-toolbelt", c.Domain)
	// pain: 0 is a real judgement ("none") and must not read as unassessed.
	assert.Equal(t, uint8(3), c.Maturity)
	assert.Equal(t, uint8(0), c.Pain)

	require.Len(t, c.Sections, 2)
	assert.Equal(t, "Vision and Scope", c.Sections[0].Heading)
	assert.Equal(t, "Prose about scope.", c.Sections[0].Text)
	assert.Equal(t, "Activities", c.Sections[1].Heading)

	require.Len(t, c.Lifecycle, 1, "only phases carrying a record are kept")
	assert.Equal(t, capmapcorpus.PhaseDefined, c.Lifecycle[0].Phase)
	assert.Equal(t, "J. Smith", c.Lifecycle[0].By)
	assert.Equal(t, 2026, c.Lifecycle[0].At.Year())

	assert.Len(t, c.NaturalKey, 16)
	assert.Equal(t, capmapcorpus.NaturalKey("similarity"), c.NaturalKey)
}

// An absent score is not a zero score. Averaging over the sentinel would be
// wrong, so the two must stay distinguishable.
func TestParseDirAbsentScoreIsNotAssessed(t *testing.T) {
	root := writeVault(t, map[string]string{
		"a.md": "---\nname: A\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	caps := corpus.Capabilities
	require.NoError(t, err)
	require.Len(t, caps, 1)
	assert.Equal(t, capmapcorpus.NotAssessed, caps[0].Maturity)
	assert.Equal(t, capmapcorpus.NotAssessed, caps[0].Pain)
}

// A directory-backed capability takes its slug from the directory, never from
// the "capability.md" filename.
func TestParseDirDirectoryBackedSlug(t *testing.T) {
	root := writeVault(t, map[string]string{
		"analytics/capability.md": "---\nname: Analytics\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	caps := corpus.Capabilities
	require.NoError(t, err)
	require.Len(t, caps, 1)
	assert.Equal(t, "analytics", caps[0].Slug)
}

func TestParseDirResolvesRelationKinds(t *testing.T) {
	root := writeVault(t, map[string]string{
		"analytics/capability.md": "---\nname: Analytics\nlevel: 1\n---\n\n# Vision and Scope\n\nroot\n",
		"analytics/robustness.md": `---
name: Robustness
level: 2
parent_ids:
  - "[[analytics]]"
similar:
  - ref: "[[missing-thing]]"
    ncd: 0.25
---

# Activities

See [[analytics]] and [[nowhere]].

# Standards

- [[Jouppi-1990]] is a citation, not a capability.
`,
	})
	corpus, err := capmapcorpus.ParseDir(root)
	rels := corpus.Relations
	require.NoError(t, err)

	// A parent naming a directory-backed capability resolves directly: only
	// body wikilinks are subject to the Obsidian dirref problem.
	parent := findRelation(t, rels, "robustness", "analytics", capmapcorpus.RelationKindParent)
	assert.Equal(t, capmapcorpus.ResolutionDirect, parent.Resolution)

	// The same target reached from a body section is a dirref: the file is
	// analytics/capability.md, so Obsidian's [[analytics]] would dangle.
	wiki := findRelation(t, rels, "robustness", "analytics", capmapcorpus.RelationKindWikilink)
	assert.Equal(t, capmapcorpus.ResolutionDirRef, wiki.Resolution)
	assert.Equal(t, "Activities", wiki.Section, "a body link carries the section it was found under")

	// A well-formed slug nothing carries is the one genuine defect state.
	broken := findRelation(t, rels, "robustness", "nowhere", capmapcorpus.RelationKindWikilink)
	assert.Equal(t, capmapcorpus.ResolutionUnresolved, broken.Resolution)

	sim := findRelation(t, rels, "robustness", "missing-thing", capmapcorpus.RelationKindSimilar)
	assert.Equal(t, capmapcorpus.ResolutionUnresolved, sim.Resolution)
	assert.InDelta(t, 0.25, sim.Ncd, 1e-9)

	// A citation is kept verbatim and is not a broken link.
	cite := findRelation(t, rels, "robustness", "Jouppi-1990", capmapcorpus.RelationKindWikilink)
	assert.Equal(t, capmapcorpus.ResolutionExternal, cite.Resolution)
	assert.Equal(t, "Standards", cite.Section)

	broke := capmapcorpus.UnresolvedRelations(rels)
	assert.Len(t, broke, 2, "only the two well-formed-but-missing targets count as broken")
	for _, r := range broke {
		assert.NotEqual(t, "Jouppi-1990", r.Target, "citations must never appear in the broken set")
	}
}

// Both spellings of a link to a directory-backed capability name the same
// target, and only the bare one is a defect.
//
// This is a regression guard with a measured cost: before it was handled,
// roughly 800 of the 5,961 relations on the real capability tree — every link
// written the Obsidian-correct way — were reported as broken.
func TestParseDirResolvesQualifiedCapabilityLink(t *testing.T) {
	root := writeVault(t, map[string]string{
		"analytics/capability.md": "---\nname: Analytics\nlevel: 1\n---\n\n# Vision and Scope\n\nroot\n",
		"leaf.md": "---\nname: Leaf\nlevel: 2\n---\n\n# Activities\n\n" +
			"Qualified [[analytics/capability]], with display [[analytics/capability|Analytics]], and bare [[analytics]].\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)

	var qualified, bare int
	for _, r := range corpus.Relations {
		if r.SourceSlug != "leaf" || r.Kind != capmapcorpus.RelationKindWikilink {
			continue
		}
		require.Equal(t, "analytics", r.Target, "both spellings reduce to the capability's slug")
		switch r.Resolution {
		case capmapcorpus.ResolutionDirect:
			qualified++
		case capmapcorpus.ResolutionDirRef:
			bare++
		default:
			t.Fatalf("unexpected resolution %v", r.Resolution)
		}
	}
	assert.Equal(t, 2, qualified, "the two qualified links resolve cleanly")
	assert.Equal(t, 1, bare, "the bare link is the one that dangles in Obsidian")
	assert.Empty(t, capmapcorpus.UnresolvedRelations(corpus.Relations))
}

// A vault may hold notes beside its capabilities. Those are skipped and
// reported, not fatal — and not silent.
func TestParseDirSkipsFilesThatCannotBeCapabilities(t *testing.T) {
	root := writeVault(t, map[string]string{
		"similarity.md": "---\nname: Similarity\nlevel: 2\n---\n\n# Vision and Scope\n\nx\n",
		"AMQP-1.0.md":   "---\nname: \"AMQP 1.0\"\ntype: de-jure\n---\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err, "a reference note beside a capability must not fail the read")
	require.Len(t, corpus.Capabilities, 1)
	assert.Equal(t, "similarity", corpus.Capabilities[0].Slug)
	require.Len(t, corpus.Skipped, 1)
	assert.Equal(t, "AMQP-1.0.md", corpus.Skipped[0].Path)
	assert.NotEmpty(t, corpus.Skipped[0].Reason)
}

// Level-4 multi-parenting is ordinary in this model: one relation per parent.
func TestParseDirMultiParent(t *testing.T) {
	root := writeVault(t, map[string]string{
		"a.md": "---\nname: A\nlevel: 3\n---\n\n# Vision and Scope\n\nx\n",
		"b.md": "---\nname: B\nlevel: 3\n---\n\n# Vision and Scope\n\nx\n",
		"leaf.md": `---
name: Leaf
level: 4
parent_ids:
  - "[[a]]"
  - "[[b]]"
---

# Vision and Scope

x
`,
	})
	corpus, err := capmapcorpus.ParseDir(root)
	rels := corpus.Relations
	require.NoError(t, err)
	var parents int
	for _, r := range rels {
		if r.SourceSlug == "leaf" && r.Kind == capmapcorpus.RelationKindParent {
			parents++
			assert.Equal(t, capmapcorpus.ResolutionDirect, r.Resolution)
		}
	}
	assert.Equal(t, 2, parents)
}

// Slugs are the corpus's identity, so two files converging on one slug is an
// error rather than a last-one-wins overwrite.
func TestParseDirRefusesDuplicateSlug(t *testing.T) {
	root := writeVault(t, map[string]string{
		"one/adversarial-robustness.md": "---\nname: A\nlevel: 2\n---\n\n# Vision and Scope\n\nx\n",
		"two/AdversarialRobustness.md":  "---\nname: B\nlevel: 2\n---\n\n# Vision and Scope\n\nx\n",
	})
	_, err := capmapcorpus.ParseDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate capability slug")
}

// A malformed file fails the read. A vault is authored, not collected, so
// skipping one would understate the corpus with no signal.
func TestParseDirRefusesFileWithoutFrontmatter(t *testing.T) {
	root := writeVault(t, map[string]string{
		"broken.md": "# Vision and Scope\n\nno frontmatter here\n",
	})
	_, err := capmapcorpus.ParseDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frontmatter")
}

// Editor state is not corpus content.
func TestParseDirSkipsObsidianDir(t *testing.T) {
	root := writeVault(t, map[string]string{
		"a.md":                   "---\nname: A\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
		".obsidian/workspace.md": "---\nname: nope\n---\n",
		".obsidian/nested/x.md":  "---\nname: nope\n---\n",
		"notes.txt":              "not markdown",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	caps := corpus.Capabilities
	require.NoError(t, err)
	require.Len(t, caps, 1)
	assert.Equal(t, "a", caps[0].Slug)
}

// Output order must not depend on filesystem walk order.
func TestParseDirSortsBySlug(t *testing.T) {
	root := writeVault(t, map[string]string{
		"z/zebra.md":  "---\nname: Z\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
		"a/alpha.md":  "---\nname: A\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
		"m/middle.md": "---\nname: M\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	caps := corpus.Capabilities
	require.NoError(t, err)
	got := make([]string, 0, len(caps))
	for _, c := range caps {
		got = append(got, c.Slug)
	}
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, got)
}

func TestNormalizeSlugConverges(t *testing.T) {
	for _, raw := range []string{
		"AdversarialRobustness", "adversarial_robustness",
		"adversarial-robustness", "adversarialRobustness",
	} {
		got, err := capmapcorpus.NormalizeSlug(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "adversarial-robustness", got, raw)
	}
}

// Citation keys are not capability slugs, and the distinction is load-bearing:
// on the real catalog a quarter of body links are of this kind.
func TestNormalizeTargetSeparatesCitations(t *testing.T) {
	for _, raw := range []string{"Jouppi-1990", "GDPR-Art-17", "boxer-adr-0009-environment-variable-registry"} {
		got, wellFormed := capmapcorpus.NormalizeTarget(raw)
		assert.False(t, wellFormed, "%q is not a capability slug", raw)
		assert.Equal(t, raw, got, "a citation key is kept verbatim")
	}
	got, wellFormed := capmapcorpus.NormalizeTarget("Similarity_NCD")
	assert.True(t, wellFormed)
	assert.Equal(t, "similarity-ncd", got)
}

// Identity follows the slug and nothing else.
func TestNaturalKeyIsStableAndDistinct(t *testing.T) {
	a := capmapcorpus.NaturalKey("similarity")
	assert.Equal(t, a, capmapcorpus.NaturalKey("similarity"))
	assert.NotEqual(t, a, capmapcorpus.NaturalKey("similarity-ncd"))
	assert.Len(t, a, 16)
}

// Vault-location resolution and Load are exercised in the internal test file
// (capmapcorpus_env_test.go): the env handle caches on first read, so those
// tests need env.SetForTest, which resets that cache and is not reachable from
// here.
