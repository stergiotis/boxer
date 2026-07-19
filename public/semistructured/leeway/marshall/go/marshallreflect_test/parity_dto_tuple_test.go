package marshallreflect_test

// parityTupleTag / parityTupleDoc: the flat dynamic-membership tuple
// (ADR-0103) — each slice element emits one attribute carrying its own
// membership. Parsed AND compiled — see parity_corpus_test.go.
type parityTupleTag struct {
	Label []byte `lw:"@membership,verbatim"`
	Value string `lw:"symbol"`
}

type parityTupleDoc struct {
	_     struct{}         `kind:"parityTupleDoc"`
	ID    uint64           `lw:",id"`
	Track []byte           `lw:",naturalKey"`
	Tags  []parityTupleTag `lw:"symbol"`
}
