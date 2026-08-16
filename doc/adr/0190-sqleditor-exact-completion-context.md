---
type: adr
status: proposed
date: 2026-08-16
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0190: sqleditor completion — an exact caret model, typed argument domains, and a completion pane

## Context

[ADR-0147](./0147-sqleditor-widget-and-completion.md) extracted the SQL
editor and decided the completion engine's shape: a lexical context tier
(§SD5), catalog probes off the frame thread (§SD6–SD9), and a keyboard-driven
popup fed by a new `TextEdit` key-capture method (§SD10–SD12). Its M0
shipped. Its M1 — context and candidates — is unbuilt, and scope-aware
context was deferred with a trigger. Two things have moved since.

**Boxer's own vocabulary is completed at argument positions.**
[ADR-0189](./0189-component-sql-authoring-surface.md) made this sharp: a
component read is `tupleElement(LW_COMPONENT('SysMem'), 'TotalBytes')`, and
both literals are known in-process — the kind from `componentsql.Default`,
the field from the kind's Projection type. `keelson('<table>')`,
`LW_GET(x, '<section>', 'chan:…')`, `LW_TAGGED(e, '<section>', …)` and the
gloss functions have the same shape. What may go there depends on the
enclosing call, the argument's ordinal and — for the field — the *type* of a
sibling argument. A clause classifier cannot say, and ADR-0147 has no tier
that can.

**The requirement is precision.** A list that is right, not one that is long.
That is achievable where a source of truth is in-process (registries, the
grammar, the parsed statement) or the server answers a question (`system.*`,
`DESCRIBE`) — and nowhere else, so the design commits to silence over
guessing. Precision also changes what the UI is for: showing that what the
user typed *resolves* matters as much as helping them type it.

Facts the probes established
([background page](../adr-background-work/sql-completion-exact-context-probes.md)):

- ClickHouse accepts `expr.name` on a named tuple. grammar1 has only `expr.N`,
  which is already canonicalised to `tupleElement(x, 1)`. The dot form is a
  grammar1 gap.
- `DESCRIBE (SELECT …)` yields exact, alias-aware types without executing.
- The lexer turns an unterminated literal into a `CatPlain` quote gap plus
  ordinary tokens; a member-access receiver sits before the `.` as an
  identifier or a `)`. Both are recoverable by the paren-balancing walk
  `enclosingCallees` already does.
- A sentinel parse — the partial token replaced by a sentinel, the brackets
  the walk knows are open closed — parses every driving case in grammar1 with
  the exact callee, ordinal, aliases, CTEs and clause: 55–600 µs warm,
  5–18 ms while the DFA warms.
- `system.functions` describes arguments in prose. Six roster packages
  declare their own `Function` around the same `Params []string`; none says
  what an argument *is*.

## Design space (QOC)

**Question 1 — How does the engine learn what surrounds the caret?**

- **O1** — **Lexical site.** Extend `enclosingCallees` into a frame walk:
  callee, ordinal, sibling ranges, open literal, member receiver.
- **O2** — **Sentinel parse.** Deterministic repair from O1's open-bracket
  knowledge, a sentinel at the caret, `nanopass.Parse`; the tree adds
  aliases, CTEs, FROM and clause.
- **O3** — **`ParseBestEffort`.** Keep ANTLR's error-recovered tree.
- **O4** — **Grammar ATN follow sets** (antlr4-c3), unchanged from ADR-0147.

C1 answers while typing; C2 exact callee and ordinal; C3 alias, CTE and FROM
knowledge; C4 cost per keystroke and carry.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | +  | +  | ++ |
| C2 | ++ | ++ | −  | ++ |
| C3 | −  | ++ | +  | −  |
| C4 | ++ | +  | +  | −− |

O1 and O2 are one model with two producers: O1 answers every frame, O2
upgrades it a beat later. O3 is killed on C2 — recovery is worst at the
caret. O4 stands rejected as in ADR-0147.

**Question 2 — Where does an argument position's domain come from?**

- **D1** — declared beside the roster, on a shared leaf `Param{Name, Domain}`.
- **D2** — a central signature table in the engine, coverage-tested.
- **D3** — parsed out of the `Params` display strings.
- **D4** — the server's `system.functions.arguments`.

D3 and D4 are prose. D2 drift-proofs by test what D1 drift-proofs by type,
one package away from the function it describes. **D1 is taken**; the few
ClickHouse built-ins with a closed literal domain (`tupleElement`, `CAST`,
time zones, `dictGet`) get a curated table in the engine, because no roster
exists for them.

