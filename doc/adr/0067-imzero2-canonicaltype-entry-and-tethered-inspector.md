---
type: adr
status: accepted
date: 2026-06-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-06
---

# ADR-0067: ImZero2 canonicaltype entry + tethered type inspector

## Context

We want ImZero2 surfaces for working with **canonical types** ([`public/semistructured/leeway/canonicaltypes/`](../../public/semistructured/leeway/canonicaltypes/)): a compact, grammar-constrained type-descriptor DSL (`u32l`, `sx128`, `vc`, `u32-s_vc`). A canonical type is a **type descriptor, not a value** — it says "this is a little-endian u32", not "this is 42". Two surfaces are in scope:

1. **An editor** to *enter* a canonical type.
2. **A reusable inspector** to *analyse and display* one — the "tethered inspector + summary" the maintainer asked about.

What the DSL is made of (from [`canonicaltypes_types.go`](../../public/semistructured/leeway/canonicaltypes/canonicaltypes_types.go), [`canonicaltypes_enums.go`](../../public/semistructured/leeway/canonicaltypes/canonicaltypes_enums.go), the [ANTLR grammar](../../public/semistructured/leeway/canonicaltypes/grammar/)):

- **Four primitive families**, 11 base runes total: string (`s` utf8 / `y` bytes / `b` bool), machine-numeric (`u` / `i` / `f`), temporal (`z` / `d` / `t`), network (`v` IPv4 / `w` IPv6).
- **Family-specific modifiers**: string width (`x`+N, fixed only — `b` takes none); numeric/temporal width (N bits); numeric byte-order (`l` LE / `n` BE / default); network CIDR (`c`).
- **A shared scalar modifier**: scalar (default) / array (`h`) / set (`m`).
- **Composition**: primitives → group (`-`) → signature (`_`). Linear, two-level.

The AST already gives us everything a UI needs in both directions: `Parser.ParsePrimitiveTypeAst` / `ParseSignature` (string→AST), `.String()` (AST→string), `.IsValid()`, `.GenerateGoCode()`, `IterateMembers()`, and `NetworkTypeAstNode.ByteWidth()`. [`sample/ct_sample.go`](../../public/semistructured/leeway/canonicaltypes/sample/ct_sample.go) enumerates every valid type.

Forces that shape the design:

- **The value-inspector pattern already exists.** [ADR-0046](0046-imzero2-value-inspector-infrastructure.md) defines the "tethered inspector": a level-1 summary chip + a level-2 pinned window joined by a spring bezier tether ([`inspector.AnchorToggle`](../../public/thestack/imzero2/egui2/widgets/inspector/anchor.go) / `AnchorTether` / `ProvenanceChip` / `Source[T]`). [`regexsummary`](../../public/thestack/imzero2/egui2/widgets/regexsummary/regexsummary.go) and `distsummary` are working consumers. The "summary" and the "inspector" the maintainer named are **the two halves of one ADR-0046 inspector**, not two features.
- **There is a compact surface token for exactly this role.** [ADR-0065](0065-imzero2-design-system-surface-sizes.md) added `SurfaceInspector` (420×560) and names "tethered inspector" / "regex-as-inspector" as its motivating use. A type inspector is genuinely compact (no haystack/test area like regex), so it fits the token rather than needing `SurfaceWorkspace`.
- **The DSL is short and flat at primitive scope.** A single primitive's canonical string is a handful of characters with no nesting, so it parses totally and cheaply every frame — which is what makes a *bidirectional* text⇄form editor tractable here when it would be fiddly for a nested signature.
- **The live-preview idiom is established.** `mappingplanview` drives a read-only code preview from an editor pane; [`codeview.BuildGo`](../../public/thestack/imzero2/egui2/widgets/codeview/) gives syntax-highlighted Go.

Invariants the design must respect:

