// Package mddocfacts holds the component DTOs that model a markdown document
// as `boxer.facts` rows, the record store generated over them, and the
// ingest path that writes a whole document — the second facts-bound store
// after [github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts],
// following the worked example doc/explanation/facts-bound-record-stores.md
// records.
//
// Two flows share it, each with its ADR. An editor "sends the document to
// play" by ingesting one MdDoc row and opening the playground on a component
// read filtered to that row's id. The markdown ingestor reads a vault: one
// MdDoc row per file plus one row per heading, fenced code block, link,
// emphasised span and tag, each pointing back at its document, and — for a
// file with a YAML block — one frontmatter row written through the raw DML,
// carrying the block exploded into (path, params, value) leaves on the mixed
// membership channel. The package sits in neutral territory on purpose —
// play registers its component SQL and mdedit and the CLI write rows, and
// none may import another.
//
// # Shape
//
// Six kinds, append only. Envelope columns of the document row:
//
//   - Id — blake3 over (content, ingest time), so every ingest is its own
//     row and a launch query can select exactly the one it just wrote.
//   - NaturalKey — blake3-256 of the content alone, so re-ingesting
//     identical text is visibly the same document across ingests. Every
//     item row carries these bytes as its DocHash.
//   - Ts — the ingest time, the store's Order lane.
//
// Item rows hash the document's id, the kind and the ordinal into their Id,
// and the content hash, the kind and the ordinal into their natural key.
// [BuildRows] is the pure mapping from an extraction to these rows;
// [MddocStore.IngestDocument] extracts and buffers in one call.
//
// The store is externally provisioned by construction — storegen gives it no
// way to run DDL, because chstore owns boxer.facts (ADR-0184 §SD2). A host
// that never ran that DDL fails VerifySchema, and the sender surfaces that
// rather than provisioning on the sly.
package mddocfacts
