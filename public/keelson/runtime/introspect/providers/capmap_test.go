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
			"owner: Platform Lead\nmaturity: 3\npain: 0\nparent_ids:\n  - \"[[analytics]]\"\n---\n\n" +
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
	for _, p := range []introspect.Provider{competenceProvider{}, competencesectionProvider{}, competencerelationProvider{}} {
		assert.Zero(t, snapshotRows(t, p), "%s must be empty with no vault", p.Name())
	}
}

func TestCapmapProvidersReadTheVault(t *testing.T) {
	writeCompVault(t, sampleVault())
	assert.Equal(t, int64(2), snapshotRows(t, competenceProvider{}), "one row per competence")
	assert.Equal(t, int64(3), snapshotRows(t, competencesectionProvider{}), "one row per body section")
	// parent + two body links from Activities + one citation from Standards.
	assert.Equal(t, int64(4), snapshotRows(t, competencerelationProvider{}), "one row per relation")
}

// The schema is the provider's contract: a pane resolves columns from it, so a
// rename is a breaking change and worth pinning.
func TestCapmapSchemasCarryTheExpectedColumns(t *testing.T) {
	for _, tc := range []struct {
		p    introspect.Provider
		want []string
	}{
		{competenceProvider{}, []string{"slug", "name", "domain", "catalog", "level", "maturity", "pain", "tags", "fact_id"}},
		{competencesectionProvider{}, []string{"slug", "ordinal", "heading", "bytes", "words", "text@text/markdown"}},
		{competencerelationProvider{}, []string{"source_slug", "target", "kind", "resolution", "section", "ncd", "source_fact_id", "target_fact_id"}},
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
