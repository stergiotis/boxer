---
type: adr
status: accepted
date: 2026-06-14
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-14
---

# ADR-0087: ImZero2 browser client architecture — embeddable component, multi-app compositor, and compartmentalization posture

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) / [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) / [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) give a single-app remote viewer with active/passive tiers and a roster. A larger question was raised in the same design dialogue: what if the browser client became (a) a **web component** droppable into any webapp, and (b) an in-client **window manager** that connects to **multiple boxer backends at once**, to support a **multi-level / compartmentalized** security posture — distinct backends at distinct sensitivity levels, presented together?

These are strategic and, as a *build*, currently undecided. This ADR exists to **decide the architecture posture and the gate** — not to authorize an implementation. It is accepted as a **posture plus a deferral with guardrails**, because the load-bearing conclusions are reusable and the failure mode being deterred (treating a browser tab as a security-enforcement boundary) is concrete. The motivating deployment classes are the locked-down fleet of [ADR-0077](./0077-keelson-browser-wasm-execution.md) and the desire to reach several boxer apps from one surface.

The central caveat, stated first because everything rests on it: **a browser tab cannot be a security reference monitor.** Once two levels' pixels share a tab, DOM, or host page, the host page, browser extensions, devtools, and the OS can observe both. Any architecture that implies otherwise is security theater.

## Design space (QOC)

**Question.** What is the architecture and the enforcement model for an embeddable, multi-app ImZero2 browser client intended to present streams at different sensitivity levels — and under what gate is it built?

**Options.**

- **O1 — Accept the posture, defer the build behind a named gate (chosen).** Adopt as decided: the client is a *presentation compositor*, never the enforcement boundary; the topology is **MILS** (separate single-level backends + a composing client), with enforcement in per-backend auth + network separation + a controlled client host; embeddability and isolation are **deployment-trust tiers** of one component. Defer the implementation until the gate (the security-bar / audience question) trips.
- **O2 — Build the multi-app compositor now as a general convenience, no isolation claims.** Useful, but conflates "embed anywhere" with "isolation" and commits engineering to an undecided shape.
- **O3 — Pursue accreditable multi-level security enforced in the browser.** Rejected: the browser cannot be a reference monitor (see Context); this would be security theater.
- **O4 — One fat remote desktop running many apps** (the KasmVNC/neko model), streamed as a single composite. Rejected: it discards boxer's per-backend topological separation — the very thing that makes compartmentalization meaningful here.

**Criteria.**

- **C1 — Honest enforcement** (no claim the browser cannot back).
- **C2 — Locked-down-fleet reachability** (nothing beyond standard `https`/`wss` + WebCodecs).
- **C3 — Self-contained / embeddable** preserved.
- **C4 — Preserves boxer's per-backend (MILS) topology.**
- **C5 — Avoids premature build** of an undecided feature.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (posture + deferral) | O2 (build now, no claims) | O3 (MLS-in-browser) | O4 (fat remote desktop) |
|----|-------------------------|---------------------------|---------------------|-------------------------|
| C1 | ++                      | + (honest but unguided)   | −− (theater)        | −                       |
| C2 | ++                      | +                         | +                   | − (session weight)      |
| C3 | ++                      | +                         | +                   | −−                      |
| C4 | ++                      | +                         | ~                   | −− (loses topology)     |
| C5 | ++                      | −− (builds the undecided) | −−                  | −−                      |

O1 is the only option that is simultaneously honest about enforcement (C1), preserves the topology that makes compartmentalization real (C4), and does not commit engineering to an undecided shape (C5). O3 is the trap this ADR exists to name. O4 trades away the property (topological separation) that distinguishes boxer's model.

## Decision

Adopt **O1**: accept the **architecture posture and guardrails** below as binding on any future ImZero2 multi-app/embeddable client, and **defer the build** behind the gate in SD7. Nothing in this ADR authorizes shipping a compositor; it constrains what one would have to be.

### Subsidiary design decisions (the accepted posture)

- **SD1 — The browser client is a presentation compositor, never the enforcement boundary.** Once two levels' pixels share a tab/DOM/host page, the host, browser, extensions, and OS can read both. Every isolation claim therefore rests on SD3, not on client JavaScript. The compositor's security role is limited to **presentation and accidental-flow hygiene**.

- **SD2 — The topology is MILS, not in-kernel MLS.** boxer runs N independent single-level backends; the client composites them. The separation is **topological** — separate processes, backends, networks, and credentials — not a mandatory-access-control policy inside one shared kernel. This is a strength (the separation is physical) and it dictates where enforcement lives.

- **SD3 — Three layers; put the teeth where they bite.** (1) **Compositor** (client, untrusted): multiplexes windows; draws its **own** trusted chrome — level labels/borders **outside** each app's pixel rect, because an app paints its whole surface and could otherwise spoof an in-window banner; refuses to bridge clipboard across windows. (2) **Per-app session** ([ADR-0082](./0082-imzero2-remote-session-auth-tls.md) auth, per backend): each backend authenticates independently, and a user only holds tokens for the compartments they are cleared for — this is where "cannot see level X" actually lives. (3) **Policy + network + deployment**: which credential reaches which compartment, network separation between levels, and — for any real isolation claim — a **controlled client host**.

- **SD4 — Embeddability and isolation are deployment-trust tiers of one component.** "Embed in any webapp" places the host page in the trust boundary; isolation demands a trusted host. Ship the component once and deploy it in two tiers: **(i) untrusted-embed** = multi-app convenience, **no isolation claims**; **(ii) trusted-shell / kiosk** = compartmentalized, isolation only as strong as the controlled host + network. The security claim is a property of the **deployment**, not the component, and must be documented as such wherever the component is offered.

