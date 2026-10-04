---
type: adr
status: accepted
date: 2026-10-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-04
---

# ADR-0275: Window arrangement — Rust reports geometry, Go decides placement

## Context

The multi-app host ([ADR-0026](./0026-app-runtime-and-capability-subjects.md) M3) renders each open app as a top-level `egui::Window` inside one OS viewport. The single viewport is an invariant of ADR-0026, so OS-level window management — minimise, tear-off, the platform's window switcher — is not available, and egui offers little in its place. `egui::Window` has a close button, collapse, a default position and size, and layer ordering. It has no minimise, no window list, no tiling or cascading, no focus or activation report, and no way to read or set the layout other than the opaque `egui::Memory`. Before this decision the shell's only window command was "Arrange Windows", which made egui forget every window's position and size.

ADR-0026 tried the other common answer, docking every app into an `egui_dock` area, and reverted it on 2026-05-13: opening an app into a tab strip read as a no-op, the app↔window one-to-one contract was strained, and split-and-rearrange was more than the "two or three apps at once" pattern needed. So the question is how to add window management on top of floating `egui::Window`s, not whether to replace them.

A survey of the egui ecosystem (2026-10-03, public documentation only) found docking and tiling crates (`egui_dock`, `egui_tiles`, `egui_frames`, `grimdock`), a crate of floating windows clipped to a container (`egui_tool_windows`), and chrome for OS windows (`egui-desktop`). None supplies minimise, a window list, cascading or tiling of floating windows, or a layout model a host could drive. Dear ImGui's docking branch was the reference for what such a layer exposes: per-window geometry kept in a table by window id, and calls to focus and place a window.

The work was sized by a spike before this record was written, at the owner's request. Its figures are under *Consequences*.

## Decision

We will keep floating `egui::Window`s as the top-level model and split window management across the FFI boundary. Rust reports each window's geometry, stacking and needed size every frame, and applies a placement when asked. Go decides every placement as a pure function of those reports. The shell offers the resulting commands in a **Window** menu.

- **SD1 — Floating windows stay the top-level model.** ✓ windowhost windows remain `egui::Window`s; no docking or tiling crate takes over the desktop. Docking stays available *inside* an app (`DockArea`).
- **SD2 — Rust reports, once per frame.** ✓ The `fetchR27Windows` fetcher returns one row per `egui::Window` shown that frame. A row holds the window's id, its outer rect, its stacking rank and its collapsed flag, plus the outer size its content needed (SD4). The fetcher also returns the work area: the rect the shell's panels leave free, which is also what a maximised window fills. The rank is read after every window of the frame has drawn. Go reads these through `StateManager.GetWindowGeom` and `StateManager.GetWindowWorkArea`, one frame late like every other register.
- **SD3 — Placement is one frame of a fixed rect.** ✓ The `windowPlace` procedural op asks for a window's outer rect. The window's apply pins position and size for that one frame, the way it already restores a window from maximised, and the window is movable and resizable again on the next. egui's `Memory` keeps the layout state. The host holds no geometry of its own between placements.
- **SD4 — Content need, learned across a few frames.** ✓ egui widens a window to what its content claims, so a placement smaller than that is undone on the next frame. Each report therefore carries the outer size the content needed as laid out that frame, which is larger than the rect exactly when the content overflowed. This is not an intrinsic minimum. egui cannot say how small content could become, and content that stretches to fill the window needs exactly what it was given. An arrangement places the windows, reads each placed window's need a frame later, and places again with those needs as minimums. It stops when nothing grows, or after max(3, windows + 1) passes, and always ends with a placement that uses every minimum learned.
- **SD5 — Arrangements are pure Go.** ✓ Cascade, Tile, Side by side, Stacked and Gather into view are functions of the work area and the reported windows, in [windowhost_arrange.go](../../public/keelson/runtime/windowhost/windowhost_arrange.go). Space is shared so that each window keeps its minimum and the rest is split as evenly as the minimums allow. When the minimums do not fit, windows keep them and overlap their neighbours evenly, still inside the work area. Tiling commands put the front window first. Cascade follows the stacking order, so the front window ends up on top.
- **SD6 — A Window menu.** ✓ The shell chrome gains a **Window** menu holding the arrangements and "Reset window positions" (the former "Arrange Windows"). **Layout** keeps zoom and density.
- **SD7 — Only windowhost windows are arranged.** ✓ Every `egui::Window` reports geometry, but only windows the windowhost owns are placed. Tethered surfaces an app opens itself — pickers, inspectors — stay where their app put them.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL: `window` apply | Records a geometry row per window, takes a pending placement | Regenerated interpreter; `ImZeroFffi` fields for the rows and the pending placements |
| egui2 IDL: `windowPlace` procedural op | Added | Generated `WindowPlace` binding; opcode ids shift |
| egui2 IDL: `fetchR27Windows` fetcher | Added | `StateManager.Sync` issue/collect; `WindowGeomValue` |
| Exported Go API (`bindings`) | `GetWindowGeom`, `GetWindowWorkArea`, `WindowGeomValue`, `WindowPlace` added | — |
| Exported Go API (`windowhost`) | `Arrange`, `ArrangeE`, `ArrangeCommands`, `Rect` added | Shell chrome Window menu |

