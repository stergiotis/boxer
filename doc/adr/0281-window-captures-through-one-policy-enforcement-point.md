---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0281: Window captures — pixels beside SVG, replayed from the granted windows' stream, through one policy enforcement point

## Context

[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD11 puts capture in the host, "PNG by default, SVG when text or geometry
inside a view matters". What was built is the SVG half: `runtime.agent.capture`
asks the window host to export one window's shapes as SVG
(`windowhost.Inst.OpsCapture`). An SVG export of one window holds that window
and nothing else, so the grant check on the instance was the whole policy; it
also leaves out the window's popups, which live on other layers (`svgexport`'s
window scope).

Pixels do not have that property. The pixel path that exists — the
`RequestScreenshot` / `RequestScreenshotRect` opcodes, egui's
`ViewportCommand::Screenshot` — grabs the whole viewport, and a crop to a
window's rect still holds whatever overlaps it: another app's window, an open
menu, a tooltip. It also encodes the PNG in Rust and writes it to a path the
caller names, so the Go side never holds the pixels and nothing can check, cut
or mark them on the way out. It serves the screenshot tour and development
tooling, which act with shell authority.

The owner wants three things of a pixel capture (design dialogue, 2026-10-04):

- it is a facility of the host, not an operation each app declares;
- it shows only windows a grant covers, and the popups, menus and tooltips a
  covered window opens belong to that window;
- a later step can change what leaves — a watermark first — without
  reopening the request path. That needs one place every capture passes
  through, where a decision is made and its conditions are carried out: a
  policy enforcement point in XACML's sense, with the decision taken
  separately by a policy decision point.

Facts about the renderer that bear on the design (egui 0.35, code reading
2026-10-04):

- The paint order of layers is public (`Memory::layer_ids`, back to front,
  popups and tooltips included). Which window opened a popup is not: egui
  records it only in crate-private per-pass state, and tooltips not at all.
- A layer's rect is its frame (`AreaState::rect`), not its painted shape.
- A window's body arrives inline in the FFFI2 stream — the window's opcode,
  its body, its end — and the interpreter already re-runs opcode bytes from
  memory for deferred blocks (`replay_deferred_block`). Everything a window
  draws, its popups included, is emitted inside that span.
- The desktop host and the `headless_wgpu` / `headless_soft` hosts hold a
  frame buffer; the mesh-only `headless` build and the browser tab
  ([ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md))
  do not hold one Go can reach. The software rasterizer of `headless_soft`
  (`softraster`) rasterizes any tessellated frame on the CPU.

