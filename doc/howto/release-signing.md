---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-06-15
---

# How to sign and verify boxer releases

How boxer establishes release provenance: cut **SSH-signed annotated tags**, and
have whatever builds or deploys from a tag **verify the signature against a
trusted public key first**. SSH signing is the standard — it's git ≥ 2.34, no
GPG keyring. The first consumer is the imzero2 on-box deploy, which refuses to
build an unsigned tag ([ADR-0085](../adr/0085-imzero2-demo-pull-build-atomic-deploy.md)
SD8: `deploy.go verifyTag` → `git verify-tag`); the same recipe applies to
anything a machine builds or deploys from a tag automatically.

## When to use this recipe

You cut a boxer release tag that something downstream acts on automatically, and
that downstream must not build a forged or mirror-tampered ref. The signing
machine holds the private key; every consumer holds only the **public** key — it
can verify, never sign.

This is a Markdown how-to (not an `example_test.go`) because it needs external
setup — an SSH key, git signing config, and a consumer box — which is the
documentation standard's explicit How-To exception.

## Prerequisites

- git ≥ 2.34 (SSH signature support) on both the signer and the consumer.
- An SSH key for signing — ed25519 recommended; a hardware-backed
  `sk-ssh-ed25519` key (a human signer) or a no-passphrase key held as a CI
  secret both work.

## Steps

### 1. On the machine that cuts releases (your laptop or CI)

```bash
# 1. A signing key (ed25519). Passphrase is fine on a laptop; omit it for CI.
ssh-keygen -t ed25519 -C release@you -f ~/.ssh/imzero2_release

# 2. Tell git to sign tags with it.
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/imzero2_release.pub
git config --global user.email release@you      # MUST match the consumer's allowed_signers principal — see the gotcha

# 3. Cut a SIGNED ANNOTATED tag and push it.
git tag -s v0.1.0 -m "imzero2 demo v0.1.0"
git push origin v0.1.0
```

Two tag rules the deploy enforces: a **lightweight** tag (`git tag v0.1.0`, no
`-s`) has no signature and is rejected; and the tag must be a plain version
matching `^v?\d+(\.\d+)*$` — `v0.1.0` works, `v0.1.0-rc1` won't even be
*selected* (`selectNewestTag`).

### 2. On the consumer — give it the public key

A consumer trusts a signer by listing its public key in a git `allowed_signers`
file and pointing `git verify-tag` at it:

```bash
git config gpg.format ssh
git config gpg.ssh.allowedSignersFile <path>/allowed_signers
# allowed_signers: one line per trusted signer — "release@you ssh-ed25519 AAAA..."
```

Produce the line (principal + key, no comment):

```bash
echo "release@you $(awk '{print $1, $2}' ~/.ssh/imzero2_release.pub)"
# → release@you ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...
```

The imzero2 on-box deploy **automates exactly this**. Put the line in the box's
root-only `/etc/imzero2/provision.yml` and re-run `ansible-pull`:

```yaml
imzero2_require_signed_tags: true        # default
imzero2_allowed_signers:
  - "release@you ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
```

The `repo` role writes it to `/opt/imzero2/.config/git/allowed_signers` and
points git at it; the deploy's `git verify-tag` (run as `imzero2`,
`HOME=/opt/imzero2`) then validates every tag before building. The box only ever
holds the public key. (See `showcase/ansible/`, and `showcase/onbox/ONBOX.md`
§Tag signing.)

## The principal gotcha (this bites everyone)

`git verify-tag` (SSH) matches the **tagger's `user.email`** against the
*principal* in `allowed_signers`. If you tag with `user.email=release@you`, the
allowed_signers line must start with exactly `release@you`. Mismatch → "no
principal matched" → verification fails and the build is refused, **even though
the key is present**. Keep the email and the principal identical.

## Verification

On the consumer:

```bash
git -C <workspace> fetch --tags
git -C <workspace> verify-tag v0.1.0
# → Good "git" signature for release@you with ED25519 key SHA256:...
```

On an imzero2 box specifically:

```bash
sudo -u imzero2 -H git -C /opt/imzero2/workspace fetch --tags
sudo -u imzero2 -H git -C /opt/imzero2/workspace verify-tag v0.1.0
```

A deploy run then logs `deploy: tag signature verified`; a bad or unsigned tag
logs `no valid signature … refusing to build` and leaves `current` untouched.

## Troubleshooting

- **Symptom:** `git verify-tag` reports "No principal matched" though the key is
  in `allowed_signers`.
  **Cause:** the tagger's `user.email` ≠ the `allowed_signers` principal.
  **Fix:** make them identical — re-tag with the right `user.email`, or correct
  the principal on the consumer.
- **Symptom:** the deploy logs "no valid signature … refusing to build" and
  `current` is untouched.
  **Cause:** the tag is lightweight (unsigned), or signed by a key the consumer
  doesn't trust.
  **Fix:** `git tag -s` with a key listed in the consumer's `allowed_signers`.
- **Symptom:** a freshly pushed tag is ignored ("no release tag found", or an
  older tag is picked).
  **Cause:** the tag name isn't a plain version; `-rc`/`-beta` suffixes aren't
  selected.
  **Fix:** name it `^v?\d+(\.\d+)*$`, e.g. `v0.2.0`.

## Variants

- **CI cuts releases.** Store the *private* key as a CI secret, run the same
  `git config` + `git tag -s` in the job. Nothing changes on the consumer. This
  keeps signing authority off the internet-exposed box — the point of SD8.
- **Prefer GPG?** `git tag -s` uses GPG when `gpg.format` isn't `ssh`; the
  consumer trusts it via `gpg --import signer.asc` (on an imzero2 box,
  `sudo -u imzero2 gpg --import signer.asc`). The ansible provisioner automates
  only the SSH path, so GPG is a manual trust step.
- **Dev / loopback box.** `imzero2_require_signed_tags: false` (→
  `IMZERO2_DEPLOY_REQUIRE_SIGNED_TAGS=false`, or `--require-signed-tags=false`)
  skips verification with a loud warning. Never on an internet-exposed box.
