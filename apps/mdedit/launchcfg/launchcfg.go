// Package launchcfg is mdedit's launch configuration (ADR-0178, update of
// 2026-10-03): the leeway-declared DTO a launch request may carry
// (ADR-0135) — a document another app hands over, opened as a document of
// its own beside the one mdedit keeps. The codec is generated into
// launchcfg.out.go by the golden test; the memberships are the
// mdeditLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is mdedit's durable id — the target of a launch request. Kept in
// this leaf so a caller does not import the app and its registering init().
const AppId = "github.com/stergiotis/boxer/apps/mdedit"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "mdeditLaunch"

// MdeditLaunch is a handed-over document: Text is its markdown, Name what
// the window calls it and offers as the file name when it is saved.
type MdeditLaunch struct {
	_ struct{} `kind:"mdeditLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	Text string `lw:"mdeditLaunchText,textArray"`
	Name string `lw:"mdeditLaunchName,textArray"`
}
