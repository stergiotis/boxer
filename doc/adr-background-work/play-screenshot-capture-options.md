---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-07-31 while building a
> feature-tour capture for the `play` app; nothing here is a decision. Provenance
> is three-tiered: (a) claims about this repository were verified against the
> working tree on the compile date; (b) the timings and the two capability probes
> in §2, §4 and §5 were *measured* on the compile date on one headless box (one
> build, one machine; §2 and §4 under a private headless weston, §5 with no
> compositor at all) — observations, not proofs; (c) effort figures for unbuilt
> work are estimates and are marked as such.
>
> **Decision taken, same day.** Option D was chosen and built; the resulting
> record is [ADR-0154](../adr/0154-headless-carrier-tree-and-driver.md), which
> is authoritative for how the seam works now. This page is the costing that
> fed it, kept as a snapshot of the reasoning — in particular the measurements
> of A and C, which the ADR does not repeat. What it says about D being unbuilt
> ("driver unwritten") was true when compiled and is not any more.

# Capturing the play feature surface — four options, costed

## 1 Question

`play` has grown a feature surface — eighteen dock tabs, two editor-side
reference panes, parameter tiers, a reactive query graph, applet export — that
nobody can review by reading a changelog. The question is how to produce a
gallery of screenshots covering as much of it as possible, **automatically and
repeatably**, preferring a runnable script over a prompt an agent interprets
afresh each time.

Four candidate answers are costed here: **A** keep extending the
environment-knob capture already in the tree; **B** build
[ADR-0127](../adr/0127-imzero2-interaction-record-replay.md)'s interaction
record/replay and drive the app the way a person does; **C** script the
already-installed `egui-mcp` binary directly, with no agent in the loop; **D**
drive and capture through [ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md)'s
headless pixel-streaming host, which needs no compositor at all. C and D were
found during the costing and are the reason this page is not a two-way
comparison.

## 2 What the environment-knob route reaches (measured)

`play` registers 26 `BOXER_PLAY_*` variables (ADR-0009 registry,
[doc/env-vars.md](../env-vars.md)). The relevant ones seed the editor buffer,
auto-run it, pick the default-active body tab, pin Map centre/zoom/table, seed
the Timeline bands query, start Preview in *as sent* mode, and arm a capture
that fires once a result lands.

Together they express **the state an app can be launched into**. Built out as
`scripts/dev/play-screenshot-tour.sh`, one launch per scene:

| Measure | Value |
| --- | --- |
| Scenes defined | 24 |
| Scenes captured | 22 |
| Wall clock, whole run | ~65 s (~2–3 s per scene) |
| Artifacts | framebuffer PNG + vector SVG, 1920×1139 after cropping |
| Live app driven | no — each scene is a fresh process that exits itself |

Covered: every body tab with data shaped for its contract (Table both leeway
and ad-hoc, Timeline intervals with a bands channel, Map raster, World
choropleth, Kanban board, Network graph, Projection, Schema, Graph, Passes,
Diagnostics, Flow, Snippets), both Preview modes, pinned parameters, a
multi-statement buffer, an error state, a wide result, and a long scan.

Two scenes do not capture, for one reason: the capture arms only once a record
or an error has landed (`apps/play/play_renderer.go:1501`), so a Run **blocked
by an unfilled placeholder** and a **zero-row result** never fire it — exactly
the two empty states a tour would want. That is a small fix in the arm
condition, not a property of the approach.

### The ceiling

What the knobs cannot express is not a gap in the knob set — it is a category.
A menu, a dialog, a drag, a second run, a caret placed on a token: none of
these is a state the app can *start* in. Seeding them would mean inventing
launch-time state for things that only exist as the consequence of an
interaction. The list, from the feature reference and the app's own chrome:

- the **Panes menu** (the ADR-0097 prose surface that explains each pane's
  verdict) and the **Endpoint** menu
- the **Subquery toggle** and everything it draws — query tinting, environment
  underlines, unresolvable-reference marks (some forty lines of the feature
  reference)
