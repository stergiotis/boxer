---
type: adr
status: accepted
date: 2026-05-14
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0034: IDS typography M0 — Iosevka pin, font binaries, build pipeline

## Context

[ADR-0030](./0030-imzero2-design-system-typography.md) commits the IDS typography slot picks (IDS Mono mono, Iosevka Aile proportional, Symbols Nerd Font Mono icons, Inter / Onest as the proportional fallback ladder) but defers the *version pinning* of each font artefact and the *build pipeline* details to a follow-on M0 ADR. This is that ADR.

Per tier3-human-review.md §Pending decisions, this ADR resolves:

- **T3-001** — Iosevka release version pin.

It also commits version pins for Iosevka Aile (downloaded from the Iosevka release; same version as IDS Mono by construction), Symbols Nerd Font Mono (per [ADR-0030 §SD12](./0030-imzero2-design-system-typography.md)), and notes the policy for the fallback-ladder fonts (Inter and Onest — *not* pre-shipped; downloaded only if the §SD10 swap trigger fires).

This ADR follows the **ADR-0028 / ADR-0033 M0 pattern**: the design contract — which versions, which build container, which build TOML, which SHA-verification scheme — lands proposed; the *execution results* (specific version tags, computed SHA-256s, codepoint audit log, any variant-name corrections vs [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md)) land as a dated Amendment after the M0b execution PR runs the build.

## Design space (QOC)

**Question 1.** Which Iosevka release should IDS pin?

**Options.**

- **O1a — Latest stable Iosevka release as of M0 execution date (chosen).** Iosevka has been stable in the v32.x line through early 2026 and continues to receive bug fixes; the variant TOML in [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md) was authored against this line. Track ahead unless a regression is known.
- **O1b — Specific known-good earlier version (e.g., v30.0).** Predates some recent variant renames; safer for the literal TOML keys in ADR-0030 §SD1 but locks us out of bug fixes and any minor improvements.
- **O1c — Build from `main` branch.** Non-deterministic; rejected at intake — defeats SHA pinning.
- **O1d — Use a pre-built Iosevka stylistic-set variant (SS01–SS18).** Already rejected by [ADR-0030 §Alternatives](./0030-imzero2-design-system-typography.md) — they're imitations of famous mono fonts (SS08 ≈ Pragmata, SS14 ≈ JetBrains Mono, SS15 ≈ Plex), defeating IDS's unique-expression goal.

**Question 2.** Which Nerd Fonts release should IDS pin for the icon fallback family?

**Options.**

- **O2a — Latest stable Nerd Fonts v3.x (chosen).** v3 introduced the icon-only variant (`NerdFontsSymbolsOnly` / "Symbols Nerd Font Mono") that [ADR-0030 §SD12](./0030-imzero2-design-system-typography.md) commits to.
- **O2b — Earlier Nerd Fonts v2.x.** Pre-dates the icon-only variant; we'd be forced to ship a full patched mono (FiraCode Nerd Font etc.) and lose IDS Mono customisation. Rejected.

**Question 3.** Should the proportional fallback fonts (Inter, Onest) be pre-shipped in M0?

**Options.**

- **O3a — No; download only when the §SD10 swap trigger fires (chosen).** Keeps the v1 binary lean (~6.1 MB total per [ADR-0030 §SD7](./0030-imzero2-design-system-typography.md), not ~8 MB if Inter + Onest were embedded prophylactically).
- **O3b — Yes; pre-ship Inter and Onest** so the swap is one-line and immediate. Adds ~2 MB to every binary for a swap that may never fire.

**Criteria.**

- **C1 — Reproducibility.** SHA-pinned artefacts; CI-verifiable.
- **C2 — Binary footprint.** Bytes per app.
- **C3 — Build determinism.** Same input → same `.ttf` output across machines.
- **C4 — Stability of the upstream surface.** Glyph stability, codepoint stability, release-process maturity.
- **C5 — Swap-readiness.** When a fallback trigger fires, how fast can we recover?
- **C6 — IDS variant-TOML compatibility.** Does the pinned version respect the variant names in [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md), or do we need rename corrections?

**Assessment.**

|    | O1a | O1b | O2a | O2b | O3a (chosen) | O3b |
|----|----|----|----|----|------|----|
| C1 | ++ | ++ | ++ | ++ | ++   | ++ |
| C2 |    |    |    |    | ++   | −  |
| C3 | ++ | ++ | ++ | ++ |      |    |
| C4 | +  | ++ | +  | −  |      |    |
| C5 |    |    |    |    | +    | ++ |
| C6 | +  | +  |    |    |      |    |

