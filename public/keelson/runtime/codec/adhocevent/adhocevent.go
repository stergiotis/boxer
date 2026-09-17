// Package adhocevent is the leeway-coded wire form of the ad-hoc dataset
// capability's announcements (ADR-0240 §SD8; ADR-0188 §SD3):
// `adhoc.event.published` on every publish and republish,
// `adhoc.event.retracted` at the leave step of a withdrawal. Events are
// hints — a consumer resolves to learn the truth — so the payload is the
// identity of what moved and nothing more.
//
// Vocabulary: the `adhoc…` cohort in vdd and the shared `appId` for the
// publisher.
package adhocevent

import "time"

// Op values; the subject carries the same fact, this is what a consumer
// decoding a mixed stream switches on.
const (
	OpPublished = "published"
	OpRetracted = "retracted"
)

// AdhocEvent is the flat wire form of one announcement.
type AdhocEvent struct {
	_ struct{} `kind:"adhocEvent"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the transition instant.
	At time.Time `lw:",ts"`

	// Op is OpPublished or OpRetracted.
	Op string `lw:"adhocEventOp,symbol"`

	// Handle and Alias name the dataset the transition concerns.
	Handle string `lw:"adhocHandle,stringArray"`
	Alias  string `lw:"adhocAlias,symbol"`

	// Publisher is the app that published it.
	Publisher string `lw:"appId,stringArray"`

	// Revision is the revision after a publish, or the last live revision
	// at a retract.
	Revision uint64 `lw:"adhocRevision,u64Array"`
}