- **ADR-0046 single-source rule.** An inspector reads its subject; it never accumulates a private parallel copy. Multi-view (tabbed) bodies render only the active tab per frame and cache derived artefacts by their minimal inputs.
- **Demo registry / capture (ADR-0057).** Each surface registers a `registry.Demo{}`; the TestDriver captures it automatically.
- **FFFI2 register-drain + WidgetIdStack.** Per-instance state lives in a package map keyed by per-call scope (the regexsummary shape), so a value-receiver fluent API survives across frames.
- **Design-first for new packages.** This ADR is the artefact; no widget code until it is accepted.

## Design space (compact)

**Q1 — Entry modality.** (a) builder-primary + read-only canonical string with paste-to-seed; (b) **full bidirectional** formula bar ⇄ form, both editable, kept in sync; (c) text-only formula bar. — **(b) chosen.** The form teaches the grammar and the bar is the expert path + the clipboard artefact; the usual bidirectional hazard (a free-text bar fighting a structured form) is defused by Q2's single-primitive scope, where the string is flat and parse is total (see SD2). (a) is the safe fallback if bidirectional proves annoying; (c) is undiscoverable and leaks raw parser errors.

**Q2 — Composition scope, v1.** (a) **single primitive** (one draft struct); (b) primitive + flat group (`-`); (c) full signature (`-` and `_`). — **(a) chosen.** Smallest correct cut, and it is what makes (Q1.b) safe. Groups/signatures become a chip/pill list wrapping the same primitive editor in a later cut (Deferred). This mirrors `mappingplanview` shipping Cut-1 and deferring the rest.

**Q3 — Summary/inspector packaging.** (a) **reusable tethered inspector** (standalone `canonicaltypesummary`, ADR-0046 shape, like regexsummary) that the editor embeds and any schema view can reuse; (b) an inline analysis pane built into the editor; (c) defer the inspector. — **(a) chosen.** A type appears in many places (schema views, codec previews, contract docs); a standalone inspector is reusable wherever a canonical type shows up, whereas (b) traps the analysis inside the editor. (c) leaves the maintainer's stated want unbuilt.

## Decision

Build **two widgets** under `public/thestack/imzero2/egui2/widgets/`:

1. **`canonicaltypeedit`** — a single-primitive editor with a bidirectional formula bar ⇄ structured form (SD1–SD4). It embeds `canonicaltypesummary`'s level-1 chip as its status strip.
2. **`canonicaltypesummary`** — a reusable tethered type inspector built on the ADR-0046 infrastructure, modelled directly on `regexsummary` (SD5–SD7). Level-2 sized `SurfaceInspector` (420×560).

Both register screenshot demos (SD8). The inspector reads **any** canonical type (primitive / group / signature); the editor v1 *writes* a single primitive (SD7).

## Subsidiary design decisions

### SD1 — Editor model: one flat `primitiveDraft`, not the four AST structs

The form binds to a single flat draft with a family discriminator and the superset of per-family fields, rather than switching the UI over the four concrete AST node types:

```go
type primitiveDraft struct {
    family     familyE     // string | numeric | temporal | network
    base       byte        // rune within family: s/y/b | u/i/f | z/d/t | v/w
    fixedWidth bool        // string only: 'x' modifier present
    width      uint16      // numeric/temporal width, or string fixed width
    byteOrder  byteOrderE  // numeric only: none | LE('l') | BE('n')
    cidr       bool        // network only: 'c' modifier
    scalarMod  scalarModE  // none | array('h') | set('m')  — all families
}
```

Derived each frame: `ast, err := draft.toAst()`; `canonical := draft.toString()`; `valid := err == nil && ast.IsValid()`. egui controls bind to plain fields; the family selector just gates which fields are live. This keeps the render code free of a four-way type switch and gives the bidirectional sync a single source of truth.

### SD2 — Bidirectional sync: per-frame edge ownership (the crux)

The draft is the source of truth; the bar shows a backing string `barBuf`. Each frame, after rendering both:

