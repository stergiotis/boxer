// Package launchcfg is terrainscope's launch configuration (keelson-app S4):
// the leeway-declared DTO a launch request may carry (ADR-0135) — the two
// points a window opens on, so another app can ask for a line-of-sight
// sweep without importing the app and its registering init(). The codec is
// generated into launchcfg.out.go by the golden test; the memberships are
// the terrainscopeLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is terrainscope's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/terrainscope"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "terrainscopeLaunch"

// TerrainscopeLaunch is what a caller asks the window to sweep: an observer
// and a target in WGS84 degrees, both required, and the parameters a first
// sweep needs. The sweep starts on the first frame, as it does after the
// second click on the map. A zero ObserverHeight, TargetHeight or Samples
// leaves the window's default (1.7 m, 0 m, 16); a nominal-only sweep is
// reached through the set_parameter operation afterwards.
type TerrainscopeLaunch struct {
	_ struct{} `kind:"terrainscopeLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	ObserverLat float64 `lw:"terrainscopeLaunchObserverLat,f64Array"`
	ObserverLon float64 `lw:"terrainscopeLaunchObserverLon,f64Array"`
	TargetLat   float64 `lw:"terrainscopeLaunchTargetLat,f64Array"`
	TargetLon   float64 `lw:"terrainscopeLaunchTargetLon,f64Array"`

	ObserverHeight float64 `lw:"terrainscopeLaunchObserverHeight,f64Array"`
	TargetHeight   float64 `lw:"terrainscopeLaunchTargetHeight,f64Array"`
	Samples        uint64  `lw:"terrainscopeLaunchSamples,u64Array"`
}
