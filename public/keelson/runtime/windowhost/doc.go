//go:build llm_generated_opus47

// Package windowhost is the M3 multi-app host. Each open app renders
// as a top-level c.Window (egui::Window — movable, resizable, titled),
// so "click app → see app on the desktop" is the natural interaction
// model. Apps are opened via Open(appId), invoking the registered
// AppCtor for a fresh AppI; closed via Close(windowKey) or the in-body
// "× Close" button. Each window mounts on first Frame and unmounts
// when removed.
//
// Window identity: every Open call allocates a fresh uint64 key. Two
// windows can co-exist for the same AppId — singleton-registered apps
// share their AppI between windows (state is shared); factory-
// registered apps yield independent AppIs per window, so isolation
// depends entirely on the app's internal structure.
//
// Layout: egui's built-in Memory persists each window's position,
// size, and collapsed state across frames, keyed by the window's
// widget id (derived from `ids.PrepareStr("window-<key>")`, stable
// for the window's lifetime). Cross-run persistence is whatever
// egui::Memory natively offers.
//
// History note: the M3 design originally chose egui_dock tabs (this
// package was named `dockhost` through 2026-05-12). The dock model's
// "one active tab per leaf" semantics made "click app, see nothing
// change" the dominant first-time experience even after the
// PanelCentral fix (517fc46b) — the wrong interaction model for the
// multi-app runtime use case. The design was reverted to per-app
// egui::Window. The Manifest's Title / Icon / SurfaceHints fields fed
// directly into c.Window's chrome anyway.
//
// Capabilities-as-host: the WindowHost is *the* interactive entry
// point. In screenshot-tour mode (IMZERO2_SCREENSHOT_DIR set) the
// carousel bypasses WindowHost and runs a single AppI's Frame
// directly via the pre-existing adaptToRenderer path, preserving tour
// driver behaviour.
package windowhost
