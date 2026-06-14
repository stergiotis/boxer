---
type: adr
status: proposed
date: 2026-06-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0085: On-box pull-build-and-atomic-deploy for the imzero2 headless demo

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) shipped the headless imzero2 demo (offscreen render → ffmpeg H.264 → WebSocket carrier → browser viewer), and a deploy kit (`showcase/`) packages it for a single VPS: a runtime-only container built off-box and run behind Caddy, which supplies the TLS + password the v1 carrier lacks (auth/TLS are [ADR-0082](./0082-imzero2-remote-session-auth-tls-fanout.md), accepted but unbuilt). That kit is a *manual* deploy — build locally, ship the image, `compose up`.

This ADR covers a different operating mode requested for an internet-facing **demo box that updates itself**: when a new *tagged* release lands on boxer, the box should rebuild and cut over to it, unattended, atomically, with rollback — in a way that fits the sovereign-appliance posture the surrounding design favours (the box reaches out, nothing reaches in; it builds what it runs from source it can audit; no external registry or CI it must trust).

Constraints and inheritances:

- **The carrier is single-session** (one viewer); a brief restart is acceptable, and Caddy's `handle_errors` can serve a "build in progress" holding page over the cutover, so true zero-downtime is not required.
- **The build is heavy.** `build_rust_headless.sh` compiles a 663-crate wgpu/egui release; from-scratch is CPU+RAM+time-expensive, but incremental builds (preserved cargo `target/` + Go caches) are light. The box must be build-capable *and* keep its caches.
- **The pieces exist.** `hmi_headless.sh` already builds-then-runs; the build scripts and `ws_probe` (a WebSocket probe client) exist; the house libraries — `runinfo`, `inprocbus`, `imzero2env` ([ADR-0009](./0009-environment-variable-registry.md)), and the `chlocalpool` external-binary-by-path precedent — exist.
- **There is no in-tree deploy tool.** This is new ops tooling; per the project's design-first practice it is specified here before implementation.

The trust posture is load-bearing: the box is internet-exposed, so an unattended "pull a tag and execute a build" loop is a supply-chain surface. The decision must keep the pull outbound-only and make tag provenance the gate.

## Design space (QOC)

**Question.** How should an internet-facing imzero2 demo box update itself on a new boxer release tag — atomically, with rollback, and idiomatically — without an inbound deploy channel and without giving up the box's ability to build and audit what it runs?

The decision is a coherent posture bundling four axes: **trigger** (push vs pull), **build location** (on-box vs off-box), **packaging** (bare processes vs container image), and **cutover** (in-place vs immutable-releases swap vs blue-green). The options below are internally-consistent bundles; the chosen bundle's per-axis rationale is in the subsidiary decisions.

**Options.**

- **O1 — pull + on-box bare build + immutable-releases symlink swap + `ws_probe` gate + a boxer Go command (chosen).**
- **O2 — pull + off-box CI-built image + image-pull cutover** (watchtower/Flux). The box is dumb; CI builds, a registry stores the artifact.
- **O3 — push.** CI (GitHub Actions) builds and deploys into the box over SSH on tag.
- **O4 — pull + on-box build, in-place** (`git pull && hmi_headless.sh && restart`); no releases/symlink, no gate.

**Criteria.**

