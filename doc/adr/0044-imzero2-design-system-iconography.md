---
type: adr
status: proposed
date: 2026-05-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

> **Amendment 2026-05-19 — Slot B removed; single-slot iconography.** The originally-proposed two-slot design (Phosphor for affordances + a subsetted Nerd Fonts blob "NFBrand" for brand marks) collapsed to a single Phosphor slot. The brand-mark slot served 10 glyphs total, 8 of which had no caller outside a single demo collapsible section; the remaining two (database, git-branch) had Phosphor equivalents and were reclassified as affordances. Dropping the second font also eliminates the awkward "NFBrand" name (introduced ad-hoc in §SD2 as originally written, never reviewed). Brand-mark glyphs that Phosphor does ship (Linux, GitHub, Apple, Google, …) remain available via the generated `PhXxxLogo` constants; the rest (Rust, Docker, Go, JavaScript, …) are gone — apps that need them reach for plain text. §SD2 is superseded by this amendment in full; §SD3/§SD5/§SD6/§SD8 references to "Slot B" / "NFBrand" / "nerd-font" are obsoleted; the keep-the-slot-name decision recorded in M1 prose is also obsoleted (the `--nerdFontTTF` flag + `nerd_font_ttf` AppConfig slot + egui `nerdfont` family name + `NERD_FONT` env var are all removed).

# ADR-0044: ImZero2 design system — iconography (Phosphor affordances + Devicon brand marks)

## Context

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD5 defers the iconography choice; [ADR-0030](./0030-imzero2-design-system-typography.md) §SD12 picks **Symbols Nerd Font Mono** as a fallback family covering "arrows, status, file-types, tool indicators, file-system glyphs, Powerline separators" via the egui font-fallback chain, and promises a `patterns/iconography.md` catalogue documenting which IDS-conformant apps use which glyphs for which meanings. That catalogue was never written, and a survey of actual usage surfaces two problems with §SD12 as it stands.

**Problem 1 — the ratio of payload to use.** A single `grep -rohE "nf\.[A-Z]\w+" src/go apps | sort -u` enumerates the universe of icon references the project actually makes: **44 unique <code>nf.\*</code> identifiers**, collapsing to **43 distinct semantic icons** after merging `nf.Check ≡ nf.CodCheck`. ADR-0030 §SD12 ships approximately **3 MB / >10 000 glyphs** to serve those 43 — a ~230× over-provision in glyph count and a ~4× over-provision in binary bytes relative to a focused subset built for the same purpose.

**Problem 2 — visual incoherence within the affordance set.** The 43 icons in use today span **five distinct upstream visual languages**:

| Upstream source         | Count | Style character                                |
|-------------------------|-------|------------------------------------------------|
| Codicons (Microsoft)    | 31    | VSCode-flavoured, mostly-filled, 16 px grid    |
| Devicons                | 8     | Brand marks, each in its own visual treatment  |
| Material Design Icons   | 2     | Google Material, 24 px grid, geometric         |
| FontAwesome             | 1     | FA Solid, heavier-weight filled                |
| Nerd Fonts Custom       | 1     | Per-glyph individual treatments                |

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md)'s Context fourth force commits IDS to a *Swiss-minimalist + scientific* aesthetic with restrained chroma, strong grid, and weight-driven hierarchy. Five competing visual languages within the same affordance row of the same panel fight that aesthetic — and would only be papered over by writing a `patterns/iconography.md` catalogue that prescribes one specific glyph per role from the messy aggregate. That is enforcement of conformity over a non-coherent source, which is fragile (every new icon adds a fresh visual-language risk) and self-defeating relative to ADR-0029's thesis.

**The usage data also reveals a clean functional split**, which is the load-bearing observation behind this ADR's decision:

- **33 UI affordances** (chevrons, status, file ops, charts, gear, save, search…) — the slot that needs visual coherence. Sourced today from Codicons (31), FontAwesome (1), MaterialDesign (1).
- **10 brand / language marks** (Go, Rust, Python, Docker, Linux, JS, Git, Database, FontAwesome-as-mark, Custom-Go) — the slot where heterogeneity is *correct*. The Rust crab and the Go gopher should look like themselves, not like each other. Sourced today from Devicons (8), MaterialDesign-brand (1), Custom (1).

A side-by-side rendering of all 43 icons in Codicons (current), Phosphor Regular, Lucide, and Tabler — scaled to IDS Body (≈ 18 px) and Caption (≈ 14 px) per ADR-0030 §SD3 — is published at `iconset-comparison.html` and was the primary input to the §SD1 family pick below.

ADR-0030's other §SD12 commitments (icon-only variant, embed-at-startup, OFL/MIT clearance, ~3 MB budget envelope) are *kept in spirit* by this ADR — only the family choice changes.

## Design space (QOC)

**Question.** Which icon family or families best serve a data-intensive, scientific-feeling UI under ADR-0029's Swiss-minimalist thesis, given the actual icon usage in the project (33 UI affordances + 10 brand marks) and ADR-0030's embed-at-startup invariant?

**Options.** Two axes interact: *how many slots* and *which family fills each slot*.

