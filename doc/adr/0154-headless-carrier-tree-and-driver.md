---
type: adr
status: accepted
date: 2026-07-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0154: Headless carrier — accessibility-tree export, coordinate-free actuation, capture on demand

## Context

The headless host of [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md)
renders offscreen, links no window system, and carries video plus input over one
WebSocket. Its browser viewer is not its only possible client: the wire is a
declared protobuf contract (`proto/boxer/imzero2/v1/input.proto`) and the tree
already ships a non-browser client, `imzero2_ws_probe`.

Two knobs make that host a capture surface as well as a remote-access one:
`IMZERO2_HEADLESS_DUMP_DIR` writes per-frame PNGs straight out of the render
loop, and `IMZERO2_HEADLESS_LISTEN` may be empty while
`IMZERO2_HEADLESS_MAX_FRAMES` bounds the run — so a capture needs no viewer, no
encoder and no compositor, and terminates itself.

Measured while costing options for a `play` screenshot gallery (see
[the capture-options background](../adr-background-work/play-screenshot-capture-options.md)):
a scene captures in about four seconds with no compositor running, and injected
input round-trips — a click through the carrier toggled a checkbox in the app.

What the seam lacks is a way to say *which* widget. The `inspection` feature is
`["desktop", "eframe?/inspection"]`; the headless build has no eframe, so there
is no `egui_inspection` plugin, no AccessKit tree, and no locators. A step is a
click at (x, y). Coordinates are deterministic within a pinned scene, but any
layout change moves them **silently**: a stale coordinate captures a plausible
wrong frame rather than failing. That is the failure mode
[ADR-0127](./0127-imzero2-interaction-record-replay.md) rejected O1/O4 for when
it chose semantic steps over raw input.

Two substrate facts, verified against egui 0.35.0, make closing the gap cheaper
than the missing feature suggests:

- **AccessKit is already linked.** egui 0.35 has no `accesskit` cargo feature —
  it is a mandatory dependency, and generation is a runtime toggle
  (`Context::enable_accesskit`). The resulting tree arrives in
  `FullOutput.platform_output.accesskit_update`, the same struct the headless
  loop already reads to push the cursor shape. eframe's `accesskit` feature
  concerns the platform adapter, not the tree.
- **Node ids are stable anchors.** `Id::accesskit_id()` is the raw egui id, and
  egui2's Go side derives ids as an XOR-fold of label/sequence hashes down the
  scope stack — a pure function of widget path, unchanged across runs and
  resolutions, and unaffected by unrelated sibling insertions (the same finding
  ADR-0127 rests its anchors on).

## Design space (QOC)

**Question.** How does a non-browser client identify and actuate a widget on the
headless seam, without a compositor and without the inspection plugin?

**Options.**

- **O1** — keep coordinates: the client computes positions out of band.
- **O2** — export a reduced AccessKit tree over the carrier on request, and add
  an AccessKit action verb so actuation can skip coordinates entirely.
- **O3** — push the tree on change, the way the cursor shape is pushed.
- **O4** — compile `egui_inspection` into the headless build and reuse the
  desktop seam.
- **O5** — do not improve the headless route; drive the desktop build under a
  compositor for anything needing a locator.

**Criteria.** C1 robustness to app change (and whether breakage is loud);
C2 works with no compositor, hence in CI; C3 wire and per-pass cost; C4 fidelity
to the path an operator actually connects to; C5 implementation weight and
security surface.

