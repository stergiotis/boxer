---
type: how-to
audience: operator provisioning a fresh box
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ansible-pull provisioning for the imzero2 on-box deploy (ADR-0085)

Bring a **fresh Fedora 44 box** up to the point where the Go deploy tool
(the `imzero2 deploy` subcommand) can take over. This playbook owns
exactly the host setup `onbox/ONBOX.md` describes — **service user, toolchain,
read-only clone, systemd units + Caddy** — and nothing the Go code already does.

## Two loops, on purpose

ADR-0085 is itself a *pull* model: `imzero2-deploy.timer` polls signed release
tags, builds on-box, gates, and atomically swaps `current`. So we keep two
**separate** loops and never let them fight over the workspace checkout:

| Loop | Driver | Cadence | Owns |
|---|---|---|---|
| **App releases** | `imzero2-deploy.timer` (Go) | 10 min | fetch · verify sig · build · gate · swap · health · rollback · prune |
| **Host config** | `imzero2-pull.timer` (ansible-pull) | daily | user · toolchain · units · drop-ins · Caddy |

The `repo` role clones the workspace **once** and never touches it again — the
deploy tool owns that tree (detached HEAD at a release tag).

## What the playbook adds that the stock units don't

Two Fedora realities the Ubuntu-flavoured `onbox/` kit doesn't cover, both
installed as systemd drop-ins:

- **Build PATH.** `deploy.go` builds with the deploy unit's *inherited* env, and
  `imzero2-deploy.service` sets only `HOME`+`CARGO_HOME`. The drop-in adds
  `…/.cargo/bin` (rustup) and `/usr/local/go/bin` to `PATH` (+ `RUSTUP_HOME`),
  or the systemd-driven `cargo`/`go` build can't find its toolchain.
- **Encoder.** Fedora's ffmpeg ships **libopenh264, not libx264**. The drop-ins
  set `IMZERO2_HEADLESS_ENCODER_ARGS` (libopenh264) for both the live demo and
  the deploy gate, overriding the units' hardcoded x264.

## 0. Box

Build-sized, per `onbox/ONBOX.md`: **≈4–8 cores, 8–16 GB, ≥80 GB disk**, caches
kept. ARM works (the Go arch is auto-detected; flip ffmpeg if you need x264).

## 1. Site secrets (root-only, not in git)

The pull timer re-runs unattended, so site values + secrets live in a file it
reads via `-e @…`, not on the command line:

```bash
sudo install -d -m 0750 /etc/imzero2
sudo tee /etc/imzero2/provision.yml >/dev/null <<'YAML'
imzero2_domain: "203.0.113.7.nip.io"            # real (sub)domain → box, or <ip>.nip.io
imzero2_basic_auth_user: "demo"
imzero2_basic_auth_hash: '$2a$14$REPLACE'        # caddy hash-password --plaintext '...'
imzero2_allowed_signers:                         # the release tag signer(s) (SD8)
  - "release@you ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
# imzero2_require_signed_tags: false             # loopback/dev box ONLY
# imzero2_repo_url: "git@github.com:you/boxer.git"  # private mirror / deploy key
YAML
sudo chmod 0640 /etc/imzero2/provision.yml
```

## 2. Kick off ansible-pull (as root)

```bash
sudo dnf -y install ansible-core git
sudo ansible-pull \
  -U https://github.com/stergiotis/boxer.git -C main \
  -i 'localhost,' -e @/etc/imzero2/provision.yml \
  showcase/ansible/site.yml
```

This installs the toolchain, clones the workspace, lands the units + drop-ins +
polkit rule, sets up Caddy, and starts `imzero2-pull.timer`. The deploy timer
and demo are **enabled but not started yet** — there's no release to serve.

## 3. Bootstrap the first release

`current/` doesn't exist, so build it once (there must be a reachable signed
release tag, e.g. `v0.1.0`). Either set `imzero2_bootstrap_first_release: true`
in `provision.yml` and re-run step 2 (does the cold build in-play), **or** run
it by hand for visibility (mirrors `onbox/ONBOX.md`):