- **O1 — One coherent family for everything (Lucide).** ~1500 icons, single weight, 2 px stroke, ISC license. SVG-first; community `lucide-static` ttf build needed for embedding. Loses all brand marks (Lucide ships almost none, intentionally).
- **O2 — One coherent family for everything (Phosphor Regular).** ~1500 icons, single weight default (6-weight axis available), ~1.5 px stroke, MIT. Official `.ttf` distribution via `@phosphor-icons/web`. Brand-mark coverage is partial: Python and Linux are present; Rust, Docker, Go-as-language are not.
- **O3 — One coherent family for everything (Tabler).** ~5000 icons, two styles (outline + filled), 2 px stroke, MIT. Best brand coverage via `tabler:brand-*` (~150 brands) but each brand re-rendered in Tabler's own visual treatment, losing canonical identity.
- **O4 — Two-slot split: Phosphor (affordances) + subsetted Devicons (brand marks).** UI-affordance slot covered by Phosphor Regular; brand-mark slot covered by a 100–200 KB subset of Nerd Fonts limited to Devicons + Logos + Custom glyphs. Each slot serves the role it is good at; the two together cover 100 % of current usage without forcing heterogeneity on the affordance row.
- **O5 — Custom-drawn IDS icon family.** Bespoke geometric set drawn from scratch by a designer, ~50–100 high-frequency affordances, with Devicons as the brand-mark slot. Maximum identity / uniqueness; maximum cost (months of contracted design work); reconsider only if Phosphor's identity becomes a recognised "shadcn icon" in the next two years.
- **O6 — Status quo: keep Symbols Nerd Font Mono.** Five visual languages, ~3 MB, ~230× over-provisioning. Documented for completeness.

**Criteria.**

- **C1 — Visual coherence on the affordance row.** A panel with `[search] [save] [database] [chart] [warning]` icons should read as one design intent, not as five.
- **C2 — Scientific-glyph coverage.** ADR-0029's "scientific" pole names glyphs like `function`, `sigma`, `waveform`, `flask`, `atom`, `dna`, `chart-scatter`. Does the family ship them?
- **C3 — Brand-mark coverage (Slot B).** Can the family render Go, Rust, Python, Docker, Linux, JavaScript, Git canonically?
- **C4 — Binary-size cost.** Embedded `.ttf` bytes added to the release binary.
- **C5 — Permissive license, air-gap-embeddable.** OFL / MIT / Apache-2.0; no CDN / network dependency.
- **C6 — Coherence with ADR-0029 / ADR-0030 aesthetic.** Swiss-minimalist, restrained, "crisp, professional, unique."
- **C7 — Long-term maintenance / drift risk.** How stable is the upstream? How likely are codepoints to renumber across versions?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (Lucide) | O2 (Phosphor) | O3 (Tabler) | **O4 (Phosphor + Devicons)** | O5 (Custom) | O6 (status quo) |
|----|-------------|---------------|-------------|------------------------------|-------------|-----------------|
| C1 | ++          | ++            | ++          | ++ (within Slot A)           | ++          | −−              |
| C2 | −           | ++            | +           | ++                           | +           | +               |
| C3 | −−          | −             | + (re-drawn) | ++ (canonical)              | ++          | ++              |
| C4 | + (~400 KB) | + (~600 KB)   | − (~1.4 MB) | + (~750 KB)                  | + (~200 KB) | −− (~3 MB)      |
| C5 | ++          | ++            | ++          | ++                           | ++ (own work) | ++           |
| C6 | +           | ++            | +           | ++                           | ++          | −               |
| C7 | ++          | ++            | +           | ++                           | − (one-bus)  | ++              |

