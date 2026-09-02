package mddocfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// MdEmphasis is one styled span of one ingested document.
type MdEmphasis struct {
	_ struct{} `kind:"mdEmphasis"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"mdEmphasisKind,symbol"`

	Doc     uint64 `lw:"mdEmphasisDoc,foreignKey"`
	DocHash []byte `lw:"mdEmphasisDocHash,blobArray,unit"`

	Ordinal uint64                `lw:"mdEmphasisOrdinal,u64Array,unit"`
	Line    uint64                `lw:"mdEmphasisLine,u64Array,unit"`
	Section option.Option[uint64] `lw:"mdEmphasisSection,u64Array,unit"`

	// Style is bold, italic, highlight or strikethrough.
	Style string `lw:"mdEmphasisStyle,symbol"`
	Text  string `lw:"mdEmphasisText,stringArray,unit"`
}
