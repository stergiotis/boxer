---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** An analysis, not a decision. Claims
> about boxer's tree were measured against HEAD on 2026-08-16. Claims about
> ClickHouse's AST-as-JSON export come from reading the upstream issue and the
> merged source at `master` on the same date; they were **not** verified
> against a running build, because the functions are absent from the locally
> installed 26.7.3.19 and no `master` build was available. Every statement
> about the emitted JSON is therefore a source reading, not an observation.

# ClickHouse AST-as-JSON, and what it would take to retire the nanopass CST (August 2026)

[ADR-0002](../adr/0002-nanopass-discipline.md) decided that nanopass operates
on the grammar's CST plus `SelectScope`, and explicitly declined to build an
AST (option O2). The kill-reason recorded there is maintenance: an AST "adds a
translation layer that must track every grammar regeneration", and grammar work
is already the highest-risk area in the package.

That reason is about a **hand-built** AST. It does not obviously apply to an
AST produced by ClickHouse itself. This page works out whether that difference
is enough to reopen the question, and what the upstream and downstream costs
would be.

The short finding: the export largely exists upstream already, it is not
released, and the one field that would make it a CST candidate — source
offsets — is the one field that was specified as optional and left unbuilt.

## 1. What upstream already has

Upstream issue [#88799 "Import/export AST as JSON"][issue-88799] (opened
2025-10-19, still open at the time of writing) specifies close to exactly the
feature this page set out to ask for:

- `parseQueryToJSON(sql)` — returns a JSON document for the parsed AST.
- `formatQueryFromJSON(json)` and `formatQueryFromJSON(json, original_query)` —
  the second form with "best effort to preserve comments, space, and
  indentation from the original query".
- A `dialect` value `clickhouse_json`, so a JSON AST can be submitted **as the
  query**.
- Per-node `begin` / `end` byte offsets into the original query, described in
  the issue as optional and as something that "could be preserved or lost
  during various AST transformations".

Commit `749fbd04` (2026-03-23) implemented most of it across 132 files: virtual
`writeJSON` / `readJSON` on `IAST`, a `createFromJSON` factory dispatching on a
`"type"` field, `serializeASTToJSON` in `src/Parsers/ASTToJSON.h`, and the two
functions in `src/Functions/`.

Two properties of that implementation are worth recording because they are
better than the usual bar for this kind of feature:

- **Round-trip is the stated design goal.** The header comments the invariant
  directly: `createFromJSON(serializeASTToJSON(ast))` should produce an
  equivalent AST.
- **It fails closed.** `IAST::writeJSON` has no default implementation — a node
  type without an override throws `BAD_ARGUMENTS` rather than emitting a
  document that `formatQueryFromJSON` could not read back. Coverage is
  therefore partial and unquantified, but partial in a way that is visible at
  the call site rather than silent.

### Availability

Measured 2026-08-16 by fetching `src/Functions/parseQueryToJSON.cpp` from each
release branch:

| Branch | `parseQueryToJSON.cpp` |
| --- | --- |
| 26.4, 26.5, 26.6, 26.7, 26.8 | absent |
| `master` | present |

The locally installed 26.7.3.19 agrees on both halves of the feature:

```
Code: 46. DB::Exception: Function with name `parseQueryToJSON` does not exist.
Code: 36. DB::Exception: Unexpected value of Dialect: 'clickhouse_json'.
          Must be one of ['polyglot', 'promql', 'prql', 'kusto', 'clickhouse'].
```

So this is unreleased work on `master`, not something a deployed server can be
asked for today.

### `EXPLAIN AST` itself did not gain a JSON mode

The issue asked for "a mode for the EXPLAIN AST statement that will output
JSON". The implementation delivered functions instead. At `master`,
`InterpreterExplainQuery`'s `ParsedAST` case still supports only `optimize` and
`graph` (Graphviz dot); the `json` setting belongs to the plan/pipeline cases.
`EXPLAIN AST` continues to emit the indented text form:

