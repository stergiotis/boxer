package trail

import "github.com/stergiotis/boxer/public/functional/option"

// LlmMessage is one message of a model call as the audit knows it (ADR-0277
// §SD2): which call, where in the conversation, who spoke, how much, and
// which tool calls it issued or answers — never the text. A call writes a
// row per message that is new since its parent, from the call row's
// MessagesFrom on; the reply is the last of them. The text, when a
// deployment keeps it, is [LlmMessageBody] on the same row.
type LlmMessage struct {
	_ struct{} `kind:"llmMessage"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind   string `lw:"runtimeKindLlmMessage,symbol"`
	CallId string `lw:"llmMessageCallId,stringArray,unit"`
	// Ordinal is the message's position in the logical conversation.
	Ordinal uint32 `lw:"llmMessageOrdinal,u32Array,unit"`
	// Role is system, user, assistant or tool.
	Role string `lw:"llmMessageRole,symbol"`
	// Sensitivity is the call's label, on every row (ADR-0264 §SD5).
	Sensitivity string `lw:"llmMessageSensitivity,symbol"`
	// Bytes is the size of the content; Digest its digest ([ContentDigest]).
	Bytes  uint64 `lw:"llmMessageBytes,u64Array,unit"`
	Digest string `lw:"llmMessageDigest,stringArray,unit"`
	// ToolCallId is the call a tool message answers.
	ToolCallId option.Option[string] `lw:"llmMessageToolCallId,stringArray,unit"`
	// ToolCallIds and ToolNames are an assistant message's calls, in the
	// order it issued them: the provider's ids and the tools named.
	ToolCallIds []string `lw:"llmMessageToolCallIds,stringArray"`
	ToolNames   []string `lw:"llmMessageToolNames,symbolArray"`
	// Images are the attached pictures as "<media type> <blake3> <bytes>";
	// the bytes are not kept.
	Images []string `lw:"llmMessageImages,stringArray"`
}
