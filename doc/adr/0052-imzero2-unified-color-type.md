---
type: adr
status: accepted
date: 2026-04-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-22
---

# ADR-0052: Unified `egui2.Color` Go Type Over Existing FFFI2 Transports

## Context

The ImZero2 Go↔egui binding at [`public/thestack/imzero2/`](../../public/thestack/imzero2) currently surfaces colors to Go callers through two unrelated, incompatible transports:

- **`uint32` PlainArg path** — used by painter, plot, and graph widgets. IDL declares `PlainArg("rgba", ctabb.U32)`; Rust decodes via `color32_from_rgba_u32()` at [`rust/imzero2/src/imzero2/interpreter.rs:27-32`](../../rust/imzero2/src/imzero2/interpreter.rs) with premultiplication. Surfaces in Go as `uint32`.
- **`Color32S` EvaluatedArg path** — used by widgets (Frame, ProgressBar, CodeView, Label). IDL declares `EvaluatedArg("color", structColor32())`; the retained holder emits Color32-construction opcodes that leave the value in the Rust-side register `r11_color32` ([`interpreter.rs:1227`](../../rust/imzero2/src/imzero2/interpreter.rs)). Surfaces in Go as `Color32S`.

An audit across [`public/thestack/imzero2/egui2/definition/egui2_definition_d_*.go`](../../public/thestack/imzero2/egui2/definition) counted ~28 color-bearing argument sites. Concrete consequences of the split:

- **Type mismatch at call sites.** The same conceptual "a color" appears as `uint32` on painter/plot methods and as `Color32S` on widget methods. Callers cannot pass a value constructed for one to a method taking the other without an explicit conversion.
- **Round-trip cruft.** [`public/thestack/imzero2/egui2/widgets/treemap/coloring.go:48-53`](../../public/thestack/imzero2/egui2/widgets/treemap/coloring.go) packs `uint32` → `(r,g,b,a)` bytes → `Color().FromRgbaUnmultiplied(...).Keep()` to feed widget-style APIs that only accept `Color32S`.
- **Naming drift.** `.Color(rgba uint32)` on plot series, `.Fill(color Color32S)` on Frame, `.Stroke(width, color)` on Frame, `fillRgba`/`strokeRgba` parameter names in the painter. No single convention; the same concept is spelled differently across widgets.
- **Inconsistent premultiplication semantics.** The `uint32` path always premultiplies in Rust; the `Color32S` path exposes both `FromRgb`/`FromRgbaUnmultiplied`/`FromBlackAlpha`. Callers have to know which path they are on to reason about alpha.
- **Colors are not first-class citizens in Go.** There is no single Go type that represents "a color in ImZero2" — the user must know, per method, which transport that method was wired to.

Forces at play that the decision must respect:

