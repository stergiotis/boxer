//go:build llm_generated_opus47

// Package taskcancel is the leeway-coded wire form of the
// cancellation request published on `task.<id>.cancel`. Third broker
// DTO to migrate off the buscodec fxamacker-cbor default onto the
// ADR-0042 SoA codec (after [keelson/runtime/codec/taskprogress]
// and [keelson/runtime/codec/taskcreated]).
//
// Vocabulary reuse:
//
//   - [vdd.MembTaskId] — shared subject identifier across all task.*
//     wire DTOs.
//   - [vdd.MembReason] — shared free-text rationale column; will be
//     reused by TaskError and future audit DTOs that carry a
//     "why this happened" annotation.
//
// Wire shape vs the legacy task.TaskCancel JSON form:
//
//   - `Id TaskIdT` → `TaskId string` (subject id moves out of the
//     plain `id` slot into a string-section tagged column).
//   - `AtMs` → `AtNs` (codec plain `ts` is nanoseconds; producers
//     multiply UnixMilli by 1e6 at the wire boundary).
//   - New `FactId uint64` plain `id` (per-row event sequence;
//     currently left zero — see the TaskProgress migration entry for
//     the producer-side sequencer follow-up).
//
// The legacy "empty payload yields zero TaskCancel" interop hook
// stays in [task.UnmarshalTaskCancel] — bypassing the codec for the
// nil-payload case keeps cancel-with-no-reason callers
// wire-compatible across the migration.
package taskcancel

// TaskCancel is the wire payload a consumer publishes on
// `task.<id>.cancel`. The producer's handle subscribes to this and
// cancels its internal context when one arrives.
type TaskCancel struct {
	_ struct{} `kind:"taskCancel"`

	// FactId is the per-row event id; distinct from TaskId (the
	// subject of the cancel request).
	FactId uint64 `lw:",id"`

	// AtNs is the cancellation timestamp in unix nanoseconds;
	// emitted as u32 seconds on the wire.
	AtNs int64 `lw:",ts"`

	// TaskId names the task whose cancellation is being requested.
	TaskId string `lw:"taskId,stringArray"`

	// Reason is a short human-readable rationale for the cancel
	// (e.g. "user clicked cancel", "deadline exceeded"). Empty
	// string means no rationale was supplied.
	Reason string `lw:"reason,textArray"`
}
