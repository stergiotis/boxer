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

[ADR-0147](./0147-sqleditor-widget-and-completion.md) extracted the SQL editor
and decided the completion engine's shape: a lexical context tier (§SD5),
catalog probes off the frame thread (§SD6–SD9), and a keyboard-driven popup fed
by a `TextEdit` key-capture method (§SD10–SD12). Its M0 shipped; its M1 —
context and candidates — did not, and scope-aware context was deferred with a
trigger. Two things moved since.

**Boxer's own vocabulary is completed at argument positions.**
[ADR-0189](./0189-component-sql-authoring-surface.md) made this sharp: a
component read is `tupleElement(LW_COMPONENT('SysMem'), 'TotalBytes')`, and both
literals are known in-process — the kind from `componentsql.Default`, the field
from the kind's Projection type. `keelson('<table>')`,
`LW_GET(x, '<section>', 'chan:…')`, `LW_TAGGED(e, '<section>', …)` and the gloss
functions have the same shape. What may go there depends on the enclosing call,
the argument's ordinal and — for the field — the *type* of a sibling argument. A
clause classifier cannot say, and ADR-0147 has no tier that can.

**The requirement is precision.** A list that is right, not one that is long.
That is reachable where a source of truth is in-process (registries, the
grammar, the parsed statement) or the server answers a question (`system.*`,
`DESCRIBE`) — and nowhere else, so the design commits to silence over guessing.
Precision also changes what the UI is for: showing that what the user typed
*resolves* matters as much as helping them type it.

Four facts from the
[probes](../adr-background-work/sql-completion-exact-context-probes.md) the
design leans on:

- ClickHouse accepts `expr.name` on a named tuple; grammar1 has only `expr.N`,
  which is already canonicalised to `tupleElement(x, 1)`. The dot form is a gap.
- The lexer turns an unterminated literal into a `CatPlain` quote gap plus
  ordinary tokens, and a member-access receiver sits before the `.` as an
  identifier or a `)` — both recoverable by the paren-balancing walk
  `enclosingCallees` already does.
- A sentinel parse — the partial token replaced by a sentinel, the open brackets
  closed — parses every driving case with the exact callee, ordinal, aliases,
  CTEs and clause: 55–600 µs warm, 5–18 ms while the DFA warms.
- `system.functions` describes arguments in prose. Six roster packages declare
  their own `Function` around the same `Params []string`; none says what an
  argument *is*.

## Design space (QOC)

