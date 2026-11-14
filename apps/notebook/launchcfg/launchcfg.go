// Package launchcfg is the notebook window's launch configuration
// (ADR-0371 §SD10): the leeway-declared DTO a launch request may carry
// (ADR-0135) — a notebook another app hands over. The codec is generated
// into launchcfg.out.go by the golden test; the memberships are the
// notebookLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is the notebook app's durable id — the target of a launch request.
// Kept in this leaf so a caller does not import the app and its registering
// init().
const AppId = "github.com/stergiotis/boxer/apps/notebook"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "notebookLaunch"

// NotebookLaunch is a handed-over notebook: Text is its markdown and Name
// what the window calls it. Mount, SnapNanos and Path, when Mount is set,
// name the document in a lading snapshot: the window reads the stored
// results the notebook names from the files beside it there.
//
// Text may be empty when the snapshot names the document: the window shows
// the snapshot's text. A Text that differs from the snapshot's is an edit
// of it, shown over the snapshot's stored results and not stored until the
// notebook is collected. A Text with no snapshot has no stored result.
type NotebookLaunch struct {
	_ struct{} `kind:"notebookLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	Text string `lw:"notebookLaunchText,textArray"`
	Name string `lw:"notebookLaunchName,textArray"`

	Mount     uint64 `lw:"notebookLaunchMount,u64Array"`
	SnapNanos int64  `lw:"notebookLaunchSnapshot,i64Array"`
	Path      string `lw:"notebookLaunchPath,stringArray"`
}
