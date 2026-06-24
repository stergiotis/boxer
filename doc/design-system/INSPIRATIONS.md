---
type: explanation
audience: IDS app authors and contributors; reviewers checking the IP boundary
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Inspirations and attributions

The ImZero2 Design System (IDS) is built atop influences in two distinct categories:

- **Inspirations** — design systems, books, and writings that were *consulted* for their taxonomy, structure, or methodology. None of their protected expression (token names, hex values, prose, illustrations, example code) is reproduced in IDS; what we borrowed is the kind of thing that is not protectable as expression — structural splits, category vocabularies, methodological frameworks.
- **Attributions** — fonts, scientific colormaps, software libraries, and other artefacts that IDS *directly adopts* under a permissive license. Each entry here carries an obligation: preserve attribution per the upstream license, cite in this file, and (where the upstream requires) include a comment in the consuming code.

This file is **non-normative** per [ADR-0029 §SD11](../adr/0029-imzero2-design-system-and-policy-as-code.md). Adding, removing, or revising entries does not require a follow-on ADR — normal PR review suffices. The file is the *running record* of the IP boundary check ([ADR-0029 §SD12](../adr/0029-imzero2-design-system-and-policy-as-code.md)); when new tokens, patterns, or assets land, the relevant inspirations / attributions are recorded here as part of the landing PR.

## Categories at a glance

| Category | Obligation | Number of entries |
|---|---|---|
| Design systems consulted for structure / vocabulary | none (concepts are not protected expression) | 8 |
| Swiss / typographic design tradition | none (cited for influence) | 5 |
| Color science directly adopted | attribution required per upstream | 5 |
| Typography directly used | attribution per OFL / MIT | 6 |
| Software substrate | acknowledged per upstream license | 4 |
| Documentation / methodology frameworks | acknowledged | 3 |

## Design systems consulted (concepts borrowed; no expression lifted)

What is borrowed: structural splits (foundations / components / patterns), category taxonomies (qualitative / sequential / diverging palettes for plots), the density-mode concept, the policy-as-code idea. None of these are protected as expression; they are the kind of thing every modern design system shares because they are *useful*, not because anyone owns them.

What is *not* borrowed: token names, hex values, prose, illustrations, example screenshots, marketing copy. Each landing in IDS is verified against the systems below per [ADR-0029 §SD12](../adr/0029-imzero2-design-system-and-policy-as-code.md)'s boundary check.

