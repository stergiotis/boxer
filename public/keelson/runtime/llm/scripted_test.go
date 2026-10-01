package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// ADR-0269 M6: the request's position picks the reply, and "$window"
// takes the last window a tool result reported.
func TestScriptedClientAnswersByPosition(t *testing.T) {
	c, err := NewScriptedClient([]byte(`# a comment
{"tool":"open_window","args":{"app":"play"}}

{"tool":"call","args":{"window":"$window","operation":"get_state"}}
{"content":"done"}
`))
	require.NoError(t, err)
	user := openaichat.Message{Role: openaichat.ChatRoleUser, Content: "go"}
	r, err := c.Complete(context.Background(), openaichat.CompletionRequest{Messages: []openaichat.Message{user}})
	require.NoError(t, err)
	require.Len(t, r.ToolCalls, 1)
	assert.Equal(t, "open_window", r.ToolCalls[0].Name)
	assert.Equal(t, "s0", r.ToolCalls[0].Id)

	msgs := []openaichat.Message{user, {Role: openaichat.ChatRoleAssistant, ToolCalls: r.ToolCalls},
		{Role: openaichat.ChatRoleTool, ToolCallId: "s0", Content: `{"window":42}`}}
	r, err = c.Complete(context.Background(), openaichat.CompletionRequest{Messages: msgs})
	require.NoError(t, err)
	assert.JSONEq(t, `{"window":42,"operation":"get_state"}`, r.ToolCalls[0].Arguments)

	again, err := c.Complete(context.Background(), openaichat.CompletionRequest{Messages: msgs})
	require.NoError(t, err)
	assert.Equal(t, r, again, "a resent request repeats its reply")

	msgs = append(msgs, openaichat.Message{Role: openaichat.ChatRoleAssistant, ToolCalls: r.ToolCalls}, openaichat.Message{Role: openaichat.ChatRoleTool, Content: "{}"})
	r, err = c.Complete(context.Background(), openaichat.CompletionRequest{Messages: msgs})
	require.NoError(t, err)
	assert.Equal(t, "done", r.Content)
	msgs = append(msgs, openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: "done"})
	r, err = c.Complete(context.Background(), openaichat.CompletionRequest{Messages: msgs})
	require.NoError(t, err)
	assert.Contains(t, r.Content, "script has ended")
}

func TestScriptedClientRefusesBadLines(t *testing.T) {
	for _, in := range []string{`{"content":"a","tool":"b"}`, `{}`, `{"what":1}`, `not json`} {
		_, err := NewScriptedClient([]byte(in))
		assert.Error(t, err, in)
	}
}
