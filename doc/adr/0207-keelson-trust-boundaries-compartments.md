---
type: adr
status: proposed
date: 2026-08-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0207: Trust boundaries for keelson apps — a compartment per boundary, a KVM guest per compartment, a trusted compositor on the seat

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) built the app runtime
on a stated threat model: *hygiene, not security*. Apps are first-party Go
packages compiled into one keelson host; capabilities are subject filters the
in-process bus checks; the capslock gate catches transitive escapes at lint
time. Its §Alternatives rejected process isolation (O4) with an explicit
trigger — "revisit only if a future requirement introduces untrusted
third-party apps". This ADR is that revisit. The requirement now on the table:
**each app belongs to a trust boundary, in the sense a web browser gives the
word, and apps in different boundaries are isolated from each other by the
strongest mechanism a workstation offers.**

### What the browser analogy does and does not say

The browser's unit of isolation is the *site*, not the page. Two pages of one
site share a renderer process and are not isolated from each other; the
boundary runs *between* sites. The browser process is trusted: it owns the
window, draws the chrome a page cannot paint over, brokers every resource a
renderer asks for, and routes input to the focused renderer only. The
enforcement is a sandboxed process on a shared kernel — Chromium's Linux
renderer runs under seccomp-bpf and namespaces — and the browser accepts the
residual: a kernel privilege escalation crosses it, and transient-execution
attacks are handled by giving each site its own process rather than by the
sandbox. Two of these three properties carry over unchanged (the unit is a
group of apps; a trusted process owns chrome, brokers, and input). The third —
what enforces the boundary — is where the requirement asks for more than a
browser has.

### What "strong, state-of-the-art" isolation means on a Linux workstation

Three mechanism tiers are in use, and they differ in what stays shared
between the two sides:

- **Software fault isolation** — a WebAssembly sandbox (wasmtime, wazero),
  RLBox in Firefox. Bounds memory and nothing else: the runtime documents
  Spectre-class leakage as partially mitigated, and the published attacks
  break out of or poison a wasm sandbox at a cost of 3–240 % to fix
  deterministically. One process, one kernel, one set of cores.
- **A sandboxed process on a shared kernel** — user namespaces + seccomp-bpf
  (Chromium's renderer, bubblewrap, Flatpak's portals), now with Landlock for
  unprivileged filesystem, TCP-port and signal/socket scoping. Confines a bug
  in the app; concedes the kernel — Chromium's own design doc lists a kernel
  bug as out of scope — and says nothing about hardware side channels. The
  browser stops here because a VM per site is unaffordable in RAM and
  latency and needs compositor plumbing a browser lacks; Microsoft's
  Hyper-V-backed Application Guard for the browser was deprecated in 2023 for
  the same economics.
