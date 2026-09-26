// Package vizevalfacts is the facts-bound record store for vizeval (ADR-0257,
// proposed, §SD8), over two kinds: [VizevalScore], one row per candidate
// scored over a scenario at a build — which rendering, of which data, how far
// it got, and what was measured — and [VizevalJudgement], one row per pair of
// candidates a model compared. The captures themselves stay on disk; a
// scorecard row carries the directory they were written to.
//
// The store is generated from both DTOs over the runtime vocabulary by
// gen_test.go, the lane doc/explanation/facts-bound-record-stores.md
// describes; chstore owns the table's DDL, so this store runs none.
package vizevalfacts
