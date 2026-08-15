---
type: adr
status: proposed
date: 2026-08-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0187: `play` SQL-expression parameters — client-side substitution, categories, and a class ceiling

## Context

[ADR-0124](./0124-play-param-editing-widgets.md) gives every `{name : Type}`
placeholder an editing widget. Its value path (§SD4) is one mechanism from end
to end: a widget writes a draft, the orchestrator mirrors the draft into a
leading `SET param_<name> = …` prelude, and on Run the values ride the request
URL so **ClickHouse substitutes them server-side, as values**.

The request is a knob whose value is SQL — a `WHERE` predicate first, so that an
applet can parameterise a query with the full language rather than with the
scalars its author anticipated, and so that a panel can offer a filter control
instead of asking the reader to edit SQL.

What constrains the design:

- **ClickHouse substitutes values, and one name.** `{name:Identifier}` is the
  only syntactic parameter it offers; the grammar records exactly this where it
  admits a slot in table position (`grammar1/ClickHouseParserGrammar1.g4:267-272`).
  Nothing in the param channel substitutes an expression. An expression-valued
  knob is therefore not an extension of §SD4 — it is a second substitution
  mechanism that has to run client-side.
- **Detection already works, and needs no grammar change.** `columnTypeExpr`
  admits a bare `identifier` (`:181-187`), so a type name ClickHouse does not
  know still parses. Probed against the current parser: `{cond:Expr}` is found
  by `collectParamSlots` in `WHERE`, `GROUP BY`, `HAVING`, `ORDER BY` and inside
  a subquery; `{cols:ExprList}` is found in `SELECT` position; `{tbl:Identifier}`
  is found in `FROM`. A slot in a `SETTINGS` value does not parse — placement
  stays where the grammar admits it.
- **`param_` is a reserved shape.** `ExtractParams` harvests *every* prelude
  setting named `param_*` onto the URL. An expression carried under that name
  ships to the server as a string value.
- **The tier bit is prelude presence.** `paramPinned` (`play_param_render.go:69-77`)
  is one lookup into `paramSyncedValues`, populated from the harvested prelude.
  A knob with no `SET` reads as permanently live.
- **The class is decided before any rewrite.** `ClassifyQuerySecurity` runs over
  the authored buffer in play (`play_diagnostics.go:257`) and over a book's SQL
  fence at applet mint (`sqlapplet.go:390`). [ADR-0132](./0132-sqlapplet-sql-defined-applets.md)
  §SD5 gates applet `AutoRun` on the read class (`sqlapplet_embed.go:62`) and
  wire-enforces `readonly` for it.
- **`nanopass.Parse` has one entry rule** — `parser.QueryStmt()`
  (`nanopass_parse.go:124`) — and discards ANTLR's recovered tree on error
  ([ADR-0147](./0147-sqleditor-widget-and-completion.md) seam fact 4). There is
  no fragment grammar to validate an expression against.
- **The colouring is already public.** `codeview.BuildSqlLex` takes any string
  and `highlight.HighlightLex` is pure lex, so it answers on text that does not
  parse; `codeview.BuildStyledSections` carries an underline. `c.TextEdit`'s
  third argument selects egui's single-line form, which the Map's `table` field
  already uses.

