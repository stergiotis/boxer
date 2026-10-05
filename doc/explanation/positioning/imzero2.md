---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# imzero2 — positioning

Positions the UI layer: Go apps drawn by a Rust immediate-mode client over
a framed foreign-function interface, the hosts that client runs under, the
design system with its policy tiers, and the headless driver agents use.
The framed interface itself is a clause here, not a page, since its one
interface-definition consumer is this layer. The widget catalog drawn with
it is positioned by [widgets](./widgets.md); the runtime that mounts the
apps by [keelson](./keelson.md).

## Short form

For an app author who writes a data app in Go and wants it on a desktop, in
a browser and on a headless appliance from one source, imzero2 is an
immediate-mode[^immediate] UI stack over a framed FFI[^fffi]: Go owns every
piece of state, the Rust side is a stateless renderer of a per-frame command
stream, and the same renderer serves a desktop window, a video stream to a
browser, a headless accessibility tree for a driver, and a CPU-rasterized
appliance with no GPU.

Unlike a web frontend beside a backend, or a remote-desktop stack over a
desktop toolkit, imzero2 leaves the app unable to tell which host it is on.
The cost is two binary targets, one frame of input latency, browsers that
must ship the video codec, and the web platform's assistive technology.

## Full form

**For** an app author who writes data apps in Go and needs the same source
on a desktop window, in a browser session, in a headless host an agent can
drive, and on an appliance image without a GPU,

**imzero2 is** an immediate-mode[^immediate] UI stack over a framed
FFI[^fffi]: an interface definition generates the Go bindings and the Rust
dispatch, Go owns state and widget ids, the Rust side renders a per-frame
command stream, and one renderer body serves every host,

**that** keeps the Go side unaware of which host is running, lets an agent
click and type by widget name and assert on the result, and boots the whole
stack from a small appliance image,

**unlike** a web frontend beside a backend — a second language, a second
state model, a DOM — or a remote-desktop stack whose codecs blur charts or
whose latency floor is seconds,

