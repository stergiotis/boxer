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

The chat's coordinator can read the windows ([ADR-0276](./0276-agents-read-and-arrange-windows.md)), capture them ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)), read what they show ([ADR-0301](./0301-window-trees-a-capture-format-naming-each-widget-under-the-message-that-drew-it.md)) and arrange them, but it cannot show the person anything on screen. The owner wants agents to point: highlight a region, attach a note to it, draw an arrow between two things. The marks should look the same whichever agent draws them.

The idea comes from [bigarrow](https://github.com/franzenzenhofer/big-arrow-on-the-screen), a macOS command-line tool an agent calls to draw a large arrow with a text sign over other windows. Clicks pass through to the app underneath, and each arrow removes itself after a set time or when the agent that drew it exits. It points at coordinates, windows, or UI elements by label. Here the marks are drawn inside the host's own viewport, not over the OS desktop, and they stay until the person clears them (SD8).

Facts that bound the design:

- **One viewport.** Every app is an `egui::Window` in a single OS viewport ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)). The viewport's logical points, origin top-left, are the one global frame. The window host holds each window's outer rect, stacking rank and collapsed flag from the last completed frame ([ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) SD2); `keelson('windows')` serves the same snapshot.
- **A cross-window paint path exists.** `paintAbsoluteOverlay` paints on an `Order::Foreground` layer above every window, in viewport coordinates, and senses nothing, so input passes through it. The paint commands include dashed lines, arrows, filled and stroked rects, and text. Apps use the same layer for their own connectors, from inside their windows.
- **Geometry moves.** The person drags windows, and an arrangement settles over several frames. A rect computed once from a read is stale after the next move.
- **A window tree locates widgets.** ADR-0301's tree gives each widget's rect in viewport points, names the window each top-level part lies in with that window's rect when the tree was taken, and gives the model a reference for each part (`#12`, `#12.3`) under a tree reference (`t3`).
- **Text is measured a frame late.** egui reports a text's size through a fetcher, one frame after it was asked (`MeasureTextSize`).
- **Marks over a window can spoof it.** An agent that draws text over another window, or beside a grant dialog, can make the person believe something the window does not say.
- **Host modals.** The grant request, the confirmation of a consequential command and a moderator's question are modals the agent chrome draws; the Powerbox file dialog is another.

The owner's answers to the policy questions (2026-10-09):

- annotating a window requires **suggest** mode on it;
- a target hidden behind another window is drawn in a **dashed "behind" style**;
- annotations stay until **the person clears them**;
- captures stay clean of annotations;
- inscribe is a **singleton**: one instance, shared by every agent, whose annotations it merges;
- the marks use a **neon palette**, outside the design system's palettes;
- inscribe is a **host component**, not an app with a surface of its own (SD1).

## Decision

We will add **inscribe**, a component of the window host under `runtime/inscribe` that paints annotations above every window. Agents name what they mean by anchors — a window, a rect inside a window, a part of a window tree — never by viewport coordinates they computed. The window host turns an anchor into a viewport rect every frame, with a pure function. Agents reach inscribe through `runtime.agent.*` verbs, and the person clears what was drawn.

```text
 model ── annotate {op, targets: [{tree, "#12.3"} | {window} | …], text}
   │  chat: tree part → {window, rect relative to the window}       (SD4)
   ↓
 runtime.agent.annotate {handle, id, op, anchors, text}
   │  grant: suggest on every anchored window                       (SD8)
   ↓
 window host ─ inscribe: one scene, keyed (task, id)                (SD1, SD7)
   │ every frame, after every window and the shell chrome:
   │   windowhost.Resolve(anchor, geometry) → rect, visibility      (SD3)
   │   one layout pass over every task's marks                      (SD6, SD7)
   ↓
 paintAbsoluteOverlay  (Foreground, no sense, outside every window span)
```

### SD1 — A component of the window host

- inscribe is a package the window host holds one of and frames once per frame, after every window and the shell chrome. One host has one viewport, so one instance serves every agent.
- It has no window, no chrome and no input: it draws through `paintAbsoluteOverlay` and senses nothing.
- While its scene is empty it draws nothing and costs one length check per frame.
- It has no manifest and is not offered in the launcher. An app with a surface of its own was weighed (*Alternatives*).

### SD2 — Anchors, not coordinates

The host takes three kinds of anchor:

| Anchor | Means | Authority (SD8) |
| --- | --- | --- |
| `{window}` | the window's outer rect | suggest on the window |
| `{window, rect}` | a rect relative to the window's top-left corner, in logical points | suggest on the window |
| `{viewport, rect}` | a rect in viewport points | desktop mode suggest |