**Question 3 — How do candidates reach the user?**

- **P1** — **Popup with key capture** (ADR-0147 §SD10–SD12): a `TextEdit`
  method steals Up/Down/Enter/Tab; IDL, regen, Rust.
- **P2** — **Completion pane with two-way match highlighting**: a docked table
  shows the domain with its context; the user types; the matching row and the
  typed token light up; a click completes.
- **P3** — **Click-only strip** (ADR-0147 M3).

E1 no IDL or FFI change; E2 shows context (types, docs, the whole
component); E3 verifies what was typed; E4 keystrokes to a long name.

|    | P1 | P2 | P3 |
|----|----|----|----|
| E1 | −− | ++ | ++ |
| E2 | −  | ++ | −  |
| E3 | −  | ++ | −  |
| E4 | ++ | +  | +  |

**P2 is taken.** P1 is not rejected: once the pane is the list, keyboard
acceptance is one captured key (Tab), which is M5. P3 is P2 with one column
and no highlight.

## Decision

We will build ADR-0147's M1 as an **exact caret model** — a lexical site per
frame, upgraded by a sentinel parse on quiescence — add **typed argument
domains** declared per parameter beside each vocabulary roster, and present
candidates in a **completion pane** whose match state is rendered in both the
pane and the editor. Keyboard acceptance follows as one captured key. This
amends ADR-0147 §SD5 and §SD10–SD12, discharges its O2 deferral, and leaves
its §SD6–SD9 standing.

### The contract

- **SD1 — Precision over recall.** A candidate is offered only from a source
  of truth: a registry, the grammar's vocabulary, the parsed statement's own
  names, or a catalog answer for the buffer's endpoint. Where none exists the
  engine offers nothing, and says so. Every candidate carries its provenance
  and its replace range. No "plausible" band below an "exact" one.

### The caret model

- **SD2 — The lexical site lives in `highlight`, beside `EntityAt`.**
  `highlight.SiteAt(spans, off)` returns the frame stack (callee, ordinal,
  ranges of the sibling arguments on both sides of the caret), the literal the
  caret is inside (its prefix is the raw text from the quote to the caret —
  text, not tokens, since a comma inside it is not structure), the
  member-access receiver when the previous significant token is `.`, and the
  partial token completion replaces. The literal may be terminated as well as
  open: a caret moved back into a complete literal is the state §SD9 tints,
  so the site reports the typed prefix and the token's whole text separately.
  Pure over the span stream; answers on a buffer that does not parse.
  ADR-0147's caret entity becomes one field of it, and the widget publishes it
  in `Result`.
- **SD3 — The scope is a sentinel parse, on a worker.** `sqlcomplete.Scope`
  comes from `nanopass.Parse` of the caret's statement after a deterministic
  repair: the partial token replaced by a sentinel (inside the open literal
  if there is one, which is then closed) and the site's open brackets closed.
  Two attempts — tail kept, then tail cut — and the first that parses wins;
  if neither, the site alone is the model. The tree gives the frame, the
  alias→expression map, CTE names, FROM sources and the clause. For a
  comma-separated call the tree's frame must agree with the site's; for
  keyword-syntax calls (`CAST(x AS T)`, `EXTRACT(HOUR FROM ts)`) the site
  reports no ordinal and the tree's is the only one. It runs on a
  `bgjob.Runner` keyed by (statement, caret) with a quiescence shorter than
  the semantic tier's; prefix filtering stays per frame, so in-process answers
  are live per keystroke and only scope-dependent ones wait.

### Domains and types

- **SD4 — Domains are declared beside the roster.** A leaf `dsl/sqlvocab`
  owns `Function{Name, Params []Param, Doc}` and `Param{Name, Domain}`.
  `Domain` is data: a kind, plus the ordinal of the sibling it depends on where
  the kind needs one — component kind; element of a tuple-typed sibling;
  introspection table; leeway section of the table in scope; membership
  channel of a sibling section; database, table and column; type name; time
  zone; setting; free expression. The leeway extraction family's trailing
  `col:` / `chan:` / `param:` tokens are ONE kind rather than three, because
  the surface takes them in any order: the ordinal does not say which one a
  position holds, only the prefix typed there does, and declaring them
  per-ordinal would offer channels where a column belongs. The six rosters adopt `[]sqlvocab.Param`, and `Function` carries
  the `Where` (server / client / host, as a bitset, since `LW_ID_*` is
  genuinely both a server UDF and a client macro) and `Family` the vocabulary
  panel already shows. A repeating tail is read off the parameter's own
  display name, which the rosters already spell with an ellipsis. **The union of rosters is one host-wired registry**,
  `sqlvocab.Default`, populated where passes and components are registered;
  the panel's `vocabDeclared()` and the engine both read it, so a roster
  cannot reach one surface and not the other. The zero `Domain` is
  *unspecified* and a test refuses it in every roster. Adding a kind is
  adding a member; resolvers are wired by the host, per buffer, through
  ADR-0147 §SD7's provider, from the providers SD12 lists.