**Question 1 — How does the engine learn what surrounds the caret?**
O1 a **lexical site** (extend `enclosingCallees` into a frame walk); O2 a
**sentinel parse** (deterministic repair, then `nanopass.Parse`); O3
**`ParseBestEffort`** (ANTLR's error-recovered tree); O4 **grammar ATN follow
sets** (antlr4-c3). Criteria: C1 answers while typing, C2 exact callee and
ordinal, C3 alias/CTE/FROM knowledge, C4 cost per keystroke.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | +  | +  | ++ |
| C2 | ++ | ++ | −  | ++ |
| C3 | −  | ++ | +  | −  |
| C4 | ++ | +  | +  | −− |

**O1 and O2 together** — they are one model with two producers: O1 answers every
frame, O2 upgrades it a beat later. O3 is killed on C2: recovery is least
reliable at the caret, the only place the answer is read. O4 stands rejected as
in ADR-0147.

**Question 2 — Where does an argument position's domain come from?**
D1 declared beside the roster on a shared `Param{Name, Domain}`; D2 a central
signature table in the engine; D3 parsed out of the `Params` display strings;
D4 the server's `system.functions.arguments`.

**D1 is taken.** D3 and D4 are prose, not machine-readable. D2 drift-proofs by
test what D1 drift-proofs by type, one package further from the function it
describes. The few ClickHouse built-ins with a closed literal domain
(`tupleElement`, `CAST`, time zones, `dictGet`) get a curated table in the
engine, because no roster exists for them.

**Question 3 — How do candidates reach the user?**
P1 a **popup with key capture** (ADR-0147 §SD10–SD12); P2 a **completion pane**
with two-way match highlighting; P3 a **click-only strip** (ADR-0147 M3).
Criteria: E1 no IDL or FFI change, E2 shows context, E3 verifies what was typed,
E4 keystrokes to a long name.

|    | P1 | P2 | P3 |
|----|----|----|----|
| E1 | −− | ++ | ++ |
| E2 | −  | ++ | −  |
| E3 | −  | ++ | −  |
| E4 | ++ | +  | +  |

**P2 is taken.** P1 is not rejected, only reduced: once the pane is the list,
keyboard acceptance is one captured key. P3 is P2 with one column and no
highlight.

## Decision

ADR-0147's M1 is built as an **exact caret model** — a lexical site per frame,
upgraded by a sentinel parse on quiescence — with **typed argument domains**
declared per parameter beside each vocabulary roster, and candidates presented
in a **completion pane** whose match state renders in both the pane and the
editor. Keyboard acceptance is one captured key. This amends ADR-0147 §SD5 and
§SD10–SD12, discharges its O2 deferral, and leaves its §SD6–SD9 standing.

### The contract

- **SD1 — Precision over recall.** A candidate is offered only from a source of
  truth: a registry, the grammar's vocabulary, the parsed statement's own names,
  or a catalog answer for the buffer's endpoint. Where none exists the engine
  offers nothing *and says why* — every empty result carries a reason, because a
  silence without one is indistinguishable from a bug. Every candidate carries
  its provenance and its replace range. No "plausible" band below an "exact" one.

### The caret model

- **SD2 — The lexical site lives in `highlight`, beside `EntityAt`.**
  `highlight.SiteAt(spans, off)` answers every frame, over the span stream
  alone, on a buffer that does not parse. It reports the frame stack (callee,
  ordinal, the sibling arguments' ranges on both sides of the caret), the string
  literal the caret is inside, the member-access receiver when the previous
  significant token is `.`, the token completion replaces, and the brackets left
  open. Three details are load-bearing. A literal's prefix is the raw text from
  the quote to the caret — text, not tokens, since a comma inside it is not
  structure. The literal may be *terminated* as well as open, because a caret
  moved back into a complete literal is the state §SD9 tints, so the site
  reports the typed prefix and the token's whole text separately. And a frame's
  callee is any nameable token butted against the `(`, not only one the lexer
  marked a function name: `CAST` and `EXTRACT` lex theirs as keywords, and they
  are exactly the frames whose ordinal rule differs. ADR-0147's
  caret entity is one field of it, and the widget publishes it in `Result`.
- **SD3 — The scope is a sentinel parse, on a worker.** `sqlcomplete.Scope`
  comes from `nanopass.Parse` of the caret's statement after a deterministic
  repair: the token being completed replaced by a sentinel (inside the literal
  if there is one, which is then closed) and the site's open brackets closed.
  Two attempts — tail kept, then tail cut — and the first that parses wins; if
  neither does, the site alone is the model. The tree gives the alias→expression
  map, CTE names, FROM sources, the clause, and the one thing the site refuses
  to guess: for a keyword-syntax call (`CAST(x AS T)`, `EXTRACT(HOUR FROM ts)`)
  there is no comma to count, so the tree's ordinal is the only one. It runs on
  a `bgjob.Runner` keyed by **(statement, caret)** — moving the caret changes
  which frame the answer is about — with a shorter quiescence than the semantic
  tier's. Prefix filtering stays per frame, so in-process answers are live per
  keystroke and only scope-dependent ones wait.

### Domains and types

- **SD4 — Domains are declared beside the roster.** A leaf `dsl/sqlvocab` owns
  `Function{Name, Params []Param, Doc, Where, Family}` and `Param{Name, Domain}`.
  A `Domain` is data: a kind, plus the ordinal of the sibling it reads where the
  kind needs one. The kinds are an enumeration in `sqlvocab` — §SD12's tables
  say what each one feeds and what answers it. The six rosters adopt
  `[]sqlvocab.Param`; the zero `Domain` is
  *unspecified* and registration refuses it, so a roster cannot grow a function
  that says nothing about its arguments.

  Three shapes the vocabulary forced. `Where` is a **bitset** (server / client /
  host), because `LW_ID_*` is genuinely both a server UDF and a client macro and
  reporting one would make the other answer wrong. A **repeating tail** is read
  off the parameter's own display name, which the rosters already spell with an
  ellipsis — that is what completes `LW_TAGGED`'s sixth argument while leaving
  `LW_MEMBERSHIP`, whose arity is exactly three, alone. And the leeway
  extraction family's trailing `col:` / `chan:` / `param:` tokens are **one**
  domain rather than three, because the surface takes them in any order: the
  ordinal does not say which one a position holds, only the prefix typed there
  does, and per-ordinal domains would offer channels where a column belongs.

  **The union of the rosters is one host-wired registry**, `sqlvocab.Default`,
  populated where passes and components are registered. The vocabulary panel's
  `vocabDeclared()` and the engine both read it, so a roster cannot reach one
  surface and not the other. Resolvers are wired by the host, per buffer,
  through ADR-0147 §SD7's provider.
- **SD5 — An in-process typer answers type-dependent domains.**
  `sqlcomplete.Typer` maps an expression to a ClickHouse type when it can, off
  the **lex tier** rather than off a CST: the shapes are syntactically shallow
  and the parser costs 5–18 ms while its DFA warms, which is not a per-frame
  budget. The closed list is a literal; `LW_COMPONENT('K')` via a host-wired
  hook; the three CAST spellings; `tuple(…)`; `tupleElement`, `.N` and `.name`;
  an alias via the scope's map; a column via the typed column probe. Anything else is *unknown*, and unknown yields nothing (SD1). Type
  strings — the Projection literal, `system.columns`, `DESCRIBE` — are parsed by
  a leaf `chtype` package covering the compound types.
- **SD6 — The Projection's elements are parsed, not emitted twice.**
  `componentsql.Artefacts.Elements()` parses the Projection's own CAST type
  literal via `chtype`, pinned by a test beside the generator that emits it. No
  generated-store change: the string is what the server sees, and the parser is
  needed for the column and `DESCRIBE` cases regardless.
- **SD7 — Member access is one shape with several receivers.** `X.` completes
  the members of X: an alias of a tuple-typed expression → its elements; a table
  or table alias → its columns; a database → its tables; a call or parenthesised
  receiver (`LW_COMPONENT('SysMem').`) → the type SD5 works out, offered only
  because SD11 made that spelling executable.

### The pane

- **SD8 — The completion pane is a table with a match state, not a popup.** It
  shows the domain at the caret with its context — name, type or kind,
  provenance, one line of doc. A domain of up to 64 candidates shows **whole**
  with the matching rows marked, because the pane's job is to say what the
  argument *is*, not only what extends what has been typed; a larger one
  (columns, functions, 597 time zones) filters to the matches. The number is set
  by what the domains are: every closed in-process one fits under it, and the
  ones that do not are the server's, which are exactly the ones a reader narrows
  by typing. The match state is computed per frame
  from the partial token, case-sensitively: *none*, *prefix* (the rows that
  extend it), *exact* (the row that equals it). The pane takes no input focus
  and captures no key, so its rows are ordinary widgets a headless driver can
  assert. It is built on the Vocabulary tab's substrate (ADR-0174), not beside
  it: the same table widget, the same cell tones, the same declared-function
  registry, and one `system.functions` lane widened to every origin which the
  tab filters to `origin != 'System'`. Function rows carry the tab's ✓ /
  MISSING / dependency marks — offered *with* the mark, never hidden, since
  hiding a MISSING function would hide the provisioning fact the tab exists to
  show. The tab is the browse-and-reconcile view; the pane is the glance at the
  caret. play docks it beside the Docs pane; `sqleditor` offers the table as a
  sub-widget for an embedder without a dock.
- **SD9 — The same match state tints the editor.** The token under the caret
  gets an exported `ToneResolved` on an exact match and nothing otherwise —
  never an error tone while it is being typed. Once the buffer settles, every
  *other* literal sitting at a position whose domain this build can enumerate
  **without asking an endpoint** is validated: resolved gets the quiet tone,
  unresolved gets `ToneError` — ADR-0189 §SD5's refusal, shown while typing. A
  domain that needs a probe produces no tint at all, because a literal marked
  wrong for a listing that has not landed is a false accusation (ADR-0174's
  `?`-never-`MISSING` rule). play composes these into `Decoration.Styled` as it
  does the subquery mark.
- **SD10 — Completing is a suffix insert, and Tab is one key.** Clicking a
  *prefix* or *exact* row inserts the candidate minus the typed prefix through
  `InsertAtCursor`, valid because the caret is at the partial's end — no
  replace-range, no IDL. Rows outside the match state are reference only. Tab is
  shell-style: the unique match's suffix, or the longest common prefix of
  several, and nothing when they agree on no more. The editor asks for the key
  only on frames where it would insert something, so Tab means a tab character
  the rest of the time.

### The companion grammar change

- **SD11 — grammar1 gains `columnExpr DOT identifier`, canonicalised.** The
  target is `tupleElement(expr, 'name')`: one alternative beside
  `ColumnExprTupleAccess`, one rule beside `tupleAccessToFunctionRule` in
  `CanonicalizeConstructors`. It has to be a separate alternative rather than a
  widening of the positional one, because `columnIdentifier` stays greedy for
  `a.b` and `a.b.c` — so the new alternative fires only after a primary that is
  not an identifier, and a golden pins both halves.

### The providers

- **SD12 — Providers in scope.** Every candidate traces to one provider; where
  the truth lives fixes exactness, cost and endpoint dependence. **A** is
  in-process and closed — exact, no I/O, live per keystroke. **B** is a server
  catalog — exact per endpoint, fetched by an ADR-0147 §SD6 probe (off the frame
  thread, cached, "not yet" until it answers) and routed per buffer. **C** is
  excluded by SD1. `▏` marks the caret.

  Wired:

  | # | Provider | Feeds | Source of truth |
  |---|---|---|---|
  | A1 | Component kinds | `LW_COMPONENT('▏')`, `LW_COMPONENT_FILTER('▏')` | `componentsql.Default.Kinds()` |
  | A2 | Component fields, typed | `tupleElement(LW_COMPONENT(k),'▏')`, `m.▏`, `LW_COMPONENT(k).▏` | the Projection's type literal (SD6), through the typer |
  | A3 | Introspection tables | `keelson('▏')` | the `introspect` catalog |
  | A4 | Ad-hoc dataset aliases | `keelson('▏')` (ADR-0134 §SD4) | play's `BindDataset` bindings, per buffer |
  | A5 | Function rosters — names, params, docs | expression positions; the doc column | `sqlvocab.Default` (SD4) |
  | A7 | Aspect vocabularies | `LW_TAGGED(…,'enc:▏')`, `LW_PLAIN(…,'item:▏')` | the three aspect enums |
  | A9 | Channels and support roles | `LW_MEMBERSHIP(…,'▏')`, `LW_SUPPORT(…,'▏')`, `'chan:▏'` | `common.AllMembershipSpecs` / `AllColumnRoles` |
  | A10 | Gloss catalog | `gloss(expr,'gloss/▏','key▏',…)` | `gloss.Default()` names and parameter keys |
  | A13 | The statement's own names | aliases (`m.▏`), tables, CTEs | the sentinel parse (SD3) |
  | B1 | Databases | `db.▏` | `system.databases` |
  | B2 | Tables and views | `FROM ▏`, `FROM db.▏` | `system.tables` |
  | B3 | Columns with types and comments | `SELECT ▏`, `WHERE ▏`, `t.▏`; the typer's input | `system.columns`, typed, on its own lane |
  | B5 | Functions | expression positions | `system.functions`, the Vocabulary tab's lane widened |
  | B6 | Settings | `SETTINGS ▏`, `SET ▏` | `system.settings` |
  | B7 | Data types | `CAST(x,'▏')` | `system.data_type_families` |
  | B8 | Time zones | `toDateTime(x,'▏')` | `system.time_zones` |
  | B9 | Dictionaries | `dictGet('▏',…)` | `system.dictionaries` |
  | B10 | Formats | `FORMAT ▏` | `system.formats` |

  Declared as a domain, no resolver yet — the position resolves and the pane
  names what is missing:

  | # | Provider | Blocked on |
  |---|---|---|
  | A6 | Membership names and ids | a membership-registry reader |
  | A8 | Canonical types | an enumeration of the `canonicaltypes` abbreviations |
  | A11 | Identity tags | a reader over `identity/tagmint` |
  | A14 | Param slots and expression params | wiring `collectParamSlots` (ADR-0187) into the provider set |
  | B4 | Leeway sections and their columns | ADR-0147 §SD9's physical-schema reader |
  | B11 | Enum values | routing B3's type string through `chtype.EnumValues` |
  | B13 | Expression types the typer cannot reach | the server oracle (M4, still gated) |

  Out of scope for this ADR: grammar keywords per clause (A12), snippets (A15),
  run history (A16), the semantic layer (A17, ADR-0139), distinct column values
  (B14 — a real query, so on explicit request only), clusters and macros (B15),
  and buffer words (C1, excluded by SD1: admitted only when they resolve through
  A or B). Recency and frequency (C2) are a ranking signal, not a provider.

  A5 is the spine: every argument domain in A6–A11 is a `Param.Domain` on a
  roster function, and B7–B9's closed literal domains hang off the curated
  built-in table (SD4). Two things cut across the table: the introspection plane
  answers B-questions from the `introspect` catalog rather than `system.*`, and
  ad-hoc datasets contribute tables no `system.tables` enumerates — which is why
  the catalog resolves per buffer.

### Milestones

- **M0 — Leaves.** ✓ `chtype`; `sqlvocab` and the six roster adoptions;
  `componentsql.Artefacts.Elements()`.
- **M1 — Site, domains, typer, pane.** ✓ `highlight.SiteAt`; `sqlcomplete` over
  the in-process sources; the pane, its `completion` tab and the caret tint.
- **M2 — Scope and validation.** ✓ The sentinel parse on its worker; the
  catalogue probes; off-caret validation.
- **M3 — SD11.** ✓ Grammar, regen, canonicalise rule, goldens; the typer's
  `.name` case; the call receiver switched on.
- **M4 — Server type oracle.** `DESCRIBE (SELECT <expr> …)` after the
  pre-execute passes, async and cached, for expressions SD5 cannot type.
  **Not built. Trigger:** the first request for member completion on a built-in
  function's result or a Nested column.
- **M5 — Tab.** ✓ ADR-0147 §SD10's key-capture method, for one key.
- **M6 — Record and close.** ✓ ADR-0147's dated Update; the help corpus's
  Completion section; the verification lanes below.

## Surfaces — Tier 1

| Surface | Change | Moved with it |
| --- | --- | --- |
| `chtype` (exported Go API) | new — ClickHouse type-string parser (SD5) | `componentsql`, `sqlcomplete` |
| `dsl/sqlvocab` (exported Go API) | new — `Function`, `Param`, `Domain` (SD4) | six roster packages; play's vocabulary model |
| `dsl/sqlcomplete` (exported Go API) | new — engine, `Scope`, `Typer`, providers, match state | `widgets/sqleditor`; the pane |
| `glosssql`/`identsql`/`chpack`/`readback`/`lwsqlsurface`/`constructsql` `Function` | reshaped — `Params []string` → `[]sqlvocab.Param` | `play_vocab_model.go`; roster tests |
| `highlight` | added — `SiteAt`, `CaretSite`, `Range` (SD2) | `sqleditor.Result` |
| `componentsql.Artefacts` | added — `Elements()`, `ProjectionType()` (SD6) | nothing; additive |
| `sqleditor` | added — `ToneResolved`; `Result.Site` / `.Scope` / `.TabPressed`; the `Pane` sub-widget | play, `sqlappletcreator` |
| play's vocabulary model and probe (ADR-0174) | reshaped — `vocabDeclared()` reads `sqlvocab.Default`; one lane covers every origin | the Vocabulary tab; the pane |
| grammar1 `ClickHouseParserGrammar1.g4` (generated-code input) | added — `ColumnExprTupleAccessNamed` (SD11) | regenerated parser/visitors; `CanonicalizeConstructors`; grammar goldens |
| egui2 IDL | added — `TextEdit.captureTab` (SD10, on ADR-0147 §SD10's mechanism) | Rust interpreter, Go bindings, both sides rebuilt |
| `passes.SchemaProviderI` | **unchanged** — the pane's typed columns come from a lane of its own, so the synchronous pass path needed no sibling | nothing |
| The `LW_` namespace, facts schema, DDL, wire formats | **unchanged** | nothing |

## Alternatives

- **`ParseBestEffort` for scope (O3).** Killed: recovery is least reliable at
  the caret, the only place the answer is read.
- **Heuristic buffer repair.** Killed: the site already knows which brackets and
  which literal are open, so the repair is deterministic.
- **A central signature table (D2).** Killed on locality: the author of a roster
  function should declare its argument domains where they declare it.
- **Emitting `Slots` from `recordstore/gen`.** Killed: a regen and more bytes on
  the store — ADR-0189 measured +55 % — for a fact the type literal carries.
- **Server as the only type oracle.** Killed: silent about `LW_COMPONENT` on any
  endpoint without the read-back helpers, and a round trip where a registry
  answers in-process. Kept as the fallback rung (M4).
- **A `tupleElement`+`LW_COMPONENT` special case instead of a typer.** Killed:
  it answers one spelling and nothing for `AS m` / `m.field` /
  `tupleElement(col, …)`.
- **Popup first (P1).** Killed on E1–E3: an IDL, Rust and generator change to
  deliver a narrower view of the same candidates, with no verification of what
  was typed. Its one key survives as SD10's Tab.
- **A new per-widget value channel for the captured key.** Killed once ADR-0177
  shipped its key-capture register, which already carries a code and its
  modifiers, already drains at frame end and already re-groups by widget id.
- **Two-band candidate lists.** Killed by SD1.
- **A typed sibling on `passes.SchemaProviderI`.** Killed on absence of a
  consumer: the pane reads its own probe lane and the passes keep their
  synchronous path.
- **Folding the pane into the Vocabulary tab as a "fits here" mode.** Deferred,
  not rejected: one table, but it turns an accepted provisioning report —
  sectioned by where a function *runs*, which means nothing for a kind, a field
  or a column — into the completion surface. **Trigger:** the two surfaces found
  open side by side showing the same function rows.
- **Leaving grammar1 without the dot form.** Rejected as a default: the server
  accepts the spelling and the canonical form exists for the positional case.

## Consequences

### Positive

- Boxer's own vocabulary is completed exactly, from the registries that define
  it — kinds, fields, tables, sections — with no server round trip.
- Domains live beside the functions they describe; a roster cannot grow a
  function that says nothing about its arguments.
- The editor validates closed-domain literals while typing, so a wrong kind or
  field is visible before the run refuses it.
- ADR-0147's O2 deferral is discharged with a deterministic repair.

### Negative

- Six roster types changed shape; mechanical, but wide.
- Two leaf packages and one engine package to own; the built-in signature table
  is hand-curated and lags ClickHouse's function set by design.
- The typer is a closed list; outside it the pane is silent until M4.
- Seven domains resolve to a position with no resolver behind them (SD12's
  second table). Each is a sentence in the pane rather than a candidate.
- A pane needs screen space adjacent to the editor; hidden, there is no
  completion at all.
- A grammar1 change is a regen with a hand-provisioned ANTLR jar
  (`grammar0/generate.sh`) and touches every visitor.

### Neutral

- Scope-dependent candidates are one quiescence window behind the buffer, as
  ADR-0147 §SD6 accepts; in-process ones are not.
- SD11 changes what the pipeline accepts, not what any query means.

## Migration — Tier 1

- **Broke.** In-repo readers of the rosters' `Params []string` — the vocabulary
  model and the roster tests — until they read `Param.Name`. Nothing outside the
  repository consumes them; `Params []string` is gone, and nothing else had one.
- **Regeneration.** grammar1 (SD11), Go side only — grammar0 and grammar2 came
  back unchanged. The egui2 IDL regen (SD10) rebuilt both sides.

## Verification — Tier 1

- **Default `go test`** covers the site walk (every probes-page row plus nested
  frames, a `)` before a `.`, a comma inside an open literal, keyword-syntax
  calls), the repair and its two attempts, the typer's closed list and its
  unknowns, `chtype` against a captured 109-type `system.columns` corpus and
  every registered kind's Projection literal, the roster domain test, the match
  state, the Tab rule, and the grammar goldens on both halves of `a.b` versus
  `f(x).n`.
- **The `//go:build integration` lane** is the precision oracle: every candidate
  the engine offers, spliced back into the buffer it was offered for and shipped
  through `BuildStatement`, must analyse against the live endpoint (`LIMIT 0`);
  the offered field set must *equal* `Elements(k)` for every registered kind —
  the "no less" half a spliced-execution check cannot see on its own; the dot
  form must run wherever the canonical form does; and each catalogue probe must
  come back with rows, so a `system.*` query whose columns changed surfaces here
  rather than as a pane that quietly says "waiting".
- **The ADR-0154 driver** (`scripts/dev/completion-pane-scene.sh`) renders three
  scenes headless: the kind domain with its heading, rows and provenance column;
  the field domain decided by the sibling argument, with element types; and a
  position no provider answers, showing its reason rather than an empty table.
- **Gap.** The driver cannot see the highlight — the match outline is a Frame
  stroke and the editor tint a styled section, neither of which enters the
  accessibility tree — so both are pinned in Go instead, and the two-way
  behaviour was confirmed once by hand through egui-mcp. M4 is unbuilt, so
  built-in result types are verified nowhere. An unanswered probe's *absence* of
  tint is asserted; its later arrival is not.

## Status

Proposed — awaiting review by the code owner. **M0–M3, M5 and M6 are built**;
M4 stands behind its trigger. The body above describes the design as built: it
was edited in place where the implementation overturned it, which is what the
proposed stage permits (DOCUMENTATION_STANDARD §1's Tier-1 policy), so a
reviewer reads what exists rather than what was first written.

ADR-0147 keeps authority over the widget, the probes and the key-capture
mechanism, and carries a dated Update recording that this ADR replaces its §SD5
and M1, reduces §SD10–SD12 and M4 to one key, and discharges its O2 deferral.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [sql-completion-exact-context-probes.md](../adr-background-work/sql-completion-exact-context-probes.md)
  — the probes this ADR leans on.
- [ADR-0147](./0147-sqleditor-widget-and-completion.md) — the widget, the seam,
  the key-capture mechanism; amended here.
- [sql-completion-survey.md](../explanation/sql-completion-survey.md) —
  ADR-0147's survey; its Tier B routes are decided here.
- [ADR-0189](./0189-component-sql-authoring-surface.md) — `LW_COMPONENT` and the
  registry the component-kind domain reads.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — the rosters as data and the
  panel substrate SD4 and SD8 build on; the `?`-never-`MISSING` rule SD9 imports.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the key-capture
  register SD10's Tab reports through.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — the driver the pane's
  scene runs on.
- [ADR-0084](./0084-nanopass-antlr-dfa-cache-bounding.md) — why the sentinel
  parse runs on a worker.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — the worker.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — the
  Projection whose type literal SD6 parses.
