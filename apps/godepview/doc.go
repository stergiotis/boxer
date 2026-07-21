// Package godepview is the keelson app that explores this module's Go
// dependency graph (ADR-0064). It renders the marshallgen-serializable
// godep.Manifest as a master-detail view: an etable of every package in
// the transitive closure (text filter + class toggles + column sort)
// paired with a graph of the focused package's import neighborhood (depth
// + direction). Thousands of packages stay legible because the table is the
// scalable full-closure surface while the graph only ever draws the focus
// node's local neighborhood.
//
// The app depends only on the godep manifest and the godep.SourceI port.
// The concrete data source — a godepcollect.LiveCollector today, a
// boxer.facts-backed reader later — is injected by the registry ctor in
// app_register.go (the composition root); the render path imports neither
// the collector nor golang.org/x/tools.
package godepview
