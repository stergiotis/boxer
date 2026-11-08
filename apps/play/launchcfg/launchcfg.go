// Package launchcfg holds play's own app id and re-exports the playground's
// launch contract, which moved to public/keelson/runtime/playground (ADR-0361
// §SD4). Its aliases go when nothing outside apps/play imports the package;
// the compiler is the check. A caller that means the role and not play
// imports the playground leaf and opens by kind; AppId stays for a caller
// that means play.
package launchcfg

import "github.com/stergiotis/boxer/public/keelson/runtime/playground"

// AppId is play's registered manifest id. Kept here, in a leaf, rather than
// in the app package: importing `apps/play` for a constant drags the whole
// playground — and its registry-registering init() — into the importer
// (ADR-0017 §SD4). `play.AppId` re-exports this value. Untyped so it flows
// into app.AppIdT contexts without this leaf depending on the app runtime.
const AppId = "github.com/stergiotis/boxer/apps/play"

// Kind is the launch kind play's manifest declares, the leaf's own.
const Kind = playground.Kind

// Endpoint constants for PlayLaunch.Endpoint.
const (
	EndpointDefault       = playground.EndpointDefault
	EndpointIntrospection = playground.EndpointIntrospection
)

// PlayLaunch is the playground's launch config.
type PlayLaunch = playground.Launch
