package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// writeCompVault materialises a vault and points the corpus at it for the
// duration of the test.
func writeCompVault(t *testing.T, files map[string]string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	capmapcorpus.SetVaultForTest(t, root)
}

func sampleVault() map[string]string {
	return map[string]string{
		"analytics/competence.md": "---\nname: Analytics\nlevel: 1\ndomain: boxer-toolbelt\ncatalog: boxer\n---\n\n" +
			"# Vision and Scope\n\nroot prose\n",
		"analytics/robustness.md": "---\nname: Robustness\nabbrev: Rob\nlevel: 2\ndomain: boxer-toolbelt\ncatalog: boxer\n" +
			"owner: Platform Lead\nmaturity: 3\npain: 0\nparent_ids:\n  - \"[[analytics]]\"\n" +
			"realised_by: [adr:0168, adr:0009, pkg:public/gov/capmapcorpus, howto:markdown-facts-obsidian-queries, adr:9999]\n" +
			"assessment:\n" +
			"  - {dimension: readiness, rating: 7, basis: [adr:0168, positioning:gov], by: maintainer, at: 2026-10-26}\n" +
			"  - {dimension: pain, rating: 1, basis: []}\n---\n\n" +
			"# Activities\n\nlinks [[analytics]] and [[nowhere]]\n\n# Standards\n\ncites [[Jouppi-1990]]\n",
	}
}

// snapshotRows materialises a provider and reports its row count, which is the
// cheapest assertion that the provider is wired and reading.
func snapshotRows(t *testing.T, p introspect.Provider) (n int64) {
	t.Helper()
	batch, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer batch.Release()
	return batch.NumRows()
}

// Off-repo the tables are empty rather than absent or erroring: a binary with
// no checkout around it has no corpus, and that is a fact about the process.
func TestCapmapProvidersAreEmptyWithoutAVault(t *testing.T) {
	capmapcorpus.SetVaultForTest(t, filepath.Join(t.TempDir(), "does-not-exist"))
	for _, p := range []introspect.Provider{
		competenceProvider{}, competencesectionProvider{}, competencerelationProvider{},
		competenceclaimProvider{}, competenceevidenceProvider{},
	} {
		assert.Zero(t, snapshotRows(t, p), "%s must be empty with no vault", p.Name())
	}
}

func TestCapmapProvidersReadTheVault(t *testing.T) {
	writeCompVault(t, sampleVault())
	assert.Equal(t, int64(2), snapshotRows(t, competenceProvider{}), "one row per competence")
	assert.Equal(t, int64(3), snapshotRows(t, competencesectionProvider{}), "one row per body section")
	// parent + two body links from Activities + one citation from Standards,
	// five realised_by anchors and two basis anchors.
	assert.Equal(t, int64(4+5+2), snapshotRows(t, competencerelationProvider{}), "one row per relation")
	assert.Equal(t, int64(2), snapshotRows(t, competenceclaimProvider{}), "one row per claim")
}

// The claims and evidence tables derive their columns against the real
// checkout this test runs in: the resolver finds it from the working
// directory, so the anchors the fixture names are the tree's own.
func TestCapmapClaimsAndEvidenceReadTheTree(t *testing.T) {
	writeCompVault(t, sampleVault())
	installAnchorResolver()

	claims, err := competenceclaimProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer claims.Release()
	byDim := map[string]map[string]string{}
	for i := 0; i < int(claims.NumRows()); i++ {
		row := map[string]string{}
		for c, f := range claims.Schema().Fields() {
			row[f.Name] = claims.Column(c).ValueStr(i)
		}
		byDim[row["dimension"]] = row
	}
	require.Contains(t, byDim, "readiness")
	assert.Equal(t, "7", byDim["readiness"]["rating"])
	assert.Equal(t, "7", byDim["readiness"]["rank"])
	assert.Equal(t, "2", byDim["readiness"]["basis_count"])
	assert.Equal(t, "[]", byDim["readiness"]["problems"], "the basis names an accepted decision and a positioning page the tree has")
	assert.Contains(t, byDim["pain"]["problems"], "empty-basis")
	// rung 2 needs an accepted decision (adr:0168 and adr:0009 are), 3 a
	// package with default-lane tests, 4 a surface the fixture lacks — so
	// the anchors support rung 3 beside the claimed 7.
	assert.Equal(t, "3", byDim["readiness"]["supported_rung"])

	evidence, err := competenceevidenceProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer evidence.Release()
	signals := map[string]string{}
	anchorIdx, signalIdx, valueIdx := -1, -1, -1
	for c, f := range evidence.Schema().Fields() {
		switch f.Name {
		case "anchor":
			anchorIdx = c
		case "signal":
			signalIdx = c
		case "value":
			valueIdx = c
		}
	}
	for i := 0; i < int(evidence.NumRows()); i++ {
		signals[evidence.Column(anchorIdx).ValueStr(i)+"|"+evidence.Column(signalIdx).ValueStr(i)] = evidence.Column(valueIdx).ValueStr(i)
	}
	assert.Equal(t, "accepted", signals["adr:0168|decision"])
	assert.Equal(t, "unresolved", signals["adr:9999|anchored"])
	assert.Equal(t, "default", signals["pkg:public/gov/capmapcorpus|tests"])
	assert.Equal(t, "howto", signals["howto:markdown-facts-obsidian-queries|documented"])
}

