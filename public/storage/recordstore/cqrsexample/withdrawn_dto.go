package cqrsexample

// Withdrawn is the funds-removed event; see opened_dto.go for the
// component-file layout rationale.
type Withdrawn struct {
	_      struct{} `kind:"acctWithdrawn"`
	ID     string   `lw:",id"`
	Amount uint64   `lw:"ledgerWithdraw,acctWithdraw,unit"`
}