```
SelectWithUnionQuery (children 1)
 ExpressionList (children 1)
  SelectQuery (children 4)
   ExpressionList (children 2)
    Identifier a
    Function count (children 1)
```

Note what that text already shows about the representation, independent of its
encoding: `x > 1` has become `Function greater`, and `1` has become
`Literal UInt64_1`. The AST is normalised at parse time. Nothing downstream of
it can recover the spelling.

## 2. What is missing, and why it is not a small fix

`src/Parsers/ASTToJSON.cpp` emits no `begin`, `end`, or offset field of any
kind. The optional part of the specification was not built.

The reason matters more than the fact. `src/Parsers/IAST.h` carries no
source-position member — ClickHouse AST nodes do not know where in the input
text they came from. Adding `begin` / `end` to the JSON is therefore not a
change to the serializer. It means threading spans from `IParserBase` / `Pos`
into the node type hierarchy, in the most conservative part of the codebase,
for every node type that wants to carry them.

Upstream also states the compatibility posture plainly, in the issue's closing
line:

> The problem is that we can't guarantee compatibility of the AST.

That is the author's own caveat on the feature, not an inference.

## 3. What nanopass needs that this does not carry

ADR-0002's invariants require each pass to take valid SQL and return valid SQL,
and its derived practice names four test categories per pass. The fourth is
"scope preservation for pure passes (case, whitespace, comments, parens)". A
normalised AST cannot satisfy that category by construction.

The two-argument `formatQueryFromJSON(json, original_query)` is the upstream
answer, and reading its implementation shows what it can and cannot promise. It
tokenizes both the original and the canonical rendering, extracts the
significant (non-whitespace, non-comment) tokens from each, walks the two
sequences in lockstep, and reuses the original inter-token material — whitespace
and comments — **where the significant tokens still match**. It is a
realignment heuristic. It holds where a rewrite changed nothing and degrades
exactly where a rewrite changed tokens, which is the work a rewriting pipeline
exists to do.

Separately, the absence of offsets removes the substrate that boxer's
editor-facing consumers are built on:

| Consumer | What it needs from the tree |
| --- | --- |
| `dsl/sqlcomplete` | the node under a cursor byte offset |
| Error-position mapping | a server-reported position mapped back to a statement |
| Body-relative ranges | `env.BodyOffset` arithmetic over node spans |
| Parameter slots ([ADR-0187](../adr/0187-play-sql-expression-parameters.md)) | slot placement diagnostics that point at a location |
| Highlighting | spans, per token |

None of these has a formulation over an offset-free AST.

Blast radius, measured at HEAD on 2026-08-16: **176 files** outside the package
tree import the root `nanopass` package — 238 if its sub-packages are counted
as well.

## 4. The premise that changed, stated precisely

ADR-0002 rejected O2 because a hand-built AST needs a translation layer that
drifts against grammar regeneration. A server-produced AST has no translation
layer — the server is definitionally correct about its own dialect, and a
grammar gap in boxer's ANTLR grammar cannot produce a disagreement because
boxer's grammar is not consulted.

That is a genuine change in the premises, and it is the honest reason to
revisit. It is also narrower than it first looks: it defeats the *drift*
argument only. It says nothing about offsets, about comment preservation, about
release availability, or about a schema whose author declines to guarantee
compatibility. The decision in ADR-0002 survives on those grounds rather than
on the one it originally recorded.

## 5. Where a server AST does pay

The [pipe-operators survey](./clickhouse-pipe-operators-integration.md) reached
a two-tier split for a different upstream feature: a fast in-process path for
editing and analysis, where being wrong costs a wrong *picture*, and a server
oracle before anything executes or is stored, where being wrong costs wrong
*results*. The same split applies here, and the repo already owns the plumbing
— ADR-0028's pre-spawned `clickhouse-local` worker pool
([`queryengine/chlocal`](../../public/keelson/runtime/queryengine/chlocal/)),
and a server-truth harness in
[`dsl_ast_servertruth_test.go`](../../public/db/clickhouse/dsl/ast_test/dsl_ast_servertruth_test.go)
that already shells to a `clickhouse` binary rather than a network endpoint.

