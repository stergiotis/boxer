// Package godep is the marshallgen-serializable manifest and the
// collection<->visualization seam for the Go dependency explorer
// (ADR-0064). It defines the two fact kinds — PackageNode (topology, with
// embedded import adjacency) and CollectionRun (per-run header) — the
// Manifest aggregate that carries them, a derived Index (id->node and
// reverse adjacency, never serialized), and the SourceI port.
//
// The package deliberately imports neither golang.org/x/tools/go/packages
// nor any egui binding: both the collector (godepcollect) and the app
// (apps/godepview) depend on godep, and godep depends on neither. That
// import-direction constraint — checked by godep_seam_test.go — makes the
// collection/visualization separation a build-time invariant rather than a
// convention.
//
// The PackageNode / CollectionRun lw: tags are marshallgen-grammar
// compliant today; the boxer.facts wiring (vdd memberships +
// factswrapper codegen + a FactsSource adapter) is deferred per ADR-0064
// SD7.
package godep
