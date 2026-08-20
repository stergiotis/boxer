package ladingmeta

import "time"

// LadingSnapshot is the commit record of one snapshot: the totals of the walk
// and the policy it actually applied (ADR-0198 §SD6).
//
// It rides the *same row* as the root [LadingEntry] — `naturalKey = '.'` —
// rather than a row of its own, because the root is a node like any other and
// has to Stat. A snapshot is complete exactly when a row carries this
// component, which is why the walker writes it last and in a later insert
// than the batch's other rows: a failed or running walk has no such row, is
// invisible to every query, and is removed by TTL with nothing to clean up.
//
// The applied policy is recorded here as well as in the mount's policy record
// because the record is mutable runtime state and a snapshot has to stay
// interpretable after it changes.
type LadingSnapshot struct {
	_ struct{} `kind:"ladingSnapshot"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"ladingKindSnapshot,symbol"`

	// Entries counts every node the walk wrote, the root included; Bytes sums
	// their Size. Both are of the snapshot, not of the store.
	Entries uint64 `lw:"ladingSnapEntries,u64Array,unit"`
	Bytes   uint64 `lw:"ladingSnapBytes,u64Array,unit"`

	// The policy as applied: the retention class the expiry was computed
	// from, the text rule the cutting followed, and the threshold above which
	// content was left as a reference.
	TtlClass  string `lw:"ladingTtlClass,symbol"`
	TextRule  string `lw:"ladingTextRule,symbol"`
	InlineMax uint64 `lw:"ladingInlineMax,u64Array,unit"`
}
