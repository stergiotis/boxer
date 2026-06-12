---
type: adr
status: withdrawn
date: 2026-06-12
withdrawn-date: 2026-06-12
---

> **Status: withdrawn (2026-06-12) — retracted before acceptance; do not implement.** The same-day end-to-end verification of ADR-0024's browser path (see its 2026-06-12 implementation Updates entry) covered the remote-access need, and no concrete fleet requirement materialized to justify carrying a second delivery head. The Phase-0 spike was never run. The design space below — option assessment, kill-reasons for the session-shaped alternatives, and the verified IronRDP/lamco/xrdp ecosystem facts — remains valid reference: the ADR-0024 foundation is carrier-agnostic by construction, so if RDP reach resurfaces, re-opening starts from this analysis (likely as a fresh ADR re-verifying the ecosystem state), not from scratch.

# ADR-0081: ImZero2 headless RDP head — in-process EGFX/AVC delivery to stock RDP clients

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) (accepted 2026-06-12) gives ImZero2 a remote-access shape: a headless render host (egui + wgpu offscreen, no eframe) feeding an ffmpeg subprocess (`h264_vaapi -bf 0 -qp:v 26`, Annex-B out), carried over WebSocket to a browser WebCodecs viewer. Its 2026-06-12 Updates entry extended the delivery analysis to RDP clients and named two shapes: *session remoting* (the unmodified desktop binary inside xrdp) as the zero-code answer, and a *protocol-native EGFX head* held for later.

Session remoting is now ruled out by the deployment constraints, which this ADR adopts as its defining forces:

- **No desktop-session machinery.** The target environment runs no X server, no Wayland compositor, no sesman/PAM session stack. Everything xrdp-shaped — and equally GNOME remote-desktop's headless RDP login sessions (a full mutter/gnome-shell per seat) and Weston's RDP backend (a compositor; mainline additionally lacks H.264) — is excluded as a class, not on version details.
- **Unprivileged, containerized.** No root, no systemd units, no PAM. The RDP endpoint must run as an ordinary process in the same container as the application.
- **Single-binary integration.** The RDP endpoint is part of the imzero2 deliverable itself — one process to deploy, supervise, and version, exactly like the WebSocket head. Carrier variance is a build/runtime concern of the headless host, not an infrastructure component beside it.
- **Client environment.** The driving fleet is locked-down Windows ([ADR-0077](./0077-keelson-browser-wasm-execution.md)'s deployment class), where `mstsc.exe` is the one universally present, allowlisted client. EGFX-capable clients (mstsc on Windows 10+, FreeRDP/Remmina, Microsoft's macOS/iOS/Android clients) are assumed; pre-EGFX clients are out of scope.
- **Fidelity bar unchanged.** Chart-grade output at LAN bandwidth requires a hardware video codec — the criterion that eliminated the VNC family in ADR-0024. RDP meets it via the Graphics Pipeline Extension ([MS-RDPEGFX](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpegfx/da5c75f9-cd99-450c-98c4-014a496942b0)): AVC420/AVC444 are H.264.

Mechanical facts the design rests on (verified 2026-06-12):

