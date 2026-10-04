package chat

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
)

const artDoc = "# Plan\n\nintro\n\n## Goals\n\n- one\n\n## Risks\n\nnone\n"

func TestEditReplace(t *testing.T) {
	ch, reason := editReplace(artDoc, "- one", "- one\n- two", false)
	require.Empty(t, reason)
	assert.Contains(t, ch.text, "- one\n- two\n")
	assert.Equal(t, mdspan.LineSpan{First: 7, Last: 8}, ch.changed)

	_, reason = editReplace(artDoc, "absent", "x", false)
	assert.Contains(t, reason, "does not occur")
	_, reason = editReplace(artDoc, "##", "###", false)
	assert.Contains(t, reason, "occurs 2 times")
	ch, reason = editReplace(artDoc, "## ", "### ", true)
	require.Empty(t, reason)
	assert.Equal(t, 2, strings.Count(ch.text, "### "))
	assert.Equal(t, mdspan.LineSpan{First: 5, Last: 9}, ch.changed)
	_, reason = editReplace(artDoc, "", "x", false)
	assert.NotEmpty(t, reason)
}

func TestEditInsertAndSection(t *testing.T) {
	ch, reason := editInsert(artDoc, "first", 1, true, nil, "")
	require.Empty(t, reason)
	assert.True(t, strings.HasPrefix(ch.text, "first\n# Plan"))

	ch, reason = editInsert(artDoc, "- two", 0, false, []string{"Goals"}, "end")
	require.Empty(t, reason)
	assert.Contains(t, ch.text, "- one\n\n- two\n\n## Risks")

	ch, reason = editInsert(artDoc, "Lead.", 0, false, []string{"Plan", "Risks"}, "start")
	require.Empty(t, reason)
	assert.Contains(t, ch.text, "## Risks\nLead.\n")

	_, reason = editInsert(artDoc, "x", 99, true, nil, "")
	assert.Contains(t, reason, "line must be 1 to")
	_, reason = editInsert(artDoc, "x", 0, false, []string{"Nope"}, "")
	assert.Contains(t, reason, "no heading matches")

	// Appending to a text without a final newline starts a new line.
	ch, _ = editInsert("a", "b", 0, false, nil, "")
	assert.Equal(t, "a\nb\n", ch.text)

	ch, reason = editSection(artDoc, []string{"Goals"}, "- replaced", false)
	require.Empty(t, reason)
	assert.Contains(t, ch.text, "## Goals\n- replaced\n\n## Risks")
	assert.NotContains(t, ch.text, "- one")

	ch, reason = editSection(artDoc, []string{"Risks"}, "## Hazards\n\nsome", true)
	require.Empty(t, reason)
	assert.True(t, strings.HasSuffix(ch.text, "## Hazards\n\nsome\n"))
}

func TestEditFrontmatterAndWrite(t *testing.T) {
	ch, reason := editFrontmatter(artDoc, map[string]any{"status": "draft"}, nil)
	require.Empty(t, reason)
	assert.True(t, strings.HasPrefix(ch.text, "---\nstatus: draft\n---\n# Plan"))
	_, reason = editFrontmatter(artDoc, nil, nil)
	assert.NotEmpty(t, reason)

	ch, _ = editWrite("one line")
	assert.Equal(t, "one line\n", ch.text)
	assert.Equal(t, mdspan.LineSpan{First: 1, Last: 1}, ch.changed)
}