**imzero2** makes the host a placement decision the app never sees. The cost
is two binary targets, one frame of input latency, browsers that must ship
the codec, remote sessions that are not yet authenticated, and the web
platform's assistive technology.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | one source, every host | [ADR-0026 §SD8](../../adr/0026-app-runtime-and-capability-subjects.md) ("placement is the host's call"); [ARCHITECTURE §1–2](../../ARCHITECTURE.md) |
| is a | IDL generates bindings and dispatch; Go owns state, Rust interprets | [fffi2 skill](../../skills/fffi2/SKILL.md) (draft); [ADR-0049](../../adr/0049-fffi2-deferred-block-scope-buffer-memoization.md); [ADR-0088 §SD1](../../adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) ("capabilities are modeled in Go") |
| is a | headless render to video for a browser viewer | [ADR-0024](../../adr/0024-imzero2-remote-access-browser-viewer.md), [ADR-0086](../../adr/0086-imzero2-active-passive-viewers-and-roster.md), [ADR-0088](../../adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) |
| is a | a mesh lane to a browser painter — *proposed, code shipped* | [ADR-0128](../../adr/0128-imzero2-mesh-draw-stream-codec-lane.md) |
| is a | headless carrier and driver | [ADR-0154](../../adr/0154-headless-carrier-tree-and-driver.md), [ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) |
| is a | CPU rasterizer; appliance images | [ADR-0205](../../adr/0205-imzero2-cpu-rasterized-pixel-host.md), [ADR-0206](../../adr/0206-gokrazy-appliance-image.md) |
| is a | design system as tokens, three policy tiers | [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) (M0–M2 and the lint built; pattern docs and model-assisted review deferred) |
| that | the Go side cannot tell which host is running | [ADR-0024](../../adr/0024-imzero2-remote-access-browser-viewer.md) ("FFFI2 protocol unchanged"); [ARCHITECTURE §1.1](../../ARCHITECTURE.md) |
| that | drift guard on the stateful-widget contract | [ADR-0013](../../adr/0013-imzero2-stateful-widget-contract.md) |
| that | per-call-site cost attribution | [ADR-0049](../../adr/0049-fffi2-deferred-block-scope-buffer-memoization.md) |
| that | drive by widget name; assert in the trace | [ADR-0154](../../adr/0154-headless-carrier-tree-and-driver.md), [ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) |
| that | a small appliance image | [ADR-0206](../../adr/0206-gokrazy-appliance-image.md) (booted under emulation; not booted in CI) |
| unlike | a web frontend beside a backend | [why-boxer](../why-boxer.md) standing commitment ("egui over a framed FFI, not a web stack"); [ADR-0059](../../adr/0059-imzero2-declarative-layouting-over-visual-builder.md) Alternatives (reactive frameworks collide with Go's control flow); the foil is accepted on the commitment |
| unlike | a remote-desktop stack | [ADR-0024](../../adr/0024-imzero2-remote-access-browser-viewer.md) Alternatives (VNC-class codecs, streaming-manifest latency, WebRTC signalling and footprint); [ADR-0081](../../adr/0081-imzero2-headless-rdp-egfx-head.md) (withdrawn: RDP needs session machinery) |
| imzero2 | the host is a placement decision the app never sees | [ADR-0026 §SD8](../../adr/0026-app-runtime-and-capability-subjects.md); [ARCHITECTURE §1](../../ARCHITECTURE.md) |
| trade | two targets, one-frame latency, codec-capable browsers, unbuilt auth, no web AT ecosystem | [ADR-0024](../../adr/0024-imzero2-remote-access-browser-viewer.md) Consequences; [ADR-0082](../../adr/0082-imzero2-remote-session-auth-tls.md) (accepted, auth and TLS unbuilt); [fffi2 skill](../../skills/fffi2/SKILL.md); [iso25010-assessment](../iso25010-assessment.md) |

## Boundary

- **Remote sessions are unauthenticated.** [ADR-0082](../../adr/0082-imzero2-remote-session-auth-tls.md)
  is accepted and its auth and TLS halves are unbuilt; the browser path is
  a loopback or emulation arrangement. The statement names this as a trade,
  not a feature.
- Proposed or unbuilt, absent from the clauses: in-browser WASM execution
  ([ADR-0077](../../adr/0077-keelson-browser-wasm-execution.md)),
  interaction record and replay ([ADR-0127](../../adr/0127-imzero2-interaction-record-replay.md)),
  declarative layout ([ADR-0059](../../adr/0059-imzero2-declarative-layouting-over-visual-builder.md)),
  the mesh path's bandwidth guard, the compositor compartment
  ([ADR-0087](../../adr/0087-imzero2-client-compositor-compartmentalization.md),
  a stance rather than a build).
- The deterministic id stack is a mechanism much depends on and has no ADR; it
  is specified in the [imzero2 skill](../../skills/imzero2/SKILL.md) and
  not claimed here until it has a record.
- The framed FFI is folded in: its only interface-definition consumer is
  this layer, and every import outside it is host wiring.

## Further reading

- [why-boxer](../why-boxer.md) — the standing commitment to an immediate-mode UI over a framed FFI; P5 and P7.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [imzero2 skill](../../skills/imzero2/SKILL.md), [fffi2 skill](../../skills/fffi2/SKILL.md), [ARCHITECTURE §1–2](../../ARCHITECTURE.md).
- Decisions: [ADR-0013](../../adr/0013-imzero2-stateful-widget-contract.md),
  [ADR-0024](../../adr/0024-imzero2-remote-access-browser-viewer.md),
  [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md),
  [ADR-0049](../../adr/0049-fffi2-deferred-block-scope-buffer-memoization.md),
  [ADR-0062](../../adr/0062-imzero2-render-cadence.md),
  [ADR-0154](../../adr/0154-headless-carrier-tree-and-driver.md),
  [ADR-0205](../../adr/0205-imzero2-cpu-rasterized-pixel-host.md),
  [ADR-0206](../../adr/0206-gokrazy-appliance-image.md),
  [ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/thestack/imzero2

[^immediate]: Immediate mode: the app redraws its whole interface every frame from its own state, instead of keeping a tree of widgets the toolkit owns.
[^fffi]: A framed foreign-function interface batches a frame's worth of UI calls into one message from Go to Rust, instead of one cross-language call per widget.
