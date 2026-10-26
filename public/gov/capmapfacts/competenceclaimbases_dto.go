package capmapfacts

// CompetenceClaimBases is the claims' bases: one attribute per anchor on the
// symbol section, the dimension as the parameter — the shape CompetenceTags
// uses for a list. Anchors repeat across competences, which is what the
// low-cardinality section is for.
type CompetenceClaimBases struct {
	_ struct{} `kind:"capmapCompetenceClaimBases"`

	ID uint64 `lw:",id"`

	Basis []ClaimBasis `lw:"capmapCompetenceClaimBasis,symbol,mixedLowCardRef"`
}

// ClaimBasis is one anchor of one claim's basis, as `kind:key`.
type ClaimBasis struct {
	Dimension string `lw:"@params"`
	Anchor    string
}