**Resolution.** O2. Kill reasons for the rest in
[§Alternatives](#alternatives).

## Decision

Extend the ADR-0024 carrier with a request/response accessibility-tree channel,
an AccessKit action input verb, and a capture-on-demand control; and add a Go
client that speaks the wire, so traces can be replayed against a headless host
with no compositor, no browser and no inspection port.

### SD1 — Tree channel: request/response, active-only, off until asked

Four additions as `SessionControl` oneof variants — the additive mechanism the
proto file's own versioning policy names, so no new framing prefix and no
version bump:

```
TreeRequest    {}                                            client → host
TreeSnapshot   { repeated TreeNode nodes; uint64 focus;      host → client
                 uint64 pass }
CaptureRequest { string name }                               client → host
CaptureDone    { string path; uint32 width, height;          host → client
                 uint64 frame_index }
```

Request/response rather than push. The cursor shape is four bytes and changes
rarely; a tree is hundreds of nodes and any hover mutates it, so pushing on
change would put a per-pass build-and-diff on the render loop for a consumer
that reads it a few times per scene. The client asks when it needs to resolve an
anchor or wait on a node.

`enable_accesskit` is turned on by the first `TreeRequest` and off on disconnect
or after an idle window, so a run nobody subscribes to pays nothing. All four
messages are honoured **only from the active connection**, like input
([ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) SD2).

### SD2 — Node schema: reduced, not raw

`TreeNode` carries id, role, name, value, bounds, a flags bitmask
(disabled / hidden / focused / selected), parent and children — what anchor
resolution needs and no more.

Serialising `accesskit::TreeUpdate` directly is available (the tree's serde
support is already enabled transitively) and would need no mapping code, but it
would bind the wire to AccessKit's own shape across upstream releases and leave
the Go side hand-parsing an unschema'd document. The reduced node keeps the
contract ours and generated on both ends; the mapping is a pure function with
pinned tests, mirroring how cursor codes are pinned today.

### SD3 — Coordinate-free actuation

One new `InputEvent` variant, `AccessKitAction { node_id, action }`, translating
to `egui::Event::AccessKitActionRequest`. egui honours injected `Click`,
`Focus`, `SetValue` and `ScrollIntoView` (verified for 0.35 in ADR-0127's
Context), so a driver actuates by node wherever one exists and falls back to
synthetic pointer events only for painter-only widgets. This is what removes
coordinates from the common path rather than merely making them resolvable.

### SD4 — Capture on demand

`CaptureRequest { name }` writes a PNG through the existing frame-sink writer
and acknowledges with `CaptureDone`. This replaces picking a frame out of a
periodic dump, and it lets the driver choose the moment.

The name is **sanitised to a basename** and written only under the configured
dump directory; with no directory configured the request is ignored. The
carrier already grants the active connection full input control, but that is no
reason to hand it arbitrary filesystem writes.

### SD5 — A Go client for the carrier

A library under `public/thestack/imzero2/` plus a `drive` subcommand beside the
existing `imzero2` subcommands. Two dependency decisions, taken opposite ways on
purpose:

- **Protobuf: generated.** `input.proto` is a shipped, versioned contract
  already generated on the Rust side; a hand-written Go codec could drift from
  it silently. Generation uses the pure-Go path (`protocompile` compiling the
  schema, `protoc-gen-go` invoked over the plugin protocol) — no system
  `protoc`, matching the Rust side's protoc-free stance. Hand-rolling a
  protobuf codec buys throughput, which a driver sending a few small events per
  scene does not need.
- **WebSocket: hand-rolled.** There is no Go WebSocket code in the tree and this
  client only ever talks to loopback: handshake, masked binary frames,
  ping/pong, close. RFC 6455 client framing is frozen, so unlike the proto there
  is no contract to drift from, and this avoids a new runtime dependency under
  the license and vulnerability gates.

### SD6 — One trace vocabulary, two executors

Traces reuse ADR-0127 §SD2's step vocabulary (`click`, `type`, `set_value`,
`drag`, `scroll`, `wait`, `note`, plus `capture`) and §SD4's anchor ladder
(exact node id → unique role+name → ancestry-scoped name and ordinal →
bounds-fraction offset with a loud warning). ADR-0127 owns the vocabulary and,
if built, the recorder that authors traces; this ADR adds a second executor for
a seam that ADR's own executor cannot reach, since the headless build cannot
compile inspection. A trace authored either way should run on both.

### SD7 — Security

The tree carries labels and values, so it is data egress, and it is subject to
the same active-connection gate as input and to ADR-0024's bind-address refusal
for non-loopback binds without the auth and TLS that
[ADR-0082](./0082-imzero2-remote-session-auth-tls.md) specifies. It adds no
*control* surface — the carrier already grants the active connection full input.
Keeping the tree off until requested also keeps it absent from the wire for
every deployment that never asks.

### SD8 — Non-goals

- **A recorder.** Authoring traces by demonstration stays ADR-0127's M1/M2.
  This ADR assumes traces exist, whether hand-written or recorded.
- **An MCP server over this seam.** It would give agents a way to drive
  *deployed* headless instances, which the desktop-only inspection seam cannot.
  Deferred rather than gated on: nothing here depends on it.
- **Browser-viewer changes.** The viewer hand-encodes its protobuf and never
  sends these messages; the only requirement is that it skips control variants
  it does not know.

### Milestones

- **M1** — proto additions, the tree mapping and its pinned tests, subscription
  gating, capture on demand; verified by extending `ws_probe`.
- **M2** — the Go client: generation, WebSocket, session and tree round-trip.
- **M3** — the `drive` subcommand: trace format, anchor ladder, waits, and a
  verify mode that asserts recorded effects.
- **M4** — move the `play` screenshot tour onto the driver, dropping its
  compositor and its app-specific capture knobs.

## Alternatives

