---
type: adr
status: proposed
date: 2026-08-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0186: `play` glosses — a catalog of named value renderings for the Table and Detail panes

## Context

[ADR-0123](./0123-play-content-typed-detail-cells.md) gave `play` content-typed
Detail cells — `` `<label>@<mime>` `` renders as one of eight media types, in
the ad-hoc Detail pane only — and left two things on record: §SD7 deferred the
Table tab and the leeway card path, and its Consequences noted that a third
convention now rode on result column names, so "the next one should probably
ask whether the namespace is the right place at all".

The request generalises the mechanism past content: a temperature, a height, a
credit-card number with a check digit. Keep `@` as the trigger; catalogue the
renderings; reach them by an explicit `@name` **and** by mapping leeway
columns to renderings with a regex over a written-out form of the column name
and all its aspects; make them active in the tabular output as well as Detail.

Substrate facts that shape the design:

- **A Table cell is one line of monospace text** — `formatCell(rec, col, row)`
  inside a frameless selectable button, per visible cell per frame; column
  widths are seeded from the same function; the per-attribute leeway grid
  builds its cell strings up front from the inner arrays. No per-cell hook.
- **The leeway Detail card is a text emitter.** `Table2CardEmitter` receives
  values through `WriteString`, and `BeginColumn` already applies a display
  rule keyed on value semantics (machine-readable ∧ ¬human-readable → hidden).
- **The aspect vocabularies carry display-relevant facts but, deliberately, no
  units and no media typing** — ADR-0182's admission criterion routes
  open-domain information to canonical types, `TableOptions` or a catalog. So
  `sem:secret`, `sem:url`, `sem:json*` are matchable; "this float is kelvin"
  never will be. This ADR is that catalog.
- **The written spelling of aspects exists** — ADR-0181 §SD2's `item:` `enc:`
  `sem:` `use:` tokens (prefixed because the vocabularies collide). `lwsql`
  composes names from tokens (§SD6) but does not decompose them.
- **An explicit `@` requires an alias, and an alias strips leeway-ness** — the
  result falls off the card path. A leeway column can only reach a rendering
  through a rule keyed on the physical name; route (b) is structural.
- ADR-0124's `-- play: ungroup` / `-- play: enum` are the in-band directive
  family; ADR-0181's `LwConstructExpand` (passreg standard set, ADR-0108) is
  the client-only constructor-macro shape.
- Measured for this ADR against `clickhouse-local`, grammar1 and `play`'s
  quote-aware splitter: `` AS `t@gloss/temperature;unit=K` `` parses in all
  three, with or without a space after `;`, and a following `; SELECT 2` still
  splits correctly; unquoted, it fails at the `@` as ADR-0122 measured.
  `mime.ParseMediaType` folds the type and keeps parameter values
  case-sensitive (`unit=K`), which units need.

## Design space (QOC)

**Q1 — How is a rendering named after the `@`?**

- *A bare token, `t@celsius`.* Killed: it re-opens ADR-0123 §SD2's rule that a
  slash-less token is never a declaration — the rule that keeps
  `dot_done@success` (ADR-0122) and email-like names silent — and the
  replacement needs either a `dot_` exemption or a near-miss heuristic to stay
  loud on typos.
- *Media-type syntax under a private top-level type, `t@gloss/celsius;unit=K`.*
  Chosen — SD2. The 0123 gate table stands verbatim, parameters come from the
  same parser, and the private type is honest about being play's, not IANA's.