O4 dominates the rows that ADR-0029 cares most about (C1 within the slot that needs it, C2 for the scientific pole, C3 for brand fidelity, C6 for aesthetic alignment) without giving up materially on the others. O2 alone is strictly worse than O4 — it abandons brand-mark fidelity for no gain in coherence (Phosphor's affordance set is identical between O2 and O4; only Slot B differs). O1 and O3 lose ground on C2 / C3. O5 is the long-game alternative; reconsider in 2027–2028 if Phosphor becomes the de-facto "shadcn icon" of frontend dev tools. O6 is the status quo and the entire reason this ADR exists.

## Decision

We will use:

- **Slot A — UI affordances**: **Phosphor Regular** (~600 KB embedded), the default weight of the Phosphor icon family. Variable / multi-weight registration is a future option (§SD4) but v1 ships Regular only. (§SD1)
- **Slot B — Brand / language marks**: a **subset of Nerd Fonts** limited to Devicons + Logos + Custom glyphs, subsetted from the existing ~3 MB blob to ~150 KB covering ~60 brand-mark codepoints. (§SD2)
- **No other icon families embedded.** Codicons, Material Design, FontAwesome (non-brand), Powerline, Pomicons, Weather Icons, IEC, Seti, Sete, Pl — none ship. The 33 affordances currently sourced from Codicons + Material + FontAwesome migrate to Phosphor; the single FA brand glyph and the Custom-Go glyph stay in Slot B's Nerd Fonts subset.

Both slots ship embedded in the binary via `include_bytes!` (Rust) / `embed.FS` (Go-side codepoint constants), pinned by SHA-256 plus upstream release version (§SD7), under the same air-gap and reproducibility invariants ADR-0030 §SD7–§SD8 establish for the text fonts.

This decision **supersedes [ADR-0030](./0030-imzero2-design-system-typography.md) §SD12** specifically. The rest of ADR-0030 (text-font choices, IDS Mono, Aile, PragmataPro override) is unaffected.

## Subsidiary design decisions

### SD1 — Slot A: Phosphor Regular

`Phosphor` is a single-author icon family by Helena Zhang and Tobias Fried (2020–present), shipping ~1500 icons across six weights (`thin` / `light` / `regular` / `bold` / `fill` / `duotone`) drawn from one design source. The `regular` weight is the default and our v1 pick.

Rationale, tied to Criteria:

- **C1 / C6.** Single design source → guaranteed stroke-weight, terminal, and proportion consistency across the affordance row. Geometric humanist character aligns with ADR-0029's Swiss-minimalist + scientific thesis without being a Helvetica clone (cf. ADR-0030 §SD2 on Aile's grotesque lineage). Stroke is ~1.5 px (vs Lucide's 2 px), more delicate at Caption sizes.
- **C2.** Phosphor is unusually strong on scientific glyphs: `function`, `sigma`, `waveform`, `flask`, `atom`, `dna`, `chart-scatter`, `binary`, `circuitry`, `radioactive`, `infinity`, `subset-of`, `superset-of`, `equals`, `not-equals`, `plus-minus`. Lucide and Tabler don't have most of these. This matters because IDS apps include `imztop` (sysmetrics), spinnaker (SQL play), and forthcoming time-series / grafana-replacement work — all of which want scientific affordances.
- **C4.** ~600 KB embedded for Regular weight only. The full six-weight set would be ~3.6 MB, comparable to the Nerd Fonts blob it replaces; v1 ships only Regular (§SD4).
- **C5.** MIT license. Official `.ttf` distribution at [@phosphor-icons/web](https://github.com/phosphor-icons/web), single file `Phosphor.ttf` for Regular.
- **C7.** Phosphor's codepoint table has been stable across the 2.x line. The project documents codepoints in `src/regular/style.css`; bumps are versioned and the codepoint deltas are part of the changelog.

Variant choice rationale (mirror of ADR-0030 §SD12's rationale, restated for Phosphor):

- **Regular weight** ships in v1. The `light` weight is reserved for a possible *density-driven* mapping per ADR-0032 (§SD4 below). Other weights stay unloaded — the binary cost is per-weight, and we cannot justify shipping six weights to use one.
- **Outline (not filled)** is the v1 default. Filled-variant glyphs (`ph-fill`) ship in a separate font file in Phosphor's distribution; v1 does not embed them. A small number of affordances currently use filled glyphs from Codicons (`circle-filled`, `error` filled, `play` filled, `pause` filled, `stop` filled, the debug-* family); v1 renders them as outline equivalents. If the visual loss is material at M1 testing, embedding `Phosphor-Fill.ttf` is a Tier 3 addition (§SD8).
- **No duotone, no thin.** These are aesthetic statements and would fight the Swiss-minimalist thesis at Caption / Micro sizes.

Build artifact location:

```
src/rust/assets/fonts/phosphor/
├── BUILD.md                  # how-to: bump the upstream pin
├── Phosphor.ttf              # Regular weight, downloaded from @phosphor-icons/web release
├── LICENSE                   # MIT — must travel with the font
└── SHA256SUMS                # CI verifies on every build
```

CI verification per ADR-0030 §SD7 — same script (`scripts/ci/lint.sh`) extended to include this directory.

### SD2 — Slot B: Subsetted Nerd Fonts (Devicons + Logos + Custom)

The brand-mark slot keeps a *subset* of the Nerd Fonts aggregation that originally backed ADR-0030 §SD12. The subset is limited to three of Nerd Fonts' constituent sets:

- **Devicons** — programming-language and developer-tool brand marks (Go, Rust, Python, JavaScript, Docker, Linux, Git, Java, Ruby, C, Cpp, …)
- **Logos** — additional brand marks not in Devicons
- **Custom** — Nerd Fonts' own one-offs (Custom-Go is the gopher mark currently in use)

Codepoint ranges retained (Nerd Fonts conventions):

| Set name in NF  | Unicode range          | Approx glyph count |
|-----------------|------------------------|--------------------|
| Devicons        | U+E700 – U+E7C5        | ~190               |
| Logos           | U+F300 – U+F372        | ~115               |
| Custom (subset) | U+E5FA – U+E631 subset | ~10 actually used  |

Everything else is stripped: Codicons (U+EA60–U+EC1E), Material Design Icons, FontAwesome (free), FontAwesome (extension), Powerline, Powerline Extra, IEC Power Symbols, Pomicons, Weather Icons, Seti-UI, Octicons, Font Logos, IndentationLines. None of these are referenced by current usage; all of them are bytes-on-the-wire today.

Subsetting approach:

- The subset is produced by [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts), the same repository ADR-0030 §SD9 already uses for IDS Mono. The driver is `scripts/subset-nerd.sh` in that repo, which uses [`pyftsubset`](https://fonttools.readthedocs.io/en/latest/subset/index.html) (Python fonttools) to strip the upstream `SymbolsNerdFontMono-Regular.ttf` down to the codepoint ranges declared in `icons.toml` (one entry per range above plus the one-off MaterialDesign FontAwesome codepoint).
- pebble2impl consumes the result as a SHA-pinned release artefact (`NFBrand.ttf`), same model as IDS Mono. Contributor machines never install `fonttools` — the toolchain lives upstream.
- Pin: `NERDFONTS_VERSION` in `stergiotis/ids-fonts` (currently `v3.4.0`).

Build artifact location (pebble2impl side, post-M1 wire-up):

```
src/rust/assets/fonts/nf-brand/
├── BUILD.md         # how-to: bump the upstream pin in stergiotis/ids-fonts
├── NFBrand.ttf      # subsetted, SHA-pinned (~150 KB)
├── LICENSE          # MIT (Nerd Fonts) — must travel
└── SHA256SUMS       # CI verifies NFBrand.ttf
```

The upstream `SymbolsNerdFontMono-Regular.ttf` is *not* vendored on pebble2impl — it lives only in ids-fonts CI staging (it is downloaded fresh during the build job and discarded after subsetting). Removing it from this repo eliminates ~3 MB of dead binary weight.

Size estimate: a 200-codepoint subset of a 10 000-glyph .ttf typically lands at 100–200 KB depending on glyph complexity; brand marks are visually richer than affordances, so the upper end is more likely. Empirical measurement during M1 will confirm.

### SD3 — Go-side icons package: `src/go/public/keelson/runtime/icons/`

The Go-side surface for icon constants moves out of `src/go/public/thestack/imzero2/nerdfont/` (the now-archived ~10 000-glyph generated package, see §SD8) into a new package under the keelson namespace (per [ADR-0035](./0035-keelson-namespace-introduction.md)):

```
src/go/public/keelson/runtime/icons/
├── doc.go                       # package overview; references this ADR
├── phosphor-icons.json          # VENDORED — from stergiotis/ids-fonts release (SHA-pinned)
├── SHA256SUMS                   # CI-verified on every build
├── phosphor.out.go              # GENERATED — ~1500 PhXxx constants + aliases
├── phosphor_lookup.out.go       # GENERATED — PhosphorByName map + PhosphorNames slice
├── affordances.out.go           # CURATED   — Slot A whitelist (33 IconXxx aliases to PhXxx)
├── brandmarks.out.go            # CURATED   — Slot B whitelist (10 IconXxx, NF-subset codepoints)
├── generate.sh                  # driver — invokes the iconsgen cmd
└── generator/
    ├── doc.go
    └── generator.go             # codegen library — reads phosphor-icons.json, emits phosphor*.out.go
```

The cmd binary lives at `src/go/cmd/iconsgen/` (project convention, cf. `cmd/keelsoncodec`, `cmd/runtimecodegen`, `cmd/envgen`); it is a thin urfave/cli wrapper over the `icons/generator` package.

Naming convention — two layers:

- **Generated layer** (`phosphor.out.go` + `phosphor_lookup.out.go`). Every Phosphor regular icon gets a `Ph<PascalName>` constant (`PhCheck`, `PhAcorn`, `PhHouseSimple`, …); any documented alias (the `alias` field in the upstream catalogue) gets its own `Ph<AliasPascalName>` constant pointing at the canonical one with a `// alias of PhX` comment. Names come straight from `pascal_name` in the upstream catalogue; codepoints from the `codepoint` integer rendered as `"\uXXXX"` string literals. The dynamic lookup file exposes `PhosphorByName map[string]string` (kebab-case → codepoint string) and `PhosphorNames []string` (sorted) — mirrors of the legacy `nerdfont.OriginalNames` / `nerdfont.CodePoints` surface, scoped to Phosphor.
- **Curated layer** (`affordances.out.go` + `brandmarks.out.go`). Each high-frequency icon gets an `Icon<Conceptual>` constant aliasing the generated `Ph<Name>` (or carrying an NF-subset codepoint literal for Slot B). The alias name is conceptual rather than literal — `IconChartBar` aliases `PhChartBar`, but `IconSliders` aliases `PhSlidersHorizontal` (because the legacy `nf.CodSettings` was conceptually "sliders, not gear"). Drift notes live in the comment header on each `IconXxx` constant; M2 call-site migration targets these curated names because they are stable across upstream renames in a way `PhXxx` is not.

Generation discipline:

- The vendored `phosphor-icons.json` is the source-of-truth for pebble2impl. The JSON is a release artefact of [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts) — that repo owns the upstream `@phosphor-icons/core` pin, downloads `src/icons.ts`, runs `scripts/ts-to-json.py` against it, and attaches the result alongside `Phosphor.ttf` to each tagged release. This split follows the discipline ADR-0030 §SD9 established for IDS Mono: contributor machines need only `curl + sha256sum -c`; the font/JSON toolchain lives upstream.
- The Go generator reads the vendored JSON via `encoding/json` and emits the two `.out.go` files. It consumes only four fields (`name`, `pascal_name`, `codepoint`, optional `alias`); all other catalogue metadata is dropped by the upstream TS→JSON step. If upstream renames any of the four fields, the JSON either omits the field (skipped entries surface as missing `PhXxx` constants) or fails to unmarshal (the generator exits non-zero with `unable to unmarshal phosphor-icons.json`) — both are the correct signal to re-evaluate the converter in `stergiotis/ids-fonts`.
- Regen is `bash src/go/public/keelson/runtime/icons/generate.sh` (out-of-band). The `.out.go` outputs are checked in per the project's `.out.go` convention.
- Bumping the upstream pin happens in *two* places, in order: first bump `PHOSPHOR_VERSION` in `stergiotis/ids-fonts` and tag a release; then re-vendor the new release's `phosphor-icons.json` here, regenerate `SHA256SUMS`, rerun `generate.sh`. Any net rename of an icon between versions surfaces as a removed `PhXxx` constant; CI catches the resulting compile error in callers (or in the curated layer's alias). M3 may add an explicit drift-detection diff against a previous codepoint snapshot.

API surface:

```go
package icons

// PhCheck is the Phosphor regular `check` icon (codepoint U+E182).
// Generated from @phosphor-icons/core — do not edit by hand.
const PhCheck = ""

// IconCheck (ph:check) — affirm, complete, success. Curated alias;
// see ADR-0044 for the curation rationale.
const IconCheck = PhCheck

// IconRust is the Devicons-subset Rust icon (nf-dev-rust, U+E7A8).
// Slot B — brand mark. Hand-curated; Slot B is small enough
// that generating from the NF aggregate is over-engineering for v1.
const IconRust = ""
```

The call-site contract is *just a Unicode string*. Egui's font-fallback chain decides which embedded font supplies the glyph based on which codepoint range the character lives in. Slot A codepoints land in Phosphor's range (U+E000–U+E5FF approx); Slot B codepoints land in Devicons/Logos/Custom ranges (above). The Rust side (§SD5) registers both `.ttf` files in the egui font atlas; nothing on the Go side needs to know which slot a constant belongs to.

### SD4 — Weight axis and density-preset mapping

Phosphor's six weights map naturally to [ADR-0032](./0032-imzero2-design-system-spacing-density-motion.md)'s three density presets, but v1 ships only Regular to bound binary size.

Reserved-for-M2 mapping (proposed; not active in v1):

| Density preset | Phosphor weight | Stroke at 18 px Body |
|----------------|-----------------|----------------------|
| Tight          | Light           | ~1.0 px              |
| Standard       | Regular         | ~1.5 px (default)    |
| Roomy          | Bold            | ~2.5 px              |

Cost of enabling the mapping: +2 × ~600 KB = ~1.2 MB additional binary. Defer until M1 confirms whether density-aware glyph weight produces a measurably better visual at Caption / Micro sizes; the simpler model is single-weight + density-aware *size* (which ADR-0030 §SD3 already does for text).

Filled variants (`Phosphor-Fill.ttf`, ~600 KB) are similarly reserved-for-M2. Specific filled affordances we lose by shipping outline-only in v1:

- `nf.CodCircleFilled` — used as a status dot. Outline `ph:circle` is visually weaker; consider a runtime substitution to a paint-canvas drawn dot instead of relying on the font.
- `nf.CodDebugPause` / `nf.CodDebugStop` / `nf.CodPlay` — media controls. Outline equivalents read fine in M1 testing IF the surrounding chrome (button background) provides the "press me" affordance; verify per app.
- `nf.CodError` — Codicons uses a filled circle with an X cut out. Outline `ph:x-circle` is the equivalent shape; semantically intact.

### SD5 — Rust-side font registration

Two `font_data.insert` calls extend the egui `FontDefinitions` chain established by ADR-0030 §SD7:

```rust
font_defs.font_data.insert(
    "phosphor".into(),
    Arc::new(FontData::from_static(PHOSPHOR_REGULAR)),
);
font_defs.font_data.insert(
    "nf-brand".into(),
    Arc::new(FontData::from_static(NF_BRAND_SUBSET)),
);
for family in [FontFamily::Proportional, FontFamily::Monospace] {
    font_defs.families.get_mut(&family).unwrap()
        .push("phosphor".into());
    font_defs.families.get_mut(&family).unwrap()
        .push("nf-brand".into());
}
```

Order matters: text codepoints route to IDS Mono / Iosevka Aile first; codepoints in Phosphor's range fall through to `phosphor`; codepoints in Devicons / Logos / Custom ranges fall through to `nf-brand`. There is no overlap between Phosphor's range and the NF brand-subset ranges — both live in PUA but in disjoint slices, so fallback ordering between the two is robust to glyph-set growth on either side.

Loader location: `src/rust/src/imzero2/app.rs::load_custom_fonts`, alongside the existing IDS Mono / Aile / Symbols-Nerd-Font-Mono registrations. The §SD8 removal step deletes the Symbols-Nerd-Font-Mono registration in the same change.

### SD6 — The legacy `nerdfont` package, the generator, and the upstream JSON catalogue

The artefacts being decommissioned:

- `src/go/public/thestack/imzero2/nerdfont/` — generated Go-side package with **~10 000** glyph constants emitted from the upstream Nerd Fonts JSON catalogue (`staticGlyphs.out.go` + `dynamicGlyphs.out.go`, ~1.1 MB of generated source).
- `src/go/public/thestack/imzero2/nerdfont/generator/` — the codegen library that emits the above.
- `src/go/public/thestack/cmd/nerdfontgen/` — the CLI wrapper around the generator.
- `src/go/public/thestack/imzero2/nerdfont/glyphnames.json` — the upstream Nerd Fonts catalogue (~535 KB, vendored).
- `src/go/public/thestack/imzero2/nerdfont/generate.sh` — the regen driver.

All five move to `../boxer_attic/src/go/public/thestack/{imzero2/nerdfont, cmd/nerdfontgen}/`, preserving pebble's `src/go/public/...` layout under a sibling repository that exists specifically to archive decommissioned-but-historically-valuable code. The pebble2impl side deletes them outright — no shim is left at the old path.

The Rust-side `src/rust/assets/fonts/symbols-nerd-font-mono/` directory (Phosphor's predecessor in the egui atlas) is *not* moved to the attic; it is deleted from pebble2impl in the same M1 change that adds `phosphor/` and `nf-brand/`. The Rust-side LICENSE and SHA file lineage is preserved in git history; there is no value in archiving the upstream binary in a parallel repo.

This deletion plus archive is the §SD1 / §SD2 implementation's final M2 step (§SD8). It is *not* part of M0 (this ADR) or M1 (Phosphor wiring) — call sites must migrate first before the legacy package can be removed.

### SD7 — Versioning and update pinning

Both Phosphor and the Nerd Fonts subset follow ADR-0030 §SD7 / §SD9 / §SD10 discipline:

- **Phosphor release pin.** Pinned to `@phosphor-icons/web` `vX.Y.Z` in `src/rust/assets/fonts/phosphor/BUILD.md`. Bumps are Tier 3 ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD10), recorded as an Amendment to this ADR if codepoints renumber.
- **Nerd Fonts release pin.** The input `.ttf` is pinned to a Nerd Fonts release tag (currently `v3.4.1` at time of writing — verify at first M1). The subsetted output (`NFBrand.ttf`) is regenerated via `subset.sh` and SHA-pinned independently.
- **Codepoint table changes.** If Phosphor renumbers any codepoint our Slot A constants reference, the change requires a same-commit update to `affordances.out.go` and a CI verification step.

### SD8 — Migration plan (M0 → M3)

This ADR is M0. Subsequent milestones land as separate commits with separate review:

| Milestone | Scope | Deliverable | Status |
|-----------|-------|-------------|--------|
| **M0** | This ADR + decision recorded + full Go-side scaffold | `doc/adr/0044-*.md` (proposed) + `iconset-comparison.html` design aid + nerdfont packages mirrored to `../boxer_attic` + `keelson/runtime/icons/` package (curated `IconXxx` + full generated `PhXxx` for every Phosphor regular icon, vendored upstream catalogue + iconsgen generator + generate.sh) | **two commits: ADR + scaffold first; full Phosphor generator second** |
| **M1** | Rust-side font embedding | Phosphor.ttf + NFBrand.ttf added to `src/rust/assets/fonts/{phosphor,nf-brand}/` with SHA pinning; `load_custom_fonts` updated; iconography catalogue verified against the actual TTF | next commit (after ADR review) |
| **M2** | Call-site migration | The 15 files importing `nerdfont` migrate to `icons.*`; old `nerdfont` package deleted from pebble2impl (build green again); old Rust SymbolsNerdFontMono removed in the same change | follow-on commit |
| **M3** | Cleanup + density-weight evaluation | Density-preset → Phosphor weight mapping (§SD4) tested under M1 screenshots; filled-variant decisions; `patterns/iconography.md` written (the catalogue ADR-0030 §SD12 promised) | as-needed |

**The build is intentionally broken between M0 and M2.** The 15 call sites (badge, filepicker, windowhost, ~10 demo apps, idsshowcase) will fail to compile after M0 because the `nerdfont` package no longer exists at its import path. This is acknowledged as the cost of decisively decommissioning Nerd Fonts before having to maintain two parallel systems. Any concurrent worktree-using session that relies on a green build needs to do M2 (or wait for M2) before proceeding.

The choice not to leave a backwards-compat shim is deliberate: a shim ties the new `icons.*` constants to legacy `nf.Cod*` names, perpetuating exactly the per-source-set nomenclature this ADR is moving away from. Better to break and fix once than to ship a translation table.

## Alternatives

- **O1 — Lucide alone.** Rejected on C2 (scientific-glyph weakness) and C3 (no brand marks at all). Reconsider if Phosphor's identity becomes recognised as a specific dev-tool brand and uniqueness becomes a concern.
- **O3 — Tabler alone.** Rejected on C3 (re-drawn brand marks lose canonical identity) and on the broader risk that 5000-icon families lean toward "everything for everyone" — the breadth that ADR-0029 wants to *avoid* in the design-system primitives.
- **O5 — Custom-drawn IDS family.** Rejected on cost (months of designer time) and on premature commitment. Phosphor is the right interim choice; the cost of switching from Phosphor to a custom family later is the same as the cost of switching from Nerd Fonts to Phosphor now, just for ~50 icons instead of 33.
- **O6 — Status quo (keep Nerd Fonts).** Rejected — this ADR exists because the status quo is the problem.
- **Combine Phosphor + Lucide + Tabler in one slot.** Considered as "best of each set." Rejected on C1 (the visual incoherence is the same problem the current Codicons + Material + FA situation has, just with different source sets). The whole point is to have *one* coherent affordance family.
- **Material Symbols (Google variable font).** Considered. Rejected on C6 — Material Symbols carries strong Google brand legibility (same critique ADR-0030 §SD2 applied to IBM Plex). Also: variable font axis is novel in egui rasterisation and would add a class of unknown bugs to v1.
- **Carbon Design System icons (IBM).** Considered. Rejected on C6 for the same brand-legibility reason that ADR-0030 §SD2 rejected IBM Plex.
- **Heroicons (Tailwind).** Considered. Rejected on count — ~300 icons is below the 33+10 + headroom we want; we'd outgrow it in M3.
- **Bundle Phosphor's brand marks (`ph-fill` python-logo, linux-logo etc) into Slot B and drop Devicons.** Considered. Rejected on coverage — Phosphor's brand-mark set is incomplete (no Rust, no Docker, no Go-as-language) and stylistically uniform in a way that defeats brand identity. Slot B specifically wants the heterogeneity Phosphor would erase.
- **SVG rendering instead of font glyphs.** Considered. Rejected on the egui font-atlas integration: text rendering uses `epaint`'s font atlas with deterministic codepoint → glyph lookup. SVG rendering would require either a parallel image-cache path or runtime SVG-to-bitmap conversion, both of which add complexity without solving anything the embedded-font approach doesn't already solve.

## Consequences

### Positive

- **One coherent visual language on the affordance row.** A panel reads as one design intent. The five-language Codicons + Material + FA + Custom mess is gone.
- **Aesthetic alignment with ADR-0029.** Phosphor's geometric-humanist character and ~1.5 px stroke match the Swiss-minimalist + scientific thesis better than Codicons' VSCode-grid aesthetic. Phosphor's scientific glyph coverage (function, sigma, waveform, flask, …) unlocks affordances that Codicons literally does not have.
- **~75 % reduction in icon-related binary bytes.** ~3 MB Symbols Nerd Font Mono → ~600 KB Phosphor + ~150 KB NF subset ≈ ~750 KB. The savings come almost entirely from the >9 500 glyphs we never reference.
- **Brand-mark fidelity preserved.** The Rust crab, Go gopher, Python snake, Docker whale, Linux Tux — all stay canonical via the Devicons subset. The heterogeneity within Slot B is intentional and aesthetically *correct*.
- **Catalogue becomes tractable.** Writing the `patterns/iconography.md` ADR-0030 §SD12 promised becomes a 50-line document instead of "pick from 10 000 glyphs"; the M3 task ships easily once M2 is done.
- **Forward-compatible with multi-weight density mapping.** §SD4 reserves the option to map ADR-0032 density presets onto Phosphor weights; v1 doesn't pay for it, M2 / M3 can opt in.

### Negative

- **Build breaks between M0 and M2.** The 15 call-site files must migrate before the build is green again. Acknowledged in §SD8; this is the cost of the "decisively decommission first" approach. Any concurrent worktree-using session that needs a green build is impacted until M2 lands.
- **Outline-only loses filled affordances in v1.** Several Codicons used filled glyphs (`circle-filled`, `error`-filled, debug controls); their Phosphor outline equivalents read differently. M1 testing will surface which substitutions are acceptable and which need `Phosphor-Fill.ttf` embedding (§SD4 deferred).
- **Phosphor's brand recognition is rising.** It's the icon family used by [shadcn/ui](https://ui.shadcn.com) variants, Lovable, and an increasing fraction of frontend tooling. If "Phosphor look" becomes a recognised dev-tool aesthetic over the next 1–2 years, we'll have re-entered the C3 uniqueness problem that ADR-0030 §SD2 already navigated for proportional fonts. Mitigation: ADR-0029 §SD10 Tier 3 escape hatch to a custom-drawn family (O5) if the issue materialises.
- **Subsetting introduces an upstream-CI step.** `fonttools subset` lives in [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts), not on contributor machines. Bumping the Nerd Fonts pin means pushing a tag upstream and waiting for the release artefact, not running tools locally — same friction profile as IDS Mono per ADR-0030 §SD9.
- **Devicons inside the Nerd Fonts subset stays a "Nerd Fonts" lineage**, not a first-class IDS-owned brand-mark family. If we ever want canonical (Rust Foundation-blessed Rust logo bytes, Go Foundation-blessed Gopher SVG bytes), we'd have to vendor each brand-mark SVG separately. Out of scope for v1; Q4 below.

### Neutral

- **Codepoints become public-stability.** Once `affordances.out.go` ships, renaming `IconCheck` or shifting its codepoint requires a Tier 3 ADR.
- **The `keelson/runtime/icons/` location is a small bet** on the [ADR-0035](./0035-keelson-namespace-introduction.md) keelson namespace surviving as the platform-spine designation. If keelson moves or renames, icons moves with it; no semantic change.
- **Two slots = two upstreams to track.** Marginal: Phosphor bumps quarterly-ish, Nerd Fonts annually-ish. Each bump is a SHA-pin update + (rarely) a codepoint table refresh.

## Status

Proposed — awaiting review by @spx. M0 lands across two commits:

**First commit** (already landed):

- This ADR file (proposed)
- `iconset-comparison.html` at repo root (visual decision aid; can be moved or deleted post-acceptance)
- Legacy `nerdfont` packages mirrored to `../boxer_attic` (initial commit there)
- `src/go/public/keelson/runtime/icons/` package scaffold: hand-curated `affordances.out.go` (Slot A, 33 high-frequency icons with Phosphor codepoints from the public catalogue) + `brandmarks.out.go` (Slot B, 10 NF-subset codepoints preserved verbatim)
- Legacy `nerdfont` packages deleted from pebble2impl

**Second commit** (§SD3 generator addition):

- `phosphor-icons.ts` (then) vendored from `@phosphor-icons/core` + local `tsToJson.py` converter
- `keelson/runtime/icons/generator/` codegen library
- `src/go/cmd/iconsgen/` cmd wrapper
- `keelson/runtime/icons/generate.sh` driver
- `phosphor.out.go` + `phosphor_lookup.out.go` (generated — full ~1500 PhXxx constants + alias constants + name lookup)
- `affordances.out.go` updated so `IconXxx` aliases to generated `PhXxx` (drift notes preserved)

**Third commit** (move font/JSON build upstream to `stergiotis/ids-fonts`):

- `stergiotis/ids-fonts` extended with Phase 2 — `scripts/fetch-phosphor.sh`, `scripts/subset-nerd.sh`, `scripts/ts-to-json.py`, `icons.toml` codepoint-range manifest, `PHOSPHOR_VERSION` + `NERDFONTS_VERSION` pins, Makefile + workflow updates (separate commit in that repo: `c9926f6`)
- pebble2impl side trimmed: `phosphor-icons.ts` + `tsToJson.py` deleted (now produced upstream); `phosphor-icons.json` becomes a release-artefact vendored from `stergiotis/ids-fonts` (the in-repo file remains the artefact, just sourced differently going forward); `generate.sh` simplified to JSON-only

**Fourth commit** (M1 — Rust-side font embedding):

- `Phosphor.ttf` + `NFBrand.ttf` vendored from the `stergiotis/ids-fonts` `v0.2.2` release into `src/rust/assets/fonts/{phosphor,nf-brand}/` (each with `LICENSE`, `SHA256SUMS`, and `BUILD.md` modelled on the existing `ids-mono/` layout).
- `src/rust/src/imzero2/appconfig.rs` gains a `phosphor_font_ttf` slot + `--phosphorFontTTF` CLI flag + matching tweak fields, alongside the existing `nerd_font_ttf` slot (whose semantics shift from "Symbols Nerd Font Mono full blob" → "NFBrand subset", same Rust name for back-compat).
- `src/rust/src/imzero2/app.rs::load_custom_fonts` registers the new `phosphor` font and inserts it into both `FontFamily::Proportional` and `FontFamily::Monospace` chains alongside the existing `nerdfont` slot (phosphor first, then nerdfont — disjoint PUA ranges so ordering is not load-bearing).
- `src/rust/imzero2_egui/src/style/tokens/typography.rs` (the `IMZERO2_IDS_FONTS=on` overlay code path) replaces `SYMBOLS_NERD_FONT_MONO` / `FAMILY_ICONS` with `PHOSPHOR_REGULAR` + `NF_BRAND` / `FAMILY_ICONS_PHOSPHOR` + `FAMILY_ICONS_NF_BRAND`. Both are embedded via `include_bytes!` and registered as fallbacks in both family chains.
- `src/rust/hmi.sh` defaults `PHOSPHOR_FONT` and `NERD_FONT` to the vendored paths (`$here/assets/fonts/{phosphor/Phosphor.ttf,nf-brand/NFBrand.ttf}`); the previous `NERD_FONT_URL` download-on-demand fallback is removed — both fonts are now always vendored.
- `src/rust/assets/fonts/symbols-nerd-font-mono/` deleted (preserved in git history; not archived to `boxer_attic` because the Rust-side LICENSE + SHA file lineage doesn't merit a parallel-repo copy).

Open questions:

1. **Phosphor multi-weight v1?** §SD4 defers density-preset weight mapping until M2. Worth reconsidering if even one app in M1 would visibly benefit (e.g., `imztop`'s density-of-information argues for Light at Caption size). Default: defer; revisit M3.
2. **Phosphor fill variant v1?** §SD4 defers `Phosphor-Fill.ttf` embedding. Worth reconsidering if M1 testing finds 3+ affordances read materially worse as outline. Default: defer; revisit M2.
3. ~~**Vendor the upstream Nerd Fonts `.ttf` input to `subset.sh`?**~~ Resolved by the move of the subset pipeline to [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts): the upstream `.ttf` lives only in ids-fonts CI staging (downloaded fresh per build, discarded after subsetting). pebble2impl never sees it.
4. **Custom brand-mark family at M3?** Reserve the option to drop the Nerd Fonts dependency for Slot B by hand-vendoring each brand mark as a Rust Foundation / Go Foundation / Docker Inc. canonical SVG. Cost: per-brand legal due-diligence on logo redistribution. Defer indefinitely; revisit only if Devicons styling becomes a recognised problem.
5. **Naming: `icons.IconCheck` reads with a stutter.** Consider `icon.Check` (singular package name, dropping the per-constant `Icon` prefix). Default: `icons.IconCheck` for v1 to match the existing `nerdfont.CodCheck` pattern's conceptual structure; revisit at M2 when the package is broadly imported.
6. **Should ADR-0030 §SD12 be edited to point at this ADR**, or left as-is with this ADR's "supersedes" claim being the bridge? Default: edit ADR-0030 §SD12 with an Amendment block (mirroring ADR-0030 §SD11's 2026-05-17 Amendment pattern) once this ADR flips to accepted.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0029 — ImZero2 design system: foundations, data-intensive patterns, policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent ADR; this is the §SD5 iconography sub-decision deferred there.
- [ADR-0030 — ImZero2 design system: typography](./0030-imzero2-design-system-typography.md) — sibling ADR; §SD12 of which this ADR supersedes.
- [ADR-0032 — ImZero2 design system: spacing, density, motion](./0032-imzero2-design-system-spacing-density-motion.md) — density-preset definition referenced by §SD4.
- [ADR-0035 — Keelson namespace introduction](./0035-keelson-namespace-introduction.md) — defines the `keelson/runtime/` location for the new icons package.
- [Phosphor Icons project](https://phosphoricons.com/) — Helena Zhang & Tobias Fried; CSS codepoint table at [@phosphor-icons/web/src/regular/style.css](https://unpkg.com/@phosphor-icons/web@2/src/regular/style.css).
- [Phosphor MIT license](https://github.com/phosphor-icons/web/blob/master/LICENSE).
- [Nerd Fonts project](https://www.nerdfonts.com/) — icon aggregation; we keep only Devicons + Logos + Custom subsets.
- [fonttools subset](https://fonttools.readthedocs.io/en/latest/subset/index.html) — subsetting toolchain for §SD2.
- `iconset-comparison.html` (repo root, M0 design aid) — side-by-side rendering of all 43 current icons across Codicons / Phosphor / Lucide / Tabler at IDS Body and Caption sizes.
- `../boxer_attic/src/go/public/thestack/imzero2/nerdfont/` — archived `nerdfont` package (post-M0).
- `../boxer_attic/src/go/public/thestack/cmd/nerdfontgen/` — archived `nerdfontgen` cmd (post-M0).
