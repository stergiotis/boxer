package changelogindex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlug(t *testing.T) {
	// Anchors verified against GitHub's rendering of the entries.
	assert.Equal(t, "go-127-adr-0199--v0020", Slug("Go 1.27 (ADR-0199) — v0.0.20"))
	assert.Equal(t, "play", Slug("play"))
	assert.Equal(t,
		"leeway-canonical-record-form-adr-0201-canonical-wire-adr-0210-and-recordstore",
		Slug("leeway: canonical record form (ADR-0201), canonical wire (ADR-0210), and recordstore"))
	assert.Equal(t, "a_b-c", Slug("a_b c"), "underscores survive")
	assert.Equal(t, "backticks-drop", Slug("`backticks` drop"))
}

func TestIsMachineryHeading(t *testing.T) {
	assert.True(t, IsMachineryHeading("Coverage and continuation"))
	assert.True(t, IsMachineryHeading("Proposed, not built — and built, not accepted"),
		"dashed variants match by prefix")
	assert.True(t, IsMachineryHeading("Reading surface added with the code"))
	assert.False(t, IsMachineryHeading("Go 1.27 (ADR-0199) — v0.0.20"))
	assert.False(t, IsMachineryHeading("New: recordstore"))
}

func TestParseTopicsSkipsFencesAndMachinery(t *testing.T) {
	topics := ParseTopics("# Title\n\n## Breaking changes\n\n## real topic\n\n```sh\n## not a heading\n```\n\n## another\n")
	require.Len(t, topics, 2)
	assert.Equal(t, "real topic", topics[0].Text)
	assert.Equal(t, "another", topics[1].Text)
}

func TestCollectEntriesNewestFirst(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07-01--2026-07-14.md"), []byte("## old topic\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07-14--2026-07-28.md"), []byte("## new topic\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("## not an entry\n"), 0o644))
	entries, err := CollectEntries(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "only window-named files are entries")
	assert.Equal(t, "2026-07-14 – 2026-07-28", entries[0].Window)
	assert.Equal(t, "2026-07-01 – 2026-07-14", entries[1].Window)
}

// TestIndexIsCurrent gates doc/changelog/INDEX.md against the entries it
// indexes: Render is deterministic, so a new or edited entry that is not
// followed by a regeneration fails here.
func TestIndexIsCurrent(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "doc", "changelog")
	require.NoError(t, Check(dir, filepath.Join(dir, "INDEX.md")),
		"regenerate with `boxer gov changelogindex` (or go generate ./public/gov/changelogindex)")
}
