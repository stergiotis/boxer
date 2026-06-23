package openaichat

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fastRetry is a tiny-delay policy so retry tests don't sleep for real.
func fastRetry(maxAttempts int) RetryPolicy {
	return RetryPolicy{MaxAttempts: maxAttempts, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func newServerClientOpts(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/", "", opts...)
	require.NoError(t, err)
	return c
}

// --- new request fields -----------------------------------------------------

func TestEncodeRequestSeedAndStop(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)

	seed := int64(42)
	r := userReq("m")
	r.Seed = &seed
	r.Stop = []string{";", "END"}
	body, err := inst.encodeRequest(r)
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `"seed":42`)
	assert.Contains(t, s, `"stop":[`)
	assert.Contains(t, s, `"END"`)

	// unset seed/stop are omitted
	bare, err := inst.encodeRequest(userReq("m"))
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "seed")
	assert.NotContains(t, string(bare), "stop")
}

func TestEncodeResponseFormat(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)

	r := userReq("m")
	r.ResponseFormat = JSONObjectFormat()
	b1, err := inst.encodeRequest(r)
	require.NoError(t, err)
	assert.Contains(t, string(b1), `"response_format":{"type":"json_object"}`)

	r.ResponseFormat = JSONSchemaFormat("sql", jsontext.Value(`{"type":"object","properties":{"sql":{"type":"string"}}}`), true)
	b2, err := inst.encodeRequest(r)
	require.NoError(t, err)
	s := string(b2)
	assert.Contains(t, s, `"type":"json_schema"`)
	assert.Contains(t, s, `"name":"sql"`)
	assert.Contains(t, s, `"properties"`)
	assert.Contains(t, s, `"strict":true`)

	// json_schema without name+schema is rejected
	r.ResponseFormat = &ResponseFormat{Type: "json_schema"}
	_, err = inst.encodeRequest(r)
	require.Error(t, err)

	// unknown type is rejected
	r.ResponseFormat = &ResponseFormat{Type: "yaml"}
	_, err = inst.encodeRequest(r)
	require.Error(t, err)
}

func TestEncodeTools(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)

	r := userReq("m")
	r.Tools = []Tool{{Name: "run_sql", Description: "run a query", Parameters: jsontext.Value(`{"type":"object"}`)}}
	r.ToolChoice = "auto"
	b, err := inst.encodeRequest(r)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"tools":[`)
	assert.Contains(t, s, `"type":"function"`)
	assert.Contains(t, s, `"name":"run_sql"`)
	assert.Contains(t, s, `"tool_choice":"auto"`)

	// a non-keyword ToolChoice forces that function by name
	r.ToolChoice = "run_sql"
	b2, err := inst.encodeRequest(r)
	require.NoError(t, err)
	assert.Contains(t, string(b2), `"tool_choice":{`)

	// ToolChoice without Tools is rejected
	bad := userReq("m")
	bad.ToolChoice = "auto"
	_, err = inst.encodeRequest(bad)
	require.Error(t, err)
}

func TestEncodeToolConversation(t *testing.T) {
	inst, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	r := CompletionRequest{ModelId: "m", Messages: []Message{
		{Role: ChatRoleUser, Content: "count rows"},
		{Role: ChatRoleAssistant, ToolCalls: []ToolCall{{Id: "call_1", Name: "run_sql", Arguments: `{"q":"select 1"}`}}},
		{Role: ChatRoleTool, ToolCallId: "call_1", Content: "1"},
	}}
	b, err := inst.encodeRequest(r)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"tool_calls":[`)
	assert.Contains(t, s, `"id":"call_1"`)
	assert.Contains(t, s, `"role":"tool"`)
	assert.Contains(t, s, `"tool_call_id":"call_1"`)
}

func TestCompleteRejectsToolMessageWithoutId(t *testing.T) {
	c, err := NewClient("https://example.com/v1/", "")
	require.NoError(t, err)
	_, err = c.Complete(context.Background(), CompletionRequest{
		ModelId:  "m",
		Messages: []Message{{Role: ChatRoleTool, Content: "result"}}, // no ToolCallId
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ToolCallId")
}

// --- tool-call response -----------------------------------------------------

func TestCompleteParsesToolCalls(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"run_sql","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
	})
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err, "tool_calls is a valid terminal state, not an error")
	assert.Equal(t, "tool_calls", resp.FinishReason)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "call_1", resp.ToolCalls[0].Id)
	assert.Equal(t, "run_sql", resp.ToolCalls[0].Name)
	assert.Equal(t, `{"q":"x"}`, resp.ToolCalls[0].Arguments)
}

