---
type: adr
status: accepted
date: 2026-06-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-13
---

# ADR-0082: Securing the ImZero2 remote session — TLS, bearer-token auth, clipboard, and single-active-session handoff

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) shipped remote access to a browser tab — headless egui render → ffmpeg H.264 → WebSocket → WebCodecs viewer, with protobuf input flowing back — but deliberately scoped v1 to a single unauthenticated session on a localhost WebSocket. It deferred authentication, multi-tenancy, and K8s packaging to "a follow-up ADR" in three places (Context, SD11, and the closing Remainders note). **This is that follow-up**, narrowed to the security boundary plus one capability (clipboard) that the parent folded in because it crosses the same boundary.

The exposure being closed is concrete, not hypothetical. The shipped carrier binds `IMZERO2_HEADLESS_LISTEN` and is already used over the LAN to an iPad (per the parent's update log). Today that wire is plaintext `ws://` with no auth, so anyone on the network can both **watch** the dashboard (data exfiltration — the rendered UI may show sensitive data) and **inject input** (full control of the session). Clipboard sync, requested alongside, would add sensitive data crossing that same open wire in both directions.

The threat model for this ADR, set in the design dialogue:

- **Primary target: hostile / internet-exposed.** A single user reaches one running instance across an untrusted network (VPS, port-forward, Tailscale exit). TLS is mandatory, the auth secret must resist brute force, and connection attempts must be rate-limited and audited.
- **Must remain usable from a locked-down enterprise fleet** (the deployment class that motivates [ADR-0077](./0077-keelson-browser-wasm-execution.md)): the viewer must depend on nothing a policy-crippled browser blocks — no client-certificate provisioning, no browser extensions, no WebRTC, no capability beyond WebCodecs (already an ADR-0024 dependency) and standard `https`/`wss`.
- **Same-principal device handoff, in scope; simultaneous multi-view is not.** One running instance is reached by **the same user** from possibly several devices, but only ever **one at a time** — the user moves between a laptop, an iPad, and a phone, they do not watch on all three at once. The requirement is therefore *easy handoff* — a deliberate "take session" on the device being moved to — not a simultaneous broadcast. This is not multi-tenancy: one principal, one shared session, one active stream. Distinct identities, per-user isolation, and OIDC/SSO stay deferred to the K8s phase. Fanning one encoder out to N live viewers (N× encode, or a join-pulse that violates the no-mid-stream-IDR rule, plus geometry reconciliation and input arbitration) is explicitly rejected for a need that only ever has one active screen — see Alternatives.

Constraints inherited from the shipped system that this ADR must not break:

- **ADR-0024 SD3 — no mid-stream IDR.** Periodic key frames re-quantise the mostly-static screen and read as a visible colour pulse (measured RMSE 316). The encoder runs an effectively-infinite GOP; the only IDR is at stream start. This is also the kill-reason for a shared-encoder broadcast (Alternatives).
- **ADR-0024 SD6 — one binary WebSocket, 1-byte type prefixes** (`0x01` video / `0x02` input / `0x03` session control). New control messages are additive on `0x03`.
- **ADR-0024 per-connection encoder + SD9 frame mailbox.** The encoder spawns on viewer connect and stops on disconnect, so every connection starts at SPS/PPS + IDR; a depth-1 latest-wins mailbox sits between render and the encoder feeder. The takeover model below re-points this single encoder rather than multiplying it.
- **[ADR-0062](./0062-imzero2-render-cadence.md) reactive cadence + blake3 frame dedup** mean an idle dashboard encodes ~nothing.
- **The carrier is already proxy-ready.** The viewer connects same-origin to `/ws` and auto-upgrades to `wss` when the page is served over `https:` (`viewer/index.html`), and the carrier's own module docs already name "a single TLS-terminating reverse proxy in front (or an SSH tunnel)" as sufficient for the whole wire.
- **Self-contained single binary is a valued property.** [ADR-0081](./0081-imzero2-headless-rdp-egfx-head.md)'s withdrawal explicitly prized "no session machinery, unprivileged, single-binary." Security must not *force* a sidecar.

There is no reusable in-process server-TLS or auth module to inherit. The only house idioms are stylistic: the Kafka client's PEM-cert-from-file + optional-CA shape (`public/streaming/persisted/kafka/cli/kafka.go`), and `Bearer <secret>` headers in the LLM clients. The Rust-side TLS reference is the rustls stack surveyed for the withdrawn ADR-0081. So this is a fresh design built from known-good primitives.

## Design space (QOC)

**Question.** How should ImZero2 secure its single remote session for a hostile / internet-exposed target that must also be reachable from a locked-down fleet, and let the one principal move that session between their own devices, without re-architecting the shipped headless-render + single-encoder + carrier path or violating ADR-0024 SD3?

The decision is a coherent *posture* bundling three semi-independent axes (where TLS terminates; how the viewer authenticates; how device contention is resolved). The options below are internally-consistent bundles; the chosen bundle's per-axis rationale is broken out in the subsidiary decisions.

**Options.**

- **O1 — Hybrid TLS + token-in-subprotocol + single active session with explicit takeover (chosen).** Bind-address gate: loopback open for dev, non-loopback requires a token and TLS (in-process rustls *or* an asserted trusted proxy). Auth is a shared high-entropy bearer token in the WebSocket subprotocol. Exactly one connection is *active* (streams + drives input); additional authenticated devices park in *standby* and take over via a deliberate button; one encoder, re-pointed on takeover.
- **O2 — Proxy-mandatory everything.** Carrier binds loopback-only; an `oauth2-proxy`/Caddy front always terminates TLS and performs OIDC/SSO; single active session behind it.
- **O3 — mTLS-rooted.** In-process rustls with mandatory client-certificate auth (MDM-pushed); single active session.
- **O4 — Simultaneous multi-device broadcast.** Token + hybrid TLS, but fan one render out to N concurrent live viewers — either N parallel encoders or one shared encoder — with a driver/viewer split for input. The rejected-complexity baseline.

**Criteria.**

- **C1 — Secure-by-default for the hostile target.** No silent plaintext/unauth on a non-loopback bind; wire confidentiality + integrity; brute-force-resistant auth.
- **C2 — Locked-down-fleet reachability.** Nothing beyond standard `https`/`wss` + WebCodecs; no client-cert provisioning, no extensions, no exotic transport.
- **C3 — Self-contained single binary preserved.** No *mandatory* sidecar (the ADR-0081-withdrawal property).
- **C4 — Implementation delta from the shipped path.** Reuse of the single per-connection encoder, single video channel, and resize path vs new plumbing.
- **C5 — Device-handoff UX.** The principal moves between their own devices without lockout and without accidentally yanking a live session.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (hybrid/token/takeover) | O2 (proxy-everything) | O3 (mTLS) | O4 (broadcast) |
|----|----------------------------|-----------------------|-----------|----------------|
| C1 | ++                         | + (security = proxy correctness; a misconfig is wide open) | ++ | ++ |
| C2 | ++                         | + (OIDC redirect + IdP reachability fragile in crippled browsers) | −− (client-cert provisioning to fleet browsers is the hard part) | ++ |
| C3 | ++                         | −− (always a sidecar)  | + (self-contained but needs a PKI) | ++ |
| C4 | ++ (reuses single encoder) | − (proxy integration)  | − (cert plumbing heavier) | −− (N× encode or join-pulse + geometry + input arbitration) |
| C5 | ++ (deliberate takeover)   | + (depends on the front) | ++ | + (everyone sees at once, but the real need — move devices — is met heavily) |

O1 wins decisively: it is the only bundle that is simultaneously fleet-compatible (C2), self-contained-capable (C3), and the **smallest delta from the shipped single-session path** (C4) while meeting the actual handoff need (C5). O4 is the option this revision deliberately rejects — it reintroduces either N× encode or a shared-encoder join-pulse (violating ADR-0024 SD3), plus geometry reconciliation across differently-sized screens and driver/viewer input arbitration, all to serve a single user who only ever looks at one screen at a time; explicit takeover delivers the real need at a fraction of the cost. O3 matches O1 on security but fails the fleet criterion on client-certificate provisioning (held as the escalation for a fleet that already runs a client-cert PKI). O2 leans on infrastructure for everything, loses the self-contained binary, and makes security contingent on a correctly-configured proxy — but its OIDC-at-the-proxy shape remains the recommended *production* front and the natural multi-tenant path.

## Decision

We adopt **O1**. Concretely:

- A **bind-address security gate** makes non-localhost deployment safe by construction: loopback stays open for development; binding any non-loopback interface refuses to start unless a token is configured and TLS is satisfied.
- The viewer authenticates with a **shared, high-entropy bearer token carried in the WebSocket subprotocol**, validated in the upgrade handshake before the session is admitted, with a constant-time compare, per-IP rate-limiting, and an audit log of failures.
- **TLS is hybrid**: optional in-process rustls (cert + key from PEM, the Kafka idiom) keeps the binary self-contained; a loopback bind behind a TLS-terminating reverse proxy or tunnel is the documented production path. Either satisfies the gate; neither is forced.
- **There is one active session.** Exactly one connection streams and drives input; additional authenticated devices park in standby and present a single **"Take session"** button. Taking over unilaterally evicts the current holder (notified, offered take-back) and re-points the single encoder to the newcomer (a fresh IDR — today's per-connection behaviour). At most one stream ever exists, so the shipped single-encoder path is reused unchanged.
- **Clipboard** rides the now-secured channel, gated by the browser's own secure-context and gesture/permission requirements, and is opt-in.

The shipped interactive `hmi.sh` desktop path is untouched; all of this lives behind the existing `headless` Cargo feature.

### Subsidiary design decisions

- **SD1 — Bind-address security gate (secure-by-default).** The carrier's posture is driven by what it binds. **Loopback** (`127.0.0.1`/`::1`, today's default) → unchanged: plaintext, no token. This is the dev default and the home of the proxy/tunnel production path. **Non-loopback** (`0.0.0.0`, a routable interface) → the carrier **refuses to start** unless *both* a token is configured *and* TLS is provided (in-process cert/key present). A separate `IMZERO2_HEADLESS_TRUST_PROXY=1` lets an operator bind loopback (or an internal interface) behind a TLS-terminating front, asserting that the wire is encrypted upstream; this enables the proxy production path while still **requiring the token** by default (defence in depth — a misconfigured proxy must not silently expose an unauthenticated session). The gate fails *closed* and *loud*: a non-loopback bind without the prerequisites is a startup error naming the missing piece, never a silent plaintext listener.

- **SD2 — Bearer-token auth in the WebSocket subprotocol.** The browser `WebSocket` constructor cannot set arbitrary request headers; the *only* field it can influence is `Sec-WebSocket-Protocol` (via `new WebSocket(url, [subprotocols])`). The token therefore travels as a subprotocol value, validated in the `tokio-tungstenite` `accept_hdr_async` handshake callback **before** the upgrade completes and **before** the connection is admitted as active or standby (SD5). This keeps the secret out of the URL — and thus out of access logs, the `Referer` header, and browser history — which `?token=` query strings cannot (the same idiom the Kubernetes apiserver uses: `base64url.bearer.authorization.k8s.io.<token>`). The server echoes the accepted subprotocol and compares the token in constant time (`subtle`-style equality, not `==`). Provisioning: **`IMZERO2_HEADLESS_TOKEN_FILE` is preferred over `IMZERO2_HEADLESS_TOKEN`** — a file avoids leakage via `/proc/<pid>/environ` and `ps`, matching the Kafka client's secrets-from-file idiom. The token must be high-entropy (256-bit, base64url ≈ 43 chars); at that entropy online brute force is a non-issue, but for the hostile target the carrier additionally applies **per-IP connection rate-limiting with backoff** and emits a **structured audit record on every auth failure** (cheap, and it defends against resource exhaustion and surfaces probing). An `IMZERO2_HEADLESS_PROXY_AUTH=1` escape declares the front (an `oauth2-proxy`/SSO sidecar) the auth boundary and disables the in-app token, so the OIDC production path does not double-authenticate.

- **SD3 — Public page, gated socket; paste-the-token bootstrap.** The served viewer HTML carries **no secret** — it is open-source JS, harmless to serve unauthenticated. Only the WebSocket is an asset, so only the WebSocket is gated; the page-serving HTTP listener needs no auth. The viewer presents a **paste-the-token field**; the token is held in memory and passed via the subprotocol (SD2), never placed in a URL on the hostile path. `?token=` in the page URL is accepted **only on a loopback bind** as a personal-use convenience (where history/log leakage is local). This cleanly separates "serve the harmless page" from "authorise the sensitive channel."

- **SD4 — TLS is hybrid: optional in-process rustls, or a documented proxy/tunnel.** The in-process path wraps the accepted `TcpStream` in a `tokio-rustls` `TlsAcceptor` before the WebSocket handshake, loading cert + key from `IMZERO2_HEADLESS_TLS_CERT_FILE` / `IMZERO2_HEADLESS_TLS_KEY_FILE` (PEM via `rustls-pemfile`, the Kafka idiom). The proxy path (Caddy / Traefik / Tailscale-serve / `ssh -L`) terminates TLS upstream and forwards to a loopback bind under `IMZERO2_HEADLESS_TRUST_PROXY`. **Production uses a real certificate** (Let's Encrypt at the proxy, or a provisioned cert file in-process); an optional `IMZERO2_HEADLESS_TLS_SELF_SIGNED=1` auto-generates a self-signed cert as a *personal-tier* convenience, with the browser trust-prompt understood as the wart of that tier. The viewer already selects `wss` when the page is `https:`, so no viewer change is needed for either path.

- **SD5 — One active session; standby + explicit takeover.** ADR-0024's single-session invariant is preserved, but a second *authenticated* connection is no longer hard-rejected. It is admitted to **standby** — authenticated, holding the WebSocket, receiving only `0x03` session control, **not** the video stream — and its viewer shows a single large **"Take session"** button. A `TakeSession` request (`0x03`, client→server) **unilaterally** makes the requester the active session: the previous holder is demoted to standby and notified with a `SessionTaken` (`0x03`) so its viewer switches to a **"Take it back"** button, and the single encoder is re-pointed at the newcomer — which spawns a fresh SPS/PPS + IDR exactly as the shipped per-connection encoder already does on connect. **At most one stream exists at any instant**, so ADR-0024 SD3 (no mid-stream IDR) holds trivially and the shipped single-encoder + single-video-channel + resize path is reused unchanged — no fan-out, no per-viewer encoders, no driver/viewer input arbitration. The active session owns geometry and cadence: its `ViewportResize` / `SetCadence` drive the offscreen target (today's resize path), and a takeover rebuilds the target to the newcomer's geometry, so differently-sized devices never stream at once and there is no scaling reconciliation. Takeover from a *live* holder is always the deliberate button — opening a tab on another device, or a background tab reloading, must not silently yank a live session; a slot *freed* by the holder disconnecting is reclaimable, with a lone standby auto-promoted (zero-friction return to your other device) and multiple standbys each keeping the button. Input, `ViewportResize`, `SetCadence`, and clipboard injection are honoured **only** from the active session and dropped from standby connections. Takeover is unilateral and needs no approval because there is one principal — this is the user moving between their own devices, not a negotiation between parties. `IMZERO2_HEADLESS_MAX_CONNECTIONS` bounds how many authenticated standbys may park at once.

- **SD6 — Clipboard rides the secured channel, gated and opt-in.** The seam is the one named in ADR-0024's Remainders: a bidirectional `0x03` clipboard message; the headless host drains `FullOutput.platform_output.copied_text` (already produced by the `CopyTextToClipboard` FFFI2 opcode and currently dropped) and writes it toward the **active** session, and injects `egui::Event::Paste` from the active session's reported clipboard. The browser's `navigator.clipboard` requires a **secure context**, which SD1/SD4 now guarantee for any non-loopback deployment. The read direction (viewer clipboard → host paste) stays **opt-in and behind the browser's gesture/permission gate**: a leaked token must not silently exfiltrate whatever the user copied for another application. Only the active session's clipboard syncs; standby connections neither read nor inject.

- **SD7 — New env knobs registered in the env registry.** `IMZERO2_HEADLESS_{TOKEN_FILE,TOKEN,TLS_CERT_FILE,TLS_KEY_FILE,TLS_SELF_SIGNED,TRUST_PROXY,PROXY_AUTH,MAX_CONNECTIONS}` are added to the Go-side [`imzero2env`](../../public/thestack/imzero2/imzero2env/imzero2env.go) registrations under the [ADR-0009](./0009-environment-variable-registry.md) registry, surfacing in `doc/env-vars.md` alongside the existing `IMZERO2_HEADLESS_*` and `IMZERO2_SCREENSHOT_*` specs. The Rust client continues to read them directly via env inheritance (the same pattern as `IMZERO2_RENDER_CADENCE`, [ADR-0062](./0062-imzero2-render-cadence.md)); the registry is the discovery/documentation home, and the secret-bearing ones (`TOKEN`, `TOKEN_FILE`) are flagged so the catalog never echoes their values.

- **SD8 — Out of scope / deferred.** Multi-*tenancy* (distinct identities, per-user session isolation, OIDC/SSO as a first-class in-app feature) → the K8s phase; an SSO front is delegated to a proxy (SD2 `PROXY_AUTH`) if needed before then. **Simultaneous multi-device viewing (broadcast fan-out)** → rejected, see Alternatives; a future "real multi-viewer" ask (distinct people watching together) is multi-tenancy-adjacent and re-opens it deliberately. Audio. Native-client auth (the Phase N native client speaks the same token, but its bootstrap UX is its own design). Each is named so the escape hatch is explicit and the v1 surface stays small.

## Alternatives

- **Simultaneous multi-device broadcast / fan-out (O4).** Stream to several devices at once, each a live viewer. Rejected: it forces a choice between **N parallel encoders** (N× encode cost) and **one shared encoder** (which must force an IDR whenever a device joins, pulsing every viewer already watching — a direct violation of ADR-0024 SD3), and on top of that needs geometry reconciliation across differently-sized screens and a driver-vs-viewer input-arbitration model — all to serve **one user who only ever looks at one screen at a time.** Explicit single-session handoff (SD5) meets the real need at a fraction of the complexity, reusing the shipped single-encoder path. Kept as the kill-reason so a genuine multi-viewer requirement (distinct people watching together) re-opens it deliberately, with the K8s/multi-tenancy phase, rather than by accident.
- **Hard-reject the second connection (shipped v1 behaviour).** Simplest, but it locks the user out of their own session from a second device — you must kill the first to use the second. Rejected: "move between my devices" is the explicit requirement; standby + takeover (SD5) is the minimal change that enables it.
- **Auto-steal on connect (no button).** A connecting device immediately becomes active. Rejected: opening the viewer tab on another device — or a background tab reloading — would silently yank a live session. The deliberate "Take session" button (SD5) is the guard.
- **mTLS client certificates instead of a bearer token (O3).** Strong identity, no shared secret to leak, MDM-friendly where a fleet already pushes client certs. Rejected as the baseline because provisioning per-device client certs into managed browsers — and the clunky browser client-cert selection UX — is exactly the friction a locked-down fleet imposes; a token over `wss` is maximally fleet-compatible. Held as the escalation for a fleet with an existing client-cert PKI; it composes with the in-process rustls of SD4.
- **Proxy-delegated OIDC/SSO as the only auth (O2).** Punts identity entirely to `oauth2-proxy`/Caddy. Rejected as the baseline because it forces a sidecar even for the self-contained personal and fleet cases and makes security contingent on the proxy being present and correctly configured. Retained as the recommended *production* front (SD2 `PROXY_AUTH` turns off the in-app token) and the natural multi-tenant path.
- **Token in the URL (`?token=`, Jupyter-style).** The simplest bootstrap. Rejected for the hostile target because the secret lands in browser history, the `Referer` header, and proxy/access logs. Kept as a loopback-only convenience (SD3).
- **In-process-TLS-only, or proxy-only.** The two non-hybrid TLS shapes. In-process-only removes the loopback-plaintext dev convenience and makes the binary own a cert lifecycle unconditionally; proxy-only always needs a sidecar. The hybrid (SD4) keeps both doors and lets the deployment pick.

## Consequences

### Positive

- **Non-localhost deployment is safe by construction.** The bind-address gate (SD1) makes accidental plaintext/unauthenticated exposure a loud startup error rather than a silent listener; the hostile target gets TLS + a high-entropy token + rate-limiting + an audit trail.
- **Fleet-reachable with nothing exotic.** A token over standard `wss` + WebCodecs is the whole client-side requirement — no client certs, extensions, or non-standard transport.
- **The self-contained single binary is preserved.** In-process TLS is optional, so there is no *mandatory* sidecar — the property ADR-0081's withdrawal prized survives, while the proxy path remains available for those who want it.
- **Device handoff is one deliberate button.** The user moves between laptop, iPad, and phone without killing the first session, and reclaims a slot freed by a closed device with zero friction (SD5).
- **ADR-0024 SD3 holds trivially.** One stream, one encoder, an IDR only on connect or takeover — exactly as shipped. No pulse, because there is never a second live viewer to disturb.
- **Smallest possible delta from the shipped path.** The single per-connection encoder, the single video channel, and the geometry/resize path are reused unchanged; the only new behaviour is the second-connection path (reject → standby + takeover), two additive `0x03` messages (`TakeSession`, `SessionTaken`), the auth handshake check, and an optional rustls wrap.
- **Clipboard is unblocked** on a channel that is now a guaranteed secure context (SD6).

### Negative

- **In-process TLS adds a cert lifecycle to own** — provisioning, rotation, and the self-signed browser trust-prompt at the personal tier. Production sidesteps it by terminating a real cert at a proxy.
- **One bearer secret, not per-user identity.** A leaked token grants full session access (mitigated by entropy + constant-time compare + rate-limit + audit + TLS, but it is a single shared secret) — the price of deferring real multi-tenant identity to the K8s phase.
- **The viewer gains a small state machine.** The previously near-stateless page now tracks active/standby ownership and renders the "Take session" / "Take it back" states — modest, but new client-side state.

### Neutral

- **Takeover is unilateral — no approval prompt.** Correct for a single principal moving between their own devices; it means the device being taken from is interrupted without consent, which is by design. A genuine multi-viewer ask (distinct people) would need a different model and is deferred with multi-tenancy.
- **Subprotocol-as-auth-carrier is an established idiom** (the Kubernetes apiserver), not a novel invention.
- **`TakeSession` / `SessionTaken` ride the existing `0x03` SessionControl with additive fields**, so ADR-0024's additive-schema-versioning discipline (`boxer/imzero2/v1`) applies unchanged.
- **The hybrid TLS posture matches how the carrier already framed deployment** — its module docs already called a TLS-terminating proxy or tunnel sufficient.

### Derived practices

- **Bind-address-driven security gates** (loopback-open / non-loopback-hardened, fail-closed-and-loud) become the default posture for any future boxer network listener.
- **Secrets-from-files over env** is the house default for the hostile target, extending the Kafka client idiom; catalog entries for secret-bearing env vars are flagged non-echoing.

## Status

Accepted — 2026-06-13.

Implementation phasing: **Phase 1** — bind-address gate (SD1) + token-in-subprotocol auth (SD2) + rate-limit/audit, no TLS yet, exercised on loopback and behind a proxy. **Phase 2** — in-process rustls (SD4), cert-from-PEM, optional self-signed. **Phase 3** — single-active-session handoff (SD5): change the second-connection path from hard-reject to standby, add the `TakeSession` / `SessionTaken` `0x03` messages, re-point the existing single encoder on takeover, and the freed-slot reclaim rule. **Phase 4** — clipboard set/paste (SD6). **Phase 5** — viewer UI (paste-field, the "Take session" / "Take it back" states, clipboard opt-in) + end-to-end verification across two devices: takeover, eviction notification, take-back, and freed-slot auto-promotion. On acceptance, ADR-0024 gains a dated `## Updates` pointer to this ADR (Tier-2), per the edit-policy tiers.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See `doc/DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-06-14 — Client-architecture envelope accepted as ADR-0087; active/passive viewer tier proposed as ADR-0086

The client-side direction deferred here split into two follow-ups. [ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md) (accepted 2026-06-14, as a posture plus a gated deferral) records the embeddable-web-component + in-client window-manager + multi-app **compartmentalization** posture: the browser client is a presentation compositor, never an enforcement boundary; the topology is MILS (separate single-level backends + a composing client) with enforcement in per-backend auth + network separation + a controlled host. Two consequences land on this ADR. **SD5's single-active session becomes the per-backend substrate** (a focused window is active, the rest passive). And **SD6 clipboard *sync* inverts to per-compartment *isolation*** in the compartmentalized (trusted-shell) deployment tier, because `navigator.clipboard` is page-global — the untrusted-embed tier makes no clipboard-isolation claim.

Separately, the deliberate re-opening of the **rejected O4** (concurrent multi-viewer) that the Alternatives held the kill-reason for is designed in [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) (proposed): a read-only **passive** tier that meets the multi-viewer need without N× encode or a mid-stream-IDR pulse. There, **SD5's "standby" is renamed "passive"** and gains a low-cost read-only video view plus a first-class connection **roster**. On ADR-0086's acceptance this ADR gains its own dated pointer for that standby→passive refinement.

## References

- [ADR-0024 — ImZero2 remote access via headless render + ffmpeg + browser viewer](./0024-imzero2-remote-access-browser-viewer.md) — the parent; this ADR is its promised auth / multi-tenancy follow-up. SD3 (no mid-stream IDR), SD4/SD6 (wire framing), the per-connection encoder + SD9 mailbox, SD11 (deferral list) are load-bearing here.
- [ADR-0077 — keelson browser-wasm execution](./0077-keelson-browser-wasm-execution.md) — the locked-down enterprise-fleet deployment class that shapes the fleet-reachability criterion.
- [ADR-0081 — ImZero2 headless RDP EGFX head](./0081-imzero2-headless-rdp-egfx-head.md) — withdrawn; the single-unprivileged-binary property this ADR preserves, and the rustls-stack reference.
- [ADR-0062 — ImZero2 render cadence](./0062-imzero2-render-cadence.md) — reactive cadence + frame dedup; why idle encode is ~free.
- [ADR-0009 — Environment variable registry](./0009-environment-variable-registry.md) — the registry the new `IMZERO2_HEADLESS_*` knobs register in; [`imzero2env.go`](../../public/thestack/imzero2/imzero2env/imzero2env.go) is the Go-side registration home, `doc/env-vars.md` the surfaced catalog.
- `public/streaming/persisted/kafka/cli/kafka.go` — the in-tree TLS-cert-from-PEM-file + optional-CA idiom this ADR follows for in-process TLS and secrets-from-file.
- Kubernetes apiserver WebSocket auth — prior art for carrying a bearer token in `Sec-WebSocket-Protocol` (the `base64url.bearer.authorization.k8s.io.<token>` subprotocol used by `kubectl exec`/`attach` streaming); see [kubernetes/kubernetes](https://github.com/kubernetes/kubernetes) apiserver wsstream / RemoteCommand handling.
- [`tokio-rustls`](https://crates.io/crates/tokio-rustls) / [`rustls-pemfile`](https://crates.io/crates/rustls-pemfile) — in-process TLS termination + PEM loading.
- [`tokio-tungstenite` `accept_hdr_async`](https://docs.rs/tokio-tungstenite/latest/tokio_tungstenite/fn.accept_hdr_async.html) — handshake callback where the subprotocol token is validated pre-upgrade.
- [`subtle`](https://crates.io/crates/subtle) — constant-time equality for the token compare.
- [WebCodecs API](https://www.w3.org/TR/webcodecs/) and [`navigator.clipboard` secure-context requirement](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard_API) — the browser-side primitives and the secure-context gate clipboard depends on.
