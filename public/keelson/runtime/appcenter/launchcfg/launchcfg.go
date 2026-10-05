// Package launchcfg is the app center's launch configuration (ADR-0260
// §SD1): the leeway-declared DTO a launch request carries (ADR-0135) — the
// app whose page the window opens on. The codec is generated into
// launchcfg.out.go by the golden test; the membership is the acLaunch cohort
// in vdd. It is its own package so the host's boot can encode an Inspect
// without linking the window.
package launchcfg

import "time"

// AppId is the app center's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/public/keelson/runtime/appcenter"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "appCenterLaunch"

// AppCenterLaunch names the app to show. Empty opens on the list alone.
type AppCenterLaunch struct {
	_ struct{} `kind:"appCenterLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	AppId string `lw:"acLaunchAppId,stringArray"`
}
