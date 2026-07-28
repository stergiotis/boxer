---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Survey compiled 2026-07-28. Sources:
> the in-repo editor, nanopass and catalog code cited inline; the `egui` 0.35.0
> crate sources this repo builds against; `play.html` as served by ClickHouse
> 26.6.1 (read for the previous survey, re-checked here for a completion
> facility); and the public documentation of the antlr4-c3 and LSP ecosystems
> (see §9). This is a survey with a recommendation, not a design. It is the
> completion-shaped sibling of
> [the highlighting survey](./sql-editor-highlighting-survey.md), whose §6
> deferred completion with the note that "the in-process route (nanopass plus
> schema knowledge from keelson) starts ahead of any external server" — this
> document tests that claim.

# Context-sensitive completion for the SQL editor — a survey

The play app's SQL editor gained lexical and semantic color, an error
underline, a line-number gutter, statement tinting and run-under-cursor
([ADR-0130](../adr/0130-imzero2-textedit-highlight-seam.md) L1–L3). It has no
completion: no candidate list, no keyword help, no way to discover a table's
columns without leaving the buffer. This survey works out what completion would
cost, given what the repository already owns.

The short version: the analysis substrate is largely present and better than
what a generic SQL tool could bring — an exact-dialect lexer, a scope model
with alias and CTE resolution, a cached column probe, and a vocabulary
(leeway handles, keelson macros) that no external server knows about. The
genuine gaps are three, and only one of them is architectural: the editor
cannot see a keystroke it needs to steal.

## 1. Current state in this repository

### 1.1 What the editor is, and where it lives

There is no `sqleditor` package. play's editor is app-local, spread over
`apps/play`:

| File | What it owns |
| --- | --- |
| `play_renderer.go` (`sqlTextEditField` :1838, `gutteredEditor` :1978) | the `TextEdit` builder chain and the gutter/editor row |
| `play_editor_gutter.go` | line-number column, marks lane, monospace geometry |
| `play_editor_styled.go` | caret resolution, error-token span, overlay assembly |
| `play_statements.go` | `;`-splitting, caret→statement, run-buffer composition |
| `play_sql_highlight.go` | the L2 quiescence tier (400 ms) and its async runner |

A second, plainer SQL editing surface exists at
[`apps/sqlappletcreator/sqlappletcreator.go`](../../apps/sqlappletcreator/sqlappletcreator.go):99
— a bare `TextEdit().CodeEditor()` with none of the above. Every affordance
play's editor has, that surface lacks, and will keep lacking as long as the
code is play-private.

### 1.2 What already exists that completion needs

Much of the machinery is in place, built for other reasons:

- **Caret position, per frame.** `reportCursor` +
  `SendRespValCursor` land a packed char range that
  `play_editor_styled.go:77` converts to `inst.caretByte`. Completion's single
  hardest input is already crossing the FFI.
- **Insertion at the caret, replacing any selection.**
  `InsertAtCursor` ([ADR-0063](../adr/0063-imzero2-textedit-insert-at-cursor.md)),
  already consumed by the snippet library.
- **A per-keystroke token stream.** `HighlightLex`
  ([`dsl_highlight_lex.go`](../../public/db/clickhouse/dsl/nanopass/highlight/dsl_highlight_lex.go):15)
  runs every frame for color, and owns strings and comments — so a `;` or a
  `.` inside either never confuses a consumer.
- **A place to put expensive analysis.** `sqlSemanticHl` debounces the full
  parse behind 400 ms of quiescence and swaps the result in when it lands.
  The same shape serves scope resolution.
- **A cached column probe.** `chSchemaProvider`
  ([`play_schema_provider.go`](../../apps/play/play_schema_provider.go):33) asks
  `system.columns` per table, cached 5 minutes over at most 256 tables, and
  degrades to "not found" on any failure. One caveat carried forward: it runs
  **inline with a 5 s timeout**, which is acceptable on the execution path and
  is not acceptable on a frame.
- **A scope model.** `BuildScopes`
  ([`nanopass_scope.go`](../../public/db/clickhouse/dsl/nanopass/nanopass_scope.go):213)
  produces `SelectScope` → `TableSource` with aliases, CTE flags, subquery
  nesting and default-database resolution, documented in
  [SCOPE_RESOLUTION.md](../../public/db/clickhouse/dsl/nanopass/SCOPE_RESOLUTION.md).
  `analysis.ExtractTables` / `ExtractColumns` / `ExtractFunctions` sit beside it.

### 1.3 The one blocking property of the parser

