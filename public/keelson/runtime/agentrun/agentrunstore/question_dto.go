package agentrunstore

// ExitQuestion is an exit's asked record (ADR-0325 §SD1, vault): the
// question put to the person and the exit it concerns.
type ExitQuestion struct {
	_        struct{} `kind:"agentrunQuestion"`
	ID       string   `lw:",id"`
	Question string   `lw:"agentrunQuestionText,textArray,unit"`
	Sink     string   `lw:"agentrunQuestionSink,symbol"`
	Tier     string   `lw:"agentrunQuestionTier,symbol"`
	Path     string   `lw:"agentrunQuestionPath,symbol"`
}
