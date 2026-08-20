package ladingdata

import "time"

// LadingBlock is one block of one file's content, as a facts-shaped row of
// `boxer.fsdata` (ADR-0198 §SD2, §SD5).
//
// Blocks are unshared: one block belongs to exactly one file in exactly one
// snapshot, which is what lets TTL express retention exactly and is why there
// is no reference counting anywhere in the store (§SD1).
//
// The block ordinal rides the natural key as a suffix — `path ‖ 0x00 ‖
// be32(seq)` — so a file's blocks are one contiguous key range and no
// `boxer.facts` migration is needed. That encoding was chosen at M0 over a
// routing plain and over leeway value cardinality (ADR-0198 SD11, decided
// 2026-08-19). 0x00 cannot occur in an io/fs path, so the split is
// unambiguous; big-endian so the byte order of the suffix is the numeric
// order of the ordinal.
type LadingBlock struct {
	_ struct{} `kind:"ladingBlock"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"ladingKindBlock,symbol"`

	// Data is the block's bytes. In the corpus profile one block is one mark
	// and one compressed block, so reading it costs exactly its own bytes
	// (measured at M0: a 1 MiB block reads 1.00 MiB compressed).
	Data []byte `lw:"ladingData,blobArray,unit"`
	// Hash is this block's BLAKE3 digest in the corpus profile, empty in the
	// fleet profile. `BLAKE3(data) != hash` is the SQL audit it exists for.
	Hash []byte `lw:"ladingBlockHash,blobArray,unit"`
	// Line0 is the 1-based line number of the block's first line, for a text
	// block; zero otherwise. It is what lets a grep-shaped query report a
	// line number without reading every earlier block.
	Line0 uint32 `lw:"ladingLine0,u32Array,unit"`
}
