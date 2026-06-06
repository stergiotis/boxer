---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# canonicaltypeedit — Explanation

`canonicaltypeedit` edits a single primitive leeway **canonical type**
(`canonicaltypes`) — `u32l`, `sx128`, `vc`, … . It is the editor half of
[ADR-0067](../../../../../../doc/adr/0067-imzero2-canonicaltype-entry-and-tethered-inspector.md);
the read-only inspector half is [`canonicaltypesummary`](../canonicaltypesummary/),
whose level-1 chip this widget embeds.

## Two synchronised views, one source of truth

The editor shows the same type two ways:

- a **formula bar** — a free-text `c.TextEdit` holding the canonical string;
- a **structured form** whose controls follow the grammar productions
  (family → base → family-specific modifiers → scalar shape).

The single source of truth is a **flat draft** (the unexported `Model` fields:
`base`, `fixedWidth`, `width`, `byteOrder`, `cidr`, `scalarMod`). The family is
*derived* from the base rune (`familyOf`), not stored, so the two cannot
desync. `draftToNode` reads only the fields that apply to the current family,
so a modifier left over from another family is harmless.

## The bidirectional discipline (ADR-0067 §SD2)

egui edits one widget per frame, so in any frame at most one of {bar, form}
reports a change. `Render` exploits that with a per-frame edge-ownership rule:

- **bar changed** → parse it. On success, load the draft (`nodeToDraft`) and
  rebuild the derived cache. On failure, keep the draft *and the buffer* (so a
  mid-typing intermediate like `u3` survives) and show the headline.
- **form changed** → rebuild the cache and re-canonicalise the bar buffer.
- **neither** → leave both.

Because the two branches are mutually exclusive per frame, there is no
clobber war. This only stays simple because the scope is a single primitive —
the canonical string is flat and parses totally. A one-frame lag (the side not
edited catches up next frame) is accepted; the embedded chip is updated after
the edge logic, so the status line is current in the same frame.

## The form is the grammar (ADR-0067 §SD3)

`renderForm` shows only the controls that apply to the current family, so
invalid *shapes* cannot be expressed: no byte-order control on a string, no
width on `bool`, CIDR only on network, width only on numeric / temporal /
fixed-width string. Residual value-level validity (e.g. a fixed-width string
needs width > 0 → `sx0` is invalid) is left to
`canonicaltypes.AstNodeI.IsValid`, surfaced by the embedded chip's dot.

Width uses a `DragValueU64` clamped to a sane range (`clampWidth`); arbitrary
widths remain reachable by typing in the formula bar.

## Status = the embedded summary chip (ADR-0067 §SD4)

`renderStatus` draws `canonicaltypesummary.New(...).Render(...)` over the live
canonical string. That chip *is* the editor's status line — its validity dot
and `N fields · K B` trailer — and its anchor toggle pops the full tethered
inspector for the type being edited. The editor thus consumes step 1 directly.

## API

Caller-owned `Model`: `NewModel()` (seeded `u32`), `Render(ids, scopeKey)`
once per frame, and the read-backs `Canonical()`, `Node()`, `Valid()`.
`SetCanonical(s)` seeds from a string (no-op on parse failure).

## Deferred (ADR-0067)

- **Groups / signatures.** The draft is one primitive; the planned next cut
  wraps it as one element of a chip list (`-` groups, `_` signatures), at which
  point the outer level likely goes builder-primary while each chip keeps the
  bidirectional bar.
- **Copy-to-clipboard** on the canonical readout, and richer width affordances.

## Tests

`model_test.go` is white-box and runtime-free: the headline is
`TestDraftRoundTrip` (parse → `nodeToDraft` → `draftToNode` must reproduce the
exact canonical string across every family and modifier — the convergence
guarantee for the bar ⇄ form sync), plus `SetCanonical`, the validity edge
(`sx0`), and the `familyOf` / default-base / `clampWidth` helpers. The render
path needs the egui FFI host and is exercised by the demo capture instead.
