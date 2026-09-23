package llm

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The wire forms, encoded through buscodec as plain CBOR structs with a
// version byte — the chlocalbroker shape rather than a generated leeway
// codec, because a completion request is a nested message model (tool
// definitions carry JSON schemas, tool calls nest in messages) that a
// flat facts row would flatten into a dozen parallel columns for no
// reader's benefit. The record that IS a flat row is the call fact
// (introspect.go), and that one is a table.

const wireVersion uint8 = 1

// wireRequest is the envelope on llm.complete: the caller's purpose and
// sensitivity declaration, and the completion request minus what the
// service owns (the model id, the endpoint).
type wireRequest struct {
	V           uint8  `json:"v"`
	Purpose     string `json:"purpose,omitempty"`
	Sensitivity uint8  `json:"sensitivity,omitempty"`

	Messages       []openaichat.Message       `json:"messages"`
	Temperature    *float32                   `json:"temperature,omitempty"`
	MaxTokens      int32                      `json:"max_tokens,omitempty"`
	Seed           *int64                     `json:"seed,omitempty"`
	Stop           []string                   `json:"stop,omitempty"`
	EnableThinking bool                       `json:"enable_thinking,omitempty"`
	Tools          []openaichat.Tool          `json:"tools,omitempty"`
	ToolChoice     string                     `json:"tool_choice,omitempty"`
	ResponseFormat *openaichat.ResponseFormat `json:"response_format,omitempty"`
	// DeadlineUnixNanos carries the caller's ctx deadline, since the
	// handler has no ctx of its own. 0 means none.
	DeadlineUnixNanos int64 `json:"deadline_ns,omitempty"`
}

// wireDescribe is the reply on llm.describe.
type wireDescribe struct {
	V            uint8  `json:"v"`
	Configured   bool   `json:"configured"`
	Model        string `json:"model,omitempty"`
	EndpointHost string `json:"endpoint_host,omitempty"`
	Local        bool   `json:"local,omitempty"`
	MaxTokens    int32  `json:"max_tokens,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// wireReply is the reply on llm.complete. Ok false carries the reason and
// the kind of failure, so a caller can map it back onto the client's
// sentinel errors; a truncated completion is Ok with Incomplete set and
// its content kept, the client's own contract.
type wireReply struct {
	V            uint8                 `json:"v"`
	Ok           bool                  `json:"ok"`
	Reason       string                `json:"reason,omitempty"`
	ErrorKind    string                `json:"error_kind,omitempty"`
	Content      string                `json:"content,omitempty"`
	Reasoning    string                `json:"reasoning,omitempty"`
	FinishReason string                `json:"finish_reason,omitempty"`
	ToolCalls    []openaichat.ToolCall `json:"tool_calls,omitempty"`
	InputTokens  int32                 `json:"input_tokens,omitempty"`
	OutputTokens int32                 `json:"output_tokens,omitempty"`
	Incomplete   bool                  `json:"incomplete,omitempty"`
	ElapsedNs    int64                 `json:"elapsed_ns,omitempty"`
}

// The failure kinds a reply can name, mapped back onto openaichat's
// sentinels by the client.
const (
	errKindRefused       = "refused"
	errKindAuth          = "auth"
	errKindModelNotFound = "model_not_found"
	errKindRateLimited   = "rate_limited"
	errKindBadRequest    = "bad_request"
	errKindServer        = "server"
	errKindTimeout       = "timeout"
	errKindOther         = "other"
)

func encode[T any](v T) (b []byte, err error) {
	b, err = buscodec.Encode(v)
	if err != nil {
		err = eh.Errorf("llm: encode: %w", err)
	}
	return
}

func decode[T any](b []byte) (v T, err error) {
	v, err = buscodec.Decode[T](b)
	if err != nil {
		err = eh.Errorf("llm: decode: %w", err)
	}
	return
}
