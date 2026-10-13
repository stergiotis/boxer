package capmapfacts

// CompetenceTags is a competence's triage tags: one attribute each, all
// wearing the same membership. A tag set is a small vocabulary applied
// across the corpus, which is what the low-cardinality symbol section is
// for, and one attribute per tag is what makes "every competence tagged X"
// an indexOf rather than a string scan.
type CompetenceTags struct {
	_ struct{} `kind:"capmapCompetenceTags"`

	ID uint64 `lw:",id"`

	Tags []CompetenceTag `lw:"capmapCompetenceTag,symbol"`
}

// CompetenceTag is one tag.
type CompetenceTag struct {
	Value string
}
