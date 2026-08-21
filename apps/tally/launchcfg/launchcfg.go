// Package launchcfg is tally's launch configuration (ADR-0200 §SD9): the
// leeway-declared DTO a launch request may carry (ADR-0135) and a workingset
// restores (ADR-0148) — the two panes' locations, the synchronized-browsing
// flag and the target pane. The codec is generated into launchcfg.out.go by
// the golden test; the memberships are the tallyLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is tally's durable app id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/tally"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "tallyLaunch"

// TallyLaunch is what reproduces a tally window. A mount is its id in hex
// (the SFTP directory spelling), a snapshot is RFC 3339 with nanoseconds or
// "" for "follow latest", a directory is an io/fs path ("." the root).
type TallyLaunch struct {
	_ struct{} `kind:"tallyLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	MountA string `lw:"tallyLaunchMountA,textArray"`
	SnapA  string `lw:"tallyLaunchSnapA,textArray"`
	DirA   string `lw:"tallyLaunchDirA,textArray"`
	MountB string `lw:"tallyLaunchMountB,textArray"`
	SnapB  string `lw:"tallyLaunchSnapB,textArray"`
	DirB   string `lw:"tallyLaunchDirB,textArray"`
	// Sync is synchronized browsing; Target is "A" or "B".
	Sync   bool   `lw:"tallyLaunchSync,bool"`
	Target string `lw:"tallyLaunchTarget,symbol"`
}
