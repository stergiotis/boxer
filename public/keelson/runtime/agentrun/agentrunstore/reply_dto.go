package agentrunstore

// ReplyMeta is what a model call's reply records beside its content
// (ADR-0325 §SD1, ledger): the provider's call id, why it finished, and
// the tokens it took.
type ReplyMeta struct {
	_            struct{} `kind:"agentrunReply"`
	ID           string   `lw:",id"`
	CallId       string   `lw:"agentrunReplyCallId,stringArray,unit"`
	FinishReason string   `lw:"agentrunReplyFinishReason,symbol"`
	InputTokens  uint32   `lw:"agentrunReplyInputTokens,u32Array,unit"`
	OutputTokens uint32   `lw:"agentrunReplyOutputTokens,u32Array,unit"`
}
