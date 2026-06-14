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

Caveat: `validate.sh` is itself **drafted, not yet run** — expect to debug it on
first use, like any new harness. Each step below is independently runnable, so
you can also work the checklist by hand.

## A — logic (scripted by `validate.sh`)

| # | Check | Method | Pass criteria |
|---|---|---|---|
| **A1** | Signed tag deploys | deploy a **signed** annotated tag | builds, gates, swaps; `current` → that tag; exit 0 |
| **A2** | Unsigned tag refused | deploy a **lightweight/unsigned** tag | aborts **before build**; `current` unchanged; log "no valid signature"; exit ≠ 0 |
| **A3** | Dev escape | the unsigned tag with `--require-signed-tags=false` | proceeds, with the loud "verification DISABLED" warning |
| **A4** | No-op when current | re-run with no newer tag | "already current"; no checkout/build |
| **A5** | Gate blocks non-streaming build | a tag whose binary won't stream (e.g. break the encoder via `--encoder-args false`) | aborts **after gate, before swap**; `current` unchanged |
| **A6** | Rollback on bad activation | pre-occupy the live port, then deploy a good tag | swap happens, health probe fails, **`current` reverts to the previous release**; exit ≠ 0 |
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
git -C /tmp/ws tag -s v-test-1 -m signed && git -C /tmp/ws push origin v-test-1
git -C /tmp/ws tag v-test-2-unsigned        && git -C /tmp/ws push origin v-test-2-unsigned
# A2 (fast, no build): unsigned must be refused before building
./rust/imzero2/main_go imzero2 deploy --root /tmp/dr --workspace /tmp/ws \
  --service none --dry-run   # expect: refuses v-test-2 / picks newest; inspect the log
```
