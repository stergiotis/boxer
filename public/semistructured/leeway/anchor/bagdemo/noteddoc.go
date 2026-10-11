package bagdemo

// NotedSection is one attribute under the fixed membership noteSection on
// the mixed ref channel, its parameters riding beside it (ADR-0326 §SD1):
// the shape gov/capmapfacts writes by hand for a competence's sections,
// with numeric parameters.
type NotedSection struct {
	Heading []uint64 `lw:"@params"`
	Text    string
}

// NotedDoc carries its sections on the symbol section, which BagDoc and
// PlainDoc share.
type NotedDoc struct {
	_ struct{} `kind:"notedDoc"`

	ID uint64 `lw:",id"`

	Sections []NotedSection `lw:"noteSection,symbol,mixedLowCardRef"`
}