The demand is not hypothetical, and it is not one shape. The Map panel
(ADR-0096) holds all three at once: a predicate slot that is plumbed and has no
user surface (`play_map.go:163`, ANDed with `in_view`); an aliased column list
the user types into a plain `TextEdit` today, unhighlighted and unvalidated
(`:437`, the Custom render's `red`/`green`/`blue`); and a table source behind
`sanitizeTable` (`:867`), whose comment states the security position that holds
in play and fails in an applet — *"the playground already grants arbitrary SQL
via the editor, so this is no new capability"*. ADR-0096 names its
`{table:Identifier}` slot as unbuilt and as "the forward path if a user-editable
raster query is ever wanted". `sqlapplet`'s `--launch` is already a SQL `WHERE`
over the manifest table.

## Design space (QOC)

**Question.** How does an expression-valued knob reach the executed query?

**Options.**

- **O1** — **Server-side param.** Carry it on the existing `param_*` channel and
  let ClickHouse substitute it.
- **O2** — **Client-side splice.** Substitute into the buffer text in `play`,
  ahead of the pass registry, and ship the substituted body.
- **O3** — **In-place edit.** The widget rewrites the slot's own source range,
  so the buffer holds the expression and no placeholder.
- **O4** — **Macro expansion.** The author writes a registered macro call and a
  pre-execute pass ([ADR-0108](./0108-keelson-sql-pass-registry.md)) expands it.

**Criteria.**

- **C1** — Reproducibility; assessed by whether the buffer still runs unaided
  when pasted into a ClickHouse client.
- **C2** — Class enforceability; assessed by whether the body that executes can
  be classified before it executes (ADR-0132 §SD5).
- **C3** — The knob survives its own use; assessed by whether the control is
  still there on the next frame after being filled.
- **C4** — Category coverage; assessed against the three shapes the repo already
  demands — predicate, aliased column list, table name.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | −  |
| C2 | ++ | ++ | ++ | −  |
| C3 | ++ | ++ | −− | +  |
| C4 | −− | ++ | +  | ++ |

O1 scores `−−` on C4 by construction: it serves exactly one of the three shapes.
That one it serves perfectly, which is why §SD2 keeps it rather than replacing
it. O3 scores `−−` on C3 for the reason it scores well elsewhere — substituting
into the slot's own range deletes the slot, so the control vanishes the moment it
is used. O4 fails C2 and C1 together: registry passes run at order ≥100,
downstream of where the class is decided today, and a macro call is text the
author types rather than a knob — no draft, no tier, no pane.

## Decision

We adopt **O2 for `Expr` and `ExprList`, and O1 for `Identifier`** — two
mechanisms split by category — and make the substituted body, not the authored
one, what gets classified.

### SD1 — The category is the type name; detection is already built

Three type names in the existing `columnTypeExpr` position:

| Slot | Category | Splice site |
| --- | --- | --- |
| `{c:Expr}` | one expression — a predicate or a scalar | any column-expression position |
| `{c:ExprList}` | aliased column-expression list | a `SELECT` / `WITH` list position |
| `{t:Identifier}` | a database, table or column name | table and database position |

No grammar change: `columnTypeExpr: identifier` already admits an unknown type
name, and §SD1 of ADR-0124 already finds the slot and records `Type` verbatim.
The names are chosen to read as types because that is the position they occupy;
`Identifier` is ClickHouse's own spelling and is not ours to rename.

Two properties fall out and are worth relying on. §SD2's catch-all totality means
an `Expr` slot in a `play` built before this decision renders a text field rather
than an error. And an `Expr` slot that ever reaches the wire unsubstituted fails
loudly on an unknown data type — the failure mode is a server error, never a
silently different query.

### SD2 — Two mechanisms, split by category, stated loudly

`Identifier` is a ClickHouse parameter. It rides §SD4 unchanged: a `SET
param_<name>` prelude, the URL channel, server-side substitution. Nothing in
this ADR's splice, validation or ceiling applies to it.

`Expr` and `ExprList` are substituted client-side and never touch `param_*`.

This is stated as its own seam because a reader meeting three type names in one
table will assume one rule governs them. The security consequence is the reason
it matters: an `Identifier` can name a different *table* but cannot introduce a
table *function*, so it widens access within an endpoint without adding egress —
a weaker escalation than a spliced expression, and one the §SD5 ceiling does not
need to police.

Adopting `Identifier` closes ADR-0096's unbuilt slot as a side effect.

### SD3 — The value path: a directive at the pinned tier, a signal at the live tier

ADR-0124's 2026-07-22 amendment made the pane tier-aware, deriving the mode from
`SET`-presence. An expression has no `SET`, so it needs a parallel derivation of
the same two tiers:

- **Pinned** — the buffer carries `-- play: expr <slot> = <text>`, in the
  directive vocabulary §SD6 established and the 2026-08-14 Update extended.
  Everything after the **first** `=` past the slot name is the value.
  Expressions contain `=` constantly, so first-separator-wins is a rule and not
  an implementation detail — it is also what makes the spacing around the
  separator free, so `cond=a=1` and `cond = a = 1` declare the same thing. The
  value is trimmed at both ends: trailing whitespace in a one-line SQL fragment
  carries nothing, and preserving it would make the drift comparison depend on
  characters nobody can see.
- **Live** — a panel writes the value as an ADR-0097 signal, name-keyed like any
  other. A Map lasso publishing a predicate is the shape this exists for; the
  buffer stays untouched.
- **Neither** — the slot is unfilled. It joins `unfilledSet()` and inherits the
  Run gate, the editor underline and the pane's "needs a value" mark that
  ADR-0124's 2026-07-25 Update wired. An empty value is unfilled: a slot's
  position is mandatory, and `WHERE ()` is not a query. A document that wants an
  optional filter ships `-- play: expr cond = true`, which is also its Reset
  default under the 2026-08-14 Update — no new mechanism.

Values are one line. This is not only the field's shape: directive carriage is
line-oriented, and a one-line value is what makes it work at all. An `ExprList`
is normalised to one line on capture, which costs nothing — SQL is
whitespace-insensitive and the field is single-line by construction.

Two things move in the existing code. `paramPinned` gains a second source, so it
stops being one map lookup; and `pinParamClaim` / `unpinParamClaim`
(`play_param_render.go:293`, `:334`) gain an arm that authors or removes a
directive line instead of a `SET`, against a drift baseline of their own. The
single-owner mirror is unharmed — the orchestrator is still the only writer to
the buffer — but this is the largest piece of work the carriage choice buys, and
it is where a mixed-tier bug would live.

Directive lines sit in the residual — **below** the `SET` prelude, never above
it. `SyncParamPrelude` rebuilds that block as `buildParamPrelude(…) + residual`
(`play_param_inject.go:30`), and its own contract already warns that intermixed
non-param `SET`s shift downward on rewrite; a directive line shifts the same way
unless its position is decided once, here, rather than discovered.

That decision turned out to be forced rather than stylistic, which the M1
implementation found. `env.harvestSetPrelude` consumes only a **leading** run of
`SET` lines, so a comment above them ends the prelude before it starts:
`BodyOffset` collapses to zero, the buffer reads as two statements, and a
run-under-cursor ships the body without its `SET param_*` lines — every
parameter the buffer plainly binds then reads as unfilled. This is a defect in
the shared prelude definition, not in this decision, and it predates it: any
comment above a prelude does it, including the `-- play: enum` and
`-- play: gloss` directives already in the vocabulary. It is recorded here
because it is what makes the placement a rule, and it is deliberately not fixed
here — `dsl/env`'s body offset is what every pass-recorded range is sliced
against, so widening it is its own decision.

### SD4 — The splice: one step, before the registry, on the trace

Substitution is a `play`-side step in the client-side rewrite, at order `-90` —
after `extract-params` (`-100`) and before the registry stage (≥100). It reports
through the same observer as every other step (`observeStep`,
`play_client.go:273`), so the Preview's "as sent" view accounts for it. That is
load-bearing rather than tidy: the Preview is documented as the same code path
that executes (`play_client.go:214-216`), and it is what replaces copy-paste
(see Consequences).

Per category:

- `Expr` is spliced **parenthesised**. `WHERE a = 1 AND {cond:Expr}` with
  `b = 2 OR c = 3` must become `… AND (b = 2 OR c = 3)`; unparenthesised it
  silently reassociates.
- `ExprList` is spliced **bare**. A list cannot be parenthesised without becoming
  a tuple.
- An unfilled slot is not spliced; the Run gate holds it.

### SD5 — The class ceiling: classify what executes, refuse a raise

`ClassifyQuerySecurity` is re-run on the **substituted** body, after §SD4's step
and before execute. The two hosts do different things with the answer, and the
difference is the whole of this seam:

- **In `play`, the class is reported.** `sanitizeTable`'s argument holds
  unmodified — the editor already grants arbitrary SQL, so an expression knob
  grants nothing new. What changes is that the Diagnostics badge and its
  witnesses describe the query that runs rather than a template nobody executes.
- **In an applet, the class is enforced.** An applet is `play` with the editor
  removed; its premise is that a committed, classified query is the whole
  surface. The mint-time class becomes a **ceiling**: a substituted body that
  classifies above it is refused before execute, with the witness that raised it
  shown. A spliced `url(…)` or `remoteSecure(…)` is egress; a spliced scalar
  subquery reads tables the applet never named.

This is cheap because the classifier already exists, is already conservative
("cannot prove → stronger class"), and already fails closed on its zero value.
The work is moving the call site, not writing an analysis.

### SD6 — Validation is splice-then-parse; there is no fragment grammar

The host buffer with the slot in place always parses — the slot is a grammar
production. So the thing worth validating is the substituted buffer, and
validating it needs no wrapper: splice, parse, and report.

This is why no fragment entry rule is added. A `SELECT 1 WHERE (<expr>)` wrapper
would validate a fragment in a context it will not execute in, and would need
its own offset rebasing on top.

Attribution rule: a parse error whose position falls **inside** the spliced range
underlines in the field and is the field's error; one outside is a query-level
error and is reported as one. ANTLR's recovery can report at a distance, so this
degrades to "the query does not parse" rather than to blaming the wrong text.

### SD7 — The field is a sibling widget, and it is useful without the parameter

A new single-line SQL field lives beside `Editor` in `widgets/sqleditor`, not as
a mode on it. A fragment has no statement split, no gutter, no prelude and no run
buffer, so most of `Editor`'s `Frame` and nearly all of its `Result` would be
inapplicable; sharing the package keeps the tones, and later the completion
catalog, in one place.

It is `c.TextEdit(…, false)` — egui's single-line form, so Enter inserts nothing
and needs no IDL change — with `HighlightJob(codeview.BuildSqlLex(v))` for
colour and `SectionStyled` for the error underline.

**It takes a row count, defaulting to one.** A fragment is a line, and §SD3's
directive carriage requires that a *parameter's* value be one; but a panel
control's value is panel state under no such constraint, and the Map's colour
block is three lines today. A field that could not be more than one line would
have left that call site on a raw `TextEdit` and made §M0 unsatisfiable as
written. The default carries the intent; the knob keeps the widget usable by the
consumer that motivated it.

**The widget does not depend on the parameter.** The Map owns a panel-local
template and splices its own controls into it (ADR-0096's 2026-07-10 divergence
Update), so it consumes the field directly and never touches a slot, a tier or a
directive. That is why §M0 ships the field alone and the Map adopts it before any
of the parameter machinery exists.

### SD8 — Deferred

- **Completion.** ADR-0147 defers scope-aware context (O2) with the trigger
  "M1–M4 shipped". An expression field is the strongest case for it, and it has a
  property the main editor lacks: the host buffer parses while the fragment is
  being typed, so scopes are available exactly where the main editor cannot have
  them. Recorded as an argument for revisiting that trigger, not as a decision to
  revisit it here.
- **Multi-line values.** Trigger: an `ExprList` that resists one-line
  normalisation in practice. The directive form would need a continuation rule.
- **Expression parameters in launch configs.** [ADR-0135](./0135-app-launch-requests.md)
  carries `Sql` outright, so nothing changes for `play`; for a frozen applet it
  is remote parameterisation with SQL, gated by §SD5 but worth deciding
  explicitly. Trigger: a second app wanting to drive an applet's filter.
- **A leeway-aware field.** Handle enumeration is ADR-0147 §SD9's separate
  reader; an expression over a leeway table would want it.
- **Per-slot refusal of the pane's control**, in the vocabulary ADR-0124 §SD8
  already anticipates.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `paramSlot` type vocabulary | added: `Expr`, `ExprList`, `Identifier` recognised as categories | nothing — grammar unchanged, verified by parse probe |
| `paramWidgetI` registry order | added entry ahead of the scalar tail | the registration slice in `play_renderer.go` |
| `paramPinned` tier predicate | reshaped — a second tier source beside `paramSyncedValues` | `pinParamClaim` / `unpinParamClaim`; a second drift baseline |
| `-- play:` directive vocabulary | added `expr` | a scanner beside `scanUngroupHint` / `scanEnumHints`; ADR-0124 §SD7's orphan advisory |
| `play` client-side rewrite steps | added `splice-expr` at order `-90` | `RewriteTrace`, the Preview "as sent" view |
| `ClassifyQuerySecurity` call site | added post-splice call; the mint-time class becomes a ceiling | `play_diagnostics.go`, the `sqlapplet` mount gate |
| `widgets/sqleditor` public API | added: a single-line field type | the Map panel's two raw `TextEdit` call sites |

## Alternatives

- **Extend `SET param_*` to carry expressions.** Rejected: `ExtractParams`
  harvests every `param_*` onto the URL, so the expression would ship as a string
  value and ClickHouse would reject it — and correctly, since it substitutes
  values.
- **Splice into the slot's own source range (O3).** Rejected: it deletes the
  slot, so the knob disappears the first time it is used.
- **A registered macro (O4).** Rejected: registry passes run downstream of where
  the class is decided, and a macro is text an author types rather than a control
  a reader operates — it would rebuild §SD4's draft and tier machinery inside the
  pass layer.
- **One `Expr` category for everything.** Rejected: it leaves two of the three
  shapes the Map already demands on raw text fields, and the splice rule differs
  per category (parenthesised versus bare) — one name would have to mean both.
- **Sanitise instead of classify.** Rejected: `sanitizeTable`'s blocklist stops
  statement-breakers, not egress. A blocklist over expressions would be a second,
  weaker security analysis competing with a classifier that already exists and is
  already conservative.
- **`Rows: 1` on the existing `Editor`.** Rejected: a fragment has no statement,
  no prelude and no run buffer, so most of the widget's contract would be
  inapplicable rather than merely unused.

## Consequences

### Positive

- An applet parameterises with the language instead of with the scalars its
  author anticipated, and stays classified while doing it.
- The Map's `where` acquires the control it has been plumbed for; its Custom
  colour field stops being unhighlighted, unvalidated raw text.
- A spliced `WHERE` predicate is an ordinary conjunct, so
  [ADR-0121](./0121-selection-condition-columns.md)'s `exposeConditions` gives it
  a `cond_N` column with no new mechanism — a panel filter becomes a selection
  column for free.
- The Diagnostics class stops describing a template nobody executes.
- `{table:Identifier}` — named unbuilt by ADR-0096 — arrives as a side effect,
  on the existing server-side path.

### Negative

- **Copy-paste reproducibility is lost for `Expr` / `ExprList`.** ADR-0124 §SD4's
  stated payoff is that a filled slot is buffer-owned and reproducible by paste;
  a client-side splice ends that. The buffer stays *parseable* — the directive is
  a comment — but not *runnable*, because the unsubstituted slot names a type
  ClickHouse does not know. The replacement is the Preview's "as sent" view,
  which is the same code path that executes, plus the applet Copy hatch. This is
  a real loss, mitigated rather than avoided.
- The subsystem now has two substitution mechanisms behind one pane. §SD2 states
  the split, but a reader who skips it will assume the ceiling covers
  `Identifier`.
- `paramPinned` stops being a single lookup, and the tier machinery grows a
  second baseline — the most likely home for a mixed-tier defect.
- The `-- play:` vocabulary grows a third directive, and each one adds surface
  that is only discoverable through prose.

### Neutral

- No grammar change and no wire-format change. The `param_*` URL channel,
  `encodeParamLiteral`'s buckets and the prelude are untouched for every existing
  slot.
- No IDL or FFI change: the single-line form, the highlight job and the styled
  overlay are all already exposed.
- ADR-0124's §SD2 chain absorbs this as a registration, exactly as it claims —
  no existing widget's `Matches` or `Render` moves.

## Migration — Tier 1

- **Breaks.** Nothing. The change is additive: `{x:Expr}` today renders a text
  field whose value ships to the server and errors there; afterwards it renders a
  field whose value is spliced. No committed buffer, book or snippet in the repo
  uses `Expr` or `ExprList` today.
- **Path.** Nothing to migrate. An author opting in adds a slot and a `-- play:
  expr` line; a document that does neither is unaffected.
- **Regeneration.** None. No IDL, no generated store, no golden regenerated by
  this decision on its own.
- **Old shape.** Kept indefinitely — §SD4's `param_*` path is unchanged and
  remains the mechanism for every value-typed slot and for `Identifier`.

## Verification plan — Tier 1

- **Lane.** Default `go test` for detection, the directive scanner, tier
  derivation, splice text and classification; the headless carrier lane
  ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)) for the field and the
  refusal message.
