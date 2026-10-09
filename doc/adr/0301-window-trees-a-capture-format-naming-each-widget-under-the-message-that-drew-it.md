---
type: adr
status: proposed
date: 2026-10-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0301: Window trees — a capture format that names each widget under the stream message that drew it

## Context

A coordinator sees a window through three things: the operations its app declares ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)), the window's place on the desktop (`keelson('windows')`, [ADR-0276](./0276-agents-read-and-arrange-windows.md)), and captures of its pixels ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md)). None of them says which widget is where, or what it is called. ADR-0269 §SD12 deferred "an accessibility tree for coordinators on the desktop host"; development agents have one, through the headless driver ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)). [ADR-0297](./0297-inscribe-an-overlay-agents-annotate-the-desktop-through.md) needs one to anchor an annotation on a widget.

Facts that bound the design (egui 0.35, code reading and a spike, 2026-10-09):

- **The FFFI2 stream has no layout.** Go emits widget ops; Rust lays them out. A text field's op carries its whole text and a `password` flag, and Rust does the masking.
- **egui keeps every widget's rect for the pass.** `ViewportState::this_pass.widgets` holds each allocated rect, interactive or not, with its egui id, layer, rect and interact rect (the rect clipped to its ui). It is public.
- **AccessKit costs no new dependency.** egui depends on the `accesskit` crate unconditionally, and builds a tree only while `Context::enable_accesskit` is on. The large dependency tree is eframe's `accesskit` feature: the per-OS screen-reader adapters, which a tree built inside the process does not need. egui gives a label's text as the node's value, not its name, and masks a password input's value.
- **The capture replay passes every message through one loop.** ADR-0281 §SD5 replays the granted windows' spans into a fresh capture context. Every message, the deferred blocks a message replays included, is consumed by the interpreter's hand-written message loop (`interpret_outer`), outside the generated dispatch.

## Design space (QOC)

**Question.** How does a coordinator learn which widgets a window shows, where, and what they are called?

**Options.**

- **O1** — hand the agent the granted windows' recorded FFFI2 bytes.
- **O2** — decode the stream in Go, with each op's role and name argument marked in the IDL, and rects reported per op by the generated widget code.
- **O3** — the AccessKit tree of the capture replay alone.
- **O4** — bracket each message in the capture replay, attribute egui's widget rects to the innermost open message by egui id, and join roles and names from the AccessKit tree of the same pass.

**Criteria.**

- **C1** — shows no more than the pixels: masked fields stay masked, clipped content is absent.
- **C2** — structure as Go emitted it: message kinds and nesting a Go reader recognises.
- **C3** — no upkeep per op and no change to generated code.
- **C4** — through ADR-0281's enforcement point.
- **C5** — covers widgets laid out inside Rust (tables, dock tabs, popups).

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | +  | +  |
| C2 | +  | ++ | −  | +  |
| C3 | +  | −− | ++ | +  |
| C4 | −  | +  | +  | +  |
| C5 | −− | −  | ++ | +  |

O4 is chosen. O1 leaks a password field's text and has no rects. O2 needs a role and name rule for every op, kept in step with the IDL by hand, and its rects would come from hooks at the generated response and block sites. O3 gives egui's view: roles and names but not the Go structure, so a reader cannot tell which part of its app drew a node. O4 takes O3's names and adds the structure for one hook in hand-written code. Where a widget is laid out inside Rust, O4 attributes it to the message that drew the whole (C5's `+`, not `++`).

## Decision

We will add a third capture format, `tree`, to the capture facility of ADR-0281. The client replays the granted windows' spans as for a pixel capture. A recorder in the interpreter's message loop notes which widgets each message drew, and the client writes them as a document. Each kept message has its kind, its parent and the union of its widgets' rects. Each widget has its egui id, its visible rect, and its AccessKit role, name and value.

```text
 runtime.agent.capture {format: "tree", instances}
   │  PEP: grant rule, scope obligation, fail closed      (SD1)
   ↓
 client: captureReplay(format 2) into a capture context
   │  AccessKit on, in that context only                   (SD3)
   │  every message: begin → interpret → end               (SD2)
   │    end: unclaimed widgets of this pass → this message
   ↓
 document v1: messages, widgets, roles, names              (SD4, SD5)
   ↓
 PEP: parse, tree handlers, seal  →  artifact (window-tree+json)
```