- **SD5 — An in-process typer answers type-dependent domains.**
  `sqlcomplete.Typer` maps an expression to a ClickHouse type when it can —
  off the lex tier rather than off a CST, because the shapes below are
  syntactically shallow and the parser costs 5–18 ms while its DFA warms:
  a literal; `LW_COMPONENT('K')` via a host-wired hook; the three CAST
  spellings; `tuple(…)`; `tupleElement`, `.N` and (after SD11) `.name`; an
  alias via the scope's map; a column via the typed column probe. Anything
  else is *unknown*, and unknown yields nothing (SD1). Type strings — the
  Projection literal, `system.columns`, `DESCRIBE` — are parsed by a leaf
  `chtype` package covering the compound types (Tuple with named elements,
  Nullable, Array, Map, LowCardinality, Nested, parameterised names with
  literal arguments).
- **SD6 — The Projection's elements are parsed, not emitted twice.**
  `componentsql.Binding.Elements()` parses the Projection's own type literal
  via `chtype`, pinned by a test against the generator's slot list. No
  generated-store change: the string is what the server sees, and the parser
  is needed for the column and `DESCRIBE` cases regardless.
- **SD7 — Member access is one shape with several receivers.** `X.` completes
  the members of X: an alias of a tuple-typed expression → its elements; a
  table alias or table → its columns; a database → its tables. A call or
  parenthesised receiver (`LW_COMPONENT('SysMem').`) is typed by SD5 and
  offered only once SD11 makes it executable.

### The pane

- **SD8 — The completion pane is a table with a match state, not a popup.**
  It shows the domain at the caret with its context: name, type or kind,
  provenance, one line of doc — for a component, the kind and *all* its
  fields; for a table, all its columns. Small closed domains show whole with
  the match highlighted; large ones (functions, columns) filter by prefix. The
  match state is computed per frame from the partial token, case-sensitively:
  *none*, *prefix* (the rows that extend it), *exact* (the row that equals it).
  The pane takes no input focus and captures no key: rows are ordinary
  widgets, so the headless tree driver can assert them. It is built on the
  Vocabulary tab's substrate (ADR-0174), not beside it: the same row cells
  (call, endpoint mark, Insert, doc), the same routing of a name into the
  Docs pane, and one `system.functions` lane widened to every origin, which
  the tab filters to `origin != 'System'`. Function rows carry the tab's
  ✓ / MISSING / dependency marks — offered *with* the mark, never hidden,
  since hiding a MISSING function would hide the provisioning fact the tab
  exists to show. The tab remains the browse-and-reconcile view; the pane is
  the glance at the caret. Play docks it beside the Docs pane; `sqleditor`
  offers the table as a sub-widget for an embedder without a dock.
