package cqrsexample

// Deposited is the funds-added event; see opened_dto.go for the
// component-file layout rationale.
type Deposited struct {
	_      struct{} `kind:"acctDeposited"`
	ID     string   `lw:",id"`
	Amount uint64   `lw:"ledgerDeposit,acctDeposit,unit"`
}