- **C1 — Atomic cutover + cheap rollback.** A half-built or non-streaming release never becomes live; reverting needs no rebuild.
- **C2 — Self-contained / sovereign.** No external registry, no CI the box must trust; an auditable source→running-binary chain on the box.
- **C3 — Outbound-only.** No inbound deploy channel into an internet-exposed box.
- **C4 — Build cost / box load.** Incremental builds; contention with serving.
- **C5 — boxer-idiomatic + observable.** A Go command on the house libs; the deploy trail in the same logging/bus as the app.
- **C6 — Operational simplicity.** Moving parts and failure modes.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (pull/on-box/bare/swap) | O2 (pull/off-box image) | O3 (push) | O4 (on-box in-place) |
|----|----------------------------|-------------------------|-----------|----------------------|
| C1 | ++ (gate + immutable swap + rollback) | ++ (image-tag pin = atomic + instant revert) | + (depends on CI scripting) | −− (a broken build can down the live demo; rollback = rebuild) |
| C2 | ++                          | −− (registry + CI; box runs an opaque image it didn't build) | − (CI builds and holds keys) | ++ |
| C3 | ++                          | ++ | −− (inbound SSH into prod) | ++ |
| C4 | − (box builds; caches mitigate) | ++ (no box build) | ++ | − (box builds) |
| C5 | ++                          | − (cutover is watchtower/external, not boxer) | − | + (could be a Go cmd) |
| C6 | +                           | + (but a registry + CI to run) | − (CI↔box coupling) | ++ (simplest) |

O1 wins on the balance: it is the only bundle simultaneously self-contained (C2), outbound-only (C3), atomic-with-rollback (C1), and idiomatic/observable (C5), at the cost of box-side build load (C4) that incremental caches and build-then-swap blunt. **O2 is *better* on box load** and is the right answer the moment self-containment stops being a requirement — a dumb box pulling a CI-built, registry-stored image; it is retained as the escape and the natural multi-box path. O4 is O1 minus the machinery that makes an unattended loop safe, and the machinery is the point. O3 is rejected on C3 — an inbound deploy channel into an internet-exposed box is exactly what the pull posture exists to remove.

## Decision

We adopt **O1**. Concretely:

- The box **polls boxer's tags outbound** and, on a newer release tag than the one running, fetches and **builds on-box from source** in a cached workspace (incremental), so it builds and can audit exactly what it runs.
- Cutover is **atomic via an immutable-`releases/` + `current` symlink swap**, gated by a real streaming check (`ws_probe`) before the swap, with **keep-last-K rollback**; the brief `systemctl restart` is masked by Caddy's holding page.
- The logic is a **boxer Go `deploy` subcommand** on the house libraries; **systemd** supplies supervision and the poll timer.

### Subsidiary design decisions

- **SD1 — Pull trigger, outbound-only.** A systemd timer fires the deploy command; it runs `git ls-remote --tags` (or fetch) and compares the newest release tag to the tag `current` points at. New → deploy; else exit. No webhook, no inbound port — the internet-exposed box never accepts a deploy connection. Tags, not every commit, are the deliberate publish gesture; the poll interval trades deploy latency for quiet.

- **SD2 — Two layers: systemd (OS) + a Go command (logic).** Supervision (`imzero2-demo.service` running `current/`) and scheduling (`imzero2-deploy.timer` + a oneshot `.service`) are systemd's job and are not reimplemented; journald is the OS-level log sink. The pull/build/gate/swap/rollback *logic* is a `deploy` subcommand of the thestack app — a Go program, not a shell script — so it logs, errors, and configures like the rest of boxer.

- **SD3 — Immutable releases beside a mutable build workspace.** `workspace/` holds the persistent checkout + cargo `target/` + Go caches and is where builds run (incremental). On success the *artifacts only* — `main_go`, the headless `imzero2`, `ws_probe`, `assets/` — are snapshotted into `releases/<tag>/`, which is never mutated after creation. Fast builds (shared cache) with immutable, swappable deployable units.

- **SD4 — Atomic cutover by symlink rename.** `current` is a symlink the service `ExecStart`s through. Cutover writes a temp symlink to the new `releases/<tag>` and `rename()`s it over `current` (POSIX-atomic), then `systemctl restart imzero2-demo`. The render loop is single-session, so the ~1 s restart is acceptable and is covered by Caddy `handle_errors` serving a static holding page whenever the upstream is momentarily down. Blue-green (two services, Caddy upstream switch) for true zero-downtime is deferred (SD9).

- **SD5 — Gate before swap, with the app's own probe.** A candidate release is started on a scratch port and **`ws_probe`** connects and asserts a frame decodes — the same end-to-end path a real viewer exercises, stronger than fetching the page. A failed build *or* a failed gate aborts: `current` is untouched, the candidate is discarded, the failure is logged and emitted (SD7). The live demo never sees a release that did not build *and* stream.

- **SD6 — Rollback and retention.** `releases/` retains the last K (env-configurable). After the restart the command re-probes the live port; failure repoints `current` to the previous release and restarts — a rollback with **no rebuild**. Releases beyond K are pruned.

- **SD7 — boxer-idiomatic surface.** urfave/cli subcommand; zerolog (`--logFormat`/`--logLevel`, run_id fields); `eh`/`eb` structured errors. **All knobs register in the [ADR-0009](./0009-environment-variable-registry.md) env registry** (`imzero2env`) — repo, poll cadence, `workspace`/`releases` roots, keep-K, scratch port, gate timeout, the `LAUNCH`/encoder env handed to the service — so they document themselves in `doc/env-vars.md`. **`runinfo` is the deployed-revision source of truth**: the command checks the workspace out at the tag's commit, so the running demo's `runinfo` boot line (`vcs_revision`) and the `current` tag agree by construction — no separate state file. Each deploy is optionally emitted as an **audited `inprocbus` `Request`** (the audited path; `Publish` is not) carrying tag, revision, build/gate durations, and verdict — putting the deploy trail in the same observability plane as the app.

- **SD8 — Tag provenance is the trust gate.** The box authenticates to the repo **read-only** (a deploy key or a public mirror); it never holds write/push credentials. For the hostile, internet-exposed target the command **verifies the tag's signature before building** (GPG/SSH-signed tags), so a compromised mirror or a forged ref cannot make the box build arbitrary code — matching [ADR-0082](./0082-imzero2-remote-session-auth-tls-fanout.md)'s fail-closed, secure-by-default ethos. Unsigned-tag acceptance is a loopback/dev convenience, not the internet default.

- **SD9 — Out of scope / deferred.** Blue-green zero-downtime cutover (the single-session demo does not need it; the escalation if an always-live SLA appears). Multi-box fleet coordination (this is one box; the moment there are several, O2's CI-image-pull is the better substrate). Test-gating beyond the smoke probe — CI already runs the suite on the tag, so the box trusts the tag and only verifies that *this* build streams. The off-box-CI-image path (O2) itself, retained as the escape for a dumb box.

## Alternatives

- **O2 — off-box CI image + image-pull (watchtower/Flux).** The box stays dumb; CI builds the image on tag, a registry stores it, the box pulls and recreates the container. Strictly better on box load and the right answer for a fleet or any box that should not build. Rejected as the *baseline* only because it forfeits C2: a registry and a CI pipeline the box must trust enter the chain, and the box runs an image it did not build from source it can audit — the property the sovereign-appliance posture values. Held as the escape and the multi-box path; the existing `showcase/` container kit is most of its artifact already.
- **O3 — push (CI SSHes into the box).** Rejected on C3: an inbound deploy channel and standing credentials into an internet-exposed box is the exposure the pull model removes.
- **O4 — on-box in-place build.** `git pull && hmi_headless.sh && restart`. Rejected: a failing or non-streaming build can tear down the live demo, half-built state can run, and rollback means rebuilding the previous tag. The immutable-releases + gate machinery (SD3–SD6) is precisely what makes an *unattended* loop safe.
- **A bash script instead of a Go command (for the logic).** Adequate to the mechanics, and the SD2 systemd layer is shell anyway. Rejected for the *logic* because it would not log, error, configure, or emit observability the boxer way — the ask was idiomatic, and an internet-exposed auto-deployer is exactly where structured, audited, configurable behaviour earns its place.
- **Webhook trigger instead of polling.** Lower latency than a poll, but a webhook is an inbound channel against the outbound-only posture; a few-minute poll suits a demo's release cadence.

## Consequences

### Positive

- **Unattended *safe* deploy on a release tag:** a release that does not build and stream never goes live (SD5), cutover is atomic (SD4), rollback needs no rebuild (SD6).
- **Outbound-only and self-contained:** the box reaches out, nothing reaches in (SD1), and it builds and can audit exactly what it runs (SD3) — the sovereign-appliance posture end to end.
- **Idiomatic and observable:** the deployer logs, errors, and configures like the app; the deployed revision is a first-class `runinfo` fact; the deploy trail can ride the audited bus (SD7) — closed-loop on its own updates.
- **The restart blip is invisible:** Caddy's holding page absorbs the cutover (SD4), and on incremental builds the *old* release serves throughout the build (build-then-swap), so viewers see nothing until the brief swap.

### Negative

- **The box must be build-capable.** On-box compilation of a 663-crate wgpu release wants a build-sized box (≈4–8 cores, 8–16 GB, ≥80 GB disk, persistent caches), not a 2-vCPU streamer; the cold build is heavy and contends with serving (mitigated by incremental caches + build-then-swap, not eliminated).
- **A toolchain on an internet-exposed box** is attack surface and maintenance (Rust + Go + build deps to keep patched) that a dumb image-pull box (O2) avoids.
- **More box-side machinery than O2:** the workspace/releases/symlink discipline, the gate, and the rollback logic are real code to own, where O2 delegates atomicity + rollback to image tags.

### Neutral

- systemd is the supervision/scheduling substrate — standard OS, not a boxer invention.
- The immutable-`releases/` + `current`-symlink swap is the conventional Capistrano-style atomic-deploy pattern; the boxer-specific part is the Go command, the env registry, the `runinfo` truth, and the `ws_probe` gate.

### Derived practices

- **boxer ops tools are Go cli commands on the house libraries** (cli/zerolog/eh-eb/env-registry, observable via the bus), not standalone scripts — the default for future on-box tooling.
- **Atomic on-box deploy = immutable `releases/` + symlink swap + a real smoke gate (the app's own probe), restart blip absorbed by a holding page** — the reusable recipe.
- **An unattended internet-exposed auto-deployer gates on tag provenance** (signed tags, fail-closed), extending [ADR-0082](./0082-imzero2-remote-session-auth-tls-fanout.md)'s secure-by-default posture to the supply chain.

## Status

Proposed — awaiting review by p@stergiotis.

Implementation phasing: **Phase 1** — the `deploy` subcommand happy path (fetch → checkout → build via the existing scripts → stage to `releases/<tag>/` → `ws_probe` gate → symlink swap → restart). **Phase 2** — rollback + retention + post-restart re-probe (SD6). **Phase 3** — env-registry knobs (SD7) surfaced in `doc/env-vars.md`; `runinfo` agreement asserted. **Phase 4** — signed-tag verification (SD8) + the optional audited bus record (SD7). **Phase 5** — the systemd `service`/`timer` units, the Caddy `handle_errors` holding page, and end-to-end validation on a build-sized Hetzner box. The existing container kit (`showcase/`) is untouched and remains the manual / off-box path (and the basis of O2).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See `doc/DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## References

- [ADR-0024 — imzero2 remote access via headless render + ffmpeg + browser viewer](./0024-imzero2-remote-access-browser-viewer.md) — the demo this deploys; `hmi_headless.sh` builds-then-runs; the carrier and `ws_probe`.
- [ADR-0082 — securing the imzero2 remote session](./0082-imzero2-remote-session-auth-tls-fanout.md) — Caddy supplies the v1-missing TLS/auth; the secure-by-default / fail-closed ethos SD8 extends to tag provenance.
- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md) — `imzero2env`, where SD7's knobs register and surface in `doc/env-vars.md`.
- `showcase/` — the existing manual / container deploy kit; the basis of the O2 escape.
- Building blocks: `public/keelson/runtime/runinfo` (deployed-revision truth), `public/keelson/runtime/inprocbus` (audited deploy records — `Request`, not `Publish`), `public/keelson/data/chlocalpool` (the in-tree external-binary-by-path precedent), `rust/imzero2/src/bin/ws_probe.rs` (the gate), `rust/imzero2/{build_rust_headless.sh,build_go.sh,hmi_headless.sh}`.
- Capistrano-style symlinked releases — prior art for the immutable-`releases/` + `current`-symlink atomic-deploy pattern.