`nanopass.Parse` discards its tree whenever the buffer has a syntax error
(`nanopass_parse.go:109-114`): ANTLR's default error strategy *does* build a
recovered tree, and the function returns `nil` for it. A buffer being typed
into is essentially never syntactically complete, so every parse-derived fact —
scopes, aliases, CTE names — is unavailable at exactly the moments completion
is wanted.

This is a property of the wrapper, not of ANTLR, and §3.2 covers the ways out.

## 2. What "context-sensitive" has to mean here

The feature decomposes into three problems that can be built and tested
independently. Conflating them is the main way this goes wrong.

1. **Context** — given a caret, what kind of thing may appear here? A table
   name, a column of a specific relation, a function, a setting, a param name,
   a type?
2. **Candidates** — for that kind, what are the names, and where do they come
   from?
3. **The seam** — how does a list get drawn, keyboard-driven, and committed
   back into a `TextEdit` that lives on the other side of an FFI?

Problems 1 and 2 are Go-side and testable without a UI. Problem 3 is the only
one that touches the IDL, Rust, and the generator.

## 3. Determining context at the caret

### 3.1 Tier A — lexical

Walk backwards from `caretByte` through the `HighlightLex` token stream:
previous significant token, bracket depth, enclosing clause keyword. The rules
are unglamorous and effective:

| Preceding context | Kind expected |
| --- | --- |
| `FROM`, `JOIN`, `INTO` | table, CTE name, table function |
| `SELECT`, `WHERE`, `ON`, `HAVING`, `GROUP BY`, `ORDER BY` | column, function, alias |
| `<ident>` `.` | columns of that alias/table, or tables of that database |
| `{` … , after `:` | param name; param type |
| `SETTINGS`, prelude `SET` | setting name |
| statement start | statement keywords |

Cost is near zero — the token stream is already computed per keystroke for
color — and correctness does not depend on the buffer parsing. This is the tier
that decides whether the feature *feels* present.

Two refinements are worth taking from `play.html`'s tokenizer, which solves
adjacent problems the same way: a one-token peek-ahead for `(` to separate
function names from identifiers, and treating the lexer's own string/comment
tracking as authoritative rather than re-scanning.

### 3.2 Tier B — scope-aware

Tier A cannot know that `o` in `FROM orders AS o JOIN users AS u` binds to
`orders`, nor that a bare column reference should be drawn from the union of
two specific tables rather than from every table in the database. `BuildScopes`
knows both. Reaching it requires getting a tree out of an incomplete buffer;
three routes, ascending in cost:

- **`ParseBestEffort`** — a sibling of `Parse` that returns
  `(tree, errs)` rather than dropping the tree. Mechanically small: the tree is
  already built and already thrown away. What is *not* known without a probe is
  how usable ANTLR's recovered tree is near the caret, which is precisely where
  recovery is worst. Leaves `Parse`'s contract untouched, which matters —
  every pass in the stack depends on "error implies no tree".
- **Buffer repair** — truncate at the caret, append a sentinel identifier and
  inferred closing brackets, parse that. Predictable and independent of ANTLR's
  recovery quality; the cost is a small heuristic layer with its own failure
  modes, and a tree whose offsets need mapping back.
- **Quiescence only** — compute scopes on the 400 ms debounce from whatever
  last parsed cleanly, and let Tier A cover the gap. Composes with either of
  the above rather than competing with them, and is the cheapest way to get
  *most* of Tier B's value: in practice a buffer is complete enough to parse
  far more often than one expects, because the incompleteness is usually the
  token being typed rather than the structure.

Note the ceiling: grammar1 parses a SELECT surface only, so Tier B is a
read-query feature. That is the right target for this editor.

### 3.3 Tier C — grammar-derived expected tokens

The principled answer is to ask the grammar. Walk grammar1's ATN from the
caret's token index and compute the exact set of tokens and rules that may
follow — the technique the antlr4-c3 "code completion core" implements. It
gives exact-dialect keyword completion at every position, for free, and tells
the caller which *rule* is expected (table identifier vs column identifier)
rather than inferring it from a keyword table.

The costs are real. No Go port of antlr4-c3 exists — the library ships for
TypeScript, Java, C++, Python and C# — so this is a from-scratch subsystem with
its own follow-set traversal, recursion guards and caching, against a grammar
whose DFA cache already needed
[ADR-0084](../adr/0084-nanopass-antlr-dfa-cache-bounding.md) to bound it. And
its main advantage over Tier A — exhaustive keyword correctness — is the part
of completion users notice least, while its main advantage over Tier B —
knowing which identifier rule applies — Tier B already supplies for the cases
that matter.