- **Adobe Spectrum** — [spectrum.adobe.com](https://spectrum.adobe.com/). Foundations / components / patterns *structural split*; the digestible writing style; the density-mode framing. Spectrum's `100/200/300` token ladder is *explicitly avoided* (it's the most-recognisable Spectrum signature) — IDS uses purpose-based names (`Padding.Default`, `Body.Mono`).
- **IBM Carbon Design System** — [carbondesignsystem.com](https://carbondesignsystem.com/). Data-density typographic heritage; dashboard / monitoring use-case framing; the operator-facing UI perspective. Carbon's "Plex" font family is *explicitly avoided* per [ADR-0030 §Alternatives](../adr/0030-imzero2-design-system-typography.md).
- **Material 3** — [m3.material.io](https://m3.material.io/). Token-system structure; the way tokens are organised by role (surfaces, content, action). Material's `primary/secondary/tertiary` emphasis ladder is *explicitly avoided*.
- **Refactoring UI** (Adam Wathan & Steve Schoger) — [refactoringui.com](https://www.refactoringui.com/). Palette-from-scratch methodology in perceptually-uniform color spaces; type-scale design via visual hierarchy; the "skip-the-shades-of-grey" insight that informed IDS's restrained semantic chroma.
- **Atlassian Design System** — [atlassian.design](https://atlassian.design/). Pattern documentation as a clear-writing exercise.
- **Polaris (Shopify)** — [polaris.shopify.com](https://polaris.shopify.com/). Living-design-system as an artifact pattern.
- **Fluent (Microsoft)** — [fluent2.microsoft.design](https://fluent2.microsoft.design/). High-density operator-facing UI patterns; tabular figure conventions.
- **Tailwind CSS** — [tailwindcss.com](https://tailwindcss.com/). Specifically the *numeric color ladder* (`slate-500` etc.) is consulted as a counter-example — IDS does not use this naming. Tailwind's palette-from-OKLab method is closer to what [ADR-0031](../adr/0031-imzero2-design-system-color.md) does, though hex values are independently generated.

## Swiss / typographic design tradition

IDS's Swiss-minimalist aesthetic ([ADR-0029](../adr/0029-imzero2-design-system-and-policy-as-code.md) Context fourth force) descends from the International Typographic Style of mid-20th-century Switzerland. None of these references are normative; they are the heritage from which the aesthetic preferences come.

- **Josef Müller-Brockmann**, *Grid Systems in Graphic Design / Raster Systeme für die visuelle Gestaltung* (1981). Foundational text on grid discipline, mathematical proportion in layout, and the type-and-grid-driven hierarchy that IDS adopts ([patterns/tables.md](./patterns/tables.md) alignment rule; [ADR-0032](../adr/0032-imzero2-design-system-spacing-density-motion.md) §SD2 grid rule).
- **Massimo Vignelli**, *The Vignelli Canon*. Restraint; clarity through subtraction; "I don't read what I design — I design what I read."
- **Armin Hofmann**, *Graphic Design Manual: Principles and Practice* (1965). Form/counterform, asymmetric balance.
- **Jan Tschichold**, *Die neue Typographie* (1928). Earlier ancestor of the International Style; functional typography.
- **Bauhaus** (Dessau / Weimar, 1919–1933). Functional design movement that informed the Swiss tradition; relevant for the type-as-information principle.

## Color science directly adopted (attribution required)

These are the scientific publications that produced the colormaps and color-science methods IDS directly uses. Each is permissively licensed and *expected* to be reused by downstream consumers — adopting them is the more-original choice than reinventing inferior versions ([ADR-0029 §SD12](../adr/0029-imzero2-design-system-and-policy-as-code.md) pre-cleared clause).

- **Fabio Crameri** (Swiss geoscientist, ETH Zürich / University of Geneva alumnus). *Scientific colour maps* (Version 8.0.1) [Zenodo](https://doi.org/10.5281/zenodo.1243862). MIT-licensed. Source for IDS's **default sequential** (`batlow`), **default diverging** (`vik`), and **default qualitative** (`batlowS`) palettes plus alternates (`lapaz`, `oslo`, `lajolla`, `roma`, `broc`, `cork`). The required citation appears in each vendored LUT file's header and in [ADR-0031 §SD3](../adr/0031-imzero2-design-system-color.md).
- **Stéfan van der Walt & Nathaniel Smith** (matplotlib core team). *Default colors for matplotlib (the viridis family).* [bids.github.io/colormap](https://bids.github.io/colormap/). CC0. Source for the **viridis family** alternates (`viridis`, `magma`, `plasma`, `inferno`) bundled per [ADR-0031 §SD3](../adr/0031-imzero2-design-system-color.md).
- **Jamie R. Nuñez, Christopher R. Anderton, Ryan S. Renslow** (Pacific Northwest National Laboratory). *Optimizing colormaps with consideration for color vision deficiency to enable accurate interpretation of scientific data.* PLOS ONE 13(7): e0199239 (2018). CC0. Source for **cividis**, the CVD-explicit sequential alternate.
- **Brettel, Viénot, Mollon (1997)**. *Computerized simulation of color appearance for dichromats.* JOSA A 14(10), 2647–2655. The CVD-simulation method used by IDS's CI palette verifier ([ADR-0031 §SD5](../adr/0031-imzero2-design-system-color.md) and §SD9).
- **Björn Ottosson (2020)**. *A perceptual color space for image processing.* [bottosson.github.io/posts/oklab](https://bottosson.github.io/posts/oklab/). The **OKLab / OKLCh** color space — IDS's construction space for the semantic palette ([ADR-0031 §SD1](../adr/0031-imzero2-design-system-color.md)).

## Typography directly used (OFL / MIT)

- **Renzhi Li** (be5invis). [**Iosevka**](https://typeof.net/Iosevka/) — OFL. Mono base for the custom **`IDS Mono`** build ([ADR-0030 §SD1](../adr/0030-imzero2-design-system-typography.md)). Iosevka's customizer is the source for the IDS variant TOML.
- **Renzhi Li**. [**Iosevka Aile**](https://typeof.net/Iosevka/aile) — OFL. Proportional sibling; IDS default proportional ([ADR-0030 §SD2](../adr/0030-imzero2-design-system-typography.md)).
- **Rasmus Andersson**. [**Inter**](https://rsms.me/inter/) — OFL. Documented swap fallback for the proportional slot ([ADR-0030 §SD10](../adr/0030-imzero2-design-system-typography.md) ladder).
- **Tabular Type Foundry**. [**Onest**](https://github.com/TabularType/Onest) — OFL. Uniqueness-preserving first swap target in the proportional fallback ladder ([ADR-0030 §SD10](../adr/0030-imzero2-design-system-typography.md)).
- **Ryan L. McIntyre** and Nerd Fonts contributors. [**Nerd Fonts**](https://www.nerdfonts.com/) — MIT. **Symbols Nerd Font Mono** icon-only variant is the IDS icon source ([ADR-0030 §SD12](../adr/0030-imzero2-design-system-typography.md), [patterns/iconography.md](./patterns/iconography.md)).
- **FontAwesome (Free) project**. [fontawesome.com/v4/icons](https://fontawesome.com/v4/icons/) — OFL on font + CC-BY-4.0 on icons. The primary glyph subset within Nerd Fonts used by IDS.

**Fonts considered and rejected**, with rationale (recorded here so future contributors don't re-litigate):

- **PragmataPro** (Fabrizio Schiavi Design) — commercial, no embedded redistribution. Supported as a personal-install override only ([ADR-0030 §SD11](../adr/0030-imzero2-design-system-typography.md)). Linked at [fsd.it/shop/fonts/pragmatapro](https://fsd.it/shop/fonts/pragmatapro/).
- **IBM Plex Mono / Plex Sans** (Bold Monday for IBM) — enterprise-recognisable; defeats unique-expression goal.
- **Söhne Mono / Söhne** (Klim Type Foundry) — commercial, app-license tier required; also ubiquitous in 2026 corporate UI.
- **Pragmasevka** — Iosevka build explicitly imitating PragmataPro; derivative-by-design.
- **Geist** (Vercel) — OFL but becoming the new Inter via Next.js ecosystem in 2026.
- **Satoshi / Switzer / other Fontshare fonts** (ITF) — Fontshare EULA does not survive air-gap deployment.
- **JetBrains Mono** — ubiquitous in dev tools; defeats unique-expression.
- **DM Sans** — humanist; weaker hinting at small UI sizes than Inter or Aile.
- **Commit Mono** — OFL but explicitly designed to be neutral; loses distinctive signature.
- **Iosevka SS08 / SS15** — Iosevka's PragmataPro / Plex impressions; carries the same Pragmasevka critique.

## Software substrate (acknowledged per upstream license)

- **Emil Ernerfeldt** and contributors. [**egui** / **eframe**](https://github.com/emilk/egui) — MIT / Apache-2.0. Immediate-mode GUI foundation that IDS layers on top of. ADR-0030, ADR-0031, ADR-0032 all bind to `egui::Style`, `egui::Visuals`, `egui::Spacing`, `egui::FontDefinitions`.
- **egui contributors**. [**egui_plot**](https://docs.rs/egui_plot) — MIT / Apache-2.0. Plot substrate consumed by [patterns/plots.md](./patterns/plots.md).
- **egui contributors**. [**egui_extras**](https://docs.rs/egui_extras) — MIT / Apache-2.0. `TableBuilder` substrate consumed by [patterns/tables.md](./patterns/tables.md).
- **valyala/bytebufferpool**, **hashicorp/golang-lru/v2**, **palette** (Rust crate). Indirect — used by IDS-adjacent infrastructure ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md), [ADR-0031](../adr/0031-imzero2-design-system-color.md) generator). Standard MIT / Apache-2.0 attribution per their crate metadata.

## Documentation and methodology

- **Daniele Procida**. [**Diátaxis**](https://diataxis.fr/). Documentation quadrant taxonomy (explanation / reference / how-to / tutorial). Boxer's documentation standard (which IDS inherits) is built on this framework; every doc in `doc/design-system/` carries a Diátaxis `type:` front-matter field.
- **Michael Nygard**, [*Documenting Architecture Decisions*](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) (2011). The **ADR** (Architecture Decision Record) format. IDS uses boxer's ADR template, which descends from Nygard's original.
- **MacLean, A.; Bellotti, V.; Young, R.; Moran, T.** *Questions, Options, and Criteria: Elements of Design Space Analysis.* Human-Computer Interaction 6(3-4): 201–250 (1991). **QOC notation** for explicit design-space comparison in ADRs (the `**Question / Options / Criteria / Assessment**` block in IDS ADRs).

## Accessibility floor

- **W3C WCAG 2.1**. [w3.org/TR/WCAG21](https://www.w3.org/TR/WCAG21/). Success Criterion 1.4.3 (Contrast Minimum, AA) and 1.4.6 (Enhanced Contrast, AAA) — basis for IDS's mandatory AA / aspirational AAA contrast targets ([ADR-0031 §SD5](../adr/0031-imzero2-design-system-color.md)). Success Criterion 2.3.3 (Animation from Interactions) — basis for reduced-motion handling ([ADR-0032 §SD5](../adr/0032-imzero2-design-system-spacing-density-motion.md)).

## What we did *not* take

Recorded explicitly to make the IP boundary visible:

- No token names lifted (no `slate-500`, no `100/200/300`, no `primary/secondary/tertiary`, no `xs/sm/md/lg/xl`).
- No hex values lifted (the IDS semantic palette is OKLCh-constructed from scratch; the data-encoding palettes are public-domain / MIT scientific publications).
- No prose, examples, illustrations, or screenshots copied from any of the design systems above.
- No font binaries from the rejected-fonts list ship in IDS; the personal-override mechanism keeps PragmataPro etc. on the user's machine only.
- No marketing or brand language ("Inter Display", "Plex", "Spectrum", "Material") used to name IDS components — naming is original.

## How to extend this file

When a new token batch, pattern, or asset lands:

1. The landing PR identifies which inspirations were consulted and which attributions are required.
2. New entries are added to the relevant category section above with a one-line description and a link.
3. For **scientific palette additions** (e.g., adopting `lapaz` in addition to `batlow`): add the family attribution to §Color science directly adopted if not already present, and update the LUT-file header to carry the upstream citation.
4. For **font changes** (e.g., the §SD10 fallback ladder firing): add or update the typography entry; record the chosen font's license obligations.
5. For **design-system additions** considered but rejected: add to §Fonts considered and rejected (typography) or the relevant rejected-list (color, structure) with the rationale, so future contributors don't re-litigate.

The boundary-check log at `doc/design-system/foundations/ip-boundary-check.md` (generated by the M0 palette generator per [ADR-0031 §SD9](../adr/0031-imzero2-design-system-color.md)) is the running technical record of hex-value verbatim searches; this file is the human-readable companion.

## Further reading

- [ADR-0029 — design system + policy-as-code](../adr/0029-imzero2-design-system-and-policy-as-code.md) — §SD11 names this file; §SD12 defines the IP boundary that informs the §What we did not take section.
- [ADR-0030 — typography](../adr/0030-imzero2-design-system-typography.md) — typography choices and the rejected-fonts rationale recorded above.
- [ADR-0031 — color foundations](../adr/0031-imzero2-design-system-color.md) — color science adoption and the OKLCh / Crameri / viridis decision recorded above.
- [ADR-0032 — spacing / density / motion](../adr/0032-imzero2-design-system-spacing-density-motion.md) — reduced-motion / WCAG 2.3.3 referenced above.
- patterns/ directory — the six pattern docs that consume the inspirations and attributions recorded here.
- `doc/design-system/foundations/ip-boundary-check.md` *(generated; M0)* — running technical log of hex-value verbatim searches.
