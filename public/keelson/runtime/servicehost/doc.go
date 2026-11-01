// Package servicehost mounts headless apps — services — at boot and stops
// them at shutdown (ADR-0352 (proposed)). It does for an app whose manifest
// declares SurfaceHeadless what windowhost does for a windowed one: a bus
// client minted from the declared caps, an instance key, the app-lifecycle
// rows, a lifetime scope drained before the bus closes, and the closing
// edge in ADR-0188's order — leave, unmount, drain, unload. Frame is never
// called; a service's work starts in Mount, ends with Cancel() and is
// released in Unmount.
//
// Which headless apps run is a predicate over keelson.apps,
// KEELSON_SERVICES_WHERE ([Where]), evaluated once at boot on
// clickhouse-local after the launch limit (ADR-0272) has been applied, as
//
//	SELECT id FROM apps WHERE surface = 'headless' AND launchable AND (<expr>)
//
// so the launch limit stays a ceiling and the service predicate chooses
// within it ([Select]). Unset, no service runs. The result and each
// service's state are keelson('services') ([RegisterIntrospect]).
package servicehost
