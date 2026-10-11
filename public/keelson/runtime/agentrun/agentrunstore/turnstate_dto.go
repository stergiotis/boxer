package agentrunstore

// TurnState is a turn entry's record (ADR-0325 §SD1, ledger): how the turn
// ended, and why when it did not end answered.
type TurnState struct {
	_      struct{} `kind:"agentrunTurnState"`
	ID     string   `lw:",id"`
	State  string   `lw:"agentrunTurnState,symbol"`
	Reason string   `lw:"agentrunTurnReason,stringArray,unit"`
}
