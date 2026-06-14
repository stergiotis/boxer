---
type: adr
status: proposed
date: 2026-06-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0086: ImZero2 active/passive remote viewers and the session roster

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) shipped single-session pixel streaming — headless egui → ffmpeg H.264 → one WebSocket → WebCodecs viewer, protobuf input back. [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) (accepted, **not yet built**) added a bind-gate + token auth + TLS, and changed the second-connection path from hard-reject to standby + an explicit "Take session" handoff, keeping exactly **one active session**. ADR-0082 deliberately **rejected O4** — fanning one render out to N concurrent live viewers — because it forces either N× encode or a shared-encoder join-pulse that violates ADR-0024 SD3, plus geometry reconciliation and input arbitration; but it **kept O4's kill-reason on the record "so a genuine multi-viewer requirement re-opens it deliberately."**

This ADR is that deliberate re-opening. The need: let several authenticated connections **watch one session concurrently, read-only**, while only one drives; and make the set of connections **first-class and visible** (who is connected, who is active, who may take over). It is resolved **not** by reviving O4's video fan-out but by a cheaper read-only **passive** tier riding the same pipeline.

Scope fixed in the design dialogue that precedes this ADR:

- **Modern browsers only.** No `<img>`/MJPEG/legacy fallback; the demo targets browsers with WebCodecs.
- **One decoder, one transport: WebCodecs + the existing WebSocket.** WebTransport is a later check, not now.
- **Demo-grade and light.** Prefer the smallest delta over the shipped pipeline; defer anything not needed to demonstrate the tier.

Inherited constraints this ADR must respect:

- **ADR-0024 SD3 — no mid-stream IDR** on the active low-latency stream (periodic IDR re-quantises the mostly-static screen and reads as a visible colour pulse, measured RMSE 316).
- **ADR-0024 SD6 — one WebSocket, 1-byte type prefixes** (`0x01` video / `0x02` input / `0x03` session control); new control is additive on `0x03`.
- **ADR-0062 reactive cadence + blake3 frame dedup** — an idle dashboard encodes ≈ nothing.
- **ADR-0082 single-active-session + auth-at-admit** — input/resize/cadence honoured only from the active connection; the bind-gate/token gate admission.
- Today's carrier (`wscarrier.rs`) holds an implicit single-connection state (a counter + a `connected` flag) and **hard-rejects** a second connection.

## Design space (QOC)

**Question.** How should ImZero2 let multiple authenticated connections watch one session concurrently (read-only), and surface the set of connections as a roster, without reviving O4's video-fan-out costs or violating ADR-0024 SD3 — on a modern-browser, WebCodecs+WebSocket-only, demo-light budget?

**Options.**

- **O1 — Active/passive tiers over one WebCodecs/WS stream, connections first-class with a roster (chosen).** One **active** connection (video + input); N **passive** connections (read-only video, no input). Roles are server-authoritative and broadcast as a roster. A **single shared periodic-IDR encoder** serves the demo (active and passive consume the same stream); the **two-encoder split** is the named upgrade.
- **O2 — Per-passive image snapshots** (MJPEG/WebP `<img>`). Self-contained and universal, but all-intra discards the temporal correlation that dominates dashboard frames, so it compresses poorly exactly where it matters; its one advantage (reach without WebCodecs) is void under the modern-browser-only scope.
- **O3 — Per-viewer video encoders.** A dedicated encoder per passive viewer — i.e. literal O4 fan-out. N× encode.
- **O4 — MSE / HLS passive tier.** The browser does demux/buffer/ABR; segments are CDN-cacheable. Offloads buffering and scales fan-out, but adds a second decode path, a container/segmenter, and a seconds-scale latency floor.
- **O5 — WebRTC passive tier.** Real-time + closed-loop congestion control + NAT traversal, but heavy dependency/signalling footprint and needs an SFU to broadcast — ADR-0024's already-deferred O3.

**Criteria.**

