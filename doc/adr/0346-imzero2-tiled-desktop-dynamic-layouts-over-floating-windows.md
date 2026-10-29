---
type: adr
status: proposed
date: 2026-10-29
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0346: A tiled desktop — dynamic layouts the host keeps live over floating windows

## Context

The multi-app host renders each open app as a floating `egui::Window` in one
OS viewport ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)).
[ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) gave
that desktop arrangements: Rust reports every window's rect, stacking rank and
content need each frame, Go computes Cascade, Tile, Side by side, Stacked and
Gather as pure functions, and applies them through a one-frame placement.
[ADR-0276](./0276-agents-read-and-arrange-windows.md) made the same state
keelson data and the same commands agent verbs under a desktop grant.

An arrangement is a one-shot command. Once it lands the windows float again:
a new window opens at its default spot, a closed window leaves a hole, a
zoom or density change leaves the layout stale, and the person reaches for
the Window menu once more. ADR-0275 deferred the pieces that would hold the
intent — a window list, minimise, snapping, keyboard cycling, persisted
geometry — and nothing since has taken them up.

Two things have moved since docking was reverted (ADR-0026, 2026-05-13) for
serving a "two or three apps at once" pattern. The chat coordinator now
opens windows for the person ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md),
ADR-0276), so desktops are busier than the person alone made them. And the
host owns a continuous pin already: a maximised window is re-pinned to the
work area every frame, so a window pinned to a cell every frame is the same
mechanism with another rect.

The shell's keyboard is the thin spot. It reads four hardcoded key fetchers
(F1, F2, F8, the command-Enter chord). Widgets capture chords in their own
focus scope ([ADR-0334](./0334-mdedit-blocks-over-the-source.md) §SD7), but
nothing serves a chord to the shell itself, and a window manager is driven
from the keyboard or it is not used.

The owner's answers to the policy questions (2026-10-29): dynamic layouts,
not a manual split tree; when the windows' minimums do not fit a layout, the
windows overlap rather than scroll or clip; Alt is the chord modifier;
workspaces are deferred; an agent may set the layout under the desktop grant
in act mode, as it may arrange.

## Design space (QOC)

**Question.** Which window-management model should the optional layer over
floating windows take?

**Options.**

- **O1** — A manual split tree, as i3 and sway have it: the person splits
  containers, each container horizontal, vertical, tabbed or stacked.
- **O2** — Dynamic layouts, as dwm, xmonad, river and Hyprland's master
  layout have it: a layout is a function of the ordered windows, the work
  area and a few parameters; the person tunes the parameters and the order.
- **O3** — Scrollable columns, as niri and PaperWM have it: windows keep
  their width, the desktop is a strip wider than the viewport, and the
  viewport scrolls along it.
- **O4** — Snap zones, as Windows Snap, FancyZones and Rectangle have it:
  windows stay floating, a chord snaps one to a half or quarter.

**Criteria.**

- **C1** — State the host must keep and an agent must drive: less is better.
- **C2** — Fit with what exists: ADR-0275's pure arrangement functions, the
  per-frame pin, the stacking and need reports.
- **C3** — Keystrokes from "three windows open" to a usable layout.
- **C4** — Holds the intent as windows open and close.
- **C5** — Stays inside the single viewport as the video stream shows it
  (ADR-0026).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | ++ |
| C2 | −  | ++ | −  | +  |
| C3 | −− | ++ | +  | −  |
| C4 | +  | ++ | ++ | −− |
| C5 | ++ | ++ | −− | ++ |

O1's tabbed and stacked containers are the tab-strip shape that was reverted,
and its tree is the most state of the four. O3 dissolves the content-need
problem by never shrinking a window, but a strip wider than the viewport
changes what the stream shows. O4 keeps nothing once a window opens or
closes. O2 is what ADR-0275 §SD5 already is, made live.

## Decision

We will add an optional tiled mode to the window host. In tiled mode a
layout — a pure Go function of the ordered windows, the work area and a few
parameters — is kept live: it is re-planned when its inputs change and every
tiled window is pinned to its cell each frame. Floating mode is unchanged and
stays the default. The shell gains one fetcher for its chords, and the
agent surface gains the layout beside the arrangements.

