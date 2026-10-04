package trail

// LlmMessageBody is the text of a kept message (ADR-0264, ADR-0277 §SD4): a
// component added to the message's audit row, never a row of its own. It is
// the only component of a message row on the text section, so ditching kept
// text clears that section and leaves the audit ([DitchBodiesSQL]).
type LlmMessageBody struct {
	_ struct{} `kind:"llmMessageBody"`

	Id        uint64 `lw:",id"`
	Content   string `lw:"llmMessageContent,textArray,unit"`
	Reasoning string `lw:"llmMessageReasoning,textArray,unit"`
	// ToolCalls are an assistant message's calls with their arguments, one
	// JSON object each.
	ToolCalls []string `lw:"llmMessageToolCalls,textArray"`
}
