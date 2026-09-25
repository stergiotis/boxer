// Package vizevalfacts is the facts-bound record store for vizeval scorecards
// (ADR-0257, proposed, §SD8): one `boxer.facts` row per candidate scored over
// a scenario at a build — which rendering, of which data, how far it got, and
// what was measured. The captures themselves stay on disk; the row carries
// the directory they were written to.
//
// The store is generated from [VizevalScore] over the runtime vocabulary by
// gen_test.go, the lane doc/explanation/facts-bound-record-stores.md
// describes; chstore owns the table's DDL, so this store runs none.
package vizevalfacts
