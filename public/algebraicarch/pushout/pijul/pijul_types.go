//go:build llm_generated_opus47

// Package pijul is the domain half of a multi-actor patch-theory
// event-store demo: four working copies (Server + Alice/Bob/Charlie)
// backed by a [BackendI] and driven through the [RepoI] interface. The
// imzero2/egui2 GUI consumer lives in hackathon_2026's pijuldemo
// package, which imports this one.
//
// The package is structured around two seams:
//
//   - [BackendI]/[RepoI] — pure-domain interfaces taking [KVLine] cells.
//     Two realisations exist: [pijulTextBackend] serialises cells into
//     pijul's textual flat-KV format and shells out to the `pijul`
//     binary; [NewPushoutBackend] operates natively on graggle patch
//     operations (in-memory store + on-disk envelopes) without ever
//     materialising text.
//
//   - [RunnerI] — a CLI-verb-level seam used internally by the
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

// ConflictData captures every side of a conflict at the cell level.
// AliceValue / BobValue hold the first two side values for the common
// 2-way case; OtherValues carries any additional sides for N-way
// conflicts (3+ actors editing the same cell, or cycle conflicts that
// surface as multiple live nodes for one path). Side identifiers
// (pijul's "1"/"2" labels in the textual working copy) are *not* part
// of this struct: they are a text-format detail handled inside the
// text backend.
type ConflictData struct {
	AliceValue  string
	BobValue    string
	OtherValues []string
}

// AllValues returns every side of the conflict in render order
// (Alice, Bob, then OtherValues). Convenience for renderers that
// iterate buttons / chips per side.
func (inst ConflictData) AllValues() (out []string) {
	out = make([]string, 0, 2+len(inst.OtherValues))
	out = append(out, inst.AliceValue)
	out = append(out, inst.BobValue)
	out = append(out, inst.OtherValues...)
	return
}