- **C1 — Concurrent read-only viewing.** Several connections watch at once.
- **C2 — No O4 costs.** No N× encode; no forced mid-stream IDR that pulses viewers already watching.
- **C3 — ADR-0024 SD3 respected** (or its violation bounded and reversible).
- **C4 — Implementation delta.** Reuse of the shipped headless-render + ffmpeg + WebCodecs + WS pipeline.
- **C5 — Compression on correlated frames.** Bandwidth on a mostly-static, occasionally-moving dashboard.
- **C6 — Stack control / lightness.** Dependency and code-path footprint at the demo.
- **C7 — Presence / roster.** The set of connections is trackable and surfaced.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (active/passive, 1 stream) | O2 (image) | O3 (per-viewer enc = O4) | O4 (MSE/HLS) | O5 (WebRTC) |
|----|-------------------------------|------------|--------------------------|--------------|-------------|
| C1 | ++                            | ++         | ++                       | ++           | ++          |
| C2 | ++ (one stream; scheduled IDR)| ++         | −− (N× encode)           | + (one shared stream) | − (SFU to broadcast) |
| C3 | + (active pulse, bounded; SD7 restores) | ++  | ++                  | + | ++ |
| C4 | ++ (delta on shipped path)    | − (new snapshot+`<img>` path) | − | − (segmenter + 2nd path) | −− (webrtc-rs + signalling) |
| C5 | + (inter-frame; passive sub-optimal) | −− (all-intra) | + | + | ++ |
| C6 | ++ (no new deps)              | +          | +                        | − (hls.js/segmenter) | −− |
| C7 | ++ (roster first-class)       | ~          | ~                        | ~ | ~ |

O1 is chosen on **lightness and stack-control** (C4/C6): it is the smallest delta over the shipped pipeline — one decoder, one transport, one encoder reconfigured — and it sidesteps O4's kill-reasons structurally (one shared stream → no N× encode; periodic *scheduled* IDRs → a joiner waits for the next one, so no *forced* IDR pulses viewers already watching). Its honest cost is C5: the single shared stream optimises the active (latency) target and lets passive ride sub-optimally (see SD5/SD6); the two-encoder split (SD7) is the gated remedy. O2 loses on C5 under the modern-browser scope that voids its only edge. O3 is O4. O4/O5 are real passive deliveries held as named upgrades, not demo-scope.

## Decision

Implement **O1**: a two-tier viewer model — one **active** session (video + input, per ADR-0082) and N **passive** sessions (read-only video) — with **connections first-class on the server** and a **roster** broadcast to all of them; one decoder (WebCodecs) and one transport (the existing WebSocket); a **single shared periodic-IDR encoder for the demo**, with the **two-encoder split named as the trigger-gated upgrade.** This refines, and does not supersede, ADR-0024 and ADR-0082.

### Subsidiary design decisions

- **SD1 — Connections are first-class on the server: a `Registry` projected as a `Roster`.** Replace the implicit single-connection state (counter + `connected` flag + hard-reject) with a `Registry` of `Connection { id, role ∈ {active, passive}, caps {webcodecs}, label?, geometry, since }`. Invariant: **≤ 1 active**. Every mutation broadcasts a `Roster`. Roles are **server-authoritative** — a client never self-assigns active.

- **SD2 — Role assignment and transitions.** Admit (post-auth) as **active iff there is no current active, else passive** (first device drives; later devices watch and may take over). `TakeSession` (honoured only from a WebCodecs-capable connection) makes the requester active and demotes the prior active to passive; it is **unilateral** (one principal, per ADR-0082). When the active disconnects: a **lone passive auto-promotes** (zero-friction return to your other device); with several passives the active slot stays empty and each keeps the button. Input, resize, and cadence are honoured **only** from the active connection (ADR-0082 SD5). `MAX_CONNECTIONS` bounds parked passives.

- **SD3 — Active/passive views are first-class in the browser: a server-driven `ViewMode` machine.** The viewer is an explicit two-state machine whose state **mirrors `roster.you.role`**: `Active` = WebCodecs decode → canvas + input capture + geometry/cadence ownership; `Passive` = WebCodecs decode, **no input**, a "Take session" button (if WebCodecs-capable) or a "view only" marker (if not). Role changes drive setup/teardown (promoted → start input; demoted → detach input). The client **must not send input until the roster confirms it is active** — that is the anti-race point of server-authoritative roles.

- **SD4 — One decoder, one transport.** Both tiers decode H.264 with WebCodecs; everything rides the shipped single WebSocket with 1-byte prefixes (ADR-0024 SD6). No `<img>`, MSE/HLS, or WebRTC at the demo. WebTransport is named as the later transport check; it would change the *feed*, not the decoder.