- **If the bar changed this frame** (`HasChanged()` edge): parse `barBuf`. On success, overwrite `draft` from the parsed AST. On failure, keep `draft`, set `barErr`, and **do not rewrite `barBuf`** — the user keeps typing their (transiently invalid) intermediate.
- **Else if any form control changed this frame**: recompute `barBuf = draft.toString()` (canonical, normalised) and clear `barErr`.
- **Else**: leave both untouched.

Because egui edits are one-widget-per-frame, a user cannot edit the bar and a form control in the same frame, so the two branches never fire together and there is no clobber war. Form edits canonicalise the bar (type `u32`, pick LE → bar becomes `u32l`); bar edits re-seed the form next frame. Programmatic `barBuf` replacement on the form-edit branch is safe because the bar is not the focused-and-edited widget that frame (cf. [ADR-0063](0063-imzero2-textedit-insert-at-cursor.md) on buffer-update semantics). This discipline is exactly why single-primitive scope (SD/Q2) is the precondition for bidirectional: a flat string parses totally; a nested signature would not.

### SD3 — Form mirrors the grammar productions (conditional reveal)

Family selector (4) → base-rune selector (family-dependent) → modifier controls gated by family, so the form *is* the grammar and invalid **shapes** are unrepresentable from it:

- **string**: "fixed width" checkbox → width control (only when fixed); scalar radio. `b` (bool) disables the width control entirely (scalar-or-collection, no width).
- **numeric**: width (8/16/32/64/128 segmented); byte-order radio (default/LE/BE); scalar radio.
- **temporal**: width; scalar radio.
- **network**: CIDR checkbox; scalar radio.

Residual value-level constraints (e.g. fixed-width string requires width > 0) are enforced by `toAst()` / `IsValid()` and surfaced inline, matching the per-family struct validation in `canonicaltypes_types.go`.

### SD4 — The editor's status strip *is* the summary's level-1

The editor's bottom row renders `canonicaltypesummary`'s level-1 chip over the live draft — canonical string + validity dot + byte-width + member/scalar glyph + anchor toggle — so the same affordance that summarises a stored type also summarises the draft, and the maintainer can pop the full inspector straight from the editor. Draft provenance is the zero `inspector.Provenance` by default (chip suppressed); a host may set one.

### SD5 — `canonicaltypesummary` mirrors `regexsummary` exactly