- **SD5 — The compositor reuses ADR-0086's active/passive model per backend.** A focused window is the **active** session (video + input) on its backend; unfocused windows are **passive** (read-only). Focus changes which backend is driven. This is why ADR-0086 is the substrate: a multi-window client is affordable only because unfocused windows are the cheap passive tier.

- **SD6 — Clipboard inverts under compartmentalization.** ADR-0082 SD6 designed clipboard *sync*; a compartmented client needs per-compartment **isolation** (no copy-from-high → paste-to-low). `navigator.clipboard` is page-global, so the trusted-shell tier (SD4-ii) must lock it down beyond what the compositor alone can promise. The untrusted-embed tier makes no clipboard-isolation claim.

- **SD7 — The gate.** The build is deferred until a concrete need fixes **both** the **security bar** — *compartmentalization-convenience* (server-enforced "you only get tokens you are cleared for", visual separation, clipboard isolation, best-effort trusted chrome in a controlled host) versus *accreditable MILS* (trusted client environment, network guards, covert-channel treatment, formal policy) — and the **host environment** (embed-anywhere vs trusted-shell). Accreditable MILS is a separate program for which this design is the **presentation layer, not the enforcement**.

- **SD8 — Web-component packaging is the lowest-risk first slice if/when the gate trips.** A custom element with Shadow DOM, connection/caps via attributes, and role/roster events. Two consequences to carry forward: it **reverses ADR-0024's "self-contained single-file viewer, no bundling step"** (an embeddable, versioned module wants a build target), and it moves **auth bootstrap to the host** (the host webapp provisions the token — aligning with ADR-0082's `PROXY_AUTH` front, and putting the host in the trust path, which is exactly the SD4 tension). Not built now.

- **SD9 — Out of scope / deferred.** The build itself; audio; a native client; and **multi-tenancy** (distinct identities / distinct people watching together) — a genuine multi-principal viewer re-opens the identity and broadcast questions deliberately, with the K8s/multi-tenancy phase, not by accident here.

## Alternatives

- **O2 — Build the compositor now, no isolation claims.** A general multi-app convenience. Rejected as the decision because it commits engineering to an undecided shape (SD7) and risks the embed-vs-isolation conflation (SD4); the *capability* is available the moment the gate trips, on ADR-0086's substrate.
- **O3 — Accreditable multi-level security enforced in the browser.** Rejected outright: a browser tab is not a reference monitor (SD1). Recorded as the kill-reason so the goal is pursued — if ever — as the separate program named in SD7, with the browser client as its presentation layer only.
- **O4 — One fat remote desktop running many apps.** Rejected: it streams a single composite from one shared host, discarding the per-backend topological separation (SD2) that makes boxer's compartmentalization real and shrinking the trusted computing base argument to one machine.

## Consequences

### Positive

- **Names and deters the security-theater failure mode** (SD1/O3): no future work can quietly treat the browser as an enforcement boundary, because the posture is on record.
- **Preserves boxer's topological separation** (SD2) as a genuine compartmentalization substrate, and keeps the load-bearing distinctions (MILS-not-MLS, compositor-labels / network-enforces, deployment-trust tiers) as institutional memory.
- **Names the gate** (SD7), so the compositor is neither built prematurely nor started by accident, and the right question — which security bar — is asked before any engineering.
- **Reuses ADR-0086 per backend** (SD5): when the gate trips, the multi-window client is mostly composition over an existing tier.

### Negative

- **Accepting a deferral ships nothing** from this ADR: there is no compositor until the gate trips.
- **One component, two security postures** (SD4): the same artifact is a no-claims convenience in one deployment and a compartmentalized client in another. This must be communicated carefully wherever it is offered, or it will be misread.
- **Clipboard isolation (SD6) is a future obligation** on ADR-0082 SD6's design, not satisfied here.

### Neutral

- **The MILS framing** is a description of the topology, not a certification.
- **The build, when it happens, leads with the web component (SD8)**, accepting the reversal of ADR-0024's single-file-viewer property and the move of auth bootstrap to the host.

### Derived practices

- **"The browser client labels; the network and backends enforce"** becomes the default stance for any future multi-level boxer client.
- **Deployment-trust tiers are stated explicitly, never implied** (SD4): a client's isolation claim is always qualified by the deployment it is claimed in.

## Status

Accepted — 2026-06-14, as a **posture plus a gated deferral** (SD7): the architecture and guardrails are in force; the implementation is not authorized by this ADR and awaits the gate. A build, when gated in, is expected to begin with SD8 (the web component) on ADR-0086's substrate.

Status lifecycle: `proposed → accepted → (deferred | deprecated | superseded by ADR-XXXX)`.

## References

- [ADR-0086 — ImZero2 active/passive remote viewers and the session roster](./0086-imzero2-active-passive-viewers-and-roster.md) — the per-backend active/passive substrate this compositor would reuse (SD5).
- [ADR-0082 — Securing the ImZero2 remote session](./0082-imzero2-remote-session-auth-tls.md) — per-backend auth (SD3 layer 2); clipboard sync that inverts to isolation here (SD6).
- [ADR-0024 — ImZero2 remote access via headless render + ffmpeg + browser viewer](./0024-imzero2-remote-access-browser-viewer.md) — the self-contained single-file viewer property that web-component packaging reverses (SD8).
- [ADR-0077 — keelson browser-wasm execution](./0077-keelson-browser-wasm-execution.md) — the locked-down enterprise-fleet deployment class motivating the fleet-reachability criterion.
- [ADR-0081 — ImZero2 headless RDP EGFX head](./0081-imzero2-headless-rdp-egfx-head.md) — withdrawn; the single-unprivileged-binary value the trusted-shell tier should not casually discard.
