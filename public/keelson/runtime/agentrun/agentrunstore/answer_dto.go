package agentrunstore

// ExitAnswer is an exit's answered record (ADR-0325 §SD1, vault): the
// exit, whether the person approved it, and why.
type ExitAnswer struct {
	_             struct{} `kind:"agentrunAnswer"`
	ID            string   `lw:",id"`
	Sink          string   `lw:"agentrunAnswerSink,symbol"`
	Tier          string   `lw:"agentrunAnswerTier,symbol"`
	Approved      bool     `lw:"agentrunAnswerApproved,bool"`
	Justification string   `lw:"agentrunAnswerJustification,textArray,unit"`
}