Four uses, none of which requires offsets:

- **Structural oracle.** The server-truth harness exists, in its own words, to
  validate "against a real ClickHouse binary instead of against beliefs" — but
  the strongest belief it can currently check is that `clickhouse format -n`
  *accepts* boxer's output. `parseQueryToJSON` would let it check what the
  server *understood*, which is the assertion the harness is shaped for and
  cannot yet make.
- **Grammar gaps close by construction** — for analysis. Constructs the local
  grammar mishandles are not the server's problem.
- **The output side.** `dialect = 'clickhouse_json'` means a generated query
  could travel as an AST document instead of as text, removing the
  generate-then-reparse boundary and the class of defects where boxer emits
  SQL the server rejects.
- **Structured `EXPLAIN` consumers.** [ADR-0153](../adr/0153-play-sql-flow-graph-panel.md)'s
  flow panel would read a document instead of parsing `EXPLAIN` text, and
  [ADR-0141](../adr/0141-play-endpoint-dispatch-seam.md)'s `EXPLAIN AST`
  verdict probe could return structure rather than a boolean.

## 6. The rest of the parser landscape

Three other candidates were surveyed — the first two on 2026-08-16, the third
on 2026-08-17 — because "replace the CST" invites asking whether some other
parser should replace it. None is a replacement either; recorded here so the
question is not re-opened from scratch. All three readings are of source, not
of running builds.

**The Web UI's WASM tokenizer is the C++ lexer, not a parser.** `play.html`
embeds `build/src/Parsers/Lexer.wasm`, which `src/Parsers/CMakeLists.txt`
builds by compiling the server's own `Lexer.cpp` a second time with
`--target=wasm32 -Os -fno-exceptions -fno-rtti -flto -nostdlib
-Wl,--no-entry -Wl,--export-all`. No emscripten, no libc, six exports, driven
by a bare `WebAssembly.instantiate`. It landed over 2025-06 to 2025-09 and is
still what `master` uses. Token-level fidelity is exact by construction, since
it is the production lexer — but it is a lexer, so it yields tokens and no
tree.

**`ClickHouse/clickhouse-analyzer` is a Rust CST parser, outside the server
tree.** rust-analyzer-shaped (marker/event parser, lossless CST, 258 syntax
kinds, token types ported from `Lexer.h`), ~20k lines, an LSP and a VS Code
extension, and a WASM/TypeScript package that is marked `"private": true` and
is not published. Effectively one author; the substantive work is a burst
between 2026-03-31 and 2026-04-24, untouched since.

Its README claims "~95% of ClickHouse SQL". Reading `tests/corpus.rs` and
`.github/workflows/corpus-coverage.yml`, that number is the share of
ClickHouse's own `tests/queries/**/*.sql` that parse with **zero reported
errors** — no comparison against the C++ parser's AST, so a query that parses
into a wrong tree counts as a pass. It is also file-granular: a file scores
clean only if the whole file is error-free, and the headline "statement"
percentage is that same verdict weighted by a naive `split(';')` count. The
test never fails; it is informational. The corpus additionally contains
deliberately-invalid negative tests, which count against it.

