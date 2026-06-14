# On-box validation checklist — ADR-0085 deploy

The unit tests prove tag selection / probe parsing / prune *selection*; they do
**not** prove the pipeline against a real fetch → build → swap → rollback. This
is that acceptance pass. Treat a green run here — not the unit tests — as the
gate before trusting the deploy unattended on an internet-exposed box.

## Can it be scripted?

**Mostly.** The deploy *logic* (A1–A8) scripts cleanly against a throwaway
**local** remote with controlled signed/unsigned tags, a temp deploy root, and
a `systemctl` **shim** that backgrounds `run-current.sh` — so it needs no real
systemd and runs on a dev box. `validate.sh` (this dir) automates A1–A8. Only
the **integration** checks (B1–B4: the real timer cadence, polkit restart, the
Caddy holding page, unprivileged user) are irreducibly tied to the real box;
they are small and one-time.

**Status (2026-06-14): validated — `validate.sh` runs 8/8 green** on a dev box
(Fedora; software + AMD-Vulkan offscreen render; ffmpeg `libopenh264`, since the
`h264_vaapi` default needs a VAAPI device). Each step is also independently
runnable (`VALIDATE_ONLY="A5 A1" …`), so you can work the checklist by hand.

The shake-out caught real defects:

- **`ws_probe` bin name** — the gate/health probe invoked `ws_probe`, but the
  cargo bin is `imzero2_ws_probe`; the gate could never build it. Fixed (A1 is
  green end-to-end).
- **The gate trusted the scratch port** — `ws_probe` connects to a fixed
  loopback port, but nothing checked the *candidate* was the listener there. A
  carrier leaked by a crashed prior deploy — or a port collision with the live
  service — would make the gate decode a **stranger's** stream and wave a broken
  release through the swap. The gate now fails fast if the scratch port (or
  scratch+1, the viewer page) is already held. This same stale-listener class
  was, during the shake-out, spuriously passing A5 *and* defeating A6's live
  health probe via one 4-hour-old leaked carrier squatting on both ports.
- **Harness hygiene** — each carrier binds `P` **and** `P+1` (ws + viewer page),
  so the live/scratch ports must differ by ≥2; and gate-candidate carriers must
  be reaped per run (`pkill -f "$WORK"`) or they squat the ports for the next.

## A — logic (scripted by `validate.sh`)

| # | Check | Method | Pass criteria |
|---|---|---|---|
| **A1** | Signed tag deploys | deploy a **signed** annotated tag | builds, gates, swaps; `current` → that tag; exit 0 |
| **A2** | Unsigned tag refused | deploy a **lightweight/unsigned** tag | aborts **before build**; `current` unchanged; log "no valid signature"; exit ≠ 0 |
| **A3** | Dev escape | the unsigned tag with `--require-signed-tags=false` | proceeds, with the loud "verification DISABLED" warning |
| **A4** | No-op when current | re-run with no newer tag | "already current"; no checkout/build |
| **A5** | Gate blocks non-streaming build | deploy with `--encoder-args "-c:v <bogus>"` → ffmpeg exits, 0 AUs | aborts **after gate, before swap**; `current` unchanged |
| **A6** | Rollback on bad activation | deploy a good tag, but make the post-swap restart fail to bring the service up | swap happens, health probe fails, **`current` reverts to the previous release**; exit ≠ 0 |
| **A7** | Retention / prune | deploy `K+2` tags | `releases/` keeps `K` (+ current); oldest pruned |
| **A8** | Revision agreement (SD7) | after A1 | the deployed binary's embedded `vcs.revision` == the tag's commit; the deploy log shows that commit |

## B — integration (manual, on the real box)

| # | Check | Method | Pass criteria |
|---|---|---|---|
| **B1** | Timer fires | `systemctl enable --now imzero2-deploy.timer`; push a newer signed tag | `journalctl -u imzero2-deploy` shows a full cycle within the interval |
| **B2** | polkit restart | run the deploy as the `imzero2` user | `systemctl restart imzero2-demo.service` succeeds with no sudo |
| **B3** | Holding page | during a build / the restart swap | `curl -sk https://$DOMAIN/` (authed) returns `building.html` while upstream is down, the live demo once up |
| **B4** | Unprivileged | demo running | `systemctl show -p MainPID imzero2-demo` → the process runs as `imzero2`, no caps |

## Manual run skeleton (if not using `validate.sh`)

```bash
# one signed + one unsigned tag on a throwaway clone (SSH signing)
ssh-keygen -t ed25519 -f /tmp/signer -N '' -C release@test -q
printf 'release@test %s\n' "$(cat /tmp/signer.pub)" > /tmp/allowed
git clone --bare --local "$PWD" /tmp/remote.git
git clone --local /tmp/remote.git /tmp/ws
git -C /tmp/ws config gpg.format ssh
git -C /tmp/ws config user.signingkey /tmp/signer
git -C /tmp/ws config user.email release@test
git -C /tmp/ws config gpg.ssh.allowedSignersFile /tmp/allowed
# tags must match the selector `^v?\d+(\.\d+)*$`; the unsigned one is newest
git -C /tmp/ws tag -s v9000.0.1 -m signed && git -C /tmp/ws push origin v9000.0.1
git -C /tmp/ws tag v9000.0.2              && git -C /tmp/ws push origin v9000.0.2
# A2 (fast, no build): unsigned newest must be refused before building
./rust/imzero2/main_go imzero2 deploy --root /tmp/dr --workspace /tmp/ws \
  --service none --dry-run   # expect: refuses v-test-2 / picks newest; inspect the log
```
