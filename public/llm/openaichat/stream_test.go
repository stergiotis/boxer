package openaichat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sse writes one event per payload, as a streaming endpoint does.
func sse(w http.ResponseWriter, payloads ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range payloads {
		_, _ = w.Write([]byte("data: " + p + "\n\n"))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

// ADR-0286 §SD1: a recorded stream — content, reasoning, two tool calls in
// fragments and a usage chunk — assembles into the response the plain
// call gives, and the deltas hand the pieces on as they come.
func TestCompleteStreamAssemblesTheResponseAndHandsOnDeltas(t *testing.T) {
	var sawStream atomic.Bool
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			sawStream.Store(true)
		}
		sse(w,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"let me "}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":", world"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"get_state","arguments":""}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"win"}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"dow\":1}"}},{"index":1,"id":"t2","type":"function","function":{"name":"get_map","arguments":"{}"}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"id":"c1","model":"m","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"completion_tokens_details":{"reasoning_tokens":3}}}`,
		)
	})
	var deltas []Delta
	resp, err := c.CompleteStream(context.Background(), userReq("m"), func(d Delta) { deltas = append(deltas, d) })
	require.NoError(t, err)
	assert.True(t, sawStream.Load(), "the stream is asked for")
	assert.Equal(t, "Hello, world", resp.Content)
	assert.Equal(t, "let me think", resp.Reasoning)
	assert.Equal(t, "tool_calls", resp.FinishReason)
	assert.Equal(t, []ToolCall{{Id: "t1", Name: "get_state", Arguments: `{"window":1}`}, {Id: "t2", Name: "get_map", Arguments: "{}"}}, resp.ToolCalls)
	assert.Equal(t, int32(11), resp.InputTokens)
	assert.Equal(t, int32(7), resp.OutputTokens)
	assert.True(t, resp.ReasoningTokens.Has)
	assert.Equal(t, int32(3), resp.ReasoningTokens.Val)
	assert.Equal(t, "c1", resp.Id)
	assert.Equal(t, []Delta{{Reasoning: "let me "}, {Reasoning: "think"}, {Content: "Hello"}, {Content: ", world"}, {ToolCall: "get_state"}, {ToolCall: "get_map"}}, deltas)
}

// An endpoint that rejects `stream` is asked once more for the plain call,
// and the caller sees no delta.
func TestCompleteStreamFallsBackWhenTheEndpointRefusesToStream(t *testing.T) {
	var calls atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"the stream parameter is not supported","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"plain"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})
	deltas := 0
	resp, err := c.CompleteStream(context.Background(), userReq("m"), func(Delta) { deltas++ })
	require.NoError(t, err)
	assert.Equal(t, "plain", resp.Content)
	assert.Equal(t, int32(2), calls.Load())
	assert.Zero(t, deltas)
}

// A stream that ends early is the plain call's incomplete completion: the
// content so far, marked.
func TestCompleteStreamMarksAnAnswerCutShort(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"id":"c2","model":"m","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
			`{"id":"c2","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
		)
	})
	resp, err := c.CompleteStream(context.Background(), userReq("m"), nil)
	require.True(t, errors.Is(err, ErrIncompleteCompletion), "%v", err)
	assert.Equal(t, "partial", resp.Content)
	assert.Equal(t, "length", resp.FinishReason)
}

// A keep-alive comment and a multi-line data field are read as the
// protocol says; an error chunk is the call's error.
func TestCompleteStreamReadsCommentsAndErrorChunks(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keep-alive\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"c3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"a\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"out of memory\",\"type\":\"server_error\"}}\n\n"))
	})
	_, err := c.CompleteStream(context.Background(), userReq("m"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of memory")
}
