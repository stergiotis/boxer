---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0297: inscribe — an overlay agents annotate the desktop through, anchored to windows rather than coordinates

## Context

The chat's coordinator can read the windows ([ADR-0276](./0276-agents-read-and-arrange-windows.md)), capture them ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)) and arrange them, but it cannot show the person anything on screen. The owner wants agents to point: highlight a region, attach a note to it, draw an arrow between two things. The marks should look the same whichever agent draws them.

The idea comes from [bigarrow](https://github.com/franzenzenhofer/big-arrow-on-the-screen), a macOS command-line tool an agent calls to draw a large arrow with a text sign over other windows. Clicks pass through to the app underneath, and each arrow removes itself after a set time or when the agent that drew it exits. It points at coordinates, windows, or UI elements by label. Here the marks are drawn inside the host's own viewport, not over the OS desktop, and they stay until the person clears them (SD8).

Facts that bound the design:

- **One viewport.** Every app is an `egui::Window` in a single OS viewport ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)). The viewport's logical points, origin top-left, are the one global frame. `keelson('windows')` reports each window's outer rect, stacking rank and collapsed flag in that frame. The window host holds the same per-frame snapshot ([ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) SD2).
- **A cross-window paint path exists.** The `paintAbsoluteOverlay` op paints on an `Order::Foreground` layer above every window, in viewport coordinates, and senses nothing, so input passes through it.
- **Geometry moves.** The person drags windows, and an arrangement settles over several frames. A keelson read sees the last completed frame. A rect an agent computes from such a read is stale after the next move.
- **Captures do not say where they came from.** A capture is replayed with the windows where they stand, and its crop is in logical points. The artifact an agent reads does not carry the crop origin or the pixel scale. A downscale obligation (ADR-0281 SD3) would change the scale again.
- **Agents see no widget positions.** A coordinator's accessibility tree is deferred ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) SD12).
- **Marks over a window can spoof it.** An agent that draws text over another window, or beside a grant dialog, can make the person believe something the window does not say.

The owner's answers to the policy questions (2026-10-09):

- annotating a window requires **suggest** mode on it;
- a target hidden behind another window is drawn in a **dashed "behind" style**;
- annotations stay until **the person clears them**;
- widget and resource anchors are **deferred**, and captures stay clean of annotations;
- inscribe is a **singleton**: one instance, shared by every agent, whose annotations it merges;
- the marks use a **neon palette**, outside the design system's palettes.

## Decision

We will add **inscribe**, a system app under `runtime/inscribe` that paints annotations on a full-viewport overlay. Agents name what they mean by anchors — a window, a rect inside a window, a rect inside one of their captures — never by global coordinates. The window host turns an anchor into a viewport rect, every frame, with a pure function. Agents reach inscribe through `runtime.agent.*` verbs, and the person clears what was drawn.

```text
 coordinator ── runtime.agent.annotate {handle, id, op, anchors, text}
   │  grant: suggest on every anchored window          (SD8)
   │  capture anchor → window-local, once              (SD4)
   ↓
 inscribe (singleton overlay)  scene keyed (task, id)  (SD1, SD7)
   │ every frame:
   │   windowhost.Resolve(anchor, snapshot) → rect, visibility   (SD3)
   │   one layout pass over every task's marks                   (SD6, SD7)
   ↓
 paintAbsoluteOverlay  (Foreground, no sense, outside every window span)
```

### SD1 — One overlay app per host

- inscribe is an app with a new surface kind, `SurfaceOverlay`, beside `SurfaceHeadless` and `SurfaceWindowed`. It has no window, no chrome and no input: it draws through `paintAbsoluteOverlay` and senses nothing.
- It is registered as a singleton (`Registry.Register`), and the window host refuses a second overlay surface. One host has one viewport, so one instance serves every agent.
- It is not offered in the launcher. The host starts it when the first annotation arrives and stops it when its scene is empty.

### SD2 — Anchors, not coordinates

An annotation names its targets by anchor:

| Anchor | Means | Authority (SD8) |
| --- | --- | --- |
| `{window}` | the window's outer rect | suggest on the window |
| `{window, rect}` | a rect in the window's local logical points, origin its top-left | suggest on the window |
| `{capture, rect}` | a rect in pixels of a capture artifact the task holds | suggest on the window it lands in |
| `{viewport, rect}` | a rect in viewport points | desktop mode suggest |

