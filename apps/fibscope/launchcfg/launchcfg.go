// Package launchcfg is fibscope's launch configuration (keelson-app S4): the
// leeway-declared DTO a launch request may carry (ADR-0135) — the id a
// window opens on, so another app can show how one of its ids splits without
// importing the app and its registering init(). The codec is generated into
// launchcfg.out.go by the golden test; the membership is the fibscopeLaunch
// cohort in vdd.
package launchcfg

import "time"

// AppId is fibscope's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/fibscope"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "fibscopeLaunch"

// FibscopeLaunch is the word to inspect. Any 64-bit value is accepted, a
// word that is not a tagged id included, since inspecting how such a word
// splits is one of the things the window is for; zero leaves the worked
// example the window opens on plainly.
type FibscopeLaunch struct {
	_ struct{} `kind:"fibscopeLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	Id uint64 `lw:"fibscopeLaunchId,u64Array"`
}
