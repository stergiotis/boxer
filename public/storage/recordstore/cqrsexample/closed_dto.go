package cqrsexample

// Closed is the account-termination event — a domain event, not a
// storage tombstone (the ledger schema has no lifecycle column). See
// opened_dto.go for the component-file layout rationale.
type Closed struct {
	_      struct{} `kind:"acctClosed"`
	ID     string   `lw:",id"`
	Reason string   `lw:"ledgerClosed,acctClosed"`
}