- **The ADR-0024 encoder tail is carrier-independent.** Phases 1–2 produce an Annex-B H.264 elementary stream, 4:2:0, no B-frames — which is byte-for-byte the payload EGFX AVC420 messages wrap (plus region/QP metadata). An RDP head changes the framing, not the stream.
- **[`ironrdp-server`](https://docs.rs/ironrdp-server) 0.11.0 (MIT/Apache-2.0)** supplies the session machinery: tokio runtime, rustls TLS, x224 + fast-path input surfaced as `KeyboardEvent`/`MouseEvent` handler traits, DVC infrastructure, CLIPRDR and RDPSND server factories, display-control (client-driven resize). NLA is reachable later via `sspi-rs`.
- **[`ironrdp-egfx`](https://docs.rs/ironrdp-egfx) 0.1.0 (MIT/Apache-2.0)** supplies the graphics pipeline: MS-RDPEGFX PDUs with client *and server* processors, including AVC420/AVC444 stream types. It is young (0.1.0) and its server lane is unexercised by us — the concentrated risk of this ADR.
- **The mainline gaps don't block this use.** [IronRDP#1158](https://github.com/Devolutions/IronRDP/issues/1158) (filed by Lamco, 2026-03) tracks what's missing server-side: ClearCodec, RemoteFX Progressive, mixed-frame API — codecs needed only when the server encodes content itself. We bring our own H.264.
- **[`lamco-rdp`](https://github.com/lamco-admin/lamco-rdp) extension crates (input translation, graphics-pipeline glue) are MIT/Apache-2.0** and being upstreamed into IronRDP (#1158 is that coordination). The [`lamco-rdp-server`](https://github.com/lamco-admin/lamco-rdp-server) *product* proves the whole Rust EGFX/AVC server path works against mstsc/FreeRDP, but is BUSL-1.1 until 2028-12-31 and desktop-sharing-only — feasibility evidence and reference material, not a dependency.

## Design space (QOC)

**Question.** How should the imzero2 headless host expose an RDP endpoint that delivers hardware-H.264 fidelity to stock RDP clients, runs unprivileged inside the single deployable binary, and maximally reuses the ADR-0024 headless-render + encoder foundation?

**Options.**

- **O1 — In-process head on mainline IronRDP**: `ironrdp-server` for session/transport/input, `ironrdp-egfx` server processor wrapping the SD3 encoder output as AVC420 (chosen; spike-gated).
- **O2 — FreeRDP server-SDK sidecar**: a small C program on FreeRDP's proven server/shadow EGFX machinery (Apache-2.0), run as a subprocess; frames out / input back over local IPC.
- **O3 — Build on the `lamco-rdp` extension crates** atop O1's skeleton, adopting their pipeline/input layers as dependencies.
- **O4 — Session-shaped servers** (xrdp ≥ 0.10.2, GNOME remote-desktop ≥ 46 headless, Weston RDP backend).
- **O5 — Windows RemoteApp/RDS hosting**: run the desktop binary on Windows Server; the OS RDP stack provides AVC444 + single-window RAIL.

**Criteria.**

- **C1 — Constraint fit**: unprivileged, no session machinery, in-binary (the forces above; hard requirement).
- **C2 — Fidelity**: hardware video codec end-to-end (EGFX AVC).
- **C3 — Foundation reuse**: encoder tail, input-mapper shape, pacing principle, supervision pattern carry over from ADR-0024.
- **C4 — License/dependency hygiene**: permissive only; subprocess over linkage where practical.
- **C5 — Cost and risk to v1**: engineer-weeks; how concentrated the unknowns are.
- **C6 — Upstream-churn exposure**: who tracks crate/protocol evolution, in what form.
- **C7 — Forward path**: NLA, clipboard, audio, AVC444, multi-monitor, RD Gateway.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 ironrdp | O2 freerdp sidecar | O3 lamco crates | O4 session-shaped | O5 RemoteApp |
|----|:--:|:--:|:--:|:--:|:--:|
| C1 | ++ | +  | ++ | −− | −− |
| C2 | ++ | ++ | ++ | +  | ++ |
| C3 | ++ | +  | ++ | −  | −− |
| C4 | ++ | ++ | +  | ++ | −  |
| C5 | +  | −  | +  | ++ | −− |
| C6 | +  | +  | −  | +  | −  |
| C7 | +  | +  | +  | −  | +  |

**Assessment notes.** O1 wins on constraint fit and foundation reuse with all-permissive dependencies; its one concentrated unknown — the maturity of `ironrdp-egfx`'s server lane at 0.1.0 — is exactly the kind of risk a Phase-0 spike retires (the ADR-0077 §SD2 pattern), so the option is adopted *spike-gated* rather than on faith. O2 has the most battle-tested EGFX/AVC server guts in existence and matches the encoder-as-subprocess house practice, but adds a C program we must author and an IPC protocol we must design (frames out, input back — re-deriving ImZero1's tail at a second seam), plus FreeRDP's large server API surface to hold correctly; it is the structural fallback, not the first choice. O3 is legally clean but couples us to a vendor fork mid-upstreaming — its correct use is reference and cherry-pick, converging into O1 as #1158 lands (C6 is the objection, not C4). O4 fails C1 outright on all three constraints (session machinery, privileges, in-binary); recorded with the additional per-option defects — mainline xrdp encodes in software only, Weston mainline has no H.264 — and the xrdp recipe remains documented in ADR-0024's Updates for environments where it *is* viable. O5 fails C1 the same way and adds three kill-reasons of its own: the data plane (`chlocalpool` spawns `clickhouse-local`, which has no Windows build), RDS CAL licensing, and a new OS target for the whole stack.

## Decision

We will implement **O1**: an RDP head inside the ADR-0024 headless host, built on mainline `ironrdp-server` + `ironrdp-egfx`, wrapping the unchanged SD3 encoder output as EGFX AVC420 — **gated by the SD1 interop spike** before any integration work.

### Subsidiary design decisions

- **SD1 — Phase-0 interop spike is the gate.** A standalone toy server (no imzero2 coupling): `ironrdp-server` + `ironrdp-egfx`, fed by ffmpeg `-f lavfi -i testsrc` through the exact SD3 flags, so the wire carries the production stream shape. Client matrix: mstsc on Windows 10/11, FreeRDP/Remmina; macOS client if reachable. Acceptance: EGFX capabilities negotiate; AVC420 frames render correctly; fast-path keyboard/mouse arrives in the handler traits; disconnect/reconnect is clean (fresh IDR); resize either works or fails cleanly (informs SD6 scoping). Timebox: days. On failure: first fix-and-upstream (Devolutions is responsive; Lamco's #1158 momentum helps), and if the server lane is structurally unfit, fall back to O2.
- **SD2 — Carrier heads are sibling Cargo features over the shared headless core.** `headless` (ADR-0024 SD1) stays the base; `head-ws` and `head-rdp` gate the carriers and may be compiled together, selected by runtime flag. Neither pollutes the desktop build; the pattern extends ADR-0024's "Cargo features for deployment-shape variance" practice.
- **SD3 — Encoder tail unchanged; AVC420 at v1; AVC444 deferred.** The head wraps the SD3 Annex-B NALs in EGFX surface commands (v1: one full-surface destination rect per frame, matching the continuous-frame model). AVC420 fidelity equals the browser head by construction — both ship 4:2:0. AVC444 (RDP's full-chroma mode, dual-420-stream packing) is the named upgrade for fine chart linework once the head is stable; it has no equally mature WebCodecs counterpart, so RDP would lead on fidelity at that point.
- **SD4 — Pacing rides EGFX frame acknowledgements.** An unacked-frame budget replaces the WebSocket send-queue rule with the same upstream-propagation principle from ADR-0024 SD9: when the budget is exhausted, the feeder stops sampling frames into the encoder; encoded frames are never dropped (`-bf 0` makes every frame a reference). If a client suspends acknowledgements (the queue-depth sentinel MS-RDPEGFX allows), the feeder falls back to timer pacing at the SD9 encoder cadence.
- **SD5 — Input maps at the head edge; the interpreter stays oblivious.** `RdpServerInputHandler` events translate to `egui::RawInput`: a scancode→`egui::Key` table (scancode set 1), unicode keyboard events to `egui::Event::Text`, mouse coordinates/buttons/wheel normalized per session, modifier state tracked per-session. This is the RDP sibling of ADR-0024 SD8; the [ADR-0013](./0013-imzero2-stateful-widget-contract.md) gated `r10_push` rule applies unchanged. `lamco-rdp`'s input-translation crate is the reference implementation (read, not depend, per O3's disposition).
- **SD6 — Resize via display-control, descoped if the spike says so.** The full path: display-control DVC → ResetGraphics → surface re-create → offscreen texture + encoder restart at the new geometry → forced IDR (ADR-0024's (re)connect rule transferring verbatim). Initial desktop size follows the client's connect-time monitor info. If the spike shows resize flaky in the young stack, v1 ships fixed-geometry-at-connect and resize becomes the first post-v1 item — connect-time sizing already covers the dominant dashboard case.
- **SD7 — Security posture matches ADR-0024 v1.** TLS via rustls (operator-provided or generated cert); no NLA at v1 — mstsc warns-but-connects, the xrdp precedent; network reachability is the access control, which confines v1 to trusted networks exactly as the WebSocket head's localhost posture does. NLA via `sspi-rs` is the named follow-up and the precondition for any multi-user or exposed deployment. Single session at v1: a second connection is rejected while one is active.
- **SD8 — Out of scope at v1** (named escape hatches): NLA/authentication; CLIPRDR clipboard (the `ironrdp-server` factory exists when wanted); RDPSND audio; multi-monitor; AVC444 (SD3); bitmap/RemoteFX fallback for pre-EGFX clients; RD Gateway integration; session reconnection-with-state (reconnect = fresh session + forced IDR); K8s packaging (shared follow-up with ADR-0024).

## Alternatives

- **O2 — FreeRDP server-SDK sidecar.** Apache-2.0, the most proven EGFX/AVC server implementation, and consistent with the encoder-as-subprocess practice. Rejected as first choice because it adds an authored C surface plus a bespoke frames/input IPC protocol — a second seam re-deriving what FFFI2 and the encoder pipe already demonstrate — and because O1's risk is cheap to retire first. **Retained as the structural fallback** if the SD1 spike shows `ironrdp-egfx`'s server lane unfit.
- **O3 — Depend on `lamco-rdp` extension crates.** MIT/Apache-2.0, so license-clean; rejected as a dependency because it tracks a vendor fork mid-upstreaming (churn lands twice: once in the fork, once when mainline absorbs it). Used as reference/cherry-pick material; converges into O1 as #1158 upstreaming completes.
- **O4 — Session-shaped servers (xrdp, GNOME remote-desktop headless, Weston RDP).** Excluded as a class by the Context constraints: all require display-server/session machinery and privileges the target does not have, and none lives inside the deliverable binary. Per-option additions: mainline xrdp is software-encode only; GNOME r-d headless drags mutter/gnome-shell per seat; Weston mainline lacks H.264 (fails C2 like the VNC family). The xrdp recipe stays documented in ADR-0024's 2026-06-12 Updates entry for environments where sessions are acceptable.
- **O5 — Windows RemoteApp/RDS.** OS-native AVC444 and single-window RAIL for free, but fails C1 (it *is* session machinery, on another OS), and the data plane kills it regardless: `chlocalpool` spawns `clickhouse-local`, which has no Windows build. RDS CALs and a new OS target compound. Revisit only if a Windows deployment of keelson materializes for independent reasons.

## Consequences

### Positive

- **`mstsc` reach with zero client-side work.** The fleet class that motivated the RDP axis is served by software already on every target machine; no viewer to build, distribute, or allowlist.
- **The third carrier proves the foundation's transport claim.** ADR-0024 called its wire format transport-abstract; WebSocket, (Phase N) WebRTC, and now EGFX all consume the same headless render + Annex-B tail — framing differs, stream doesn't.
- **Protocol machinery comes from the ecosystem.** Frame acks, resize, TLS, input framing, and later clipboard/audio are crate surfaces, not hand-rolled protocol code; all dependencies are MIT/Apache-2.0.
- **Risk is concentrated and cheap to retire.** One unknown (the 0.1.0 server lane), one days-scale spike, two named fallbacks in order (upstream fixes, then O2).

### Negative

- **A 0.1.0 dependency in the load-bearing path.** Even after a passing spike, expect interop bugs surfacing only against particular client versions; mstsc is closed and gives poor diagnostics. Budget interop-hardening time (Phase 2) rather than treating the spike as exhaustive.
- **TLS certificate provisioning becomes an operational concern** that the WebSocket-on-localhost head did not have.
- **No authentication at v1** confines deployment to trusted networks; the NLA follow-up is load-bearing for anything broader.
- **EGFX-capable clients required.** Pre-EGFX RDP clients get a rejected connection, not a degraded session, until a fallback codec path (if ever) is justified.

### Neutral

- **Additive to ADR-0024, not a re-sequencing.** The browser head remains the v1 target there; this head lands after (or beside) it on the same foundation. Estimated ~2–3 weeks after a passing spike, on top of ADR-0024 Phases 1–2.
- **The spike is independent of Phases 1–2** and may run first — it touches none of the imzero2 code.

### Derived practices

- **Spike-gate young protocol crates.** ADR-0077 §SD2 gated a guest-target hypothesis on a measured number; this ADR gates a 0.1.0 protocol dependency on a days-scale interop matrix. The pattern — adopt working hypotheses, gate on the cheapest decisive experiment — is house style now.
- **Carrier heads as sibling features over a shared core** is the deployment-shape pattern for any future carrier (WebRTC per ADR-0024 O3, native client per O2 there).

## Status

Withdrawn — 2026-06-12, same day as proposed, before review and before the Phase-0 spike ran. ADR-0024's browser delivery was implemented and verified end-to-end that day; with the primary remote-access channel working and no concrete RDP-only fleet requirement on the table, a second delivery head was not worth its protocol-machinery and interop-hardening cost. The phasing below is retained for reference only: **Phase 0** — SD1 spike (independent; may precede ADR-0024 Phases 1–2) → **Phase 1** — head integration on the headless foundation (EGFX pipeline glue, SD5 input mapper, SD7 TLS) → **Phase 2** — interop hardening across the client matrix; SD6 resize per spike verdict → follow-ups per SD8 (NLA first among them).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See `doc/DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless render + encoder foundation; its 2026-06-12 Updates entry holds the RDP-axis analysis this ADR descends from, including the session-remoting recipe excluded here.
- [ADR-0077](./0077-keelson-browser-wasm-execution.md) — the locked-down-fleet deployment class; the §SD2 spike-gate pattern SD1 reuses.
- [ADR-0013](./0013-imzero2-stateful-widget-contract.md) — widget contract the SD5 input mapper must respect.
- [ADR-0062](./0062-imzero2-render-cadence.md) — render cadence; SD4's timer-pacing fallback runs at the SD9 encoder cadence of ADR-0024.
- [MS-RDPEGFX — RDP Graphics Pipeline Extension](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpegfx/da5c75f9-cd99-450c-98c4-014a496942b0) — AVC420/AVC444, frame acknowledgements, ResetGraphics.
- [IronRDP](https://github.com/Devolutions/IronRDP) (MIT/Apache-2.0) — [`ironrdp-server`](https://docs.rs/ironrdp-server) 0.11.0, [`ironrdp-egfx`](https://docs.rs/ironrdp-egfx) 0.1.0, `sspi-rs`; non-AVC server codec gaps tracked in [#1158](https://github.com/Devolutions/IronRDP/issues/1158).
- [`lamco-rdp`](https://github.com/lamco-admin/lamco-rdp) (MIT/Apache-2.0) — IronRDP extensions: input translation, graphics pipeline; upstreaming in progress. [`lamco-rdp-server`](https://github.com/lamco-admin/lamco-rdp-server) (BUSL-1.1) — feasibility proof of the Rust EGFX/AVC server path.
- [FreeRDP](https://github.com/FreeRDP/FreeRDP) (Apache-2.0) — server SDK / shadow server; the O2 fallback's EGFX/AVC machinery.
- [xrdp releases](https://github.com/neutrinolabs/xrdp/releases) — the excluded session-remoting shape (GFX v0.10.0, H.264 v0.10.2); kept viable in ADR-0024's Updates for session-tolerant environments.