O1a (latest stable Iosevka) wins on C4 with one caveat: variant names may have renamed since ADR-0030 was authored (this is exactly the situation [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md) flagged with "exact TOML key names should be verified against the Iosevka customizer for the pinned release version"). The M0b execution either confirms the TOML works as written or lands rename corrections in the Amendment. O2a is forced by the icon-only variant requirement. O3a is forced by binary-footprint discipline; the swap-readiness gap is small because the fallback PR can be prepared in a stash.

## Decision

We will:

- Pin **Iosevka** (mono base + Aile proportional sibling) to the **latest stable v32.x or v33.x release** at the M0 execution date. The specific tag (e.g., `v32.5.0`) lands in the Amendment.
- Pin **Nerd Fonts** to the **latest stable v3.x release** for the Symbols Nerd Font Mono icon-only variant. The specific tag lands in the Amendment.
- **Not pre-ship Inter or Onest.** The proportional fallback ladder per [ADR-0030 §SD10](./0030-imzero2-design-system-typography.md) downloads its binary only when the swap trigger fires; the fallback PR ships the binary + SHA pin together.
- Build IDS Mono via the **`iosevkadocker/build` container** (tag-pinned per [ADR-0030 §Status Q6](./0030-imzero2-design-system-typography.md); image SHA pinning deferred to T3 if drift incident occurs).
- Verify all committed font binaries via per-directory `SHA256SUMS` files; CI fails on drift.
- Audit Symbols Nerd Font Mono codepoints against the patterns/iconography.md catalogue once at M0b; commit the audit log to the Amendment.

## Subsidiary design decisions

### SD1 — IDS Mono build TOML (commit verbatim from ADR-0030 §SD1)

The TOML lives at `rust/imzero2/assets/fonts/ids-mono/private-build-plans.toml`:

```toml
# iosevka-release: <pinned-version>   # filled in M0b Amendment
# customizer-checked: <date>           # M0b: confirms variant names against the pinned customizer

[buildPlans.IDSMono]
family = "IDS Mono"
spacing = "term"
serifs = "sans"
noCvSs = false
noLigation = true

[buildPlans.IDSMono.variants.design]
g = "single-storey-earless-corner"
zero = "dotted"
asterisk = "hex-high"
at = "fourfold"
brace = "curly"
six = "open-contour"
nine = "open-contour"
capital-i = "serifed"
```

**Variant-name verification.** [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md) explicitly flags that "exact TOML key names should be verified against the Iosevka customizer for the pinned release version (the Iosevka project occasionally renames variants between releases)." The M0b execution opens the pinned-version customizer and confirms each key. If renames are needed, they land in the M0b Amendment as a corrected TOML; the design intent (single-storey g, dotted 0, serifed I, etc.) is preserved per [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md), only the key names update.

### SD2 — IDS Mono build pipeline

