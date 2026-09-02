// Package mddocfacts holds the component DTO that models a sent markdown
// document as `boxer.facts` rows, and the record store generated over it —
// the second facts-bound store after
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts], following
// the worked example doc/explanation/facts-bound-record-stores.md records.
//
// The flow it serves (its ADR records the decision): an editor "sends the
// document to play" by ingesting one MdDoc row and opening the playground on
// a component read filtered to that row's id, whose content column play's
// Detail pane renders as markdown. The package sits in neutral territory on
// purpose — play registers its component SQL and mdedit writes rows, and
// neither may import the other.
//
// # Shape
//
// One kind, append only. Envelope columns:
//
//   - Id — blake3 over (content, send time), so every send is its own row
//     and the launch query can select exactly the one it just wrote.
//   - NaturalKey — blake3-256 of the content alone, so re-sending identical
//     text is visibly the same document across sends.
//   - Ts — the send time, the store's Order lane.
//
// The store is externally provisioned by construction — storegen gives it no
// way to run DDL, because chstore owns boxer.facts (ADR-0184 §SD2). A host
// that never ran that DDL fails VerifySchema, and the sender surfaces that
// rather than provisioning on the sly.
package mddocfacts
