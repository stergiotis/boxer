//go:build llm_generated_opus47

// Package taskcreated is the leeway-coded wire form of the spawn
// payload published once per task on `task.<id>.created`. Second
// broker DTO to migrate off the buscodec fxamacker-cbor default
// onto the ADR-0042 SoA codec (after [keelson/runtime/codec/taskprogress]).
//
// Vocabulary reuse across task.* wire DTOs:
//
//   - [vdd.MembTaskId] — shared with [keelson/runtime/codec/taskprogress];
//     subject identifier carried by every task.* payload.
//   - [vdd.MembTitle], [vdd.MembAppId], [vdd.MembTileKey],
//     [vdd.MembRunId] — shared with future audit / event DTOs that
//     join back to runtime lifecycle.
//   - Narrow memberships (`taskKind`, `taskCancellableB`,
//     `taskEstimatedMs`) live in
//     `keelson/vdd/keelson_dimdata_taskcreated.go`.
//
// Wire shape vs the legacy task.TaskCreated JSON form:
//
//   - Field rename `Id TaskIdT` → `TaskId string` (subject id moves
//     into a string-section tagged column; the plain `id` slot is
//     the fact-row id).
//   - Field rename `AtMs` → `At` (codec plain `ts` is a `time.Time`;
//     producers convert `time.Now().UnixMilli()` via `time.UnixMilli`
//     at the wire boundary).
//   - New `FactId uint64` plain `id` (per-row event sequence; the
//     existing producer leaves it zero until a real sequencer lands —
//     matching the taskprogress precedent).
//   - Field rename `OwnerAppId app.AppIdT` → `OwnerAppId string`
//     (codec field type is plain string; callers cast with
//     `string(appId)` at construction).
//
// Cardinality is ExactlyOne for every tagged field; Go zero values
// carry absence semantics (Title="" ⇒ no title, EstimatedMs=0 ⇒ no
// estimate, OwnerTileKey=0 ⇒ no tile context). Mirrors capabilitygrant's
// ExpiresAt sentinel approach instead of `option.Option[T]`.
package taskcreated

import "time"

// TaskCreated is the wire payload published once at task spawn on
// subject `task.<id>.created`. Observers consume this to build their
// initial row before any TaskProgress arrives.
//
// `OwnerAppId` / `OwnerTileKey` / `OwnerRunId` are populated by the
// host-supplied TaskApi (via MountContextI.Tasks()) so audit rows can
// join back to runtime-start (RunId) and app-lifecycle (TileKey)
// facts rows. Direct callers of task.Spawn that bypass the host
// TaskApi may leave these zero/empty; the supervisor and observers
// treat them as best-effort metadata.
type TaskCreated struct {
	_ struct{} `kind:"taskCreated"`

	// FactId is the per-row event id; distinct from TaskId (the
	// subject of the fact).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// TaskId is the per-task identifier (a nanoid by default; see
	// task.TaskIdT). Shared string-section column across task.*
	// wire DTOs.
	TaskId string `lw:"taskId,stringArray"`

	// Kind is the task's domain category (e.g. "ch.export").
	// Symbol section because the value set is enumerable per
	// deployment.
	Kind string `lw:"taskKind,symbol"`

	// Title is a short human-readable label for the task.
	Title string `lw:"title,textArray"`

	// OwnerAppId is the runtime app namespace that spawned the task.
	// String section (app.AppIdT is opaque to the codec).
	OwnerAppId string `lw:"appId,stringArray"`

	// OwnerTileKey is the window/tile identifier for audit join-back
	// to app-lifecycle facts. 0 means "no tile context".
	OwnerTileKey uint64 `lw:"tileKey,u64Array"`

	// OwnerRunId is the runtime-start identifier (nanoid/uuid) for
	// audit join-back to runtime-lifecycle facts.
	OwnerRunId string `lw:"runId,stringArray"`

	// CancellableB declares whether the producer honours
	// task.<id>.cancel publications.
	CancellableB bool `lw:"taskCancellableB,bool"`

	// EstimatedMs is the producer-supplied predicted duration in
	// milliseconds. Zero means no estimate.
	EstimatedMs int64 `lw:"taskEstimatedMs,i64Array"`
}
