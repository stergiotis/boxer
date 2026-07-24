package marshallreflect_test

// parityRejectBareSlice: a `[]S` section field whose tag is a bare section name
// (no membership) and whose element carries `<section>:<column>` value tags but
// no `@membership` field — a flat tuple missing its membership. BOTH front-ends
// must reject it, with the SAME message: goplan.ClassifySliceSection routes a
// membership-less bare-section `[]S` to the flat tuple builder precisely so the
// error names what is missing, rather than the nested builder complaining that
// `text:text` is not a bare sub-column name.
//
// This pins the routing rule itself. Before it was hoisted into goplan the two
// front-ends decided differently here — reflect consulted the outer tag, the
// AST side did not — so the same DTO was rejected by two different builders
// with two unrelated messages.
type parityRejectBareSliceElem struct {
	Text string `lw:"text:text"`
}

type parityRejectBareSlice struct {
	_     struct{}                    `kind:"parityRejectBareSlice"`
	ID    uint64                      `lw:",id"`
	Texts []parityRejectBareSliceElem `lw:"text"`
}
