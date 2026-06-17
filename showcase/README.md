---
type: explanation
audience: contributor or operator
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# showcase — the boxer/keelson demonstrator

How the public **boxer/keelson demonstrator** is built, deployed, and served: a
live gallery of capability-scoped apps (the keelson app runtime, ADR-0026 —
`play`, `imztop`, `idsshowcase`, `hn_explorer`, the leeway widgets, …) rendered
through the **imzero2** headless pixel-streaming host (ADR-0024) into a browser
tab.

## Why this lives here, not under `rust/imzero2/`

`imzero2` is the **transport** — offscreen egui render → H.264 → WebSocket. It is
*how* the demonstrator reaches a viewer, not *what* is demonstrated. What's
demonstrated is boxer/keelson as a whole. So the build/deploy/publish machinery
lives at the repo root, next to `apps/`, and treats imzero2 as a dependency.

The imzero2 name is **kept for the transport vocabulary** — the `imzero2 demo`
subcommand, the `imzero2-demo.service` unit, and the `IMZERO2_*` env vars are the
streaming host's surface and are not renamed. Only the *deploy machinery's home*
moved up here.

> The deploy **logic** — the Go `imzero2 deploy` subcommand (ADR-0085) — lives in
> [`deploy/`](deploy/), wired into the imzero2 CLI as `imzero2 deploy`.

## Two delivery paths (ADR-0085)

| Path | Where | What it is |
|---|---|---|
| **On-box (O1)** | [`onbox/`](onbox/ONBOX.md) + [`ansible/`](ansible/README.md) | Build-on-box, self-updating from signed release tags, atomic swap. The blessed path; provisioned by the ansible-pull playbook. |
| **Off-box (O2)** | the container kit here + [`DEPLOY.md`](DEPLOY.md) | Build a runtime image locally, ship it, `compose up`. For a box that should not build (small/cheap, no toolchain) and the basis of the future fleet path. |

Both put the carrier behind Caddy, which supplies the TLS + password the v1
carrier doesn't (ADR-0082). The transport binaries are built by the imzero2
scripts that stay put: `rust/imzero2/build_rust_headless.sh` + `build_go.sh`.

## Start here

- How the running system is wired (the processes + transports) → [`EXPLANATION.md`](EXPLANATION.md).
- Standing up a box → [`ansible/README.md`](ansible/README.md) (Fedora 44, ansible-pull).
- The on-box deploy model → [`onbox/ONBOX.md`](onbox/ONBOX.md).
- The container/off-box path → [`DEPLOY.md`](DEPLOY.md).
