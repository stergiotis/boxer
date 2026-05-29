---
type: adr
status: proposed
date: 2026-05-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0030: ImZero2 design system — typography (mono, proportional, type scale)

## Context

[ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) introduces the ImZero2 Design System (IDS) and reserves the typography choice for a sub-ADR (§SD5). The deferred decision is non-trivial because ImZero2 apps are data-intensive: tables of thousands-to-millions of rows, time-series plots, system-metrics dashboards (imztop), live-SQL panels — monospace footprint dominates the screen. The font choice is the single largest contributor to "screenshot character" after layout itself, and the maintainer's stated aesthetic target is **crisp, professional, unique**.

Prior consultation rejected three candidates explicitly:

- **PragmataPro** on licensing grounds — commercial, per-seat, no embedded redistribution. Embedding the `.ttf` in distributed binaries is restricted; the screenshot tour ([reference_screenshot_tour]) needs a font that ships with the repo.
- **IBM Plex Mono** on uniqueness grounds — globally recognised as the IBM corporate default; screenshots immediately read as "enterprise dashboard." Comes with a polished Plex Sans / Plex Serif family but the recognition cuts against the unique-expression goal.
- **Pragmasevka** as derivative-by-design — it's an Iosevka build configured to imitate PragmataPro's metrics and glyph shapes; "Iosevka pretending to be PragmataPro" defeats the unique-expression goal at the level of intent.

The remaining design space is *which Iosevka variant* (mono) and *which proportional partner*. Three forces apply:

- **Data density.** Monospace advance ≈ 0.5 em is preferred (Iosevka native; PragmataPro reference) over ≈ 0.6 em (JetBrains / Plex / Fira Mono) to fit more columns per width.
- **OFL license.** Embedded distribution requires SIL Open Font License or equivalent permissive terms. Iosevka, Iosevka Aile, Inter, and DM Sans are all OFL.
- **Screenshot reproducibility.** Font choice must be embedded at compile time, not loaded from system fonts; the screenshot tour must produce identical pixels across maintainer machine, CI, and contributor checkouts. Tier 2 LLM grading ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9) requires this stability.

The IDS performance invariant ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD13) is satisfied trivially: font choice is a startup-time decision (`FontDefinitions::insert_font_data` called once), not a render-time one.

## Design space (QOC)

**Question.** Which (monospace, proportional) font pair best serves *crisp, professional, unique* for a data-intensive desktop UI under the OFL constraint, given that Iosevka has been pre-selected as the mono base?

**Options.** Each option is a *pair*: mono variant + proportional partner.

- **O1 — Iosevka custom (cv-tweaked) + Inter.** Conservative-distinctive mono with the proven-UI proportional. Inter is everywhere in modern dev tools (Linear, Figma, Vercel, GitHub) — neutral by virtue of ubiquity.
- **O2 — Iosevka custom (cv-tweaked) + Iosevka Aile (chosen).** Same mono; proportional is Iosevka's proportional sibling. Maximum family unity, rare-in-the-wild proportional partner.
- **O3 — Iosevka Curly + DM Sans.** More eccentric mono (cursive `g`, calligraphic descenders, light programming ligatures). Distinctive proportional (geometric, tighter spacing).
- **O4 — Iosevka SS08 (Pragmata-style) + Inter.** Iosevka's published PragmataPro-imitating stylistic set. Closest to the maintainer's stated PragmataPro preference but inherits the Pragmasevka critique.
- **O5 — Iosevka SS15 (Plex-style) + IBM Plex Sans.** Iosevka mimicking Plex Mono, paired with the actual Plex Sans. Family-cohesive but defeats the purpose of avoiding the Plex identity.

**Criteria.**

- **C1 — Crispness at UI sizes (10–16 pt).** Renders sharp, legible glyphs at the sizes IDS uses.
- **C2 — Data density.** Advance width and tabular-figure quality for tables.
- **C3 — Uniqueness.** Does the screenshot look like every other dev tool? Subjective judgement against the field of widely-used pairs.
- **C4 — Family coherence (mono ↔ proportional).** Do the two fonts read as part of one design intent?
- **C5 — Maturity / battle-testing for UI use.** How widely deployed at scale in production UIs?
- **C6 — OFL clearance.** Permissive license suitable for embedding in distributed binaries.
- **C7 — Build / acquisition cost.** Effort to produce the font files (download a release vs. configure + build).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | ++ | ++ | +  | ++ | ++ |
| C2 | ++ | ++ | +  | ++ | +  |
| C3 | +  | ++ | ++ | −  | −  |
| C4 | +  | ++ | +  | +  | ++ |
| C5 | ++ | +  | +  | +  | ++ |
| C6 | ++ | ++ | ++ | ++ | ++ |
| C7 | +  | +  | +  | ++ | ++ |

