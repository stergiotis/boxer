// Package adhocrequest is the leeway-coded wire form of a request to the
// ad-hoc dataset capability (ADR-0240 §SD8): `adhoc.publish`,
// `adhoc.resolve` and `adhoc.retract` share one kind under an op symbol,
// the watchbill shape. A verb leaves the fields it does not use zero:
// publish carries alias, an optional handle to republish onto, the
// keep-after-close flag and the stream; resolve carries the alias and an
// optional bound handle to verify; retract carries the handle.
//
// The publisher is never a field — it is the envelope's sender and
// instance (ADR-0240 §SD2).
//
// Vocabulary: the `adhoc…` cohort in vdd.
package adhocrequest

import "time"

// Op values.
const (
	OpPublish = "publish"
	OpResolve = "resolve"
	OpRetract = "retract"
)

// AdhocRequest is the flat wire form of one request.
type AdhocRequest struct {
	_ struct{} `kind:"adhocRequest"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the request instant.
	At time.Time `lw:",ts"`

	// Op is the verb: OpPublish, OpResolve or OpRetract.
	Op string `lw:"adhocReqOp,symbol"`

	// Alias is the stable alias (publish, resolve).
	Alias string `lw:"adhocAlias,symbol"`

	// Handle names a dataset: the one to republish onto, the bound one a
	// resolve verifies, the one to retract. Empty where not applicable.
	Handle string `lw:"adhocHandle,stringArray"`

	// KeepAfterClose marks a publish as the app's rather than the window's.
	KeepAfterClose bool `lw:"adhocKeepAfterClose,bool"`

	// ArrowStream is the Arrow IPC stream a publish carries.
	ArrowStream []byte `lw:"adhocArrowStream,blobArray"`
}
