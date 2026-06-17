---
type: how-to
audience: operator provisioning a build box
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# On-box pull-build-and-atomic-deploy (ADR-0085) — setup

The **build-on-box, bare-OS** mode (Option B). A build-capable box polls boxer's
release tags, builds on-box (incremental, cached), gates the build with
`ws_probe`, and atomically swaps a `current` symlink — all driven by the
`imzero2 deploy` subcommand under a systemd timer. Caddy fronts it with TLS + a
password and shows a holding page during builds/restarts.

This is **not** the container kit in `../` (that's the off-box / manual path).

## Box

Build-sized, not a 2-vCPU streamer (the 663-crate wgpu release is the cost):
**≈4–8 cores, 8–16 GB, ≥80 GB disk**, and *keep the caches* so only the rare
cold build is heavy. Hetzner CX42 or CAX31 (ARM — adjust the static-ffmpeg arch)
are reasonable. Any OS with the toolchain; build == run on the same box, so
glibc matches by construction.

## Layout

```
/opt/imzero2/
  workspace/        # persistent git clone + cargo target/ + Go caches (builds run here)
  releases/<tag>/   # immutable: main_go, imzero2, ws_probe, assets/
  current -> releases/<tag>
  holding/building.html
  run-current.sh
  .cargo/
```

## One-time setup (as root)

1. **User + dirs**
   ```bash
   useradd --system --create-home --home-dir /opt/imzero2 --shell /usr/sbin/nologin imzero2
   install -d -o imzero2 -g imzero2 /opt/imzero2/{releases,holding,.cargo}
   ```
2. **Toolchain + runtime deps** (names vary by distro): `git`, Rust **1.92**
   (rustup honours the repo's `rust-toolchain`), Go **1.26**, `ffmpeg` with
   libx264, Mesa software drivers (`mesa-vulkan-drivers` + DRI), `fontconfig` +
   Noto fonts, and `caddy`. Install as the `imzero2` user where it owns caches.
3. **Clone** (read-only deploy key; the box never holds push creds):
   ```bash
   sudo -u imzero2 git clone <read-only-url> /opt/imzero2/workspace
   ```
4. **Install the ops files** (from this `onbox/` dir):
   ```bash
   install -m755 run-current.sh /opt/imzero2/run-current.sh
   install -m644 building.html  /opt/imzero2/holding/building.html
   install -m644 imzero2-demo.service imzero2-deploy.service imzero2-deploy.timer /etc/systemd/system/
   install -m644 49-imzero2-deploy.rules /etc/polkit-1/rules.d/
   chown -R imzero2:imzero2 /opt/imzero2
   ```
5. **Caddy**: point this `Caddyfile` at it and export `DOMAIN`,
   `BASIC_AUTH_USER`, `BASIC_AUTH_HASH` (`caddy hash-password --plaintext '...'`).
   `DOMAIN=<box-ip>.nip.io` works with no DNS. Open 80/443. `systemctl enable --now caddy`.

## Tag signing (SD8)

By default the deploy **refuses to build an unsigned tag** — a compromised
mirror or forged ref must not make the box build arbitrary code. So:

- Publish releases as **signed annotated tags**: `git tag -s v0.1.0 -m '...'`
  (lightweight tags have no signature and will be rejected).
- The box must **trust the signer**. For GPG, import the signer's public key
  into the `imzero2` user's keyring (`sudo -u imzero2 gpg --import signer.asc`).
  For SSH signing, set (as the `imzero2` user):
  ```bash
  git config --global gpg.format ssh
  git config --global gpg.ssh.allowedSignersFile /opt/imzero2/.config/git/allowed_signers
  # allowed_signers: one line "release@you  ssh-ed25519 AAAA..." per trusted key
  ```
- **Dev/loopback escape only:** `IMZERO2_DEPLOY_REQUIRE_SIGNED_TAGS=0` (or
  `--require-signed-tags=false`) skips verification. The deploy logs a loud
  warning; never use it on an internet-exposed box.

## Bootstrap the first release

`current/` does not exist yet, so run the deploy from the workspace once. Build
just the Go launcher, then let `deploy` build the rest at the newest tag:

```bash
sudo -u imzero2 -H bash -lc '
  cd /opt/imzero2/workspace/rust/imzero2 && ./build_go.sh &&
  ./main_go --logFormat=console imzero2 deploy --root /opt/imzero2 --dry-run'   # build + gate, no swap
# happy? drop --dry-run to create releases/<tag> and current:
sudo -u imzero2 -H /opt/imzero2/workspace/rust/imzero2/main_go imzero2 deploy --root /opt/imzero2
```

(There must be at least one release tag, e.g. `v0.1.0`, reachable in the clone —
otherwise `deploy` logs "no release tag found".)

## Go live + automate

```bash
systemctl daemon-reload
systemctl enable --now imzero2-demo.service     # serves current/
systemctl enable --now imzero2-deploy.timer     # polls every 10 min
systemctl start  imzero2-deploy.service         # run one cycle now
journalctl -fu imzero2-deploy.service           # watch a deploy
```

On a new tag: the timer fires the deploy → fetch → checkout → build (incremental)
→ stage → `ws_probe` gate → atomic swap → restart. Caddy shows `building.html`
during the cold/first build and the restart blip; a build or gate failure leaves
`current` untouched.

## Notes / scope

- **Phases 1–4 implemented.** Happy path; post-restart health re-probe with
  automatic **rollback** + **keep-last-K retention** (SD6); knobs in the
  `IMZERO2_DEPLOY_*` env registry with deployed-revision logging (SD7); and
  **signed-tag verification** (SD8, above). A failed build, gate, or signature
  check keeps `current` untouched; a release that fails to serve after the swap
  is rolled back (no rebuild). The audited-bus deploy record (optional in SD7)
  is deferred — deploy events already reach the facts store via the logbridge.
- **Flags → env registry** is Phase 3; today the knobs are `imzero2 deploy`
  flags (`--scratch-port`, `--gate-aus`, `--gate-timeout`, `--encoder-args`, …).
- **Restart permission** comes from the polkit rule; no sudo in the tool.