- *A second sigil (`t@@celsius`, `t#celsius`).* Killed: `#` opens a ClickHouse
  comment (ADR-0122's measurement), and any second sigil is a fourth convention
  on the namespace this ADR is trying not to grow.

**Q2 — Where does a column's binding to a gloss come from?**

- *Only the explicit alias.* Killed: it cannot reach a leeway column at all
  (Context).
- *Arrow field metadata.* Killed as in ADR-0123: ClickHouse writes none, and a
  SQL author cannot.
- *A `keelson` registry table keyed by `table.column`.* Killed for v0 as in
  ADR-0123: a result is an expression, and `concat(a, b)` has no key. Recorded
  as the natural home for rules that should outlive a query.
- *Rules over a written-out column, from three tiers — explicit alias, in-band
  directive, gloss affinity.* Chosen — SD3.

**Q3 — What does a rule match against?**

- *The physical name.* Killed: aspects are base62 digit lists in it
  (`…:s:124::I:0::…`) — unreadable and unmatchable.
- *The column's ADR-0181 constructor call text, `LW_TV(_, 'sensor', …)`.*
  Killed: quotes and commas make a hostile regex subject, and non-leeway
  columns have no constructor.
- *A one-line token spelling in ADR-0181's own vocabulary.* Chosen — SD3. What
  a rule matches is what an author would type to mint the column.

## Decision

`play` gains a **glossary**: a catalog of **glosses**, each a named way of
showing a value. A gloss reaches a column by an explicit `` `label@<media
type>` `` alias, by a `-- play: gloss` directive rule, or by the gloss's own
affinity for aspects; the ADR-0123 media types become the content-type family
of the catalog. Glosses render in the Table grids and both Detail paths.

### SD1 — A gloss has two faces, and lives in two layers

- **Inline face** — one line, monospace-safe, cheap enough for every visible
  cell every frame: `Inline{Text, Tone}`, Tone ∈ the ADR-0031 semantic palette.
  `4111 •••• •••• 1111` in *success* for a passing Luhn check, *error* for a
  failing one; `21.5 °C`; `1.83 m`; `••••••`; `[image/png · 359 B]`.
- **Block face** — optional, Detail-only: markdown, code view, image,
  hyperlink. Absent, Detail shows the inline face in a wrapped label.
- **`Accepts(kind)`** over a small value-kind enum (numeric / text / bytes /
  temporal / bool / other) derived from an Arrow type *or* a leeway canonical
  type, so the Arrow-backed grids and the text-backed card ask the same
  question. A mismatch is loud in the ADR-0123 style, never a silent coercion.
- **Parameters** are declared by the gloss and bound once per column
  (`Bind(params) → Instance`), not per cell. **Affinity** — default rules a
  gloss applies to (SD3).
- **Two layers.** Catalog, kinds, cell accessor, rules engine and every
  *inline* face are pure Go in `public/hmi/gloss` — no imzero2 — so they test
  without a GUI and the SD7 macro can validate against them. Block faces are
  imzero2 code bound to a gloss by name in `play`, where ADR-0123's renderers
  already live. A host registers a gloss once, and a block face once if it has
  one.

### SD2 — Naming: `label@<media type>`; the 0123 gate stands; `gloss/` is a private top-level type

The token after `@` is a media type in the RFC 2045 §5.1 sense, parsed by
`mime.ParseMediaType`. ADR-0123 §SD2's five-row table is unchanged; "known
type" now means "in the catalog":

| declared type | family | example |
| --- | --- | --- |
| a registered IANA type | content | `` `notes@text/markdown` `` |
| `gloss/<name>[;k=v…]` | presentation | `` `t@gloss/temperature;unit=K` `` |
| any other type with a slash | — | plain, reason shown (0123 §SD2) |

`gloss` is play's private top-level type, never on any wire: a display
directive in media-type syntax, not the type of any bytes. Not `x-gloss/`
(RFC 6838 §3.4 discourages the prefix; it buys nothing), not
`application/vnd.boxer.…` (unreadable at the point of use).

**Parameters are validated.** A gloss declares its parameter names; an
undeclared name is loud like an unknown type, so `;unti=K` cannot silently
render as °C. The content family declares `charset` (accepted, ignored) on the
text types and reserves `encoding` on the image types (0123 §SD2's
`;encoding=base64`, still deferred).

`gloss/raw` is a catalog member — identity inline face, no block face — so a
directive can override an affinity without a reserved word (SD3). Kanban's
`dot_<label>@<token>` is untouched: slash-less, outside the gate.

### SD3 — The rule route: spec line, rules, precedence, directive

**The spec line** is a one-line, space-separated token spelling of a result
column, computed once per schema and cached alongside `colLabels`. Prefixes are
ADR-0181's where one exists; the rest follow the same shape:

```text
name:temperature section:sensor role:val ct:f64 arrow:float64 enc:… sem:measured sem:scale-of-measurement-metric-ratio use:…
name:ts item:ts ct:z64 arrow:timestamp[ms, tz=UTC] sem:transaction-time
name:temp_c arrow:float64
```

Token order is fixed as shown; aspects repeat one token each, in enum order,
spelled by their `String()` — the same spellings the constructor family
parses; `role:` uses the physical role spelling (`val`, `lr`, `len`, …); `ct:`
the canonical type; `arrow:` the Arrow type's own `String()`. Backbone columns
carry `item:` and no `section:`/`use:`; a non-leeway column carries `name:` and
`arrow:` only. It is built by a new `lwsql.SpecLine` — the read-direction dual
of `Composer` (0181 §SD6) — over `ddl.ParseColumn` and the `Extract*` calls.

**A rule** is a Go RE2 regex, unanchored and case-sensitive, matched against
the spec line, plus a media type. Rules are ordered; the first match wins.

**Precedence, per column:** explicit alias › directive rules in buffer order ›
affinities in catalog order › none; an aliased column is never offered to the
rules. The header hover names the winner and its source and shows the spec
line; the Glossary tab (SD6) lists shadowed matches.

**The directive** joins ADR-0124's family:

```sql
-- play: gloss gloss/temperature;unit=K name:.*temp\b
-- play: gloss gloss/raw sem:secret
```

`<token>` is the first whitespace-delimited word (the compact `;k=v` form —
the Glossary tab's Insert button writes it), the regex is the rest of the line,
trimmed. An unknown type, an undeclared parameter, an empty or invalid regex
each surface as a note, as `-- play: enum` errors do; the buffer still runs.

**Affinities in v0**, narrow by intent: `gloss/secret` ← `\bsem:secret\b`;
`gloss/url` ← `\bsem:url\b`; `application/json` ← `\bsem:json(-scalar|-array|-object)?\b`.
Units have no aspect and hence no affinity — that is what the directive is for.

### SD4 — Where glosses render

| surface | face | mechanism |
| --- | --- | --- |
| Table, per-row grid | inline | the cell text and the width seed come from the resolved instance instead of `formatCell`; tone colours the run |
| Table, per-attribute grid | inline | applied where the sink builds each cell string, on the inner array |
| Detail, ad-hoc | block, else inline | ADR-0123's `renderRichCell` generalised; the `(executed, row)` cache keeps its shape |
| Detail, leeway card | inline | one seam on `Table2CardEmitter`: a per-column glosser set by the host, consulted in `BeginColumn` next to the hide rule and applied to the cell text in `EndColumn` — the "Secret mask rule mirrors the hide rule" note becomes this |
| column header | — | label + small gloss name; physical name, spec line and rule on hover |

A **raw** toggle on the Table options bar bypasses every gloss for the session
— the escape hatch a wrong rule needs. Sorting is untouched: it permutes rows
on the raw values. Block faces on the leeway card stay deferred (card rows are
text; SD8).

### SD5 — The v0 catalog

| media type | accepts | params | inline face | block face | affinity |
| --- | --- | --- | --- | --- | --- |
| `text/markdown`, `text/plain`, `application/json`, `application/sql`, `text/x-go` | text, bytes | `charset` | first line (0123's `firstLineOf`) | as ADR-0123 | json ← `sem:json*` |
| `image/png`, `image/jpeg`, `image/gif` | bytes | (`encoding` reserved) | `[<type> · <size>]` — no decode | as ADR-0123 | — |
| `gloss/temperature` | numeric | `unit` ∈ K, C, F (the stored unit) | `21.5 °C`, one decimal | — | — |
| `gloss/length` | numeric | `unit` ∈ m, cm, mm, km, ft (stored unit) | auto-scaled SI, `1.234 km` | — | — |
| `gloss/bytes` | numeric ≥ 0 | — | `humanize.IBytes` | — | — |
| `gloss/luhn` | text, numeric | — | groups of four, middle groups masked, ✓/✗ tone by check digit | mask + verdict line | — |
| `gloss/secret` | any | — | `••••••`, never length-revealing | same | ← `sem:secret` |
| `gloss/url` | text | — | text in accent tone | `HyperlinkTo` | ← `sem:url` |
| `gloss/raw` | any | — | `formatCell` | — | — |

One or two exemplars per archetype (unit formatting, check digit, masking,
linking) is the whole of v0; the rest are one-file additions against the same
interface. The quantity family is spelled after `public/science/units`
(`temperature`, `length`, `mass`, `duration`, …) so later members have names
waiting; unit *conversion* (`;show=F`) is deferred — v0 formats the stored unit.

### SD6 — The Glossary tab

A result-side sibling of the Vocabulary tab (ADR-0174): every gloss with its
accepted kinds, parameters, a sample inline rendering and its affinities; the
effective rule list of the current buffer; and, for the current result, each
column's spec line and resolution (gloss, source, or the reason a declaration
was refused). Insert-at-caret writes a `-- play: gloss` line or a
`gloss(…)` call (SD7).

### SD7 — `gloss(…)`: a constructor macro

A client-side nanopass macro in the ADR-0181 shape, registered in the passreg
standard set next to `LwConstructExpand`:

```sql
SELECT gloss(reading, 'gloss/temperature', 'unit', 'K'),
       gloss(a + b,  'gloss/length', 'unit', 'm', 'label', 'span')
```

expands to `` reading AS `reading@gloss/temperature;unit=K` `` and
`` a + b AS `span@gloss/length;unit=m` ``. Its value over the alias: no
backticks to forget; the media type and its parameters are **validated at
rewrite time** against the catalog (a late-bound Factory hook, ADR-0108 §SD7),
so an unknown gloss is a Diagnostics-tab error with a source range rather than
a per-cell note; and it is discoverable in the Vocabulary tab's Client section.

- **Arguments:** the expression, then the media type as a string literal, then
  `key, value` pairs. Values are ClickHouse literals of any supported type —
  numbers, strings, and whatever else the nanopass `marshalling` package reads
  — marshalled to the parameter's text form and quoted per RFC 2045 by
  `mime.FormatMediaType`, so `'digits', 1` and `'unit', 'K'` both spell
  correctly. Non-literal arguments are a loud rejection with the call's
  source range, as in the LW_ family.
- **`label`** is the one reserved key: it names the alias. Absent, a bare
  (possibly qualified) identifier expression contributes its own name; any
  other expression contributes its source text, as ClickHouse itself would.
- **Position rule:** legal only as a whole projection item (0181 §SD2's rule).
- Never installed server-side; on a raw endpoint it fails as an unknown
  function.

### SD8 — Deferred

- Block faces on the leeway card, and reveal-on-click for `gloss/secret`.
- A paint variant of the inline face (array cells as sparklines) — the
  contract is one method away, the imzero2 cell budget is not measured.
- Per-deployment rule files or env, and an in-app rule editor persisted through
  ADR-0185 — text rules first, because SQL travels and UI state does not.
- Unit conversion, `iban`/`isbn`/`epoch`/`duration`/`percent`, colour swatches.
- ADR-0123 §SD7's own list (base64 / `data:` URIs / URL sources, webp / avif /
  svg) stands.
- Reading kanban's `dot_*@<tone>` tokens as glosses — a tempting unification
  that would move ADR-0122's contract; not taken.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/hmi/gloss` (new exported Go API) | catalog, `Gloss`/`Instance`, value kinds, cell accessor, rules engine, built-in inline faces (SD1, SD5) | play's block-face bindings; the SD7 pass; the tests moved from `play_detail_rich_test.go` |
| `public/hmi/gloss/glosssql` + passreg standard set | new pass entry `gloss(…)` (SD7) | `defaults.RegisterStandard` order comment; the Vocabulary tab's Client list |
| `lwsql` (exported Go API under `public/`) | +`SpecLine` (SD3) | golden over the leeway fixture names |
| `leewaywidgets.Table2CardEmitter` (exported Go API under `public/`) | +1 setter for a per-column glosser (SD4) | play's card driver wiring |
| result-column naming convention (ADR-0123 §SD2/§SD3) | "known type" = catalog; `gloss/` family added; parameters validated | 0123's status text and §SD7 list; `features.md` §Detail/§Table; `snippets.md` |
| `-- play:` directive family (ADR-0124) | +`gloss` (SD3) | `features.md` §Query parameters cross-reference |
| play tab roster | +Glossary (SD6) | Panes menu; the tab-marks table |

## Alternatives

- **Sniffing values, per-type prefixes (`md_`), Arrow metadata.** Rejected in
  ADR-0123; nothing here changes the reasons.
- **Peeling `@…` off a physical leeway name before classification**, so an
  explicit alias could keep leeway-ness. Rejected: it puts a play convention
  inside leeway's name parser or rewrites the schema at every classifier site;
  the rule route needs neither.
- **An `LW_`-prefixed macro.** Rejected: `LW_` is leeway's namespace and a
  gloss is not leeway; play's client macros are lower-case (`keelson`,
  `docsearch`).
- **A per-column overrides UI first, Grafana-style.** Rejected on sequencing:
  a rule must exist as text before it can exist as a widget; SD6 shows and
  inserts text.
- **A gloss per unit (`gloss/celsius`, `gloss/kelvin`).** Rejected once
  parameters were free: one family per quantity, the unit a parameter.

## Consequences

### Positive

- One catalog answers "how is this column shown" for content and non-content
  values alike, reachable without an alias for leeway data.
- ADR-0123's gate and its measured separator survive unchanged; the fourth
  convention on the column namespace is a rule set, not a name.
- Every inline face is testable in plain Go; hosts other than play can share
  the catalog and the macro.

### Negative

- Six more characters per explicit alias (`gloss/`).
- The Table grid does per-cell work it did not do — bounded to the visible
  range and to inline faces, but a Luhn check per visible cell per frame is
  more than a `strconv`.
- The catalog is one more registry to keep documented; SD6 exists to keep that
  honest.

### Neutral

- Two spellings live under one syntax: IANA types for content, `gloss/` for
  presentation. The catalog treats them identically.
- `Table2CardEmitter` grows a display hook it did not need for its own demo.

## Migration — Tier 1

- **Breaks.** Nothing at rest and nothing in SQL: every ADR-0123 declaration
  parses to the same gloss. In Go, `richKindE` / `parseRichColumn` /
  `richCellCache` fold into the catalog and its cache; only `play` calls them.
- **Path.** Additive. Hosts embedding `PlayApp` (sqlapplet) inherit the default
  catalog; a host that wants more registers glosses at wiring time.
- **Regeneration.** None — no IDL, codegen or FFI change; the card seam is a Go
  method.
- **Old shape.** ADR-0123's names and semantics are kept indefinitely as the
  content family; its file is edited in place (still *proposed*) to point here.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the catalog, the parse table, every inline
  face, the rules engine and the spec line; a nanopass golden for `gloss(…)`
  expansions; `clickhouse-local` for the alias measurements; a headless play
  scene for the Table + Detail rendering, per the play screenshot recipe.
- **What would fail.**
  - The ADR-0123 parse-table cases, moved verbatim (`dot_done@success` and an
    email-like name silent, `notes@text/markdwn` loud, `TEXT/Markdown` folded);
    plus `;unit=K` accepted, `;unti=K` refused with the reason, `unit=k`
    refused (values are case-sensitive).
  - One golden per built-in inline face — Luhn pass and fail with their tones;
    a `gloss/secret` face of equal length for inputs of different lengths.
  - A spec-line golden over the leeway fixture names: fixed token order,
    aspects by `String()`, backbone with `item:`, non-leeway with `name:` and
    `arrow:` only.
  - Precedence: alias beats directive beats affinity; a `gloss/raw` directive
    suppresses `gloss/secret`'s affinity.
  - The `gloss(…)` golden: label rules, typed parameter marshalling, the
    position rule, a non-literal argument rejected with a source range.
- **Gap.** The card hook's visual result and the header hover text are checked
  by hand at the milestone; the per-cell cost of inline faces on a wide visible
  range is not benchmarked here (the raw toggle is the mitigation).

## Milestones

- **M0 — the core.** `public/hmi/gloss`: catalog, kinds, cell accessor, params,
  the content family with inline faces; ADR-0123's parser and tests migrated,
  behaviour identical.
- **M1 — Table.** Inline faces in both grids, width seed, tone runs, header
  label + hover, the raw toggle; `gloss/temperature`, `gloss/length`,
  `gloss/bytes`, `gloss/luhn`, `gloss/secret`, `gloss/url`, `gloss/raw`.
- **M2 — rules.** `lwsql.SpecLine`, the rules engine, affinities, the
  `-- play: gloss` directive, precedence and hover provenance.
- **M3 — Detail.** Block faces bound in play; the card seam; the Glossary tab.
- **M4 — the macro.** `glosssql` + passreg entry + Vocabulary listing +
  goldens.
- **M5 — docs.** `features.md` §Table/§Detail/§Glossary, `snippets.md`, the
  ADR-0123 pointer edits.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0123](./0123-play-content-typed-detail-cells.md) — content-typed detail cells; the gate this ADR keeps.
- [ADR-0122](./0122-play-kanban-panel.md) §SD2 — the measured `@` separator; `dot_*@<token>`.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md), [ADR-0121](./0121-selection-condition-columns.md) — the other conventions on result column names.
- [ADR-0124](./0124-play-param-editing-widgets.md) — the `-- play:` directive family.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) §SD2/§SD6 — spec tokens, the constructor family, `Composer`.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — the aspect vocabularies and their admission criterion.
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — passreg, the late-bound Factory.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — the Vocabulary tab the Glossary tab mirrors.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the semantic palette behind `Tone`.
- RFC 2045 §5.1 (media-type parameter syntax), RFC 6838 §3.4 (`x-` discouraged); Go `mime.ParseMediaType` / `FormatMediaType`.