## Alternatives

- **Dock every app at the top level (`egui_dock`).** Built and reverted in ADR-0026 (2026-05-13), for the reasons in *Context*.
- **Tile the top level (`egui_tiles`).** Its id-keyed tree reconciles well against a list Go owns, but it has no floating windows, so it takes the same tabbed shape that was reverted.
- **Floating windows from a crate (`egui_tool_windows`).** It covers z-order, collapse and clipping inside a container. It has no minimise, window list or arrangement, so the work this record decides would still be ours, on top of a second window implementation. Work-in-progress as of 2026-10-03.
- **Arrangements in Rust.** Rust has the geometry but not the window's identity in the shell: which windows the host owns, which is active, what an app asked for. Go has those. Pure functions in Go are also unit-testable without a renderer.
- **Write egui's `AreaState` and resize state directly.** The accessors are `pub(crate)` in egui, and the window's title-drag path rewrites the stored position after `Area::begin`. The one-frame fixed rect goes through `Window`'s own API, and the restore-from-maximised path already relied on it.
- **Make window bodies scroll instead of learning needs.** A window could then shrink below its content and every placement would hold. It changes every app's layout, hides content behind a scrollbar the app did not ask for, and conflicts with apps that size their own scroll areas. Rejected by the owner in favour of SD4.
- **An intrinsic minimum from egui's sizing pass.** In a sizing pass, text is laid out unwrapped, so the measured width is the widest line, not the narrowest the content allows. It also needs a second layout of the body.
- **egui's persistence feature for the layout.** Kept off because it would make `egui_table`'s state the authority for column widths ([ADR-0148](./0148-app-workingsets.md)), and the store is keyed by egui id, which is opaque to the host.
- **OS-window tear-off (multi-viewport).** Foreclosed by ADR-0026's single viewport. The only implementation found (2026-10-03) also depends on forks of egui and `egui_tiles`.

## Consequences

### Positive

- Window management is ordinary Go: policy, tests and future commands (a window list, snapping, keyboard cycling) need no further IDL change, only Go.
- The geometry report is the read side ADR-0148's deferred "host-side geometry" and "desktop resume" need.
- `WindowPlace` gives the screenshot and scene paths a deterministic way to position windows.

### Negative

- An arrangement can visibly settle over two to four frames while needs are learned. A window whose content stretches never reports a need, so it can be squeezed until its own content overflows, and only that overflow is learned.
- When minimums do not fit, the windows overlap. The scene in *Verification plan* showed Side by side needing 812 + 373 + 259 pt in a 1384 pt work area (2026-10-04).
- The **Window** menu stays open after a command, like the zoom items in **Layout**. The IDL has no op to close a menu.
- Each frame carries the report. Its size is about 44 + 37·n bytes for n windows, by the fetcher's field widths. Frame-time cost was not measured.

### Neutral

- Size of the spike (2026-10-04, from the commits that built it):
  - hand-written IDL and Rust: about 100 and 45 lines;
  - Go bindings: about 50;
  - windowhost hooks and the menu: about 55;
  - arrangement code: 403 lines, of which `arrangeRects` is 108 and `fitSpans` 60, with 150 lines of tests;
  - generated code: about 60 lines in the interpreter and 50 in Go bindings, plus the opcode renumbering.
- Collapsed windows are reported but no command collapses or expands them.

## Migration

The opcode table shifts, so a stale Rust client fails on the first frame (`unable to convert from representation`). A browser bundle or headless client predating the change has to be rebuilt. The shell's "Arrange Windows" item is renamed "Reset window positions" and moves to the **Window** menu. No scene in the tree clicked it.

## Verification plan

- `windowhost_arrange_test.go` covers the pure geometry. The tiling commands partition the work area without overlap for 1–13 windows, Cascade wraps on a crowded desktop, and Gather moves only what sticks out. Minimums are honoured when they fit, and overlap is even when they do not.
- No lane checks the frame-to-frame behaviour (placement, need reporting, passes) against a running host. That was verified by hand in a headless scene: three windows opened with `launch: "subject_alias IN ('launcher','widgets','opsdemo')"`, then each Window command run, reading the window bounds from the tree and checking the captures. A maintained scene beside the windowhost package, with `expect` steps on window bounds would turn that into a lane; it is not written.

## Status

Accepted 2026-10-04. SD1–SD7 are built.

Deferred:
- a window list, raising a window from the Window menu;
- minimise;
- edge snapping;
- keyboard window cycling;
- persisting geometry across runs and replaying open windows (ADR-0148's deferrals, which this report makes possible);
- an IDL op to close a menu after a command.
