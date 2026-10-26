package capmapfacts

import "time"

// CompetenceClaims is a competence's assessment on its facts row (ADR-0337
// §SD2, §SD7): one claim per dimension, each field riding a membership of
// its own with the dimension as the membership's parameter. The parameter is
// what groups a claim's fields — the rating under `readiness` goes with the
// assessor, the date and the basis anchors under `readiness` — so no claim
// ordinal is stored.
//
// A dynamic-membership tuple owns its section within one DTO (ADR-0103), so
// the fields spread over three sections: the rating on the symbol section,
// the assessor on the string section, the date on the time section. The basis
// is the fourth tuple and the second to want the symbol section, which is
// why it is a component of its own ([CompetenceClaimBases]) on the same row,
// as the tags are.
//
// Problems are not stored: they are the lint's reading of the claim,
// recomputed on every parse.
type CompetenceClaims struct {
	_ struct{} `kind:"capmapCompetenceClaims"`

	ID uint64 `lw:",id"`

	Rating []ClaimRating `lw:"capmapCompetenceClaimRating,symbol,mixedLowCardRef"`
	By     []ClaimBy     `lw:"capmapCompetenceClaimBy,stringArray,mixedLowCardRef"`
	At     []ClaimAt     `lw:"capmapCompetenceClaimAt,timeArray,mixedLowCardRef"`
}

// ClaimRating is one claim's rating, as the vault spells it.
type ClaimRating struct {
	Dimension string `lw:"@params"`
	Rating    string
}

// ClaimBy is who made one claim.
type ClaimBy struct {
	Dimension string `lw:"@params"`
	By        string `lw:"value,unit"`
}

// ClaimAt is when one claim was made.
type ClaimAt struct {
	Dimension string    `lw:"@params"`
	At        time.Time `lw:"value,unit"`
}
