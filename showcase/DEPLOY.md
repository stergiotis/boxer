# Deploy the imzero2 pixel-streaming demo on a Hetzner VPS

A single-box demo: the headless imzero2 carrier (ADR-0024) behind Caddy, which
adds the TLS + password that v1 does **not** ship (ADR-0082 is proposed). One
viewer at a time (v1 single-session); software render + encode (no GPU needed).

> **Two deploy paths (ADR-0085).** This is the **off-box / manual** path ("O2"):
> build the image locally, ship it, `compose up` — best for a box that should
> not build (small/cheap, no toolchain on it) or a future fleet. For the
> **on-box**, self-updating path ("O1": build on-box from signed tags, gate,
> atomic swap), see [`onbox/ONBOX.md`](onbox/ONBOX.md) and the ansible-pull
> provisioner in [`ansible/`](ansible/README.md).

## What you get / limits

- `https://<your-domain>` → password prompt → the live demo in a WebCodecs
  browser tab. Pixels stream out; input streams back.
- **One concurrent viewer.** A second connection is rejected (v1). Fine for
  showing one person or screen-sharing.
- **Demo selection:** boots into the **interactive carousel** — the viewer
  opens any demo (sccmap, widgets, leewaywidgets, ...) from the in-browser
  launcher. Auto-booting one demo (`LAUNCH=sccmap`) needs `clickhouse-local`
  baked in (+~2 GB), because `--launch` resolves via a SQL query; the lean
  default avoids it. See demo.env.example.
- Security is entirely Caddy's: TLS (Let's Encrypt) + HTTP basic-auth. The app
  port is never published to the host. Do not `-p` 8089/8090 publicly.

## Box sizing

CPU-only, ~1.5 cores + ~0.5 GiB for one active session.
- **CX22** (2 vCPU / 4 GiB, ~€4/mo) — enough for one viewer of a calm dashboard.
- **CX32** (4 vCPU / 8 GiB, ~€6/mo) — headroom for an animated demo. Recommended.

## 0. Prereqs (local)

Binaries must be built once with the project scripts:

```bash
cd rust/imzero2
./build_rust_headless.sh && ./build_go.sh     # produces target/headless/release/imzero2 + main_go
```

## 1. Build the image (local)

```bash
cd showcase
./build.sh                 # ENGINE=docker ./build.sh  if you use docker
```

Optional local smoke test (no TLS, no auth):

```bash
podman run --rm -p 8089:8089 imzero2-demo:latest      # interactive carousel
# open http://127.0.0.1:8089/ in a WebCodecs browser (Chrome/Firefox/Safari 16.4+),
# then click into a demo (sccmap, widgets, leeway, ...) from the launcher
```

## 2. Create the Hetzner box

Console: new server → **Ubuntu 24.04**, type **CX32**, add your SSH key, create.
Or CLI: `hcloud server create --name imzero2-demo --type cx32 --image ubuntu-24.04 --ssh-key <key>`

Install Docker on it:

```bash
ssh root@<box-ip> 'curl -fsSL https://get.docker.com | sh'
```

## 3. Ship the image + config

No registry needed — pipe the image straight over SSH:

```bash
# from showcase  (~1 GB image → ~400 MB over the wire with gzip)
podman save imzero2-demo:latest | gzip | ssh root@<box-ip> 'gunzip | docker load'
scp docker-compose.yml Caddyfile demo.env.example root@<box-ip>:/root/imzero2/
```

(Alternative: skip steps 1+3, `scp` the repo and run `./build.sh` on the box —
needs CX32's RAM. Shipping the image is faster and keeps the toolchain off the box.)

## 4. Point DNS + set secrets (on the box)

```bash
ssh root@<box-ip>
cd /root/imzero2 && cp demo.env.example demo.env
```

Edit `demo.env`:
- `DOMAIN` — either a real (sub)domain whose A record points at `<box-ip>`, or
  the zero-setup option **`<box-ip>.nip.io`** (resolves to the IP; Let's Encrypt
  will issue for it — if LE rate-limits nip.io, use a real domain).
- `BASIC_AUTH_USER` / `BASIC_AUTH_HASH` — generate the hash:
  ```bash
  docker run --rm caddy:2 caddy hash-password --plaintext 'YOUR_PASSWORD'
  ```
- `LAUNCH` — the demo to show (see the list in demo.env.example).

Open the firewall to 80/443 (Hetzner Cloud Firewall or `ufw allow 80,443/tcp`).
Port 80 must be reachable for the Let's Encrypt challenge.

## 5. Run

```bash
docker compose up -d
docker compose logs -f          # watch Caddy get a cert + the carrier come up
```

Visit `https://<DOMAIN>`, pass the password prompt, and the demo loads.

## 6. Teardown

```bash
docker compose down            # stop; `down -v` also drops the Caddy cert volume
# then delete the server in the Hetzner console / `hcloud server delete imzero2-demo`
```

## Troubleshooting

- **Black screen / no frames:** the encoder. Check `docker compose logs imzero2`
  for ffmpeg errors; switch `IMZERO2_HEADLESS_ENCODER_ARGS` to libopenh264 (see
  demo.env.example) or confirm `ffmpeg -encoders | grep libx264` inside the image.
- **`no wgpu adapter`:** software Vulkan/GL missing. The image installs Mesa
  lavapipe + llvmpipe; if it still fails, add `-e WGPU_BACKEND=gl` (forces the
  llvmpipe GL path with `LIBGL_ALWAYS_SOFTWARE=1`).
- **TLS won't issue:** DNS not pointing at the box yet, or port 80 blocked.
  nip.io occasionally hits LE rate limits — use a real domain.
- **Second viewer can't connect:** expected (v1 single-session).
