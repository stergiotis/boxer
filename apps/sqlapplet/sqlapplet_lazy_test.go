package sqlapplet

import (
	"maps"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mintedDef opens a minted applet and returns the def its factory serves.
func mintedDef(t *testing.T, reg *app.Registry, slug string) (def *AppletDef) {
	t.Helper()
	a, err := reg.Open(app.AppIdT(appletIdPrefix + slug))
	require.NoError(t, err)
	inst, ok := a.(*appletApp)
	require.True(t, ok)
	def = inst.def
	return
}

// Minting parses no SQL: a buffer that does not parse still mints, and the
// parse error arrives when the applet is opened.
func TestMintDefersClassification(t *testing.T) {
	fsys := fstest.MapFS{
		"reader.md": docMD("title: Reader", sqlFence),
		"broken.md": docMD("title: Broken", "```sql\nINSERT INTO t VALUES (1)\n```"),
	}
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "t", fsys: fsys, topics: []app.TopicT{app.TopicRuntime}}})
	require.Empty(t, errs)
	assert.Equal(t, 2, minted, "a buffer is not looked at when minting")

	reader := mintedDef(t, reg, "reader")
	require.NotNil(t, reader.pending, "classification is pending after mint")
	assert.Equal(t, analysis.QuerySecurityMutating, reader.Class, "pending reads as the conservative class")

	cfg := EmbedConfig{Log: zerolog.Nop(), EndpointURL: "http://example.invalid/query"}
	pa, err := NewEmbedded(reader, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { pa.Close() })
	assert.Equal(t, analysis.QuerySecurityRead, reader.Class)
	assert.True(t, pa.AutoRun, "a read-class applet still runs on open")

	broken := mintedDef(t, reg, "broken")
	_, err = NewEmbedded(broken, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	_, again := NewEmbedded(broken, cfg)
	assert.Equal(t, err, again, "the classification error is computed once and kept")
}

// The lazy mint path must arrive at the def the eager one does, for every
// committed applet, and the frontmatter it reads without parsing the markdown
// must be what a full parse of the same document reports.
func TestLazyParseMatchesEager(t *testing.T) {
	booksMu.Lock()
	snapshot := append([]registeredBook(nil), books...)
	booksMu.Unlock()
	require.NotEmpty(t, snapshot)
	docs := 0
	for _, b := range snapshot {
		lazy, lerrs := parseBookLazy(b.id, b.fsys)
		require.Empty(t, lerrs, b.id)
		for _, def := range lazy {
			docs++
			full := markdown.Parse(def.Source).Frontmatter()
			fm, err := markdown.ParseFrontmatter(def.Source)
			require.NoError(t, err, "%s/%s", b.id, def.Slug)
			assert.Equal(t, kvPairs(full), kvPairs(fm), "%s/%s: frontmatter", b.id, def.Slug)

			eager, eerr := ParseDocSource(b.id, def.Slug+".md", def.Source)
			require.NoError(t, eerr, "%s/%s", b.id, def.Slug)
			require.NoError(t, def.ensureClassified(), "%s/%s", b.id, def.Slug)
			got := *def
			got.pending = nil
			assert.Equal(t, *eager, got, "%s/%s", b.id, def.Slug)
		}
	}
	assert.Positive(t, docs)
}

// kvPairs flattens a frontmatter KV for comparison: the KV itself holds its
// comparators as func fields, which never compare equal.
func kvPairs(kv *containers.BinarySearchGrowingKV[string, any]) (out map[string]any) {
	if kv == nil {
		return
	}
	out = maps.Collect(kv.IteratePairs())
	return
}

// Every window of an applet shares its def, so concurrent first opens must
// classify it once and agree.
func TestEnsureClassifiedConcurrent(t *testing.T) {
	defs, errs := parseBookLazy("t", fstest.MapFS{"reader.md": docMD("title: Reader", sqlFence)})
	require.Empty(t, errs)
	require.Len(t, defs, 1)
	def := defs[0]
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, def.ensureClassified())
		}()
	}
	wg.Wait()
	assert.Equal(t, analysis.QuerySecurityRead, def.Class)
}
