package ladingmeta

import "time"

// LadingEntry is one node of one snapshot of one mount, as a facts-shaped row
// of `boxer.fsmeta` (ADR-0198 §SD2).
//
// The backbone is the whole key: the envelope's Id is the mount's tagged id,
// Ts is the snapshot, NaturalKey is the io/fs path and ExpiresAt is the row's
// expiry — so nothing here repeats them, and `(mount, snapshot, path)` needs
// no extraction to be a key range (§SD3).
//
// Every attribute is `unit` or a scalar shape, which is what keeps the three
// component refusals off this kind. Text is plain `bool` and not `bool,unit`:
// the bool section has no Single cardinality, and the `,unit` spelling emits
// code that does not compile (measured, ADR-0198 `## Updates` 2026-08-19).
type LadingEntry struct {
	_ struct{} `kind:"ladingEntry"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what identifies the row
	// as an entry. It is what the fs() expansion and the fssnap view select
	// on without enumerating attributes.
	Kind string `lw:"ladingKindEntry,symbol"`

	// NodeKind is `file`, `dir`, `symlink` or `other`.
	NodeKind string `lw:"ladingNodeKind,symbol"`
	// Content is `none`, `blocks` or `ref` (§SD5).
	Content string `lw:"ladingContent,symbol"`

	// Mode is fs.FileMode's bits verbatim, type bits included, as Lstat
	// reported them — the store never follows a link.
	Mode uint32 `lw:"ladingMode,u32Array,unit"`
	// BlockSize and Blocks describe how Content was cut. Both are zero where
	// Content is not `blocks`.
	BlockSize uint32 `lw:"ladingBlockSize,u32Array,unit"`
	Blocks    uint32 `lw:"ladingBlocks,u32Array,unit"`

	Size uint64 `lw:"ladingSize,u64Array,unit"`

	// Mtime is the source's, at the source's resolution — never rounded.
	Mtime time.Time `lw:"ladingMtime,timeArray,unit"`

	// LinkTarget is a symlink's target verbatim and unresolved; empty
	// otherwise.
	LinkTarget string `lw:"ladingLinkTarget,stringArray,unit"`
	// Err is this node's walk error, empty where there was none. A tree with
	// an unreadable directory still snapshots (§SD6).
	Err string `lw:"ladingErr,stringArray,unit"`

	// ContentHash is the file's BLAKE3 digest; empty where Content is `none`.
	ContentHash []byte `lw:"ladingContentHash,blobArray,unit"`

	// Text records that the content was cut at newlines rather than at a
	// fixed offset, which is what makes a line-oriented query over its blocks
	// boundary-safe.
	Text bool `lw:"ladingText,bool"`
}
