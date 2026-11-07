// Package launchcfg is writingstylescope's launch configuration (keelson-app
// S4): the leeway-declared DTO a launch request may carry (ADR-0135) — the
// two documents a window opens on and the section-length floor, so another
// app can ask for a comparison without importing the app and its
// registering init(). The codec is generated into launchcfg.out.go by the
// golden test; the memberships are the writingstylescopeLaunch cohort in vdd.
//
// The documents travel as text, as mdedit's handed-over document does, not
// as references into a lading snapshot: the host caps a launch config at
// 64 KiB before it is decoded, which holds the document pairs the window is
// for (its own panes cap at 1 MiB each, and a sweep over two documents of
// that size is far past what a window is asked to compare), and a snapshot
// reference would make the open depend on the snapshot store holding both
// files first. deferred: a lading reference beside the text, for a pair that
// is already a snapshot's — the first caller that opens the window on one.
package launchcfg

import "time"

// AppId is writingstylescope's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/writingstylescope"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "writingstylescopeLaunch"

// WritingstylescopeLaunch is what a caller asks the window to compare:
// document A (the matrix's rows) and document B (its columns) as markdown,
// and the section-length floor in bytes. A zero Floor leaves the window's
// default (200); a floor of 0 is reached through the set_floor operation
// afterwards. A window opened with one neither restores the person's drafts
// nor persists these.
type WritingstylescopeLaunch struct {
	_ struct{} `kind:"writingstylescopeLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	DocA  string `lw:"writingstylescopeLaunchDocA,textArray"`
	DocB  string `lw:"writingstylescopeLaunchDocB,textArray"`
	Floor uint64 `lw:"writingstylescopeLaunchFloor,u64Array"`
}
