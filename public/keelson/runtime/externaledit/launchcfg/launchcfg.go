// Package launchcfg is the launch configuration that opens an editor on a
// handed-over text: the leeway-declared DTO a launch request carries. An
// offering app opens it by kind, so the host decides which app edits. The
// codec is generated into launchcfg.out.go by the golden test; the
// membership is the extEditLaunch cohort in vdd.
package launchcfg

import "time"

// Kind is the config kind an editor's manifest declares and a request names.
const Kind = "externalEditLaunch"

// ExternalEditLaunch names the edit to open.
type ExternalEditLaunch struct {
	_ struct{} `kind:"externalEditLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	// Id is the edit's id, as the service answered the offer.
	Id string `lw:"extEditLaunchId,symbol"`
}
