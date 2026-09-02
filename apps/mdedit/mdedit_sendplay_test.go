package mdedit

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// ---------------------------------------------------------------------------
// The rows
// ---------------------------------------------------------------------------

// TestSendWritesTheIngestorsRows pins that a send stores what the markdown
// ingestor stores — the document row plus its items — through the one row
// builder both share, so a document sent from here and one ingested from a
// vault read the same in play.
func TestSendWritesTheIngestorsRows(t *testing.T) {
	src := "---\ntags: [a]\n---\n# Title\n\nbody with [[Other]] and **bold**\n"
	ts := time.Unix(1_700_000_000, 0).UTC()
	rows := mddocfacts.BuildRows([]byte(src), "notes.md", ts, mdextract.Extract([]byte(src)))

	assert.Equal(t, "mdDoc", rows.Doc.Kind)
	assert.Equal(t, "Title", rows.Doc.Title, "the extractor's first heading is the title")
	assert.Equal(t, "notes.md", rows.Doc.FileName)
	assert.Equal(t, src, rows.Doc.Content)
	assert.Len(t, rows.Doc.NaturalKey, 32, "the natural key is the blake3-256 of the content")
	assert.NotZero(t, rows.Doc.Id, "the launch filter key")
	assert.Len(t, rows.Headings, 1)
	assert.Len(t, rows.Links, 1)
	assert.Len(t, rows.Emphases, 1)
	assert.Len(t, rows.Tags, 1)
	require.NotNil(t, rows.Frontmatter)
}

// ---------------------------------------------------------------------------
// The launch query
// ---------------------------------------------------------------------------

func TestPlayLaunchSQL(t *testing.T) {
	sql := playLaunchSQL(12345678901234567890)

	assert.Contains(t, sql, "LW_COMPONENT('MdDoc')", "the component read the registered store answers")
	assert.Contains(t, sql, "tupleElement", "the canonical authoring form (ADR-0189)")
	assert.Contains(t, sql, "'Content'", "the tuple field carries the Go field's name")
	assert.Contains(t, sql, "gloss(", "the content is declared markdown, never sniffed")
	assert.Contains(t, sql, "'text/markdown'")
	assert.Contains(t, sql, "'label', 'doc'")
	assert.Contains(t, sql, `"id:id" = 12345678901234567890`, "filtered to exactly the row this send wrote")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(sql), "LIMIT 1"))
}

func TestUtoa(t *testing.T) {
	assert.Equal(t, "0", utoa(0))
	assert.Equal(t, "42", utoa(42))
	assert.Equal(t, "18446744073709551615", utoa(^uint64(0)), "the full uint64 range — ids are hashes")
}

// TestSendToPlay_RequiresABus keeps the gesture honest in a bus-less harness.
func TestSendToPlay_RequiresABus(t *testing.T) {
	inst := &App{src: "# Doc\n"}
	inst.sendToPlay()
	require.Contains(t, inst.status, "no bus")
}
