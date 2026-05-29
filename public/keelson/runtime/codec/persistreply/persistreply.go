//go:build llm_generated_opus47

// Package persistreply is the leeway-coded wire form of the
// runtime.persist reply payload.
//
// Vocabulary:
//
//   - [vdd.MembPersistFound] — narrow bool. Meaningful only on Get
//     replies; Set/Delete replies leave it false.
//   - [vdd.MembPersistValue] — narrow blob (variable-length opaque
//     payload via the codec's scalar-blob grammar). Empty when Found
//     is false or when the op was not a Get.
//   - [vdd.MembReason] — shared text. Empty on success; carries the
//     backend's error message on any failure. Joined with TaskCancel
//     / TaskError / WatchReply / GrantReply through the same
//     cross-DTO column.
//
// The Go-level [persist.PersistReply] keeps its existing shape
// (Found/Value/Error); the codec DTO is the wire-side projection
// only. Conversion lives in `persist.MarshalReply` /
// `UnmarshalReply`. Field rename at the boundary: `Error` (Go) →
// `reason` (wire) — semantically the same short failure rationale,
// joined with the other reason-bearing DTOs through the shared
// vocabulary term.
package persistreply

// PersistReply is the flat wire form of a runtime.persist reply.
type PersistReply struct {
	_ struct{} `kind:"persistReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// AtNs is the reply timestamp in unix nanoseconds; stamped at
	// marshal time inside persist.MarshalReply.
	AtNs int64 `lw:",ts"`

	// Found is true when a Get located the requested key. False
	// for missing keys on Get, and for Set / Delete replies in
	// general.
	Found bool `lw:"persistFound,bool"`

	// Value is the opaque payload returned by a successful Get.
	// Empty otherwise. Scalar-blob column — see the TaskDone
	// migration entry in ADR-0042 for the grammar extension.
	Value []byte `lw:"persistValue,blobArray"`

	// Reason carries the backend's error message on failure. Empty
	// on success.
	Reason string `lw:"reason,textArray"`
}
