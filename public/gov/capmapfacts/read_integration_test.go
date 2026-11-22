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
	"github.com/stergiotis/boxer/public/identity/identifier"
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
	_, err := capmapfacts.Ingest(ctx, want, cli, qualified, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, liveIds(t, cli))
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
	_, err := capmapfacts.Ingest(ctx, first, cli, qualified, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, liveIds(t, cli))
	require.NoError(t, err)

	second := sampleCorpus()
	for i := range second.Competences {
		if second.Competences[i].Slug == "robustness" {
			second.Competences[i].Maturity = 5
			second.Competences[i].Tags = []string{"reviewed"}
		}
	}
	_, err = capmapfacts.Ingest(ctx, second, cli, qualified, fixedNow.Add(time.Hour), capmapfacts.Origin{Run: "run-capmap"}, liveIds(t, cli))
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
	_, err = capmapfacts.Ingest(ctx, want, cli, qualified, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, liveIds(t, cli))
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
			"realised_by: [adr:0223, pkg:public/streaming/watchbill]\n" +
			"assessment:\n" +
			"  - {dimension: readiness, rating: 6, basis: [adr:0223, howto:watchbill-jobs], by: maintainer, at: 2026-10-26}\n" +
			"  - {dimension: evolution, rating: product, basis: [adr:0223], by: maintainer, at: 2026-10-26}\n" +
			"  - {dimension: pain, rating: 1, basis: []}\n" +
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

// legacyIds gives every natural key the id rows carried before ids were
// resolved through the facts table's generator.
type legacyIds struct{}

func (legacyIds) GetId(_ context.Context, key []byte) (identifier.TaggedId, bool, error) {
	return identifier.TaggedId(capmapfacts.LegacyId(key)), false, nil
}
func (legacyIds) GetUntaggedId(_ context.Context, key []byte) (identifier.UntaggedId, bool, error) {
	return identifier.TaggedId(capmapfacts.LegacyId(key)).RemoveTag(), false, nil
}
func (legacyIds) Release() error           { return nil }
func (legacyIds) GetTag() identifier.IdTag { return identifier.IdTag(0) }

// A table that holds a corpus from before ids were resolved, and then a load
// since: each competence reads once, as the newest load wrote it, although
// its two rows carry two ids; and a relation the newer load no longer names
// still finds its source, by the id its era gave it (ADR-0405 §SD5).
func TestReadCorpusSpansRowsFromBeforeIdsWereResolved(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()
	ctx := context.Background()

	old := sampleCorpus()
	_, err := capmapfacts.Ingest(ctx, old, cli, qualified, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, legacyIds{})
	require.NoError(t, err)
	got, err := capmapfacts.ReadCorpus(ctx, cli, qualified)
	require.NoError(t, err)
	capmapcorpus.SortCorpus(&old)
	require.Equal(t, old.Relations, got.Relations, "a table of the old era alone reads as it did")

	newer := sampleCorpus()
	for i := range newer.Competences {
		if newer.Competences[i].Slug == "robustness" {
			newer.Competences[i].Maturity = 5
		}
	}
	dropped := newer.Relations[len(newer.Relations)-1]
	newer.Relations = newer.Relations[:len(newer.Relations)-1]
	_, err = capmapfacts.Ingest(ctx, newer, cli, qualified, fixedNow.Add(time.Hour), capmapfacts.Origin{Run: "run-capmap"}, liveIds(t, cli))
	require.NoError(t, err)

	got, err = capmapfacts.ReadCorpus(ctx, cli, qualified)
	require.NoError(t, err)
	require.Len(t, got.Competences, 2, "each competence once, under either id")
	for _, comp := range got.Competences {
		if comp.Slug == "robustness" {
			assert.Equal(t, uint8(5), comp.Maturity, "the newest load wins across the two eras")
		}
	}
	require.Len(t, got.Relations, len(old.Relations), "the relation only the old load named is still there")
	found := false
	for _, rel := range got.Relations {
		if rel.SourceSlug == dropped.SourceSlug && rel.Target == dropped.Target && rel.Kind == dropped.Kind && rel.Section == dropped.Section {
			found = true
		}
	}
	assert.True(t, found, "and its source resolves through the id it was written with")
}
