package capmapfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../tags)\" -run TestGenerateCapmapCodecs ."

// CompetenceRecord is a competence's own attributes on its facts row: the
// kind marker, metadata and scores, its prose sections on the text section
// and its lifecycle times on the time section. The tags and the lifecycle's
// who share the symbol section with the metadata, so they are components
// of their own (CompetenceTags, CompetenceLifecycle) on the same row.
//
// Empty text is an absence, not a value: writing "" would put a
// meaningless entry in a dictionary-encoded column and make "has no owner"
// indistinguishable from "owner is the empty string". Scores are written
// unconditionally, sentinel included: "not assessed" is a value to select
// on.
type CompetenceRecord struct {
	_ struct{} `kind:"capmapCompetence"`

	ID uint64 `lw:",id"`
	// The kind marker's value repeats the kind as text so a row is legible
	// without resolving membership ids; the membership is what identifies it.
	_ struct{} `lw:"capmapKindCompetence,symbol,const=competence"`

	Slug    string                `lw:"capmapCompetenceSlug,symbol"`
	Domain  option.Option[string] `lw:"capmapCompetenceDomain,symbol"`
	Catalog option.Option[string] `lw:"capmapCompetenceCatalog,symbol"`
	Owner   option.Option[string] `lw:"capmapCompetenceOwner,symbol"`

	Name      option.Option[string] `lw:"capmapCompetenceName,stringArray,unit"`
	Abbrev    option.Option[string] `lw:"capmapCompetenceAbbrev,stringArray,unit"`
	Synopsis  option.Option[string] `lw:"capmapCompetenceSynopsis,stringArray,unit"`
	VaultPath option.Option[string] `lw:"capmapCompetenceVaultPath,stringArray,unit"`

	Level    uint8 `lw:"capmapCompetenceLevel,u8Array,unit"`
	Maturity uint8 `lw:"capmapCompetenceMaturity,u8Array,unit"`
	Pain     uint8 `lw:"capmapCompetencePain,u8Array,unit"`

	Sections    []CompetenceSection `lw:"capmapCompetenceSection,textArray,mixedLowCardRef"`
	LifecycleAt []CompetencePhaseAt `lw:"capmapCompetenceLifecycleAt,timeArray,mixedLowCardRef"`
}

// CompetenceSection is one prose section. The heading rides as the
// membership's parameter because headings are authored text and cannot be
// registered names (ADR-0168 §SD5).
type CompetenceSection struct {
	Heading string `lw:"@params"`
	Text    string `lw:"value,unit"`
}

// CompetencePhaseAt is when a lifecycle phase was recorded, the phase
// riding as the parameter.
type CompetencePhaseAt struct {
	Phase string    `lw:"@params"`
	At    time.Time `lw:"value,unit"`
}
