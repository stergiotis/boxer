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

## Notes / Phase 1 scope

- **Happy path only.** Rollback/retention (ADR-0085 SD6) and signed-tag
  verification (SD8) are later phases; the deploy keeps a failed candidate on
  disk and never swaps it in.
- **Flags → env registry** is Phase 3; today the knobs are `imzero2 deploy`
  flags (`--scratch-port`, `--gate-aus`, `--gate-timeout`, `--encoder-args`, …).
- **Restart permission** comes from the polkit rule; no sudo in the tool.
