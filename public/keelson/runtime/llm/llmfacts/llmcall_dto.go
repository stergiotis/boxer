package llmfacts

import "time"

// LlmCall is one completion as a `boxer.facts` row (ADR-0254 §SD4). Id is
// the xxh3 of the call id and NaturalKey the call id, so the row is
// addressable; Ts is when the call was answered.
type LlmCall struct {
	_ struct{} `kind:"llmCall"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindLlmCall,symbol"`
	// CallId is the service-minted id of the call, the natural key again as
	// a column a scan can filter on.
	CallId string `lw:"llmCallId,symbol"`
	// App is the requesting app and Instance its window (ADR-0191).
	App      string `lw:"llmCallApp,symbol"`
	Instance uint64 `lw:"llmCallInstance,u64Array,unit"`
	// Purpose is book/slug, what keelson('llm_prompts') joins on.
	Purpose string `lw:"llmCallPurpose,symbol"`
	// Sensitivity is "ordinary" or "confined" (ADR-0145 §SD3).
	Sensitivity string `lw:"llmCallSensitivity,symbol"`
	// Model and EndpointHost are the host's at the time of the call.
	Model        string `lw:"llmCallModel,symbol"`
	EndpointHost string `lw:"llmCallEndpointHost,symbol"`
	// The request's shape and the answer's cost.
	Messages        uint32 `lw:"llmCallMessages,u32Array,unit"`
	Tools           uint32 `lw:"llmCallTools,u32Array,unit"`
	PromptBytes     uint64 `lw:"llmCallPromptBytes,u64Array,unit"`
	CompletionBytes uint64 `lw:"llmCallCompletionBytes,u64Array,unit"`
	InputTokens     uint32 `lw:"llmCallInputTokens,u32Array,unit"`
	OutputTokens    uint32 `lw:"llmCallOutputTokens,u32Array,unit"`
	ToolCalls       uint32 `lw:"llmCallToolCalls,u32Array,unit"`
	FinishReason    string `lw:"llmCallFinishReason,symbol"`
	ElapsedMs       uint64 `lw:"llmCallElapsedMs,u64Array,unit"`
	// How it ended: truncated at the ceiling, refused by the service, or
	// the provider's error text.
	Incomplete bool `lw:"llmCallIncomplete,bool"`
	Refused    bool `lw:"llmCallRefused,bool"`
	// Error is the provider's error or the refusal reason: one element
	// when there is one, none otherwise — the string section is array-valued,
	// the log kind's shape.
	Error []string `lw:"llmCallError,stringArray"`
}