Recorded as a real option with a real payoff, and rejected for now on cost.
The trigger that would re-open it: grammar1 growing past the SELECT surface far
enough that hand-maintained clause tables in Tier A start disagreeing with it.

## 4. Candidate sources

Ordered by how much new plumbing each needs.

**Already available, no new I/O:**

- **Keywords** — grammar1's `LiteralNames` / `SymbolicNames` vocabulary
  (`clickhouse_lexer.out.go`). Exact dialect, no keyword table to maintain.
- **Param names and types** — `collectParamSlots` already returns
  `{Name, Type, Src}` per `{name:Type}` slot.
- **Snippets** — the help-corpus fenced blocks the Snippets tab already
  inserts at the caret (`play_snippets.go`) are completion items that need only
  a trigger.
- **CTE names and aliases** — from Tier B, once it exists.

**Available behind a probe that exists:**

- **Columns** — `chSchemaProvider.GetColumns`. Needs to be moved off the frame
  thread; the background-task primitive
  ([ADR-0038](../adr/0038-keelson-background-task-primitive.md)) is the
  existing answer, and `sqlSemanticHl`'s runner is the in-editor precedent.

**Available behind a probe that does not exist yet:**

- **Tables** — a `system.tables` probe, a near-copy of `fetchColumnNames`
  ([`play_client.go`](../../apps/play/play_client.go):400).
- **Functions** — a `system.functions` probe.
- **Settings** — a `system.settings` probe, for the `SET param_…` prelude and
  `SETTINGS` clauses.

**Available, and this is where boxer differs from a generic SQL tool:**

- **leeway friendly handles** ([ADR-0116](../adr/0116-play-leeway-column-handle-resolution.md))
  — arguably the *preferred* completion vocabulary, since the pass stack lowers
  them to physical names. `lwsql.Resolver` is lookup-only today (`Resolve`);
  enumeration is a new method over data the resolver already holds.
- **keelson macros** — the registered pre-execute passes
  ([ADR-0108](../adr/0108-keelson-sql-pass-registry.md)) expose vocabulary that
  the server has never heard of. An external language server structurally
  cannot offer these.
- **keelson introspection tables** — when a read routes to the introspection
  plane, the catalog is
  [`introspect/catalog.go`](../../public/keelson/runtime/introspect/catalog.go),
  not `system.*`.
- **Semantic-layer entries** ([ADR-0139](../adr/0139-semantic-layer-text2dsl.md)
  T1) — measures and dimensions carry name, description and a grammar-validated
  expression: the ideal completion item, with documentation attached. ADR-0139
  is proposed, so this is a later tier and not a dependency.

Two routing complications are structural rather than incidental. Endpoint
auto-routing ([ADR-0141](../adr/0141-play-endpoint-dispatch-seam.md)) means
"which catalog" is a per-buffer decision, not a constant; and ad-hoc datasets
([ADR-0134](../adr/0134-adhoc-datasets.md)) contribute tables that no
`system.tables` enumerates. A completion engine that assumes one catalog will
be wrong in both cases.

## 5. The editing seam

### 5.1 What egui 0.35 actually provides

Checked against the crate sources this repo builds against:

- **`TextEdit::return_key(None)`** (`builder.rs:405`) — Enter stops inserting a
  newline. A first-class builder method, not a workaround. The default is
  `Some(Enter)` (`builder.rs:152`).
- **`EventFilter`** (`data/input/event_filter.rs`) — `tab`,
  `horizontal_arrows`, `vertical_arrows`, `escape`. A multiline `TextEdit`
  defaults both arrow axes to `true`, and `code_editor()` sets `tab` via
  `lock_focus(true)`. **There is no public builder method to set the filter**;
  only `lock_focus` reaches one field of it. So Up/Down cannot be reclaimed
  through the builder.
- **`TextEdit::show()` → `TextEditOutput`** (`output.rs:6`) — carries `galley`,
  `galley_pos`, `text_clip_rect`, `state` and `cursor_range`. Combined with
  `Galley::pos_from_cursor` (`epaint` `text_layout_types.rs:1163`) this yields
  an exact caret rect. imzero2's TextEdit path uses `apply_widget`, which
  returns only a bare `Response`.
- **`Popup` / `PopupAnchor::Position(Pos2)`** (`containers/popup.rs:24`) —
  egui can anchor a popup at an arbitrary point, should an anchored list ever
  be wanted Rust-side.
