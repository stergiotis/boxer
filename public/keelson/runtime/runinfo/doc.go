//go:build llm_generated_opus47

// Package runinfo captures the identity of a single pebble2impl process
// boot — a "run" — and makes it discoverable to every app launched
// inside that run via two channels: the PEBBLE2_RUN_ID environment
// variable, and a zerolog field on the base logger that app loggers
// derive from.
//
// Init() is called once from the runtime entry point (carousel main).
// It allocates a nanoid run_id, captures hostname / pid / Go version /
// VCS revision (via boxer/public/observability/vcs), and stores the
// result in a process singleton. Subsequent Get() calls return the
// same Inst. Init also has two intentional side effects:
//
//  1. Sets PEBBLE2_RUN_ID in the environment so apps that prefer plain
//     env lookups, sub-processes spawned by the runtime, and CLI tooling
//     can read it without importing this package.
//  2. Returns a logger-wrapper closure so callers can tag their base
//     log.Logger with the run_id. Per-app loggers built via
//     app.AppLogger inherit the field automatically.
//
// The runtime-start event itself (a row in runtime.facts) is written by
// the carousel using factsstore.WriteRuntimeStart with the *Inst as
// input — runinfo doesn't depend on factsstore so the package stays
// usable from contexts that haven't wired persistence yet (tests,
// headless CLI tools).
package runinfo
