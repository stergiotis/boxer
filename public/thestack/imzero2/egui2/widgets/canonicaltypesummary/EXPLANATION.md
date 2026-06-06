---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# canonicaltypesummary — Explanation

`canonicaltypesummary` surfaces a single leeway **canonical type**
(`canonicaltypes`) at two levels of detail in the same cell of UI. It is the
inspector half of [ADR-0067](../../../../../../doc/adr/0067-imzero2-canonicaltype-entry-and-tethered-inspector.md)
and is built on the shared value-inspector infrastructure from
[ADR-0046](../../../../../../doc/adr/0046-imzero2-value-inspector-infrastructure.md).
A canonical type is a *type descriptor*, not a value — `u32l`, `sx128`, `vc`,
`u32-s_vc` — so everything here describes a type, never an instance of one.

## The two levels

- **Level 1 (in-flow target)** — a compact monospace row: a Phosphor
  `brackets-angle` glyph hinting at expandability, the canonical string
  truncated to a configurable cap, a green / red validity dot
  (`canonicaltypes.AstNodeI.IsValid`; elided when the string is empty), and a
  terse `N fields · K B` footprint trailer. Paired with the standard
  `inspector.AnchorToggle`.
- **Level 2 (inspector window)** — a draggable `c.Window` sized to
  `styletokens.SurfaceInspector` (420×560, the archetype created for the
  tethered-inspector role) with a three-tab body and the optional
  `inspector.ProvenanceChip`. A spring bezier `inspector.AnchorTether` ties
  the toggle to the open window.

The toggle / window / tether / R10-databinding wiring is a direct port of
`distsummary` and `regexsummary`; see those packages for the shared idiom.

## The three tabs

- **Layout** — a left-to-right strip of framed segments, one per member,
  interleaved with the structural boundary markers (`-` between members of a
  group, an accent `_` between groups of a signature). Fixed-width members are
  sized roughly in proportion to their byte footprint; variable-length members
  render at a fixed width with a muted "var" label.
- **Members** — a decomposed table, one row per `IterateMembers()` entry:
  canonical string, family, base, bit width, byte order, shape
  (scalar/array/set), byte footprint, and a short note. Columns are pinned to
  fixed widths via a small `cellLabel` helper rather than the table widget,
  because the row count is small.
- **Go codec** — the type rendered as compilable Go via each primitive's
  `GenerateGoCode`, shown in a syntax-highlighted `codeview` block. The
  highlighted holder is cached on the instance and rebuilt only when the
  generated source changes.

## Footprint is type-level, not byte-exact

The byte numbers come from `NetworkTypeAstNode.ByteWidth()` for network types
(address bytes plus the CIDR `+1` prefix byte) and from width÷8 for fixed
machine-numeric / temporal / fixed-width string types. Unbounded strings and
any array/set shape are reported as variable. **The precise serialized
encoding of non-network types is owned by the runtime serialization layer**,
so the strip and table show the *type-level* footprint, not a guaranteed
byte-exact runtime layout. The Layout tab states this inline.

## State and scope

Per-instance state — the pinned flag, the selected tab, the cached parse
(`inSrc`/`ast`/`parseErr`, so an unchanged string is not re-parsed every
frame), and the retained code-view holder — lives in a package-level
`sync.Map` keyed by `idPrefix#<callId-hex>`. That scope combines the
developer-supplied `idPrefix` with the per-call `idGen` disambiguator, so the
same `Renderer` can render multiple types in one frame (one per row of a
schema view) without colliding. Entries are never reclaimed, matching the
`distsummary` / `regexsummary` posture.

## Deferred (ADR-0067)

- **String → full signature parse.** Input is parsed via
  `ParsePrimitiveTypeOrGroupAst` (primitive or flat group). A `_`-separated
  signature parse path and an `AstNodeI` overload (`.Ast()`) are deferred
  until a caller needs them.
- **Proportional ruler.** The Layout strip approximates relative widths; a
  true byte-accurate ruler with a shared scale is a later refinement.
- **CBOR/wire-hex and grammar parse-tree tabs.** Considered and dropped /
  deferred at acceptance.

## Tests

`canonicaltypesummary_test.go` is white-box and runtime-free: it pins the
per-family classification and byte-footprint maths (the hand-rolled logic
most prone to drift), the group aggregation, the parse-error path, the
codegen shapes, the formatting helpers, and the `Renderer` defaults /
fluent-copy contract. The render path needs the egui FFI host and is not unit
tested here.
