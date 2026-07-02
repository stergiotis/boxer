package cqrsexample

// Opened, Deposited (deposited_dto.go), Withdrawn (withdrawn_dto.go),
// Closed (closed_dto.go) and AccountState (accountstate_dto.go) are the
// ledger components: the first four are event payloads (the row's
// archetype IS the event type), the fifth is the snapshot. One kind per
// source file (marshallgen.ParsePlan reads a single kind per input);
// each owns distinct sections so the per-kind membership ids cannot
// collide in storage.
//
// Opened is the account-creation event.
type Opened struct {
	_     struct{} `kind:"acctOpened"`
	ID    string   `lw:",id"`
	Owner string   `lw:"ledgerOwner,acctOwner"`
}
