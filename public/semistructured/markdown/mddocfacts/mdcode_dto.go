package mddocfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// MdCodeBlock is one fenced code block of one ingested document.
type MdCodeBlock struct {
	_ struct{} `kind:"mdCodeBlock"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"mdCodeKind,symbol"`

	Doc     uint64 `lw:"mdCodeDoc,foreignKey"`
	DocHash []byte `lw:"mdCodeDocHash,blobArray,unit"`

	Ordinal uint64 `lw:"mdCodeOrdinal,u64Array,unit"`
	// Line is the opening fence's line.
	Line uint64 `lw:"mdCodeLine,u64Array,unit"`
	// Section is the ordinal of the heading the block sits under.
	Section option.Option[uint64] `lw:"mdCodeSection,u64Array,unit"`

	// Language is the info string's first word, "" for a bare fence; Info
	// is the whole info string.
	Language string `lw:"mdCodeLanguage,symbol"`
	Info     string `lw:"mdCodeInfo,stringArray,unit"`
	Content  string `lw:"mdCodeContent,textArray,unit"`
	Lines    uint64 `lw:"mdCodeLines,u64Array,unit"`
}