O2 dominates on C3 + C4 — the two axes the maintainer most cares about — without dropping below acceptable on C1, C2, C5. O1 is the safer fallback if Iosevka Aile's UI maturity (C5) turns out to be insufficient under real-world testing. O3 loses on C5 (DM Sans is less hinted for small UI sizes than Inter or Aile) and is a stretch on "professional" — Curly's cursive `g` skews toward whimsy. O4 and O5 are rejected on C3: imitating famous fonts is the same Pragmasevka critique that rejected the inspiration in the first place.

## Decision

We will use:

- **Monospace**: a custom Iosevka build called **`IDS Mono`**, derived from a pinned Iosevka release with stylistic variants picked for distinctiveness and tabular legibility (§SD1).
- **Proportional**: **Iosevka Aile** (default release), the proportional sibling of Iosevka (§SD2).
- **Type scale**: five sizes (Display / Heading / Body / Caption / Micro) with explicit point sizes and line heights, scaled by the active density preset (§SD3).

Both fonts ship embedded in the binary via `include_bytes!`, pinned by SHA-256 plus Iosevka release version (§SD7). A documented fallback to **O1 — Inter** is reserved for the proportional slot if Aile fails real-world UI hinting tests during M1 (§SD10).

## Subsidiary design decisions

### SD1 — Monospace: IDS Mono (custom build)

`IDS Mono` is an Iosevka build with the following intent. The TOML below sketches the stylistic-variant picks; exact TOML key names should be verified against the Iosevka customizer for the pinned release version (the Iosevka project occasionally renames variants between releases):

```toml
[buildPlans.IDSMono]
family = "IDS Mono"
spacing = "term"              # fixed-pitch monospace
serifs = "sans"               # sans-style; no terminal serifs
noCvSs = false              # allow character-variant overrides
noLigation = true            # no ligatures — data-intensive UIs don't benefit

[buildPlans.IDSMono.variants.design]
g = "single-storey-earless-corner"   # distinctive single-storey g (signature element)
zero = "dotted"                      # dotted zero, unambiguous against O
asterisk = "hex-high"                    # six-pointed, centred
at = "fourfold"                      # cleaner @ at small sizes
brace = "curly"                      # rounder { }
six = "open-contour"                 # open bowl, disambiguates from 8
nine = "open-contour"                # open bowl, disambiguates from 8
capital-i = "serifed"                # disambiguates from l and 1
```

Variants justified:

- **`g = single-storey-earless-corner`** — visually distinctive without going full-cursive (Curly); avoids the "looks like every Mono" double-storey default. This is the signature character of IDS Mono.
- **`zero = dotted`** — most legible in numeric tables; the slashed-zero alternative degrades visually at sub-12-pt sizes.
- **`capital-i = serifed`** — the `Il1` ambiguity is the single most-cited monospace legibility complaint; the serif costs almost nothing in width.
- **`six/nine = open-contour`** — opens up the bowls so 6/9/8 don't blur at small Caption sizes.
- **`noLigation = true`** — explicitly disabled. Programming ligatures (`=>`, `==`, `>=`) are a religious-war topic; they confuse character counting and column alignment in data tables. Apps that want ligatures can layer a separate font set (Tier 3 escalation per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD10).

**Character set to evaluate** in the Iosevka customizer before pinning: `g a y 0 6 9 8 Q R l I 1 @ * { } & $`. These are the glyphs that carry the signature and the disambiguation work.

Weights shipped: **Regular (400)**, **Medium (500)**, **Bold (700)**, each with a matching italic. Variable axis not used initially — discrete weights keep `FontDefinitions` simple and binary size bounded; variable-axis migration is captured as Open Question Q5.

Build artifact location: `src/rust/assets/fonts/ids-mono/IDSMono-{Regular,Italic,Medium,MediumItalic,Bold,BoldItalic}.ttf`. Build TOML committed at the same directory's `private-build-plans.toml`. SD9 describes the build pipeline.

### SD2 — Proportional: Iosevka Aile

Iosevka Aile is the proportional sibling of Iosevka, drawn by the same designer (Renzhi Li) with shared design DNA. Choosing it delivers:

- **Family coherence.** Aile and Iosevka mono share metrics, weight axis, and stylistic character. The screenshots read as one type system rather than "monospace plus an unrelated UI font."
- **Uniqueness.** Aile is rare in production UI use — the standard pick in 2026 is Inter or one of its near-clones. Going with Aile makes the screenshots visibly distinct from the Linear / Figma / Vercel / GitHub-flavoured baseline.
- **Swiss-minimalist alignment.** Aile sits in the geometric-grotesque family that traces back through Akzidenz-Grotesk and Helvetica — the canonical Swiss / International Typographic Style faces. Pairing it with restrained chroma, strong grid, and weight-driven hierarchy ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) Context fourth force) produces screenshots in the Swiss tradition without being a Helvetica clone. The fallback ladder (Onest → Inter, §SD10) preserves grotesque-family coherence under swap.
- **OFL.** Same license as Iosevka mono; embeddable without restriction.