- **Hardware virtualization** — a KVM guest per boundary, with a
  deliberately small device model. The microVM class (Firecracker, Cloud
  Hypervisor, QEMU's `microvm`, crosvm) exposes virtio net / block / vsock,
  runs the VMM under seccomp and a jailer, treats vCPU threads as hostile,
  and adds on the order of 5 MiB of VMM overhead plus a guest kernel; cold
  start is 100 ms–1 s, snapshot restore tens of ms (a snapshot may be
  restored once, for entropy). gVisor sits between the tiers — a user-space
  kernel, not a VM — and states it does not address side channels. Between
  guests the mitigation of record for transient execution is a *deployment*
  setting: core scheduling or SMT off, and L1D flush on VM entry, at 1–50 %.
  Confidential computing (SEV-SNP, TDX, CCA) is the tier above this one and
  protects a guest *from the host*; for a seat whose compositor must read every
  framebuffer, it adds cost and nothing else.

The desktop that runs entirely on the third tier is **Qubes OS**: one qube per
trust domain, a GUI domain that composites guest framebuffers shared through
hypervisor grants, a coloured frame and a `[name]` title prefix the guest
cannot draw over, input delivered only to the focused qube, a two-step
clipboard through a global buffer that clears after one paste, and every
cross-qube RPC as a named service under a host-side allow / deny / ask policy.
Its costs are the ones to expect: ~400 MB per qube before the app, CPU
rendering in guests because no GPU is shared, and Xen rather than KVM for a
smaller trusted base. ChromeOS's crosvm is the other reference and the one
with a GPU story — virtio-gpu with Venus and a cross-domain Wayland proxy —
at the price of the host's Vulkan stack becoming shared attack surface. What
every system on this tier agrees on: the untrusted side produces pixels, the
trusted compositor owns the frame and the focus, the guest-to-compositor
messages are the security-critical parse surface, and clipboard / files cross
only as explicit policy-gated RPC. Sources in §References.

### What is already in the tree

The shape proposed below is mostly composition. The pieces, and the record for
each:

- **A keelson host is already a process behind a wire.** One Go process drives
  one Rust renderer over FFFI2 on pipes; the headless hosts ship pixels
  (ADR-0024, ADR-0205) or tessellated meshes (ADR-0128) over one WebSocket and
  accept input back. Nothing in the Go side knows which host loop it got
  ([ARCHITECTURE §1.1](../ARCHITECTURE.md#11-what-a-mode-is-made-of)).
- **The host boots as a guest today.** [ADR-0206](./0206-gokrazy-appliance-image.md)
  builds a gokrazy image — Go host, `headless_soft` Rust host, optionally a
  ClickHouse — and boots it under QEMU. A compartment is that image with a
  different transport.
- **Capabilities are subject filters, and the transport that enforces them
  server-side exists.** `natsbus` defers authorization to the NATS server
  (ADR-0026 §SD4); provisioning of the per-app NKey/JWT has not landed.
- **The file dialog is already a Powerbox over the bus.** ADR-0026 §SD7's
  `fs.dialog.*` / `fs.handle.{uuid}.>` moves bytes through the bus, and the
  2026-08-08 Update removed the standing `fs.handle.>` grants — the dynamic,
  per-file grant is the whole story.
- **The compositor posture is decided.** [ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md)
  accepted, for the *browser* client, that the compositor labels and the
  backends enforce; that the topology is MILS; that trusted chrome lives
  outside the app's pixel rect; that clipboard inverts to isolation; and it
  deferred the build behind a gate asking which security bar and which host.
  [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) gives the
  one-active / N-passive input model per backend.
- **Launch and clipboard have brokers.** `windowhost.open`
  ([ADR-0135](./0135-app-launch-requests.md)) is an audited request/reply
  subject; `clipboardbroker` mediates the clipboard in-process.
- **A sensitivity label exists for a run** ([ADR-0145 §SD3](./0145-sealed-app-data.md)),
  derived rather than declared — the same authoring rule this ADR applies to a
  compartment.

What is absent: the concept of a boundary; a compositor that takes N sources;
a transport for the frame lanes other than pipes and WebSocket; per-boundary
authorization on the bus; and any statement about what an app that has
compromised its own host can reach.

## Design space (QOC)

**Question.** How does a keelson app acquire a trust boundary, and what
enforces it?

**Options.**

- **O1 — One process, labelled compartments.** Keep ADR-0026's single host;
  group apps into named compartments and show the name in chrome. Nothing
  enforces.
- **O2 — A host process per compartment on the shared kernel.** One keelson
  host (Go + Rust) per compartment, confined by user namespaces, seccomp-bpf
  and Landlock (the bubblewrap / Chromium shape); a trusted compositor on the
  seat composes their output.
- **O3 — A KVM guest per compartment, a trusted compositor on the seat
  (chosen).** One keelson host per compartment booted from the ADR-0206 image
  inside a KVM microVM; frames, input, bus and file bytes cross the boundary
  over virtio; the same compositor as O2.
- **O4 — A wasm guest per app inside one host.** ADR-0077's "prospective
  untrusted-app sandbox": each app compiled to wasm and run under wazero,
  capabilities as host imports.
- **O5 — Compartments as remote backends only.** ADR-0087 as it stands: each
  boundary is a separate machine, composed in the browser viewer.

**Criteria.**

- **C1 — Boundary strength.** What an app that fully controls its own
  compartment can still reach: another compartment's memory, kernel, pixels,
  input, data; side channels.
- **C2 — Trusted side.** Size of the trusted computing base and, in
  particular, the parser surface the trusted side exposes to a compartment.
- **C3 — Reuse.** How much of the tree (image, hosts, lanes, bus, brokers,
  posture) the option composes rather than replaces.
- **C4 — Per-compartment cost**, and whether it can be measured before the
  design commits: RAM, start latency, per-frame CPU.
- **C5 — One model, several deployments.** Whether a compartment on the seat,
  a compartment on another machine, and a cheaper fallback speak one protocol.
- **C6 — GUI fidelity.** Text crisp at the seat's DPR; input-to-photon latency.
- **C7 — Honesty of the claim** (ADR-0026 C7, ADR-0087 C1).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (labels) | O2 (process) | O3 (KVM guest) | O4 (wasm) | O5 (remote only) |
|----|-------------|--------------|----------------|-----------|------------------|
| C1 | −−          | +            | ++             | −         | ++               |
| C2 | ~           | +            | +              | −         | +                |
| C3 | ++          | +            | ++             | −−        | +                |
| C4 | ++          | +            | −              | ~         | −−               |
| C5 | −           | ++           | ++             | −         | −                |
| C6 | ++          | +            | +              | ~         | −                |
| C7 | −−          | +            | ++             | −         | ++               |

O1 is the isolation theatre ADR-0087 exists to name. O4 isolates memory and
nothing else — no kernel between guests to speak of, but also no answer to
side channels — and pays the Go-on-wasm tax ADR-0077 §Negative records, per
app rather than per compartment. O5 is the strongest boundary but puts no
boundary on a seat, which is the requirement. O2 and O3 share a compositor,
a protocol and a set of brokers and differ in one thing: whether the kernel is
shared. That is the property the requirement asks for, so O3 is the target and
O2 is kept as the named fallback tier of the same design (§SD9), not as a
rejected option.

## Decision

We will introduce **compartments** as the unit of trust in keelson. A
compartment is a keelson host behind a transport; on a seat it is a **KVM
microVM** booted from the ADR-0206 appliance image; a **trusted compositor**
on the seat owns the display, chrome, focus, and the brokers that see both
sides. Inside a compartment, ADR-0026's model stands unchanged. Between
compartments, the boundary is the hypervisor.

### Subsidiary design decisions

- **SD1 — The unit is the compartment; apps do not choose theirs.** A compartment is a host-policy object: a name, a colour, a set of
  app-id patterns, an image variant, a data backend (§SD8), a bus account
  (§SD5), an isolation tier (§SD9). The policy lives with the compositor, never
  in a `Manifest` — a manifest field would let an app pick its own trust, the
  mistake the derived-not-declared rule of ADR-0145 §SD3 already avoids. An app
  no pattern covers launches in the compartment the policy names as default, or
  is refused; the policy says which. Within a compartment, apps share one
  windowhost, one in-process bus, one facts table, and the hygiene model of
  ADR-0026 — as two pages of one site share a renderer.

- **SD2 — A compartment is a keelson host behind a transport.** The guest is
  the ADR-0206 image: Go host, `headless_soft` Rust host, and the ClickHouse
  variant when §SD8 places data in-guest. On a seat the transport is virtio
  (vsock, or virtio-net on a host-only link); on another machine it is the
  network, which is ADR-0087's topology unchanged; under §SD9's fallback it is
  a socketpair. The guest does not know which. What crosses the transport is
  §SD3–§SD7 and nothing else; in particular, no host filesystem, no host
  display, no host network beyond the compositor's services.

- **SD3 — Pixels cross the boundary; FFFI2 never does.** An FFFI2 opcode
  stream programs a full egui interpreter in its receiver: opcodes allocate
  rects anywhere in the viewport and some name host-side files (ADR-0056 §SD14's
  CA bundle path was one). A guest emitting FFFI2 into the seat's renderer
  would be driving the trusted UI. The guest therefore rasterizes at the DPR the
  compositor announces and ships **BGRA damage rectangles**; the compositor
  blits them into the compartment's window, clipped to its rect. The trusted
  side parses a rectangle header. The mesh lane (ADR-0128) is the upgrade —
  text crisp at any DPR, ~20 kbit/s idle — and is admitted only once its wire
  format is bounded and fuzzed as adversarial input, since the compositor's
  mesh parser would then be fed by the least-trusted party. The video lane
  (ADR-0024) is not used on a seat: a codec is the largest parser of the three
  and the encode is CPU spent on a local link.

- **SD4 — The compositor is the seat's keelson host, and it is trusted.** The
  seat runs an ordinary desktop keelson (Go host + Rust desktop renderer). A
  compartment appears there as a **compartment window**: windowhost draws the
  chrome — colour and name, outside the pixel rect (ADR-0087 §SD3) — and the
  Go side owns policy, focus and input routing (one active compartment
  receives input, the rest are passive, ADR-0086's model per backend), while
  the guest's frames enter the Rust renderer directly over the transport as a
  texture rather than round-tripping through the Go host and the FFFI2 pipe.
  The trusted computing base is the host OS, KVM and the VMM, the seat's
  keelson host and renderer, the NATS server, and the host-side brokers. This
  is the shape Qubes gives its GUI domain — it sees every compartment; no
  compartment sees another — and it is ADR-0087's "controlled client host"
  built natively. The browser viewer remains the remote presentation tier and
  gains no isolation claim it did not have.

- **SD5 — The bus crosses as NATS accounts, one per compartment.** ADR-0026 §SD4 decided an external server with per-app
  NKey/JWT; accounts are the server's hard namespace, and traffic crosses
  accounts only through explicit exports and imports. The compartment policy
  compiles into the account configuration: which subjects a compartment
  exports, which it imports, under `compartment.{name}.>` on the importing
  side. The server runs on the trusted side and is the one service every guest
  may reach. `inprocbus` remains the intra-compartment bus; `natsbus` is the
  guest's client to the server, and its deferred-to-server authorization is
  now load-bearing, so per-app JWT provisioning lands with this ADR's M2. This
  is the role Qubes gives qrexec policy: every cross-compartment RPC is a named
  service under a host-side rule.

- **SD6 — The Powerbox stays on the trusted side.** The file dialog is drawn by
  the compositor, so it cannot be spoofed by a compartment; the resulting
  `fs.handle.{uuid}.>` grant is imported into the requesting compartment's
  account and the handle serves bytes across the boundary exactly as it serves
  them in-process today. Guests have no host filesystem; the image's `/perm` is
  theirs and theirs alone.

- **SD7 — Clipboard is compartment-local; crossing is a user action.** Copy and paste inside a compartment never leave it. Two compositor
  actions move data across — *copy to global* from the focused compartment,
  *paste from global* into the focused compartment — one shot, cleared after
  paste, written to the audit facts. This is the inversion ADR-0087 §SD6 named
  as a future obligation, and the Qubes clipboard model. `clipboardbroker`
  gains the trusted-side half; the in-compartment half is unchanged.

- **SD8 — Data follows MILS: a ClickHouse per compartment, in the guest.** The
  compartment's `boxer.facts` and everything else it persists live in the
  ADR-0206 ClickHouse variant inside its own guest. ADR-0026's data-centricity
  (C6) inverts at the boundary, deliberately: another compartment's data is
  reachable only through a subject it exports (§SD5), never through a shared
  server. A shared server with per-compartment databases and credentials is a
  **named weaker tier** — one process, one kernel, a server bug crosses — that
  policy may select and chrome must show as such.

- **SD9 — The fallback tier is the same protocol in a sandboxed process.** A
  compartment whose policy says `isolation: process` runs the same guest
  binaries as a host-OS process under user namespaces, seccomp-bpf and
  Landlock, speaking §SD3–§SD7 over a socketpair. It does not defend against a
  kernel privilege escalation or shared-kernel side channels, and its chrome
  says so. It exists so that the protocol never depends on the VM and the VM
  stays what ADR-0087 §SD4 says isolation is: a property of the deployment.

- **SD10 — The VMM is an external process, chosen after a measured probe.**
  The Go host stays CGO-free, so a library VMM linked into the process is out;
  the VMM is a binary behind the `extbin` chokepoint
  ([ADR-0118](./0118-extbin-external-process-chokepoint.md)). Candidates are
  the microVM class — Firecracker, Cloud Hypervisor, QEMU's `microvm` machine
  — and the choice follows M0, a trial under `doc/trials/` measuring per
  compartment: RSS after first frame, cold start to first frame, snapshot
  restore to first frame, and per-frame cost of the pixel lane. This ADR quotes
  none of those numbers; the trial's §0 will. Two decisions hang on them:
  whether snapshot/restore is required for acceptable launch latency, and
  whether the in-guest ClickHouse (§SD8) can be the default or must be the
  exception.

- **SD11 — No GPU in a guest, by construction.** `headless_soft` rasterizes on
  the CPU (ADR-0205), so a compartment needs no GPU and the design needs no
  shared-GPU trust story. The compositor uses the seat's GPU for its own pass.

- **SD12 — Out of scope, recorded.** Side-channel hardening beyond what the
  kernel and hypervisor ship (core scheduling, L1D flushing are deployment
  settings, named in the howto, not built); audio; device passthrough;
  confidential computing (it protects a guest *from the host*, and here the
  host is the trusted side by design); and assurance. This ADR makes
  separation **enforced** — by a hypervisor — and claims nothing about
  separation **assured**: no evaluated trusted base (the seat is a stock
  Linux), no covert-channel analysis, no formal policy model. That assurance
  program is what ADR-0087 §SD7 called *accreditable MILS*, and it stays
  separate; the distinction 0087 drew as *convenience* versus *accreditable*
  is spelled *enforced* versus *assured* here, because "convenience"
  described a browser tab and undersells a hypervisor.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Frame lanes (ADR-0024 / ADR-0128 carriers) | added: a pixel lane over vsock / socketpair — damage rectangles, DPR announcement, input back | the `headless_soft` carrier gains the transport; the seat renderer gains a compartment-surface primitive in the egui2 IDL (regenerated) |
| Capability subjects (ADR-0026 §SD3) | added: `compartment.{name}.>` as the imported form of an exported subject; `windowhost.open` forwarded across accounts | `natsbus` JWT provisioning; the capslock cross-checker's subject table |
| `boxer.facts` vocabulary | added: compartment policy; audit kinds for clipboard moves, cross-compartment launches, file grants | the facts codegen; `keelson('…')` introspection tables |
| Environment registry (ADR-0009) | added: compositor transport and policy location | `doc/env-vars.md` |
| `Manifest` | **unchanged** — no compartment field (§SD1) | — |

## Alternatives

- **O1 — labels without enforcement.** Rejected: the isolation theatre
  ADR-0087 §SD1 names; a label a compartment could earn by asking is worse
  than none.
- **O2 as the primary boundary.** Not rejected — demoted to the fallback tier
  (§SD9). As the primary it shares the kernel with the thing it confines,
  which is the residual the requirement asked to remove.
- **O4 — wasm per app.** Rejected as the boundary: software fault isolation
  bounds memory and nothing else, offers no side-channel story, and costs
  ADR-0077's wasm tax per app; the per-compartment grain also fits the browser
  analogy and it does not.
- **O5 alone.** Rejected as the answer: it is the design's remote deployment,
  not a boundary on a seat.
- **FFFI2 across the boundary.** Rejected in §SD3: it hands the trusted
  renderer's interpreter to the least-trusted party.
- **The video lane on a seat.** Rejected in §SD3: the largest parser, and an
  encode the local link does not need.
- **A library VMM (libkrun-class) in the Go host.** Rejected in §SD10: it
  breaks the CGO-free invariant and folds the VMM into the process it is meant
  to keep at arm's length.
- **A compartment field on `Manifest`.** Rejected in §SD1.
- **A Flatpak-shaped boundary.** Developer-declared permissions
  (`finish-args`-style) enforced by user namespaces and seccomp, portals for
  files. Its sandbox is O2's, so it is the §SD9 tier and not the boundary; its
  authoring rule — the app declares, the user accepts at install — is the one
  §SD1 refuses; and its declared holes (`--filesystem=home`,
  `--share=network`, `--socket=x11`) have no counterpart here, because nothing
  a compartment can declare reaches the seat. Its portal model *is* §SD6's
  Powerbox, borrowed.

## Consequences

### Positive

- The boundary is a hypervisor, and the claim can be stated without
  qualification about the kernel or the compositor's JavaScript — the two
  qualifications ADR-0026 C7 and ADR-0087 SD1 had to make.
- Almost everything is composition: the image (ADR-0206), the CPU rasterizer
  (ADR-0205), the bus and its server-side authorization (ADR-0026 §SD4), the
  Powerbox (§SD7), the compositor posture (ADR-0087), the input model
  (ADR-0086). The new code is a transport, a compositor window, account
  provisioning, and the clipboard's trusted half.
- One protocol serves the seat, a remote machine and the fallback tier, so a
  compartment's isolation is a deployment choice the chrome reports.
- Cross-compartment flows — launches, file grants, clipboard moves — become
  audited facts on the trusted side, which is more than the single-process
  model could record.

### Negative

- A compartment costs a guest kernel, a Go host, a Rust host and possibly a
  ClickHouse; RAM and start latency per compartment are measured in M0, not
  known now, and may force snapshot/restore and a shared-data tier sooner than
  wanted.
- The trusted side grows a parser fed by adversarial input (the pixel lane's
  header, later the mesh lane), and a policy compiler for NATS accounts. Both
  need the fuzz and negative-test treatment the tree gives leeway decoders.
- Data-centricity across compartments is gone on purpose; `play` in one
  compartment cannot browse another's facts unless that compartment exports
  them. This is the point, and it will feel like a regression.
- Two new operational dependencies on the seat: a VMM binary and a NATS
  server. Neither is embedded (ADR-0026 §SD4, §SD10).

### Neutral

- Inside a compartment nothing changes: ADR-0026 remains the model, and
  today's single-process deployment is the degenerate case of one compartment.
- The browser viewer's posture (ADR-0087) is untouched; it is the remote
  presentation of the same compartments.

## Migration — Tier 1

- **Breaks.** Nothing. Every surface above is additive; a deployment with no
  compartment policy is one compartment and behaves as today.
- **Path.** Nothing to migrate. When accepted, ADR-0026 gains a dated Update
  scoping its threat-model sentence to *within a compartment*, and ADR-0087
  gains one recording that its §SD7 gate tripped with this answer to the
  security-bar question.
- **Regeneration.** The egui2 IDL for the compartment-surface primitive; the
  facts codegen for the new vocabulary kinds; both sides of the FFFI2 boundary
  rebuilt for the seat renderer.
- **Old shape.** Kept indefinitely — the single-process host is a compartment.

## Verification plan — Tier 1

- **Lane.** The `//go:build integration` lane, booting two compartments under
  the chosen VMM (QEMU where the runner has no `/dev/kvm`) with a compositor in
  headless mode; plus the M0 trial for cost.
- **What would fail.** A guest publishing on another account's subject
  receives a permission error, not delivery; a copy in one compartment is not
  visible to a paste in another until the trusted-chrome action; input events
  reach only the active compartment; a frame rectangle outside the window rect
  is clipped, not drawn.
- **Gap.** Side channels between guests are not tested; the trial measures
  cost, not leakage. A kernel or hypervisor escape is out of the repository's
  reach to test and is the residual the design accepts.

### Milestones

- **M0 — The trial.** Two ADR-0206 guests under Firecracker / Cloud Hypervisor
  / QEMU-microvm; the four numbers of §SD10.
- **M1 — Pixel lane and compartment window.** The lane over vsock and a
  compartment window in the seat host: two guests, trusted chrome, focus routing.
- **M2 — Accounts and brokers.** NATS accounts from policy, per-app JWT
  provisioning, the Powerbox and `windowhost.open` across accounts.
- **M3 — Clipboard.** The clipboard's trusted half and its audit facts.
- **M4 — Process tier.** The process-isolation tier (§SD9) over a socketpair.
- **M5 — Snapshot/restore.** Only if M0 says launch latency needs it — one
  restore per snapshot (the entropy caveat), and vsock reconnect after restore.

## Status

Proposed — awaiting review by the code owner. Opened 2026-08-27 as the revisit
ADR-0026 §Alternatives (O4) named.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0026 — App runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md) — the hygiene threat model this ADR scopes to *within a compartment*; §SD4 external NATS; §SD7 Powerbox; §Alternatives O4 and its revisit trigger.
- [ADR-0087 — ImZero2 browser client architecture and compartmentalization posture](./0087-imzero2-client-compositor-compartmentalization.md) — the compositor posture, MILS topology, trusted chrome, clipboard inversion, and the §SD7 gate.
- [ADR-0086 — Active/passive remote viewers and the roster](./0086-imzero2-active-passive-viewers-and-roster.md) — the input model reused per compartment.
- [ADR-0206 — gokrazy appliance image](./0206-gokrazy-appliance-image.md) — the guest.
- [ADR-0205 — CPU-rasterized pixel host](./0205-imzero2-cpu-rasterized-pixel-host.md) — why a guest needs no GPU.
- [ADR-0128 — Mesh draw-stream codec lane](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the deferred upgrade of §SD3.
- [ADR-0024 — Remote access via headless render](./0024-imzero2-remote-access-browser-viewer.md) — the pixel lanes' origin.
- [ADR-0077 — Keelson browser-wasm execution](./0077-keelson-browser-wasm-execution.md) — the wasm tax and the "prospective untrusted-app sandbox" that O4 would have been.
- [ADR-0135 — App-launch requests](./0135-app-launch-requests.md) — `windowhost.open`, forwarded across accounts in §SD5.
- [ADR-0145 — Sealed app data](./0145-sealed-app-data.md) — the derived-not-declared label rule §SD1 borrows.
- [ADR-0118 — extbin external-process chokepoint](./0118-extbin-external-process-chokepoint.md) — where the VMM binary is resolved.
- [ARCHITECTURE §1 — The stack at a glance](../ARCHITECTURE.md#1-the-stack-at-a-glance) — the four process kinds and the boundaries between them today.
External:

- [Firecracker design](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md), [specification](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md), [snapshot support](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md) — device model, jailer, overhead and latency targets, the restore-once caveat.
- [Cloud Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor); [QEMU `microvm`](https://www.qemu.org/docs/master/system/i386/microvm.html); [libkrun](https://github.com/libkrun/libkrun) — the library VMM §SD10 rejects for the CGO-free host.
- [crosvm architecture](https://crosvm.dev/book/architecture/overview.html), [Wayland / cross-domain](https://crosvm.dev/book/devices/wayland.html), [Venus](https://docs.mesa3d.org/drivers/venus.html) — GUI and GPU across a VM boundary, and the parse surface it costs.
- [gVisor security model](https://gvisor.dev/docs/architecture_guide/security/) and [platforms](https://gvisor.dev/docs/architecture_guide/platforms/).
- [Qubes OS architecture](https://doc.qubes-os.org/en/latest/developer/system/architecture.html), [GUI virtualization](https://doc.qubes-os.org/en/latest/developer/system/gui.html), [copy and paste](https://doc.qubes-os.org/en/latest/user/how-to-guides/how-to-copy-and-paste-text.html), [qrexec](https://doc.qubes-os.org/en/latest/developer/services/qrexec.html), [qmemman](https://doc.qubes-os.org/en/latest/developer/services/qmemman.html) — the model §SD4–§SD7 follow.
- [Chromium site isolation](https://www.chromium.org/Home/chromium-security/site-isolation/), [side-channel threat model](https://chromium.googlesource.com/chromium/src/+/main/docs/security/side-channel-threat-model.md), [Linux sandbox](https://chromium.googlesource.com/chromium/src/+/main/sandbox/linux/README.md), [sandbox design](https://chromium.googlesource.com/chromium/src/+/main/docs/design/sandbox.md) — the analogy, and where it stops.
- [Windows deprecated features](https://learn.microsoft.com/en-us/windows/whats-new/deprecated-features) — Application Guard's retirement.
- [Landlock](https://docs.kernel.org/userspace-api/landlock.html), [bubblewrap](https://github.com/containers/bubblewrap), [Flatpak sandbox permissions](https://docs.flatpak.org/en/latest/sandbox-permissions.html) — the §SD9 tier.
- [Wasmtime security](https://docs.wasmtime.dev/security.html), [Swivel](https://arxiv.org/abs/2102.12730), [RLBox](https://rlbox.dev/) — why O4 is memory isolation only.
- [Core scheduling](https://docs.kernel.org/admin-guide/hw-vuln/core-scheduling.html), [L1TF](https://docs.kernel.org/admin-guide/hw-vuln/l1tf.html) — the §SD12 deployment settings.
- [AMD SEV-SNP in KVM](https://docs.kernel.org/virt/kvm/x86/amd-memory-encryption.html), [Intel TDX](https://docs.kernel.org/arch/x86/tdx.html), [Arm CCA](https://www.arm.com/architecture/security-features/arm-confidential-compute-architecture) — the tier §SD12 leaves out, and why.
