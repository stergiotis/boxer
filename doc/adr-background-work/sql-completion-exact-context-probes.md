---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Probes run 2026-08-16 against the tree
> at that date and a ClickHouse 26.7.3 development endpoint. This page feeds
> [ADR-0190](../adr/0190-sqleditor-exact-completion-context.md); once that
> ADR is accepted the ADR is authoritative and this is a snapshot.

# SQL completion — probes behind the exact-context design

[ADR-0147](../adr/0147-sqleditor-widget-and-completion.md) decided the
completion engine's *shape* (a lexical context tier, catalog probes off the
frame thread, a keyboard-driven list) and deferred scope-awareness. Its
[survey](../explanation/sql-completion-survey.md) never named the problem
that turns out to matter most for boxer's own vocabulary: an **argument
position** whose valid values depend on the enclosing call, the argument's
ordinal, and sometimes the *type* of a sibling argument —
`tupleElement(LW_COMPONENT('SysMem'), '<field>')` being the canonical case
after [ADR-0189](../adr/0189-component-sql-authoring-surface.md). Six small
probes establish what the design can lean on.

## P1 — the server accepts `expr.name` on a named tuple; grammar1 does not

Against ClickHouse 26.7.3:

```sql
SELECT CAST(tuple(1,'x'), 'Tuple(a UInt8, b String)').b   -- → x
SELECT CAST(tuple(1,'x'), 'Tuple(a UInt8, b String)') AS m, m.b   -- → (1,'x')  x
SELECT tuple(1,2).1                                        -- → 1
```

All three execute. grammar1 (`ClickHouseParserGrammar1.g4:211`) has only
`columnExpr DOT DECIMAL_LITERAL # ColumnExprTupleAccess`; the identifier
form is absent, so `LW_COMPONENT('SysMem').TotalBytes` fails to parse
(`missing DECIMAL_LITERAL at 'TotalBytes'`) and never reaches the expansion
pass. `m.b` parses today, but as a `columnIdentifier` (table.column) — the
same spelling as a qualified column, resolved by the server.

The positional form is already canonicalised to the function-call shape:
`tupleAccessToFunctionRule` in
`nanopass_passes_canonicalize_constructors.go` rewrites `x.1` →
`tupleElement(x, 1)`. A named sibling rule would be the same dozen lines.

## P2 — `DESCRIBE (SELECT …)` types an expression without executing it

```sql
DESCRIBE (SELECT CAST(tuple(1,'x'), 'Tuple(a UInt8, b String)') AS m,
                 m.b AS mb, tupleElement(m,'a') AS ma FROM system.one)
-- m   Tuple(a UInt8, b String)
-- mb  String
-- ma  UInt8
```

Exact, alias-aware, no rows read. An unknown identifier fails with
`UNKNOWN_IDENTIFIER` naming it. Two caveats for a consumer: 26.x
pretty-prints compound types across lines in TSV (`Tuple(\n    a UInt8,…`),
so a type-string parser must tolerate whitespace; and a statement calling a
UDF the endpoint lacks fails analysis, so `LW_COMPONENT` can be typed this
way only where the leeway read-back helpers are installed.

## P3 — how the lexer represents the states completion is asked in

`highlight.HighlightLex` on buffers cut at the caret (categories as named in
`dsl_highlight.go`):

| Buffer (caret at end) | Tail of the span stream | `EntityAt` |
| --- | --- | --- |
| `SELECT LW_COMPONENT('Sys` | `LW_COMPONENT`·fn `(` **`'`·plain** `Sys`·ident | name=`Sys`, enclosing=[LW_COMPONENT] |
| `… AS m, tupleElement(m, '` | `tupleElement`·fn `(` `m` `,` **`'`·plain** | name="", enclosing=[tupleElement] |
| `SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot` | `…` `)` `,` **`'`·plain** `Tot`·ident | name=`Tot`, enclosing=[tupleElement] |
| `SELECT LW_COMPONENT('SysMem').Tot` | `)` `.`·punct `Tot`·ident | name=`Tot`, enclosing=[] |
| `SELECT m.Tot FROM boxer.facts` | `m` `.` `Tot` … | (caret past FROM) |