```bash
sudo -u imzero2 -H bash -lc '
  export PATH=/opt/imzero2/.cargo/bin:/usr/local/go/bin:$PATH
  export CARGO_HOME=/opt/imzero2/.cargo RUSTUP_HOME=/opt/imzero2/.rustup
  export IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libopenh264 -b:v 4M -bf 0 -g 60"
  cd /opt/imzero2/workspace/rust/imzero2 && ./build_go.sh &&
  ./main_go --logFormat=console imzero2 deploy --root /opt/imzero2 --dry-run'   # build + gate, no swap
# happy? drop --dry-run to create releases/<tag> + current, then:
sudo systemctl start imzero2-deploy.timer imzero2-demo.service
```

(Re-running ansible-pull after this also starts them — it detects `current/`.)
Watch a deploy: `journalctl -fu imzero2-deploy.service`.

## Fedora notes

- **Encoder:** libopenh264 by default. For x264 quality set
  `imzero2_install_rpmfusion_ffmpeg: true` (pulls RPM Fusion + swaps in the full
  `ffmpeg`) and point `imzero2_encoder_args` back at the x264 string.
- **firewalld:** 80/443 are opened automatically (not `ufw`).
- **SELinux (enforcing):** handled end-to-end, on by default. The playbook
  installs a persistent fcontext rule (release tree + `run-current.sh` →
  `bin_t`) so new release binaries inherit an exec-able label, and the deploy
  tool `restorecon`s each release as a backstop (`deploy.go relabelSELinux`).
  If you still see an execute AVC (`ausearch -m avc -ts recent`), confirm the
  rule with `semanage fcontext -l | grep imzero2`. Turn it off
  (`imzero2_selinux_label_releases: false`) only on a box with SELinux disabled.

## Hardening (systemd sandboxing)

`imzero2_systemd_hardening: true` (default) adds sandbox drop-ins to both units —
`ProtectSystem=strict`, `ProtectHome`, the `Protect*`/`Restrict*` kernel knobs,
and (demo only) `PrivateDevices`, a `@system-service` syscall allowlist, and
loopback-only egress. Asymmetric on purpose:

- **Demo** (the carrier behind Caddy) is locked down hard — it only reads
  `current/` + fonts and binds `127.0.0.1`. FFFI2 (Go↔carrier) is stdin/stdout
  pipes, so the sandbox doesn't sever it.
- **Deploy** oneshot is lighter — it builds (cargo/go/cc/ld), needs the network,
  and JITs the gate carrier, so no syscall allowlist / egress filter / cap drop.

Two rules are baked in: **never `MemoryDenyWriteExecute`** (llvmpipe/lavapipe
shader JIT + ffmpeg need W^X), and the build keeps network + exec.

Verify once on a real box: after the first deploy, confirm the **stream comes
up** and a **cold build** completes. If the stream won't start, the usual
suspects on the demo are `PrivateDevices` and `SystemCallFilter` — drop them from
`…/imzero2-demo.service.d/20-hardening.conf` (or set
`imzero2_systemd_hardening: false`) and re-test. See what systemd actually
applied with `systemd-analyze security imzero2-demo.service`. (The canonical
`onbox/imzero2-demo.service` keeps only light hardening pending this check; once
verified under the sandbox, these drop-ins can be folded upstream.)

## Re-running / convergence

The playbook is idempotent; `imzero2-pull.timer` re-applies it daily. Re-run on
demand with `sudo systemctl start imzero2-pull.service`, or just re-run the
step-2 command. Toolchain installs are guarded (version check / `creates:`), so
steady-state runs are cheap and never disturb a running deploy.

## Security

- **Signed tags on by default.** List the signer(s) in `imzero2_allowed_signers`
  or the deploy refuses to build. Disable only on a loopback/dev box.
- `provision.yml` is root-only and out of git; the pull timer reads it via `-e @`.
- Caddy is the only thing bound to 80/443; the carrier stays on `127.0.0.1:8089`.
