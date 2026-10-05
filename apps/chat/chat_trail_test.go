package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The tool loop records each model call and each tool call as it happens,
// and a landed turn's tool entries keep their steps to inspect.
func TestTheTrailRecordsEveryStep(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}],"title":"Asking"}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("s1", "call_operation", `{"window":100,"operation":"set_note","text":"tidied"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)

	kinds := make([]stepKindE, 0, len(res.steps))
	for _, s := range res.steps {
		kinds = append(kinds, s.kind)
		assert.True(t, s.done, "a landed turn has no running step")
	}
	assert.Equal(t, []stepKindE{stepModel, stepTool, stepModel, stepTool, stepModel, stepTool, stepModel, stepTool, stepModel}, kinds)
	first := res.steps[1]
	assert.Equal(t, "request_access", first.name)
	assert.Equal(t, "Asking", first.title)
	assert.Contains(t, first.args, "\n  \"plan\": \"tidy the note\"", "arguments are indented for reading")
	assert.Equal(t, res.activity[0], first.activity)
	assert.Contains(t, res.steps[5].result, `"text":"first"`, "a step keeps what the model read back")
	assert.True(t, res.steps[7].refused, "a refused call is marked")
	assert.Equal(t, 1, res.steps[0].tools)

	conv := newConversation()
	conv.begin("tidy it", 0, false)
	conv.landTurn(req, res, nil, time.Now().UnixMilli())
	var tools []entry
	for _, e := range conv.entries {
		if e.speaker == speakerTool {
			tools = append(tools, e)
		}
	}
	require.Len(t, tools, 4)
	for _, e := range tools {
		require.Len(t, e.steps, 2, "the model call that asked, then the call itself")
		assert.Equal(t, stepModel, e.steps[0].kind)
		assert.Equal(t, stepTool, e.steps[1].kind)
		assert.Equal(t, e.text, e.steps[1].activity)
	}
}

func TestStepsOfToolsPairsARoundsFirstToolWithItsModelCall(t *testing.T) {
	steps := []trailStep{{kind: stepModel, round: 0}, {kind: stepTool, name: "a"}, {kind: stepTool, name: "b"}, {kind: stepModel, round: 1}}
	got := stepsOfTools(steps)
	require.Len(t, got, 2)
	assert.Len(t, got[0], 2)
	assert.Len(t, got[1], 1, "a second tool of the round carries only itself")
	assert.Equal(t, "b", got[1][0].name)
}

func TestClipKeepsWholeRunes(t *testing.T) {
	s := strings.Repeat("ä", 10)
	got := clip(s, 5)
	assert.True(t, strings.HasPrefix(got, "ää\n… "), got)
	assert.Contains(t, got, "16 bytes more")
	tail := clipTail(s, 5)
	assert.True(t, strings.HasSuffix(tail, "\nää"), tail)
	assert.Equal(t, "short", clip("short", 5))
}

func TestANilTrailRecordsNothing(t *testing.T) {
	var tr *turnTrail
	tr.reset()
	i := tr.begin(trailStep{})
	tr.finish(i, func(s *trailStep) { s.name = "x" })
	steps, _, changed := tr.snapshot(0)
	assert.Nil(t, steps)
	assert.False(t, changed)
}