Weights shipped: **Regular (400)**, **Medium (500)**, **SemiBold (600)**, **Bold (700)**, with matching italics. Aile's italic is true cursive (not oblique), which carries character — used sparingly for emphasis and labels, never for whole-paragraph bodies.

**Risk acknowledgement.** Aile is less battle-tested than Inter for small-size UI hinting on mixed-DPI Windows / Linux / Mac targets. If real-world testing during M1 (token wiring) surfaces hinting issues at 11 pt or below, the fallback ladder is **Aile → Onest → Inter** (§SD10): Onest is the uniqueness-preserving first swap (OFL, less common than Inter, humanist grotesque); Inter is the bulletproof generic final fallback. The font slot in the typography token (`Body.family`) is a single string; each swap is a one-line change.

Build artifact location: `src/rust/assets/fonts/iosevka-aile/IosevkaAile-{Regular,Italic,Medium,MediumItalic,SemiBold,SemiBoldItalic,Bold,BoldItalic}.ttf`. Binaries downloaded from the Iosevka project's GitHub releases page; no local rebuild required since Aile defaults are accepted as-is.

### SD3 — Type scale

Five named sizes at the **Standard** density preset:

| Token   | Size (pt) | Line height | Use                                                |
|---------|-----------|-------------|----------------------------------------------------|
| Display | 22        | 1.20        | App-level title, prominent panel headers           |
| Heading | 16        | 1.25        | Sub-panel header, dialog title                     |
| Body    | 13        | 1.40        | Default UI text, button/menu labels, table rows    |
| Caption | 11        | 1.35        | Plot axis labels, secondary text, badge content    |
| Micro   | 9         | 1.30        | Fine print, status-bar metrics, watermark          |

