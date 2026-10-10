---
type: adr
status: accepted
date: 2026-10-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-10
---

# ADR-0304: SLL-exact column qualifiers — the table part of `t.c` gets its own rule

## Context

[ADR-0196](./0196-nanopass-two-stage-sll-parsing.md) parses every statement
under SLL prediction first and re-parses under full-context LL when SLL reports
anything. The fast path is memoised in the DFA cache; the fallback is not, and
costs SLL plus LL. On the benchmark statements a warm SLL parse costs
0.25 µs per token and LL about 8 µs, so whether a statement stays on the fast
path decides its parse cost by more than an order of magnitude.

Over every statement the test suite parses (15,302 grammar1 inputs LL accepts,
harvested 2026-10-09), SLL rejected **29%**, and 97.5% of those rejections were
one message: `expecting '.'` after a qualified column reference. ADR-0196 §SD6
noticed that 86% of its corpus rejects carried a dotted name but found dotted
names "necessary and not sufficient", and left it.

The mechanism, measured in
[antlr4-go — what the runtime and the generated parser cost](../adr-background-work/antlr4-go-runtime-and-codegen-review.md):
the qualifier in `columnIdentifier : (tableIdentifier DOT)? nestedIdentifier`
reused `tableIdentifier`, which decides `(databaseIdentifier DOT)?` itself. SLL
prediction carries no call stack, so on reaching the end of a rule it continues
into every place the rule is invoked. `tableIdentifier` is also a FROM-list
item, a JOIN target and an INSERT target, so for `t.c` the `db.table` reading
stays viable whenever the next token could follow a table anywhere — `,` `)`
`WHERE` `JOIN` EOF — and SLL resolves the conflict to the lowest alternative,
the database prefix. The parse fails a token later. `t.c FROM` survives because
nothing follows a table with `FROM`, which is why the pattern looked partial.

## Decision

We will spell the column qualifier as its own rule, with `tableIdentifier`'s
right-hand side:

```antlr
columnIdentifier : (columnQualifier DOT)? nestedIdentifier ;
columnQualifier  : (databaseIdentifier DOT)? (identifier | COLUMNS | paramSlot) ;   // grammar1
columnQualifier  : (databaseIdentifier DOT)? (IDENTIFIER | paramSlot) ;            // grammar2
```

Only `DOT` follows `columnQualifier`, so its decision no longer leaks into FROM
contexts and SLL predicts it as LL does. Both grammars take the same edit.

The accepted language does not change: the alternatives and their order are the
same, so LL resolves every ambiguity as before — including the three-way `t.c`
ambiguity ADR-0196 §SD6 kept, which this ADR leaves in place. Only the rule's
name in the tree changes.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `grammar{1,2}.ColumnIdentifierContext.TableIdentifier()` | removed; `ColumnQualifier()` replaces it | every consumer reading a column's table part (compiler-found) |
| `grammar{1,2}.ColumnQualifierContext` | added | type switches that matched `*TableIdentifierContext` to reach column qualifiers (not compiler-found; audited) |
| `nanopass.ColumnQualifierName` | added — `TableIdentifierName` for the qualifier | — |
| generated parsers, `.interp` | regenerated with ANTLR 4.13.2 | — |

## Alternatives

- **Reorder `tableIdentifier`'s alternatives** (non-greedy `??`) so SLL's
  lowest-alternative choice is the short reading. Rejected: LL resolves the
  genuine three-part ambiguity by the same rule, so `db.t.c` would silently
  change from (database, table, column) to (table, nested field).
- **A semantic predicate** in `tableIdentifier` consulting the parent context.
  Rejected: puts Go into a grammar that has none, and re-derives LL's choice by
  hand where a mistake parses silently.
- **Give the FROM-side uses the new rule** instead and keep `tableIdentifier`
  for the qualifier. Equivalent for SLL, but the table-reference side has more
  consumers — scopes, qualification, security classification, the AST — than
  the qualifier side.
