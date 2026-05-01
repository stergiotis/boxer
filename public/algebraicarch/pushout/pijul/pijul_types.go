//go:build llm_generated_opus47

// Package pijul wraps a patch-theory event-store as a multi-actor demo:
// it spawns four working copies (Server + Alice/Bob/Charlie) backed by
// a [BackendI], drives them through the [RepoI] interface, and renders
// the resulting state through the imzero2/egui2 UI.
//
// The package is structured around two seams:
//
//   - [BackendI]/[RepoI] — pure-domain interfaces taking [KVLine] cells.
//     The current implementation is [pijulTextBackend], which serialises
//     cells into pijul's textual flat-KV format and shells out to the
//     `pijul` binary. The planned successor is a native Go backend
//     wrapping ../../../../../../hackathon_2026/src/go/public/pushout
//     that operates directly on graggle patch operations without ever
//     materialising text.
//
//   - [pijulRunnerI] — a CLI-verb-level seam used internally by the
//     text backend; one method per `pijul` subcommand.
package pijul

// KVLine is one parsed `<path> "<value>"` cell from the demo's flat-KV
// record file. It is the package's domain noun: the [RepoI] interface
// reads and writes slices of these without exposing the underlying
// serialisation. When a cell is in an unresolved conflict, Conflict
// carries both sides and Value is empty; when the introducing patch is
// known, Credit carries the patch metadata.
type KVLine struct {
	Path     string
	Value    string
	Conflict *ConflictData
	Credit   *PatchMetadata
}

// ConflictData captures both sides of a two-way conflict at the cell
// level. Side identifiers (pijul's "1"/"2" labels in the textual
// working copy) are *not* part of this struct: they are a text-format
// detail handled inside the text backend.
type ConflictData struct {
	AliceValue string
	BobValue   string
}

// Task is one unit of work scheduled on [DemoStore.TaskQueue]. Action
// performs the work and returns the actors whose state was touched (so
// the dispatcher knows whose UI cache to refresh). OnDone runs while
// holding the store lock; keep it short.
type Task struct {
	Action func() (actors []string, err error)
	OnDone func(err error)
}