- **No completion facility.** egui ships nothing here; neither does
  `play.html`, which has a highlighted editor and no completion at all. The
  nearest in-ecosystem reference remains `egui_code_editor`, whose completion
  is keyword-set-grade — useful as a parts reference, as the previous survey
  concluded, not as a dependency.

### 5.2 The three gaps, in order of how much they constrain the design

**Gap 1 — key capture.** `TextEditFluid` has no key channel, and the
fetcher route is wrong here for a structural reason: fetchers run in
`StateManager.Sync()` at frame end, so a `consume_key` there fires *after* the
`TextEdit` has already handled the keystroke. Enter would both accept a
completion and insert a newline. The IDL comment on `fetchF1KeyPressed`
(`egui2_definition_d_fetchers.go:378`) states the principle directly: consumed-
event ownership is explicit per binding, and F1 is a *global* affordance with no
competing consumer, which is exactly what completion keys are not.

The shape that fits the existing seam: a `TextEdit` builder method that runs
`consume_key` **before** `apply_widget` — the same point in `interpreter.rs`
where the highlight layouter is installed — and pushes the pressed set back
through a per-widget value channel, the way `SendRespValCursor` already does for
the caret. Go calls it only on frames where a popup is open, so arrow keys and
Tab behave normally the rest of the time. Enter can additionally use
`return_key(None)` while the popup is open, which is the more idiomatic switch
for that one key.

The one-frame lag is inherent and benign: on the frame a popup *opens*, its keys
are not yet captured. A keystroke is a per-frame event either way.

**Gap 2 — replacing the partial word.** `InsertAtCursor` inserts at the caret
*replacing any selection*. So a single new method — set the selection to a char
range — composes with what exists to give replace-range for free, with no new
insertion path and no second way to mutate the buffer. The alternative, a
whole-buffer rewrite Go-side, leaves egui's persisted cursor in the wrong place,
since no `setCursor` exists (only `CursorAtEnd`).

**Gap 3 — anchoring a popup at the caret.** No caret pixel rect crosses the
FFI, and `WindowFluid` offers `DefaultPos` but no `FixedPos`/`CurrentPos`, so a
window cannot follow the caret frame to frame. Two routes: report the caret rect
from Rust (requires the TextEdit path to use `.show(ui)` instead of
`apply_widget`), or place the list in a docked strip and not anchor it at all.

Computing the caret position Go-side is the tempting third option and it is a
trap — the gutter's own `monoAdvanceRatio` comment records why: the host may
leave the monospace face unconfigured, in which case `TextStyle::Monospace`
resolves to the proportional main font and per-glyph advances stop being
uniform. Row alignment survives that; column alignment does not.

## 6. Where the code should live

Completion pushes the editor past the size where "a few helpers in `apps/play`"
is the right home, and the second editing surface in `sqlappletcreator` has been
waiting for the first four affordances already.

A `widgets/sqleditor` package would own the buffer-shaped concerns: the
`TextEdit` chain, no-wrap layout, gutter and marks lane, styled overlays,
statement splitting, caret resolution, the highlight tiers, and the completion
engine's context and candidate machinery behind an injected provider interface.

The test for what moves is whether a thing is derivable from buffer-and-caret
alone. Run-under-cursor passes it: every function in `play_statements.go` is
already pure over `(sql, caret)` — the split, the caret-to-statement boundary
rule, the prelude-plus-statement composition — with only three thin methods
binding the app's buffer and holding a memo. So the derivation moves, and the
widget publishes the statement under the cursor as a result; what stays behind
is what play does *with* it (shipping it, the wire preview, the staleness
witness that gates auto-run, history snapshotting the full buffer). By the same
test, param slots and the prelude hide/mirror state machine, unfilled-input
gating, endpoint routing and the diagnostics banner stay — each needs play's
model, not merely play's screen.

The seam therefore runs in both directions: inwards a provider interface
("given a buffer and a caret, what may go here"), outwards a result carrying
buffer, caret, active-statement range and number, and the composed run buffer.
The inward half is what makes the completion engine unit-testable without a UI;
the outward half is what lets `sqlappletcreator` inherit run-under-cursor,
having shipped only whole buffers until now.

This is a refactor with a real risk profile: play's editor is load-bearing and
its id handling is subtle. It wants to land as a move-then-extend, not as a
rewrite — the extraction commit should be behaviour-identical and reviewable as
such, with completion added afterwards.

