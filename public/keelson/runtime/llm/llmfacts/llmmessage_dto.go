package llmfacts

import "time"

// LlmMessage is one message of a retained conversation (ADR-0264 §SD3): a
// kind of its own beside [LlmCall], so the text can be ditched without
// touching the counts (§SD6). Id is the xxh3 of NaturalKey, which is the
// call id and the ordinal; Ts is the call's.
//
// A call keeps only the messages new since its parent, from the call row's
// RetainedFrom on; the reply is the message at the call's message count.
type LlmMessage struct {
	_ struct{} `kind:"llmMessage"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind         string `lw:"runtimeKindLlmMessage,symbol"`
	CallId       string `lw:"llmMessageCallId,symbol"`
	Conversation string `lw:"llmMessageConversation,symbol"`
	App          string `lw:"llmMessageApp,symbol"`
	// Sensitivity is the call's label, on every row (§SD5).
	Sensitivity string `lw:"llmMessageSensitivity,symbol"`
	// Ordinal is the message's position in the conversation.
	Ordinal uint32 `lw:"llmMessageOrdinal,u32Array,unit"`
	// Role is system, user, assistant or tool.
	Role      string `lw:"llmMessageRole,symbol"`
	Content   string `lw:"llmMessageContent,textArray,unit"`
	Reasoning string `lw:"llmMessageReasoning,textArray,unit"`
	// ToolCallId is the call a tool message answers.
	ToolCallId string `lw:"llmMessageToolCallId,symbol"`
	// ToolCalls are an assistant message's calls, one JSON object each.
	ToolCalls []string `lw:"llmMessageToolCalls,stringArray"`
	// Images are the attached pictures as "<media type> <blake3> <bytes>";
	// the bytes are not kept.
	Images []string `lw:"llmMessageImages,stringArray"`
}