Density scaling per [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD3:

- **Tight**: subtract 1 pt from each size, floor at 9 pt. Display=21, Heading=15, Body=12, Caption=10, Micro=9.
- **Standard**: as above.
- **Roomy**: add 1 pt. Display=23, Heading=17, Body=14, Caption=12, Micro=10.

Concrete Rust API:

```rust
pub const DISPLAY_PT: f32 = 22.0;
pub const HEADING_PT: f32 = 16.0;
pub const BODY_PT:    f32 = 13.0;
pub const CAPTION_PT: f32 = 11.0;
pub const MICRO_PT:   f32 = 9.0;

pub fn scaled(base: f32, density: Density) -> f32 {
    match density {
        Density::Tight    => (base - 1.0).max(9.0),
        Density::Standard => base,
        Density::Roomy    => base + 1.0,
    }
}
```

Mirrored on the Go side via `egui2/styletokens` codegen ([reference_generate_sh]).

### SD4 — Weights and italics

Discrete weights (no variable-axis interpolation in v1):

- **IDS Mono**: 400 / 500 / 700, italic at each
- **Iosevka Aile**: 400 / 500 / 600 / 700, italic at each

Weight roles:

- **400** — default body and mono
- **500** — Heading default; Body emphasis
- **600** (Aile only) — Display default
- **700** — strong emphasis; rarely used for bodies of text

Italic roles:

- Emphasis in body text
- Expression labels in plot legends (e.g., `r = 0.97`)
- Placeholder text in input fields

Italic is *not* used for whole-paragraph italics — long italic spans degrade at 11–13 pt and read as decorative rather than informational.

### SD5 — Numeric and tabular figures

IDS Mono is monospace; all figures are tabular by construction. Iosevka Aile is proportional; tabular figures are an OpenType feature (`tnum`). Aile's `tnum` is enabled by default in the IDS token apply path for any token tagged `Numeric` — surfaced as `Body.Numeric`, `Caption.Numeric`. Plot axis labels, table cells with numeric content, and badges with counts use the numeric tokens.

Slashed-vs-dotted zero contrast is deliberate:

- **IDS Mono uses dotted `0`** (cv99) — numeric `0` in tables is unambiguously the dotted-zero shape.
- **Iosevka Aile uses default open-round `0`** — numeric `0` in prose contexts reads naturally.

The two coexist by role: mono = data / code; proportional = chrome / prose.

### SD6 — `Body.Mono` / `Caption.Mono` pairings

`Body.Mono` and `Caption.Mono` are tokens combining size + IDS Mono family + tabular figures (already on by monospace construction). Reached for in:

- Table cells with code, identifiers, or numeric content
- Code-display panels (regex_explorer SQL preview, snarl JSON view, time-range picker raw input)
- Status / diagnostic surfaces (frame-timing overlay, log tails)
- Pretty / Vertical / JSONEachRow output panels from `ch.local.exec` ([ADR-0028](./0028-chlocal-low-latency-sql-cap.md))

Composition rule: **mono signals "this is data or code"; proportional signals "this is interface chrome."** Plot legends and table headers are proportional; the numeric contents and identifier-typed columns are mono.

### SD7 — Font file distribution: embed in the binary

Font bytes are embedded at compile time via `include_bytes!`:

```rust
const IOSEVKA_IDS_REGULAR: &[u8] = include_bytes!(
    "../assets/fonts/ids-mono/IDSMono-Regular.ttf"
);
// … one const per (family, style)
```

Inserted into the egui `FontDefinitions` at startup, once. Approximate binary-size cost:

- **IDS Mono**: 6 styles × ~250 KB ≈ ~1.5 MB
- **Iosevka Aile**: 8 styles × ~200 KB ≈ ~1.6 MB
- **Symbols Nerd Font Mono** (§SD12): 1 style × ~3 MB ≈ ~3.0 MB
- **Total**: ~6.1 MB added to the release binary.

Acceptable for a desktop app; the alternative (load from `~/.fonts` or system paths) loses screenshot reproducibility and adds per-user installation friction. WASM and mobile targets are out of scope for IDS v1; if either becomes in-scope the embed strategy is reconsidered. The §SD11 PragmataPro personal-override is the *only* font slot that is not embedded — it stays user-installed and never ships.

Repo layout:

```
src/rust/assets/fonts/
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
└── iosevka-aile/
    ├── IosevkaAile-Regular.ttf        # downloaded from Iosevka releases; no local rebuild
    ├── IosevkaAile-Italic.ttf
    ├── …
    └── SHA256SUMS
```

`SHA256SUMS` files pin the exact bytes; a CI check in `scripts/ci/lint.sh` verifies sums on every build so a tampered or accidentally-bumped font is caught.

### SD8 — Screenshot tour reproducibility

Because fonts are embedded (§SD7), the screenshot tour ([reference_screenshot_tour]) produces identical pixels across maintainer machine, CI, and contributor checkouts — without depending on system-installed fonts.

The font rasteriser (egui's via `epaint`) is deterministic for a given (font bytes, size, DPI, text). The only remaining variability is the DPI dimension at which the tour renders; that pinning is owned by the screenshot tour pipeline, not by IDS. Tier 2 LLM comparisons ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9) require pinned DPI and pinned fonts together; this ADR delivers the font half.

### SD9 — IDS Mono build pipeline

The build lives upstream in [`stergiotis/ids-fonts`](https://github.com/stergiotis/ids-fonts) — not in pebble2impl. CI there clones Iosevka at the pinned `IOSEVKA_VERSION`, applies the IDS Mono build plan, and publishes a tagged release with six `.ttf` files, `SHA256SUMS`, and the OFL `LICENSE`. See [ADR-0034 §SD2 pivot Amendment](./0034-imzero2-design-system-typography-m0.md#sd2-pivot--docker-on-ci-not-on-contributor-machine) for the architectural rationale.

- **Build manifest upstream.** `private-build-plans.toml` lives in `ids-fonts/`; not duplicated here.
- **Built artefacts vendored.** `IDSMono-*.ttf` are downloaded from a pinned `ids-fonts` release tag and committed under `src/rust/assets/fonts/ids-mono/`. Storage cost is ~56 MB at Iosevka v34.5.0 — the v34 line ships larger TTFs than this §SD7 estimate predicted; see [ADR-0034 §SD7-finding Amendment](./0034-imzero2-design-system-typography-m0.md#sd7-finding--aile-bundle-budget-overrun).
- **Pin discipline.** Each `ids-fonts` release tag pins one Iosevka version (upstream's `IOSEVKA_VERSION` file). Bumping the Iosevka pin means: bump upstream, push a new tag, re-vendor here against the new release URL.
- **No font toolchain on contributor machines.** Re-vendoring is `curl` + `sha256sum -c`. No Docker, no Node.js, no ttfautohint.
- **SHA-pinned.** Per-directory `SHA256SUMS` committed alongside the binaries; `scripts/ci/lint.sh` verifies on every build. The release's `LICENSE` file travels with the bytes per SIL OFL §2.

### SD10 — Update / version pinning policy and the Aile → Onest → Inter fallback ladder

Font updates are Tier 3 ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD10) decisions:

- **Iosevka release bumps** are captured as an Amendment to this ADR (or a follow-on ADR if the bump changes visible glyph shapes). The rebuild step regenerates the SHAs, which CI verifies.
- **Stylistic-variant changes** (e.g., flipping `g` from `single-storey-earless-corner` to a different variant) require a Tier 3 ADR.
- **Adding a third text font family** (e.g., a display serif for marketing screens) is a Tier 3 decision and likely warrants its own typography sub-ADR. §SD11 (PragmataPro override) and §SD12 (icon fallback) are *not* additional text families in this sense — they extend the existing slots.

**Aile fallback trigger.** The §SD2 risk is real; we commit in advance to a concrete trigger:

- Two or more screenshot-tour outputs at Caption or Micro size show measurable hinting artefacts (uneven stem widths, broken counters) on at least two of {Linux X11, Linux Wayland, macOS, Windows};
- *or* a Tier 2 LLM rubric run on M1 pilot screenshots returns a `typographic-rhythm` finding (rule V7) that traces back to Aile-specific glyph rendering rather than to spacing or hierarchy.

**Fallback ladder.** When triggered, attempt the swaps in order, re-evaluating against M1 screenshots after each:

1. **Aile → Onest** (preferred swap). Onest is OFL, less common than Inter in production UI (uniqueness preserved), and well-drawn for small-size hinting. Humanist grotesque rather than strictly Swiss-geometric — re-grade Tier 2 rubric V7 after the swap; if hierarchy and rhythm pass, accept.
2. **Onest → Inter** (final fallback). Bulletproof, ubiquitous, defeats uniqueness — accept only if Onest also fails for the same reasons.

Each swap is recorded as an Amendment to this ADR (no new ADR number) and lands as a one-line change to the `Body.family` / `Caption.family` token plus the binary swap in `src/rust/assets/fonts/`.

### SD11 — Mono personal override slot (PragmataPro and others)

The `Body.Mono.family` token resolves to **IDS Mono** by default. Users holding a personal commercial-font license — most concretely PragmataPro, but the mechanism is general (Cascadia Code, JetBrains Mono, MonoLisa, Berkeley Mono, etc.) — can override locally without IDS shipping the font. Two motivating use cases:

- **Personal viewing** — the maintainer's daily-use preference for their own machine.
- **Marketing material generation** — promotional screenshots that show the maintainer's chosen font rather than the IDS canonical default. The font choice is part of the personal brand of the maintainer who built the project; marketing artefacts can carry that signature.

Mechanism:

- **Configuration source**: per-user config at `$XDG_CONFIG_HOME/imzero2/fonts.toml` (`~/.config/imzero2/fonts.toml` on Linux), with an environment-variable escape (`IMZERO2_FONT_BODY_MONO`).
- **Font discovery**: the override names the font family; egui's `FontDefinitions::insert_font_data` loads the bytes from the user's font directories (`~/.fonts`, `~/.local/share/fonts`, OS-level system font roots resolved via `fontconfig` on Linux, `CTFontManager` on macOS, registry-walk on Windows) at startup. The user is responsible for the install — IDS does not fetch.
- **Fallback chain**: if the override is set but the named family is not found on the host, the apply path emits a structured warning and falls back to IDS Mono. Apps continue to start; the warning is one-line in stderr and one audit row in `runtime.facts` so operators can see the misconfiguration.
- **Distribution invariant**: PragmataPro (or any other paid font's) bytes are *never* in our repo, our binary, or our build artefacts. The override is opt-in per-machine. The font's license stays the user's responsibility.

**Screenshot tour interaction — two modes.** §SD8 reproducibility holds only with the *default* font, and Tier 2 LLM grading ([ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9) must compare apples to apples. But the tour pipeline also serves marketing-material generation, where the maintainer *wants* their preferred font in the captures. To support both cleanly, the tour has two modes — selected by a single switch with a separate output destination per mode:

- **Conformance mode** (default — no flag needed). Captures with the IDS-default fonts. Any `IMZERO2_FONT_*` override is *ignored* and a one-line stderr warning is emitted (`override IMZERO2_FONT_BODY_MONO=… ignored in conformance mode`). Output goes to `IMZERO2_SCREENSHOT_DIR`. This is what Tier 2 grading, visual regression, and the standard screenshot-tour pipeline read.
- **Branding mode** (opt-in via `IMZERO2_SCREENSHOT_MODE=branding` env var or `--mode=branding` on the tour CLI). `IMZERO2_FONT_*` overrides are honoured. Output goes to a *separate* directory `IMZERO2_BRANDING_DIR` (default `screenshots-branding/`); never read by Tier 2 grading or visual regression. Marketing materials come from here.

The directory split is the load-bearing detail — it makes accidental cross-contamination impossible. Tier 2 cannot read branding-mode captures even if both modes ran in the same session; marketing material is never sampled as conformance evidence even if a contributor sets the mode env var by mistake.

Branding mode is forward-compatible with future overrides (proportional family, density preset, palette tweaks). For v1, only `IMZERO2_FONT_BODY_MONO` is defined; adding `IMZERO2_FONT_BODY_PROPORTIONAL` later would automatically be honoured in branding mode without further design work.

The token API surface adds one method: `tokens.body_mono_family() -> &str` reads the override at startup (gated on mode) and returns the resolved family name; downstream code uses `FontId::new(size, family_name.into())`.

Only the *mono* slot supports the override at runtime in v1. Proportional fonts are embedded-only — the rationale is that proportional family-coherence with Iosevka Aile is part of IDS's identity, and an overrideable proportional slot in *normal* (non-branding) use would fragment the look of the fleet. Mono is more often a personal-taste matter (everyone has a favourite coding font), and the data-density argument cuts both ways for different fonts. In branding mode this restriction is relaxed by design — the maintainer's marketing screenshots are theirs to compose.

**Amendment 2026-05-17 — v1 mechanism is a CLI flag, not the TOML surface.** The mechanism prose above describes a `$XDG_CONFIG_HOME/imzero2/fonts.toml` file plus an `IMZERO2_FONT_BODY_MONO` env var with `fontconfig` / `CTFontManager` / registry-walk discovery. The shipped v1 takes a simpler shape and folds the override into the existing imzero2 font-flag family alongside `--mainFontTTF` / `--nerdFontTTF` / `--fallbackFontTTF`:

- **CLI surface**: `--monoFontTTF <path>` on the imzero2 subcommand (Go side: `application.Config.MonoFontTTF` + `MonoFontTweak`, see `src/go/public/thestack/imzero2/application/config.go`). The Rust loader at `src/rust/src/imzero2/app.rs::load_custom_fonts` registers the resulting face as the `FontFamily::Monospace` primary, keeping `--mainFontTTF` scoped to `FontFamily::Proportional`. `nerdfont` + `fallback` ride along in both family chains as coverage extenders.
- **Shell surface**: `MONO_FONT` env var in `src/rust/hmi.sh`, conditionally passed through as `--monoFontTTF` when non-empty. The companion helper `src/rust/hmi-fonts-pragmatapro.sh` sets `MONO_FONT` (not `MAIN_FONT`) so PragmataPro is scoped to the mono slot as designed; the helper relies on `$PRAGMATA_DIR` (default `$HOME/.local/share/fonts/pragmatapro`) for discovery rather than the OS font-database walk originally specified.
- **Fallback behaviour**: when `--monoFontTTF` is unset, `main` doubles as the Monospace primary so the pre-split default UX (`MAIN_FONT=NotoSans` backing both families) is preserved; the strict family split only materialises when the user explicitly sets the mono slot. When `--monoFontTTF` is set but the file is missing/unreadable, the loader logs a structured error and `Monospace` falls through to egui's default (`Hack`) plus the shared coverage extenders. No fontconfig dependency.
- **Discovery / branding-mode hooks deferred**: the originally specified `fonts.toml` config file and the `IMZERO2_FONT_BODY_MONO` / `IMZERO2_FONT_BODY_PROPORTIONAL` env-var family remain a forward-compatible extension point. The conformance-vs-branding split described above is unchanged at the screenshot-tour layer; the loader is mode-agnostic and applies whatever path it receives.
- **Distribution invariant**: unchanged — PragmataPro (and any other paid font) is never in repo, binary, or build artefacts; the override is opt-in per-machine; license stays with the user.

### SD12 — Icons: Symbols Nerd Font Mono as fallback family

UI icons (arrows, status, file-types, tool indicators, file-system glyphs, Powerline separators) come from **Symbols Nerd Font Mono** (MIT), embedded as a fallback family in egui's `FontDefinitions`.

Variant choice rationale:

- The **icon-only** variant — "Symbols Nerd Font Mono" / `NerdFontsSymbolsOnly` upstream — provides just the icon glyphs, not a patched base font. The full Nerd Font variants (FiraCode Nerd Font, JetBrainsMono Nerd Font, etc.) bundle text+icons on top of a specific base font, which would override our IDS Mono customisation.
- The **Mono** variant uses fixed-width glyph cells, matching the rest of our text in monospace contexts (table rows, code panels, status badges).

Integration (Rust sketch):

```rust
font_defs.font_data.insert(
    "symbols-nerd-font-mono".into(),
    Arc::new(FontData::from_static(SYMBOLS_NERD_FONT_MONO)),
);
font_defs.families
    .get_mut(&FontFamily::Proportional).unwrap()
    .push("symbols-nerd-font-mono".into());
font_defs.families
    .get_mut(&FontFamily::Monospace).unwrap()
    .push("symbols-nerd-font-mono".into());
```

Text codepoints route to IDS Mono / Iosevka Aile first; codepoints those fonts do not cover (Private Use Area U+E000–U+F8FF, plus the extended Nerd Fonts plane U+F0000+) fall through to the Nerd Font fallback. The result: `"\u{f013}"` in source renders as a gear icon, `"\u{f06a}"` as an exclamation-circle, etc. The icon catalogue is documented at [nerdfonts.com/cheat-sheet](https://www.nerdfonts.com/cheat-sheet); the IDS patterns doc records which icons IDS-conformant apps use for which meanings (`patterns/iconography.md`).

Size cost: ~3 MB embedded (icon-only Mono variant). The full Nerd Fonts package is ~12 MB; we ship only the icon-only subset.

License: **MIT** for the Nerd Fonts project (the glyph-aggregation and patching work). The underlying glyph designs come from upstream icon fonts (Material Icons, FontAwesome, Octicons, Codicons, Powerline, Pomicons, Devicons, Weather Icons) which carry their own permissive licenses (Apache-2.0, OFL, MIT, CC-BY-4.0). The Nerd Fonts project documents per-glyph attribution; IDS cites the Nerd Fonts project in `INSPIRATIONS.md` and lets per-glyph attribution flow from there.

Build artefact location: `src/rust/assets/fonts/symbols-nerd-font-mono/SymbolsNerdFontMono-Regular.ttf`, downloaded from the Nerd Fonts GitHub release page and SHA-256 pinned in `SHA256SUMS`. CI verifies the SHA on every build.

**Icon-coverage subsetting** is *deferred* — v1 ships the full ~3 MB icon-only file. If binary size ever becomes a real constraint (WASM or mobile in scope), a Tier 3 sub-decision can subset to only the codepoints IDS-conformant apps actually use, via the existing fontforge/Python tooling Nerd Fonts itself uses.

## Alternatives

- **O1 — IDS Mono + Inter.** Strong fallback; rejected as primary only because Aile delivers more uniqueness without compromising the criteria. Documented as the formal swap target if Aile fails real-world testing (§SD10).
- **O3 — Iosevka Curly + DM Sans.** Rejected on C5 (DM Sans hinting at small UI sizes is weaker than Inter or Aile) and on the "professional" axis — Curly's cursive `g` and calligraphic descenders skew toward whimsy. Reconsider if the project's character later evolves toward more expressive aesthetics.
- **O4 — Iosevka SS08 (Pragmata-style) + Inter.** Rejected on C3: SS08 is Iosevka's PragmataPro impression; choosing it for "uniqueness" is the Pragmasevka argument warmed over.
- **O5 — Iosevka SS15 (Plex-style) + IBM Plex Sans.** Rejected on C3 and on the broader premise of avoiding the Plex identity that drove [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md)'s typography precedent.
- **Pre-built Iosevka (default, no customisation).** Considered for minimum-effort acquisition. Rejected: the default double-storey `g`, undotted `0`, and serifless capital `I` lose the signature §SD1 engineers; the time saved (an evening at most) is not worth the lost identity.
- **System fonts via `fontconfig` lookup.** Rejected on reproducibility: screenshot tour outputs differ per machine; Tier 2 LLM grading becomes unstable.
- **JetBrains Mono + Inter.** Strong industry-standard combo. Rejected on C3 — extremely common in dev tools, defeats unique expression.
- **Mono only, no proportional partner.** Considered for radical density. Rejected: panel headers, dialog text, plot legends, and prose explanations read worse as monospace; legibility cost outweighs the consistency gain.
- **Variable-axis fonts from M0.** Considered for finer-grained weight control. Rejected for v1 to keep `FontDefinitions` simple; migration captured as Q5.

## Consequences

### Positive

- **Screenshots carry a recognisable signature.** IDS Mono's single-storey `g`, dotted `0`, and serifed capital `I` produce immediately-identifiable monospace; pairing with Aile yields a coherent type system that very few products share.
- **Tables are dense and readable.** Iosevka's ≈ 0.5 em advance fits ~20% more columns per width than JetBrains / Plex / Fira Mono; tabular figures are intrinsic to the mono and enabled for Aile numerics.
- **Single design DNA.** Both mono and proportional from Renzhi Li — Aile and Iosevka share shape vocabulary; the screenshots read as one product rather than as "monospace plus a chrome font."
- **Embedding makes screenshots reproducible.** No system-font dependency; CI, maintainer, and Tier 2 grading agree pixel-for-pixel.
- **Single OFL family.** No license complications, no per-foundry agreements, no embedding restrictions.
- **Density preset scales typography in one step.** `Tight` / `Standard` / `Roomy` adjusts every size by one point; no per-token tuning required.

### Negative

- **Binary size grows ~3 MB.** Acceptable for desktop targets; would be a concern for WASM or mobile distribution (neither is in scope for IDS v1).
- **Iosevka custom build is a contributor hurdle.** Rebuilds need Docker + tens of minutes; most contributors will never rebuild, but the option must work for anyone who needs it. Mitigation: committed artifacts + SHA verification + opt-in `make fonts-ids-mono`.
- **Aile is less battle-tested for small-size UI hinting.** Real-world M1 testing may surface issues; fallback documented as §SD10 swap to Inter with a concrete trigger.
- **No ligatures by default.** A subset of contributors expect `=>`, `>=`, `!=` ligatures in code-display contexts; the IDS default opts out. Apps that want them must layer a separate font family, which is an explicit Tier 3 deviation.
- **Italic is true cursive (Aile).** Strong design statement; some uses of italic emphasis may feel heavier than expected. Mitigation: §SD4 limits italic to specific roles.
- **TOML variant names are version-coupled.** Iosevka occasionally renames stylistic variants between releases; pinning the release version (§SD9) is what keeps the TOML interpretable. A bump that renames variants requires re-checking the customizer.

### Neutral

- **Type-scale point sizes are public-stability.** Renaming `Body` to `BodyDefault` or changing 13 pt to 14 pt requires a Tier 3 ADR.
- **The token API surface grows.** Each pairing (`Body.Mono`, `Caption.Mono`, `Body.Numeric`, `Caption.Numeric`) is a token; the count is small but explicit.
- **Pre-built downloads beat in-repo rebuilds for Aile.** Iosevka's release page is the source of truth for Aile bytes; the IDS mono is built locally.

## Status

Proposed — awaiting review by @spx. M0 (acquire + commit font artifacts) can begin immediately upon acceptance; M1 (wire into the IDS token apply path; convert pilot app to IDS Mono + Aile) lands as part of [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md)'s M1.

Open questions:

1. **Iosevka version pin.** Pin to the latest stable Iosevka release at the time of M0, or pin to a known-good earlier version with longer change-history? Default to latest stable unless a regression is known; revisit per Iosevka release notes.
2. **Aile substitution trigger threshold.** §SD10 names the trigger qualitatively; M1 testing should produce concrete screenshots that calibrate "measurable hinting artefacts" before any swap decision.
3. **Display-font escape hatch.** Should a future Display weight (28+ pt) shift to Aile Bold or to a separate display-only sibling (e.g., Iosevka Sparkle)? Defer until a screenshot context demands it.
4. **i18n.** Both Iosevka and Aile cover Latin extended and a useful Cyrillic subset; CJK, Devanagari, Arabic need fallback fonts. Out of scope for v1; revisit when a non-Latin locale lands.
5. **Variable axis.** Iosevka and Aile both ship variable-weight files; v1 uses discrete weights for `FontDefinitions` simplicity. Migrate to variable when the M1 token API stabilises and binary-size matters less.
6. **Build container reproducibility.** Should `iosevkadocker/build` be pinned to a specific image SHA, or accept tag-based pinning? Tag-based for v1; tighten if a build drift incident occurs.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0029 — ImZero2 design system: foundations, data-intensive patterns, policy-as-code](./0029-imzero2-design-system-and-policy-as-code.md) — parent ADR; this is the §SD5 typography sub-decision.
- [Iosevka project](https://typeof.net/Iosevka/) — primary font project; releases at [github.com/be5invis/Iosevka/releases](https://github.com/be5invis/Iosevka/releases).
- [Iosevka customizer](https://typeof.net/Iosevka/customizer) — interactive tool that generates the TOML for §SD1; primary reference for variant naming.
- [Iosevka Aile (proportional)](https://typeof.net/Iosevka/aile) — proportional sibling used by §SD2.
- [Onest](https://github.com/TabularType/Onest) — humanist grotesque, OFL; first proportional swap target in the §SD10 fallback ladder.
- [Inter](https://rsms.me/inter/) — bulletproof generic grotesque, OFL; final proportional fallback in the §SD10 ladder.
- [Nerd Fonts](https://www.nerdfonts.com/) — icon aggregation project; §SD12 uses the "Symbols Nerd Font Mono" icon-only variant ([github.com/ryanoasis/nerd-fonts releases](https://github.com/ryanoasis/nerd-fonts/releases)).
- [PragmataPro](https://fsd.it/shop/fonts/pragmatapro/) — commercial mono (Fabrizio Schiavi Design); supported as a §SD11 personal-install override only, never shipped.
- [SIL Open Font License 1.1](https://openfontlicense.org/) — covers Iosevka, Iosevka Aile, Onest, Inter.
- [`epaint` (egui's rasteriser)](https://github.com/emilk/egui/tree/master/crates/epaint) — deterministic font rendering relied on by §SD8.
- [`iosevkadocker/build` container](https://hub.docker.com/r/avivace/fontforge) — reference image for the §SD9 build pipeline (verify current canonical image at build time).
