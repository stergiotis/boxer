package example

// LedgerNote is the one hand-written component of the evidence-profile
// fixture store: a text beside the profile's chain and written-by
// components.
type LedgerNote struct {
	_    struct{} `kind:"ledgerNote"`
	ID   uint64   `lw:",id"`
	Text string   `lw:"ledgerNoteText,stringArray,unit"`
}
