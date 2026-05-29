//go:build llm_generated_opus47

// Package taskdone is the leeway-coded wire form of the
// terminal-success payload published on `task.<id>.done`. Fifth and
// final broker DTO from the task package to migrate off the buscodec
// fxamacker-cbor default onto the ADR-0042 SoA codec.
//
// First in-tree consumer of the codec's scalar-blob grammar extension:
// the producer's `Result []byte` opaque payload routes to the blob
// section as a single variable-length value (not a slice of uint8).
// Per the boxer coding standard, `byte` is the blob spelling; sized
// integers stay in their own lane.
//
// Vocabulary:
//
//   - [vdd.MembTaskId] — shared subject identifier across all task.*
//     wire DTOs.
//   - [vdd.MembTaskResult] — narrow, blob section. The semantic
//     ("application-defined success payload") is task-specific, so
//     the membership stays kind-prefixed rather than landing in the
//     shared file as a generic `result`. A future RPC-reply DTO that
//     wants a generic result column can introduce its own term then.
//
// Wire shape vs the legacy task.TaskDone JSON form:
//
//   - `Id TaskIdT` → `TaskId string`.
//   - `AtMs` → `AtNs`.
//   - New `FactId uint64` plain `id`.
//   - `Result []byte` keeps the same Go shape; the codec now stores
//     it as a single variable-length blob column instead of a CBOR
//     bytes envelope. Empty Result reconstructs as an empty (non-nil)
//     slice — observers should always presence-check via length.
package taskdone

// TaskDone is the wire payload published once at task success on
// subject `task.<id>.done`. Result is opaque on the wire — observers
// decode by the originating TaskCreated.Kind.
type TaskDone struct {
	_ struct{} `kind:"taskDone"`

	// FactId is the per-row event id; distinct from TaskId (the
	// subject of the success).
	FactId uint64 `lw:",id"`

	// AtNs is the completion timestamp in unix nanoseconds; emitted
	// as u32 seconds on the wire.
	AtNs int64 `lw:",ts"`

	// TaskId names the task that completed.
	TaskId string `lw:"taskId,stringArray"`

	// Result is the application-defined success payload. Opaque to
	// the codec; observers decode by TaskCreated.Kind. Empty (nil or
	// len=0) when the task signals success without a payload.
	Result []byte `lw:"taskResult,blobArray"`
}
