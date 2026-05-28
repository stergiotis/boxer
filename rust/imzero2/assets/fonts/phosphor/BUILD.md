---
type: how-to
audience: engineer bumping the Phosphor font pin
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Currently consumes Phosphor v2.1.1
> release artefacts from upstream `stergiotis/ids-fonts` at tag `v0.3.0`.

# How to bump Phosphor

**The build lives upstream.** Per [ADR-0044 §SD3](../../../../../doc/adr/0044-imzero2-design-system-iconography.md),
the Phosphor TTF + matching JSON catalogue are produced by
[`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts) — the
same repo that ships IDS Mono. CI there fetches the upstream
`@phosphor-icons/web` + `@phosphor-icons/core` packages at the pinned
version, runs `scripts/ts-to-json.py` against the catalogue, and
attaches all three artefacts (`Phosphor.ttf` + `phosphor-icons.json` +
`phosphor-icons.mjs` audit trail) to each tagged release.

Most contributors never need to do anything. The `.ttf` bytes in this
directory are committed under SHA pinning;
[`scripts/ci/lint.sh`](../../../../../scripts/ci/lint.sh) verifies them
on every build.

## Current pin

- **ids-fonts release:** `v0.3.0` ([release page](https://github.com/stergiotis/ids-fonts/releases/tag/v0.3.0))
- **Phosphor upstream:** `@phosphor-icons/web` `2.1.1` / `@phosphor-icons/core` `2.1.1`
- **Family name:** `Phosphor` (regular weight only — ADR-0044 §SD4 defers other weights)

## Files

| File           | Origin                                                                      | Notes |
|----------------|-----------------------------------------------------------------------------|-------|
| `Phosphor.ttf` | `Phosphor.ttf` artefact of `stergiotis/ids-fonts` `v0.3.0` release          | ~600 KB, ~1500 glyphs |
| `LICENSE`      | `Phosphor.LICENSE` artefact of the same release (Phosphor's upstream MIT)   | Must travel with the bytes. LF-normalised by the upstream ids-fonts pipeline since `v0.2.4`; MIT text matches the `phosphor-icons/web` GitHub repo verbatim. |
| `SHA256SUMS`   | This dir's `sha256sum` of `Phosphor.ttf` — matches release `SHA256SUMS`     | CI-verified                 |

The Go-side codepoint catalogue (`phosphor-icons.json`) lives separately
at [`src/go/public/keelson/runtime/icons/`](../../../../go/public/keelson/runtime/icons/);
the Go generator (`src/go/cmd/iconsgen`) regenerates `phosphor.out.go`
and `phosphor_lookup.out.go` from it. See ADR-0044 §SD3 for the split.

## Bumping the pin

1. Bump `PHOSPHOR_VERSION` in [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts) — the source-of-truth pin for both `@phosphor-icons/web` and `@phosphor-icons/core`.
2. Push a new release tag (`v0.X.Y`) — CI builds and publishes.
3. In this repo, fetch the new release's `Phosphor.ttf` plus `phosphor-icons.json`, verify both SHAs against the release's `SHA256SUMS`, then:
   - Replace `Phosphor.ttf` here.
   - Replace `src/go/public/keelson/runtime/icons/phosphor-icons.json`.
   - Regenerate `SHA256SUMS` in both directories.
   - Run `bash src/go/public/keelson/runtime/icons/generate.sh` to regenerate the Go constants.
4. Eyeball the diff — a removed `PhXxx` constant means upstream renamed/dropped an icon; resolve before committing.

## Attribution

Phosphor Icons by Helena Zhang and Tobias Fried, distributed under the
MIT license. See `LICENSE` in this directory for the full text.

This directory ships an unmodified redistribution of the upstream
`Phosphor.ttf` from `@phosphor-icons/web@2.1.1`. No subsetting or
re-rasterising; the bytes match upstream verbatim.
