package mddocfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// MdHeading is one heading of one ingested document. The tree is carried
// twice — Parent for a walk, Path for a filter — so a section query needs no
// recursive CTE.
type MdHeading struct {
	_ struct{} `kind:"mdHeading"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"mdHeadingKind,symbol"`

	// Doc is the document row's id; DocHash the document's blake3-256
	// content hash — the same bytes as that row's natural key.
	Doc     uint64 `lw:"mdHeadingDoc,foreignKey"`
	DocHash []byte `lw:"mdHeadingDocHash,blobArray,unit"`

	Ordinal uint64 `lw:"mdHeadingOrdinal,u64Array,unit"`
	Line    uint64 `lw:"mdHeadingLine,u64Array,unit"`
	Level   uint8  `lw:"mdHeadingLevel,u8Array,unit"`
	Text    string `lw:"mdHeadingText,stringArray,unit"`
	Slug    string `lw:"mdHeadingSlug,stringArray,unit"`
	// Anchor is the explicit `{#anchor}`, absent otherwise.
	Anchor option.Option[string] `lw:"mdHeadingAnchor,stringArray,unit"`
	// Parent is the enclosing heading's ordinal, absent at the top level.
	Parent option.Option[uint64] `lw:"mdHeadingParent,u64Array,unit"`
	// Path is the ancestors' texts, outermost first, self excluded.
	Path []string `lw:"mdHeadingPath,stringArray"`
}