A spike on 2026-10-04 (`headless_soft`; three apps open at 1400×900, and six
of play's scenes at 1920×1200) tested the option chosen below; its figures are
quoted where they decide something.

## Design space (QOC)

**Question.** Where are the pixels a grant does not cover removed?

**Options.**

- **O1** — crop the live frame to the window's rect, and accept what overlaps it.
- **O2** — Rust hands Go the live frame's pixels and its layer table; Go
  computes per-pixel ownership and the PEP redacts.
- **O3** — redaction inside the Rust screenshot handler, policy passed down.
- **O4** — replay the frame's FFFI2 bytes of the granted windows only, into a
  separate, input-less egui context initialised from a copy of the live
  context's state, and rasterize that.

**Criteria.**

- **C1** — no pixel of an uncovered window leaves, popups and overlaps
  included.
- **C2** — a covered window's popups are kept, without heuristics.
- **C3** — the decision and every later obligation live in one place, in Go,
  beside the grant.
- **C4** — the same path on every host.
- **C5** — the capture shows what the person saw.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | −  | ++ |
| C2 | −  | −  | −  | ++ |
| C3 | −  | ++ | −− | +  |
| C4 | +  | +  | +  | ++ |
| C5 | +  | ++ | ++ | −  |

O4 is chosen. O2 was the first draft. Its stacking can be exact, since
`Memory::layer_ids` is public, but it covers by rects, and that errs both
ways: inside a covered popup's rounded corners, under a translucent fill or a
modal's backdrop, the uncovered window beneath shows through. Its popup
attribution would bracket each window body or patch egui. O4 never draws an
uncovered window, and a popup is the window's because it was emitted inside
the window's span. O3 splits the policy across the FFI boundary, so a
watermark would be Rust code with a Go decision. O1 fails the requirement.
O4's cost on C5 is named under Consequences.

## Decision

We will add a capture facility to the host, `runtime/capture`, through which
every product capture passes — pixel and SVG. Its service is the policy
enforcement point. It asks a policy decision point for a decision, selects
the granted windows' spans of one recorded frame, has the renderer replay
them into a capture context and return the pixels or the SVG as bytes,
carries out every obligation the decision attaches, and hands back a
labelled artifact handle, recording the whole on the audit trail.

```text
 caller ── runtime.agent.capture {handle, instances, format, crop, key}
   │
   ↓
 PEP  capture.Service ─── facts (PIP): grant entries, window labels,
   │                       model locality
   │ 1. Decide ─────────→ PDP  PolicyI.Decide(request, facts)
   │ ←─ Permit | Deny, obligations[]
   │ 2. record the next frame's stream; each window's span marked
   │ 3. scope: keep the granted windows' spans; re-check the grant
   │ 4. renderer: replay(spans, copy of live state) → RGBA | SVG
   │ 5. transform obligations, then encode
   │ 6. label, digest, trail row, artifact handle
   ↓
 artifact, read only through runtime.agent.read in the task
```

### SD1 — One facility; who goes through it

- **Product paths go through the PEP**: `runtime.agent.capture` now, and any
  later capture a person or an app asks for. The service is the only code in
  a product path that asks the renderer for pixels or an SVG.
- **Development paths stay outside**: `IMZERO2_SCREENSHOT_DIR`, the scene
  runner's `capture` step, the headless driver, egui-mcp. They act with the
  authority of the shell that launched them (ADR-0154's development agents).
  The host refuses the `RequestScreenshot*` opcodes unless a development flag
  is set, so a product package cannot reach them by accident.
- **SVG moves behind the PEP**: the format is a field of the request, and the
  decision, the obligations and the record are the same for both.

### SD2 — Decision and enforcement are separate

- **The request** names a subject (a task's grant; later a person or an app),
  the action (`capture`), the format (`svg` | `png`), the windows (instance
  keys; empty means every window of the task), and an optional crop in
  logical points.
- **The PDP** is a Go interface, `capture.PolicyI`, returning a decision:
  `Permit` or `Deny` with a reason, and a list of obligations. Policies
  compose deny-overrides: any `Deny` wins, and the obligations of every
  `Permit` are joined. The first policy is ADR-0269's grant rule — every
  named window has an entry in the task, in any mode, observe included
  (§SD3) — and it attaches the `scope` obligation of SD4.
- **The PIP** is what the PDP reads and never writes: the grant's entries and
  modes, each window's label (ADR-0145), the model endpoint's locality
  (ADR-0254 §SD3).
- **The PEP** never decides. It enforces what it was told, and it is the only
  holder of the bytes until the artifact is written.

### SD3 — Obligations fail closed

- An obligation is a named, versioned value with typed parameters.
- The PEP holds a registry of handlers keyed by obligation and format. A
  decision carrying an obligation with no handler for the request's format is
  enforced as `Deny`, with a reason naming the obligation. A future watermark
  obligation therefore denies SVG captures until an SVG handler exists, rather
  than letting them through unmarked.
- Handlers run in a fixed phase order: **scope** (what is drawn — the spans,
  the crop), **transform** (what is added or degraded — watermark,
  downscale), **encode** (PNG; SVG is already encoded). Transform handlers
  work on the RGBA buffer, before encoding.
- The record names every obligation applied, with its version.

### SD4 — Coverage: only the granted windows' spans are drawn

- **Spans.** While a capture is pending, the window host records the next
  frame's outgoing stream below the writer's buffering, and marks where each
  window's emission begins and ends — the window's opcode through its end,
  together with the placement and raise opcodes the host sends for it. Spans
  lie on message boundaries.
- **The `scope` obligation** carries the windows and the crop. Its handler
  keeps the granted windows' spans and drops everything else: other windows,
  the shell's chrome, the desktop. Whatever a granted window emits — its
  popups, menus, tooltips, its dialogs — is in its span and is kept; nothing
  else is drawn.
- **A granted window shows whole.** A part another window covered on screen
  is drawn, since the covering window is not.
- **The grant is checked again** when the spans are selected, a frame after
  the decision.
- **The PEP hashes the spans it replays**, so the record can name the input a
  capture came from.

### SD5 — The replay, and what it must not disturb

- **The capture context.** The renderer replays the spans into an egui
  context with no input, initialised from a copy of the live context's
  `Memory` (window positions and sizes, scroll offsets, open states), then its
  options and its fonts — in that order, since `Memory` also holds the options
  and any pending font definitions and copying it last discards both (the
  spike's first run). The result is tessellated and rasterized by
  `softraster`, or handed to the SVG exporter, which then shows popups too.
- **Isolation.** The replay is an interpretation of the same bytes, so the
  interpreter's effects are fenced:
  - every write toward Go is an error during a capture replay (in the spike
    each replay tried to send 270–2 255 bytes of replies, all of them
    `Fetch*` answers);
  - the replay starts with empty per-frame registers, and the live ones are
    put back after it; state that outlives a frame — dock layouts,
    column-width epochs, window open bindings — is seen by the replay and
    restored after it;
  - the image and scrolling-texture caches upload nothing and change no live
    entry; a texture the live context does not hold is not drawn, and is
    counted;
  - each node declares in the IDL what its apply code reaches (`ir.EffectE`):
    a procedural node must declare `EffectLocal` or `EffectHost`, and the
    generator refuses one that does not; a builder factory draws and a fetcher
    answers the server, by their kinds. The generated apply code of a
    host-effect node and of every fetcher is skipped during a capture replay,
    after its arguments are read. Ten procedural nodes are host-effect: SVG
    and screenshot export, the video pipeline, clipboard, viewport close,
    window placement, animation freeze, and the frame's register reset.
- **Textures.** Images resolve from a CPU mirror of the live context's
  textures; text from the capture context's own font atlas. A mesh whose
  texture is unknown is counted, since the rasterizer skips it silently.
- **Per host.** Every native host replays and rasterizes the same way: the
  desktop host, both headless pixel hosts, and the mesh-only host, which links
  the software rasterizer for capture. The browser tab refuses (SD7).
- **Fidelity measured by the spike** (`headless_soft`, 2026-10-04).
  Replaying a whole frame against the live frame of the same pass differed
  by at most 3 of 255 per channel, in 0–744 of 1 260 000 pixels across four
  desktop scenes at 1400×900 (a scrolling texture and an open combo box among
  them), and in 12–447 of 2 304 000 pixels across six of play's scenes at
  1920×1200 (its dock, a result table, a chart, the kanban board, the map's
  raster texture, and two open menus). Dropping one window's span removed
  that window and its open combo popup, and the live session continued
  without a desync.

### SD6 — Labels, taint and records

- A capture's label is the highest label of the windows it replayed. A
  confined capture stays an artifact handle `read` does not open for a model
  the locality rule refuses. This replaces ADR-0269 §SD7's "any window
  intersecting the captured area", which assumed a capture of the screen.
- Every capture is untrusted and taints the conversation, as now.
- Each capture is one trail row ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md)):
  the request, the decision and the policy that took it, the obligations
  applied with their versions, the windows replayed and the span digest, the
  format and size, the label, and a digest of the bytes handed out — the
  bytes after every obligation, so a later watermark is part of what the
  digest covers.

### SD7 — Deferred, recorded

- **A capture of what was on screen**, for a person or for evidence: the
  live frame with occlusion and hover. Its source would be O2's redaction,
  with shape-aware masks; it is its own decision.
- **Pixels from the browser tab**: the software rasterizer in the tab's
  worker, or a readback from its painter.
- **The chat's capture tool**: a coordinator tool that captures and passes
  the PNG to a vision-capable model (`llm` messages carry images). ADR-0265
  §SD5 lists it; it is built on this ADR.
- **Watermarking** and other transform obligations: the reason for SD3, each
  its own decision.
- **Person- and app-initiated captures**: a subject other than a task grant,
  and the policy that decides for it.
- **The SVG export opcodes** (`ExportSvg`, `ExportSvgWindow`): whether they
  stay once this facility serves their product callers is decided after it is
  built.
- **A capture context kept between captures**, to keep its font atlas and
  text layouts warm.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `runtime/capture` (new, under `public/keelson/runtime`) | added: the service (PEP), `PolicyI` (PDP), the obligation registry | the agent service's `capture` handler, which calls it |
| `runtime.agent.capture` wire (`buscodec`) | added fields: format, instances, crop; the existing single instance still accepted, SVG the default | the agent client; the chat coordinator's tests |
| FFFI2 runtime (Go writer) | added: recording a frame's outgoing bytes below the buffered writer, with span marks | the window host's `Frame` |
| egui2 IDL | added: a capture-replay opcode taking spans and returning pixels or SVG as a fetch; an effect mark on opcodes, with a generator check | `app egui2gen generate`; both sides of the FFFI boundary rebuilt |
| Interpreter (Rust) | added: capture-replay mode — writes refused, registers cleared, caches read-only, marked opcodes skipped | the image and scrolling-texture caches |
| Mesh-only `headless` Rust build | added: the software rasterizer, for capture | the cargo features of the headless builds; the license gate's crate tree |
| `windowhost.Inst.OpsCapture` | reshaped: span marking and the source for the PEP, no longer the policy | the agent dispatcher's host interface and its fakes |
| `RequestScreenshot*` opcodes | narrowed: refused without a development flag | the screenshot tour, play's capture knobs |
| Trail kinds (`boxer.facts`) | added: the capture record | the runtime vocabulary cohort and its golden |
| ADR-0269 §SD7, §SD11 | amended: the capture label is the replayed windows'; capture goes through this ADR's PEP | dated Updates on ADR-0269 |

## Alternatives

- **A capture operation per app.** Rejected by the owner: every app would
  implement the same thing, and none of them can see what overlaps its window.
- **Crop the live frame** (O1). Overlaps leak, and nothing can mark the image.
- **Redact the live frame from a layer table** (O2). Rect coverage lets an
  uncovered window show through a covered layer's corners, translucent fills
  and modal backdrops, and popup ownership is private to egui. Kept as the
  source of a deferred "what was on screen" capture.
- **Redact in Rust** (O3). Splits the policy across the FFI boundary.
- **Replay in the live context, as a second viewport.** Shares fonts and
  textures, but egui's memory, focus and interaction state are shared across
  viewports, so the replay would change the live UI.
- **Every pixel exit through the PEP, development paths included.** One choke
  point, at the cost of moving the tour, the scene runner and the driver behind
  a policy built for task grants; the owner chose product paths.
- **Obligations skipped when unsupported.** A watermark that silently does
  not apply to one format is the failure an enforcement point exists to
  prevent.

## Consequences

### Positive

- A capture a model receives was drawn from the shared windows only; the
  pixels of the others are never produced.
- A covered window's popups and dialogs are in its capture with no
  attribution rule.
- One source on every native host, and for both formats; the SVG gains the
  popups the window-scoped export leaves out.
- A later watermark is a handler and a policy line; the request path, the
  wire and the record do not change.

### Negative

- A capture is not what the person saw: no hover or pressed state, no
  occlusion, and one frame late.
- Every opcode carries an effect mark from now on, and a mis-marked one
  repeats its effect during a capture.
- The interpreter gains a mode whose isolation rules every cache that holds
  state across frames must follow.
- The replay costs a pass on the render thread. For a cold capture context
  the spike measured 13–35 ms at 1400×900 and 29–54 ms for play at
  1920×1200, where the live frame's own interpretation took a few
  milliseconds, plus 4–63 ms of rasterizing, the high end when the rasterizer
  first receives the textures (2026-10-04).
- The mesh-only build grows by the software rasterizer.

### Neutral

- Development captures remain outside any policy, as now, and are now named
  as such.

## Migration — Tier 1

Nothing to migrate: the wire change is additive, an SVG request without a
format behaves as before, and the development opcodes keep working under
their flag.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the PEP against a fake renderer — an unknown
  obligation denies, deny-overrides composition, the scope handler keeps
  exactly the granted spans. The Rust unit tests: a write during a capture
  replay fails; the registers are empty after one; a marked opcode does not
  run. A headless scene on the `headless_soft` host: two windows overlapping,
  the unshared one with a combo box open over the shared one; the capture of
  the shared window shows neither, and the shared window's own open popup
  appears. The generator check fails on an unmarked opcode.
- **What would fail.** A pixel of an uncovered window in a capture; a capture
  produced while an obligation had no handler; a byte written to Go during a
  replay; a product package reaching the screenshot opcodes.
- **Gap.** Replay fidelity against the live frame is a spike measurement, not
  a lane; the desktop host is checked live; the browser tab is not covered
  because it refuses.

## Status

Proposed — awaiting review by the code owner.

Milestones:

- **M1 — Replay in the interpreter**: the capture context, isolation, the
  effect marks. Built 2026-10-04, uncommitted.
- **M2 — Spans**: recording below the writer, span marks in the window host,
  the replay opcode and its fetch.
- **M3 — The PEP, PDP and `scope` handler**; SVG moved behind it (SD1–SD4).
- **M4 — Records and labels** (SD6), and the wire fields.
- **M5 — The mesh-only and desktop hosts.**

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — task grants, §SD7 labels, §SD11 capture.
- [ADR-0145](./0145-sealed-app-data.md) — sensitivity labels.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — development agents and the driver.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the locality rule.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the coordinator; §SD5 defers a capture tool.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail.
- [egui-software-backend-survey](../adr-background-work/egui-software-backend-survey.md) — the software rasterizer's fidelity and cost.
- OASIS, *eXtensible Access Control Markup Language (XACML) Version 3.0* (2013) — PEP, PDP, PIP, obligations, deny-overrides.