A model never computes the rect of the second kind; SD4 says where it comes from.

### SD3 — The window host resolves, every frame

- `windowhost.Resolve(anchor, geometry)` returns a viewport rect and a visibility: **shown**; **behind**, when a window ranked further front overlaps the rect; **collapsed**; or **gone**. It is a pure function of the anchor and the geometry snapshot, like the arrangements, and is unit-tested the same way.
- inscribe calls it on the render goroutine every frame, so a mark follows its window as the window moves.
- The window host owns it because it owns the geometry and the window identities. Behind is by rects and stacking, not pixels.

### SD4 — Widgets through window trees; captures stay clean

- A model points at a widget by a part of a window tree it read: `{tree: "t3", node: "#12.3"}`. The chat holds the trees it read and turns such a target into `{window, rect}`: the part's rect less the origin of its window's rect when the tree was taken (ADR-0301, `Tree.WindowOf`). The arithmetic is code. From then on the mark follows the window.
- A widget that moves inside its window — scrolled, re-laid out — is not followed; the mark keeps the place it had when the tree was taken.
- inscribe draws after every window, outside every window's span, so the capture replay (ADR-0281 SD4) never contains an annotation. An agent's own marks never reach what it observes. Development captures of the whole viewport do show them.

### SD5 — Operations

| Op | Draws |
| --- | --- |
| `highlight(targets, label?)` | an outline around each target, with an optional short label |
| `callout(target, text)` | a note beside the target, joined to it by a leader line |
| `arrow(from, to, label?)` | an arrow from one target to another |
| `step(target, text)` | a numbered marker with a note; numbers run per task in arrival order |
| `spotlight(targets)` | dims the viewport outside the targets |
| `clear(id?)` | removes one of the task's annotations, or all of them |

- Each annotation has a caller-chosen `id`. Annotating an existing `(task, id)` replaces it.
- No op takes a colour, width, font or position. Style and placement are inscribe's (SD6).
- Text is untrusted, plain (no markdown), single-line, and capped in length.

### SD6 — One visual language, in neon

- **The palette is neon, deliberately outside the design system** ([ADR-0031](./0031-imzero2-design-system-color.md), [ADR-0040](./0040-imzero2-design-system-palette-consolidated.md)). A mark must not read as part of any app. Colours the design system gives apps would let a mark pass for app UI, and a mark that looks like UI is the spoofing case. The design lint's L2 rule ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD8) flags raw colours outside the token module; the neon colours are named in one file of `runtime/inscribe`, each line carrying the rule's per-line exception with this decision as its reason, and are used nowhere else.
- Each neon stroke is drawn over a dark halo, and each note sits on a dark plate, so the same colours read on the light and the dark theme.
- **Behind** draws the outline dashed. **Collapsed** places the mark on the window's title bar. **Gone** retires the annotation.
- inscribe places notes and labels: one pass over every annotation tries the sides of each target in turn and keeps the first place clear of the notes already placed and inside the viewport. A note's size comes from egui's measure of its text a frame later; in the first frame it is estimated from the text's length, so a new note can shift by a few points once.
- Every note and label carries its task's attribution tag, so the person can tell who drew it.

### SD7 — Merging annotations from several agents

- One scene holds every annotation, keyed by `(task, id)`. Paint order is arrival order.
- A task is given a neon hue when it first annotates. The hue is kept while the task has annotations. With more tasks than hues, hues repeat and the attribution tag tells tasks apart. Everything else about the style is the same for every task.
- The layout pass of SD6 runs over all tasks' annotations together.
- A task can replace and clear only its own annotations.
- Each task has a budget of annotations, and the scene has a ceiling. An annotation over either is refused. It never evicts another task's marks.
- `spotlight` from several tasks combines: everything outside the union of their targets is dimmed once.

### SD8 — Authority and lifetime

- `runtime.agent.annotate` and `runtime.agent.clear` are `runtime.agent.*` verbs, so they inherit the grant check, action records and taint handling ([ADR-0276](./0276-agents-read-and-arrange-windows.md) SD3/SD4).
- Annotating requires suggest or act on every anchored window's entry. A viewport anchor requires desktop mode suggest or act.
- Each accepted call is an action record (ADR-0269 SD9) with effect `view`. An annotation changes what the person sees, not any app's state, so ADR-0269 SD11 is unaffected.
- **Annotations stay until the person clears them**: one task's from that task's badge, or all at once from the **Window** menu. Agents can clear their own. A task's annotations are retired when the task ends or its grant is revoked, and an annotation is retired when its window leaves the grant or closes, since the authority it was drawn under is gone.
- While a host modal is open, inscribe draws nothing. The chrome that draws a host modal tells the window host so in that frame, and inscribe, framed after it, reads the flag. No mark can stand beside a decision the person is making.

