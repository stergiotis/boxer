package app

import "github.com/stergiotis/boxer/public/config/env"

// EasterEgg gates whimsical, non-essential UI flourishes in keelson apps. It is
// off by default; set KEELSON_EASTEREGG=1 (any truthy value) to enable.
//
// It lives in the shared runtime/app package on purpose: every app already
// imports this package, so any of them can branch on app.EasterEgg.Get()
// without taking a new dependency, and the home matches the KEELSON_ name's
// platform scope. The first consumer is the splashscreen app, which colourises
// its grayscale artwork (viridis) when the egg is on; with it off the splash
// keeps the design-system grayscale ramp.
//
// Read it only through this typed var (ADR-0009 routes all environment access
// through the registry; a raw os.Getenv would trip the CS011 lint). The var
// auto-registers at init, so it surfaces in `boxer env list`, doc/env-vars.md,
// and the config inspector with no extra wiring.
var EasterEgg = env.NewBool(env.Spec{
	Name:        "KEELSON_EASTEREGG",
	Default:     "false",
	Description: "enable whimsical easter-egg UI flourishes in keelson apps (e.g. the splashscreen colourises its artwork with the viridis palette)",
	Category:    env.CategoryE("keelson"),
})