- **What would fail.**
  - A table test over `ClassifyQuerySecurity` on substituted bodies goes red if a
    splice carrying `url(…)`, `remoteSecure(…)` or a cross-table scalar subquery
    is admitted under a `read` ceiling. This is the assertion the decision exists
    for.
  - A test asserting `ExtractParams` never sees an expression-category name goes
    red if carriage regresses to a `param_`-shaped setting — the failure mode
    that would silently ship an expression as a string.
  - A splice test goes red if an `Expr` is spliced unparenthesised: `a OR b`
    into `x AND {c:Expr}` must not reassociate.
  - A tier test goes red if a directive-carried value reads as live, which is the
    pre-existing `paramPinned` behaviour this ADR changes.
- **Gap.** No live assertion that ClickHouse substitutes `Identifier` in every
  position the grammar admits; the grammar's own comment claims table and
  database position, and that is the claim adopted here. Worth a probe against
  the pinned server before §M4 rather than a permanent gap.

## Milestones

- **M0 — the field.** SQL field in `widgets/sqleditor`, single-line by default;
  the Map's two raw `TextEdit` call sites adopt it. No parameter machinery.
- **M1 — detection and the directive.** The three categories, the `-- play: expr`
  scanner, the orphan advisory, the widget registration. `Identifier` is
  complete here: being a ClickHouse parameter (§SD2) it only gains a better
  editor over the untouched §SD4 path. `Expr` / `ExprList` are declared and
  shown but not substituted, so M1 also owns the gates that keep their drafts
  out of the prelude and out of the signal store — a draft falling through to
  either would ship an expression as a string — and withholds the tier control
  until M3 gives pin/unpin a directive arm. One advisory line says the knob is
  not yet wired, which is what explains a run gate that will not open; M2
  retires it.