- **FFFI2 rules.** Generated files under `components/*.out.go` and `rust/imzero2/src/imzero2/enums_out.rs` are off-limits; all changes must land in the hand-written definition files and the generator under [`public/thestack/fffi2/`](../../public/thestack/fffi2). FFFI2 reserves argument names matching `[a-zA-Z][0-9]*` ([`fffi2/ir/idl/fffi2_ir_idl_arguments.go:23`](../../public/thestack/fffi2/ir/idl/fffi2_ir_idl_arguments.go)).
- **ImZero2 execution model.** The runtime is not a linear opcode stream: deferred blocks record opcodes on the Go side and splice them in at a different position later; the Rust side can cull whole blocks without executing their opcodes (documented in [`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) §11). Any stateful encoding scheme that assumes ordered execution breaks under this model.
- **Performance sensitivity.** Painter/plot call sites run in tight per-frame loops (treemap with thousands of rectangles, graphs with hundreds of edges). Per-call overhead of a few bytes is tolerable; per-call atomic operations or allocations are not.
- **Screenshot-based testing.** Parts of the test infrastructure rely on bit-identical pixel output. Changes that alter the pixels produced at the final `egui::Color32` value would require regenerating baselines across many tests.

## Design space (QOC)

**Question.** How should ImZero2 surface colors to Go so that every color-bearing call site accepts the same Go type, without breaking the runtime's deferred/culled execution model or blowing up the dev-time cost?

**Options.**

- **O1 — Status quo.** Keep `uint32` for painter/plot and `Color32S` for widgets; document the split.
- **O2 — New FFFI2 `ColorArg` primitive.** Introduce a third IDL primitive alongside `PlainArg`/`EvaluatedArg`, with a tag-byte wire format: `{ tag: u8, payload: variant-specific }`. Reserves bits for theme/palette references.
- **O3 — Go-side `egui2.Color` union over existing FFFI2 primitives (chosen).** Introduce a Go union type; mark color-bearing args with `.AsColor()`; each IDL arg keeps its existing `PlainArg`/`EvaluatedArg` transport. The generator emits Go methods taking `egui2.Color` and per-transport encode code that unwraps the union.
- **O4 — Theme/palette/register as first-class FFFI2 variants.** O2 plus Rust-side resolution of theme slots (`ctx.style().visuals.*`), palette indices, and explicit register references from day one.

**Criteria.**

- **C1 — Per-call runtime cost.** Wire bytes, decode ops, allocations per color argument on the hot path.
- **C2 — Cognitive load for Go callers.** Does a user have to know which transport a method uses? How many color types exist in their mental model?
- **C3 — Composability with deferred blocks and culling.** Does the design remain correct when opcodes are spliced out of emission order or skipped at apply time?
- **C4 — Dev cost.** Lines touched in the generator, number of definition rewrites, screenshot baseline churn.
- **C5 — Extensibility toward theme / palette / explicit register reuse.** Can those land additively later without re-breaking the ABI?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | +  | +  | −  |
| C2 | −− | +  | ++ | −  |
| C3 | ++ | +  | ++ | +  |
| C4 | ++ | −  | +  | −− |
| C5 | −  | ++ | +  | ++ |

O3 is the Pareto optimum: uniquely dominant on C2, co-dominant on C3, competitive on C1/C4/C5. O2 edges it on C5 alone, and the gap is narrow because O3's `.AsColor()` marker is a per-arg annotation that can later point at a third FFFI2 primitive if theme references materialise.

## Decision

We introduce a Go union type `egui2.Color` in [`public/thestack/imzero2/egui2/widgets/color/`](../../public/thestack/imzero2/egui2) and a per-argument annotation `.AsColor()` on FFFI2 IDL calls. No new FFFI2 wire primitive is introduced; no wire bytes change relative to the current encoding. Every color-bearing argument in the ImZero2 definition files is rewritten from `PlainArg("rgba", ctabb.U32)` / `EvaluatedArg("color", structColor32())` into the same call plus `.AsColor()`. The generator emits Go method signatures taking `egui2.Color` and per-transport encode code that unwraps the union.

### Subsidiary design decisions

- **SD1 — `egui2.Color` is a two-variant union: `literal(u32)` and `retained(Color32S)`.** Constructors: `Hex(u32)`, `RGB(r,g,b uint8)`, `RGBA(r,g,b,a uint8)`, `Gray(v uint8)` produce literal values; `.Keep()` on any literal yields a retained variant via the existing retained-holder mechanism. Rejected a three-way union that would also include "theme slot" — deferred to a follow-up ADR if a concrete need materialises; see SD5.
- **SD2 — No stateful encoder-side auto-reuse.** The first iteration of this design considered a Go-side "last emitted u32" cache that would opportunistically emit a shorter wire encoding for repeated colors. Rejected because ImZero2 splices deferred blocks out of emission order and culls whole blocks at apply time; any encoder state tracking emission order diverges from Rust-side register state under those conditions. Users who need register reuse opt in explicitly via `.Keep()`, which sits in the same block as its consumers and is therefore moved or culled as a unit.
- **SD3 — Cross-transport unwrapping is defined and documented.** When a literal is passed to an `EvaluatedArg`-transport method, the encoder synthesises `Color32::from_rgba_premultiplied(r,g,b,a)` + Keep opcodes on the fly (~+5 B over a pre-retained color). When a retained holder is passed to a `PlainArg`-transport method, the encoder flattens to `u32` (register sharing is unavailable on that transport, so nothing is lost). Both directions are correct; neither is the pathological case.
- **SD4 — Method names are preserved where they denote distinct slots.** `.Fill`, `.Stroke`, `.Shadow` on `Frame` remain distinct — they address different properties, not different encodings of the same property. What gets unified is the argument *type*, not the method *name*. Parameter names are normalised to `col` (avoiding the FFFI2 reserved pattern `[a-zA-Z][0-9]*`).
- **SD5 — Theme / style / palette references are deferred.** A full `ctx.style().visuals.*` path incurs an atomic `Arc<Style>` refcount bump per call (egui's `Context::style()` clones an `Arc`), which argues against making it the default. The `.AsColor()` marker is designed to accommodate a future third variant if demand materialises; that variant would live as a new FFFI2 primitive and a new Go union arm, additively.
- **SD6 — Color-specific codegen, not a generic `UnionArg` registry.** The generator already has type-specific knowledge (e.g., `resolveTypeToTransferRegister` switches on `"color32"` at [`fffi2/compiletime/rustclient/fffi2_compiletime_rust_client.go:67-81`](../../public/thestack/fffi2/compiletime/rustclient/fffi2_compiletime_rust_client.go)). Adding color-aware encode snippets follows the same pattern. A generic union mechanism is premature with one concrete unification target; if a second target (e.g., RichText) lands and the per-type branches start duplicating, refactor to a registry then, with two concrete shapes in hand.
- **SD7 — Color32S stays reachable.** The existing retained holder type continues to exist as the implementation of the `retained` variant of `egui2.Color`. It is no longer the primary user-facing type; callers construct via `egui2.Hex(...).Keep()` rather than `Color().FromRgb(...).Keep()`. The old builder API is wrapped, not deleted, in the first pass.
- **SD8 — Premultiplication is internal and documented once.** Literal constructors (`Hex`, `RGB`, `RGBA`, `Gray`) produce sRGB non-premultiplied values in Go; the Rust side premultiplies at decode. Unmultiplied semantics that the `Color32S` fluent API currently exposes (`FromRgbaUnmultiplied`, `FromBlackAlpha`) remain available through the retained construction path for users who need them, but are off the main `egui2.Color` surface.
- **SD9 (addendum 2026-04-24) — Color arrays are literal-only; retained is a scalar-only concept.** Color-bearing arguments that surface as slices (e.g., `ctabb.U32h` at [`egui2_definition_d_walkers.go`](../../public/thestack/imzero2/egui2/definition) line 99's `rgbas`) are marked `.AsColors()` rather than `.AsColor()` and surface in Go as a new type `egui2.Colors`, defined as `type Colors []uint32` — a zero-overhead new type with the same memory footprint as `[]uint32`. Constructors: `NewColors(n int)` (pre-sized), `ColorsFromU32(s []uint32)` (zero-copy borrow), `ColorsFromSlice(cs []Color)` (packs literal scalars; panics on any retained element). Per-element setters `SetHex`, `SetRGB`, `SetRGBA`, `SetGray` accept `uint32` / raw bytes directly; no `Set(int, Color)` overload is exposed, so retained colors cannot enter the array. Zero-cost conversion back via `AsU32() []uint32` for interop. Wire format is unchanged — packed `u32` array, identical to the existing `U32h` encoding. Rationale for the literal-only constraint: (i) memory — a `[]Color` of ~24-byte union structs would pay ~6× the footprint of `[]uint32` for no wire gain, pathological in walkers / treemap / heatmap scenes with thousands of cells; (ii) use-case survey — identifiable `[]color` call sites are all literal palettes computed by business logic (heatmap colormaps, graph node fills, per-cell treemap colors), while retained colors address register-sharing for scalar calls, which is a different concern; (iii) constraint-by-construction — not exposing retained-accepting setters makes the invariant a type-system property rather than a runtime check. API symmetry: `.AsColor()` → `egui2.Color` (scalar; literal or retained), `.AsColors()` → `egui2.Colors` (bulk; literal only). If a concrete `[]retained_color` use case ever materialises, it is a new ADR.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; the notes below capture detail not visible in the ratings.

- **O1 — Status quo.** The type split is the user-visible defect this ADR exists to fix; keeping it documents the problem rather than solving it. Zero dev cost buys zero progress.
- **O2 — New FFFI2 `ColorArg` primitive.** The earlier draft of this design proposed a `{ tag: u8, payload: … }` wire format as a first-class third IDL primitive. It is viable, but more invasive than O3 (two additional emitter paths in `goserver`/`rustclient`, a new IR struct, modifications to the six call sites that iterate `plain+eval` today) while delivering no runtime win — the wire bytes under O3 are already optimal for each transport. O2's advantage is purely forward-looking: it reserves tag bits for theme/palette/register variants without further ABI work. That payoff only materialises if theme references actually land; the wait-and-see position is cheaper.
- **O4 — Theme/palette/register as first-class FFFI2 variants from day one.** Maximum expressiveness, maximum cost. Requires a theme-slot enum (≥30 entries to cover `Visuals`), `ctx.style()` plumbing in every color-taking Rust call site including painter paths that today have no `ui` in scope, dedicated screenshot baselines per theme variant, and a `Context::style()` atomic refcount on every per-call access (egui caches `.style()` at the frame boundary precisely to avoid this). An audit suggested the theme variant costs ~60% of the full-refactor complexity for ~20% of the value, because most existing callers compute concrete colors from business logic (treemap coloring, graph palettes, plot series) rather than from a theme. Deferring is cheap and reversible; committing is neither.

## Consequences

### Positive

- **One Go type for every color argument.** Callers write `egui2.Hex(0xff4488ff)` or `egui2.RGB(r,g,b)` and pass the result to `.Fill`, `.Stroke`, `.PaintCircleFilled`, `.Color` on `PlotLine`, etc. without translation. The `uint32`/`Color32S` split disappears from user code.
- **Wire format is unchanged.** Every byte that crosses the FFI today crosses it unchanged after the refactor. Screenshot baselines stay valid by construction; the only test failures expected are ones that inspect the Go-side argument type directly.
- **Composes with deferred blocks and culling.** The encoder is stateless per call; correctness follows from reusing primitives that ImZero2 already knows how to defer and cull. No new discipline required of block authors.
- **Treemap round-trip goes away.** [`treemap/coloring.go:48-53`](../../public/thestack/imzero2/egui2/widgets/treemap/coloring.go) collapses from `splitRGBA` + `retainedFromRGBA` to a single `egui2.Hex(rgba)`.
- **Parameter names normalise.** All color args use `col`; the audit-flagged naming drift (`.Color(rgba)` vs `.Fill(color)`) survives only at the method-name level where it is semantically meaningful.
- **Forward path preserved.** If theme references become necessary, they land as a third variant on `egui2.Color` pointing at a new FFFI2 primitive; existing call sites do not re-break.

### Negative

- **Color-specific codegen branches.** The generator gains a `generateColorEncode` path in both `goserver` and `rustclient`. This is additive to the existing per-type switching (already has `structColor32`), not a novel pattern, but the number of per-type special cases grows by one.
- **Cross-transport unwrapping cost.** A literal passed to an `EvaluatedArg`-transport method pays ~+5 B of synthesised construction opcodes on the wire relative to passing a pre-retained color. In practice this affects widget-taking call sites that previously demanded a pre-built `Color32S`; the cost is negligible for typical UI (≤3 widget-color calls per frame).
- **`Color32S` remains as an implementation detail.** The retained-holder type continues to exist under the `.Keep()` variant. Users who read the generated code will still see `Color32S`; the ADR does not promise to delete the type, only to demote it from the primary user-facing surface.
- **Screenshot tests must be re-run once.** Even though the wire format is unchanged, the argument-name normalisation and method-signature changes will force a full test suite run to confirm no pixels drift. Expectation: zero drift; obligation: verify rather than assume.

### Neutral

- **Precedent for future unification.** The `.AsColor()` marker pattern generalises to other multi-transport value types (RichText, Vec2/Pos2, opaque handles) if they become contentious. This ADR does not commit to that generalisation and SD6 explicitly defers it, but the shape is available.
- **No change to the retained-holder runtime mechanism.** `Register0Transfer` and `r11_color32` continue to serve their existing role; the refactor is purely at the Go-type and generator-emit layers.
- **Definition-file rewrites are mechanical.** ~28 call-site edits across ~7 `egui2_definition_d_*.go` files, each swapping `PlainArg(..., ctabb.U32)` / `EvaluatedArg(..., structColor32())` into the same call plus `.AsColor()` and renaming `rgba` → `col` where applicable.

### Derived practices

- **New colors enter through `egui2.Color` constructors.** Literal `uint32` values passed to color arguments should be wrapped in `egui2.Hex(...)` at the call site, not elsewhere. Definition files that add new color-taking methods use `.AsColor()` from the outset; a definition that needs a color argument but omits `.AsColor()` is a bug.
- **`Color32S` is not user-facing.** Hand-written code outside the retained-holder plumbing should not name `Color32S` directly. Construction goes through `egui2.Color`; retention goes through `egui2.Color.Keep()`.
- **Premultiplication lives in one place.** The Rust decode step performs premultiplication; Go constructors produce sRGB non-premultiplied values. Unmultiplied-input constructors on the retained path stay available for the narrow cases that need them.
- **Theme references are a future ADR.** If a concrete use case appears, the work starts by extending the `.AsColor()` marker to carry a variant set and adding a corresponding Rust-side decoder. The shape is designed to admit this without breaking existing callers.

## Status

Accepted — 2026-04-22. Amended 2026-04-24 with SD9 (color arrays as `egui2.Colors`, literal-only). Design frozen; implementation begins with the `egui2.Color` / `egui2.Colors` Go types, the `.AsColor()` / `.AsColors()` IDL annotations, and the per-transport encoder paths in [`fffi2/compiletime/goserver/`](../../public/thestack/fffi2/compiletime/goserver) and [`fffi2/compiletime/rustclient/`](../../public/thestack/fffi2/compiletime/rustclient).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) — Diátaxis + ADR conventions this document follows.
- [`doc/adr/0001-clickhouse-observability-pipeline.md`](0050-clickhouse-observability-pipeline.md) — prior ADR in this repository.
- [`doc/adr/0002-query-categorization-provenance.md`](0051-query-categorization-provenance.md) — prior ADR; template shape followed here.
- [`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) — ImZero2 runtime conventions, including block skipping / culling (§11) and reserved name rules.
- [`public/thestack/imzero2/egui2/definition/`](../../public/thestack/imzero2/egui2/definition) — hand-written IDL definition files targeted by the refactor.
- [`public/thestack/fffi2/ir/idl/fffi2_ir_idl_arguments.go`](../../public/thestack/fffi2/ir/idl/fffi2_ir_idl_arguments.go) — IDL primitive declarations (`PlainArg`, `EvaluatedArg`).
- [`public/thestack/fffi2/compiletime/goserver/fffi2_compiletime_go_server.go`](../../public/thestack/fffi2/compiletime/goserver/fffi2_compiletime_go_server.go) — Go emitter.
- [`public/thestack/fffi2/compiletime/rustclient/fffi2_compiletime_rust_client.go`](../../public/thestack/fffi2/compiletime/rustclient/fffi2_compiletime_rust_client.go) — Rust emitter; existing per-type switching precedent at lines 67–81.
- [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) — runtime decoder; `color32_from_rgba_u32` at line 27, `r11_color32` at line 1227.
