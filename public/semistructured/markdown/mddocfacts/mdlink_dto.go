package mddocfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// MdLink is one outgoing reference of one ingested document — the edge list
// a graph or a backlink panel is built from. Targets are as written; matching
// them to documents (by file stem, path or alias) is the reader's join.
type MdLink struct {
	_ struct{} `kind:"mdLink"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"mdLinkKind,symbol"`

	Doc     uint64 `lw:"mdLinkDoc,foreignKey"`
	DocHash []byte `lw:"mdLinkDocHash,blobArray,unit"`

	Ordinal uint64                `lw:"mdLinkOrdinal,u64Array,unit"`
	Line    uint64                `lw:"mdLinkLine,u64Array,unit"`
	Section option.Option[uint64] `lw:"mdLinkSection,u64Array,unit"`

	// Spelling is wikilink, embed, inline, image or autolink.
	Spelling string `lw:"mdLinkSpelling,symbol"`
	// Target is the destination without its fragment; Fragment the
	// `#heading` part; Text the alias or label as written.
	Target   string `lw:"mdLinkTarget,stringArray,unit"`
	Fragment string `lw:"mdLinkFragment,stringArray,unit"`
	Text     string `lw:"mdLinkText,stringArray,unit"`
	// External is true when Target carries a URL scheme. Plain bool: the
	// bool section has no single-value cardinality (the ladingmeta note).
	External bool `lw:"mdLinkExternal,bool"`
}