- the **Conditions** toggle (ADR-0121)
- Table's leeway display modes, pagination, and **Pin result** (ADR-0115)
- **History** with more than one run in it
- the Graph tab's **signal set** and **Live** re-run — the reactive story's
  headline, where a panel write re-runs a dependent query
- per-pane **node binding** (ADR-0097 slice 6c)
- **Kanban drag** between lanes, **Timeline brush**, **column-width drag**
  (ADR-0151)
- **selection cross-filtering** — click a row, watch a dependent query move
- the **Docs pane following the caret** (a fresh buffer's caret is at offset 0)
- **Save as applet…** (ADR-0132/0135) and the **Load .sql** Powerbox dialog
- workingset save/restore (ADR-0148)

Roughly twenty states, and they are disproportionately the *interesting* ones:
the panels are what play looks like, these are what play does.

Marginal cost of pushing the knob route further: one new registered variable
per state, permanently, in shipped app code — plus, for the menus and dialogs,
a fabricated "open" state that has no other reason to exist.

## 3 What ADR-0127 would cost

[ADR-0127](../adr/0127-imzero2-interaction-record-replay.md) (proposed
2026-07-17, unimplemented — no `RecorderPlugin`, no `IMZERO2_RECORD`, no
`app imzero2 replay` in the tree) decides semantic interaction record/replay
over the `egui_inspection` seam. Its milestones:

- **M1** — a `RecorderPlugin` in the Rust client: raw-input hook, output-event
  hook, `interaction_snapshot` attribution, tree snapshots, JSONL dual-layer
  steps, ring buffer with export-on-panic.
- **M2** — the script emitter: coalescing, dual anchors, auto-waits.
- **M3** — `app imzero2 replay`: a Go client speaking the inspection wire
  (4-byte big-endian length + MessagePack), anchor ladder, navigate/verify.
- **M4** — heal-on-green and the teach-in how-to.

Costs specific to this repository, verified on the compile date:

- Boxer has **no MessagePack dependency** (zero hits in `go.sum`); M3 adds one,
  which is a supply-chain review under
  [ENGINEERING_PRACTICES](../ENGINEERING_PRACTICES.md), or a hand-rolled codec.
- M1 is Rust against egui's still-young `Plugin` API — a new maintenance
  surface the ADR names in its own Consequences.
- Estimated (not measured) weight: several hundred lines of Rust for M1–M2 and
  the better part of a thousand lines of Go for M3, plus the anchor ladder's
  test surface.

None of that is wasted work — the ADR lists five consumers, and screenshots are
the fourth. But **for screenshots specifically, none of it is on the critical
path**, because of §4.

## 4 The third option: script the MCP server (measured)

ADR-0127's own §SD6 orders its executors, and rung 1 is "via egui-mcp, day one,
no new code". The unstated corollary, confirmed here: *an agent is not required
to be the thing calling it.*

`egui-mcp` is an ordinary stdio JSON-RPC process. Probed on the compile date:

- `initialize` + `tools/list` over pipes returns 15 tools — `attach`,
  `query_tree`, `get_node`, `click`, `drag`, `hover`, `scroll`, `type_text`,
  `press_key`, `wait_for`, `resize`, `screenshot`, `batch`, `status`,
  `disconnect`.
- Driving a live `play` (launched with `EGUI_INSPECTION=1` under the same
  headless weston) from ~40 lines of Python: `attach` succeeded on
  `127.0.0.1:5719`; `click` targeted by label text resolved to a widget id and
  reported the position it clicked; `screenshot` returned a base64 PNG.
- The captured frame is the **Panes menu open**, showing each pane's rejection
  prose — a state no environment knob can seed.

Properties that matter for the comparison:

- Targeting is by **locator** (label, role, id), not coordinates — the same
  anchor idea ADR-0127 §SD4 specifies, resolved against the live AccessKit
  tree. Resolution-independent by construction.
- The capture path is the inspection seam's own screenshot, not
  `BOXER_PLAY_SCREENSHOT`, so it captures overlays and menus and needs no
  cooperation from the app.
- The `inspection` feature already rides in the desktop default build
  (`rust/imzero2/Cargo.toml`), so nothing is rebuilt to enable it.
- Zero changes to boxer. The trace is a data file in the repo; the driver is a
  script.

What it does **not** give, and what M1/M2 exist to fix: the trace is
hand-authored. Writing one means knowing the labels to target — cheap for
twenty states, tedious and rot-prone for two hundred, which is exactly the
authoring cost a recorder removes.

## 5 The fourth option: the headless pixel-streaming host (measured)

[ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md)'s headless host
is shipped: a `headless_wgpu` build that links no window system, renders
offscreen, and serves an H.264 stream plus an input channel over one WebSocket
(`rust/imzero2/hmi_headless.sh`). Its browser viewer is not the only possible
client — the tree already carries a non-browser one (`imzero2_ws_probe`), and
the wire is a documented protobuf schema
(`proto/boxer/imzero2/v1/input.proto`).

Two properties make it a capture substrate rather than just a remote-access
feature:

- **`IMZERO2_HEADLESS_DUMP_DIR` + `_DUMP_EVERY`** write per-frame PNGs straight
  out of the render host — no encoder, no browser, no framebuffer readback.
- **`IMZERO2_HEADLESS_LISTEN` may be empty** ("disables remote access") and
  **`IMZERO2_HEADLESS_MAX_FRAMES`** stops after N frames, so a capture run needs
  no viewer and terminates itself.

### Measured on the compile date

| Probe | Result |
| --- | --- |
| `play` scene, no carrier, no compositor, `MAX_FRAMES=90` | clean exit in **4 s**, three PNGs at 1920×1200, zero errors |
| Visual result vs the desktop/weston path | equivalent |
| Input injection (`imzero2_ws_probe`, click at 851,114) | **Conditions checkbox toggled** — round-trip confirmed |
| Roster role | `you=#1 active` — the first connection is admitted active, and input is honoured only from the active one |

No weston, no X, no compositor lifecycle, no PNG-versus-SVG race, and no crop
step beyond the host's own chrome margins. For the static scenes this is
strictly simpler than what §2 was built on.

The input vocabulary is complete for driving a UI: `MouseMove`, `MouseButton`
(position, button, pressed, modifiers), `MouseWheel`, `KeyEvent`, `TextInput`,
`PointerGone`, `PinchZoom`, plus `SessionControl` for resize, cadence and
takeover.

### Two gaps, one of them structural

**Structural: coordinate-only targeting.** The headless host cannot compile
`inspection` — by design — so there is no AccessKit tree, no `query_tree`, no
locators. A step is a click at (x, y). That is exactly the ADR-0127 O1/O4 class
the record/replay ADR rejected for robustness: pinned window size, data and dock
layout make coordinates deterministic *within a scene*, but any UI change moves
them **silently**, and a wrong coordinate captures a plausible-looking wrong
frame rather than failing.

**Mechanical: no driver exists.** `ws_probe` is a verification tool whose
gestures are scheduled by *received video access unit* count. With the default
reactive cadence and the carrier's blake3 frame dedup, a static UI emits almost
no access units — so the probe's clicks never fired until `cadence 0`
(continuous) was forced. That cost two runs to diagnose and is the wrong
scheduling model for a tour; a driver should pace on wall clock or on the host's
frame counter. A better driver is cheapest **in Rust**, where the crate already
has the protobuf types, `tokio-tungstenite`, and `ws_probe`'s 359 lines to start
from — no new dependency. A Go driver would need a WebSocket client (none in
`go.mod`), Go codegen for `input.proto` (only the Rust side generates today),
and `google.golang.org/protobuf` promoted from indirect.

### Composing away the structural gap

The two seams share one coordinate frame — both egui-mcp and the headless input
protocol speak logical points, and at `pixels_per_point: 1.0` those equal the
dumped PNG's pixels. Observed: the MCP `click` on **Panes** reported
`pos: [792.5, 114.0]`, and in the headless PNG of the same pinned layout that
button sits at the same place.

So option C can *generate* option D's coordinates: attach egui-mcp to the
desktop build once, `query_tree` for each target widget's `bounds` centre, and
emit those into the headless trace. D's silently-rotting hand-guessed
coordinates become a regenerable artifact, and the regeneration step is the
thing that fails loudly when a label moves.

## 6 Comparison

| | A — env knobs | B — ADR-0127 M1–M3 | C — scripted egui-mcp | D — headless host |
| --- | --- | --- | --- | --- |
| Reaches launch-time states | yes | yes | yes | yes |
| Reaches gesture-only states | **no**, by category | yes | yes | yes |
| Targeting | n/a | locator, healing ladder | locator | **coordinate only** |
| Needs a compositor | yes (weston) | yes | yes (weston) | **no** |
| Capture path | app-side readback, PNG+SVG | via the seam | via the seam | **render host writes PNGs** |
| New code in boxer | one knob per state, permanent | Rust plugin + Go client + new dep | none | a driver (Rust, no new dep) |
| Status | built, 22/24 scenes, ~65 s | unimplemented | probed working, driver unwritten | probed working, driver unwritten |
| Flake surface | none beyond the compositor | waits, anchor resolution | waits, anchor resolution | waits, **silent** coordinate rot |
| Authoring cost per state | one Go branch + registry entry | demonstrate once | write a locator step | write a coordinate step |
| Serves other consumers | no | repros, tests, teach-in, perf | marginally | it *is* the deploy/remote path |

## 7 Reading

**D replaces A's substrate outright for the static scenes.** Same frames, no
compositor, PNGs straight from the render host, self-terminating. Everything in
§2 that is scene definitions survives the swap; only the launcher changes. That
is a simplification, not a new capability, and it is cheap.

**For gestures, C and D differ on one axis that matters more than cost:** C
targets by locator and D by coordinate. A locator that no longer resolves fails
loudly; a coordinate that no longer points at the button captures a plausible
wrong frame. In a UI under active development that asymmetry dominates — which
is the same argument ADR-0127 §QOC used to reject O1/O4. The composition in §5
(generate D's coordinates from C's tree) is what makes D's gesture story
defensible, and it needs C anyway.

**B remains a separate decision.** It is a real capability with four consumers
beyond this one, and should be argued on those — deterministic panic repros,
interaction regression tests, teach-in — not on screenshots, which C and D
already reach. Its Motivation item 4 (*deterministic demo re-capture*) is the
weakest leg of its own case once C and D are on the table; the recorder's value
here is authoring ergonomics at scale, which only bites well past the ~20 states
this app currently needs.

**Note what D is not.** It is the deployment and remote-access path
(ADR-0024/0082/0086). Using it as the tour substrate means the gallery is
rendered by the same host an operator would connect to — which is a fidelity
argument in its favour, and also means tour breakage and remote-access breakage
are the same breakage.

The one thing worth doing regardless of the choice: fix the capture arm
condition so a blocked Run and a zero-row result can be captured at all.

## References

- [ADR-0127](../adr/0127-imzero2-interaction-record-replay.md) — interaction
  record/replay over the inspection seam (proposed, unimplemented).
- [How to drive imzero2 from an AI agent with egui_mcp](../howto/egui-mcp.md) —
  the seam both C and ADR-0127 replay through.
- [ADR-0057](../adr/0057-demo-registry-and-drivers.md) — the demo registry and
  the headless-weston arrangement the tour reuses.
- [ADR-0009](../adr/0009-environment-variable-registry.md) — why every knob in
  option A is permanent registered surface.
- `scripts/dev/play-screenshot-tour.sh` — option A as built.