Two static readings of the same tree qualify the number in both directions.
The grammar contains 34 `while !p.eof() { p.advance() }` loops; several are
honest error recovery that wraps skipped tokens in `Error` nodes, but others
are "consume remaining body generically" — the `ALTER` tail, access-control
DDL, unknown `SHOW` targets. Those parse clean while producing a flat token
bag. And of the 482 distinct words across the 620 `system.keywords` entries in
the repo's own `generated/keywords.json` — codegen'd from a 26.4.1.485 server —
the parser spells 252 (52%) once `interval_unit.rs` is credited for
the interval set; the gaps cluster in access control and auth (41 words),
MySQL/ANSI compatibility (30), `EXTRACT` date-part names (21), backup and
storage (17), and refreshable materialized views (11) — the same regions the
swallow loops cover. Where it is genuinely deep is the `SELECT` surface:
`FINAL`, `ASOF`/`SEMI`/`ANTI`/`CROSS`, `PREWHERE`, `ARRAY JOIN`, `SAMPLE`,
`QUALIFY`, `WITH FILL`/`TIES`, `INTERPOLATE`, `ROLLUP`/`CUBE`/`TOTALS`,
`GROUPING SETS`, `LIMIT BY`, column transformers, lambdas, recursive CTEs.
Data types are parsed structurally — any bare word plus optional parenthesised
parameters — so new types work without a list update, at the cost of accepting
nonsense ones.

**`polyglot-sql` is in the server, but only on the ingress path.** Commit
`d7bc47e3` (2026-03-14) vendored the third-party crate into
`rust/workspace/polyglot` behind `allow_experimental_polyglot_dialect`. It
takes the raw query text — explicitly bypassing the ClickHouse lexer, because
"foreign dialects may contain syntax that the ClickHouse Lexer cannot tokenize
correctly" — transpiles it to ClickHouse SQL, and hands that to the normal C++
parser. That is how ClickHouse *uses* it, and it is not a statement about the
crate: polyglot has a ClickHouse dialect and parses ClickHouse SQL in both
directions. Because it is the one candidate here with a Go SDK, it was
surveyed on its own terms.

### `polyglot-sql` as a nanopass substrate

Read at `tobilg/polyglot` on 2026-08-17 (upstream 0.9.1; ClickHouse pins
0.1.15). A Rust/WASM SQL transpiler for 30-plus dialects, explicitly "inspired
by sqlglot", 294k lines in the main crate, MIT, created 2026-01-15, 933 stars,
effectively one author, 63 published versions in six months.

It is better than the ClickHouse AST on the two axes §3 cares about, and worse
on the one that matters more:

- **Comments survive.** AST nodes carry `leading_comments`, `trailing_comments`
  and `pre_alias_comments` as `Vec<String>`. Anchored to nodes, not positioned
  in text.
- **Some spans exist.** `Option<Span>` — byte start/end plus line and column —
  on exactly five types: `Identifier`, `Column`, `TableRef`, `Star`,
  `Function`. That is the name-like set, which is the useful half for
  completion and qualification, but `Option` on five of N node types gives no
  general answer for `env.BodyOffset` arithmetic or parameter-slot placement.
- **Still not lossless.** There is no preserve-original mode; the generator
  re-emits text, and preservation is spot-level (numeric literals keep their
  original spelling, window-frame and parameter-mode keywords keep their case).
  Derived practice (4) cannot hold over it either.
- **ClickHouse is a minor dialect.** `dialects/clickhouse.rs` is 698 lines,
  against DuckDB 7715, Snowflake 4114, TSQL 3862, Postgres 1879. A
  ClickHouse-only pipeline would be the demanding consumer of one of the
  thinner dialects.

Three specifics rule it out as a substrate rather than merely disfavour it:

- **Query parameters are absent.** `query_param` / `QueryParameter` has no
  occurrence in the crate. The only `{name:Type}` handling is one line in
  `parser.rs` for *table* position (`{db:Identifier}.table`).
  [ADR-0187](../adr/0187-play-sql-expression-parameters.md) is SQL *expression*
  parameters, already shipped through M5.
- **Aggregate combinators are name-mapped, not grammatical.** `countIf`,
  `sumIf` and `avgIf` are hardcoded entries in the dialect file; ClickHouse
  composes roughly twenty combinators over its whole function surface. (Fairly:
  parametric aggregates, `f(params)(args)`, *are* parsed.)