Value `Renderer` + fluent setters; package-level `instanceStates sync.Map` keyed by per-call scope; `New(idPrefix)`; `Render(idGen c.WidgetIdCreatorI, canonical string)`. Level-1 atoms: canonical string (monospace, truncated to a cap) + validity dot (green/red, elided when empty) + a byte-width/member trailer + `inspector.AnchorToggle(toggleId, &state.pinned)` + `tether.CaptureToggle()`. Level-2: a `c.Window` defaulting to `SurfaceInspector` (420×560), `OpenBound` + R10 databinding so the title-bar X flips the toggle, `ProvenanceChip` header, then the tabs (SD6); `tether.CaptureWindow()` at the body top and `tether.Paint()` after. The input is the **canonical string** (matching regexsummary's `pattern string`) parsed inside; an `AstNodeI` convenience overload (`.Ast()`) is deferred until a caller already holds a parsed node. The inspector is **read-only** — it never writes back; editing is `canonicaltypeedit`'s job.

### SD6 — Level-2 tabs (the analysis), v1

- **Layout** (lead view): a horizontal byte-map ruler, one segment per `IterateMembers()` entry, labelled with its footprint. Fixed segments solid; variable ones (var-length `s`/`y`, the CIDR `+1` prefix byte, array/set repetition) hatched and annotated. Uses `NetworkTypeAstNode.ByteWidth()` where the AST defines it and width÷8 for fixed machine-numeric/temporal/fixed-string. **Caveat (honest scope):** the precise serialized encoding of non-network types is owned by the runtime serialization layer, so the ruler shows the *type-level* footprint (fixed vs variable + known widths), not a byte-exact runtime layout.
- **Members**: a small table, one row per member — family, base, width, byte-order, CIDR, scalar, footprint, notes. Trivial for a single primitive; meaningful for composites.
- **Go codec**: `GenerateGoCode()` via `codeview.BuildGo`, read-only and syntax-highlighted, like mappingplanview's preview. Cached by the canonical string; recomputed only when the type changes.

Only the active tab renders per frame (ADR-0046).

### SD7 — Reusability asymmetry: inspector reads any AST, editor writes one primitive

The summary parses a full canonical string (primitive | group | signature), so it is reusable across schema views the day it lands; the editor v1 only *produces* single primitives. The layout ruler, member table, and `IterateMembers()` already handle composites, so generalising the inspector now costs little and avoids rework when the editor grows groups/signatures.

### SD8 — Demos (ADR-0057)

Two `registry.Demo{}` entries, Category `"Leeway"`, seeded from `sample/ct_sample.go`: the editor (`Kind: DemoKindMixed`) and the summary (level-1 row + an opened level-2 window → `DemoFlagNeedsLargeArea`). Capture is automatic.

## Deferred (IDS "defer until needed")

- **Composition** (groups `-`, signatures `_`): the editor's `primitiveDraft` becomes one element of a chip/pill list. Revisit when a caller needs to author composites.
- **Bidirectional at signature scale**: where bidirectional gets hard; revisit with composition — likely builder-primary for the composite outer level, bidirectional retained per-chip.
- **Grammar parse-tree tab**: an additive level-2 view; add on request.
- **`LiveSource[T]` provenance** (bus-bound): inherits ADR-0046's deferral.

## Resolutions at acceptance (2026-06-06)

- **Level-2 tab set** — Layout + Members + Go-codec. Grammar parse-tree deferred. A CBOR/wire-hex tab was considered and **dropped as unnecessary** — not planned.
- **Names** — `canonicaltypeedit` / `canonicaltypesummary` (the `*summary` suffix matches distsummary/regexsummary; the `canonicaltype*` prefix stays greppable to the package).
- **Summary input** — canonical `string` primary (regexsummary-style); an `AstNodeI` overload (`.Ast()`) is deferred until a caller holds a parsed node.

## Updates

### 2026-06-06 — group/signature cut implemented

The deferred composition cut landed; the core design above is unchanged.

- **`canonicaltypesummary`** now parses full signatures: `parseType` splits the canonical string on the `_` separator, parses each segment as a primitive-or-group, and wraps them in `NewSignatureAstNode` (staying on the exported API — no grammar-internal walk; it assumes the canonical `_` form, which is what `String()` and the editor emit). `generateGoSource` emits a `NewSignatureAstNode` over `NewGroupAstNode`/primitive literals. The Layout/Members tabs flatten members via `IterateMembers` (the total footprint stays correct); **drawing the `-`/`_` boundaries in the strip is deferred**.
- **`canonicaltypeedit`** gains `SignatureModel`: a **chip strip** of primitive elements joined by `-`/`_` separators, with one shared bar+form editing the selected chip. The interaction is the **chip/pill builder** option (builder-primary outer; per-chip editing stays bidirectional). The single-primitive `Model` is retained and reused as each element's editor — its bar+form+sync was extracted into `renderEditBody`. The assembled AST is built structurally on `rebuild` (`-`-runs → groups, `_` → signature).
- **The primitive case stays simple (progressive disclosure).** `SignatureModel` shows no sequence chrome for a single element — the common no-group/no-signature case is just the bar+form (a small `+ element` grows it); the chip strip (selector / separators / remove) appears only at >1 element and collapses back on remove. The status reads "live type" for a lone primitive, "live signature" once it grows.
- **Still deferred:** chip **reorder** (add/remove/select/separator-toggle are in); full bidirectional sync at the whole-signature scale (the outer level is builder-primary); the `AstNodeI` (`.Ast()`) summary overload; drawing group boundaries in the Layout strip; copy-to-clipboard.
- **Verified** by the gallery capture: `canonicaltypeedit.png` shows `[u32]-[s]_[vc]` with the `u32` element's numeric form and a live `u32-s_vc · 3 fields · 9 B+var` summary chip.
