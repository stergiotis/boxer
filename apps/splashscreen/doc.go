// Package splashscreen is a windowed keelson app (ADR-0026 AppI) that
// credits each copyright holder whose work is in the running binary, and
// shows the binary's build provenance.
//
// A credited party is boxer, the library (boxer.go: its copyright,
// licence, NOTICE and artwork, from the embedded assets), and, when one is
// registered with [SetProgram], the program that links it (program.go).
// Each party has a tab of its own with two pages:
//
//   - Splash — the party's artwork, scaled to the window.
//   - About  — the party's copyright, licence and NOTICE.
//
// A tab draws only what its own party supplied, so one party's notices are
// never shown under another's name. A Build tab, belonging to neither,
// lists the running binary's revision, module and run identity.
//
// The window chrome (title bar, drag, resize, close) is owned by the
// runtime; this app renders only the body. The NOTICE copy under assets/
// is refreshed from the repo-root NOTICE via the go:generate directive in
// app_register.go.
package splashscreen
