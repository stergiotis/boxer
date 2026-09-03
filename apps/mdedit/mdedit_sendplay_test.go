package mdedit

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// The row
// ---------------------------------------------------------------------------

func TestBuildMdDocRow(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0).UTC()
	row := buildMdDocRow("# Title\n\nbody\n", "Title", "notes.md", 2, ts)

	assert.Equal(t, "mdDoc", row.Kind)
	assert.Equal(t, "Title", row.Title)
	assert.Equal(t, "notes.md", row.FileName)
	assert.Equal(t, "# Title\n\nbody\n", row.Content)
	assert.Equal(t, uint64(2), row.Words)
	assert.Equal(t, ts, row.Ts)
	assert.Len(t, row.NaturalKey, 32, "the natural key is the blake3-256 of the content")
	assert.Len(t, row.ContentHash, 64, "hex of the same digest")
	assert.NotZero(t, row.Id)
}

// TestBuildMdDocRow_IdentityRules pins the two-level identity: the natural
// key is the CONTENT (identical text is the same entity across sends), while
// the id also hashes the send time (every send is its own row — the launch
// filter key).
func TestBuildMdDocRow_IdentityRules(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0).UTC()
	t2 := t1.Add(time.Minute)

	a := buildMdDocRow("same text", "", "", 2, t1)
	b := buildMdDocRow("same text", "", "", 2, t2)
	c := buildMdDocRow("other text", "", "", 2, t1)

	assert.Equal(t, a.NaturalKey, b.NaturalKey, "identical content is the same entity")
	assert.NotEqual(t, a.Id, b.Id, "each send is its own row")
	assert.NotEqual(t, a.NaturalKey, c.NaturalKey)
	assert.Equal(t, a.ContentHash, b.ContentHash)
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

// TestSendToPlay_RequiresABus keeps the launch half honest in a bus-less
// harness. The upload half deliberately does NOT require one — it goes to
// ClickHouse directly — so only the play gesture refuses here.
func TestSendToPlay_RequiresABus(t *testing.T) {
	inst := &App{src: "# Doc\n"}
	inst.sendDoc(true)
	require.Contains(t, inst.status, "no bus")
	inst.mu.Lock()
	sending := inst.sending
	inst.mu.Unlock()
	require.False(t, sending, "a refused gesture claims no in-flight slot")
}
