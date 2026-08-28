// Package hostboot is the reusable runtime bootstrap for hosts that run
// keelson apps in the window host (ADR-0211). It wires, in one call, what
// the imzero2 demo carousel used to wire inline: process identity, the
// facts store with its runtime-start row and heartbeat, the in-process bus
// with its audit sink, the optional bus-backed services (file dialogs,
// persisted state, clickhouse-local pools, ad-hoc datasets, clipboard,
// coverage, system metrics, introspection), the task supervisor, the
// window host with its seeded windows and decorated chrome, and the
// signal-driven shutdown edge.
//
// The carousel is its first caller with every service on; a downstream
// repository hosting one app calls it with the services it needs and a
// SeedWindow carrying that app's launch config:
//
//	rt, err := hostboot.Boot(ctx, hostboot.Options{
//		Log:      log.Logger,
//		Services: hostboot.Services{Persist: true, Introspect: true},
//		SeedWindows: []hostboot.SeedWindow{{AppId: myapp.ManifestId, Kind: mylaunch.Kind, Config: cfgBytes}},
//		HelpHost: true,
//	})
//	if err != nil { return err }
//	return rt.Run(appCfg)
//
// Boot never fails on an optional service: each one that cannot start is
// logged and left nil on the Runtime, exactly as the carousel behaved. A
// seeded window that cannot open is an error, because the caller asked for
// that window specifically.
package hostboot
