package example

// SeqReading is the one component of the u64-Order fixture store: a single
// verbatim-membership value beside the mandatory plain id binding.
type SeqReading struct {
	_     struct{} `kind:"seqReading"`
	ID    uint64   `lw:",id"`
	Value uint64   `lw:"reading,measure,lowCardVerbatim"`
}