No anchor carries a coordinate the agent must compute from another reading.

### SD3 — The window host resolves, every frame

- `windowhost.Resolve(anchor, snapshot)` returns a viewport rect and a visibility: **shown**, **behind** (part of the rect lies under a window ranked further front), **collapsed** or **gone**. It is a pure function of the anchor and the geometry snapshot, like the arrangements, and is unit-tested the same way.
- inscribe calls it on the render goroutine every frame, so a mark follows its window as the window moves.
- The window host owns it because it owns the snapshot and the window identities. The same function serves any later consumer — a tour, the help system — and keelson may expose it as a read, but the drawing path does not go through SQL.

### SD4 — Captures carry their transform, and stay clean

- A capture artifact records how its pixels map to the viewport: the crop origin in points, the effective scale (pixels per point after any transform obligation), and the rect and rank of each window drawn.
- When a `{capture, rect}` anchor arrives, the capture facility maps it to `{window, rect}` once, choosing the front-most recorded window under the rect's centre. From then on it follows that window like any other window anchor. A rect that lands on no recorded window is refused.
- inscribe draws outside every window's span, so the capture replay (ADR-0281 SD4) never contains an annotation. An agent's own marks never reach what it observes. Development captures of the whole viewport do show them.

### SD5 — Operations

| Op | Draws |
| --- | --- |
| `highlight(anchor, label?)` | an outline around the target, with an optional short label |
| `callout(anchor, text)` | a note attached to the target by a leader line |
| `arrow(from, to, label?)` | an arrow between two anchors |
| `step(anchor, text)` | a numbered marker; numbers run per task in arrival order |
| `spotlight(anchors)` | dims the viewport except the targets |
| `clear(id?)` | removes one of the task's annotations, or all of them |

- Each annotation has a caller-chosen `id`. Annotating an existing `(task, id)` replaces it.
- No op takes a colour, width, font or position. Style and placement are inscribe's (SD6).
- Text is untrusted, plain (no markdown), and capped in length.

### SD6 — One visual language, in neon

- **The palette is neon, deliberately outside the design system** ([ADR-0031](./0031-imzero2-design-system-color.md), [ADR-0040](./0040-imzero2-design-system-palette-consolidated.md)). A mark must not read as part of any app. Colours the design system gives apps would let a mark pass for app UI, and a mark that looks like UI is the spoofing case. The exemption is scoped to `runtime/inscribe`, through the design lints' allowlist.
- Each neon stroke is drawn over a dark halo, so the same colours read on the light and the dark theme.
- **Behind** draws the outline dashed where the target lies, and the leader or arrow ends at the nearest visible edge of the target's window. **Collapsed** attaches the mark to the title bar. **Gone** retires the annotation.
- inscribe places labels and notes: one pass over every annotation keeps them clear of each other and inside the work area.
- Every annotation carries its task's attribution marker, so the person can tell who drew it.

### SD7 — Merging annotations from several agents

- One scene holds every annotation, keyed by `(task, id)`. Paint order is arrival order.
- A task is given a neon hue when it first annotates. The hue is kept for the task's life. With more tasks than hues, hues repeat and the attribution marker tells tasks apart. Everything else about the style is the same for every task.
- The layout pass of SD6 runs over all tasks' annotations together. No task's marks are placed without regard to another's.
- A task can replace and clear only its own annotations.
- Each task has a budget of annotations, and the scene has a ceiling. An annotation over either is refused. It never evicts another task's marks.
- `spotlight` from two tasks combines: everything outside the union of their targets is dimmed once.

### SD8 — Authority and lifetime

- `runtime.agent.annotate` and `runtime.agent.clear` are `runtime.agent.*` verbs, so they inherit the grant check, action records and taint handling ([ADR-0276](./0276-agents-read-and-arrange-windows.md) SD3/SD4).
- Annotating requires suggest or act on every anchored window's entry. A viewport anchor requires desktop mode suggest or act.
- Each accepted call is an action record (ADR-0269 SD9) with effect `view`. An annotation changes what the person sees, not any app's state, so ADR-0269 SD11 is unaffected.
- **Annotations stay until the person clears them**: one task's from that task's badge, or all at once from the shell. Agents can clear their own. An annotation whose anchored window leaves the task's grant, or whose grant ends, is retired, since the authority it was drawn under is gone.
- While a host modal is open — a grant request, the Powerbox file dialog — inscribe draws nothing. No mark can stand beside a decision the person is making.

