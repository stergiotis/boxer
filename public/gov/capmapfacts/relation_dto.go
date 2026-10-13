package capmapfacts

import (
	"github.com/stergiotis/boxer/public/functional/option"
)

// RelationRecord is one relation's facts row.
//
// The target appears twice on purpose: as a foreign key when it resolved to
// a competence, and always as text. A broken link and a citation have a
// target worth recording and no row to point at, so the text column is the
// one that is never empty.
type RelationRecord struct {
	_ struct{} `kind:"capmapRelation"`

	ID uint64   `lw:",id"`
	_  struct{} `lw:"capmapKindRelation,symbol,const=relation"`

	RelKind    string                `lw:"capmapRelationKind,symbol"`
	Resolution string                `lw:"capmapRelationResolution,symbol"`
	TargetText string                `lw:"capmapRelationTargetText,symbol"`
	Section    option.Option[string] `lw:"capmapRelationSection,symbol"`

	Source uint64                `lw:"capmapRelationSource,foreignKey"`
	Target option.Option[uint64] `lw:"capmapRelationTarget,foreignKey"`

	Ncd option.Option[float64] `lw:"capmapRelationNcd,f64Array,unit"`
}
