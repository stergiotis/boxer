// Package taskerror is the leeway-coded wire form of the
// terminal-failure payload published on `task.<id>.error`. Fourth
// broker DTO to migrate off the buscodec fxamacker-cbor default onto
// the ADR-0042 SoA codec.
//
// Every field maps to an existing shared vocabulary entry:
//
//   - [vdd.MembTaskId] — subject id (since taskprogress).
//   - [vdd.MembReason] — short failure rationale (since taskcancel).
//   - [vdd.MembErrorText] — rendered error chain; new shared term
//     introduced with this migration, reusable by future DTOs that
//     surface a captured error alongside their payload.
//
// Wire shape vs the legacy task.TaskError JSON form:
//
//   - `Id TaskIdT` → `TaskId string`.
//   - `AtMs` → `At` (codec plain `ts` is a `time.Time`; producers
//     convert via `time.UnixMilli` at the wire boundary).
//   - New `FactId uint64` plain `id`.
//   - `Error []byte` → `ErrorText string`. The producer captures
//     `eh.FormatErrorWithStackS(taskErr)` — already a UTF-8 multi-line
//     rendering — and the codec stores it in a text-section column.
//     Callers that previously read `e.Error` as `[]byte` now read
//     `e.ErrorText` as `string` (or `[]byte(e.ErrorText)` for the
//     legacy reader surface).
//
// When a structured eh.MarshalError CBOR/JSON envelope is eventually
// added to the wire, it lands as a separate column (e.g.
// `errorStructured`), not by changing ErrorText's section — text
// stays the human-readable column observers render directly.
package taskerror

import "time"

// TaskError is the wire payload published once at task failure on
// subject `task.<id>.error`. ErrorText carries the producer's
// FormatErrorWithStackS rendering of the failure (multi-line text);
// observers render it directly via the errorview widget.
type TaskError struct {
	_ struct{} `kind:"taskError"`

	// FactId is the per-row event id; distinct from TaskId (the
	// subject of the failure).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// TaskId names the task that failed.
	TaskId string `lw:"taskId,stringArray"`

	// Reason is a short human-readable rationale (often the
	// underlying error's .Error() short form). Empty when the
	// producer didn't supply one.
	Reason string `lw:"reason,textArray"`

	// ErrorText is the FormatErrorWithStackS rendering of the
	// captured error chain. Empty when the producer surfaced a
	// reason-only failure with no Go error attached.
	ErrorText string `lw:"errorText,textArray"`
}
