// Package launchcfg is the trail viewer's launch configuration (ADR-0350,
// proposed): the leeway-declared DTO a launch request carries (ADR-0135) —
// the case the window opens on, and optionally its time window, its tab and
// a saved view. The codec is generated into launchcfg.out.go by the golden
// test; the memberships are the tvLaunch cohort in vdd. It is its own
// package so an app that opens the viewer — chat, on a conversation — does
// not link the window.
package launchcfg

import "time"

// AppId is the trail viewer's durable id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/public/keelson/runtime/trailviewer"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "trailViewerLaunch"

// TrailViewerLaunch is the case the window opens on: Case is a case kind's
// name as the viewer spells it (run, window, app, conversation, task,
// subject, principal, entity, destination, all) and Value its value. The
// window is LastNanos, a span ending now, when set; else FromNanos and
// ToNanos, Unix nanoseconds, when both are set; else the viewer's default.
// Tab is a tab's name, empty for the default. View names a saved view the
// window opens once its saved views are read; the case and window given here
// apply until then. A config that does not read as a case, a window or a tab
// is ignored with a diagnostic in the window; it never refuses the open.
type TrailViewerLaunch struct {
	_ struct{} `kind:"trailViewerLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	Case  string `lw:"tvLaunchCase,stringArray"`
	Value string `lw:"tvLaunchValue,stringArray"`

	LastNanos int64 `lw:"tvLaunchLast,i64Array"`
	FromNanos int64 `lw:"tvLaunchFrom,i64Array"`
	ToNanos   int64 `lw:"tvLaunchTo,i64Array"`

	Tab  string `lw:"tvLaunchTab,stringArray"`
	View string `lw:"tvLaunchView,stringArray"`
}