- **SD5 — A single shared periodic-IDR encoder for the demo; broadcast NAL units to all connections.** Change the encoder from effectively-infinite GOP to a **periodic IDR**, keeping `-bf 0` so the **active latency target is unchanged**; broadcast each NAL unit to every connection's per-connection latest-wins queue (the SD9 mailbox spirit, per connection). A joining viewer starts at the next **scheduled** IDR (no *forced* mid-stream IDR → no pulse for viewers already watching); force an IDR **only on the 0→1 transition** (nobody to pulse), so the first viewer still starts instantly. Takeover re-points *who is active* (input + geometry owner); it rebuilds the stream (a fresh IDR) **only if the new active's geometry differs**.

- **SD6 — The single encoder optimises the active target; passive rides it sub-optimally — by design for the demo.** This is stated plainly because it is easy to overstate: the single stream does **not** satisfy both optimisation targets. It satisfies **active latency** (`-bf 0`, every frame a reference) and lets passive ride the same stream, **giving up** B-frames / passive-tuned compression and **independent resolution/bitrate** (passive is pinned to the active's geometry). The cost is bounded for the demo: ADR-0062 reactive cadence + blake3 dedup make an idle dashboard ≈ free regardless of B-frames, so the penalty appears only under motion and on thin links — neither of which the demo stresses.

- **SD7 — The two-encoder split is the named, trigger-gated upgrade.** When **measured passive bandwidth, or a thin-link / small-screen passive viewer**, actually bites, add a second encoder config: the **active** stream returns to effectively-infinite GOP (pulse-free — restoring ADR-0024 SD3 literally), and a **passive** stream is tuned for compression (periodic IDR, B-frames, optionally a different codec), shared by all passives. The trigger is recorded so the limit is a stated gate, not a silent cap. (SVC / temporal layers is the only single-encoder way to serve both targets — carrier drops enhancement-layer NALs for passive — but it is not light and hardware support is uneven; noted, not pursued.)

- **SD8 — Wire additions are additive on `0x03`** (ADR-0082's `boxer/imzero2/v1` discipline): `ClientHello { caps {webcodecs}, geometry, label? }`, `Roster { you {id, role}, active_id, count, max, connections[{ id, role, label?, webcodecs }] }`, `TakeSession`, `RoleChanged`. Video stays `0x01` (now broadcast to all connections); input stays `0x02` (dropped from passive connections at the server). The hello extends the existing geometry/cadence handshake rather than adding a new frame type.

- **SD9 — Roster identity stays minimal.** Per connection the roster carries `id`, `role`, an optional user/device `label` ("iPad"), and the `webcodecs` (takeover-capable) flag — nothing more. Peer IP is used for ADR-0082 rate-limiting/audit only and never appears in the roster. One principal (ADR-0082); this is not multi-tenancy.

- **SD10 — ADR-0024 SD3's rationale is active-scoped.** SD3's no-mid-stream-IDR rule was chosen for the active low-latency single stream. The demo's shared periodic-IDR stream accepts a tunable refresh on the active view as the lightness price; SD7's upgrade restores the pulse-free active stream when warranted. SD3 is not contradicted — its scope is clarified.

- **SD11 — Out of scope / deferred.** `<img>`/MSE/HLS/WebRTC delivery; AV1/VP9 codec negotiation; WebTransport; per-passive resolution/bitrate; audio; clipboard (ADR-0082 SD6); the embeddable web component, in-client window manager, and multi-app compartmentalization ([ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md)). Build order: roles + roster + `ViewMode` **loopback-first, no auth**; then the shared periodic-IDR encoder + broadcast; then ADR-0082 auth wrapping `admit`; then takeover end-to-end.

## Alternatives

- **O2 — Per-passive image snapshots (MJPEG/WebP `<img>`).** Self-contained, universal, trivial client, CDN-cacheable. Rejected as the default because it is all-intra: it discards the frame-to-frame correlation that dominates a dashboard, so it compresses far worse than inter-frame video exactly where bandwidth matters; its one edge — reach without WebCodecs — is void under the modern-browser-only scope. Not retained even as a fallback for this demo.
- **O3 — Per-viewer encoders.** A dedicated encoder per passive viewer is literal O4 fan-out (N× encode). Rejected (ADR-0082 O4).
- **O4 — MSE / HLS passive tier.** The browser handles demux/buffer/ABR and segments are static and cacheable, which scales passive fan-out and reaches non-WebCodecs and Apple-native clients. Rejected for the demo (second decode path, container/segmenter infrastructure, seconds-scale latency floor); **held as the passive scaling/reach upgrade** when viewer counts or reach justify it.
- **O5 — WebRTC.** Real-time latency, closed-loop congestion control, NAT traversal, and a data channel for input. Rejected for the demo on dependency/signalling footprint and the need for an SFU to broadcast — ADR-0024's deferred O3; **held as the active-tier real-time upgrade** if NAT/congestion become hard requirements.
- **Two encoders from the start.** Genuinely optimises both targets but pays a second encode whenever ≥ 1 passive. Rejected for the demo on lightness; promoted to default by SD7's trigger.
- **SVC / temporal layers.** The only single-encoder way to serve both targets (carrier drops enhancement-layer NALs for passive). Rejected on complexity and uneven hardware support.

## Consequences

### Positive

- **Smallest delta over the shipped pipeline.** One decoder, one transport, one encoder reconfigured (`-g ∞ → periodic`, broadcast-to-N), the second-connection path changed from hard-reject to passive, and four additive `0x03` messages. No new dependencies.
- **O4's costs are structurally absent** (the deliberate re-opening ADR-0082 anticipated): one shared stream → no N× encode; scheduled IDRs → a joiner waits for the next one, so nobody already watching is pulsed; passive is read-only → no input arbitration; one geometry owner → no reconciliation.
- **Presence and handoff are first-class** (SD1/SD3): the roster surfaces who is connected and who may take over, and the user can move between their own devices.
- **Server-authoritative roles** remove the two-clients-both-think-they-are-active race.

### Negative

- **The single encoder optimises active only; passive rides sub-optimally** (SD6) — no B-frames/passive-tuning, no independent resolution/bitrate. Bounded for the demo, remedied by SD7's two-encoder upgrade.
- **The active view pays a periodic refresh** on the shared stream (SD5/SD10) — a tunable price, reversed by SD7.
- **Modern-browser-only** (no `<img>`/MSE fallback): a browser without WebCodecs cannot watch at all.
- **A prerequisite is unbuilt:** roles/roster/`ViewMode` build loopback-first, but any non-loopback deployment needs ADR-0082's auth at `admit`, which is itself not yet implemented.

### Neutral

- **The roster is minimal** (SD9) and forward-compatible: the hello carries `caps`, so codec/format negotiation (AV1/VP9, MSE/HLS) clips on later without a wire break.
- **Format/codec choice is deferred but anticipated** — the abstraction (caps-at-hello, role-mirrored `ViewMode`) accommodates the SD7 upgrade and the deferred deliveries without re-architecture.

### Derived practices

- **Server-authoritative roles + per-connection latest-wins queues** become the pattern for any future multi-connection boxer carrier.
- **Gated-upgrade triggers are stated, not silently capped** (SD7): when a demo cut bounds a target, the trigger that lifts it is written down.

## Status

Proposed — 2026-06-14. On acceptance, [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) (SD3, active-scoped) and [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) (SD5, standby → passive) gain dated Tier-2 `## Updates` pointers to this ADR, per `doc/DOCUMENTATION_STANDARD.md` §1.

Implementation phasing: **Phase 1** — `Registry` + roles + `Roster` + browser `ViewMode` + roster panel, loopback, no auth. **Phase 2** — single shared periodic-IDR encoder + broadcast-to-N + per-connection queues. **Phase 3** — ADR-0082 auth at `admit`. **Phase 4** — takeover end-to-end (promote/demote, lone-passive auto-promote, geometry rebuild on differing-geometry takeover).

Status lifecycle: `proposed → accepted → (deferred | deprecated | superseded by ADR-XXXX)`.

## References

- [ADR-0024 — ImZero2 remote access via headless render + ffmpeg + browser viewer](./0024-imzero2-remote-access-browser-viewer.md) — the shipped pipeline this tier reuses; SD3 (no mid-stream IDR), SD6 (wire framing), SD9 (frame mailbox) are load-bearing here.
- [ADR-0082 — Securing the ImZero2 remote session](./0082-imzero2-remote-session-auth-tls.md) — single-active-session, standby + takeover, auth-at-admit; this ADR refines its SD5 (standby → passive) and is the deliberate re-opening of its rejected O4.
- [ADR-0062 — ImZero2 render cadence](./0062-imzero2-render-cadence.md) — reactive cadence + blake3 dedup; why idle passive encode is ≈ free.
- [ADR-0087 — ImZero2 browser client architecture](./0087-imzero2-client-compositor-compartmentalization.md) — the embeddable-component / multi-app compositor envelope this single-app tier is the per-backend substrate for.
- [WebCodecs API](https://www.w3.org/TR/webcodecs/) — the single browser-side decoder for both tiers.