- **SD9 — The same match state tints the editor.** The token under the caret
  gets an exported `ToneResolved` on an exact match and nothing otherwise —
  never an error tone while it is being typed. On the quiescence parse, every
  other literal in a closed in-process domain is validated: resolved gets the
  quiet tone, unresolved gets `ToneError` — ADR-0189 §SD5's refusal, shown
  while typing. A catalog probe that has not answered produces no tint (the
  vocabulary panel's rule: `?`, never `MISSING`). Play composes these into
  `Decoration.Styled` as it does the subquery mark.
- **SD10 — Completing is a suffix insert; Tab is one key, later.** Clicking a
  *prefix* or *exact* row inserts the candidate minus the typed prefix through
  `InsertAtCursor`, valid because the caret is at the partial's end — no
  replace-range, no IDL. Rows outside the match state are reference only.
  Tab, in M5, completes the unique prefix match or extends to the longest
  common prefix of the matching rows, shell-style, and needs only ADR-0147
  §SD10's key-capture method for that one key: no arrow keys, no Enter, no
  anchoring.

### The companion grammar change

- **SD11 — grammar1 gains `columnExpr DOT identifier`, canonicalised.** The
  target is `tupleElement(expr, 'name')`: one alternative beside
  `ColumnExprTupleAccess`, one rule beside `tupleAccessToFunctionRule` in
  `CanonicalizeConstructors`. `columnIdentifier` stays greedy for `a.b` and
  `a.b.c`, so the alternative fires only after a non-identifier primary; a
  golden pins both. Separable, sequenced early because it makes the headline
  spelling writable.

### The providers

- **SD12 — Providers in scope.** Every candidate traces to one provider
  below; the letter says where the truth lives, which fixes exactness, cost
  and endpoint dependence. **A** is in-process and closed — exact, no I/O,
  live per keystroke. **B** is a server catalog — exact per endpoint, fetched
  by an ADR-0147 §SD6 probe (off the frame thread, cached, "not yet" until it
  answers) and routed per buffer (§SD7). **C** is excluded by SD1. `▏` marks
  the caret.

  | # | Provider | Feeds | Source of truth | Status |
  |---|---|---|---|---|
  | A1 | Component kinds | `LW_COMPONENT('▏')`, `LW_COMPONENT_FILTER('▏')` | `componentsql.Default.Kinds()` | exists |
  | A2 | Component fields, typed | `tupleElement(LW_COMPONENT(k),'▏')`, `m.▏`, `LW_COMPONENT(k).▏` | Projection type literal via `Elements()` | SD6 |
  | A3 | Introspection tables | `keelson('▏')` | the `introspect` catalog (33 names today) | exists |
  | A4 | Ad-hoc dataset aliases | `keelson('▏')` (ADR-0134 §SD4) | play's `BindDataset` bindings, per buffer | exists |
  | A5 | Function rosters — names, params, docs | function positions; signature help; the doc column | `sqlvocab.Default` (SD4): the `LW_` families, the client macros, play's `ts*` | exists; SD4 adds `Domain` |
  | A6 | Membership names and ids | `LW_GET(x,'section','▏',…)` and siblings | membership registry (`keelson('memberships')`) | exists |
  | A7 | Aspect vocabularies | `LW_TAGGED(…,'enc:▏/sem:…/use:…')`, `LW_PLAIN(…,'item:▏')` | `encodingaspects` / `valueaspects` / `useaspects` enums | exists |
  | A8 | Canonical types | the `'type'` param of `LW_PLAIN` / `LW_TAGGED` | `canonicaltypes` grammar | exists |
  | A9 | Channels and support roles | `LW_MEMBERSHIP(…,'▏')`, `LW_SUPPORT(…,'▏')`, `'chan:▏'` | constants in `constructsql` / `lwextract` | exists; enumerate |
  | A10 | Gloss catalog | `gloss(expr,'gloss/▏','key▏',…)`; the `"x@gloss/…"` alias spelling | `gloss.Default()` names and parameter keys | exists |
  | A11 | Identity tags | `LW_ID_HAS_TAG(x, ▏)` | tag registry (`identity/tagmint`) | exists |
  | A12 | Grammar vocabulary | keywords per clause, JOIN kinds, INTERVAL units, `FORMAT` | grammar1 lexer `LiteralNames` | exists |
  | A13 | The statement's own names | aliases (`m.▏`), CTEs in `FROM ▏`, window names in `OVER ▏` | the sentinel parse | SD3 |
  | A14 | Param slots and expression params | `{▏:` names, `{x:▏}` types, `SET param_▏` | `collectParamSlots`; ADR-0187 expression params | exists |
  | A15 | Snippets | statement templates | help-corpus fenced SQL | exists |
  | A16 | Run history | recent statements | `play_runs_history.go` | exists; template-grade |
  | A17 | Semantic layer | measures and dimensions, with docs | ADR-0139 | not built |
  | B1 | Databases | `FROM ▏`, `db.▏` | `system.databases` | new probe |
  | B2 | Tables, views, table functions | `FROM db.▏`, `JOIN ▏`, `FROM ▏(` | `system.tables`, `system.table_functions` | new probe |
  | B3 | Columns with types and comments | `SELECT ▏`, `WHERE ▏`, `t.▏`; the typer's input; subcolumns | `system.columns` | exists names-only, inline for the passes; a second, typed, off-thread lane for the pane |
  | B4 | Leeway physical-schema reader (ADR-0147 §SD9) | `LW_GET(x,'▏',…)` sections; friendly `section:column` handles | derived from B3's physical names, sharing the Resolver's derivation | the reader |
  | B5 | Functions and combinators | function positions; syntax and examples in the pane | `system.functions`, `system.aggregate_function_combinators` — the Vocabulary tab's lane (SD8) | widen |
  | B6 | Settings and their values | `SETTINGS ▏`, `SET ▏`, enum-typed `= ▏` | `system.settings`, `system.merge_tree_settings` | new probe |
  | B7 | Data types | `CAST(x,'▏')`, `x::▏`, `{p:▏}` | `system.data_type_families` | new probe |
  | B8 | Time zones | `toDateTime(x,'▏')`, `DateTime('▏')` | `system.time_zones` | new probe |
  | B9 | Dictionaries and attributes | `dictGet('▏','▏',k)`, `dictHas` | `system.dictionaries` | new probe |
  | B10 | Formats | `FORMAT ▏`, `url(…,'▏')` | `system.formats` | new probe |
  | B11 | Enum values | `WHERE col = '▏'`, `IN ('▏')` on Enum columns | B3's type string via `chtype` | SD5 |
  | B12 | Documentation | the doc column; the Docs pane | `system.documentation` | exists |
  | B13 | Expression types | member access on what the typer cannot type | `DESCRIBE (SELECT …)` | M4 |
  | B14 | Distinct values | literals on low-cardinality columns | `SELECT DISTINCT … LIMIT n` | on explicit request only — a real query |
  | B15 | Clusters and macros | `cluster('▏')`, `remote(…)` | `system.clusters`, `system.macros` | niche |
  | C1 | Buffer words | identifiers typed elsewhere in the buffer | none | excluded — admitted only when they resolve through A or B |
  | C2 | Recency and frequency | — | usage | a ranking signal, not a provider |

  A5 is the spine: every argument domain in A6–A11 is a `Param.Domain` on a
  roster function, and B7–B9's closed literal domains hang off the curated
  built-in table (SD4). Two things cut across the table: the introspection
  plane answers B-questions from the `introspect` catalog rather than
  `system.*`, and ad-hoc datasets contribute tables no `system.tables`
  enumerates — which is why the catalog resolves per buffer.

### Milestones

- **M0 — Leaves.** `chtype`; `sqlvocab` and the roster adoption;
  `componentsql.Binding.Elements()`. Headless, golden-pinned.
- **M1 — Site, domains, typer, pane.** SD2 in `highlight`; `sqlcomplete` over
  in-process sources; the pane with match highlighting and suffix click. The
  driving cases — `LW_COMPONENT('|`, `LW_COMPONENT('Sys|Mem')`,
  `tupleElement(LW_COMPONENT('SysMem'), '|`, `keelson('|` — are visible with
  no server and no parse.
- **M2 — Scope and validation.** SD3 on the worker; aliases, CTEs, FROM; the
  typed column probe (`system.columns` now carrying `type`); off-caret
  validation tints; `tupleElement(m, '|`, `m.|`, `tupleElement(col, '|`.
- **M3 — SD11.** Grammar, regen, canonicalise rule, goldens; the typer's
  `.name` case; call-receiver member access.
- **M4 — Server type oracle.** `DESCRIBE (SELECT <expr> …)` after the
  pre-execute passes, async and cached, for expressions SD5 cannot type.
  **Trigger:** the first request for member completion on a built-in
  function's result or a Nested column.
- **M5 — Tab.** ADR-0147 §SD10's key-capture method, for one key; the pane
  is unchanged.
- **M6 — Record and close.** Dated Update on ADR-0147 (§SD5, §SD10–SD12, O2,
  M1, M4); help-corpus and vocabulary-panel paragraphs; the lanes below.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `dsl/sqlvocab` (exported Go API) | new — `Function`, `Param`, `Domain` (SD4) | six roster packages; play's vocabulary model |
| `glosssql`/`identsql`/`chpack`/`readback`/`lwsqlsurface`/`constructsql` `Function` | reshaped — `Params []string` → `[]sqlvocab.Param` | `play_vocab_model.go`; roster tests |
| `dsl/sqlcomplete` (exported Go API) | new — `Scope`, `Typer`, domains, match state, provider interfaces | `widgets/sqleditor`; the pane |
| `chtype` (exported Go API) | new — ClickHouse type-string parser (SD5) | `componentsql`, `sqlcomplete` |
| `highlight` | added — `SiteAt`, `CaretSite` (SD2) | `sqleditor.Result` |
| `componentsql.Binding` | added — `Elements()` (SD6) | nothing; additive |
| `passes.SchemaProviderI` | **unchanged** — the pane's typed columns come from its own probe lane, so the synchronous pass path needed no sibling | nothing |
| `sqleditor` tones and `Result` | added — `ToneResolved`; the site outwards; the pane sub-widget | play, `sqlappletcreator` |
| play's vocabulary model and probe (ADR-0174) | reshaped — `vocabDeclared()` reads `sqlvocab.Default`; the probe covers every origin on one lane | the Vocabulary tab; the pane |
| grammar1 `ClickHouseParserGrammar1.g4` (generated-code input) | added — `ColumnExprTupleAccessNamed` (SD11) | regenerated parser/visitors; `CanonicalizeConstructors`; grammar goldens |
| egui2 IDL (`TextEdit` key capture) | **unchanged until M5**, then ADR-0147 §SD10 as decided there | Rust interpreter, generator, both sides rebuilt |
| The `LW_` namespace, facts schema, DDL, wire formats | **unchanged** | nothing |

## Alternatives

- **`ParseBestEffort` for scope (O3).** Killed: recovery is least reliable at
  the caret, the only place the answer is read.
- **Heuristic buffer repair.** Killed: the site already knows which brackets
  and literal are open, so the repair is deterministic.
- **A central signature table (D2).** Killed on locality: the author of a
  roster function should declare its argument domains where they declare it.
- **Emitting `Slots` from `recordstore/gen`.** Killed: a regen and more bytes
  on the store ADR-0189 measured at +55 %, for a fact the literal carries.
- **Server as the only type oracle.** Killed: silent about `LW_COMPONENT` on
  any endpoint without the read-back helpers, and a round trip where a
  registry answers in-process. Kept as the fallback rung (M4).
- **A `tupleElement`+`LW_COMPONENT` special case instead of a typer.** Killed:
  it answers one spelling and nothing for `AS m` / `m.field` /
  `tupleElement(col, …)`.
- **Popup first (P1).** Killed on E1–E3: an IDL, Rust and generator change to
  deliver a narrower view of the same candidates, with no verification of what
  was typed. Its one key survives as M5.
- **Two-band candidate lists.** Killed by SD1.
- **Folding the pane into the Vocabulary tab as a "fits here" mode.**
  Deferred, not rejected: one table, but it turns an accepted provisioning
  report — sectioned by where a function *runs*, which means nothing for a
  kind, a field or a column — into the completion surface. **Trigger:** the
  two surfaces found open side by side showing the same function rows.
- **Leaving grammar1 without the dot form.** Rejected as a default: the server
  accepts the spelling and the canonical form exists for the positional case.

## Consequences

### Positive

- Boxer's own vocabulary is completed exactly, from the registries that
  define it — kinds, fields, tables, sections — with no server round trip.
- Domains live beside the functions they describe; a roster cannot grow a
  function that says nothing about its arguments.
- The first visible feature needs no IDL, Rust or generator change; the one
  key that does (M5) is a small, isolated step.
- The editor validates closed-domain literals while typing — a wrong kind or
  field is visible before the run refuses it.
- ADR-0147's O2 deferral is discharged with a deterministic repair.

### Negative

- Six roster types change shape in one commit; mechanical, but wide.
- Two leaf packages and one engine package to own; the built-in signature
  table is hand-curated and lags ClickHouse's function set by design.
- The typer is a closed list; outside it the pane is silent until M4.
- Long names are typed by hand until M5; the suffix click closes most of that.
- A pane needs screen space adjacent to the editor; hidden, there is no
  completion at all.
- A grammar1 change is a regen with a hand-provisioned ANTLR jar
  (`grammar0/generate.sh`) and touches every visitor.

### Neutral

- Scope-dependent candidates are one quiescence window behind the buffer, as
  ADR-0147 §SD6 accepts; in-process ones are not.
- Someone with IDE habits pressing Tab before M5 gets a tab character.
- SD11 changes what the pipeline accepts, not what any query means.

## Migration — Tier 1

- **Breaks.** In-repo readers of the rosters' `Params []string` (the
  vocabulary model, roster tests) until they read `Param.Name`. Nothing
  outside the repository consumes them.