func TestArtefactRevisions(t *testing.T) {
	a := newArtefact()
	n, err := a.commit(0, artRevision{text: "a\n"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, err = a.commit(0, artRevision{text: "stale\n"})
	var stale errStale
	require.ErrorAs(t, err, &stale)
	assert.Equal(t, 1, stale.head)

	n, _ = a.commit(1, artRevision{text: "b\n"})
	assert.Equal(t, 2, n)
	n, err = a.revert(1)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	_, text := a.head()
	assert.Equal(t, "a\n", text)

	dropped := a.truncate(1)
	assert.Len(t, dropped, 2)
	head, _ := a.head()
	assert.Equal(t, 1, head)
	// A number taken back is not handed out again.
	n, _ = a.commit(1, artRevision{text: "c\n"})
	assert.Equal(t, 4, n)
	assert.False(t, a.reinstate(1, dropped), "the head moved since the truncate")
}

func TestArtefactProposal(t *testing.T) {
	a := newArtefact()
	ctx := context.Background()
	got := make(chan bool, 1)
	go func() {
		ok, err := a.propose(ctx, &artProposal{text: "x\n"})
		assert.NoError(t, err)
		got <- ok
	}()
	var p *artProposal
	require.Eventually(t, func() bool { p = a.pending(); return p != nil }, 5*time.Second, time.Millisecond)
	p.decide(false)
	p.decide(true)
	assert.False(t, <-got)
	assert.Nil(t, a.pending())

	cctx, cancel := context.WithCancel(ctx)
	go func() {
		require.Eventually(t, func() bool { return a.pending() != nil }, 5*time.Second, time.Millisecond)
		cancel()
	}()
	_, err := a.propose(cctx, &artProposal{text: "y\n"})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestArtefactToolsFollowThePolicy(t *testing.T) {
	names := func(ts []openaichat.Tool) (out []string) {
		for _, x := range ts {
			out = append(out, x.Name)
		}
		return
	}
	assert.NotContains(t, names(artefactTools(false)), "artefact_edit")
	assert.Contains(t, names(artefactTools(true)), "artefact_edit")
	for _, x := range artefactTools(true) {
		var v any
		require.NoError(t, json.Unmarshal(x.Parameters, &v), x.Name)
	}

	coord := newCoordinator(nil, nil, "conv")
	art := newArtefact()
	coord.offerArtefact(art)
	assert.Len(t, coord.tools(context.Background()), 6, "the five reads and artefact_images")
	art.setPolicy(artPolicy{write: true})
	assert.Len(t, coord.tools(context.Background()), 14, "and the writes; no capture without Apps")

	ctx := context.Background()
	art.setPolicy(artPolicy{})
	content, _ := coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_write"}, map[string]any{"base_revision": 0.0, "text": "x"})
	assert.Contains(t, content, "read the artefact, not change it")

	art.setPolicy(artPolicy{write: true})
	content, activity := coord.dispatch(ctx, toolOrigin{turn: "t1"}, openaichat.ToolCall{Name: "artefact_write"},
		map[string]any{"base_revision": 0.0, "text": "# A\n\nsee [[#B]]\n"})
	assert.Contains(t, activity, "revision 1")
	var w writeView
	require.NoError(t, json.Unmarshal([]byte(content), &w))
	assert.Equal(t, 1, w.Revision)
	require.Len(t, w.Findings, 1)
	assert.Equal(t, "ML001", w.Findings[0].Rule)

	content, _ = coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_edit"}, map[string]any{"base_revision": 0.0, "old_text": "A", "new_text": "B"})
	assert.Contains(t, content, "at revision 1, not 0")
	content, _ = coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_edit"}, map[string]any{"old_text": "A", "new_text": "B"})
	assert.Contains(t, content, "base_revision is required")

	content, _ = coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_read"}, map[string]any{"from_line": 3.0})
	assert.Equal(t, "revision 1 · 3 lines · lines 3–3\n3\tsee [[#B]]\n", content)
	content, _ = coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_find"}, map[string]any{"pattern": "SEE"})
	assert.Contains(t, content, `"line":3`)
	content, _ = coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: "artefact_inspect"}, map[string]any{})
	assert.Contains(t, content, `"fragment":"B"`)
}

// A turn's revisions go with it when it is taken back, and come back when
// the edit is abandoned.
func TestRewindTakesTheTurnsRevisions(t *testing.T) {
	conv := newConversation()
	conv.begin("draft it", 1, false)
	_, _ = conv.art.commit(0, artRevision{text: "v1\n"})
	conv.land(conv.request("draft it"), &llm.Response{Content: "drafted"}, nil, 2)

	_, undo, ok := conv.rewind()
	require.True(t, ok)
	head, _ := conv.art.head()
	assert.Equal(t, 0, head)
	require.True(t, conv.restore(undo))
	head, text := conv.art.head()
	assert.Equal(t, 1, head)
	assert.Equal(t, "v1\n", text)
}

func TestLineDiff(t *testing.T) {
	d := lineDiff("a\nb\nc\nd\ne\nf\ng\n", "a\nb\nC\nd\ne\nf\ng\nh\n")
	add, del := diffCounts(d)
	assert.Equal(t, 2, add)
	assert.Equal(t, 1, del)
	var ops []diffOpE
	for _, l := range d {
		ops = append(ops, l.op)
	}
	assert.Equal(t, []diffOpE{diffOpSame, diffOpSame, diffOpDel, diffOpAdd, diffOpSame, diffOpSame, diffOpSame, diffOpSame, diffOpAdd}, ops)
	d = lineDiff("1\n2\n3\n4\n5\n6\n7\n8\nx\n", "1\n2\n3\n4\n5\n6\n7\n8\ny\n")
	assert.Equal(t, diffOpGap, d[0].op)
	assert.Empty(t, lineDiff("same\n", "same\n")[0].text)
}

// Under Ask first, a write waits inside the turn for the person; accepted,
// it is the next revision and the model reads the result.
func TestAskFirstWriteWaitsForThePerson(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("w1", "artefact_write", `{"title":"Drafting the plan","base_revision":0,"text":"# Plan\n"}`),
		{Content: "Drafted.", FinishReason: "stop"},
	}}
	coord, cli, req := questionsRig(t, model)
	coord.offer(false, false)
	conv := newConversation()
	conv.art.setPolicy(artPolicy{write: true, ask: true})
	coord.offerArtefact(conv.art)

	done := make(chan *turnResult, 1)
	go func() {
		res, err := runTurn(context.Background(), cli, coord, req, nil)
		assert.NoError(t, err)
		done <- res
	}()
	var p *artProposal
	require.Eventually(t, func() bool { p = conv.art.pending(); return p != nil }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, stagePerson, coord.stageNow())
	assert.Equal(t, "Drafting the plan", p.title)
	p.decide(true)
	res := <-done
	require.NotNil(t, res)
	assert.Equal(t, "Drafted.", res.final.Content)
	head, text := conv.art.head()
	assert.Equal(t, 1, head)
	assert.Equal(t, "# Plan\n", text)
	assert.Equal(t, "Drafting the plan", conv.art.revisions()[0].title)
	assert.Contains(t, toolReplies(res.messages)["w1"], `"revision":1`)
	// The first model call was told where the artefact stands.
	require.NotEmpty(t, model.seen)
	found := false
	for _, m := range model.seen[0].Messages {
		found = found || strings.Contains(m.Content, "The artefact is empty")
	}
	assert.True(t, found)
}

