// Package splashscreen is a windowed keelson app (ADR-0026 AppI) that
// presents the project's splash artwork alongside two companion panes:
//
//   - Splash — the bundled grayscale artwork, scaled to the window.
//   - About  — name, version, copyright and build/run provenance.
//   - NOTICE — the project NOTICE, rendered verbatim.
//
// The window chrome (title bar, drag, resize, close) is owned by the
// runtime; this app renders only the body. The splash asset is embedded
// (see app_register.go); the NOTICE copy under assets/ is refreshed from
// the repo-root NOTICE via the go:generate directive in app_register.go.
package splashscreen
