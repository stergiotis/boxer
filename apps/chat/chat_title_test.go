package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

func TestTitlesAreOneCleanLine(t *testing.T) {
	assert.Equal(t, "How do I read llm_calls?", firstLineTitle("\n\n## How do I read llm_calls?\nmore"))
	assert.Equal(t, "Reading the call record", modelTitle("\"Reading the call record.\"\n"))
	assert.Equal(t, "Token budgets", cleanConvTitle("Title: **Token budgets**"))
	long := cleanConvTitle(strings.Repeat("word ", 40))
	assert.Equal(t, maxConvTitleRunes, len([]rune(long)))
	assert.True(t, strings.HasSuffix(long, "…"))
}

// The first send titles the conversation with its first line; the model's
// title replaces it after the first answer; a rename wins over both, and
// an empty rename goes back to the first line.
func TestATitleFromTheFirstLineThenTheModel(t *testing.T) {
	inst := appOn(t, &fakeModel{})
	require.True(t, inst.startTurn("what is a leeway table?\nsecond line"))
	assert.Equal(t, "what is a leeway table?", inst.conv.title)
	assert.Equal(t, titleFirstLine, inst.conv.titleSource)
	drainUntil(t, inst)
	require.True(t, inst.conv.titleAsked)
	require.Eventually(t, func() bool { inst.drain(); return inst.conv.titleSource == titleModel }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, "a2", inst.conv.title, "the fake model's second answer is the title call's")

	inst.conv.rename("  My name  ")
	assert.Equal(t, "My name", inst.conv.title)
	assert.Equal(t, titleManual, inst.conv.titleSource)
	inst.conv.rename("")
	assert.Equal(t, "what is a leeway table?", inst.conv.title)

	old := inst.conv
	inst.newConversation()
	assert.NotSame(t, old, inst.conv)
	assert.Empty(t, inst.conv.title)
}

// The title call is kept when the conversation is, names the first turn,
// and declares what the conversation holds.
func TestTheTitleCallIsKeptWithTheConversation(t *testing.T) {
	r := titleRequest("chat-1", "turn-1", true, "q", strings.Repeat("x", titleExcerptRunes+10), queryengine.SensitivityConfined)
	assert.True(t, r.Retain)
	assert.Equal(t, "turn-1", r.Turn)
	assert.False(t, titleRequest("chat-1", "turn-1", false, "q", "a", queryengine.SensitivityOrdinary).Retain)
	assert.Equal(t, titlePurpose, r.Purpose)
	assert.Equal(t, queryengine.SensitivityConfined, r.Sensitivity)
	require.Len(t, r.Messages, 2)
	assert.Less(t, len([]rune(r.Messages[1].Content)), titleExcerptRunes+100)
}

// The transcript carries the title, what the window talked to, every
// message under its own heading, the tool calls and a failure's details.
func TestTheTranscriptAsMarkdown(t *testing.T) {
	conv := keeping()
	conv.title, conv.startedAt, conv.apps = "Reading calls", 1000, true
	conv.notKept = "ring"
	conv.entries = []entry{
		{speaker: speakerUser, text: "q1", atMs: 1000},
		{speaker: speakerTool, text: "listed 2 window(s)", atMs: 2000},
		{speaker: speakerModel, text: "a1\n\n```sql\nSELECT 1\n```\n", atMs: 2000},
		{speaker: speakerUser, text: "q2", atMs: 3000, edited: true, failed: true, reason: "the model provider answered HTTP 503",
			fail: failure{kind: "server", callId: "llm-1", detail: "llm: HTTP 503:\noverloaded"}},
	}
	md := transcriptMarkdown(conv, transcriptMeta{model: llm.Description{Configured: true, Model: "m", EndpointHost: "h"}, loc: time.UTC})
	for _, want := range []string{
		"# Reading calls\n",
		"- **Model:** m at h\n",
		"- **Kept:** not kept (ring)\n",
		"- **Conversation:** `" + conv.id + "`\n",
		"- **Apps:** on\n",
		"## You · 00:00\n\nq1\n",
		"*Tool calls:*\n\n- listed 2 window(s)\n",
		"## Model · 00:00\n\na1\n\n```sql\nSELECT 1\n```\n",
		"## You · 00:00 (edited)\n\nq2\n",
		"> [!failure] Not answered: the model provider answered HTTP 503\n",
		"> call: llm-1\n",
		"> error: llm: HTTP 503: overloaded\n",
	} {
		assert.Contains(t, md, want)
	}
}

// The composer warns once the conversation nears the context size, and
// only when the size is known.
func TestTheContextWarning(t *testing.T) {
	inst := newApp()
	inst.conv.lastIn, inst.conv.lastOut = 800, 50
	_, ok := inst.contextWarning()
	assert.False(t, ok, "no size, no warning")
	inst.model.ContextTokens = 1000
	line, ok := inst.contextWarning()
	require.True(t, ok)
	assert.Contains(t, line, "85 %")
	inst.model.ContextTokens = 4000
	_, ok = inst.contextWarning()
	assert.False(t, ok)
	assert.Equal(t, "950", tokens(950))
	assert.Equal(t, "12.3k", tokens(12345))
	assert.Equal(t, "131k", tokens(131072))
}
