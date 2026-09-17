---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-17
---

# ADR-0242: imzero2 session and stream lifetimes

## Context

A bounded queue does not by itself make a remote viewer recoverable. Dropped
role updates can leave the browser permanently out of step with the server;
restarting an oversized texture bootstrap can prevent it ever completing.
Pixel deduplication also prevents encoder supervision and scheduled keyframes
from progressing on an unchanged screen. Content-addressed mesh bodies need a
retirement agreement, not lifetime-long retention on both ends.

This decision refines ADR-0024, ADR-0086 and ADR-0088 and the implemented mesh
lane described by ADR-0128 (proposed). It does not change origin validation,
authentication, TLS, listener exposure, proxy routing, or admission policy.

## Decision

### SD1 — Authoritative state is not disposable frame traffic

Each connection retains the latest roster independently of its bounded payload
queue. The writer delivers pending state without blocking the render thread;
an unresponsive writer still times out. Stream generations order hello changes
before their payloads and invalidate obsolete queued work.

### SD2 — Input belongs to an owner and a focus interval

Focus changes are ordered input events. Losing focus cancels held input rather
than completing a click. Changing the active connection invalidates queued input
and paste from the previous owner and resets keys, buttons and modifiers before
new input is consumed. Disconnect cancellation is server-side: it cannot depend
on a departing browser delivering an unload message. Moving focus between the
canvas and its paste trap does not leave the remote input surface.

Mouse-button numbers remain the existing wire numbers; the browser translates
DOM numbering. Copy/cut shortcuts become egui clipboard actions at the input
boundary, not widget-specific workarounds.

### SD3 — Encoder health and joining progress independently of pixel changes

Supervision checks both child exit and feeder failure before deduplication,
using the existing restart budget. A fresh encoder generation receives the
current frame even if unchanged. Only accepted submissions advance dedup state.

A viewer awaiting a keyframe temporarily drives the periodic encoder at the
configured frame-rate cap, including on an idle application. Actual keyframe
delivery, not submission counts or active-viewer telemetry, completes joining.
A bounded join watchdog reports a non-progressing encoder or disconnects a
stalled peer. The extra work stops when no viewer needs it.

The single encoder and conditional GOP remain: no new forced keyframe on each
join, and the lone active stream remains pulse-free. While passives are present,
a finite GOP is applied even when a raw encoder override omitted `-g`. Overrides
that prevent decodable keyframes fail explicitly rather than promising bounded
joining. The two-encoder alternative remains deferred under ADR-0086.

### SD4 — Mesh delivery commits an ordered frame batch

A mesh batch contains texture updates, a dependent frame, and retirement. Queue
admission is atomic; the writer retains progress between its constituent wire
messages. One in-flight batch and one queued batch bound per-peer backlog.
Rejected later work requests a fresh snapshot rather than appending history.
The writer services input and control between messages and keeps write timeouts.

Texture sets precede the frame and frees follow it. A bootstrap includes textures
used and freed in that same frame. The mirrored texture store finalizes frees
even in video mode or with no viewer, so switching lanes does not retain history.

### SD5 — Cache retention follows committed liveness

The sender remembers only bodies retained by its last accepted mesh frame. An
additive retirement message tells the browser to retain that frame's draw-order
bodies and the explicitly supplied live texture keys. A body that disappears and
later returns is resent. A texture that is unused but still live is retained.
Retirement-only changes are work even if the draw-order hash is unchanged.
Disconnect disposes browser GPU caches; reconnection bootstraps them again.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.imzero2.v1.InputEvent` | Add field 9, `focus`, carrying a boolean `focused` | Browser writer, Rust translation, generated Go carrier types |
| Mesh prefix `0x04` | Add subtype 3: `count u32`, `count × texture-key u32`, little-endian; retire against the preceding frame's order and this live-key set | Mesh serializer and browser painter |
| Internal carrier delivery | Owner/stream generations, retained control state and bounded mesh batches | Headless loop, encoder drain and connection writer |

Existing mesh frame/body/texture layouts and existing protobuf field numbers do
not change. Texture keys in retirement use the same folded wire keys as updates.

## Alternatives

- **Larger or unbounded queues.** A larger queue moves the bootstrap failure
  threshold; an unbounded queue replaces it with retained historical work.
- **Independent browser LRU.** The sender can omit a body the browser evicted.
  An acknowledged cache protocol could repair that, but exact frame liveness
  already supplies a smaller agreement.
- **Force an IDR on every join.** Changes an existing viewer's image merely
  because another connects. Advancing the scheduled GOP preserves the chosen
  single-encoder policy, at the cost of temporary idle encode work.
- **Replay all encoded data since an IDR.** Retains history proportional to an
  effectively infinite active GOP and adds a replay-to-live handoff.

## Consequences

Recovery requires explicit owner and stream state, and an idle join temporarily
costs encoding. Mesh bodies returning after an absent frame cross the wire again.
Retained memory follows live scene size and bounded in-flight work rather than
session duration. One WebSocket and the self-contained viewer remain; no
frontend framework or new authentication mechanism is introduced.

## Migration

The embedded viewer and host are deployed together. Old readers ignore additive
protobuf fields and unknown mesh subtypes. An old browser page can still render
but needs reloading to gain cancellation and bounded GPU retention. A new viewer
prunes only on explicit retirement, preserving rendering against an older host.
Regenerate Go carrier types from `input.proto`; Rust types regenerate at build.
Raw encoder overrides no longer opt out of a finite GOP while passives watch.

## Verification plan

Rust unit tests cover translation/cancellation, owner changes, saturated control
queues, encoder retry/dedup, idle join state, ordered bootstrap and cache liveness.
An opt-in browser regression runs the real viewer script and CSS, checking
computed button visibility, emitted input, focus transitions and GPU disposal.
Opt-in process tests exercise available encoders and real browser joins; missing
browsers or encoders are reported as gaps rather than passing coverage.

## Status

Accepted — 2026-09-17, following approval of the ordered repair plan. Origin and
authentication policy remain separate work.
