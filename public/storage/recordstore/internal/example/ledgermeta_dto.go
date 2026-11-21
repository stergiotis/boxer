package example

// LedgerMeta is the evidence-profile fixture's one ledger-side component:
// a label that stays beside the chain, where the note is the vault's.
type LedgerMeta struct {
	_     struct{} `kind:"ledgerMeta"`
	ID    uint64   `lw:",id"`
	Label string   `lw:"ledgerMetaLabel,symbol"`
}
