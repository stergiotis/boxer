// Package godep is the marshallgen-serializable manifest and the
// collection<->consumption seam for the Go dependency graph (ADR-0064). It
// defines the two fact kinds — PackageNode (topology, with embedded import
// adjacency) and CollectionRun (per-run header) — the Manifest aggregate
// that carries them, a derived id->node Index (never serialized), and the
// SourceI port.
//
// The derived *lenses* this package once carried — bounded neighbourhood
// walks, the group quotient, sibling-app violations, module rollups,
// reverse reachability, witness paths — are gone with the app that was
// their only consumer. They are SQL now, over the keelson('go_*') tables
// (ADR-0064's 2026-08-01 Updates); the manifest is what those tables are
// built from.
//
// The package deliberately imports neither golang.org/x/tools/go/packages
// nor any egui binding: its collector (godepcollect) and its readers
// depend on godep, and godep depends on neither. That import-direction
// constraint — checked by godep_seam_test.go — makes the separation a
// build-time invariant rather than a convention.
//
// The PackageNode / CollectionRun lw: tags are marshallgen-grammar
// compliant today; the boxer.facts wiring (vdd memberships +
// factswrapper codegen + a FactsSource adapter) is deferred per ADR-0064
// SD7.
package godep