### SD1 — Two desktop modes, five layouts

The desktop is **floating** (today's behaviour) or **tiled**. A tiled desktop
has one layout:

- **Main and stack** — one main window takes a column of the work area at
  the main ratio, the others stack in the remaining column; the main count
  and the ratio are the parameters.
- **Tile**, **Columns**, **Rows** — ADR-0275's grid, side-by-side and
  stacked arrangements, kept live.
- **Monocle** — every tiled window fills the work area; the active one is
  in front.

A split tree (i3), dwindle partitioning (bspwm, Hyprland) and scrollable
columns (niri) are not layouts of this record. Dwindle fits the model and can
join as a layout later without a new decision.

### SD2 — The layout is live

A tiled window is placed every frame through the existing one-frame
placement, so it is unmovable and unresizable by the pointer and holds its
cell, the way a maximised window holds the work area. The plan is a function
of the ordered tiled windows, the work area and the layout's parameters,
computed over the arrangement geometry ADR-0275 §SD5 has.

A change of input — a window opens, closes, floats or unfloats, the order
changes, the work area changes with zoom, density or the viewport, or a
parameter moves — starts a settling run as ADR-0275 §SD4 describes: place,
read each window's need a frame later, place again with the needs as
minimums, until nothing grows or the passes run out. The settled rects then
hold until the next change.

A pinned window does not hold a cell smaller than its content: egui grows
the window around what its body laid out, from the cell's origin, and clips
nothing short of the screen (the measurement of 2026-10-29 under *Updates*).
The run's minimums are what turn that growth into a layout. With the needs
learned, a cell is never smaller than its window, and when the minimums do
not fit the windows overlap evenly inside the work area, as `fitSpans`
already does. The active window is raised so what the person works in is on
top. Nothing scrolls and nothing is clipped on the host's account. Two
limits follow: during the passes a squeezed window spills over its
neighbours for a frame or two, and a window whose need exceeds the work
area spills past the work area for as long as it is tiled.

### SD3 — Order and focus

Tiled windows have an order, back of the stack to main. A window that opens
joins as the main, the convention dwm and xmonad set and the "front window
first" rule ADR-0275's tiling commands have. Closing removes it. Chords
(SD5) move the active window along the order, swap it with a neighbour, or
promote it to main.

The active window stays what ADR-0275 derives from the stacking report. A
focus chord raises the target window; the report follows a frame later and
the active window moves with it. Directional focus picks the nearest window
in that direction by the reported rects.

### SD4 — Per-window float, and maximise

Any window can be toggled to floating. A floating window is out of the
layout, keeps egui's drag and resize, and comes back to the main position
when toggled again. A maximised window is treated as floating while it is
maximised. Tethered surfaces an app opens itself — pickers, inspectors — are
never tiled, as ADR-0275 §SD7 has it. Switching the desktop to floating
leaves every window where the layout last put it.

### SD5 — Shell chords through one fetcher

The IDL gains one fetcher that drains, once per frame, the shell chords
pressed this frame as a bit set, and consumes those key events so no widget
sees them. The chord table is a Go table, with the Rust arm generated from it
and checked by a test, the way the key vocabulary of
[ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) is kept. The
modifier is Alt; Shift extends a chord.

| Chord | Tiled | Floating |
| --- | --- | --- |
| Alt+J / Alt+K | focus next / previous in the order | focus next / previous in stacking |
| Alt+H / Alt+L, Alt+↑ / Alt+↓ / Alt+← / Alt+→ | focus by direction | focus by direction |
| Alt+Shift+J / Alt+Shift+K | swap with next / previous | — |
| Alt+Shift+arrow | — | snap the active window to that half; a second press to the quarter |
| Alt+Enter | promote the active window to main | — |
| Alt+Space | cycle the layout | switch to tiled |
| Alt+Shift+Space | toggle the active window floating | — |
| Alt+F | toggle monocle | maximise |
| Alt+= / Alt+− | main ratio up / down | — |
| Alt+Shift+= / Alt+Shift+− | main count up / down | — |
| Alt+Tab | focus the previously active window | focus the previously active window |

A chord that means nothing in the current mode is dropped. Alt is taken
because egui binds nothing to it, the OS window managers the host runs under
leave Alt+letter to the application, and i3's default modifier is Alt.
In the browser tab some Alt chords belong to the browser before the page
sees them; which ones survive is recorded under *Updates* when measured.

### SD6 — The layout is keelson data and an agent verb

`keelson('desktop')` gains the mode, the layout, the main ratio and count.
`keelson('windows')` gains `tiled` (the window is in the layout), `floating`,
and `order`. `runtime.agent.layout` sets the mode, the layout and its
parameters, and floats or unfloats a window; it requires the desktop grant
in act mode, like `arrange` (ADR-0276 §SD4), and is an action record. The
chat's coordinator gets it as a tool beside `arrange_windows`.

Under tiled mode, `runtime.agent.arrange` is refused with the remedy "set the
layout": a one-shot arrangement is a floating-mode command. `place` on a
tiled window floats it first, as a person's drag would if the window took
one; `raise` works in either mode.

### SD7 — Optional, and seeded for scenes

Tiled mode is off until chosen. The Window menu gains a **Desktop layout**
submenu: Floating, the five layouts, the parameters. (Not "Layout": the menu
bar has a Layout menu for zoom and density, and two entries of one name a
level apart read as one thing and resolve as one in the driver.) `IMZERO2_DESKTOP_LAYOUT`
(registered per [ADR-0009](./0009-environment-variable-registry.md)) seeds a
process's mode and layout, for scenes and demos, the way `IMZERO2_DENSITY`
seeds the density preset. The choice is session-scoped; persisting it, and
the order and float set with it, waits on ADR-0148's host-side geometry
deferral.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL: shell-chord fetcher | Added | Regenerated interpreter; opcode ids shift; the Go chord table and its generated Rust arm with the drift test; browser host digest |
| Exported Go API (`windowhost`) | Mode, layout, parameters, order, float; `SetLayout`, `FloatWindow`, their readers | Shell chrome Layout submenu; the chord dispatch |
| `keelson('desktop')`, `keelson('windows')` schema | Columns added (SD6) | Readers selecting `*` see more columns |
| `runtime.agent.layout` | Added; `arrange` refused under tiled mode | `agent.Service` dispatch, wire types, `HostI`, `agent.Client`, the dispatch test hosts, the chat's tool and coordinator prompt |
| Env var registry | `IMZERO2_DESKTOP_LAYOUT` added | `doc/env-vars.md` regeneration |

## Alternatives

- **A manual split tree (O1).** Its tabbed and stacked containers are the
  shape ADR-0026 reverted; its tree is the most state to hold, to persist and
  for an agent to drive; and reaching a layout takes the most keystrokes.
- **Scrollable columns (O3).** Not a layout of this record, for C5: a strip
  wider than the viewport changes what the video stream shows. It would also
  need a desktop scroll the IDL has no op for.
- **Snap zones only (O4).** Falls out of `Place` and SD5's chords, and is
  kept as the floating-mode half of the chord table, but it holds nothing
  once a window opens or closes.
- **Scroll the body of a squeezed window.** Rejected by the owner for
  arrangements (ADR-0275) and again here: it changes how every app's window
  sizes and nests a scroll area inside apps that size their own. Overlap was
  chosen instead.
- **Clip a squeezed window to its cell.** egui's resize container enlarges
  its clip rect to the last content size, so content painted past a fixed
  rect is not clipped by egui. Clipping on the host's account would hide
  content with no sign that it is hidden. Overlap shows it.
- **One fetcher per chord**, as F1, F2 and F8 have. Eleven chords would be
  eleven fetchers and eleven opcode shifts; one bit set is one.
- **Chords captured by a shell-level widget** through ADR-0334 §SD7. Capture
  is gated on focus, and the shell has no focused widget; the chords must
  fire whatever has focus.
- **Super as the modifier.** Grabbed by GNOME and KDE for their own window
  management, and by browsers in the tab.
- **A tiling crate (`egui_tiles`) for the layout.** Its tree has no floating
  windows and takes the tabbed shape; and the host has the geometry and the
  pin already, so a crate would supply only the function Go has.
- **Workspaces.** Deferred by the owner. The order and float set are what a
  workspace would hold per set; nothing here forecloses adding one.

## Consequences

### Positive

- The deferred list of ADR-0275 shortens: keyboard cycling, a live tiling,
  and snapping land; the window list and minimise remain.
- The layout functions are pure Go over the geometry ADR-0275 has, so
  layouts, chords and policy are unit-testable without a renderer, and a new
  layout is a function and a menu entry.
- One table and one verb serve the person and the agent; what the agent
  sets, the person sees in the menu, and the other way round.

### Negative

- A tiled window cannot be dragged or resized by the pointer; the order
  changes by chord or menu only. Tiling window managers take a modifier-drag
  for this, and an unmovable egui area gives no drag to read.
- A window whose minimums do not fit overlaps its neighbour, by the owner's
  choice, and the overlapped part is hidden until the window is active. A
  window whose content stretches never reports a need, so it is squeezed
  until its content overflows and only that overflow is learned (ADR-0275).
- egui grows a pinned window around its content rather than holding its
  cell (M1, under *Updates*). So a squeezed window spills over its
  neighbours for a frame or two while a run settles, a window whose need
  exceeds the work area spills past it for as long as it is tiled, and how
  well an app tiles depends on whether its body scrolls. The owner accepted
  these limits on 2026-10-29 rather than change how apps' windows size.
- Every tiled window costs one placement op per frame, on top of the report
  ADR-0275 already carries. Frame-time cost is not measured; it is one small
  op per window.
- Some Alt chords are the browser's in the tab; those bindings are
  unavailable there.
- The opcode table shifts once more; stale browser bundles and headless
  clients fail on the first frame until rebuilt.

### Neutral

- A settling run after each input change can show the layout moving over
  two to four frames, as arrangements do today.
- Tiled mode changes nothing for an app: it is laid out at a size, as it is
  when the person resizes it.

## Migration — Tier 1

- **Breaks.** Nothing on the Go side. The IDL change shifts opcode ids, so a
  Rust client built before it fails on the first frame
  (`unable to convert from representation`).
- **Path.** Rebuild the browser bundle and the headless client; refresh the
  browser host digest in the same commit (`imzero2tab hostdigest --write`).
- **Regeneration.** `app egui2gen generate`; both sides of the FFI.
- **Old shape.** Floating mode is the old shape and stays the default.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the layout functions: each layout
  partitions the work area without overlap for 1–13 windows when the
  minimums fit, overlaps evenly inside the work area when they do not, puts
  the main first, and leaves floating windows out. Default `go test` for the
  chord table's generated Rust arm against the committed file. Unit tests in
  `agent` for the refusals: `layout` without desktop act, `arrange` under
  tiled mode. An integration-lane scene beside the windowhost package, in the
  shape of `TestSceneWindowArrangements`: three windows on a headless host,
  tiled mode set, rects asserted from the accessibility tree; a fourth opened
  and the re-tile asserted; one closed; one floated.
- **What would fail.** A layout that leaves a hole or stacks tiled windows
  while their minimums fit; a settled layout that moves without an input
  change; a chord the shell drops while a text edit has focus; a verb
  accepted without its grant.
- **Gap.** What egui does with a pinned window smaller than its content was
  measured once by M1's spike (under *Updates*), against egui 0.36.2, and is
  not asserted by a lane; an egui upgrade that changes it shows up in the
  integration scene as rects that do not hold. Which Alt chords the browser
  tab passes through is measured by hand and recorded, not asserted.

## Status

Proposed — awaiting review by the code owner.

- **M1 — Spike: pin three windows into a third of the work area.** ✓
  Recorded under *Updates* (2026-10-29); SD2 revised in place from it.
- **M2 — Layouts and the live plan (SD1–SD4).** ✓ With the Desktop layout
  submenu and the env var (SD7), without chords. Recorded under *Updates*
  (2026-10-29).
- **M3 — The shell-chord fetcher and the chord table (SD5).**
- **M4 — The keelson columns and `runtime.agent.layout` (SD6).** With the
  chat tool and the integration scene.

Deferred, from ADR-0275's list and this record: workspaces; a window list in
the Window menu; minimise; persisting the mode, order and float set; dwindle
and scrollable layouts; a modifier-drag to reorder tiled windows.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-10-29 — M1: a pinned window grows around its content; it is not clipped and does not hold its cell

Measured against egui 0.36.2 alone, outside the host, with the window built
the way the interpreter's `windowPlace` path builds it: `resizable(true)`,
`collapsible(false)`, then `fixed_pos`, `fixed_size` and `constrain(false)`
every frame. Three windows were pinned side by side into thirds of a work
area left by a top and a bottom panel, each with a body of 700 by 1200
points inside a cell of about 460 by 840, over twelve frames. The painted
shapes of each window's layer were read before the pass ended, with their
clip rects.

- **A body that allocates past the cell.** The body is given the cell, and
  the overflow the interpreter reports is the difference, as ADR-0275 §SD4
  says. But the window's area and its chrome end at the content's size, from
  the cell's origin, over the neighbouring cells and under the status bar,
  and the only clip on the shapes is the screen. The cause is in egui's
  `Resize`: with resizing off, which `fixed_size` turns off, the container
  "follows the contents" and advances by the laid-out size, and the window
  frame paints around that. `fixed_size` fixes what the body is given, not
  what the window becomes.
- **A long wrapped label.** Wraps to the cell's width, so the width holds;
  the height grows to the text, as above.
- **A body in a scroll area**, and **egui's own `Window::scroll`.** The
  window holds its cell exactly and the content is clipped to the scroll
  viewport. This is the measurement behind the rejected scroll alternative;
  it also says that an app whose body already scrolls tiles cleanly.
- **The one-frame placement**, for contrast: the frame after it, the window
  is back at its content's size and egui has pushed it to the top of the
  screen to fit as much as it can, over the menu bar, which is the behaviour
  ADR-0275's update of 2026-10-04 recorded.

What this changes: nothing in the choice of overlap, and one thing in how
SD2 is worded. The host cannot rely on egui to keep a squeezed window inside
its cell or inside the work area; the settling run's learned minimums are
what make the overlap the even one `fitSpans` computes. SD2 now says so and
names the two limits: a spill of a frame or two while a run settles, and a
window whose need exceeds the work area spilling past it. The spike is not
in the tree; the setup above reproduces it from the egui crate alone.

### 2026-10-29 — M2: the layouts, the live plan, the submenu and the env var

Built as SD1–SD4 and SD7 say, in `windowhost`, with three things the
building settled:

- **The ratio yields before anything overlaps.** Under main and stack, a
  stack column whose minimum does not fit beside the main column at the
  ratio first shrinks the main column towards its own minimum; only when
  both minimums together exceed the work area do the columns overlap. The
  draft had the main column keep its ratio and overlap at once, which
  squeezed the stack for no reason.
- **A window takes its cell a frame after it opens.** A placement is
  emitted with the window's handle, which its first frame has not produced
  yet, so a new window draws once at its default rect and is pinned from the
  next frame. The same limit applies to arrangements.
- **Minimums persist across a work-area change only.** A change of the set,
  the order, the layout or its parameters learns the minimums afresh, as an
  arrangement does; a change of the work area alone (zoom, density, the
  viewport) starts from the minimums already learned, so a resize does not
  squeeze every window again first.

Checked by hand in a headless scene: three windows under each layout, a
switch through the submenu, one window floated. The rects behave as SD2
describes, including the even overlap where a window's minimum is set by a
single long line of its content. The maintained lane for this is M4's
integration scene.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — one viewport; the docking revert of 2026-05-13.
- [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) — the report, the placement, the arrangements.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — the desktop grant and the agent verbs.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md), [ADR-0334](./0334-mdedit-blocks-over-the-source.md) §SD7 — key vocabulary and chord capture.
- [ADR-0148](./0148-app-workingsets.md) — the host-side geometry deferral.
- i3 user's guide (containers, layouts); dwm and xmonad (master and stack, new window as master); river (the layout as an external generator); niri (scrollable tiling).