- **Path.** M0 first, one commit per surface: `chtype`, then `sqlvocab` with
  the six adoptions and the panel, then `Elements()`. Everything after is
  additive.
- **Regeneration.** grammar1 only (SD11), Go side. The IDL regen belongs to
  M5 and is ADR-0147 §SD10's, both sides rebuilt.
- **Old shape.** `Params []string` removed outright; nothing else has one.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the site walk, repair-and-parse goldens,
  the typer, `chtype`, `Elements()`, the roster domain test, the match state
  and grammar goldens; the headless tree driver (ADR-0154) for the pane; the
  `//go:build integration` lane for the probes, `DESCRIBE`, and the precision
  oracle.
- **What would fail.**
  - **The contract, made mechanical.** For each driving case, every candidate
    the engine offers, spliced into the buffer, must execute (`LIMIT 0`)
    against the live endpoint; and the offered set for
    `tupleElement(LW_COMPONENT(k), '…')` must equal `Elements(k)` for every
    registered kind — no more, no less.
  - **The two-way highlight.** With the caret token `TotalBytes` in the field
    position, the driver finds exactly one highlighted row and the editor's
    styled section over the token carries `ToneResolved`; with `TotalByte`
    (prefix) it finds the prefix rows and no editor tint; with `Foo` off the
    caret on quiescence, `ToneError`.
  - Site walk: open literal with a comma or space inside; nested frames; a
    `)` before `.`; the ordinal after a comma inside a closed sibling.
  - Repair: both attempts on each driving case; the site's frame equals the
    tree's for comma-separated calls; a JOIN position falls back to the site.
  - `chtype` on every registered kind's Projection literal and a captured
    `system.columns` sample, including a multi-line `DESCRIBE` type.
  - Roster test: no `Param` with the zero `Domain`.
  - Grammar: `a.b`, `a.b.c` still parse as `columnIdentifier`; `f(x).n`
    parses as the new alternative and canonicalises to `tupleElement`.
  - Suffix click: the buffer after clicking `SysMem` on `'Sys` reads
    `'SysMem`; a click with the caret mid-token changes nothing.