- **Coordinates only (O1).** Cheapest, and already works — but breakage is
  silent, which for a gallery means publishing a wrong picture rather than
  failing a run. Retained underneath O2 as the last rung of the anchor ladder,
  where painter-only widgets need it.
- **Push the tree like the cursor (O3).** The cursor precedent does not
  transfer: a four-byte value that changes rarely versus a document that changes
  whenever the pointer moves. Costs a per-pass build and diff whether or not
  anything reads it. A subscription mode remains an additive follow-up if a live
  consumer appears.
- **Compile `egui_inspection` headless (O4).** It requires eframe, which the
  headless build exists to exclude; adopting it would pull a windowing stack
  into an appliance host. It would also open a second unauthenticated port
  beside a carrier whose own bind gate exists to prevent exactly that.
- **Drive the desktop build instead (O5).** Keeps a compositor in the loop for
  every locator-dependent step, and leaves deployed headless instances
  undriveable. Reasonable as a stopgap, which is how it is being used today.
- **Raw `accesskit::TreeUpdate` on the wire.** No mapping code and nothing lost,
  but the contract becomes AccessKit's to change and the Go side parses without
  a schema.
- **Hand-rolled Go protobuf.** Wins throughput this consumer does not need, and
  risks silent divergence from a contract the Rust side generates from.

## Consequences

### Positive

- One substrate reaches every state: no compositor, no browser, no inspection
  port, locators and coordinate-free actuation, captures on command.
- The gallery is rendered by the same host an operator connects to, so the
  capture path exercises the deployment path.
- Anchors are pure functions of widget path, courtesy of the deterministic id
  stack — the same property ADR-0127 relies on.
- The app under test needs no capture knobs of its own; the driver decides when
  to capture.

### Negative

- A second consumer of `platform_output` on the render loop, and a mapping that
  must track AccessKit's node model as egui moves.
- Traces rot on intentional UI change. Loudly rather than silently, which is the
  point, but it is still a standing maintenance cost.
- Two executors for one trace vocabulary means two places a step's semantics can
  drift.
- The Go client owns a hand-rolled WebSocket implementation. Small and
  loopback-scoped, but ours to maintain.

### Neutral

- The tree is off unless requested, so deployments that never ask are unchanged
  on the wire and on the render loop.
- Coordinates remain in the protocol and in the ladder; this ADR demotes them
  rather than removing them.

## Status

Accepted 2026-07-31, with M1–M4 built and verified the same day.

- **M1** — the tree channel, the AccessKit action verb and capture-on-demand
  ship in the headless host; `ws_probe` gained matching verbs.
- **M2/M3** — `public/thestack/imzero2/carrierclient` speaks the wire, and
  `imzero2 drive` replays traces against it.
- **M4** — `scripts/dev/play-screenshot-tour.sh` captures 29 scenes of `play`,
  five of them states no launch knob can reach, with no compositor running.

Verified across the two seams: the same widget resolves to the same node id
and the same bounds centre through `egui-mcp` on the desktop host and through
this channel on the headless one, so a locator written against either runs on
both.

**M5 stays deferred** (§SD8): an MCP server over this seam would let an agent
drive a *deployed* headless instance, which the desktop-only inspection port
cannot. Nothing in M1–M4 depends on it.

Two limits found while building, neither of them blocking:

- `egui_dock`'s tab strip is custom-painted and emits no AccessKit nodes, so a
  dock tab resolves by position only — the ladder's last rung. The
  `BOXER_PLAY_FOCUS_*` knobs remain the better way to select a body tab.
- A `wait` has to poll rather than resolve once. The state worth waiting for is
  usually a node that is present but not yet enabled, which a single resolution
  fails on.

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless host
  and the carrier this extends.
- [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) — the active/passive
  roster whose gate the new messages inherit.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — auth and TLS for
  non-loopback binds.
- [ADR-0127](./0127-imzero2-interaction-record-replay.md) — the step vocabulary
  and anchor ladder reused here, and the recorder that would author traces.
- [Capture options background](../adr-background-work/play-screenshot-capture-options.md)
  — the measurements that motivated this.
- [How to drive imzero2 from an AI agent with egui_mcp](../howto/egui-mcp.md) —
  the desktop-only seam, for contrast.
- `proto/boxer/imzero2/v1/input.proto` — the contract being extended.
- egui 0.35.0 sources: `context.rs` (`enable_accesskit`, AccessKit action
  handling), `id.rs` (`accesskit_id`).

### Related ADRs

- [ADR-0009](./0009-environment-variable-registry.md) — where any new Go-owned
  env knob would be registered.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the existing capture
  machinery and its headless-CI arrangement.
