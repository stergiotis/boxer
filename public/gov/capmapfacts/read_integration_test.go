//go:build integration

package capmapfacts_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapfacts"
)

// The read-back half of the ingest gate. `boxer capmap dump` writes a vault
// from what these queries return, so what they get wrong is what a dump would
// silently lose — and none of it can be caught without a server, since the
// decode is SQL over physical leeway columns.

// What was written comes back as what was written. This is the property the
// whole dump rests on: the store is only a safe place to keep a corpus if the
// corpus can be recovered from it.
func TestReadCorpusRoundTripsTheEncoding(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()

	ctx := context.Background()
	want := sampleCorpus()
	_, err := capmapfacts.Ingest(ctx, want, cli, qualified, fixedNow)
	require.NoError(t, err)

	got, err := capmapfacts.ReadCorpus(ctx, cli, qualified)
	require.NoError(t, err)

	capmapcorpus.SortCorpus(&want)
	assert.Equal(t, want.Competences, got.Competences)
	assert.Equal(t, want.Relations, got.Relations)
}

// A re-ingest restates entities under the same ids by design, so the table
// holds one row per competence per ingest. A dump must emit each competence
// once, and it must be the newest one.
func TestReadCorpusTakesTheNewestIngest(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()

	ctx := context.Background()
	first := sampleCorpus()
	_, err := capmapfacts.Ingest(ctx, first, cli, qualified, fixedNow)
	require.NoError(t, err)

	second := sampleCorpus()
	for i := range second.Competences {
		if second.Competences[i].Slug == "robustness" {
			second.Competences[i].Maturity = 5
			second.Competences[i].Tags = []string{"reviewed"}
		}
	}
	_, err = capmapfacts.Ingest(ctx, second, cli, qualified, fixedNow.Add(time.Hour))
	require.NoError(t, err)

	got, err := capmapfacts.ReadCorpus(ctx, cli, qualified)
	require.NoError(t, err)
	require.Len(t, got.Competences, 2, "each competence once, however many ingests wrote it")
	for _, comp := range got.Competences {
		if comp.Slug != "robustness" {
			continue
		}
		assert.Equal(t, uint8(5), comp.Maturity, "the newest ingest wins")
		assert.Equal(t, []string{"reviewed"}, comp.Tags)
	}
}

// The end-to-end shape the CLI verbs are: a vault loaded into the facts table
// and dumped back out is the same vault.
//
// It compares the parsed model rather than the bytes — YAML emission style is
// not what the corpus means — and it is the only test that exercises load and
// dump against each other rather than each against a fixture.
func TestVaultSurvivesLoadAndDump(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()

	src := writeIntegrationVault(t)
	want, err := capmapcorpus.ParseDir(src)
	require.NoError(t, err)
	require.Len(t, want.Competences, 3)

	ctx := context.Background()
	_, err = capmapfacts.Ingest(ctx, want, cli, qualified, fixedNow)
	require.NoError(t, err)

	stored, err := capmapfacts.ReadCorpus(ctx, cli, qualified)
	require.NoError(t, err)

	out := t.TempDir()
	_, err = capmapcorpus.WriteVault(stored, out)
	require.NoError(t, err)

	got, err := capmapcorpus.ParseDir(out)
	require.NoError(t, err)

	capmapcorpus.SortCorpus(&want)
	capmapcorpus.SortCorpus(&got)
	assert.Equal(t, want.Competences, got.Competences)
	assert.Equal(t, want.Relations, got.Relations)
}

// writeIntegrationVault materialises the fixture the load/dump round trip runs
// on: a directory-backed parent, a leaf carrying the whole frontmatter surface,
// and a multi-parent building block.
func writeIntegrationVault(t *testing.T) (root string) {
	t.Helper()
	files := map[string]string{
		"boxer/analytics/capability.md": "---\n" +
			"name: Analytics\nabbrev: Ana\nsynopsis: counting things\n" +
			"level: 1\nparent_ids: []\ndomain: boxer-toolbelt\ncatalog: boxer\nmaturity: 255\npain: 255\n" +
			"---\n\n# Vision and Scope\n\nroot prose linking [[robustness]]\n",
		"boxer/analytics/robustness.md": "---\n" +
			"name: Robustness\nabbrev: Rob\nlevel: 2\nparent_ids:\n  - \"[[analytics]]\"\n" +
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
	root = t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}
