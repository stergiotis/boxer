---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# nanopass — positioning

Positions the ClickHouse SQL transformation pipeline: the parser, the
concrete syntax tree with scopes, the pass vocabulary with declared
properties, the environment that makes settings and parameters ordinary values,
and the registry that applies passes before execution. The workbench that
runs the passes on every query is positioned by [play](./play.md).

## Short form

For an app that runs SQL typed by a user — a workbench, an applet, an
introspection endpoint — and the people who write the rewrites it applies,
who need the rewritten query to still be the user's query, comments and
positions intact, nanopass is a pass framework for ClickHouse SELECT
statements: small stateless passes[^pass] over the parsed text, declared and
checked pass properties, and a registry that applies the active passes
before execution and lists them as a table.

Unlike rewriting through an abstract syntax tree — the server's exported
tree or a transpiler's — which drops comments, whitespace and positions and
returns a different query than the one typed, nanopass edits spans of the
original text. The cost is a re-parse per pass, a parser that accepts more
than the canonical grammar, and output that is normalised rather than byte-
identical.

## Full form

**For** an app that runs SQL typed by a user — a workbench, an applet, an
introspection endpoint — and the authors of the rewrites it applies (macro
expansion, qualification, literal extraction, security triage, profiling),
who need the rewritten query to remain the query the user typed,

**nanopass is** a pass framework for ClickHouse SELECT statements: stateless
passes[^pass] over a concrete syntax tree[^cst] with one shared scope
analysis, pass properties declared and verified over a corpus, and a
registry that applies the active passes before execution and exposes them as
a table,

**that** keeps a rewrite lossless — a pass edits spans of the original token
stream, so comments, whitespace and offsets survive for completion and error
placement — and makes idempotence a checked declaration rather than a
comment,

**unlike** rewriting through an abstract syntax tree, whether the server's
exported one or a transpiler's, which returns a different query than the one
typed,

**nanopass** treats SQL as a data structure edited in place. The cost is a
re-parse per pass that grows steeply on long pipelines, a parser that
accepts more than the canonical grammar, normalised rather than byte-
identical output, and a best-effort apply that can skip a pass.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | apps running user-typed SQL | [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md) Context (play, the introspection query endpoint) |
| For | pass authors | [nanopass-sql skill](../../skills/nanopass-sql/SKILL.md); [ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md) Context |
| is a | grammar pair, CST, one scope analysis | [ADR-0002](../../adr/0002-nanopass-discipline.md); [dsl EXPLANATION](../../../public/db/clickhouse/dsl/EXPLANATION.md) |
| is a | declared, verified pass properties; the environment | [ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md) |
| is a | a registry applied at pre-execute, listed as a table | [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md) |
| that | lossless span edits; offsets for completion and errors | [ADR-0002](../../adr/0002-nanopass-discipline.md) Update 2026-08-16; [ADR-0098](../../adr/0098-nanopass-local-rewrite-combinator-core.md) (proposed; code present) |
| that | no parallel scope analyses | [ADR-0002](../../adr/0002-nanopass-discipline.md) |
| that | idempotence as a checked declaration | [ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md) (`AssertProperties`) |
| that | active rewrites as a table | [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md) |
| unlike | the server's exported AST | [ADR-0002](../../adr/0002-nanopass-discipline.md) Update (no offsets, cannot keep trivia, unreleased, no compatibility guarantee) |
| unlike | a transpiler's tree-returning rules | [ADR-0098](../../adr/0098-nanopass-local-rewrite-combinator-core.md) Alternatives (an external tree-rewriting DSL; the typed-IL nanopass framework "is the AST ADR-0002 declined") |
| nanopass | SQL as a data structure edited in place | [dsl EXPLANATION](../../../public/db/clickhouse/dsl/EXPLANATION.md) ("SQL is treated as a data structure, not a string") |
| trade | re-parse per pass and its cliff; over-accepting grammar; normalised output; best-effort apply | [ADR-0002](../../adr/0002-nanopass-discipline.md) Consequences; [ADR-0192](../../adr/0192-nanopass-cost-profiling.md) Context; [ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md), [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md) Consequences |

## Boundary

- Parser technology is a boundary, not a foil: [ADR-0196](../../adr/0196-nanopass-two-stage-sll-parsing.md)
  keeps ANTLR and rejects PEG, hand-written descent and tree-sitter on
  rewrite cost and cgo, and [ADR-0084](../../adr/0084-nanopass-antlr-dfa-cache-bounding.md)
  bounds its memory for long-running processes.
- Accepted and unbuilt, absent from the clauses: form-tag validation,
  requires/produces scheduling, parse-once caching
  ([ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md)
  v1 scope). The properties that *are* checked are idempotence and
  fixed-point need.
- The local rewrite core ([ADR-0098](../../adr/0098-nanopass-local-rewrite-combinator-core.md))
  is proposed with code present; the lossless clause rests on 0002's
  Update first and 0098 second.
- A query builder (`astbuilder`) and an AST package exist beside the CST
  pipeline for SQL *construction*; the "no AST" discipline governs
  rewriting, not construction. Positioning covers the rewrite pipeline.

## Further reading

- [why-boxer](../why-boxer.md) P2 — the premise this pipeline enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [nanopass-sql skill](../../skills/nanopass-sql/SKILL.md); [clickhouse-ast-json-export](../../adr-background-work/clickhouse-ast-json-export.md).
- Decisions: [ADR-0002](../../adr/0002-nanopass-discipline.md),
  [ADR-0006](../../adr/0006-nanopass-environment-and-first-class-pass.md),
  [ADR-0084](../../adr/0084-nanopass-antlr-dfa-cache-bounding.md),
  [ADR-0098](../../adr/0098-nanopass-local-rewrite-combinator-core.md),
  [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md),
  [ADR-0117](../../adr/0117-passthrough-table-classifier.md),
  [ADR-0192](../../adr/0192-nanopass-cost-profiling.md),
  [ADR-0196](../../adr/0196-nanopass-two-stage-sll-parsing.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass

[^pass]: A pass is one small rewrite with one job, such as turning CASE into a function call; passes are chained into a pipeline.
[^cst]: A concrete syntax tree keeps every token of the source, including comments and whitespace, which an abstract syntax tree discards.