// The schema is the provider's contract: a pane resolves columns from it, so a
// rename is a breaking change and worth pinning.
func TestCapmapSchemasCarryTheExpectedColumns(t *testing.T) {
	for _, tc := range []struct {
		p    introspect.Provider
		want []string
	}{
		{competenceProvider{}, []string{"slug", "name", "domain", "catalog", "level", "maturity", "pain", "tags", "fact_id", "claim_count", "dimensions"}},
		{competencesectionProvider{}, []string{"slug", "ordinal", "heading", "bytes", "words", "text@text/markdown"}},
		{competencerelationProvider{}, []string{"source_slug", "target", "kind", "resolution", "target_status", "section", "ncd", "source_fact_id", "target_fact_id"}},
		{competenceclaimProvider{}, []string{"slug", "dimension", "rating", "rank", "rung_name", "supported_rung", "basis", "basis_count", "by", "at", "problems"}},
		{competenceevidenceProvider{}, []string{"slug", "anchor", "signal", "value", "rung"}},
	} {
		got := map[string]struct{}{}
		for _, f := range tc.p.Schema().Fields() {
			got[f.Name] = struct{}{}
		}
		for _, name := range tc.want {
			_, ok := got[name]
			assert.Truef(t, ok, "%s must expose a %q column", tc.p.Name(), name)
		}
	}
}

// `words` is a size channel a reader picks by name, so what it counts has to be
// stated rather than inferred from a number: whitespace-separated tokens over
// the section's markdown source, punctuation and link syntax included. That is
// the prototype's definition — the non-empty elements of `splitByWhitespace`
// over the concatenated prose — restated where the prose is still per-section,
// so a SUM over the sections of a competence gives the same total.
func TestCapsectionWordsCountsWhitespaceTokens(t *testing.T) {
	writeCompVault(t, map[string]string{
		"analytics/competence.md": "---\nname: Analytics\nlevel: 1\n---\n\n" +
			"# Vision and Scope\n\none two  three\nfour\n",
	})
	batch, err := (competencesectionProvider{}).Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer batch.Release()
	require.Equal(t, int64(1), batch.NumRows())
	idx := batch.Schema().FieldIndices("words")
	require.Len(t, idx, 1)
	// Runs of whitespace collapse and a newline is whitespace, so the four
	// tokens count four however the source happens to be wrapped.
	assert.EqualValues(t, 4, batch.Column(idx[0]).(interface{ Value(int) int64 }).Value(0))
}

// The media-typed name is the whole point of the convention: a pane that knows
// it renders the cell as markdown rather than as a wall of text.
func TestCapsectionTextDeclaresItsMediaType(t *testing.T) {
	var found bool
	for _, f := range (competencesectionProvider{}).Schema().Fields() {
		if f.Name == "text@text/markdown" {
			found = true
		}
	}
	assert.True(t, found, "the section body must declare text/markdown in its column name (ADR-0123 §SD2)")
}

// All three read one snapshot, so a query joining them cannot see two
// different vaults.
func TestCapmapProvidersAreLive(t *testing.T) {
	for _, p := range []introspect.Provider{competenceProvider{}, competencesectionProvider{}, competencerelationProvider{}} {
		assert.Equal(t, introspect.FreshnessLive, p.Freshness(), "%s", p.Name())
	}
}