### SD9 — Deferred

- Anchors on pixels of a PNG capture. A model can point at a tree part instead.
- Following a widget inside its window (SD4), and anchors on ADR-0269 resources.
- Animation, freehand drawing, and annotations the person makes.
- A capture that includes the annotations, on request.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API (`inscribe`) | the scene, anchors, ops and the overlay | — |
| Exported Go API (`windowhost`) | `Resolve`, the visibility enum; the overlay framed after the shell chrome; the host-modal flag; "Clear annotations" in the **Window** menu | Agent chrome sets the host-modal flag |
| `runtime.agent.annotate`, `.clear` | Added | `agent.Service` dispatch, wire types, `HostI`, `agent.Client`, the dispatch test host and the chat's test host |
| Agent chrome | a per-task clear on the task badge | — |
| Chat coordinator | `annotate` and `clear_annotations` tools; tree parts as targets | Coordinator prompt |

## Alternatives

- **An app with a surface of its own (`SurfaceOverlay`).** It would give inscribe an app identity — a manifest, an app-center page — at the cost of excluding it from every list of windows the window host keeps (arrangements, `keelson('windows')`, capture spans, menus, operations, frame times) and every surface check. The owner chose the host component (2026-10-09).
- **Agents send viewport coordinates, computed from `keelson('windows')`.** The rect is a frame late when read and wrong after the next move. It also puts arithmetic on the model.
- **inscribe resolves anchors itself.** It would hold a second copy of window identity and geometry.
- **Resolution in keelson SQL.** Right for an agent reasoning about the desktop, wrong for a path that must run every frame on the render goroutine.
- **The host resolves tree parts.** The capture service would keep each tree after it is read, and the agent wire would carry tree references. The chat already holds the trees it read, and what reaches the host is then a window and a rect the grant check understands.
- **Pixel anchors on PNG captures.** The artifact would need to carry its crop and scale, and the model would read coordinates off an image. Deferred (SD9).
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
- Behind is by window rects and stacking, not pixels. A translucent window counts as covering.
- A widget anchor keeps the place its widget had when the tree was taken; a scroll inside the window leaves the mark behind.
- The neon palette is a second colour vocabulary to keep legible as themes change, outside the design system's review.
- inscribe has no app identity: it is not in the app center, and its marks are recorded under the agent's calls, not under an app.

### Neutral

- Hiding the overlay under host modals means marks blink out while the person decides.
- A new note can move by a few points in its second frame, when its measured size replaces the estimate.

## Verification plan

- Unit tests for `Resolve`: each anchor kind, each visibility, a window moved between frames.
- Unit tests for the scene and layout: replace by id, the ownership refusal, budgets, hue assignment, retiring by task and by window, and a layout pass over two tasks' overlapping notes.
- Chat tests: a tree part resolves to its window and a rect relative to it.
- Agent dispatch tests: refusals under observe and without desktop mode; a record per accepted call.
- A scene document beside the chat's other scenes: a scripted coordinator reads a window's tree and highlights a button by its tree part; captures show the mark, the mark following the window after an arrangement moves it, and the mark dashed once another window is raised over it.

## Status

Proposed 2026-10-09.

Milestones:

- **M1 — `Resolve` in the window host.** (SD2, SD3)
- **M2 — inscribe's scene, layout and neon style, framed by the window host, and the host-modal flag.** (SD1, SD5–SD7)
- **M3 — The agent verbs, authority, records and retirement; the Window menu and badge clears.** (SD8)
- **M4 — The chat's tools, with tree parts as targets.** (SD4)
- **M5 — The scene document.**

## References

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — task grants, modes, records.
- [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) — the geometry report.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — `keelson('windows')`, desktop mode, the `runtime.agent.*` window verbs.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) — captures, spans and obligations.
- [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) — the design lint, its L2 colour rule and per-line exceptions.
- [ADR-0301](./0301-window-trees-a-capture-format-naming-each-widget-under-the-message-that-drew-it.md) — window trees, the windows they name, and the references an anchor cites.
- [bigarrow](https://github.com/franzenzenhofer/big-arrow-on-the-screen) — the tool the idea comes from: an agent-called arrow and sign over the macOS desktop.