### SD9 — Deferred

- Anchors on widgets, as `{capture, widget id}` against a window tree ([ADR-0301](./0301-window-trees-a-capture-format-naming-each-widget-under-the-message-that-drew-it.md)): the widget's rect, taken relative to its window, then followed as a window anchor. Anchors on ADR-0269 resources stay deferred.
- Animation, freehand drawing, and annotations the person makes.
- A capture that includes the annotations, on request.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.SurfaceE` | `SurfaceOverlay` added | Window host: hosts one overlay, refuses a second; launcher and app center skip it |
| Exported Go API (`windowhost`) | `Resolve`, the anchor types, the visibility enum | — |
| Capture artifact metadata | crop origin, effective scale, drawn windows' rects and ranks (SD4) | The capture service; transform obligations update the scale |
| `runtime.agent.annotate`, `.clear` | Added | `agent.Service` dispatch, wire types, `HostI`, `agent.Client`, the dispatch test host and the chat's test host |
| Chat coordinator | `annotate` / `clear_annotations` tools | Coordinator prompt |
| Shell chrome | "Clear annotations"; per-task clear on the task badge | — |
| `designlint` allowlist | `runtime/inscribe` exempt from the palette rules | — |

## Alternatives

- **Agents send viewport coordinates, computed from `keelson('windows')`.** The rect is a frame late when read and wrong after the next move. It also puts arithmetic on the model.
- **inscribe resolves anchors itself.** It would hold a second copy of window identity and geometry. The capture facility and any other consumer would need the same mapping.
- **Resolution in keelson SQL.** Right for an agent reasoning about the desktop, wrong for a path that must run every frame on the render goroutine.
- **Paint primitives (line, rect, text, colour) instead of semantic ops.** Each agent would invent its own style, and the merge of SD7 would have nothing to lay out.
- **Design-system colours.** Marks would look like app UI, which is both less visible and the spoofing case.
- **One overlay per agent.** Overlapping labels from separate overlays cannot be laid out together, and the person would have several things to clear.
- **Observe mode for annotating.** Annotation proposes something to the person, which is what suggest means. Capture stays at observe.
- **Annotations end with the turn.** The owner chose to leave clearing to the person.

## Consequences

### Positive

- An annotation follows its window through drags and arrangements without the agent re-sending it.
- Every mark looks the same and names its author, whichever agent drew it.
- What an agent observes through capture is never altered by its own marks.

### Negative

- An overlay that is never cleared accumulates. The budget bounds it per task, but the person carries the clean-up.
- Behind-detection is by window rects and stacking, not pixels. A translucent window counts as covering.
- The neon palette is a second colour vocabulary to keep legible as themes change, outside the design system's checks.
- A capture anchor binds to one window. A mark the agent meant to span two windows lands on one of them.

### Neutral

- Hiding the overlay under host modals means marks blink out while the person decides a grant.

## Verification plan

- Unit tests for `Resolve`: each anchor kind, each visibility, a window moved between frames.
- Unit tests for the capture transform: a pixel rect round-trips to window-local and back under a crop and a downscale.
- Unit tests for the scene: replace by id, the ownership refusal, budgets, hue assignment, and a layout pass over two tasks' overlapping labels.
- Agent dispatch tests for the refusals under observe, and without desktop mode.
- A scene document beside the chat's other scenes: a scripted coordinator highlights a window, the window is dragged, and captures before and after show the mark following it, dashed once another window is raised over it.

## Status

Proposed 2026-10-09.

Milestones:

- **M1 — `Resolve` and the anchor types in the window host.** (SD2, SD3)
- **M2 — the transform on capture artifacts.** (SD4)
- **M3 — `SurfaceOverlay`, inscribe's scene, layout and neon style.** (SD1, SD5–SD7)
- **M4 — the agent verbs, authority, clearing and modal suppression, and the chat's tools.** (SD8)
- **M5 — the scene document.**

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — task grants, modes, records, SD12's deferred accessibility tree.
- [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) — the geometry report.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — `keelson('windows')`, desktop mode, the `runtime.agent.*` window verbs.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) — captures, spans and obligations.
- [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) — design lints and their allowlist.
- [bigarrow](https://github.com/franzenzenhofer/big-arrow-on-the-screen) — the tool the idea comes from: an agent-called arrow and sign over the macOS desktop.
