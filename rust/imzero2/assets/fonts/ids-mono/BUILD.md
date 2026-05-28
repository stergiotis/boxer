---
type: how-to
audience: engineer bumping the IDS Mono font pin
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Currently consumes IDS Mono v0.1.1
> release artefacts from upstream.

# How to bump IDS Mono

**The build lives upstream.** Per [ADR-0034 §SD2 pivot
Amendment](../../../../../doc/adr/0034-imzero2-design-system-typography-m0.md),
the IDS Mono build was moved to a dedicated repo,
[`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts).
CI there handles the build on every tagged release — no Docker, no
Node.js, no font toolchain installs on pebble2impl contributor machines.

Most contributors never need to do anything. The `.ttf` bytes in this
directory are committed under SHA pinning;
[`scripts/ci/lint.sh`](../../../../../scripts/ci/lint.sh) verifies them
on every build.

## Current pin

- **IDS Mono release:** `v0.1.1` ([release page](https://github.com/stergiotis/ids-fonts/releases/tag/v0.1.1))
- **Iosevka upstream:** `v34.5.0`
- **Family name:** `IDS Mono`
- **Files:** `IDSMono-{Regular,Italic,Medium,MediumItalic,Bold,BoldItalic}.ttf` + `SHA256SUMS` + `LICENSE`

## To bump the IDS Mono pin

1. In `../ids-fonts/`: edit `IOSEVKA_VERSION` or tweak
   `private-build-plans.toml`, run `make build` to validate locally, then
   `git tag v0.X.Y && git push --tags`. CI publishes the release with the
   six `.ttf` files, `SHA256SUMS`, and the OFL `LICENSE`.
2. In this repo: replace the contents of `assets/fonts/ids-mono/` with
   the released files. The six TTFs, `SHA256SUMS`, and `LICENSE` all live
   at `https://github.com/stergiotis/ids-fonts/releases/download/<tag>/`.
   Verify with `sha256sum -c SHA256SUMS` against the release-published
   SHA256SUMS, then commit. Document the bump in an Amendment to
   [ADR-0034](../../../../../doc/adr/0034-imzero2-design-system-typography-m0.md).

## To verify locally

```bash
cd src/rust/assets/fonts/ids-mono/
sha256sum -c SHA256SUMS
```

Expected output: each `.ttf` line ends in `OK`. The full IDS lint
(`scripts/ci/lint.sh`) runs the same check; a PR that passes lint has
verified font hashes.

## Troubleshooting

- **Symptom:** Variant key unknown to the customizer (e.g.
  `single-storey-earless-corner`).
  **Cause:** Iosevka renamed a stylistic variant between releases.
  **Fix:** Open the [Iosevka customizer](https://typeof.net/Iosevka/customizer)
  at the pinned version, find the equivalent variant, update
  `private-build-plans.toml` **in `ids-fonts/`** (not here), and
  document the rename in an Amendment to
  [ADR-0034](../../../../../doc/adr/0034-imzero2-design-system-typography-m0.md).

- **Symptom:** `SHA256SUMS` mismatch after a release bump.
  **Cause:** Stale local copy, or the `SHA256SUMS` file in the release
  asset doesn't match the staged `.ttf` files.
  **Fix:** Re-download the release artefacts and `SHA256SUMS` together;
  verify with `sha256sum -c` against the release-published file before
  committing.