// --- typed error sentinels --------------------------------------------------

func TestCompleteErrorSentinels(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusUnauthorized, ErrAuth},
		{http.StatusForbidden, ErrAuth},
		{http.StatusNotFound, ErrModelNotFound},
		{http.StatusBadRequest, ErrBadRequest},
		{http.StatusUnprocessableEntity, ErrBadRequest},
		{http.StatusInternalServerError, ErrServer},
		{http.StatusServiceUnavailable, ErrServer},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/models" {
					_, _ = io.WriteString(w, `{"data":[]}`)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			})
			_, err := c.Complete(context.Background(), userReq("m"))
			require.Error(t, err)
			assert.True(t, errors.Is(err, tc.want), "status %d: want %v, got %v", tc.status, tc.want, err)
		})
	}
}

// --- retry ------------------------------------------------------------------

func TestRetrySucceedsAfterTransient(t *testing.T) {
	var calls atomic.Int32
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"try later"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}, WithRetry(fastRetry(4)))
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, int32(3), calls.Load())
}

func TestRetryExhaustionReturnsSentinel(t *testing.T) {
	var calls atomic.Int32
	var stat RequestStat
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
	}, WithRetry(fastRetry(3)), WithObserver(func(s RequestStat) { stat = s }))
	_, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRateLimited))
	assert.Equal(t, int32(3), calls.Load())
	assert.Equal(t, 3, stat.Attempts)
	assert.Equal(t, http.StatusTooManyRequests, stat.Status)
	assert.Error(t, stat.Err)
}

func TestRetrySkipsClientError(t *testing.T) {
	var calls atomic.Int32
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"nope"}}`)
	}, WithRetry(fastRetry(4)))
	_, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadRequest))
	assert.Equal(t, int32(1), calls.Load(), "4xx must not be retried")
}

func TestRetryable(t *testing.T) {
	assert.True(t, retryable(http.StatusTooManyRequests, nil))
	assert.True(t, retryable(http.StatusServiceUnavailable, nil))
	assert.True(t, retryable(http.StatusInternalServerError, nil))
	assert.False(t, retryable(http.StatusBadRequest, nil))
	assert.False(t, retryable(http.StatusNotFound, nil))
	assert.False(t, retryable(http.StatusOK, nil))
	assert.True(t, retryable(0, errors.New("dial tcp: connection refused")))
	assert.False(t, retryable(0, context.Canceled))
	assert.False(t, retryable(0, context.DeadlineExceeded))
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	assert.Equal(t, 5*time.Second, parseRetryAfter("5"))
	assert.Equal(t, time.Duration(0), parseRetryAfter("garbage"))
	assert.Equal(t, time.Duration(0), parseRetryAfter("Mon, 01 Jan 2001 00:00:00 GMT"), "past date yields no delay")
}

// --- observer + response cap ------------------------------------------------

func TestObserverReceivesStats(t *testing.T) {
	var stat RequestStat
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`)
	}, WithObserver(func(s RequestStat) { stat = s }))
	_, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err)
	assert.Equal(t, "m", stat.Model)
	assert.Equal(t, "stop", stat.FinishReason)
	assert.Equal(t, http.StatusOK, stat.Status)
	assert.Equal(t, 1, stat.Attempts)
	assert.Equal(t, int32(7), stat.InputTokens)
	assert.Equal(t, int32(11), stat.OutputTokens)
	assert.NoError(t, stat.Err)
}

func TestMaxResponseBytesCap(t *testing.T) {
	big := strings.Repeat("x", 1000)
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+big+`"},"finish_reason":"stop"}]}`)
	}, WithMaxResponseBytes(100))
	_, err := c.Complete(context.Background(), userReq("m"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestMaxResponseBytesUnlimited(t *testing.T) {
	big := strings.Repeat("x", 5000)
	c := newServerClientOpts(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+big+`"},"finish_reason":"stop"}]}`)
	}, WithMaxResponseBytes(0))
	resp, err := c.Complete(context.Background(), userReq("m"))
	require.NoError(t, err)
	assert.Len(t, resp.Content, 5000)
}