### SD1 — A format of the capture facility

- `capture.FormatTree` (`"tree"`) joins `svg` and `png`. A tree is requested, decided and recorded like a pixel capture: the same `runtime.agent.capture` wire, the same grant rule (observe suffices), the same `scope` obligation, the same label over the windows drawn, the same sealed artifact read through `runtime.agent.read`, marked untrusted.
- The registry gains tree handlers, keyed by obligation as for the other formats; an obligation with no tree handler denies the capture (ADR-0281 §SD3). The scope handler refuses a crop.
- The source gains `RenderTree(windows, recheck)`. The window host serves it with the pixel path's recording and spans, and the client's `captureReplay` with format 2.
- The artifact's media type is `application/vnd.boxer.window-tree+json`.

### SD2 — The recorder: one hook, ownership by egui id

- While a tree capture replays, the interpreter's message loop calls the recorder before and after each message. On the live frame no recorder is installed and the loop is unchanged.
- At a message's end, every widget in egui's per-pass list that no message has claimed becomes that message's. Children end before their parent and claim first, so a widget belongs to the innermost message open when egui registered it. A container's own widgets, registered after its body ran, land on the container.
- Widgets registered before the first message (the replay's root ui) belong to no message. A block's end marker is a message of its own; its row is dropped and its widgets go to the enclosing message.
- A message's rect is the union of the visible rects in its subtree.
- Nothing depends on the order of egui's per-layer lists, only on ids.

### SD3 — Names from AccessKit, in the capture context only

- The capture context, a fresh context per capture, has AccessKit on for a tree capture. The live context's AccessKit state is untouched.
- Each widget is joined to the AccessKit node of the same egui id for its role, name and value. A label's name is the node's value, as egui sets it; a widget without a text value reports its numeric value.
- Roles use the lower-snake vocabulary of the headless driver's tree (`button`, `check_box`). Both come from one function, `optree::role_name`.

### SD4 — No more than the pixels

- Only the granted windows' spans are replayed (ADR-0281 §SD4).
- A widget's rect is its interact rect, the part inside its ui's clip, through its layer's transform. A widget clipped to nothing is not in the tree, and neither is its name.
- Messages whose subtree holds no visible widget are left out, and a kept message's parent is its nearest kept ancestor.
- Values are what AccessKit carries. egui masks a password input's value. Other values are what the field shows; the capture's label (ADR-0281 §SD6) governs who may read them.

### SD5 — The document

- `capture.Tree` is the schema, version 1: `ops`, each with `op`, `parent` (an index into `ops`, −1 at the top), `rect` (`[x, y, w, h]` in viewport logical points) and `widgets`, each with `id`, `rect`, and `role`, `name`, `value` when there is a node.
- `op` is the message's opcode by its Go binding name (`Button`, `Window`, `LabelAtoms`). It follows the IDL and changes when the IDL renames an op.
- Byte offsets into the stream and opcode ids are not in the document: the first are meaningless outside one frame's replay, the second shift on every IDL change.
- `capture.ParseTree` refuses an unknown version and a parent that does not precede its child; the enforcement point parses every tree before sealing it.

### SD6 — The coordinator's tool

- The chat coordinator gains `read_window_tree(windows)`. It captures a tree, reads it, and hands the model an indented outline, one line per message that names something:

  ```text
  #0 Window [10,20 300x200]
    #2 Button [20,60 40x18] button "Save"
  ```

- The outline is wrapped as untrusted, taints the conversation like a capture, quotes the apps' text so it cannot close the untrusted fence, and is cut at a byte bound.

### SD7 — What this answers, and what it leaves

- ADR-0269 §SD12's coordinator accessibility tree is answered for captured windows: a snapshot through the capture facility, not a live tree.
- ADR-0297's widget anchors become possible as `{capture, widget id}`: a widget's rect in the tree, taken relative to its window, then followed as a window anchor.
- Canvas content drawn by paint commands has no widgets and stays out of the tree. Paint ops get no rect; a sense region's widget belongs to the canvas that drains it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 `captureReplay` | format 2, `tree` | Client: the recorder, AccessKit on the capture context |
| Exported Go API (`capture`) | `FormatTree`, `MediaTypeTree`, `Tree`, `TreeOp`, `TreeWidget`, `ParseTree`, `TreeHandlerI`, `Registry.RegisterTree`; `SourceI.RenderTree`; `SourceResult.Tree` | Every `SourceI`: the window host, the agent and chat test hosts |
| Exported Go API (`windowhost`) | `RenderTree` | — |
| `runtime.agent.capture` wire | `format` accepts `tree` | `agent.CaptureFormatTree` |
| Chat coordinator | `read_window_tree` tool and a prompt line | — |
| Browser host digest | the Rust host changes | `tabhost/browserhost.sum` |

## Alternatives

- **The raw stream (O1).** A password field's text in cleartext, no rects, and opcode ids that shift with the IDL.
- **A stream decoder in Go (O2).** A second description of every op, maintained by hand, plus rect hooks at every generated response and block site, which AccessKit and egui's widget list already provide.
- **AccessKit alone (O3).** Names without the Go structure. Kept as the source of names inside O4.
- **A live tree fetch on the desktop host.** What ADR-0269 §SD12 named. It would run AccessKit on the live context, and would need its own grant scoping; the capture path already scopes, labels and records.
- **Label markers in the IDL instead of AccessKit.** Upkeep per op, and masking reimplemented in Go.
- **A crop for trees.** Filtering messages by rect is easy; the parent remapping after it is not worth it until a caller needs it. Refused, as for SVG.
- **Per-layer list lengths to find a message's widgets.** The spike did this. egui reorders a layer's list when a widget moves to the top, so a widget could be missed or counted twice. Ownership by unclaimed id has no such dependency.

## Consequences

### Positive

- A coordinator can say which button, field or label a window shows, where, and under which part of the app, with the grant and record of any capture.
- The recorder is one hook in hand-written code. A new widget needs nothing to appear in the tree.
- The tree and the headless driver's tree share their role names.

### Negative

- Widgets laid out inside Rust are attributed to the message that drew the whole: egui_dock's tab strip, for one, lands on the dock area's message.
- egui ids are stable across frames only as egui's are; an id derived from a position in a list moves when the list does.
- `op` names follow the IDL, so a reader matching on them breaks when the IDL renames an op.
- A tree capture takes the pixel path's frames — record, replay, collect — and builds an AccessKit tree for the capture context. Its cost was not measured beyond one observation: a tool call reading one small window took 82 ms in the scene below.
- A name is the widget's whole text even when the widget is only partly on screen.

### Neutral

- The spike (2026-10-09, headless host, two scenes): in both, every AccessKit node with bounds in the capture context either matched a message's widget by egui id or was a `text_run` whose parent did — 268 of 300 nodes and 447 of 562, the remainder text runs. Of 1303 rows in the larger scene, 998 came from deferred blocks.

## Verification plan

- Rust unit tests on the recorder: ownership by the innermost message, rows without widgets dropped with parents remapped, an end marker's widgets going to the enclosing message, a clipped widget left out, and the role vocabulary.
- `capture` tests: a tree is rendered from the scope's windows, parsed and sealed under its media type; a crop is refused before rendering; a document with an unknown version, a bad parent or no JSON fails the capture.
- An agent test: a tree capture under observe is read back untrusted, as a window tree.
- Chat tests on the outline: indentation, what is left out, the untrusted fence, the byte bound.
- [chat-coordinator-window-tree.scene.md](../../apps/chat/scenes/chat-coordinator-window-tree.scene.md): a scripted coordinator opens the operations demo and reads its tree on a headless host; the scene waits for the demo's Clear button, by role and name, in the outline the model read. Run by `scripts/dev/scene.sh`, not on every change.

## Status

Proposed 2026-10-09.

- **M1 — The recorder and format 2 in the client.** (SD2, SD3, SD4) Built.
- **M2 — `FormatTree` in the capture facility, and `RenderTree` in the window host.** (SD1, SD5) Built.
- **M3 — The coordinator's `read_window_tree`.** (SD6) Built.
- **M4 — ADR-0297's widget anchors.** (SD7) With ADR-0297.

## References

- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the driver's tree and its role vocabulary.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — §SD7 untrusted content, §SD12 the deferred coordinator tree.
- [ADR-0276](./0276-agents-read-and-arrange-windows.md) — the window rects a tree's rects share a frame with.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) — the capture facility, spans and replay.
- [ADR-0297](./0297-inscribe-an-overlay-agents-annotate-the-desktop-through.md) — anchors on widgets.
