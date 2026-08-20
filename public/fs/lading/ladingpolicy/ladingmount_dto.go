package ladingpolicy

import "time"

// LadingMount is one mount's declared policy, as a row of `boxer.facts`
// (ADR-0198 §SD2).
//
// It is the one kind of the four that does not live on the store's own
// tables, and deliberately so: a mount's policy is runtime state — it is
// edited, it outlives every snapshot taken under it, and it is not retained
// by the store's TTL. The snapshot rows record the policy they *applied*
// under their own memberships, so a snapshot stays interpretable after this
// record changes.
//
// The Id is the mount's tagged id, minted by the application under its own
// tag; the store claims no tag and mints nothing (§SD3). NaturalKey is left
// to the writer — a mount has one policy, so there is nothing to distinguish
// within a key.
type LadingMount struct {
	_ struct{} `kind:"ladingMount"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"ladingKindMount,symbol"`

	// Name is the mount's human name. Resolving a name to an id belongs to
	// the application or to whatever registers the mount; the store accepts
	// ids, and this field is what a name-as-sugar macro would read.
	Name string `lw:"ladingMountName,stringArray,unit"`
	// Store names which set of tables the mount's rows live in — the unit a
	// capability grant covers.
	Store string `lw:"ladingMountStore,symbol"`

	// TtlClass is a retention class, not a free duration, and whole days
	// only: an expiry inside a day would leave a partition partially expired,
	// which `ttl_only_drop_parts = 1` never clears on a background merge
	// (measured, ADR-0198 `## Updates` 2026-08-19).
	TtlClass string `lw:"ladingMountTtlClass,symbol"`
	// TextRule decides which files are cut at newlines; InlineMax is the size
	// above which content is left as a reference rather than stored.
	TextRule  string `lw:"ladingMountTextRule,symbol"`
	InlineMax uint64 `lw:"ladingMountInlineMax,u64Array,unit"`
}
