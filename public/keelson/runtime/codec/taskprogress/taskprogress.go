// Package taskprogress is the leeway-coded wire form of the periodic
// progress payload published on `task.<id>.progress`. It is the first
// broker DTO to migrate off the buscodec default (fxamacker-cbor)
// onto the ADR-0042 SoA codec.
//
// Vocabulary split:
//   - Shared (cross-DTO) memberships [vdd.MembTaskId], [vdd.MembNote] —
//     defined once in keelson/vdd/keelson_dimdata_shared.go so future
//     task.* wire DTOs (TaskCreated/Done/Error/Cancel) can reuse the
//     same vocabulary entries.
//   - Narrow (progress-specific) memberships in
//     keelson/vdd/keelson_dimdata_taskprogress.go.
//
// Wire shape vs. the legacy task.TaskProgress JSON form:
//   - Field rename `AtMs` → `At`. The codec's plain `ts` column is a
//     `time.Time` (strict 1:1 with the facts SetTimestamp); producers
//     that historically captured `time.Now().UnixMilli()` convert via
//     `time.UnixMilli` at the codec boundary.
//   - `FactId uint64` synthesised plain `id` (event sequence). Distinct
//     from `TaskId string`, which identifies the *subject* (the task)
//     and is carried as a tagged-value column.
//
// Cardinality is ExactlyOne for every tagged field — the Go zero value
// carries semantics (Total=0 ⇒ indeterminate task; EtaMs=0 ⇒
// not-yet-computed; ThroughputPerSec=0.0 ⇒ first report). This mirrors
// capabilitygrant.ExpiresAt's "0 = no TTL" sentinel pattern instead of
// option.Option[T], keeping the wire compact for the common case.
package taskprogress

import "time"

// TaskProgress is the periodic progress payload broadcast by the
// task producer on subject `task.<id>.progress`. The estimator's
// humanized-change gate (see keelson/runtime/task/handle.go) decides
// when to emit; this struct carries the raw numbers + derived
// throughput/ETA so each observer humanizes per its own locale/font.
type TaskProgress struct {
	_ struct{} `kind:"taskProgress"`

	// FactId is the per-row event id (sequence number assigned by the
	// producer). It is the leeway-fact identifier, distinct from
	// TaskId (which names the *subject* of the fact).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// TaskId is the per-task identifier (a nanoid by default; see
	// task.TaskIdT). Carried as a string-section column so future
	// task.* DTOs can share the membership and join on this column.
	TaskId string `lw:"taskId,stringArray"`

	// Current is the work-units-completed counter at the At timestamp.
	Current uint64 `lw:"progressCurrent,u64Array"`

	// Total is the work-units denominator. Zero marks an
	// indeterminate task (count visible, end unknown).
	Total uint64 `lw:"progressTotal,u64Array"`

	// Unit is the canonical magnitude name ("items", "bytes",
	// "steps", or "unspecified") matching task.UnitE.String().
	// Encoded as a leeway symbol so the on-disk column is a
	// LowCardinality(String) dictionary.
	Unit string `lw:"progressUnit,symbol"`

	// ThroughputPerSec is the producer-side derived rate (Current per
	// second) over the estimator's sliding window. Zero means
	// not-yet-computed (early-frame reports).
	ThroughputPerSec float64 `lw:"progressThroughputPerSec,f64Array"`

	// EtaMs is the producer-side derived ETA-to-completion in
	// milliseconds. Zero means not-yet-computed; meaningless when
	// Total is zero.
	EtaMs int64 `lw:"progressEtaMs,i64Array"`

	// Note is a short free-text annotation appended to the humanized
	// rendering. Empty string means no annotation.
	Note string `lw:"note,textArray"`
}