## 7. Comparison

| Approach | Dialect fidelity | Context quality | New IDL/Rust | External deps | Knows boxer vocabulary |
| --- | --- | --- | --- | --- | --- |
| Lexical tier only (Tier A) | exact (`grammar1` lexer) | clause-level | none | none | yes |
| Tier A + scope (Tier B) | exact | alias/CTE-resolved | none | none | yes |
| Tier C (ATN / antlr4-c3 port) | exact | rule-exact | none | none (new subsystem) | yes |
| `egui_code_editor` completion | keyword-set | none | vendor/adapt widget | small crate | no |
| Generic SQL language server (LSP) | generic SQL | server-dependent | LSP client | server binary | no |
| Server-side (`system.*` only, no analysis) | exact names | none | none | none | partial |

The LSP row is worth stating plainly because it is the industry default answer:
no ClickHouse-dialect-exact language server exists, and an external server would
know nothing about leeway handles, keelson macros, ad-hoc datasets, or the
introspection plane — the vocabulary most worth completing here. It also
re-introduces an external binary, against the same premise that ruled out nvim
in the highlighting survey.

## 8. Recommendation

A ladder, mirroring the highlighting survey's L0–L3, so each rung is
independently useful and independently abandonable.

- **C0 — a candidate list, no IDL change.** A completions strip below the
  editor showing candidates for the current caret context, click-to-insert
  through the existing insert-at-caret op. Ships on today's opcodes. No
  keyboard, no anchoring. Useful on its own as discovery ("what columns does
  this table have?"), and it exercises the context classifier and the async
  probes where they can be seen.
- **C1 — Tier A context and the catalog plumbing.** The lexical classifier, the
  `system.tables` / `system.functions` probes moved off the frame thread,
  prefix filtering and ranking. Still C0's UI.
- **C2 — the seam.** Key capture and select-range: type, arrow, Enter, Esc.
  This is the rung where it stops being a curiosity and starts being
  completion.
- **C3 — Tier B scope-awareness.** Best-effort parse on quiescence;
  alias- and CTE-resolved candidates; leeway handle enumeration.
- **C4 — anchored popup and item documentation.** Caret rect over the FFI, a
  floating list, per-item descriptions from the semantic layer once ADR-0139
  lands.

Taking C0 and C1 before C2 is deliberate: they validate the two things most
likely to be wrong — whether the lexical classifier picks the right context,
and whether the probes are fast enough to feel live — with no IDL churn and no
regen risk. C2 is an IDL + Rust + generator change and deserves the same
treatment ADR-0130 got.

The extraction into `widgets/sqleditor` precedes all of it, behaviour-identical,
so that `sqlappletcreator` inherits what already exists and every rung above
lands in one place rather than two.

## 9. Sources

- In-repo: [`apps/play`](../../apps/play) editor files as tabulated in §1.1;
  [`apps/sqlappletcreator/sqlappletcreator.go`](../../apps/sqlappletcreator/sqlappletcreator.go);
  [the nanopass SQL pipeline](../../public/db/clickhouse/dsl/nanopass) (`nanopass_parse.go`,
  `nanopass_scope.go`, `SCOPE_RESOLUTION.md`, `highlight/`, `analysis/`);
  [`widgets/codeview`](../../public/thestack/imzero2/egui2/widgets/codeview);
  [`lwsql`](../../public/semistructured/leeway/lwsql/lwsql.go);
  [`introspect/catalog.go`](../../public/keelson/runtime/introspect/catalog.go);
  the egui2 IDL under
  [`definition/`](../../public/thestack/imzero2/egui2/definition) and
  `rust/imzero2/src/imzero2/interpreter.rs`.
- `egui` 0.35.0 crate sources (the version in `rust/imzero2/Cargo.toml`):
  `widgets/text_edit/builder.rs`, `widgets/text_edit/output.rs`,
  `data/input/event_filter.rs`, `containers/popup.rs`; `epaint` 0.35.0
  `text/text_layout_types.rs`.
- `play.html` served by ClickHouse 26.6.1 — re-checked for a completion
  facility; it has none.
- [antlr4-c3](https://github.com/mike-lischke/antlr4-c3) — the code-completion
  core, for the Tier C technique and the language ports it does and does not
  ship.
- [egui_code_editor](https://github.com/p4ymak/egui_code_editor) — completion
  as a parts reference.
- Prior in-repo survey:
  [sql-editor-highlighting-survey.md](./sql-editor-highlighting-survey.md), and
  the ADRs cited inline.
