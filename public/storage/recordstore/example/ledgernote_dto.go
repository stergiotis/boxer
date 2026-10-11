package example

// LedgerNote is the evidence-profile fixture's vaulted component: a text
// the vault holds behind a commitment on the ledger row (gen.Evidence.Vault).
type LedgerNote struct {
	_    struct{} `kind:"ledgerNote"`
	ID   uint64   `lw:",id"`
	Text string   `lw:"ledgerNoteText,stringArray,unit"`
}