> **Originally specified a local Docker build (`iosevkadocker/build`) writing into `assets/fonts/ids-mono/`. Superseded by the [§SD2 pivot Amendment](#sd2-pivot--docker-on-ci-not-on-contributor-machine) below — the build now lives in `ids-fonts` CI.**

Current shape (post-pivot):

1. `ids-fonts/` CI clones Iosevka at the pinned `IOSEVKA_VERSION`, applies the IDS Mono build plan from `private-build-plans.toml`, and runs `npm run build -- ttf::IDSMono` on `ubuntu-24.04` with `ttfautohint` / `p7zip-full` / `python3` from apt + Node.js 22.
2. On `v*` tag push, the release attaches six `.ttf` files, `SHA256SUMS`, the build TOML, `IOSEVKA_VERSION`, and the OFL `LICENSE` to a versioned URL: `https://github.com/stergiotis/ids-fonts/releases/download/<tag>/`.
3. pebble2impl re-vendors by `curl` + `sha256sum -c` into `rust/imzero2/assets/fonts/ids-mono/`.

Output: six `.ttf` files (Regular, Italic, Medium, MediumItalic, Bold, BoldItalic). No Docker, Node.js, or font toolchain required on pebble2impl contributor machines.

**Container tag pinning.** [ADR-0030 §Status Q6](./0030-imzero2-design-system-typography.md) frames the tag-vs-SHA choice; M0 ships with **tag-based pinning** (`iosevkadocker/build:latest` or a specific dated tag like `iosevkadocker/build:2026-04`). SHA-based pinning is reserved for the build-drift trigger (T3-006) — same rationale as the Iosevka version pin: bug fixes flow through latest-tag unless drift surfaces.

### SD3 — Iosevka Aile binary download

Aile is published as part of the Iosevka release tarball, *not* built locally. Download from:

```
https://github.com/be5invis/Iosevka/releases/download/<version>/PkgWebFont-IosevkaAile-<version>.zip
```

Subset extracted (matches [ADR-0030 §SD2](./0030-imzero2-design-system-typography.md) weight requirements): eight `.ttf` files (Regular, Italic, Medium, MediumItalic, SemiBold, SemiBoldItalic, Bold, BoldItalic) at ~200 KB each. Committed to `rust/imzero2/assets/fonts/iosevka-aile/`.

Aile's version is bound to the Iosevka version (same release tarball); the Iosevka pin from §SD1 transitively pins Aile.

### SD4 — Symbols Nerd Font Mono binary download

Per [ADR-0030 §SD12](./0030-imzero2-design-system-typography.md) the icon-only variant. Download from:

```
https://github.com/ryanoasis/nerd-fonts/releases/download/<v3.x.x>/NerdFontsSymbolsOnly.zip
```

Subset extracted: `SymbolsNerdFontMono-Regular.ttf` (~3 MB). Committed to `rust/imzero2/assets/fonts/symbols-nerd-font-mono/`.

**Codepoint audit.** [ADR-0030 §SD12](./0030-imzero2-design-system-typography.md) notes that Nerd Fonts occasionally renumber individual glyphs between major versions. The M0b execution runs a one-time audit:

- For each catalogue entry in patterns/iconography.md, render the glyph at the proposed codepoint using the pinned Symbols Nerd Font Mono.
- Visually confirm the glyph matches the intended meaning (or programmatically — extract the glyph name from the font's glyph table and compare to the catalogue label).
- For any mismatch, either update the catalogue codepoint or pick a different glyph.

The audit log (mismatch count, fixes applied) lands in the M0b Amendment alongside the SHA pins.

### SD5 — `SHA256SUMS` per-directory verification

Each font directory under `rust/imzero2/assets/fonts/` carries a `SHA256SUMS` file:

```
# rust/imzero2/assets/fonts/ids-mono/SHA256SUMS
abc123…  IDSMono-Regular.ttf
def456…  IDSMono-Italic.ttf
…
```

CI verification: `scripts/ci/lint.sh` includes a step that runs `sha256sum -c` over each `SHA256SUMS` file. Drift fails the build with a structured error naming the changed file and its expected vs observed SHA.

### SD6 — Inter and Onest: fallback-trigger-gated, not pre-shipped

Per [ADR-0030 §SD10](./0030-imzero2-design-system-typography.md), the proportional fallback ladder is **Aile → Onest → Inter**. The §SD10 trigger fires when Aile fails real-world hinting at small sizes (≥ 2 OS / DPI combinations show measurable artefacts, or Tier 2 V7 finding).

When the trigger fires, a Tier 3 follow-on ADR (probably ADR-003N for N ≥ 35):

- Downloads Onest from `https://github.com/TabularType/Onest/releases/<version>` and commits the binaries + `SHA256SUMS` to `rust/imzero2/assets/fonts/onest/`.
- Updates `Body.family` / `Caption.family` token references from `iosevka-aile` to `onest`.
- Lands as an Amendment to ADR-0030 with a screenshot diff.
- If Onest *also* fails (rare), a second Amendment swaps to Inter via `https://github.com/rsms/inter/releases/<v4.x.x>`.

M0 does *not* pre-ship Inter or Onest. Total binary footprint stays at ~6.1 MB per [ADR-0030 §SD7](./0030-imzero2-design-system-typography.md).

### SD7 — Repo layout (committed artefacts)

```
rust/imzero2/assets/fonts/
├── ids-mono/
│   ├── BUILD.md                       # how-to: bump the upstream pin
│   ├── IDSMono-Regular.ttf            # release artefacts from ids-fonts <tag>
│   ├── IDSMono-Italic.ttf
│   ├── IDSMono-Medium.ttf
│   ├── IDSMono-MediumItalic.ttf
│   ├── IDSMono-Bold.ttf
│   ├── IDSMono-BoldItalic.ttf
│   ├── LICENSE                        # OFL 1.1 — must travel with fonts (OFL §2)
│   └── SHA256SUMS                     # CI verifies these
├── iosevka-aile/
│   ├── IosevkaAile-Regular.ttf        # downloaded from Iosevka release; no local rebuild
│   ├── IosevkaAile-Italic.ttf
│   ├── IosevkaAile-Medium.ttf
│   ├── IosevkaAile-MediumItalic.ttf
│   ├── IosevkaAile-SemiBold.ttf
│   ├── IosevkaAile-SemiBoldItalic.ttf
│   ├── IosevkaAile-Bold.ttf
│   ├── IosevkaAile-BoldItalic.ttf
│   └── SHA256SUMS
└── symbols-nerd-font-mono/
    ├── SymbolsNerdFontMono-Regular.ttf  # downloaded from Nerd Fonts release
    ├── codepoint-audit.md               # one-time audit log (M0b)
    └── SHA256SUMS
```

Onest and Inter directories appear *only* when the fallback trigger fires.

### SD8 — `include_bytes!` wiring (recap; no decision here)

Per [ADR-0030 §SD7](./0030-imzero2-design-system-typography.md), the Rust side embeds via `include_bytes!`:

```rust
const IOSEVKA_IDS_REGULAR: &[u8] = include_bytes!(
    "../assets/fonts/ids-mono/IDSMono-Regular.ttf"
);
// ... one const per (family, style)
```

This ADR doesn't re-decide the embed strategy; it just *materialises* the bytes the next-token-binding work consumes.

### SD9 — Phasing

> **Landed as of 2026-06-08:** M0a/M0b — Iosevka pin, SHA-pinned IDS Mono binaries, build pipeline. M1 (token wiring) belongs to ADR-0030.

- **M0a — Design + scaffolding (this ADR's proposed scope).** Land the build TOML + Makefile target + the empty per-directory layout (no `.ttf` files yet) + the `SHA256SUMS` verification step in `scripts/ci/lint.sh`.
- **M0b — Execute + Amend (follow-on PR).** Pin specific Iosevka, Nerd Fonts versions. Run the Iosevka build container. Download Aile + Symbols Nerd Font Mono. Compute SHA-256s. Run codepoint audit. Verify TOML variant names against the customizer (and patch if any renamed). Land all artefacts in one PR with an Amendment to this ADR.
- **M1 — Wire into apps** (resumes [ADR-0030](./0030-imzero2-design-system-typography.md) M1 phasing). Pilot app (carousel) reads the embedded bytes via `FontDefinitions`.

M0a is implementable immediately on acceptance; M0b is gated on running the build + downloads; M1 belongs to the parent ADR-0030.

## Alternatives

- **Build Iosevka from `main` branch** (O1c). Rejected at intake; non-deterministic.
- **Use a pre-built Iosevka SS variant** (O1d). Already rejected by [ADR-0030 §Alternatives](./0030-imzero2-design-system-typography.md) for inheriting the "looks like \[famous font\]" critique.
- **Pin a specific known-good earlier Iosevka version** (O1b). Considered for stability; rejected because the latest stable is well-tested and the build-drift trigger (T3-006) handles regressions if they appear.
- **Bundle the full Nerd Fonts package** (O2b alternative). Rejected on binary footprint — 12 MB vs ~3 MB for the icon-only variant.
- **Pre-ship Inter / Onest as part of M0** (O3b). Rejected on binary footprint; the fallback PR ships its own binary when the trigger fires.
- **System-font fallback for the fallback ladder** (e.g., "use system Helvetica if Aile fails"). Rejected per [ADR-0030 §Alternatives](./0030-imzero2-design-system-typography.md) "System fonts via `fontconfig` lookup" — loses screenshot reproducibility, which is the whole point of embedding.
- **Pinning the `iosevkadocker/build` container by SHA in M0** (resolves T3-006 eagerly). Rejected; defer until a drift incident actually surfaces. Tag-based pinning is acceptable for v1.
- **Embedding the full Iosevka variable-axis file instead of discrete weights** (resolves [ADR-0030 §Status Q5](./0030-imzero2-design-system-typography.md) eagerly). Rejected; [ADR-0030](./0030-imzero2-design-system-typography.md) commits to discrete weights for v1 (FontDefinitions simplicity), variable-axis is a deferred T3 question.

## Consequences

### Positive

- **All font artefacts SHA-pinned and CI-verified.** Drift fails the build; supply-chain reasoning is straightforward.
- **Resolves T3-001** (Iosevka version pin) once accepted.
- **Aligns with [ADR-0030 §SD8](./0030-imzero2-design-system-typography.md) screenshot reproducibility.** Pinned bytes mean pinned screenshots; Tier 2 LLM grading remains pixel-stable across CI runs and contributor machines.
- **M0a is small.** Build TOML + Makefile + SHA verification step; most of the work is data wrangling at M0b.
- **Variant-name verification surfaces drift early.** If [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md) TOML keys have renamed in newer Iosevka, M0b catches it before M1 wiring.
- **Binary footprint stays at ~6.1 MB.** Fallback fonts are downloaded only when needed.

### Negative

- **Iosevka build takes tens of minutes** in the Docker container. Mitigation: most contributors consume committed `.ttf`s; the `make fonts-ids-mono` target is opt-in.
- **Tag-based container pinning leaves room for `latest` drift.** Mitigation: T3-006 triggers SHA pinning on actual drift evidence.
- **Aile is bound to the Iosevka version.** A bump to Iosevka also bumps Aile; both `SHA256SUMS` files re-emit. Acceptable cost; coupling matches the upstream release shape.
- **Codepoint audit is a one-time-per-NerdFont-bump cost.** Mitigation: Tier 3 escalation if Nerd Fonts renumbers heavily; rare event.
- **PragmataPro (per [ADR-0030 §SD11](./0030-imzero2-design-system-typography.md)) is user-installed, not version-pinned.** This is by design — the override is a personal choice, not an IDS-shipped surface. Documented; no action.

### Neutral

- **`SHA256SUMS` is the public-stability surface for font bytes.** Re-emitting fonts requires a coordinated PR (new bytes + new SHAs).
- **The build-TOML header carries machine-readable provenance.** `# iosevka-release: v32.5.0` is parsed by no tool today; it could be in the future (a future Tier 3 lint rule).
- **Three font directories created at M0.** Onest and Inter directories appear only when the fallback ladder fires.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). M0a/M0b have shipped — the Iosevka pin, the SHA-pinned IDS Mono binaries (`rust/imzero2/assets/fonts/ids-mono/`), and the build pipeline. M1 (token-path wiring) belongs to ADR-0030. The open questions below are version-pin refinements.

Open questions:

1. **Specific Iosevka release version.** Latest stable as of M0b execution; lands in the Amendment. Re-pinned on every Iosevka bump (Tier 3 amendment).
2. **Specific Nerd Fonts release version.** Latest v3.x as of M0b execution.
3. **Variant-name corrections vs [ADR-0030 §SD1](./0030-imzero2-design-system-typography.md) TOML.** Confirmed at M0b customizer check; corrections land in the Amendment if any.
4. **Codepoint audit results.** Per-glyph match log lands in the Amendment.
5. **`iosevkadocker/build` image tag.** Latest stable at M0b; SHA pinning deferred per T3-006.
6. **Should the `BUILD.md` in `ids-mono/` include a quick-start for non-Docker contributors?** Defer — Docker is the supported path; non-Docker is on the contributor's risk.
7. **Pre-fetch artefact-cache.** Should `make fonts-ids-mono` also pre-fetch Aile + Nerd Fonts so contributors can re-validate the full set in one command? Defer to M0b — the value depends on observed contributor friction.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-16 — M0b execution: pins, downloads, and an architectural pivot

Closes §SD9 M0b. Lands the pinned version + downloaded artefacts the parent ADRs need, plus one architectural change ([build pipeline moves to its own CI-driven repo](#sd2-pivot-docker-on-ci-not-on-contributor-machine)) and one quantitative finding that the proposed text didn't anticipate ([Iosevka v34.x TTF size is ~13× the §SD7 estimate](#sd7-finding-aile-bundle-budget-overrun)).

**T3-001 resolved.** Iosevka version pinned at **v34.5.0** (latest stable at execution date). The TOML's `g = single-storey-earless-corner` / `zero = dotted` / `capital-i = serifed` / `six = open-contour` / `nine = open-contour` / `asterisk = high` / `at = fourfold` / `brace = curly` picks were *not* verified against the v34.5.0 customizer at M0b — the verification was deferred to first CI build (see §SD2-pivot below). If a variant renamed between the ADR's authoring and v34.5.0 the CI build fails with a clear error and the TOML gets patched in lockstep.

**Nerd Fonts pinned at v3.4.0** (latest stable). Symbols Nerd Font Mono (icon-only variant per §SD12) downloaded from `github.com/ryanoasis/nerd-fonts/releases/download/v3.4.0/NerdFontsSymbolsOnly.zip`.

**Aile downloaded but bundle reduced.** Iosevka Aile downloaded from `github.com/be5invis/Iosevka/releases/download/v34.5.0/PkgTTF-IosevkaAile-34.5.0.zip` — see [§SD7 finding](#sd7-finding-aile-bundle-budget-overrun) for why only Regular landed.

**SHA-pinned bytes shipped in `pebble2impl`** at `rust/imzero2/assets/fonts/`:

| File | SHA-256 |
|---|---|
| `iosevka-aile/IosevkaAile-Regular.ttf` | `e1e31be1c6a1db5be00b0645261700539d13a98e08c315413fd9567f3670eb7a` |
| `symbols-nerd-font-mono/SymbolsNerdFontMono-Regular.ttf` | `f0f624d9b474bea1662cf7e862d44aebe1ae1f6c7f9cb7a0ca5d0e5ac9561c60` |

`scripts/ci/lint.sh` gained an `ids font SHA256SUMS` step that runs `sha256sum -c` against each per-directory `SHA256SUMS`; drift fails the build.

#### SD2 pivot — Docker on CI, not on contributor machine

§SD2 specified a Docker-based build (`iosevkadocker/build`) on the contributor's machine, with the resulting `.ttf` files committed back into `pebble2impl/assets/fonts/ids-mono/`. M0b discovered three problems with that shape:

1. **Contributor friction.** Asking every contributor who touches `private-build-plans.toml` to install Docker (or the alternative npm+premake5+ttfautohint chain) is heavyweight for a font build.
2. **Iosevka build deps churn.** The `iosevkadocker/build` image referenced by §SD2 was already showing signs of being stale relative to upstream Iosevka's current build instructions.
3. **Multi-font scope.** The IDS will plausibly want a custom proportional later (Aile fallback, Phase 2 Nerd-font-merged variant). Replicating the Docker dance for each new font is per-typeface friction.

Resolved by **moving the build to its own CI-driven repository: `github.com/stergiotis/ids-fonts`** (initial commits `f4bcf73` + `2ebf3de`). The new repo houses:

- The IDS `private-build-plans.toml` (single source of truth — *not* duplicated into pebble2impl).
- An `IOSEVKA_VERSION` file pinning the upstream release; CI clones Iosevka at that tag.
- `.github/workflows/build.yml` — runs the build on `ubuntu-24.04` with `ttfautohint` / `p7zip-full` / `python3` from apt + Node.js 22 from the official action. On every push to `main` it smoke-tests (artifact retention 30 days); on `v*` tag push it creates a GitHub release with the six TTFs, `SHA256SUMS`, the build TOML, and `IOSEVKA_VERSION` attached.
- Phase-2 scaffolding for the Nerd-Font-merged variant: `icons.toml` placeholder + `scripts/subset-nerd.sh` + `scripts/patch.sh` stubs.
- License: SIL OFL 1.1 (matches upstream Iosevka), Reserved Font Name "IDS Mono".

**What this means for §SD2 / §SD7 of this ADR:**
- Local Docker build is no longer the supported path. The `Makefile` target `fonts-ids-mono` in pebble2impl was **deleted outright** (along with the whole `Makefile`) once the §SD2 pivot landed — keeping a half-broken shortcut pointing at an abandoned-looking image was worse than no shortcut. Emergency rebuilds run against `../ids-fonts/Makefile` (`make build`) directly.
- The contributor flow becomes: bump `IOSEVKA_VERSION` in `ids-fonts/`, push a tag, CI publishes a release, pebble2impl bumps its `SHA256SUMS` to the new release URL. No Docker on any contributor machine.
- `iosevkadocker/build` reference in §SD2 / §SD9 + open question 5 about the image tag are obsolete — the *new* CI uses npm+apt directly, not the abandoned-looking docker image.

**IDS Mono bytes are NOT yet shipped in pebble2impl.** The ids-fonts repo is scaffolded but the first release tag hasn't been pushed; the `.ttf` files will land in pebble2impl via a follow-up PR that consumes the release URL. Until then, apps that set `IMZERO2_IDS_FONTS=on` fall back to egui's default mono in monospace contexts (only Aile + Symbols Nerd Font Mono are wired in).

#### SD7 finding — Aile bundle budget overrun

§SD7 estimated the Aile bundle at **8 styles × ~200 KB ≈ 1.6 MB**. Actual against v34.5.0: **8 styles × ~10 MB ≈ 80 MB**. The published Iosevka Aile TTFs grew dramatically — likely because of expanded CJK / Cyrillic / Latin Extended coverage between the ADR's authoring assumptions and current Iosevka releases.

To stay within reasonable repo-bloat bounds, M0b shipped **Regular only** (10 MB committed). The other seven weights (Italic / Medium / MediumItalic / SemiBold / SemiBoldItalic / Bold / BoldItalic) are deferred pending one of:

- **Subsetting decision.** `pyftsubset` the upstream TTFs to the codepoints IDS apps actually use (Latin Basic + Extended-A + Cyrillic Basic + common punctuation + the Nerd Font codepoints). Plausibly drops each style from ~10 MB to ~500-700 KB, bringing the 8-weight bundle to ~5 MB — close to §SD7's original budget. Belongs in `ids-fonts` rather than pebble2impl, naturally pairs with Phase 2 of that repo.
- **Git LFS adoption.** Move `assets/fonts/**` to LFS so the full 80 MB doesn't grow `.git`. Acceptable but doesn't address the per-binary `include_bytes!` cost (every consumer binary gains 80 MB).
- **Acceptance.** Ship 8 weights × 10 MB. Repo grows ~80 MB permanently; demo binary grows 80 MB.

The right call is subsetting in `ids-fonts`. Tracked as a Phase 2 prerequisite there. Until then, the type-scale's Italic / Medium / SemiBold / Bold tokens fall back through egui's font-matching to the Regular weight; the visible cost is no true italic / true bold rendering, only weight-faking. Acceptable for early IDS adoption; not acceptable for the polished final state.

#### Other open questions

- **Codepoint audit** (Open Question 4 + §SD4) **not executed at M0b.** The `Symbols Nerd Font Mono v3.4.0` bytes are SHA-pinned; the per-glyph match log against the IDS icon catalogue is deferred. The Phase-2 path in `ids-fonts` (`icons.toml` as machine-readable manifest, CI subsets + patches) makes the audit a build-time artefact rather than a one-shot M0b deliverable — preferable.
- **TOML variant-name verification** (Open Question 3) **deferred to first CI build** in `ids-fonts`. If any of the eight `variants.design` picks renamed between the §SD1 authoring and v34.5.0, CI fails fast with the unknown-key error and the TOML gets patched.
- **`iosevkadocker/build` SHA pinning** (Open Question 5, T3-006) **obsoleted by §SD2 pivot above.** The new ids-fonts CI uses apt + npm directly; container SHA pinning no longer applies.
- **Pre-fetch artefact cache** (Open Question 7) **resolved by §SD2 pivot.** The release URL *is* the pre-fetch artefact cache; contributors `curl` from a versioned URL.

`status` stays `proposed` per the ADR convention. This amendment refines execution detail and records the §SD2 pivot; the human-review flip to `accepted` is owed (a notable share of the original §SD2 specification is replaced by the ids-fonts repo arrangement, so the review is more substantive than a typical post-M0b sign-off).

### 2026-05-17 — family rename, filename normalization, Rust-side assets lift

Three follow-up changes inside ~24h of the M0b pivot Amendment above; together they bring `ids-fonts` to its first usable consumer-side state and land the M1 wiring this ADR's §SD9 phasing flagged.

**1. Family rename: "Iosevka IDS" → "IDS Mono"** (`ids-fonts v0.1.0`).

The original family name contained upstream Iosevka's Reserved Font Name. A strict reading of SIL OFL §3 + FAQ §3.6 treats any `Iosevka <X>` compound name as RFN infringement on derivative works. Iosevka's author tolerates customizer-style naming in practice, but tolerance is not license; for clean public redistribution the rename eliminates the ambiguity.

- New family: **`IDS Mono`** (filename slug `IDSMono`, TOML build-plan key `[buildPlans.IDSMono]`).
- LICENSE in `ids-fonts` updated to declare RFN as `"IDS Mono"`. Upstream Iosevka attribution preserved per OFL §4.
- Phase-2 family will be `IDS Mono Nerd Font` when the [`icons.toml`](https://github.com/stergiotis/ids-fonts/blob/main/icons.toml) subset-and-patch pipeline ships.

**2. Filename normalization** (`ids-fonts v0.1.1`).

`v0.1.0` shipped 12 TTFs with non-conventional naming (`IDSMono-regularupright.ttf`, `IDSMono-Extendedbolditalic.ttf`). Two upstream build-plan defects:

- TOML table keys were lowercase (`weights.regular`, `slopes.upright`) — Iosevka treats these verbatim as filename-suffix components, so capitalization controls the output shape.
- No `widths.Normal` block, so all default widths built (Normal + Extended → 12 TTFs instead of 6).

Fixed at `v0.1.1`: capitalized keys (`weights.Regular`, etc.) plus an explicit `widths.Normal` block. Output is the conventional six `IDSMono-{Regular,Italic,Medium,MediumItalic,Bold,BoldItalic}.ttf` files (~56 MB total, half the v0.1.0 size). **Skip v0.1.0 entirely**; pin v0.1.1 or later.

**3. Rust-side assets lift** (this PR, pebble2impl side).

Moved `rust/imzero2/imzero2_egui/assets/` → `rust/imzero2/assets/`. The fonts and color tokens are not egui-specific (TTFs, palette TOMLs, scientific colormap LUTs); they're project-wide Rust resources that any future crate can consume without depending on `imzero2_egui`. [ADR-0030 §SD7](./0030-imzero2-design-system-typography.md) and this ADR's §SD7 layout blocks updated; Go codegen paths (`gen.go`, `vendor.go`, `emit.go`) and `scripts/ci/lint.sh` SHA-verify path also updated.

**SHA-pinned bytes shipped** at `rust/imzero2/assets/fonts/ids-mono/`:

| File | SHA-256 |
|---|---|
| `IDSMono-Regular.ttf` | `70ee925069a35adce5fb2bea263e5734515153eaafdfd4b03c75fd5044146a5c` |
| `IDSMono-Italic.ttf` | `e4bfbd3f3f107c090cb16b1efa58ac1aef34e1b33689cb08737a162b6ef72376` |
| `IDSMono-Medium.ttf` | `76aa5521f6045f2b21633f8bf999068ce750b720bbae7b83f32fcbf3a8fb94cb` |
| `IDSMono-MediumItalic.ttf` | `0ad26ed055464125075b9c8d8419adc0c92424935ae30cfb3b4a077a357f0a70` |
| `IDSMono-Bold.ttf` | `b3d612e3b473a92a143015b102304292ceae06e70bc4851182ed02165f2f0227` |
| `IDSMono-BoldItalic.ttf` | `c0516ca39d2315e162f47a75a754c52d7b52fe93f307b89d1f519ecb64b74144` |
| `LICENSE` | OFL 1.1 — travels with the binaries per OFL §2 |

**M1 partial landing.** The `IDS_MONO_REGULAR` const + `install_fonts` Monospace primary registration in `imzero2_egui/src/style/tokens/typography.rs` closes the `Iosevka IDS bytes are NOT yet shipped in pebble2impl` line of the 2026-05-16 Amendment. Italic / Medium / Bold weights are vendored but not yet `include_bytes!`-embedded — same size-budget constraint as Aile (the §SD7-finding Amendment above).

**Status note.** The 2026-05-16 Amendment was authored when the family was still "Iosevka IDS"; its prose was updated in-place to "IDS Mono" for consistency with current naming. The architectural pivot it documents (`iosevkadocker/build` → `stergiotis/ids-fonts` CI) is unchanged in substance and date. This 2026-05-17 entry is the canonical source for the rename event itself.

## References

- [ADR-0030 — typography](./0030-imzero2-design-system-typography.md) — parent ADR; §SD1 commits the variant intent; §SD7 binary embed; §SD9 build pipeline; §SD10 fallback ladder; §SD12 NerdFont; §Status Q1 / Q6 open questions resolved by this ADR.
- [ADR-0029 — design system + policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — IDS framework; §SD13 hard performance invariant (no runtime presence — font bytes are embedded; loading is a startup cost).
- [ADR-0033 — IDS palette M0](./0033-imzero2-design-system-palette-m0.md) — sibling M0 ADR; same design-contract-then-execution-Amendment pattern reused here.
- [ADR-0028 — `ch.local.exec` low-latency cap](./0028-chlocal-low-latency-sql-cap.md) — M0 spike Amendment pattern (original precedent).
- tier3-human-review.md — resolves T3-001; partially anchors T3-006 (build-container SHA pinning deferred per trigger).
- INSPIRATIONS.md — attributions for Iosevka (Renzhi Li, OFL), Aile (same), Nerd Fonts (Ryan L. McIntyre + contributors, MIT), FontAwesome (free subset), Inter (Rasmus Andersson, OFL), Onest (Tabular Type Foundry, OFL).
- patterns/iconography.md — codepoint catalogue audited at M0b.
- [Iosevka GitHub releases](https://github.com/be5invis/Iosevka/releases) — source of IDS Mono build + Aile binary.
- [Iosevka customizer](https://typeof.net/Iosevka/customizer) — variant-name verification at M0b.
- [Nerd Fonts GitHub releases](https://github.com/ryanoasis/nerd-fonts/releases) — source of Symbols Nerd Font Mono.
- [`iosevkadocker/build` container](https://hub.docker.com/r/avivace/fontforge) — reference image for the build pipeline.
- [SIL Open Font License 1.1](https://openfontlicense.org/) — covers Iosevka, Aile, Inter, Onest.
