package trail

import "github.com/stergiotis/boxer/public/functional/option"

// LlmCall is one completion the model service answered, refused or failed
// (ADR-0254 §SD4): why it was asked, what it cost, how it ended — and
// nothing of what was said. Who asked is the row's [Origin]; the
// conversation and the task, when there are any, its [Conversation] and
// [Delegation]. The row's timestamp is when the request reached the service.
type LlmCall struct {
	_ struct{} `kind:"llmCall"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindLlmCall,symbol"`
	// CallId is the service-minted id of the call, the row's natural key.
	// Parent is the call this one continues (ADR-0264 §SD2).
	CallId string                `lw:"llmCallId,stringArray,unit"`
	Parent option.Option[string] `lw:"llmCallParent,stringArray,unit"`
	// Purpose is book/slug, what keelson('llm_prompts') joins on.
	Purpose string `lw:"llmCallPurpose,symbol"`
	// Sensitivity is "ordinary" or "confined" (ADR-0145 §SD3).
	Sensitivity string `lw:"llmCallSensitivity,symbol"`
	// Model and EndpointHost are the host's configuration at the time of the
	// call; ReportedModel is the name the provider answered with and
	// ProviderId its id for the completion, the key into its own records.
	Model         string                `lw:"llmCallModel,symbol"`
	EndpointHost  string                `lw:"llmCallEndpointHost,symbol"`
	ReportedModel option.Option[string] `lw:"llmCallReportedModel,symbol"`
	ProviderId    option.Option[string] `lw:"llmCallProviderId,stringArray,unit"`
	// The request's shape. ToolsDigest is the digest of the tool definitions
	// offered, absent when none were; MaxTokens the ceiling asked for.
	Messages    uint32                `lw:"llmCallMessages,u32Array,unit"`
	Tools       uint32                `lw:"llmCallTools,u32Array,unit"`
	ToolsDigest option.Option[string] `lw:"llmCallToolsDigest,stringArray,unit"`
	MaxTokens   uint32                `lw:"llmCallMaxTokens,u32Array,unit"`
	// The answer's cost.
	PromptBytes     uint64 `lw:"llmCallPromptBytes,u64Array,unit"`
	CompletionBytes uint64 `lw:"llmCallCompletionBytes,u64Array,unit"`
	InputTokens     uint32 `lw:"llmCallInputTokens,u32Array,unit"`
	OutputTokens    uint32 `lw:"llmCallOutputTokens,u32Array,unit"`
	ToolCalls       uint32 `lw:"llmCallToolCalls,u32Array,unit"`
	FinishReason    string `lw:"llmCallFinishReason,symbol"`
	ElapsedMs       uint64 `lw:"llmCallElapsedMs,u64Array,unit"`
	// How it ended: truncated at the ceiling, refused by the service, or
	// the provider's error text — one element when there is one.
	Incomplete bool     `lw:"llmCallIncomplete,bool"`
	Refused    bool     `lw:"llmCallRefused,bool"`
	Error      []string `lw:"llmCallError,stringArray"`
	// Retention is the verdict on keeping the text (ADR-0264 §SD4):
	// "not-asked", "kept" or "not-kept".
	Retention string `lw:"llmCallRetention,symbol"`
	// MessagesFrom is the ordinal the call's own LlmMessage rows start at:
	// 0 when it wrote the whole request, the parent's length when it wrote
	// only what was new. HistoryHash is the hash over the logical
	// conversation after the call. OmitFrom and OmitTo are the range of the
	// logical conversation the request declared it left out.
	MessagesFrom uint32                `lw:"llmCallRetainedFrom,u32Array,unit"`
	HistoryHash  string                `lw:"llmCallHistoryHash,stringArray,unit"`
	OmitFrom     option.Option[uint32] `lw:"llmCallOmitFrom,u32Array,unit"`
	OmitTo       option.Option[uint32] `lw:"llmCallOmitTo,u32Array,unit"`
}
