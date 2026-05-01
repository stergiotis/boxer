//go:build llm_generated_opus47

// Package pijul wraps the Pijul VCS as a multi-actor event-store demo:
// it spawns four working copies (Server + Alice/Bob/Charlie), drives the
// `pijul` CLI via os/exec, and renders the resulting state through the
// imzero2/egui2 UI. The package is structured around a [PijulRunnerI]
// seam so the CLI driver can be swapped for a future native Go
// implementation backed by the pushout/graggle package
// (../../../../../../hackathon_2026/src/go/public/pushout).
package pijul

import "time"

// KVLine is one parsed `<path> "<value>"` row from the demo's flat-KV
// data file. When the row is in a Pijul conflict block, Conflict carries
// both sides and Value is empty.
type KVLine struct {
	Path         string
	Value        string
	Conflict     *ConflictData
	CreditHash   string
	CreditAuthor string
}

// ConflictData captures the two sides of a `>>>>>>> N === <<<<<<< M`
// block as written by Pijul into the working copy.
//
// AliceLabel / BobLabel hold Pijul's *side labels* (typically "1" and
// "2"), not patch hashes — Pijul does not embed the conflicting hashes
// into the marker line. The fields are kept under the Alice/Bob naming
// only because the demo personifies the two sides; the parser does not
// know which actor authored which side.
type ConflictData struct {
	AliceLabel string
	AliceValue string
	BobLabel   string
	BobValue   string
}

// PatchEnvelope is one entry in the demo's shared "Inbox" — a binary
// Pijul change file copied out of `.pijul/changes/` for peer-to-peer
// distribution.
type PatchEnvelope struct {
	FromActor string
	Hash      string
	PatchPath string
}

// LogEntry maps the output of `pijul log --output-format json`.
// ParsedTime is filled in by [parsePijulLogJSON] and is *not* a JSON
// field; it is derived from Timestamp for graph-age comparison in
// credit resolution.
type LogEntry struct {
	Hash       string    `json:"hash"`
	Authors    []string  `json:"authors"`
	Timestamp  string    `json:"timestamp"`
	Message    string    `json:"message"`
	ParsedTime time.Time `json:"-"`
}

// Task is one unit of work scheduled on [DemoStore.TaskQueue]. Action
// performs the work and returns the actors whose state was touched (so
// the dispatcher knows whose UI cache to refresh). OnDone runs while
// holding the store lock; keep it short.
type Task struct {
	Action func() (actors []string, err error)
	OnDone func(err error)
}