- **M2 — splice and trace.** The `splice-expr` step at order `-90`, per-category
  splice rules, the Preview trace entry, splice-then-parse validation.
- **M3 — tiers.** `paramPinned`'s second source, pin/unpin's directive arm, the
  second drift baseline, the live-tier signal path.
- **M4 — the ceiling.** Post-splice classification; `play` reports, applets
  refuse a raise with the witness; the `Identifier` probe.
- **M5 — docs.** `features.md` §Query parameters, a snippet, the ADR-0096 and
  ADR-0124 pointers.

## Status

Proposed 2026-08-15. Nothing implemented. The design dialogue settled three
questions — where a filled expression's text lives (§SD3), how many grammatical
categories v1 carries (§SD1), and what an applet may do with one (§SD5) — and
this record is the result. The Milestones order is deliberate: §M0 ships the
field alone, because §SD7's widget earns its keep in the Map before any of the
parameter machinery exists, and descoping there is cheaper than descoping later.

One claim is unverified and marked as such in the Verification plan: that
ClickHouse substitutes `Identifier` in every position the grammar admits. The
grammar's own comment claims table and database position only, and §SD2 adopts
that narrower claim rather than the broader one.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0124](./0124-play-param-editing-widgets.md) — the param pane, the widget
  chain, the tier amendment and the directive vocabulary this extends.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) §SD5 — the query security
  class, `AutoRun` gating and `readonly` enforcement that §SD5 turns into a
  ceiling.
- [ADR-0147](./0147-sqleditor-widget-and-completion.md) — the `sqleditor`
  package §SD7 extends, and the deferred scope-aware completion §SD8 argues for.
- [ADR-0096](./0096-play-geo-raster-map-panel.md) — the Map panel: the plumbed
  `where`, the raw colour field, and the unbuilt `{table:Identifier}` slot.
- [ADR-0097](./0097-play-reactive-query-graph.md) — signals, which carry the
  live tier in §SD3.
- [ADR-0121](./0121-selection-condition-columns.md) — `exposeConditions`, which
  a spliced predicate composes with.
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — the pass registry the splice
  runs ahead of, and the macro route rejected as O4.
- [ADR-0130](./0130-imzero2-textedit-highlight-seam.md) — the styled-section
  channel the field's error underline uses.