- **The failure mode is a transpiler's.** `write_unsupported_comment` emits
  `/* message */` **into the generated SQL** when a construct cannot be
  expressed. That is a reasonable diagnostic for a transpiler and a silent
  corruption vector for a pipeline whose contract is valid SQL in, valid SQL
  out.

Operationally it would add a native artefact: the Go SDK uses purego rather
than cgo, calling `libpolyglot_sql_ffi.so`, which the SDK deliberately neither
bundles nor downloads — the caller builds and version-matches it per release.

Where it would be a good fit is the direction boxer does not currently go:
dialect **ingress** (the [ADR-0139](../adr/0139-semantic-layer-text2dsl.md)
text2dsl direction, should non-ClickHouse SQL ever need accepting) and its
column-lineage / OpenLineage output. Recorded here so the substrate question is
not re-opened without those distinctions.

## 7. Costed options

| | What it is | Cost | Blocked on |
| --- | --- | --- | --- |
| **A — adopt as a second lane** | AST JSON as a structural oracle in the server-truth harness, and optionally as the wire form for generated queries | Small; additive, no ADR-0002 conflict, reuses the ADR-0028 worker pool | A release carrying the functions, or a `master` build |
| **B — contribute `begin`/`end` upstream** | Implement the optional offset fields already named in #88799 | Large; source spans must be plumbed into `IAST`, touching the parser | Upstream appetite; the field names are already blessed, so no design negotiation |
| **C — replace the CST** | Retire the ANTLR grammar and nanopass's tree in favour of the server AST | Very large; 176 importing files, and every offset-dependent consumer in §3 needs a new formulation | B shipping, being released, and the schema proving stable in practice against an explicit non-guarantee |

A is worth doing on its own merits and does not commit to B. B is a reasonable
upstream contribution whose value is not boxer-specific, and it is the honest
form of "add an `EXPLAIN AST` export upstream" — the export exists; offsets are
what is absent. C depends on B and on a compatibility promise upstream has
declined to make; it is recorded as an option, not as a plan.

## 8. Verification

Option A is the only one near enough to name a lane for. It would extend the
existing server-truth harness, which skips when no `clickhouse` binary is on
`PATH` — so the lane that goes red is the same one, on the same trigger, with
a structural assertion added. Nothing in the default `go test` lane changes.

For B and C there is no lane to name yet, and "none, because the work is not
scoped" is the accurate entry.

## 9. Open questions

- Which release will carry `parseQueryToJSON`? It is on `master` and absent
  from 26.8, so 26.9 is the earliest candidate — unconfirmed.
- How much of ClickHouse SQL does `writeJSON` actually cover? The fail-closed
  design makes this measurable by running the corpus through
  `parseQueryToJSON` and counting `BAD_ARGUMENTS`, which needs a build.
- Does the round-trip invariant hold in practice, and under which
  transformations? Also needs a build.
- Would upstream accept per-node offsets, given that the AST is reused across
  transformations that would invalidate them? The issue anticipates this by
  making the fields optional; whether a partial implementation is acceptable is
  a conversation to have on #88799 before writing code.

## References

- [ADR-0002](../adr/0002-nanopass-discipline.md) — the decision this page tests.
- [ADR-0098](../adr/0098-nanopass-local-rewrite-combinator-core.md) — the rewrite core the CST feeds.
- [Pipe operators and boxer's SQL stack](./clickhouse-pipe-operators-integration.md) — the earlier survey that reached the same two-tier split.
- [`nanopass/README.md`](../../public/db/clickhouse/dsl/nanopass/README.md) — package overview.
- [`dsl/EXPLANATION.md`](../../public/db/clickhouse/dsl/EXPLANATION.md) — DSL-level architecture rationale.

[issue-88799]: https://github.com/ClickHouse/ClickHouse/issues/88799
