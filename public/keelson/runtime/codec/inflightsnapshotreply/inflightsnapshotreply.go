//go:build llm_generated_opus47

// Package inflightsnapshotreply is the leeway-coded wire form of the
// supervisor's list-inflight reply payload. Final task.* DTO in the
// broker-DTO migration cohort.
//
// First **list-of-structs** migration. The leeway codec is
// one-fact-kind-per-row and doesn't natively model a list-of-structs;
// `[]task.InflightSnapshotEntry` flattens into parallel `[]T`
// columns, one per entry-field. The wrapper kind carries one row per
// reply with N attributes per parallel-array column (one per entry).
//
// Slice-order preservation: the codec emits Arbitrary `[]T` fields in
// declared slice-index order and the read path appends back in the
// same order, so entries zip correctly by index on reconstruction.
// All eleven memberships are distinct (each `inflight…` is its own
// vdd entry) — the read-side classifier separates the parallel
// streams even where multiple fields share the same physical section
// (`stringArray` carries Ids + OwnerAppIds; `symbol` carries Kinds +
// States + Units; `i64Array` carries CreatedAtMs + LastEmitMs + EtaMs;
// `u64Array` carries Current + Total).
//
// Wire shape vs the legacy task.InflightSnapshotReply JSON form:
//
//   - `Entries []InflightSnapshotEntry` → eleven parallel `[]T`
//     fields (Ids/Kinds/Titles/OwnerAppIds/States/CreatedAtMss/
//     LastEmitMss/Currents/Totals/Units/EtaMss). The supervisor's
//     existing `task.InflightSnapshotReply` Go shape stays unchanged;
//     translation happens at the codec boundary in
//     [task.MarshalInflightSnapshotReply] / `UnmarshalInflightSnapshotReply`.
//   - `AtMs` → `AtNs` (codec plain `ts` is nanoseconds; producers
//     multiply UnixMilli by 1e6 at the wire boundary).
//   - New `FactId uint64` plain `id`.
package inflightsnapshotreply

// InflightSnapshotReply is the flat parallel-array wire form of the
// supervisor's list-inflight reply. All eleven entry-field slices
// MUST have the same length; the boundary translation in
// task/inflight.go enforces this on Marshal and assumes it on
// Unmarshal.
type InflightSnapshotReply struct {
	_ struct{} `kind:"inflightSnapshotReply"`

	FactId uint64 `lw:",id"`

	// AtNs is the snapshot-sampling timestamp in unix nanoseconds.
	AtNs int64 `lw:",ts"`

	// Per-entry parallel arrays. Each slice's element at index i
	// describes the i-th in-flight task.
	Ids          []string `lw:"inflightTaskId,stringArray"`
	Kinds        []string `lw:"inflightTaskKind,symbolArray"`
	Titles       []string `lw:"inflightTitle,textArray"`
	OwnerAppIds  []string `lw:"inflightAppId,stringArray"`
	States       []string `lw:"inflightState,symbolArray"`
	CreatedAtMss []int64  `lw:"inflightCreatedAtMs,i64Array"`
	LastEmitMss  []int64  `lw:"inflightLastEmitMs,i64Array"`
	Currents     []uint64 `lw:"inflightCurrent,u64Array"`
	Totals       []uint64 `lw:"inflightTotal,u64Array"`
	Units        []string `lw:"inflightUnit,symbolArray"`
	EtaMss       []int64  `lw:"inflightEtaMs,i64Array"`
}