- **Restructure `tableIdentifier`** (e.g. `identifier (DOT identifier)?`).
  Changes the tree shape every table consumer reads.
- **Fix it in the runtime.** Not available: context-free continuation at rule
  end is what SLL is.

## Consequences

### Positive

- SLL rejections over the harvested corpus fall from 4,497 to 131 of 15,302
  (29% → 0.9%) in grammar1, and from 30 to 14 of 282 in grammar2; no input SLL
  accepts gets a tree different from LL's.
- Ordinary SQL with qualified names — the common case for anything with a join —
  stays on the memoised path. Measured before/after figures are on the
  background page; statements that also carry a `BETWEEN` or a `CAST` do not
  move (see Negative).
- Two consumers that dereferenced `Identifier()` on the qualifier without a nil
  check (`analysis.ExtractColumns`, play's lineage) panicked on
  `SELECT columns.name FROM system.columns`; they now go through
  `ColumnQualifierName`.

### Negative

- A downstream type switch on `*TableIdentifierContext` that meant to reach
  column qualifiers stops reaching them, silently. Inside this repository the
  audit found four (the two `COLUMNS` canonicalisers, the AST converter, and an
  exclusion in `ExtractTables` that became dead); a differential over every
  harvested input, run once, checks the rest (Verification plan).
- The remaining 0.9% — `CAST(x AS T)` against the alias suffix, the `BETWEEN`
  operand against binary `AND`, and a table function in a FROM item — are not
  expressible as a grammar repair without changing tree types or the language;
  [ADR-0305](./0305-ll-islands-in-the-sll-stage.md) predicts them in
  LL instead.

### Neutral

- The generated parsers are regenerated (~2,700 lines of diff in grammar1).
  Regeneration was checked byte-reproducible on the unmodified tree first.

## Migration — Tier 1

- **Breaks.** `ColumnIdentifierContext.TableIdentifier()` no longer exists in
  either grammar package.
- **Path.** Call `ColumnQualifier()`; its accessors (`DatabaseIdentifier()`,
  `Identifier()`, `ParamSlot()`, `COLUMNS()` in grammar1) match
  `tableIdentifier`'s. Use `nanopass.ColumnQualifierName` for the decoded name.
  Re-check any walk that type-switches on `*TableIdentifierContext` and expected
  to see column qualifiers.
- **Regeneration.** `grammar0/generate.sh` (ANTLR 4.13.2 jar and a JRE).
- **Old shape.** Removed outright.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `TestSLLAcceptsQualifiedNames` (each qualified
  shape must be SLL-clean with LL's tree), `TestSLLNeverDisagreesWhenItSucceeds`,
  `TestSLLFallbackIsLoadBearing` (its witnesses moved to the remaining classes),
  `TestExtractColumns`' `COLUMNS` qualifier case, and `TestPassGolden`, which
  pins every pass, the scope builder, the analyses, highlighting and the
  canonical AST over the in-tree fixtures. The golden was recorded before the
  grammar edit; the edit moved two of its cells, both the `ExtractColumns`
  panic becoming a result.
- **What would fail.** A grammar edit that lets the qualifier's decision see
  FROM-side continuations again fails the first test with an SLL rejection.
- **Gap.** The once-off differential — 15,762 test-suite inputs through every
  pass, the scope builder, the analyses, highlighting and the AST, 613,358
  digests compared old against new — is not a checked-in lane; it needs both
  trees. It differed in the two `ExtractColumns` cells above and in 14
  `InjectParamsAsCTE` cells, which turned out to differ from run to run in
  either tree (map iteration order; fixed alongside).

## Status

Accepted 2026-10-10.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0196](./0196-nanopass-two-stage-sll-parsing.md) — two-stage parsing; §SD6 is the observation this ADR explains.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — the DFA cache the fast path fills.
- [antlr4-go — what the runtime and the generated parser cost](../adr-background-work/antlr4-go-runtime-and-codegen-review.md) — the measurements and the deferred options.