- **Gap.** M4 is unbuilt until its trigger, so built-in result types are not
  verified anywhere. The built-in signature table covers what is curated. An
  unanswered probe's *absence* of tint is asserted, not its later arrival.

## Status

Proposed — awaiting review by the code owner. **M0–M3, M5 and M6 are built**;
M4 stayed behind its trigger. The decisions above have been edited in place
where the implementation overturned them, which is what the proposed stage
permits (DOCUMENTATION_STANDARD §1's Tier-1 policy); §Updates records what
shipped and names each in-place correction, so a reviewer reading the body sees
the design as built rather than as first written.

ADR-0147 keeps authority over the widget, the probes and the key-capture
mechanism, and has its dated Update recording that this ADR replaces its §SD5
and M1, reduces §SD10–SD12 and M4 to one key (this ADR's M5), and discharges
its O2 deferral.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-16 — M0–M3, M5 and M6 shipped; M4 stayed gated

**M0 — the leaves.** `chtype` parses ClickHouse type strings (a name, an
optional argument list, arguments that are nested types, named elements or
literals) and knows no type names, so a family a newer server adds parses
without a change. `componentsql.Artefacts` gained `ProjectionType` and
`Elements`, which read the slot list back off the Projection's own CAST
literal; the pin lives in `readback`, beside the emit it depends on.
`dsl/sqlvocab` carries `Param{Name, Domain}` and the six rosters adopted it.

Four things the implementation added to §SD4. `Where` is a **bitset** and its
third member is `WhereHost`, not "play" — the leaf is host-agnostic and the
panel's section titles stayed in play. `Registry.Lookup` answers with a
**slice**, because `LW_ID_*` is declared twice (once per population) and one
of the two answers would otherwise be lost; `Signature` is the accessor a
completion engine wants. `Domain.Ref` is `NoRef = -1` rather than zero, since
zero is a valid ordinal and a domain that silently read argument 0 would be a
wrong answer rather than an absent one — registration validates it. And the
extraction family's trailing tokens share one domain rather than three, for
the reason now written into §SD4.

**M1 — site, engine, pane.** `highlight.SiteAt` answers per frame. Two
corrections the code forced: a frame's callee is any nameable token butted
against the `(`, not only one the lexer marked a function name — `CAST` and
`EXTRACT` lex their names as keywords and are exactly the frames whose ordinal
rule differs; and the literal site covers a *terminated* literal too, with the
typed prefix and the token's whole text reported separately, which is what
§SD9's "the caret moved back into a complete literal" needs.

`dsl/sqlcomplete` resolves member access, then the innermost frame with a
known signature, then the clause. Three additions to the domain list:
database, table and column are domains and not only providers (§SD12 lists
them as B1–B3 but nothing named what asks for them, and both a member access
on a table alias and a FROM position do); a repeating tail is read off the
parameter's own display ellipsis, which is how the rosters already spell it;
and a free expression position resolves through a host-composed `Expressions`
provider — a column and a callable name are both valid there, so offering only
one would be exactly as wrong as offering neither. The tuple-element domain is
answered by the typer rather than by a provider, because what names the
elements is an expression's type and a provider keyed on a component kind
would serve one spelling and nothing else.

The pane shows a domain whole up to `PaneWholeDomainMax = 64` candidates and
filters larger ones to the matches; the number is set by the closed in-process
domains, all of which fit, against the server's (columns, functions, 597 time
zones), which are the ones a reader narrows by typing. play docks it as the
`completion` tab (dock id 28), registered NOT lazy: the engine runs from the
editor's `Bind` whether the tab is open or not, because the editor's own tint
reads the same answer.

**M2 — scope and validation.** The repair and the sentinel parse are as §SD3
describes. The tree also answers the keyword-syntax ordinal the site refuses to
guess, and its frame must name the same call the site did — the caret can move
between the launch and the drain. The tier keys on (statement, caret) rather
than on content, since moving the caret changes which frame the answer is
about; its quiescence is 150 ms against the semantic tier's 400.

`passes.SchemaProviderI` was **not** given a typed sibling. The pane's typed
columns come from a catalogue lane of its own — off-thread, cached, per
endpoint — and the synchronous path the passes use is untouched, so the
interface would have had no consumer. The Surfaces table above is corrected.
The vocabulary probe now lists every origin, with the tab filtering to
`origin != 'System'` and the pane reading the whole listing: one lane, two
readers.

**M3 — SD11.** One grammar1 alternative, one canonicalise rule, goldens on
both halves (`f(x).n` canonicalises; `a.b` and `a.b.c` stay qualified columns).
grammar0 and grammar2 regenerated unchanged. The typer's `.name` case composes,
so `LW_COMPONENT('k').a.b` is two of them, and play turns on §SD7's call
receiver.

**M4 — not built.** Its trigger — the first request for member completion on a
built-in function's result or a Nested column — has not fired, and the ADR
gates it on that. The typer's closed list is what answers today; outside it the
pane is silent with its reason, which is the designed behaviour rather than a
gap. **Trigger unchanged.**

**M5 — Tab.** A `captureTab` builder method on `TextEdit` whose consume runs
before the widget, reporting through ADR-0177's key-capture register rather
than a new per-widget value channel: that register already carries a code and
its modifiers, is already drained at frame end and already re-grouped by widget
id. Verified live through egui-mcp — `LW_COMPONENT('SysM` + Tab becomes
`'SysMem`, and a second Tab on the complete name inserts a tab character.

**Verified live** (play, headless weston, egui-mcp): the kind list and its
exact-match outline, the field list with types for
`tupleElement(LW_COMPONENT('SysMem'), 'Tot`, the resolved tint on the caret's
token, the error tint on an off-caret `'Nonesuch'` with the gutter mark beside
it, and the FROM position answered from `system.tables`.

## References

- [sql-completion-exact-context-probes.md](../adr-background-work/sql-completion-exact-context-probes.md)
  — the probes this ADR leans on.
- [ADR-0147](./0147-sqleditor-widget-and-completion.md) — the widget, the
  seam, the key-capture mechanism; amended here.
- [sql-completion-survey.md](../explanation/sql-completion-survey.md) —
  ADR-0147's survey; its Tier B routes are decided here.
- [ADR-0189](./0189-component-sql-authoring-surface.md) — `LW_COMPONENT` and
  the registry the component-kind domain reads.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — the rosters as data and
  the panel substrate SD4 and SD8 build on; the `?`-never-`MISSING` rule SD9
  imports.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the driver the
  pane's verification uses.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — why the sentinel
  parse runs on a worker.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — the worker.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — the
  Projection whose type literal SD6 parses.
