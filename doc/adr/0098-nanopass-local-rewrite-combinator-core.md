---
type: adr
status: proposed
date: 2026-06-27
---

> **Status: proposed — pre-human-review.** Not yet reviewed by a code owner of the
> nanopass package; the change is implemented on a spike branch and the design may
> still change in review. Do not cite as settled.

# ADR-0098: Nanopass Local-Rewrite Combinator Core

## Context

The nanopass package ([`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass))
canonicalises ClickHouse SELECT statements through a sequence of small passes.
A large fraction of those passes — the `Canonicalize*` family — are **local term
rewrites**: "match this CST node shape, replace it with text built from the
original spans of the children it keeps." Examples: `a ? b : c → if(a, b, c)`,
`x::T → CAST(x, 'T')`, `[1,2] → array(1,2)`, `CASE … END → multiIf(…)`.

Every such pass re-implemented the same skeleton:

```
Parse → NewRewriter → WalkCST + type-switch → build replacement from child spans → GetText
```

Two costs followed:

- **Copied boilerplate.** ~8 passes carried the same parse/walk/emit scaffolding,
  differing only in the rule.
- **Duplicated, divergent overlap reasoning.** The genuinely tricky part of a CST
  rewrite is avoiding overlapping token edits when matches nest. Each pass solved
  it *differently* and implicitly: `CanonicalizeCasts` carried a
  `containsNonCanonicalCastChild` guard to process innermost-first;
  `CanonicalizeTernary` matched outermost and skipped the subtree; others varied
  again. The "how to drive a rewrite safely" knowledge lived nowhere
  authoritative.

The obvious pull is toward a **tree-rewriting DSL** (terse `pattern => template`
rules with a strategy language). The danger is equally clear, and it runs
straight into [ADR-0002](0002-nanopass-discipline.md): a DSL ergonomic enough to
be worth it wants to match *abstract* terms and *return new trees* — which (a)
reintroduces the AST ADR-0002 deliberately rejected (the translation layer that
drifts on every grammar regeneration), and (b) collides with the
`TokenStreamRewriter`, the very thing that gives nanopass lossless round-trips by
editing tokens rather than re-rendering subtrees.

This ADR records the narrower move that captures the boilerplate win without
paying either cost.

## Design space (QOC)

**Question.** How can the local-rewrite passes shed their shared boilerplate —
and centralise the overlap/nesting contract — without reintroducing an AST or
sacrificing lossless trivia preservation?

**Options.**

- **O1** — Status quo: every pass copies the parse/walk/emit skeleton.
- **O2** — External tree-rewriting DSL (pattern + strategy combinator language).
- **O3** — Typed-IL nanopass framework (Racket/Dybvig style: generated record
  types per intermediate language, `define-pass` over them).
- **O4** — A minimal, unexported, Go-hosted **combinator core** in the `passes`
  package: `rewriteNodes` / `rewriteTokens` drivers plus span/builder helpers. A
  rule returns **replacement text built from original child spans**; mutation
  stays in the `TokenStreamRewriter`. *(chosen)*

**Criteria.**

- **C1 — Boilerplate reduction** for local rewrites.
- **C2 — No AST / grammar-drift resistance** (the ADR-0002 axis).
- **C3 — Lossless trivia preservation** (the token-edit model is preserved).
- **C4 — One overlap/nesting contract** instead of per-pass variants.
- **C5 — Performance** (no per-pass tax).
- **C6 — No force-fit**: passes that are *not* local span-rewrites can stay as
  they are rather than being contorted.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | +  |
| C2 | +  | −− | −− | ++ |
| C3 | ++ | −  | −  | ++ |
| C4 | −  | +  | +  | ++ |
| C5 | +  | ?  | −  | +  |
| C6 | ++ | −  | −  | ++ |

O2 and O3 win C1 but lose decisively on C2/C3 — the axes ADR-0002 already
adjudicated — and on C6. O4 takes the boilerplate win that matters while staying
inside the substrate, and is the only option that is `++` on every ADR-0002-load-bearing
criterion.

## Decision

Adopt **O4**: a small combinator core, and convert the passes that fit it.

### The core

[`passes/nanopass_passes_rewrite_core.go`](../../public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_rewrite_core.go),
unexported (internal to `passes`):

```go
type nodeRule  func(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (replacement string, ok bool)
type tokenRule func(tok antlr.Token) (replacement string, ok bool)

func rewriteNodes(sql, name string, rules ...nodeRule) (string, error) // top-down; first match replaces node + skips subtree
func rewriteTokens(sql, name string, rule tokenRule) (string, error)   // per-token

func spanOf(pr, node) string                  // original source text of a kept child
func columnExprOperands(pr, node) []string    // spans of direct columnExpr children
func terminalText(node, tokenType) string     // text of a kept terminal child
func callForm(fn string, args ...string)      // "fn(a, b, c)"
```

`rewriteNodes` walks top-down; the first matching rule replaces the node and the
walk skips its subtree, so edits never overlap. Nested matches are handled by the
pass declaring `NeedsFixedPoint` — the runner re-applies until convergence. This
is exactly the contract the hand-written passes each re-derived; it now lives in
one place.

### The bright line

A rule returns **replacement text** assembled from the **original spans** of the
children it keeps (`spanOf`). It never constructs or returns a tree. This is what
keeps O4 sugar over the existing model rather than a smuggled AST: the
`TokenStreamRewriter` remains the only thing that touches the buffer, and
hidden-channel trivia is preserved exactly as before. *If a proposed rule wants
to return a node, it has crossed into O3 and this ADR does not cover it.*

### Conversion criterion

A pass is converted **iff** it is a local term rewrite that builds replacement
text from original child spans — node-shaped (`rewriteNodes`) or pure per-token
(`rewriteTokens`). It is **not** converted if it needs cross-token state,
structural reordering, or a node-conditional token edit that is not a
whole-node replacement.

**Converted (8):** `CanonicalizeEquals`, `CanonicalizeTernary`,
`CanonicalizeCasts`, `CanonicalizeSugar`, `CanonicalizeWhitespace`
(+`…SingleLine`), `CanonicalizeCaseConditionals`, `CanonicalizeConstructors`.

**Descoped, with reason (left exactly as they were):**

- `CanonicalizeMultiIf` — matches a node (`multiIf` call, 3 args) but *renames the
  name token* rather than replacing the node; whole-node replacement would mean
  re-extracting and re-emitting the argument list, i.e. *more* code.
- `CanonicalizeKeywordCase` — per-token, but needs a CST-derived set of
  identifier token indices to exclude; not a pure `tokenRule`.
- `CanonicalizeIdentifiers` — carries a stateful adjacency/fusion guard across
  tokens to avoid quote fusion.
- `RemoveRedundantParens` — the rule is a precedence engine (precedence table +
  multi-guard analysis) that drives token *deletion*, not span-assembly; the
  boilerplate is a rounding error against the rule.
- `CanonicalizeJoin` — structural token reordering (strictness-before-direction)
  with hidden-channel whitespace surgery.

Forcing these would add code, contradicting the point. Per the repo's
descope-over-gate practice, they stay as hand-written passes.

## Consequences

### Positive

- **Less code.** The converted passes drop from 925 to 721 code-only lines
  (−22%); net of the 86-line core, 925 → 807 (−13%). Each *further* conversion is
  near-pure savings against the now-fixed core; break-even was ~3 passes.
- **One contract for overlap/nesting.** The part most prone to per-pass
  divergence now has a single audited definition. `CanonicalizeCasts` shed its
  bespoke `containsNonCanonicalCastChild` guard entirely — the driver's
  skip-subtree + fixpoint subsumes it. `CanonicalizeConstructors` collapsed from
  four separate `WalkCST` passes to one rule set per direction.
- **Performance is neutral-to-better, not a tax.** On a realistic
  `CanonicalizeFull` run, allocations are flat (−0.1%); unchanged-pass controls
  confirm no drift. On the synthetic suite the geomean is −8% time / −10% allocs,
  and deeply-nested casts are markedly faster (−38% time, −35% allocs): by
  centralising the traversal the core happens to apply outermost-first, which
  collapses the expensive nested-paren `CAST(...)` structure into function form
  earlier, cheapening each fixpoint re-parse. One synthetic shape (a `::` chain)
  regresses ~15% — ~14µs absolute — which does not matter in practice.
- **ADR-0002 stands.** Passes remain stateless, the CST + `SelectScope` remains
  the only representation, fixpoint remains the answer for nesting. The
  combinator is sugar over that substrate.

### Negative

- **A second shape to learn:** the rule signature and the skip-subtree/fixpoint
  contract. Mitigated by documenting it once, in the core file, instead of
  implicitly across ~8 passes.
- **The suite is no longer uniform:** ~5 passes remain hand-written. Accepted —
  uniformity bought by force-fitting is a net loss (it would add code and bend
  the model).

### Neutral

- The core is **unexported** and lives in `passes`, not the core `nanopass`
  package: no new public API surface, no documentation burden beyond the file's
  own comments. Promoting any helper to the public surface is a later,
  separately-justified step.

## Alternatives

The QOC matrix carries the rankings; the notes below record nuance.

- **O2 — external DSL.** The composition half it would sell (top-down/bottom-up,
  fixpoint) already exists at the pipeline level as `Sequence`/`FixedPoint`
  (ADR-0006). The remaining half — terse rule syntax — is the part that forces an
  abstract term representation and a tree-returning rule, i.e. the two things that
  break C2 and C3. Net: it buys what we already have and breaks what we rely on.
- **O3 — typed-IL framework.** This *is* the AST ADR-0002 declined, plus a
  codegen step and per-IL drift. Rejected there, rejected here.

## Status

Proposed — 2026-06-27. Promote to `accepted` after review by a code owner of
[`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0002: Nanopass Pipeline Discipline](0002-nanopass-discipline.md) — the
  stateless-CST-no-AST substrate this ADR stays within.
- [ADR-0006: Nanopass Environment and First-Class Pass](0006-nanopass-environment-and-first-class-pass.md)
  — `LiftBodyPass`, which the converted passes wrap; the pipeline-level
  strategy combinators (`Sequence`, `FixedPoint`).
- [`passes/nanopass_passes_rewrite_core.go`](../../public/db/clickhouse/dsl/nanopass/passes/nanopass_passes_rewrite_core.go)
  — the combinator core.