func testMeta() artMeta {
	return artMeta{conversation: "chat-x-1", title: "A plan", started: time.Date(2026, 10, 4, 19, 30, 5, 123, time.UTC),
		model: "m", endpoint: "127.0.0.1", run: "run-1", app: "chat", window: 7, keep: true}
}

// Every write carries the chat's properties; the model cannot change or
// remove them, and its own properties stay beside them.
func TestChatPropertiesAreStampedIntoEveryWrite(t *testing.T) {
	coord := newCoordinator(nil, nil, "conv")
	art := newArtefact()
	art.setPolicy(artPolicy{write: true})
	art.setMeta(testMeta())
	coord.offerArtefact(art)
	ctx := context.Background()
	write := func(name string, args map[string]any) string {
		content, _ := coord.dispatch(ctx, toolOrigin{}, openaichat.ToolCall{Name: name}, args)
		return content
	}

	write("artefact_write", map[string]any{"base_revision": 0.0, "text": "---\nstatus: draft\n---\n# Plan\n"})
	_, text := art.head()
	for _, want := range []string{"status: draft\n", "chat_conversation: chat-x-1\n", "chat_title: A plan\n", "chat_started: 2026-10-04T19:30:05Z\n",
		"chat_model: m\n", "chat_endpoint: 127.0.0.1\n", "chat_run: run-1\n", "chat_app: chat\n", "chat_window: 7\n", "chat_keep: true\n"} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "chat_task", "an empty property is left out")
	assert.True(t, strings.HasSuffix(text, "---\n# Plan\n"))

	// An edit of a chat property is undone by the stamp: no change at all.
	content := write("artefact_edit", map[string]any{"base_revision": 1.0, "old_text": "chat_run: run-1", "new_text": "chat_run: forged"})
	assert.Contains(t, content, `"unchanged":true`)

	content = write("artefact_set_frontmatter", map[string]any{"base_revision": 1.0, "delete": []any{"chat_run"}})
	assert.Contains(t, content, "cannot be deleted")
	content = write("artefact_set_frontmatter", map[string]any{"base_revision": 1.0, "set": map[string]any{"chat_model": "x"}})
	assert.Contains(t, content, "cannot be set")

	// A rename lands with the next write; the changed lines cover it.
	m := testMeta()
	m.title = "The plan"
	art.setMeta(m)
	content = write("artefact_edit", map[string]any{"base_revision": 1.0, "old_text": "# Plan", "new_text": "# The plan"})
	var w writeView
	require.NoError(t, json.Unmarshal([]byte(content), &w))
	require.NotNil(t, w.Changed)
	_, text = art.head()
	assert.Contains(t, text, "chat_title: The plan\n")
	assert.Less(t, w.Changed.First, w.Changed.Last)

	content = write("artefact_write", map[string]any{"base_revision": 2.0, "text": "---\nbad: [\n---\nx\n"})
	assert.Contains(t, content, "not valid YAML")
}

func TestChangedLines(t *testing.T) {
	assert.Equal(t, mdspan.LineSpan{First: 2, Last: 3}, changedLines("a\nb\nc\n", "a\nB\nX\nc\n"))
	assert.Equal(t, mdspan.LineSpan{First: 2, Last: 2}, changedLines("a\nb\nc\n", "a\nc\n"))
	assert.Equal(t, mdspan.LineSpan{First: 1, Last: 1}, changedLines("", "x\n"))
}
