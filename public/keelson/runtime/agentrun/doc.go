// Package agentrun is the run journal (ADR-0308): the append-only record
// a conversation's coordinator writes before every act and the only source
// of what its model receives. One run is one conversation's journal, with
// a row of its own (the run row) and an ordered, typed, labelled, chained
// sequence of entries; the coordinator is the run's single writer (§SD9),
// assigns ordinals, computes each entry's chain digest (ADR-0314 §SD1)
// before the append and flushes before the act leaves (§SD2), so an act
// the journal does not know of cannot happen.
//
// The package holds the entry kinds (a closed enum with a golden, §SD2),
// the label every entry carries (ADR-0311 §SD1), the chain digest, the
// store seam with its memory and SQL implementations, the writer that
// keeps a run's entries and ships them to the projection (ADR-0309), the
// resume fold (§SD4), the host service `runtime.agentrun.*` and the
// introspection tables (§SD10). The generated record store is the
// agentrunstore subpackage; the projection is the projection subpackage.
package agentrun
