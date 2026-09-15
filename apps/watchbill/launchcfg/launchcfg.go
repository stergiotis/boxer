// Package launchcfg is the watchbill management app's launch configuration
// (ADR-0236 §SD4): the leeway-declared DTO a launch request may carry
// (ADR-0135) — one job to show, or a filter to open on. The codec is
// generated into launchcfg.out.go by the golden test; the memberships are
// the wbLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is the management app's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/watchbill"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "watchbillLaunch"

// WatchbillLaunch is what a caller asks the window to show. JobId selects
// one job and scrolls to it; Kind and State narrow the list; every field
// is optional and "" leaves the default.
type WatchbillLaunch struct {
	_ struct{} `kind:"watchbillLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	JobId string `lw:"wbLaunchJobId,stringArray"`
	Kind  string `lw:"wbLaunchKind,symbol"`
	State string `lw:"wbLaunchState,symbol"`
}