Two facts follow. **An unterminated string literal is a `CatPlain` gap span
for the quote followed by ordinary tokens for its content** — the lexer
gap-fills what it cannot tokenise rather than swallowing the rest of the
buffer. So "the caret is inside an open literal" is recognisable (walk back to
a `CatPlain` span whose text is a quote with no closing partner), and the
literal's typed prefix is the raw text from that quote to the caret — taken
as text, not tokens, since a comma or space inside it would otherwise read as
structure. And a **member-access receiver is recoverable lexically**: the token
before the `.` is either an identifier chain or a `)`, and the paren-balancing
walk `enclosingCallees` already performs finds the call the `)` closes.

## P4 — a sentinel parse gives the exact call frame, sub-millisecond when warm

Replace the token being completed with a sentinel identifier (or put the
sentinel inside the open literal and close it), close the brackets the lexical
walk knows are open, and `nanopass.Parse` the statement:

| Repaired statement | Parse | Sentinel's frame |
| --- | --- | --- |
| `SELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, '§')` | ok | `tupleElement` arg#1, in projectionClause |
| `SELECT tupleElement(LW_COMPONENT('SysMem'), '§') FROM boxer.facts WHERE 1` | ok | `tupleElement` arg#1 |
| `SELECT * FROM boxer.facts WHERE tupleElement(LW_COMPONENT('SysMem'), '§') > 1` | ok | `tupleElement` arg#1, in whereClause |
| `SELECT LW_COMPONENT('§') FROM boxer.facts` | ok | `LW_COMPONENT` arg#0 |
| `WITH c AS (SELECT 1 AS q) SELECT tupleElement(m, §) FROM c` | ok | `tupleElement` arg#1; CTE `c` in scope |
| `SELECT toHour(ts, §)` (bracket closed by repair) | ok | `toHour` arg#1 |
| `SELECT m.§ FROM boxer.facts` | ok | `columnIdentifier` (member access on an identifier) |
| `SELECT LW_COMPONENT('SysMem').§` | **fail** | grammar1 gap (P1) |
| `SELECT a FROM t JOIN §` | **fail** | JOIN wants ON/USING — a table position, which the lexical clause rule covers |

Timings on the bounded DFA cache (ADR-0084): 5–18 ms for the first parses of
a session while the DFA warms, then 55–600 µs per statement of this size.
Warm-up alone rules out running it on the render thread; the widget's
`semanticTier` (a `bgjob.Runner`, content-keyed supersession) is the
precedent for where it runs.

## P5 — `system.functions` carries prose, not signatures

26.7's `system.functions` has `syntax`, `arguments`, `parameters`,
`returned_value`, `examples`, `introduced_in`, `categories`. For
`tupleElement`: `syntax = tupleElement(tuple, index|name[, default_value])`,
`arguments` a markdown list. Useful for item documentation and signature
help; not machine-readable domains. So which argument positions have a closed
domain (`tupleElement` name ← the tuple's elements, `CAST` type ←
`system.data_type_families` (140 rows), `toDateTime` tz ←
`system.time_zones` (597), `dictGet` ← `system.dictionaries`) has to be
declared client-side, and only for the handful of built-ins where it pays.

## P6 — the vocabulary rosters: six `Function` types around one `Params []string`

`glosssql`, `identsql`, `readback` and `constructsql` declare
`{Name string; Params []string; Doc string}`; `chpack` adds `Body` (it is
the server-installed roster), `lwsqlsurface` adds `Family`. play's
Vocabulary tab (ADR-0174) copies each into its own `vocabEntry`. `Params` are display strings (`"'Kind'"`, `"'section'"`). No
roster declares what an argument *is*, and no shared type exists to declare
it on. The registry a domain resolves against exists in every case:
`componentsql.Default.Kinds()`, the introspection catalogue, the leeway
schema of the bound table (ADR-0147 §SD9's reader), `passreg`.

The `Projection` a kind publishes bakes its slot list only as the CAST's type
literal — `'Tuple(Id UInt64, NaturalKey String, Ts DateTime64(9,\'UTC\'), …,
UsageWatts Nullable(Float32), ActiveCPUs Array(Int32))'` — bare Go field
names, nested type arguments, an embedded string literal. Any consumer that
wants the element names either parses that or asks the generator to emit
them a second time.
