//go:build llm_generated_opus47

// Package errkind is the ADR-0042 M5 retrofit of the
// rowmarshall.Error hand-coded writer (ADR-0041's parallel-array
// shredded shape). The DTO is pre-shredded — callers flatten the
// boxer error-tree (Streams[].Facts[]) into the parallel arrays
// before Append, mirroring the layout the hand-coded writer emits
// on the wire.
//
// Package name is `errkind` rather than `error` to avoid shadowing
// the Go builtin error interface in any file that imports it.
//
// Wire shape (per ADR-0041 §Decision):
//
//	string : Messages    (errorMsg)  +  Sources       (errorSource)      —  2N entries
//	symbol : Funcs       (errorFunc) +  StreamNames   (errorStreamName)  —  2N entries
//	u32    : Lines       (errorLine)                                      —   N entries
//	u64    : FactIds     (errorFactId) + ParentIds   (errorParentId)     —  2N entries
//	blob   : Data        (errorData)                                      —   N entries
//
// where N = total facts across all streams. Within each section the
// `val` array is grouped by kind in writer order; `lr` lists the
// distinct kinds; `lrcard` is `[N, …]` (one entry per kind). All
// slices in the DTO have equal length N.
package errkind

// Error is one captured error-tree row, pre-shredded into parallel
// arrays. Callers compute total fact count N (across all streams),
// then populate each slice with N entries — StreamNames repeats the
// stream name once per fact in that stream.
type Error struct {
	_ struct{} `kind:"error"`

	// Id is a caller-supplied correlation id for the error row.
	Id uint64 `lw:",id"`

	// NaturalKey is an optional opaque join key (may be nil).
	NaturalKey []byte `lw:",naturalKey"`

	// CapturedTs is wall-clock nanoseconds (truncated to seconds on the
	// wire as DateTime UInt32).
	CapturedTs int64 `lw:",ts"`

	// Per-fact parallel arrays — all same length N. See package doc
	// for the section / membership routing.
	Messages    []string `lw:"errorMsg,stringArray"`
	Sources     []string `lw:"errorSource,stringArray"`
	Funcs       []string `lw:"errorFunc,symbolArray"`
	StreamNames []string `lw:"errorStreamName,symbolArray"`
	Lines       []uint32 `lw:"errorLine,u32Array"`
	FactIds     []uint64 `lw:"errorFactId,u64Array"`
	ParentIds   []uint64 `lw:"errorParentId,u64Array"`
	Data        [][]byte `lw:"errorData,blobArray"`
}
