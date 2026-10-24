---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Vendored: `winit`

The windowing library under eframe, carried here for one change to its Wayland
backend. The decision and its alternatives are
[ADR-0332](../../../../doc/adr/0332-a-hidden-desktop-host-keeps-running-passes.md).
Upstream: <https://github.com/rust-windowing/winit>.

- **Upstream version**: `0.30.13`, as published on crates.io (the copy cargo
  unpacks into its registry); the `winit` line eframe 0.36 depends on.
- **Licence**: Apache-2.0. The text is kept verbatim beside this file
  (`LICENSE`) and the crate is recorded in
  [THIRD_PARTY_NOTICES.md](../../../../THIRD_PARTY_NOTICES.md).

## Why vendored rather than a crates.io dependency

winit's Wayland backend withholds `RedrawRequested` from a window while the
frame callback it asked for has not arrived, and a compositor sends none to a
surface it does not show: minimised, on another workspace, or behind a locked
screen. eframe runs an egui pass only on a redraw, so such a window runs none,
and the imzero2 client stops answering the Go host until the window is shown
again. Neither winit nor eframe exposes a setting for it. The change is small
and sits on a line (`0.30.x`) that moves only when eframe moves to a new winit
minor, so carrying it costs a re-apply on that bump.

## Deltas against upstream

Each site is marked `// boxer delta:` in the source.

| What | Why |
| --- | --- |
| `WindowState` records when its frame callback was requested and reports `frame_callback_deadline()`, `FRAME_CALLBACK_BOUND` (250 ms) after that (`platform_impl/linux/wayland/window/state.rs`) | the bound on how long a redraw waits for a frame callback |
| the redraw dispatch throttles a window only until that deadline (`platform_impl/linux/wayland/event_loop/mod.rs`) | a window the compositor does not show gets its redraw once the bound passes; a shown one gets its callback well inside it, so nothing changes for it |
| the event loop's wait ends at the earliest deadline of a window with a redraw pending (same file) | without it the loop sleeps until another event, and the redraw left pending at the throttle would wait for it |
| `#![allow(clippy::all, clippy::uninlined_format_args)]` after upstream's lint levels in `src/lib.rs` | `check.sh` runs `cargo clippy --workspace -- -D warnings`, and clippy lints path dependencies |
| removed the `[[example]]`, `[[test]]` and `[dev-dependencies]` sections of `Cargo.toml`; `examples/`, `tests/`, `docs/`, `FEATURES.md` and `Cargo.toml.orig` not copied | not reachable from the library build |

## How this copy is built

It replaces the crates.io `winit` through `[patch.crates-io]` in
`../../Cargo.toml`, and is **excluded** from the imzero2 workspace, like
`egui_software_backend`: `check.sh` names its packages for `cargo fmt` and so
does not reformat this source. The browser host does not link winit.

## Re-syncing

On an eframe bump that moves winit, unpack the new release, re-apply the table
above, and run the hidden-window check of ADR-0332's verification plan: a
minimised client keeps passing, at most `FRAME_CALLBACK_BOUND` apart. If
upstream has a bound of its own by then, drop this copy.
