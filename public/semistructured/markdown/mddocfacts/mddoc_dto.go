package mddocfacts

import "time"

// MdDoc is one sent markdown document.
type MdDoc struct {
	_ struct{} `kind:"mdDoc"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind carries the kind label for readability; the membership id is what
	// identifies the kind (the sysmfacts convention).
	Kind string `lw:"mddocKind,symbol"`

	// Title is the document's first heading text, "" when it has none.
	Title string `lw:"mddocTitle,stringArray,unit"`

	// FileName is the document's display name: from an editor, the fs
	// Powerbox name — a basename — or "" for a scratch document; from the
	// vault ingestor, the path relative to the directory ingested, forward
	// slashes, so a `[[folder/note]]` target matches it.
	FileName string `lw:"mddocFileName,stringArray,unit"`

	// Content is the markdown source, verbatim.
	Content string `lw:"mddocContent,textArray,unit"`

	// ContentHash is the blake3-256 of Content, hex — the natural key's
	// material as a queryable column.
	ContentHash string `lw:"mddocContentHash,stringArray,unit"`

	// Words is the sender's prose word count (markup excluded).
	Words uint64 `lw:"mddocWords,u64Array,unit"`
}
