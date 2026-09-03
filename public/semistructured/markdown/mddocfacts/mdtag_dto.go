package mddocfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// MdTag is one tag occurrence of one ingested document, from the body or
// from the frontmatter `tags` property. Nesting is kept in Name
// (`project/alpha`); a parent tag's children are a prefix match.
type MdTag struct {
	_ struct{} `kind:"mdTag"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"mdTagKind,symbol"`

	Doc     uint64 `lw:"mdTagDoc,foreignKey"`
	DocHash []byte `lw:"mdTagDocHash,blobArray,unit"`

	Ordinal uint64 `lw:"mdTagOrdinal,u64Array,unit"`
	// Line is 0 for a frontmatter tag.
	Line    uint64                `lw:"mdTagLine,u64Array,unit"`
	Section option.Option[uint64] `lw:"mdTagSection,u64Array,unit"`

	// Source is body or frontmatter.
	Source string `lw:"mdTagSource,symbol"`
	Name   string `lw:"mdTagName,symbol"`
}
